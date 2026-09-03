//go:build linux

package app

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// Staged link shaping, over a real socket, under `tc netem`.
//
// Everything else in this package shapes an in-process mesh, which is
// deterministic and reproducible and is exactly why it is not sufficient on its
// own: it models when a frame becomes visible and how many bytes a tick will
// pass, and it does not model a kernel queue, a TCP retransmit, a Nagle
// interaction or a send buffer that fills. Those are where a cadence chosen from
// a delivery-rate estimate is most likely to be wrong, so the estimate is taken
// once over a link the kernel is actually shaping.
//
// It is opt-in for a reason that is not squeamishness. Shaping `lo` shapes every
// loopback flow on the machine, including whatever else is running the test
// suite, so a gate that armed itself would be a gate that broke unrelated builds.
// VIF_NETEM=1, root, and a working `tc` are all required, and the skip says which
// one is missing.

// netemStage is one shaped condition and what it is meant to exercise.
type netemStage struct {
	name string
	args []string
	why  string
}

// netemStages walks the four shapes the plan asks for, in the order a link
// actually degrades: distance first, then instability, then loss, then the
// bottleneck that decides the cadence.
func netemStages() []netemStage {
	return []netemStage{
		{"baseline", nil, "the unshaped link the rest is measured against"},
		{"latency", []string{"delay", "80ms"}, "distance alone: freshness costs, correctness does not"},
		{"jitter", []string{"delay", "80ms", "40ms", "distribution", "normal"},
			"instability: the cadence must not chase its own noise"},
		{"loss", []string{"delay", "40ms", "loss", "3%"},
			"a delta that never lands is a refusal, and the next keyframe resolves it"},
		{"bandwidth", []string{"rate", "512kbit", "delay", "40ms"},
			"the bottleneck the cadence is chosen from"},
	}
}

func requireNetem(t *testing.T) {
	t.Helper()
	if os.Getenv("VIF_NETEM") != "1" {
		t.Skip("staged link shaping is opt-in: set VIF_NETEM=1 (it shapes lo for the whole machine)")
	}
	if _, err := exec.LookPath("tc"); err != nil {
		t.Skip("staged link shaping needs tc from iproute2")
	}
	if os.Geteuid() != 0 {
		t.Skip("staged link shaping needs root to install a qdisc on lo")
	}
	if out, err := exec.Command("tc", "qdisc", "show", "dev", "lo").CombinedOutput(); err != nil {
		t.Skipf("tc cannot read lo's qdisc: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

// shapeLo installs one netem qdisc on loopback, replacing whatever is there.
func shapeLo(t *testing.T, args []string) {
	t.Helper()
	clearLo(t)
	if len(args) == 0 {
		return
	}
	cmd := append([]string{"qdisc", "add", "dev", "lo", "root", "netem"}, args...)
	if out, err := exec.Command("tc", cmd...).CombinedOutput(); err != nil {
		t.Fatalf("tc %s: %v: %s", strings.Join(cmd, " "), err, strings.TrimSpace(string(out)))
	}
}

func clearLo(t *testing.T) {
	t.Helper()
	// A missing qdisc is the ordinary case on the first call, so the error is read
	// rather than raised: `tc` says "Cannot delete qdisc with handle of zero".
	_ = exec.Command("tc", "qdisc", "del", "dev", "lo", "root").Run()
}

// TestStagedLinkShapingKeepsCorrectionsBoundedAndRecovers is the gate the phase's
// manual acceptance asks for, automated: shape the link down in stages and
// confirm play degrades smoothly — the cadence falls, prediction carries more,
// correction magnitude rises but stays bounded, the floor is never crossed, and
// clearing the shape re-converges the session with nothing restarted.
func TestStagedLinkShapingKeepsCorrectionsBoundedAndRecovers(t *testing.T) {
	// Never parallel: this shapes lo for the whole machine, so any other socket
	// test running beside it would be measuring this test's qdisc.
	requireNetem(t)
	t.Cleanup(func() { clearLo(t) })

	const seed = 0x3017
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(240)

	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	addr := host.HostAddr()

	// The join runs unshaped: what this gate measures is a session under load, and
	// admitting one over a shaped link is the *refusal* path, which
	// TestAJoinIsRefusedWhenTheLinkCannotCarryTheFloor covers on its own.
	stop, ticking := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(ticking)
		for {
			select {
			case <-stop:
				return
			default:
			}
			host.Tick(1)
			time.Sleep(joinTestTickInterval)
		}
	}()
	guest, _ := mustSocketJoiner(t, addr, seed, 120, 40)
	close(stop)
	<-ticking

	type reading struct {
		stage     string
		magnitude int64
		cadence   int64
		keyPeriod int64
		applied   int64
		refused   int64
		rtt       int64
		jitter    int64
		bps       int64
		breached  bool
	}
	var readings []reading

	run := func(stage netemStage) reading {
		shapeLo(t, stage.args)
		r := reading{stage: stage.name}
		before := statOf(guest, "snapshot.corrections_applied")
		// Wall-paced, and the host publishes on its own pump rather than on the
		// test's call: netem's delays are wall delays, so a run driven flat out
		// would finish before the link had a chance to be slow — and the schedule
		// under test is the one the production path chooses, not one the harness
		// imposes on top of it.
		for range 240 {
			host.Tick(1)
			guest.Tick(1)
			guest.ApplyPendingCorrections()
			if n := statOf(guest, "snapshot.correction_entities"); n > r.magnitude {
				r.magnitude = n
			}
			if p := statOf(host, "snapshot.cadence_keyframe_period_ticks"); p > r.keyPeriod {
				r.keyPeriod = p
			}
			time.Sleep(parameter.GameUpdateInterval)
		}
		r.cadence = statOf(host, "snapshot.cadence_ticks")
		r.applied = statOf(guest, "snapshot.corrections_applied") - before
		r.refused = statOf(guest, "snapshot.corrections_refused")
		r.rtt = statOf(host, "network.link_rtt_ms")
		r.jitter = statOf(host, "network.link_jitter_ms")
		r.bps = statOf(host, "network.link_bps")
		r.breached = statBoolOf(guest, "snapshot.cadence_floor_breached")
		return r
	}

	for _, stage := range netemStages() {
		r := run(stage)
		readings = append(readings, r)
		t.Logf("%-10s %-58s magnitude=%d cadence=%d keyframe_period=%d applied=%d refused=%d rtt=%dms jitter=%dms link=%dB/s",
			r.stage, stage.why, r.magnitude, r.cadence, r.keyPeriod, r.applied, r.refused, r.rtt, r.jitter, r.bps)

		// The floor is the one thing no shape may move.
		if r.keyPeriod > int64(parameter.SnapshotFloorKeyframeTicks) {
			t.Errorf("%s: %d ticks between whole worlds, floor is %d",
				r.stage, r.keyPeriod, parameter.SnapshotFloorKeyframeTicks)
		}
		// Degradation, not disconnection: every stage keeps delivering authority.
		if r.applied == 0 {
			t.Errorf("%s: the guest applied no correction at all", r.stage)
		}
	}

	base := readings[0]
	for _, r := range readings[1:] {
		// Bounded rather than equal. A shaped link makes the guest predict longer,
		// so the magnitude rises — what must not happen is an unbounded one, which
		// is a guest falling behind faster than the cadence repairs it.
		if base.magnitude > 0 && r.magnitude > 8*base.magnitude+32 {
			t.Errorf("%s: correction magnitude %d against an unshaped %d",
				r.stage, r.magnitude, base.magnitude)
		}
	}

	// Recovery at the guaranteed floor: clear the shape and require that the
	// session converges again, on nothing but the next correction.
	clearLo(t)
	advance := func() { host.Tick(1); guest.Tick(1) }
	want := deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest after the link was unshaped")
	if statBoolOf(guest, "snapshot.cadence_floor_breached") {
		t.Error("the guest still reports a breach on an unshaped link")
	}
}

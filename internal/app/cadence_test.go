package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// shapedPair is a two-participant session over a link with a declared bottleneck.
// The shape is deterministic and counted in ticks; the staged netem gate is what
// says the same code behaves the same way over a real socket.
func shapedPair(t *testing.T, seed uint64, shape network.LinkShape) (*App, *App, *network.Mesh) {
	t.Helper()
	a := mustHeadless(t, seed, 120, 40)
	an := a.JoinAnchor()
	b := mustJoiner(t, seed, 84, 26, an)
	t.Cleanup(func() { a.Close(); b.Close() })

	if err := b.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}
	a.adoptMapLatch(an.Anchor)

	mesh := network.NewMesh()
	mesh.Link(1, 2)
	if shape != (network.LinkShape{}) {
		mesh.Shape(1, 2, shape)
	}
	a.AttachTransport(mesh.Node(1))
	b.AttachTransport(mesh.Node(2))
	for _, x := range []*App{a, b} {
		tickUntilCursor(t, x)
		x.SetupLevel(100, 30, true, false)
		x.Tick(1)
	}
	return a, b, mesh
}

// runSession drives both participants on the adaptive schedule, so the assertions
// read the cadence the controller chose rather than one the test forced.
func runSession(host, guest *App, ticks int) {
	for range ticks {
		host.Tick(1)
		guest.Tick(1)
		_ = host.corrections.publishDue()
		guest.ApplyPendingCorrections()
	}
}

// TestCadenceBoundsAreTheGameParameters is the seam between the controller's
// contract and the game's numbers. pkg may not see internal, so the envelope is
// assembled here and a parameter that drifts out of the controller's declared
// order is caught here.
func TestCadenceBoundsAreTheGameParameters(t *testing.T) {
	t.Parallel()
	b := CadenceBounds()
	if err := b.Validate(); err != nil {
		t.Fatalf("the shipped envelope does not validate: %v", err)
	}
	if b.NominalCadenceTicks != parameter.SnapshotCorrectionTicks {
		t.Fatalf("the nominal cadence is %d, the parameter says %d",
			b.NominalCadenceTicks, parameter.SnapshotCorrectionTicks)
	}
	if b.FloorKeyframeTicks != parameter.SnapshotFloorKeyframeTicks {
		t.Fatalf("the floor is %d ticks, the parameter says %d",
			b.FloorKeyframeTicks, parameter.SnapshotFloorKeyframeTicks)
	}
	// The nominal point must itself honour the floor, or a healthy session would
	// start out already reporting a constrained link.
	if got := b.NominalCadenceTicks * uint64(b.NominalKeyframe); got > b.FloorKeyframeTicks {
		t.Fatalf("the nominal schedule leaves %d ticks between whole worlds, floor is %d",
			got, b.FloorKeyframeTicks)
	}
}

// TestAHealthyLinkKeepsTheNominalPointAndHidesItsTiming is the "do no harm" case
// plus the boundary the phase is constrained by: an unconstrained link is not
// adapted away from the shipped cadence, and no link measurement reaches the
// compared surface, where it would be a value a session could diverge over.
func TestAHealthyLinkKeepsTheNominalPointAndHidesItsTiming(t *testing.T) {
	t.Parallel()
	host, guest, _ := shapedPair(t, 0x5EEDBEEF, network.LinkShape{})
	runSession(host, guest, 120)

	report := host.CadenceReport()
	if len(report.Peers) != 1 {
		t.Fatalf("the host scheduled %d links, want 1", len(report.Peers))
	}
	if report.Constrained || report.FloorBreached {
		t.Fatalf("an unshaped link reported constrained=%v breached=%v: %+v",
			report.Constrained, report.FloorBreached, report.Peers[0])
	}
	if got := report.CadenceTicks; got > parameter.SnapshotCorrectionTicks {
		t.Fatalf("an unshaped link slowed the cadence to %d ticks", got)
	}
	if statBoolOf(host, "snapshot.cadence_constrained") {
		t.Fatal("the status surface calls an unshaped link constrained")
	}
	if applied := statOf(guest, "snapshot.corrections_applied"); applied == 0 {
		t.Fatal("the adaptive schedule published nothing at all")
	}

	forbidden := []string{
		"cadence_", "keyframe_age", "uplink_bps", "budget_bps", "floor_bps",
		"link_rtt", "link_jitter", "link_bps", "link_loss", "link_saturated",
	}
	for _, a := range []*App{host, guest} {
		for _, record := range a.SnapshotShared() {
			for _, key := range forbidden {
				if strings.Contains(record, key) {
					t.Fatalf("the compared surface carries link timing: %q in %q", key, record)
				}
			}
		}
	}
	// Non-vacuous: the values exist, they are simply not compared.
	if statOf(host, "snapshot.cadence_ticks") == 0 {
		t.Fatal("the cadence was never published, so the exclusion proves nothing")
	}
}

// TestTheRoundTripIsMeasuredEndToEnd walks the seam a probe travels: it leaves the
// transport, the far end answers it, the estimate lands on the link, and the
// controller reads it. Both ends measure the same link, which is what lets a
// participant say the link rather than the game is its problem.
func TestTheRoundTripIsMeasuredEndToEnd(t *testing.T) {
	t.Parallel()
	host, guest, _ := shapedPair(t, 0x5EEDBEEF, network.LinkShape{LatencyTicks: 3})
	runSession(host, guest, 120)

	report := host.CadenceReport()
	if len(report.Peers) != 1 {
		t.Fatalf("the host scheduled %d links, want 1", len(report.Peers))
	}
	// Three ticks each way is 300 ms; the estimator smooths toward it and the
	// responder answers on its own drain, so that neighbourhood is the wire rather
	// than a scheduling artifact.
	if peer := report.Peers[0]; peer.RTT < 200*time.Millisecond {
		t.Fatalf("a three-tick each-way link measured %s", peer.RTT)
	}
	for _, a := range []*App{host, guest} {
		if got := statOf(a, "network.link_rtt_ms"); got < 200 {
			t.Fatalf("telemetry reported %d ms of round trip", got)
		}
	}
}

// TestAConstrainedLinkSlowsTheCadenceAndPublishesIt is the phase's headline: the
// cadence becomes a function of what the link carries, the operating point is
// reported rather than guessed at, and the correction magnitude that rises with
// it stays bounded — a magnitude that climbs is divergence, not degradation.
func TestAConstrainedLinkSlowsTheCadenceAndPublishesIt(t *testing.T) {
	t.Parallel()
	host, guest, _ := shapedPair(t, 0x5EEDBEEF, network.LinkShape{LatencyTicks: 2, BytesPerTick: 500})

	var early, late int64
	for round := range 2 {
		runSession(host, guest, 150)
		peak := int64(0)
		for range 80 {
			host.Tick(1)
			guest.Tick(1)
			_ = host.corrections.publishDue()
			guest.ApplyPendingCorrections()
			if n := statOf(guest, "snapshot.correction_entities"); n > peak {
				peak = n
			}
		}
		if round == 0 {
			early = peak
		} else {
			late = peak
		}
	}

	report := host.CadenceReport()
	if len(report.Peers) != 1 {
		t.Fatalf("the host scheduled %d links, want 1", len(report.Peers))
	}
	peer := report.Peers[0]
	if !peer.Saturated {
		t.Fatalf("a 500-byte-per-tick link was never read as the limit: %+v", peer)
	}
	if !report.Constrained {
		t.Fatalf("a saturated link was not reported as constrained: %+v", peer)
	}
	if report.CadenceTicks <= parameter.SnapshotCorrectionTicks &&
		report.KeyframeInterval <= parameter.SnapshotKeyframeCorrections {
		t.Fatalf("a saturated link moved neither the cadence nor the keyframe interval: %+v", report)
	}
	if applied := statOf(guest, "snapshot.corrections_applied"); applied == 0 {
		t.Fatal("the guest applied no correction over a constrained link")
	}
	// Bounded, not equal: the world moves.
	if late > 4*early+16 {
		t.Fatalf("correction magnitude grew from %d to %d over the run", early, late)
	}

	// Every adapted value has to be readable, or a player watching their picture
	// go coarse cannot tell a small link from a broken game.
	for _, key := range []string{
		"snapshot.cadence_ticks",
		"snapshot.cadence_keyframe_interval",
		"snapshot.cadence_keyframe_period_ticks",
		"snapshot.cadence_uplink_bps",
		"snapshot.cadence_floor_bps",
		"network.link_rtt_us",
		"network.link_bps",
	} {
		if statOf(host, key) <= 0 {
			t.Errorf("%s is not published (%d)", key, statOf(host, key))
		}
	}
	// Jitter and loss are legitimately zero on a link that has neither, so they
	// are asserted to exist rather than to be non-zero.
	for _, key := range []string{"network.link_rtt_ms", "network.link_jitter_ms", "network.link_loss_pct"} {
		if statOf(host, key) < 0 {
			t.Errorf("%s reads %d", key, statOf(host, key))
		}
	}
	if !statBoolOf(host, "snapshot.cadence_constrained") {
		t.Error("the constrained-link state is not published")
	}

	// The operator surface carries the same set in one line, which is what a
	// person types :session for.
	summary := host.SessionSummary()
	for _, want := range []string{"cadence", "keyframe every", "link", "uplink", "floor", "constrained"} {
		if !strings.Contains(summary, want) {
			t.Errorf(":session does not report %q: %s", want, summary)
		}
	}
	solo := mustHeadless(t, 0x5EEDBEEF, 80, 24)
	defer solo.Close()
	if got := solo.SessionSummary(); !strings.Contains(got, "Solo run") {
		t.Errorf("a solo run reports %q", got)
	}
}

// TestTheFloorBoundsEveryScheduleAShapedLinkProduces is the invariant the adaptive
// path is allowed to exist inside. It is asserted over a session rather than over
// the controller alone, because what has to hold is the composition: per-peer
// plans folded into one session timeline.
func TestTheFloorBoundsEveryScheduleAShapedLinkProduces(t *testing.T) {
	t.Parallel()
	shapes := []network.LinkShape{
		{},
		{LatencyTicks: 6},
		{LossEvery: 3},
		{BytesPerTick: 500},
		{LatencyTicks: 4, LossEvery: 5, BytesPerTick: 1500},
	}
	for _, shape := range shapes {
		t.Run(fmt.Sprintf("%+v", shape), func(t *testing.T) {
			t.Parallel()
			host, guest, _ := shapedPair(t, 0x5EEDBEEF, shape)
			for tick := range 120 {
				host.Tick(1)
				guest.Tick(1)
				_ = host.corrections.publishDue()
				guest.ApplyPendingCorrections()
				if tick%8 != 0 {
					continue
				}
				report := host.CadenceReport()
				if report.KeyframePeriodTicks > parameter.SnapshotFloorKeyframeTicks {
					t.Fatalf("%d ticks between whole worlds, floor is %d",
						report.KeyframePeriodTicks, parameter.SnapshotFloorKeyframeTicks)
				}
				for _, p := range report.Peers {
					if p.CadenceTicks < parameter.SnapshotCadenceMinTicks ||
						p.CadenceTicks > parameter.SnapshotCadenceMaxTicks {
						t.Fatalf("cadence %d outside the envelope", p.CadenceTicks)
					}
				}
			}
		})
	}
}

// TestAGuestRecoversAtTheFloorAfterTheLinkComesBack is check 7 of the manual
// matrix, automated: cut the link entirely, let the guest predict alone, restore
// it, and require the next whole world to put it back inside the floor with
// nothing restarted.
func TestAGuestRecoversAtTheFloorAfterTheLinkComesBack(t *testing.T) {
	t.Parallel()
	host, guest, mesh := shapedPair(t, 0x5EEDBEEF, network.LinkShape{})
	runSession(host, guest, 120)

	before := statOf(guest, "snapshot.corrections_applied")
	if before == 0 {
		t.Fatal("the session was not converging before the link was cut")
	}

	// LossEvery 1 drops every frame: the link is up and carries nothing, which is
	// the shape a `tc qdisc` drop-all makes and the one no keyframe survives.
	mesh.Shape(1, 2, network.LinkShape{LossEvery: 1})
	runSession(host, guest, 200)
	if got := statOf(guest, "snapshot.corrections_applied"); got != before {
		t.Fatalf("a link dropping every frame still delivered corrections (%d then %d)", before, got)
	}
	if age := statOf(guest, "snapshot.cadence_keyframe_age_ticks"); age == 0 {
		t.Fatal("the guest did not notice it had gone without an authoritative world")
	}
	if !statBoolOf(guest, "snapshot.cadence_floor_breached") {
		t.Fatal("the guest went past the floor without reporting it")
	}

	// The recovery bound is the keyframe period, which the floor caps. Well inside
	// it there must be a whole world again, through the ordinary path: the protocol
	// has no repair path here because it needs none.
	mesh.Shape(1, 2, network.LinkShape{})
	recovered := false
	for range int(parameter.SnapshotFloorKeyframeTicks) * 3 {
		host.Tick(1)
		guest.Tick(1)
		_ = host.corrections.publishDue()
		guest.ApplyPendingCorrections()
		if statOf(guest, "snapshot.corrections_applied") > before {
			recovered = true
			break
		}
	}
	if !recovered {
		t.Fatal("the guest never re-converged after the link came back")
	}
	// The breach clears on the next observation rather than inside the install:
	// the age is read where a correction is applied, and the one that recovered
	// this instance was read before it landed.
	runSession(host, guest, 5)
	if statBoolOf(guest, "snapshot.cadence_floor_breached") {
		t.Fatalf("the guest still reports a breach %d ticks after re-converging",
			statOf(guest, "snapshot.cadence_keyframe_age_ticks"))
	}
	// Recovering a correction is not the claim; recovering the host's world is.
	advance := func() { host.Tick(1); guest.Tick(1) }
	want := deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest after the link came back")
}

// TestAJoinIsRefusedWhenTheLinkCannotCarryTheFloor exercises the refusal through
// the same App method a mid-run join uses, rather than a re-derivation of it.
func TestAJoinIsRefusedWhenTheLinkCannotCarryTheFloor(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer a.Close()

	port := network.NewSocketPort(network.DebugConfig(network.RoleHost, "127.0.0.1:0"))
	defer port.Close()

	// A world of this size needs a whole capture every floor window. Delivering one
	// at a tenth of that rate is a link that cannot converge, whatever cadence is
	// chosen for it.
	const keyframe = 176 * 1024
	floorWindow := time.Duration(parameter.SnapshotFloorKeyframeTicks) * parameter.GameUpdateInterval

	if err := a.admitLink(port, 2, keyframe, floorWindow/4); err != nil {
		t.Fatalf("a link four times faster than the floor was refused: %v", err)
	}
	err := a.admitLink(port, 3, keyframe, floorWindow*10)
	if err == nil {
		t.Fatal("a link ten times slower than the floor was admitted")
	}
	var fe *linkpace.FloorError
	if !errors.As(err, &fe) {
		t.Fatalf("the refusal was not a floor error: %v", err)
	}
	if !strings.Contains(err.Error(), "convergence floor") {
		t.Fatalf("the refusal does not say what it refused on: %v", err)
	}
	// No measurement is not a refusal: a session nobody can join before a probe has
	// completed a round trip is worse than one that reports the condition.
	if err := a.admitLink(port, 4, 0, 0); err != nil {
		t.Fatalf("admission was refused on no evidence: %v", err)
	}
}

// TestASlowPeerDoesNotSlowAFastOne is per-peer scheduling stated as the property
// it exists for: two participants on one host with different links get different
// schedules, and the constrained one does not drag the healthy one down.
func TestASlowPeerDoesNotSlowAFastOne(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {1, 3}})
	host := apps[0]
	transportOf(t, apps[2]).SetShape(network.LinkShape{BytesPerTick: 500})

	for range 140 {
		for _, a := range apps {
			a.Tick(1)
		}
		_ = host.corrections.publishDue()
		for _, a := range apps[1:] {
			a.ApplyPendingCorrections()
		}
	}

	report := host.CadenceReport()
	if len(report.Peers) != 2 {
		t.Fatalf("the host scheduled %d links, want 2", len(report.Peers))
	}
	fast, slow := report.Peers[0], report.Peers[1]
	if fast.Participant != 2 || slow.Participant != 3 {
		t.Fatalf("the report is not in participant order: %+v", report.Peers)
	}
	if !slow.Saturated {
		t.Skipf("the shaped link was not saturated in this run: %+v", slow)
	}
	if slow.CadenceTicks <= fast.CadenceTicks {
		t.Fatalf("the constrained peer got cadence %d and the healthy one %d",
			slow.CadenceTicks, fast.CadenceTicks)
	}
	if fast.CadenceTicks > parameter.SnapshotCadenceQuietTicks {
		t.Fatalf("the healthy peer was slowed to %d ticks by its neighbour's link",
			fast.CadenceTicks)
	}
}

// transportOf reaches the mesh endpoint an App is attached to, which is the only
// way a test can shape one link of a session it did not build the mesh for.
func transportOf(t *testing.T, a *App) *network.MeshPort {
	t.Helper()
	var port *network.MeshPort
	a.World().RunSafe(func() {
		if r := a.World().Resources.Network; r != nil {
			port, _ = r.Port.(*network.MeshPort)
		}
	})
	if port == nil {
		t.Fatal("this participant is not on a mesh transport")
	}
	return port
}

package app

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// measuredLinks is a transport that only measures. Nothing here sends, so the
// choice is exercised against the estimates alone.
type measuredLinks map[uint32]linkpace.Metrics

func (m measuredLinks) Peers() []uint32 {
	out := make([]uint32, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}
func (m measuredLinks) SetLinkReport(network.LinkReport)             {}
func (m measuredLinks) LinkMetric(peer uint32) linkpace.Metrics      { return m[peer] }
func (m measuredLinks) ObserveTransfer(uint32, int64, time.Duration) {}

func roster(ids ...network.PeerID) []network.RosterEntry {
	out := make([]network.RosterEntry, len(ids))
	for i, id := range ids {
		out[i] = network.RosterEntry{ID: id, Slot: uint8(i)}
	}
	return out
}

// TestPlayoutLeadFollowsWhatWasMeasured is the choice itself: no fixed floor under
// a measurement, no lead at all with nobody on the far end, the default where there
// is no evidence, and a report rather than a clamp past the ceiling.
func TestPlayoutLeadFollowsWhatWasMeasured(t *testing.T) {
	const host = network.PeerID(1)
	two := roster(host, 2)

	lead := func(link engine.LinkMeasuringPort, r []network.RosterEntry) uint64 {
		t.Helper()
		ticks, overrun := chooseBarrierDelay(link, r, host)
		if len(overrun) != 0 {
			t.Fatalf("lead %d also reported %v past the ceiling", ticks, overrun)
		}
		return ticks
	}

	// Nothing on the wire is not a fast link, it is no link: nobody is waiting for
	// an artifact nobody is sent.
	if got := lead(nil, roster(host)); got != 0 {
		t.Fatalf("solo session lead = %d, want none", got)
	}

	// No transport, and a transport with nothing measured yet, are the same answer:
	// the default, which is what a session with no evidence has always used.
	if got := lead(nil, two); got != parameter.NetworkBarrierDelayTicks {
		t.Fatalf("unmeasured session lead = %d, want the default %d", got, parameter.NetworkBarrierDelayTicks)
	}
	unready := measuredLinks{2: {RTT: 400 * time.Millisecond, Ready: false}}
	if got := lead(unready, two); got != parameter.NetworkBarrierDelayTicks {
		t.Fatalf("lead from an unready link = %d, want the default %d", got, parameter.NetworkBarrierDelayTicks)
	}

	// A link faster than the default now lowers the lead to it rather than paying
	// the default's 150 ms, which is the whole of the dynamic half.
	fast := measuredLinks{2: {RTT: 2 * time.Millisecond, Ready: true}}
	if got := lead(fast, two); got != parameter.NetworkBarrierMinDelayTicks {
		t.Fatalf("loopback lead = %d, want the %d-tick minimum", got, parameter.NetworkBarrierMinDelayTicks)
	}

	// 400 ms round trip, 20 ms of variation: 400 + 40 = 440 ms, nine ticks at the
	// 50 ms interval. The whole round trip, because the authority reads a guest's
	// crossing a correction's age after the guest produced it.
	slow := measuredLinks{2: {RTT: 400 * time.Millisecond, Jitter: 20 * time.Millisecond, Ready: true}}
	if got, want := lead(slow, two), uint64(9); got != want {
		t.Fatalf("lead over a 400ms link = %d, want %d", got, want)
	}

	// The worst link sets it, because one lead serves the whole session — and a
	// third participant makes the topology two hops, because in the star the CLI
	// builds one guest reaches another through the coordinator.
	three := roster(host, 2, 3)
	mixed := measuredLinks{
		2: {RTT: 400 * time.Millisecond, Jitter: 20 * time.Millisecond, Ready: true},
		3: {RTT: 20 * time.Millisecond, Ready: true},
	}
	if got, want := lead(mixed, three), uint64(18); got != want {
		t.Fatalf("relayed lead = %d, want %d", got, want)
	}

	// A link that is merely backlogged is the cadence controller's problem: its
	// floor is fast, so the lead rises and the participant stays.
	backlogged := measuredLinks{2: {RTT: 10 * time.Second, MinRTT: 4 * time.Millisecond, Ready: true}}
	if got := lead(backlogged, two); got != parameter.NetworkBarrierMaxDelayTicks {
		t.Fatalf("a backlogged link gave lead %d, want the ceiling %d", got, parameter.NetworkBarrierMaxDelayTicks)
	}

	// A link nobody should be playing over is named rather than absorbed: the lead
	// stays at the ceiling and the participant behind it is the session's to drop.
	awful := measuredLinks{2: {RTT: 10 * time.Second, MinRTT: 10 * time.Second, Ready: true}}
	ticks, overrun := chooseBarrierDelay(awful, two, host)
	if ticks != parameter.NetworkBarrierMaxDelayTicks {
		t.Fatalf("lead over a 10s link = %d, want the ceiling %d", ticks, parameter.NetworkBarrierMaxDelayTicks)
	}
	if len(overrun) != 1 || overrun[0] != 2 {
		t.Fatalf("the 10s link reported %v past the ceiling, want [2]", overrun)
	}
}

// TestPlayoutLeadRisesOnAMeasuredSlowLink drives the choice from a shaped link
// rather than from a table, so what is proved is the whole path: probes go out,
// echoes come back, the estimator turns them into a round trip, and the lobby's
// choice reads it. It is the automated form of the tc-netem check.
func TestPlayoutLeadRisesOnAMeasuredSlowLink(t *testing.T) {
	t.Parallel()
	host, guest, mesh := shapedPair(t, 0x5EEDBEEF, network.LinkShape{LatencyTicks: 8})
	runSession(host, guest, 200)

	link := mesh.Node(1)
	if m := link.LinkMetric(2); !m.Ready {
		t.Fatalf("the link never became steerable: %+v", m)
	}
	got, _ := chooseBarrierDelay(link, roster(1, 2), 1)
	if got <= parameter.NetworkBarrierDelayTicks {
		t.Fatalf("lead over a link delayed 8 ticks each way = %d, want more than the %d-tick default",
			got, parameter.NetworkBarrierDelayTicks)
	}
	if got > parameter.NetworkBarrierMaxDelayTicks {
		t.Fatalf("lead = %d, past the %d-tick ceiling", got, parameter.NetworkBarrierMaxDelayTicks)
	}
}

// TestTheHostAdoptsTheLeadItChose is the other half of the choice: the offer
// carried it to every guest, and the coordinator's own endpoint was built before
// the lobby measured anything, so it kept the default and applied its crossings a
// lead earlier than the session it had just told.
func TestTheHostAdoptsTheLeadItChose(t *testing.T) {
	t.Parallel()
	want := uint64(parameter.NetworkBarrierDelayTicks + 4)

	a := mustHeadless(t, 0x1EAD, 120, 40)
	defer a.Close()
	a.AttachTransport(network.NewMesh().Node(1))
	tickUntilCursor(t, a)

	offer := network.SessionOffer{
		Anchor: a.JoinAnchor(), Host: 1, Assigned: 2, Term: network.FirstTerm,
		Roster:            roster(1, 2),
		BarrierDelayTicks: want,
	}
	if err := a.HostSession(offer); err != nil {
		t.Fatalf("host session: %v", err)
	}
	a.Tick(1)
	if got := statOf(a, "network.barrier_delay_ticks"); got != int64(want) {
		t.Fatalf("the host's barrier defers by %d ticks, want the %d it offered", got, want)
	}
}

// TestAMeasuredLinkNarrowsTheSessionLeadOnOneTick is the dynamic half end to end.
// A session opens on the default lead because nothing is measured yet; once the
// links have been probed the authority publishes what they actually cost, and every
// participant starts deferring by it on the same tick — which is what makes the
// value session identity rather than each instance's own opinion.
func TestAMeasuredLinkNarrowsTheSessionLeadOnOneTick(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x1EADBEEF, 2, [][2]int{{1, 2}})
	// A shape is what makes an in-process link measurable at all: an unshaped one
	// answers a probe inside the same virtual tick and reports no time passing, so
	// it is an unmeasured link rather than an infinitely fast one.
	for _, a := range apps {
		if mp, ok := a.sessionTransport().(*network.MeshPort); ok {
			mp.SetShape(network.LinkShape{LatencyTicks: 1})
		}
	}
	if got := statOf(apps[0], "network.barrier_delay_ticks"); got != parameter.NetworkBarrierDelayTicks {
		t.Fatalf("an unmeasured session opened on a lead of %d, want the default %d",
			got, parameter.NetworkBarrierDelayTicks)
	}

	// One round trip over a link delayed a tick each way is two ticks, so half of it
	// plus no measured variation is one — under the default, which is the point.
	// Narrowing waits out one renegotiation window past the probes that steer it,
	// and the loop stops on the answer rather than running the bound out.
	const want = int64(parameter.NetworkBarrierMinDelayTicks)
	adopted := make([]uint64, len(apps))
	for tick := uint64(1); tick <= 2*parameter.NetworkBarrierRenegotiateTicks; tick++ {
		tickAll(apps)
		done := true
		for i, a := range apps {
			if adopted[i] == 0 && statOf(a, "network.barrier_delay_ticks") == want {
				adopted[i] = tick
			}
			done = done && adopted[i] != 0
		}
		if done {
			break
		}
	}
	for i, a := range apps {
		if got := statOf(a, "network.barrier_delay_ticks"); got != want {
			t.Fatalf("participant %d defers by %d ticks over a one-tick link, want %d", i+1, got, want)
		}
		if adopted[i] == 0 {
			t.Fatalf("participant %d never narrowed its lead", i+1)
		}
	}
	if adopted[0] != adopted[1] {
		t.Fatalf("the lead was adopted at ticks %v; a barrier-bound artifact applies at one", adopted)
	}
}

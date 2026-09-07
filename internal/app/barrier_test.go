package app

import (
	"testing"
	"time"

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

func roster(ids ...network.PeerID) []network.SessionParticipant {
	out := make([]network.SessionParticipant, len(ids))
	for i, id := range ids {
		out[i] = network.SessionParticipant{ID: id, Slot: uint8(i)}
	}
	return out
}

// TestPlayoutLeadIsChosenFromWhatWasMeasured is gap 2: the value travelled from
// the offer to the barrier all along and nothing ever put a measurement in it.
func TestPlayoutLeadIsChosenFromWhatWasMeasured(t *testing.T) {
	const host = network.PeerID(1)
	two := roster(host, 2)

	// No transport, and a transport with nothing measured yet, are the same answer:
	// the constant, which is what every session got before this existed.
	if got := chooseBarrierDelay(nil, two, host); got != parameter.NetworkBarrierDelayTicks {
		t.Fatalf("unmeasured session lead = %d, want the default %d", got, parameter.NetworkBarrierDelayTicks)
	}
	unready := measuredLinks{2: {RTT: 400 * time.Millisecond, Ready: false}}
	if got := chooseBarrierDelay(unready, two, host); got != parameter.NetworkBarrierDelayTicks {
		t.Fatalf("lead from an unready link = %d, want the default %d", got, parameter.NetworkBarrierDelayTicks)
	}

	// A link faster than the floor does not lower it: the floor is also the default.
	fast := measuredLinks{2: {RTT: 2 * time.Millisecond, Ready: true}}
	if got := chooseBarrierDelay(fast, two, host); got != parameter.NetworkBarrierDelayTicks {
		t.Fatalf("loopback lead = %d, want the floor %d", got, parameter.NetworkBarrierDelayTicks)
	}

	// 200 ms round trip, 20 ms of variation: 100 + 40 = 140 ms, three ticks at the
	// 50 ms interval — which the floor happens to equal, so push it further out.
	slow := measuredLinks{2: {RTT: 400 * time.Millisecond, Jitter: 20 * time.Millisecond, Ready: true}}
	if got, want := chooseBarrierDelay(slow, two, host), uint64(5); got != want {
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
	if got, want := chooseBarrierDelay(mixed, three, host), uint64(10); got != want {
		t.Fatalf("relayed lead = %d, want %d", got, want)
	}

	// And a link nobody should be playing over does not turn the lead into latency
	// the player feels on every remote actor.
	awful := measuredLinks{2: {RTT: 10 * time.Second, Ready: true}}
	if got := chooseBarrierDelay(awful, two, host); got != parameter.NetworkBarrierMaxDelayTicks {
		t.Fatalf("lead over a 10s link = %d, want the ceiling %d", got, parameter.NetworkBarrierMaxDelayTicks)
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
	got := chooseBarrierDelay(link, roster(1, 2), 1)
	if got <= parameter.NetworkBarrierDelayTicks {
		t.Fatalf("lead over a link delayed 8 ticks each way = %d, want more than the %d-tick floor",
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
		Participants:      roster(1, 2),
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

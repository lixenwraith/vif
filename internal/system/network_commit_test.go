package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// linkedPort is a transport that sends nothing and names its links and their
// measurements, which is all the commit and the lead read from one.
type linkedPort struct {
	peers []uint32
	rtt   time.Duration
}

func (p *linkedPort) Send(uint32, uint8, []byte) bool              { return true }
func (p *linkedPort) Broadcast(uint8, []byte)                      {}
func (p *linkedPort) BroadcastExcept(uint32, uint8, []byte)        {}
func (p *linkedPort) PeerCount() int                               { return len(p.peers) }
func (p *linkedPort) IsRunning() bool                              { return true }
func (p *linkedPort) Drain([]network.Inbound) int                  { return 0 }
func (p *linkedPort) Peers() []uint32                              { return p.peers }
func (p *linkedPort) SetLinkReport(network.LinkReport)             {}
func (p *linkedPort) ObserveTransfer(uint32, int64, time.Duration) {}
func (p *linkedPort) LinkMetric(uint32) linkpace.Metrics {
	return linkpace.Metrics{RTT: p.rtt, MinRTT: p.rtt, Ready: p.rtt > 0}
}

// sessionSystem is participant local in a session participant 1 authors, at tick.
func sessionSystem(t *testing.T, local uint32, tick uint64, port *linkedPort) *NetworkSystem {
	t.Helper()
	s := boundsSystem(t)
	r := engine.NewNetworkResource(port)
	r.ParticipantID = local
	s.world.Resources.Network = r
	s.world.Resources.Event.Queue.RebaseStamp(0, tick)
	s.Init()
	return s
}

// rawBatch is one guest's epoch as it sends it to the authority.
func rawBatch(t *testing.T, source uint32, produced uint64, applyTicks ...uint64) []byte {
	t.Helper()
	frames := make([]event.ScheduledWireFrame, len(applyTicks))
	for i, at := range applyTicks {
		frames[i] = event.ScheduledWireFrame{ApplyTick: at, Frame: event.WireFrame{
			Event: "EventGoldJumpRequest", Domain: "shared", Seq: uint64(i + 1),
		}}
	}
	body, err := event.EncodeWireBatch(event.WireBatch{Source: source, ProducedTick: produced, Frames: frames})
	if err != nil {
		t.Fatalf("encode batch: %v", err)
	}
	return body
}

// TestTheAuthorityChoosesTheTickALateCrossingApplies: on time as stamped, late at
// the authority's next tick, and past the commit bound void — it applies nowhere
// and only closes its source's fence.
func TestTheAuthorityChoosesTheTickALateCrossingApplies(t *testing.T) {
	t.Parallel()
	s := sessionSystem(t, 1, 100, &linkedPort{peers: []uint32{2}})
	stale := 100 - uint64(parameter.NetworkCommitLateTicks) - 1
	s.scheduleCrossings(2, rawBatch(t, 2, 95, 101, 99, stale))

	s.mu.Lock()
	got := append([]barrierArtifact(nil), s.scheduled...)
	s.mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("scheduled %d of the guest's crossings, want 3", len(got))
	}
	for i, a := range got {
		if a.applyTick != 101 {
			t.Fatalf("crossing %d applies at %d, want the authority's next tick 101", i, a.applyTick)
		}
		if a.void != (i == 2) {
			t.Fatalf("crossing %d void = %t", i, a.void)
		}
	}
	// Eviction weighs epochs, so the one epoch carrying both late crossings is one
	if late := s.world.Resources.Network.CommitLate[2].Load(); late != 1 {
		t.Fatalf("the authority counted %d late epochs from participant 2, want 1", late)
	}
	s.applyDue(101)
	if fence := s.AppliedCrossingFences().Seq(2); fence != 3 {
		t.Fatalf("participant 2's fence is %d after the void crossing, want 3", fence)
	}
}

// TestARawEpochOnAnotherLinkIsRefused: a producer with a link of its own is heard
// on it, so its identity arriving on another link is claimed, not relayed.
func TestARawEpochOnAnotherLinkIsRefused(t *testing.T) {
	t.Parallel()
	s := sessionSystem(t, 1, 100, &linkedPort{peers: []uint32{2, 3}})
	s.scheduleCrossings(3, rawBatch(t, 2, 95, 105))
	if got := scheduledCount(s); got != 0 {
		t.Fatalf("a claimed identity scheduled %d crossings", got)
	}
	if s.statForged.Load() == 0 {
		t.Fatal("the claimed identity was not counted")
	}
}

// TestAGuestAppliesAPeerOnlyAsCommitted: another guest's raw epoch is passed on,
// never applied; the authority's committed copy is.
func TestAGuestAppliesAPeerOnlyAsCommitted(t *testing.T) {
	t.Parallel()
	s := sessionSystem(t, 2, 100, &linkedPort{peers: []uint32{1}})
	s.scheduleCrossings(1, rawBatch(t, 3, 95, 105))
	if got := scheduledCount(s); got != 0 {
		t.Fatalf("a raw peer epoch scheduled %d crossings", got)
	}
	s.scheduleCrossings(1, boundsBatch(t, 3, 95, 105, 1, 0))
	if got := scheduledCount(s); got != 1 {
		t.Fatalf("the committed copy scheduled %d crossings, want 1", got)
	}
}

// TestAGuestPacesIntoItsBand: inside the band it rests, a little late or early it
// trims the tick interval toward it, and far outside it steps.
func TestAGuestPacesIntoItsBand(t *testing.T) {
	t.Parallel()
	ahead := int64(parameter.NetworkAheadTicks)
	for _, c := range []struct {
		latest   int64
		slower   bool
		faster   bool
		stepping bool
	}{
		{latest: ahead},
		{latest: ahead - parameter.NetworkPaceBandTicks},
		{latest: ahead + 1, slower: true},
		{latest: ahead + parameter.NetworkPaceStepTicks, slower: true, stepping: true},
		{latest: ahead - parameter.NetworkPaceBandTicks - 1, faster: true},
		{latest: ahead - parameter.NetworkPaceBandTicks - parameter.NetworkPaceStepTicks, faster: true, stepping: true},
	} {
		trim, step := paceDecision(c.latest)
		slower, faster := trim > 0 || step > 0, trim < 0 || step < 0
		if slower != c.slower || faster != c.faster || (step != 0) != c.stepping {
			t.Errorf("latest %d: trim %d step %d", c.latest, trim, step)
		}
	}
}

// TestAGuestStampsWithItsOwnRoundTrip: the lead is this guest's round trip to the
// authority plus the relay tick, less how late the authority's epochs land; the
// authority's own is a tick, and an unmeasured link keeps the default.
func TestAGuestStampsWithItsOwnRoundTrip(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{1}}
	guest := sessionSystem(t, 2, 100, port)
	if got, _ := guest.ownLead(port); got != parameter.NetworkBarrierDelayTicks {
		t.Fatalf("unmeasured lead %d, want the default %d", got, parameter.NetworkBarrierDelayTicks)
	}
	port.rtt = 150 * time.Millisecond
	guest.paceLatest, guest.paceKnown = -1, true
	want := uint64(3 + parameter.NetworkRelaySlackTicks + 1)
	if got, _ := guest.ownLead(port); got != want {
		t.Fatalf("lead over a 150 ms round trip landing a tick early = %d, want %d", got, want)
	}
	authority := sessionSystem(t, 1, 100, port)
	if got, _ := authority.ownLead(port); got != parameter.NetworkBarrierMinDelayTicks {
		t.Fatalf("the authority's own lead is %d, want %d", got, parameter.NetworkBarrierMinDelayTicks)
	}
}

// TestAHandoffDropsWhatNoAuthorityCommitted: a guest's own crossings still waiting
// to apply, and those applied but unproved, go with the authority they were sent
// to, and the fence closes over the unapplied ones.
func TestAHandoffDropsWhatNoAuthorityCommitted(t *testing.T) {
	t.Parallel()
	s := sessionSystem(t, 2, 100, &linkedPort{peers: []uint32{1}})
	s.mu.Lock()
	s.scheduled = append(s.scheduled, barrierArtifact{
		frame: event.WireFrame{Event: "EventGoldJumpRequest", Seq: 1}, applyTick: 104, source: 2,
	})
	s.suffix = append(s.suffix, localCrossing{frame: event.ScheduledWireFrame{Frame: event.WireFrame{Seq: 1}}})
	s.mu.Unlock()

	s.DropUncommitted()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.scheduled) != 0 || len(s.suffix) != 0 {
		t.Fatalf("kept %d scheduled and %d retained own crossings", len(s.scheduled), len(s.suffix))
	}
	if s.appliedCrossSeq != 1 {
		t.Fatalf("the local fence stands at %d, want it closed over the dropped crossing", s.appliedCrossSeq)
	}
}

// stateBatch is one owner's cursor-state sync as the owner or the authority sends it.
func stateBatch(t *testing.T, source uint32, seq, applyTick uint64, committed bool) []byte {
	t.Helper()
	frame, encErr := event.NewWireFrame(event.GameEvent{
		Type: event.EventCursorStateSync, Domain: core.DomainShared,
		Payload: &event.CursorStatePayload{Entity: 7, Slot: 1, Seq: seq},
	})
	if encErr != "" {
		t.Fatal(encErr)
	}
	body, err := event.EncodeWireBatch(event.WireBatch{
		Source: source, Committed: committed,
		Frames: []event.ScheduledWireFrame{{Frame: frame, ApplyTick: applyTick}},
	})
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}
	return body
}

// TestAnOwnerSyncIsWrittenAtTheCommittedTick: the authority commits a guest's raw
// sync to its next tick, and another guest writes one only as committed, at that
// tick, so a late owner's value lands on one tick everywhere.
func TestAnOwnerSyncIsWrittenAtTheCommittedTick(t *testing.T) {
	t.Parallel()
	authority := sessionSystem(t, 1, 100, &linkedPort{peers: []uint32{2}})
	authority.scheduleCursorState(2, stateBatch(t, 2, 1, 0, false))
	if len(authority.states) != 1 || authority.states[0].applyTick != 101 {
		t.Fatalf("the authority scheduled %+v, want one sync at its next tick 101", authority.states)
	}

	guest := sessionSystem(t, 3, 100, &linkedPort{peers: []uint32{1}})
	guest.scheduleCursorState(1, stateBatch(t, 2, 1, 0, false))
	if len(guest.states) != 0 {
		t.Fatalf("a guest scheduled a raw sync: %+v", guest.states)
	}
	guest.scheduleCursorState(1, stateBatch(t, 2, 1, 104, true))
	if len(guest.states) != 1 || guest.states[0].applyTick != 104 {
		t.Fatalf("the guest scheduled %+v, want the committed sync at 104", guest.states)
	}
}

// TestADepartedIdentityLeavesNoFence: an identity is handed out again and its next
// holder's sequence starts at one, so nothing of the departed holder may keep its
// fence up — including a crossing it had committed for a tick after its departure,
// and a departure that finds its cursor already gone.
func TestADepartedIdentityLeavesNoFence(t *testing.T) {
	t.Parallel()
	event.EnsureRegistry()
	s := sessionSystem(t, 1, 100, &linkedPort{peers: []uint32{2}})
	s.scheduleCrossings(2, rawBatch(t, 2, 99, 101, 108))
	s.applyDue(101)
	if got := s.AppliedCrossingFences().Seq(2); got != 1 {
		t.Fatalf("fence for participant 2 = %d after its first crossing, want 1", got)
	}

	s.removeParticipant(&event.ParticipantDepartedPayload{Participant: 2, Slot: 5})
	s.applyDue(108)
	if got := s.AppliedCrossingFences().Seq(2); got != 0 {
		t.Fatalf("fence for participant 2 = %d after it departed, want none", got)
	}
}

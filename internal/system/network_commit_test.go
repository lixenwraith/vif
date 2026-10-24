package system

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/linkpace"
)

// linkedPort names its links and their measurements, which is all the commit and
// the lead read from one, and keeps what it floods and sends.
type linkedPort struct {
	peers   []uint32
	rtt     time.Duration
	flooded [][]byte
	sent    []sentFrame
}

// sentFrame is one frame a linkedPort was asked to send; to is zero for everyone.
type sentFrame struct {
	to   uint32
	kind network.MessageType
	body []byte
}

func (p *linkedPort) Send(to uint32, kind uint8, b []byte) bool {
	p.sent = append(p.sent, sentFrame{to, network.MessageType(kind), b})
	return true
}
func (p *linkedPort) Broadcast(kind uint8, b []byte) {
	p.sent = append(p.sent, sentFrame{0, network.MessageType(kind), b})
}
func (p *linkedPort) BroadcastExcept(_ uint32, _ uint8, b []byte) {
	p.flooded = append(p.flooded, b)
}
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
	s.world.RunSafe(s.world.LatchSession)
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

// TestTheBarrierHoldsAnInputToTheTicksItCouldBeMadeAt: no receiver can check the
// tick a peer stamps its press with, so the tick it applies at bounds it: before that
// tick, and no earlier than the longest lead and the latest commit allow.
func TestTheBarrierHoldsAnInputToTheTicksItCouldBeMadeAt(t *testing.T) {
	t.Parallel()
	s := sessionSystem(t, 1, 100, &linkedPort{peers: []uint32{2}})
	claims := []uint64{0, 90, 500}
	frames := make([]event.ScheduledWireFrame, len(claims))
	for i, tick := range claims {
		f, msg := event.NewWireFrame(event.GameEvent{Type: event.EventInputPressed, Domain: core.DomainPlayer,
			Payload: &event.InputPayload{Key: "1", Tick: tick}})
		if msg != "" {
			t.Fatalf("encode press: %s", msg)
		}
		f.Seq = uint64(i + 1)
		frames[i] = event.ScheduledWireFrame{Frame: f, ApplyTick: 101}
	}
	body, err := event.EncodeWireBatch(event.WireBatch{Source: 2, ProducedTick: 99, Frames: frames})
	if err != nil {
		t.Fatalf("encode batch: %v", err)
	}
	s.scheduleCrossings(2, body)
	s.applyDue(101)

	var got []uint64
	for _, ev := range s.world.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.InputPayload); ok {
			got = append(got, p.Tick)
		}
	}
	if want := []uint64{100 - parameter.NetworkApplyHorizonTicks, 90, 100}; !slices.Equal(got, want) {
		t.Fatalf("presses applied at 101 claiming ticks %v carry %v, want %v", claims, got, want)
	}
}

// TestACommittedCopyRestartsItsHopCount: the authority originates the copy it
// commits, so a raw path that spent most of the hop limit cannot strand it on the
// way back to the relays behind it.
func TestACommittedCopyRestartsItsHopCount(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{2}}
	s := sessionSystem(t, 1, 100, port)
	body, err := event.EncodeWireBatch(event.WireBatch{
		Source: 3, ProducedTick: 100, Hops: parameter.NetworkRelayHopLimit - 1,
		Frames: []event.ScheduledWireFrame{{ApplyTick: 104, Frame: event.WireFrame{
			Event: "EventGoldJumpRequest", Domain: "shared", Seq: 1,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.scheduleCrossings(2, body)
	if len(port.sent) != 1 {
		t.Fatalf("the authority sent %d copies, want the committed one", len(port.sent))
	}
	got, err := event.DecodeWireBatch(port.sent[0].body)
	if err != nil || !got.Committed || got.Hops != 1 {
		t.Fatalf("committed copy leaves with %d hops (committed %t, err %v), want 1", got.Hops, got.Committed, err)
	}
}

// TestARawEpochOnAnotherLinkIsRefused: a producer with a link of its own is heard
// on it, and a bot behind its holder on the holder's, so either identity arriving
// on another link is claimed, not relayed.
func TestARawEpochOnAnotherLinkIsRefused(t *testing.T) {
	t.Parallel()
	s := sessionSystem(t, 1, 100, &linkedPort{peers: []uint32{2, 3}})
	bot := s.world.CreateEntity(core.DomainShared)
	s.world.Components.Cursor.SetComponent(bot, component.CursorComponent{PeerID: 4, Holder: 2})
	s.scheduleCrossings(3, rawBatch(t, 2, 95, 105))
	s.scheduleCrossings(3, rawBatch(t, 4, 95, 105))
	if got := scheduledCount(s); got != 0 {
		t.Fatalf("claimed identities scheduled %d crossings", got)
	}
	if got := s.statForged.Load(); got != 2 {
		t.Fatalf("%d claims counted, want 2", got)
	}
	s.scheduleCrossings(2, rawBatch(t, 4, 95, 105))
	if got := scheduledCount(s); got != 1 {
		t.Fatalf("the bot's epoch on its holder's link scheduled %d crossings, want 1", got)
	}
}

// TestAGuestSendsTheAuthorityWhatItCarriesThenItsOwn: a guest linked to the
// authority sends it alone the raw epochs it carries for the participants behind
// it, as they arrive, and its own when its tick closes; the link bundles them.
func TestAGuestSendsTheAuthorityWhatItCarriesThenItsOwn(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{1, 3}}
	s := sessionSystem(t, 2, 100, port)
	s.Carry(3)
	s.scheduleCrossings(3, rawBatch(t, 3, 100, 104))
	s.Flush(100)
	if got := sentTo(port); !slices.Equal(got, []uint32{1, 1}) || sourcesOf(port) == nil {
		t.Fatalf("the guest sent %+v, want two epochs to the authority", port.sent)
	}
	if got := sourcesOf(port); !slices.Equal(got, []uint32{3, 2}) {
		t.Fatalf("the authority's link carries epochs of %v, want the leaf's then the guest's", got)
	}
}

// sourcesOf is the source of each epoch a linkedPort was asked to send, in order.
func sourcesOf(port *linkedPort) []uint32 {
	var out []uint32
	for _, f := range port.sent {
		if b, err := event.DecodeWireBatch(f.body); err == nil && f.kind == network.MsgEvent {
			out = append(out, b.Source)
		}
	}
	return out
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
// trims the tick interval toward it, and far outside it steps; a step forward
// waits for the whole window, since one jittered arrival would freeze the world.
func TestAGuestPacesIntoItsBand(t *testing.T) {
	t.Parallel()
	ahead, far := int64(parameter.NetworkAheadTicks), int64(parameter.NetworkPaceStepTicks)
	for _, c := range []struct {
		latest, earliest int64
		slower, faster   bool
		stepping         bool
	}{
		{latest: ahead, earliest: ahead},
		{latest: ahead - parameter.NetworkPaceBandTicks, earliest: ahead - parameter.NetworkPaceBandTicks},
		{latest: ahead + 1, earliest: ahead, slower: true},
		{latest: ahead + far, earliest: ahead, slower: true},
		{latest: ahead + far, earliest: ahead + far, slower: true, stepping: true},
		{latest: ahead - parameter.NetworkPaceBandTicks - 1, earliest: ahead - parameter.NetworkPaceBandTicks - 1, faster: true},
		{latest: ahead - parameter.NetworkPaceBandTicks - far, earliest: ahead - parameter.NetworkPaceBandTicks - far, faster: true, stepping: true},
	} {
		trim, step := paceDecision(c.latest, c.earliest)
		slower, faster := trim > 0 || step > 0, trim < 0 || step < 0
		if slower != c.slower || faster != c.faster || (step != 0) != c.stepping {
			t.Errorf("latest %d earliest %d: trim %d step %d", c.latest, c.earliest, trim, step)
		}
	}
}

// TestAGuestStepsOnceForOneLateness: epochs read while a step is still being
// served measure the lateness it already answers, so they order no second step;
// once it is served the next window decides again.
func TestAGuestStepsOnceForOneLateness(t *testing.T) {
	t.Parallel()
	guest := sessionSystem(t, 2, 100, &linkedPort{peers: []uint32{1}})
	r := guest.world.Resources.Network
	window := func(from uint64) {
		for i := range uint64(parameter.NetworkPaceWindow) {
			guest.observeAuthorityEpoch(from + i)
		}
	}
	window(60)
	first := r.PaceStep.Load()
	if first < parameter.NetworkPaceStepTicks {
		t.Fatalf("a window 40 ticks late stepped %d", first)
	}
	window(70) // arrived while the guest waits the step out
	if got := r.PaceStep.Load(); got != first {
		t.Fatalf("the step owed grew to %d while %d was being served", got, first)
	}
	guest.world.Resources.Event.Queue.RebaseStamp(0, 100+parameter.NetworkPaceServedTicks)
	window(60)
	if got := r.PaceStep.Load(); got == first {
		t.Fatal("a late window after the step was served ordered none")
	}
}

// TestALeadFallsAsItsHigherReadingsAgeOut: a higher reading is adopted at once and
// holds for the window; the lead then falls to the highest reading still in it
// rather than a tick at a time, since the window slides instead of restarting.
func TestALeadFallsAsItsHigherReadingsAgeOut(t *testing.T) {
	t.Parallel()
	s := boundsSystem(t)
	window := parameter.NetworkBarrierRenegotiateTicks
	read := func(n int, target, want uint64) {
		t.Helper()
		for range n {
			if got := s.leadOf(target); got != want {
				t.Fatalf("reading %d after %d readings: lead %d, want %d", target, s.leadCount, got, want)
			}
		}
	}
	read(1, 13, 13)        // adopted at once
	read(10, 9, 13)        // a lower reading waits
	read(window-11, 6, 13) // the 13 is the oldest reading still in the window
	read(10, 6, 9)         // it aged out; the 9s are the highest left
	read(1, 6, 6)          // and then they are
}

// TestAGuestStampsWithTheAgeItsEpochsCommitAt: a reading is the age the authority
// committed this guest's newest epoch at plus the relay tick, taken off the
// committed copy on whichever link returns it; the authority's own is a tick, and
// a guest no copy has reached keeps the default.
func TestAGuestStampsWithTheAgeItsEpochsCommitAt(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{2}}
	guest := sessionSystem(t, 3, 100, port)
	if got := guest.ownLead(); got != parameter.NetworkBarrierDelayTicks {
		t.Fatalf("lead before any copy returned = %d, want the default %d", got, parameter.NetworkBarrierDelayTicks)
	}
	body, err := event.EncodeWireBatch(event.WireBatch{Source: 3, ProducedTick: 88, Committed: true, Age: 4})
	if err != nil {
		t.Fatal(err)
	}
	guest.scheduleCrossings(2, body)
	if got, want := guest.ownLead(), uint64(4+parameter.NetworkRelaySlackTicks); got != want {
		t.Fatalf("lead with the newest epoch committed 4 ticks old = %d, want %d", got, want)
	}
	authority := sessionSystem(t, 1, 100, port)
	if got := authority.ownLead(); got != parameter.NetworkBarrierMinDelayTicks {
		t.Fatalf("the authority's own lead is %d, want %d", got, parameter.NetworkBarrierMinDelayTicks)
	}
}

// seqBatch is one guest's epoch carrying the given sequences, all for one tick.
func seqBatch(t *testing.T, source uint32, produced, applyTick uint64, seqs ...uint64) []byte {
	t.Helper()
	frames := make([]event.ScheduledWireFrame, len(seqs))
	for i, seq := range seqs {
		frames[i] = event.ScheduledWireFrame{ApplyTick: applyTick, Frame: event.WireFrame{
			Event: "EventGoldJumpRequest", Domain: "shared", Seq: seq,
		}}
	}
	body, err := event.EncodeWireBatch(event.WireBatch{Source: source, ProducedTick: produced, Frames: frames})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// sentSeqs lists the sequences of the epochs a linkedPort was asked to send, in order.
func sentSeqs(t *testing.T, port *linkedPort) []uint64 {
	t.Helper()
	var seqs []uint64
	for _, f := range port.sent {
		b, err := event.DecodeWireBatch(f.body)
		if err != nil || f.kind != network.MsgEvent {
			t.Fatalf("the guest sent %#x (%v), want epochs", f.kind, err)
		}
		for _, f := range b.Frames {
			seqs = append(seqs, f.Frame.Seq)
		}
	}
	return seqs
}

// TestAHandoffResendsWhatNoAuthorityCommitted: with the authority lost a guest's
// own epochs wait, and the handoff sends the successor the crossings the lost
// authority never returned committed, retained and scheduled as they were.
func TestAHandoffResendsWhatNoAuthorityCommitted(t *testing.T) {
	t.Parallel()
	event.EnsureRegistry()
	port := &linkedPort{peers: []uint32{1, 3}}
	guest := sessionSystem(t, 2, 99, port)
	guest.Flush(99)
	cross := func(tick uint64) {
		guest.world.Resources.Event.Queue.RebaseStamp(0, tick)
		if !guest.Cross(event.GameEvent{Type: event.EventGoldJumpRequest, Domain: core.DomainPlayer}) {
			t.Fatal("the barrier did not take an own crossing")
		}
		guest.Flush(tick)
	}
	cross(100)
	cross(101)
	body, err := event.EncodeWireBatch(event.WireBatch{Source: 2, ProducedTick: 100, Committed: true})
	if err != nil {
		t.Fatal(err)
	}
	guest.scheduleCrossings(1, body)

	// The authority goes with epoch 101 in flight; the successor link is all that
	// is left, and nothing is offered on it until the handoff names the successor.
	port.peers, port.sent = []uint32{3}, nil
	guest.statHostLost.Store(true)
	cross(102)
	if len(port.sent) != 0 {
		t.Fatalf("a guest whose authority is lost sent %v", sentTo(port))
	}
	guest.world.Resources.Network.Authority.Store(3)
	guest.statHostLost.Store(false)
	guest.ResendUncommitted()
	guest.world.Resources.Event.Queue.RebaseStamp(0, 103)
	guest.Flush(103)
	if got := sentTo(port); !slices.Equal(got, []uint32{3}) {
		t.Fatalf("after the handoff the guest sent %v, want the successor's link alone", got)
	}
	if got := sentSeqs(t, port); !slices.Equal(got, []uint64{2, 3}) {
		t.Fatalf("the successor was sent sequences %v, want the two the lost authority never committed", got)
	}
	guest.mu.Lock()
	defer guest.mu.Unlock()
	if len(guest.suffix) != 3 || len(guest.scheduled) != 3 {
		t.Fatalf("the handoff left %d retained and %d scheduled own crossings, want all 3 of each",
			len(guest.suffix), len(guest.scheduled))
	}
}

// TestAResentCrossingIsCommittedOnceAndAGapIsFilled: a crossing the authority
// already holds is refused by sequence whatever epoch carries it and leaves the
// committed copy, while one it never received is committed from the resend.
func TestAResentCrossingIsCommittedOnceAndAGapIsFilled(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{2}}
	authority := sessionSystem(t, 1, 100, port)
	authority.scheduleCrossings(2, seqBatch(t, 2, 99, 104, 1, 2))
	authority.scheduleCrossings(2, seqBatch(t, 2, 100, 104, 2, 3))
	authority.mu.Lock()
	var seqs []uint64
	for _, a := range authority.scheduled {
		seqs = append(seqs, a.frame.Seq)
	}
	authority.mu.Unlock()
	if !slices.Equal(seqs, []uint64{1, 2, 3}) {
		t.Fatalf("the authority scheduled sequences %v, want each once", seqs)
	}
	if len(port.sent) != 2 {
		t.Fatalf("the authority committed %d epochs, want both", len(port.sent))
	}
	copy, err := event.DecodeWireBatch(port.sent[1].body)
	if err != nil || len(copy.Frames) != 1 || copy.Frames[0].Frame.Seq != 3 {
		t.Fatalf("the resend's committed copy carries %+v (%v), want sequence 3 alone", copy.Frames, err)
	}
	if got := authority.statDuplicates.Load(); got != 1 {
		t.Fatalf("%d duplicates counted, want the repeated sequence", got)
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

// TestALeftSessionAppliesNothingButTheRunsOwn: once this instance has left its
// session, nothing another participant sent applies, barrier-bound or not, nor any
// roster change still scheduled; no source keeps a fence, a sync or a digest for
// the identity's next holder to trip on. Its own crossings still apply.
func TestALeftSessionAppliesNothingButTheRunsOwn(t *testing.T) {
	t.Parallel()
	s := sessionSystem(t, 2, 100, &linkedPort{peers: []uint32{1}})
	s.scheduleCrossings(1, boundsBatch(t, 1, 99, 101, 1, 0))
	s.scheduleCrossings(1, boundsBatch(t, 1, 100, 108, 1, 0))
	s.applyDue(101)
	if s.AppliedCrossingFences().Seq(1) == 0 {
		t.Fatal("the authority's first crossing raised no fence; the case below proves nothing")
	}
	own := barrierArtifact{frame: event.WireFrame{Event: "EventGoldJumpRequest", Domain: "shared", Seq: 1}, applyTick: 108, source: 2}
	arrival := own
	arrival.frame.Event, arrival.frame.Seq, arrival.authored = "EventParticipantJoined", 2, true
	s.mu.Lock()
	s.scheduled = append(s.scheduled, own, arrival)
	s.relay = 1
	s.mu.Unlock()
	s.states = append(s.states, pendingState{applyTick: 108, source: 1, payload: syncFor(0, 1, 1, 40)})
	s.pendingDigest[1] = stateDigest{Tick: 108, Hash: 7, Valid: true}

	s.LeaveSession()
	if got := s.AppliedCrossingFences().Seq(1); got != 0 {
		t.Errorf("participant 1 keeps fence %d after this instance left its session", got)
	}
	if len(s.scheduled) != 1 || s.scheduled[0].source != 2 || s.scheduled[0].frame.Seq != 1 {
		t.Errorf("scheduled %+v after leaving; want this instance's own crossing alone", s.scheduled)
	}
	if len(s.states) != 0 || s.pendingDigest[1].Valid || s.relay != 0 {
		t.Errorf("the left session keeps %d syncs, digest %+v and relay %d", len(s.states), s.pendingDigest[1], s.relay)
	}
}

// inRun re-stamps one encoded batch with the run it was produced in.
func inRun(t *testing.T, body []byte, run uint64) []byte {
	t.Helper()
	batch, err := event.DecodeWireBatch(body)
	if err != nil {
		t.Fatal(err)
	}
	batch.Run = run
	if body, err = event.EncodeWireBatch(batch); err != nil {
		t.Fatal(err)
	}
	return body
}

// TestABatchFromAnotherRunIsRefused: a guest's last epochs and syncs of the run
// before a reset reach the authority after it applied the reset, a round trip
// after they were produced. Neither may land in the new run: an old sync would
// hold the slot's sequence over the new run's, an old epoch its source's window.
func TestABatchFromAnotherRunIsRefused(t *testing.T) {
	t.Parallel()
	authority := sessionSystem(t, 1, 3, &linkedPort{peers: []uint32{2}})
	authority.world.Resources.Event.Queue.RebaseStamp(1, 3)

	authority.scheduleCursorState(2, inRun(t, stateBatch(t, 2, 300, 0, false), 0))
	authority.scheduleCursorState(2, inRun(t, stateBatch(t, 2, 1, 0, false), 1))
	if len(authority.states) != 1 || authority.states[0].payload.Seq != 1 {
		t.Fatalf("the authority scheduled %+v, want the new run's first sync alone", authority.states)
	}

	authority.scheduleCrossings(2, inRun(t, rawBatch(t, 2, 150, 153), 0))
	authority.scheduleCrossings(2, inRun(t, rawBatch(t, 2, 1, 4), 1))
	authority.mu.Lock()
	got := append([]barrierArtifact(nil), authority.scheduled...)
	authority.mu.Unlock()
	if len(got) != 1 || got[0].applyTick != 4 {
		t.Fatalf("the authority scheduled %+v, want the new run's crossing at tick 4 alone", got)
	}
}

// TestAnInstallThatHoldsAnOwnCrossingClosesItsFence: a capture installed past the
// tick an own crossing was stamped for holds it, the authority having committed it
// late. The pruned copy never dispatches, so the install closes its place in the
// local fence; otherwise every later sequence waits behind it for the whole run.
func TestAnInstallThatHoldsAnOwnCrossingClosesItsFence(t *testing.T) {
	t.Parallel()
	event.EnsureRegistry()
	port := &linkedPort{peers: []uint32{1}}
	guest := sessionSystem(t, 2, 100, port)
	guest.refreshLink(port)
	for range 2 {
		if !guest.Cross(event.GameEvent{Type: event.EventGoldJumpRequest, Domain: core.DomainPlayer}) {
			t.Fatal("the barrier did not take an own crossing")
		}
	}
	guest.AdoptSnapshot(110, 1, network.CrossingFences{{Source: 2, Seq: 1}})
	if got := guest.AppliedCrossingFences().Seq(2); got != 1 {
		t.Fatalf("the local fence is %d after an install holding sequence 1, want 1", got)
	}
	guest.CrossingApplied(2)
	if got := guest.AppliedCrossingFences().Seq(2); got != 2 {
		t.Fatalf("the local fence is %d after sequence 2 applied, want 2", got)
	}
}

// sentTo lists the participants a port was asked to send to, in order; zero is a
// broadcast to every link.
func sentTo(port *linkedPort) []uint32 {
	out := make([]uint32, 0, len(port.sent))
	for _, f := range port.sent {
		out = append(out, f.to)
	}
	return out
}

// TestAGuestForwardsDownOnlyToThoseBehindIt: a committed copy and a correction
// chunk from the authority go to the link that sends this guest raw artifacts,
// never back up and never across the successor link.
func TestAGuestForwardsDownOnlyToThoseBehindIt(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{1, 3, 4}}
	guest := sessionSystem(t, 2, 100, port)
	guest.Carry(3)
	guest.scheduleCrossings(3, rawBatch(t, 3, 100, 104))
	port.sent = nil
	guest.scheduleCrossings(1, boundsBatch(t, 5, 100, 105, 1, 0))
	chunks, err := network.EncodeSnapshotChunks(0, 100, []byte("world"))
	if err != nil {
		t.Fatal(err)
	}
	guest.receiveCorrection(1, chunks[0])
	if got := sentTo(port); len(port.flooded) != 0 || !slices.Equal(got, []uint32{3, 3}) {
		t.Fatalf("the guest sent to %v and flooded %d frames, want the copy and the chunk to participant 3 alone",
			got, len(port.flooded))
	}
}

// TestAGuestCarriesOnlyWhomItRelaysFor: a raw epoch on a link this guest does not
// carry is dropped and counted, so nobody's traffic lands on a run that did not
// offer to carry it. One that joined through it is carried whatever its player
// chose, and any once its player relays for others.
func TestAGuestCarriesOnlyWhomItRelaysFor(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{1, 3, 4}}
	s := sessionSystem(t, 2, 100, port)
	var relaying atomic.Bool
	s.world.Resources.Network.Relaying = relaying.Load
	carried := func(from uint32, produced uint64) bool {
		t.Helper()
		port.sent = nil
		s.scheduleCrossings(from, rawBatch(t, from, produced, produced+4))
		s.Flush(produced)
		if got := sentTo(port); slices.ContainsFunc(got, func(to uint32) bool { return to != 1 }) {
			t.Fatalf("the guest sent to %v, want the authority alone", got)
		}
		return slices.Contains(sourcesOf(port), from)
	}
	if carried(3, 100) || s.statUncarried.Load() != 1 {
		t.Fatalf("a link this guest does not carry was carried, %d refused", s.statUncarried.Load())
	}
	s.served[4] = true
	if !carried(4, 101) {
		t.Fatal("a participant that joined through this guest was not carried")
	}
	relaying.Store(true)
	if !carried(3, 102) || s.statUncarried.Load() != 1 {
		t.Fatal("a guest relaying for others did not carry a participant that reaches it")
	}
}

// TestTheAuthorityReturnsTheCommittedCopyToItsProducer: the commit goes down every
// link, the producer's included, which is how a guest learns what was committed.
func TestTheAuthorityReturnsTheCommittedCopyToItsProducer(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{2}}
	s := sessionSystem(t, 1, 100, port)
	s.scheduleCrossings(2, rawBatch(t, 2, 100, 104))
	if got := sentTo(port); len(port.flooded) != 0 || !slices.Equal(got, []uint32{0}) {
		t.Fatalf("the authority sent to %v and flooded %d frames, want one broadcast", got, len(port.flooded))
	}
	if got, err := event.DecodeWireBatch(port.sent[0].body); err != nil || !got.Committed || got.Age != 1 {
		t.Fatalf("the broadcast is not the committed copy aged a tick (committed %t, age %d, err %v)", got.Committed, got.Age, err)
	}
}

// TestAParticipantWithNoPathAdoptsTheLinkThatReturnsItsEpochs: with neither the
// authority nor its relay on a link, a participant offers its raw epoch on every
// link not behind it, and the first link to return the committed copy is its path
// from then on.
func TestAParticipantWithNoPathAdoptsTheLinkThatReturnsItsEpochs(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{2, 4}}
	s := sessionSystem(t, 3, 100, port)
	s.Flush(100)
	if got := sentTo(port); !slices.Equal(got, []uint32{2, 4}) {
		t.Fatalf("a participant with no path sent to %v, want its epoch offered on both links", got)
	}
	port.sent = nil
	body, err := event.EncodeWireBatch(event.WireBatch{Source: 3, ProducedTick: 100, Committed: true})
	if err != nil {
		t.Fatal(err)
	}
	s.scheduleCrossings(2, body)
	s.Flush(101)
	if got := sentTo(port); !slices.Equal(got, []uint32{2}) || port.sent[0].kind != network.MsgEvent {
		t.Fatalf("after its epoch came back through participant 2 it sent %v, want its epoch to 2 alone", got)
	}
}

// TestAJoinerAtAHeldTickHearsWhatIsScheduledPastIt: a held session stands at one tick
// with crossings still due after it, which a joiner given that world never heard.
// They go to it as the committed epochs they went out in; an own crossing not yet
// sent goes out in its own epoch, and a void one applies nothing.
func TestAJoinerAtAHeldTickHearsWhatIsScheduledPastIt(t *testing.T) {
	t.Parallel()
	event.EnsureRegistry()
	host := sessionSystem(t, 1, 99, &linkedPort{peers: []uint32{2}})
	host.Flush(99)
	host.world.Resources.Event.Queue.RebaseStamp(0, 100)
	stale := 100 - uint64(parameter.NetworkCommitLateTicks) - 1
	host.scheduleCrossings(2, rawBatch(t, 2, 98, 101, 103, stale))
	cross := func() {
		t.Helper()
		if !host.Cross(event.GameEvent{Type: event.EventGoldJumpRequest, Domain: core.DomainPlayer}) {
			t.Fatal("the barrier did not take an own crossing")
		}
	}
	cross()
	host.Flush(100)
	cross()

	type epoch struct {
		source   uint32
		produced uint64
		applies  []uint64
	}
	var got []epoch
	for _, b := range host.PendingAfter(100) {
		if !b.Committed {
			t.Fatalf("participant %d's epoch %d goes out raw", b.Source, b.ProducedTick)
		}
		e := epoch{source: b.Source, produced: b.ProducedTick}
		for _, f := range b.Frames {
			e.applies = append(e.applies, f.ApplyTick)
		}
		got = append(got, e)
	}
	lead := uint64(parameter.NetworkBarrierDelayTicks)
	want := []epoch{{2, 98, []uint64{101, 103}}, {1, 100, []uint64{100 + lead}}}
	if !slices.EqualFunc(got, want, func(a, b epoch) bool {
		return a.source == b.source && a.produced == b.produced && slices.Equal(a.applies, b.applies)
	}) {
		t.Fatalf("a joiner at tick 100 is sent %+v, want %+v", got, want)
	}
}

// TestOnlyTheAuthorityRunningPastAGuestResumesIt: a held authority sends no epoch, so
// one it produced past a guest's tick is its resume. One at the guest's own tick, or
// another participant's past it, is not.
func TestOnlyTheAuthorityRunningPastAGuestResumesIt(t *testing.T) {
	t.Parallel()
	guest := sessionSystem(t, 2, 100, &linkedPort{peers: []uint32{1, 3}})
	resumed := 0
	guest.world.Resources.Network.OnAuthorityAhead = func() { resumed++ }
	epoch := func(source uint32, produced uint64) []byte {
		t.Helper()
		body, err := event.EncodeWireBatch(event.WireBatch{Source: source, ProducedTick: produced, Committed: true})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	guest.scheduleCrossings(1, epoch(1, 100))
	guest.scheduleCrossings(3, epoch(3, 101))
	if resumed != 0 {
		t.Fatalf("epochs at the guest's tick and from another participant resumed it %d times", resumed)
	}
	guest.scheduleCrossings(1, epoch(1, 101))
	if resumed != 1 {
		t.Fatalf("the authority's epoch past the guest resumed it %d times, want 1", resumed)
	}
}

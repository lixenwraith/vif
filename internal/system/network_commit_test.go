package system

import (
	"slices"
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

// TestAGuestSendsTheAuthorityOneFrameATick: a guest linked to the authority sends it
// alone its own raw epoch and those it carries for the participants behind it, as
// one frame, since nobody else does more with a raw one than pass it on.
func TestAGuestSendsTheAuthorityOneFrameATick(t *testing.T) {
	t.Parallel()
	port := &linkedPort{peers: []uint32{1, 3}}
	s := sessionSystem(t, 2, 100, port)
	s.scheduleCrossings(3, rawBatch(t, 3, 100, 104))
	s.Flush(100)
	if len(port.sent) != 1 || port.sent[0].to != 1 || port.sent[0].kind != network.MsgBundle {
		t.Fatalf("the guest sent %+v, want one bundle to the authority", port.sent)
	}
	var sources []uint32
	if err := network.Unbundle(port.sent[0].body, func(kind network.MessageType, body []byte) {
		if b, err := event.DecodeWireBatch(body); err == nil && kind == network.MsgEvent {
			sources = append(sources, b.Source)
		}
	}); err != nil || len(sources) != 2 || sources[0] != 3 || sources[1] != 2 {
		t.Fatalf("the bundle carries epochs of %v (%v), want the leaf's then the guest's", sources, err)
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

// TestALeadFallsToTheHighestReadingOfItsWindow: a lower target is adopted only
// once it has held for the window, and then at the highest value it read in it,
// since a trough the link cannot hold is climbed back a tick at a time.
func TestALeadFallsToTheHighestReadingOfItsWindow(t *testing.T) {
	t.Parallel()
	window := uint64(parameter.NetworkBarrierRenegotiateTicks)
	var lowSince, lowMax uint64
	next := uint64(12)
	for _, r := range []struct{ tick, target, want uint64 }{
		{90, 13, 13}, // a higher target is adopted at once
		{100, 6, 13}, // the window opens
		{110, 9, 13}, // a higher lower reading is remembered
		{100 + window - 1, 7, 13},
		{100 + window, 5, 9}, // the window closes on its highest reading
	} {
		next, lowSince, lowMax = chooseLead(next, r.target, r.tick, lowSince, lowMax)
		if next != r.want {
			t.Fatalf("tick %d target %d: lead %d, want %d", r.tick, r.target, next, r.want)
		}
	}
}

// TestAGuestStampsWithItsOwnRoundTrip: the lead is this guest's round trip to the
// authority plus the relay tick, less how late the authority's epochs land; the
// authority's own is a tick, and an unmeasured link keeps the default. Behind a
// relay the trip is how long this guest's own epochs took to come back committed.
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

	behind := &linkedPort{peers: []uint32{2}}
	leaf := sessionSystem(t, 3, 100, behind)
	leaf.paceLatest, leaf.paceKnown = -1, true
	body, err := event.EncodeWireBatch(event.WireBatch{Source: 3, ProducedTick: 88, Committed: true})
	if err != nil {
		t.Fatal(err)
	}
	leaf.scheduleCrossings(2, body)
	want = uint64(12 + parameter.NetworkRelaySlackTicks + 1)
	if got, _ := leaf.ownLead(behind); got != want {
		t.Fatalf("lead behind a relay, own epochs back in 12 ticks = %d, want %d", got, want)
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

// bundledSeqs lists the sequences of the epochs one bundle carries.
func bundledSeqs(t *testing.T, bundle []byte) []uint64 {
	t.Helper()
	var seqs []uint64
	err := network.Unbundle(bundle, func(kind network.MessageType, body []byte) {
		b, err := event.DecodeWireBatch(body)
		if err != nil || kind != network.MsgEvent {
			return
		}
		for _, f := range b.Frames {
			seqs = append(seqs, f.Frame.Seq)
		}
	})
	if err != nil {
		t.Fatal(err)
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
	if got := sentTo(port); !slices.Equal(got, []uint32{3}) || port.sent[0].kind != network.MsgBundle {
		t.Fatalf("after the handoff the guest sent %v, want one bundle to the successor", got)
	}
	if got := bundledSeqs(t, port.sent[0].body); !slices.Equal(got, []uint64{2, 3}) {
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
	guest.scheduleCrossings(3, rawBatch(t, 3, 100, 104)) // participant 3 is behind this guest
	port.sent = nil
	guest.scheduleCrossings(1, boundsBatch(t, 5, 100, 105, 1, 0))
	chunks, err := network.EncodeSnapshotChunks(100, []byte("world"))
	if err != nil {
		t.Fatal(err)
	}
	guest.receiveCorrection(1, chunks[0])
	if got := sentTo(port); len(port.flooded) != 0 || !slices.Equal(got, []uint32{3, 3}) {
		t.Fatalf("the guest sent to %v and flooded %d frames, want the copy and the chunk to participant 3 alone",
			got, len(port.flooded))
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
	if got, err := event.DecodeWireBatch(port.sent[0].body); err != nil || !got.Committed {
		t.Fatalf("the broadcast is not the committed copy (committed %t, err %v)", got.Committed, err)
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
	if got := sentTo(port); !slices.Equal(got, []uint32{2}) || port.sent[0].kind != network.MsgBundle {
		t.Fatalf("after its epoch came back through participant 2 it sent %v, want one bundle to 2", got)
	}
}

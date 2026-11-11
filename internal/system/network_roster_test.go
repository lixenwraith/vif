package system

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	link "github.com/lixenwraith/vif/pkg/network"
)

// rosterChange is one roster change of participant 7 in slot 2, as its authority
// crosses it: an arrival, or a departure that keeps its place or gives it up.
type rosterChange struct{ arrives, dismissed bool }

var (
	leaves  = rosterChange{}
	dropped = rosterChange{dismissed: true}
	arrives = rosterChange{arrives: true}
)

// crossRoster schedules each change on net's barrier, in order, from source.
func crossRoster(net *NetworkSystem, source uint32, changes ...rosterChange) {
	for _, c := range changes {
		ev := event.GameEvent{Type: event.EventParticipantDeparted, Origin: event.OriginSession, Domain: core.DomainPlayer,
			Payload: &event.ParticipantDepartedPayload{Participant: 7, Slot: 2, Dismissed: c.dismissed}}
		if c.arrives {
			ev.Type, ev.Payload = event.EventParticipantJoined, &event.ParticipantJoinedPayload{Participant: 7, Slot: 2}
		}
		net.schedule(ev)
		net.scheduled[len(net.scheduled)-1].source = source
	}
}

// applyRoster applies what the barrier holds due, each roster change in turn with
// what it asked of the cursors, and reports whether one was due.
func applyRoster(w *engine.World, net *NetworkSystem) (applied bool) {
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	net.applyDue(net.lastApplyTick)
	for _, ev := range slices.Clone(w.Resources.Event.Queue.Consume()) { // the queue reuses the slice
		applied = true
		net.HandleEvent(ev)
		for _, asked := range w.Resources.Event.Queue.Consume() {
			cursors.HandleEvent(asked)
		}
	}
	return applied
}

// linkedHolderWorld is holderWorld on a port linked to participant 7, with the mesh
// that links them.
func linkedHolderWorld(t *testing.T) (*engine.World, *NetworkSystem, map[uint32]bool, *link.Mesh) {
	t.Helper()
	w, net, finals := holderWorld(t)
	mesh := network.NewMesh()
	mesh.Link(1, 7)
	w.Resources.Network.Port = mesh.Node(1)
	net.drainWith(mesh.Node(1), mesh.Node(1).Drain)
	w.Resources.Event.Queue.Consume()
	return w, net, finals, mesh
}

// returnedWorld is a guest's world, participant 7 with its cursor live in slot 2,
// which the run took back from the run that left it there.
func returnedWorld(t *testing.T) (*engine.World, *NetworkSystem) {
	t.Helper()
	w, _, _ := testCursorWorld(t)
	spawnRemoteCursor(t, w, 2, 25, 5, 7)
	port, far := network.NewLoopbackPair(7, 1)
	t.Cleanup(func() { _ = port.Close(); _ = far.Close() })
	w.Resources.Network = engine.NewNetworkResource(port)
	w.RunSafe(w.LatchSession)
	net := ready(NewNetworkSystem(w)).(*NetworkSystem)
	w.RunSafe(func() { w.Supersede(true) })
	net.barrierActive.Store(true)
	return w, net
}

// drives reports whether this run drives the cursor in slot 2.
func drives(w *engine.World) bool {
	c, _ := w.Components.Cursor.GetComponent(w.Resources.Player.Slot(2))
	return c.Control == component.ControlLocal && w.Resources.Player.LocalSlot() == 2
}

// TestTheDepartingAreThoseTheAuthorityCrossedOutAndNothingBroughtBack: a participant
// reads as departing while the last roster change the barrier holds for it is its
// authority's departure that keeps its place, with no dismissal scheduled for it.
func TestTheDepartingAreThoseTheAuthorityCrossedOutAndNothingBroughtBack(t *testing.T) {
	for name, tc := range map[string]struct {
		source  uint32
		changes []rosterChange
		void    bool
		want    []uint32
	}{
		"crossed out":                      {source: 1, changes: []rosterChange{leaves}, want: []uint32{7}},
		"dropped":                          {source: 1, changes: []rosterChange{dropped}},
		"brought back":                     {source: 1, changes: []rosterChange{leaves, arrives}},
		"crossed out again":                {source: 1, changes: []rosterChange{leaves, arrives, leaves}, want: []uint32{7}},
		"dropped, its link lost behind it": {source: 1, changes: []rosterChange{dropped, leaves}},
		"crossed out by a guest":           {source: 2, changes: []rosterChange{leaves}},
		"crossed out, the crossing void":   {source: 1, changes: []rosterChange{leaves}, void: true},
		"nothing scheduled":                {source: 1},
	} {
		_, net, _ := holderWorld(t)
		crossRoster(net, tc.source, tc.changes...)
		if tc.void {
			net.scheduled[0].void = true
		}
		if got := net.Departing(); !slices.Equal(got, tc.want) {
			t.Fatalf("%s: departing %v, want %v", name, got, tc.want)
		}
	}
}

// TestADepartureItsArrivalStandsBehindLeavesItsPlaceLinked: a departure with its
// participant's arrival behind it, due at its tick or a later one, tells the session
// layer nothing and closes no link, and neither does one whose place a dial is
// taking back; one alone, or the last of its participant's changes, does both.
func TestADepartureItsArrivalStandsBehindLeavesItsPlaceLinked(t *testing.T) {
	for name, tc := range map[string]struct {
		changes      []rosterChange
		later, back  bool // the arrival applies a tick later; the session layer answers a dial has the place
		told, linked bool
	}{
		"alone":                        {changes: []rosterChange{leaves}, told: true},
		"its arrival behind it":        {changes: []rosterChange{leaves, arrives}, linked: true},
		"its arrival a tick behind it": {changes: []rosterChange{leaves, arrives}, later: true, linked: true},
		"its arrival and another departure behind it": {changes: []rosterChange{leaves, arrives, leaves}, told: true},
		"its place being taken back":                  {changes: []rosterChange{leaves}, back: true, told: true, linked: true},
	} {
		w, net, finals, mesh := linkedHolderWorld(t)
		told := 0
		w.Resources.Network.OnDeparture = func(id uint32, final bool) bool {
			if id == 7 {
				told++
			}
			finals[id] = final
			return tc.back
		}
		crossRoster(net, 1, tc.changes...)
		if tc.later {
			net.scheduled[1].applyTick++
		}
		applyRoster(w, net)
		if tc.later && (told != 0 || !mesh.Node(1).Connected(7)) {
			t.Fatalf("%s: before its arrival applied the session layer was told %d times, linked %v", name, told, mesh.Node(1).Connected(7))
		}
		for net.lastApplyTick++; applyRoster(w, net); net.lastApplyTick++ {
		}
		if got := mesh.Node(1).Connected(7); told != map[bool]int{true: 1}[tc.told] || got != tc.linked {
			t.Fatalf("%s: the session layer was told %d times and the link stands %v, want told %v, linked %v",
				name, told, got, tc.told, tc.linked)
		}
	}
}

// TestADepartureIsAnnouncedOnceForEachTimeItsParticipantIsIn: a lost link crosses
// one departure however often it is noticed, and another only once an arrival of
// its participant is scheduled; the link a departure closed as it applied is that
// departure's, whatever arrival is scheduled by the time its loss is drained.
func TestADepartureIsAnnouncedOnceForEachTimeItsParticipantIsIn(t *testing.T) {
	plain := event.ParticipantDepartedPayload{Participant: 7, Slot: 2}
	w, net, _, mesh := linkedHolderWorld(t)
	port := mesh.Node(1)
	lose := func() []event.ParticipantDepartedPayload {
		mesh.Node(7).Disconnect(1)
		net.drainWith(port, port.Drain)
		return departures(w)
	}
	if got := lose(); !slices.Equal(got, []event.ParticipantDepartedPayload{plain}) {
		t.Fatalf("a lost link crossed %+v, want %+v", got, plain)
	}
	if net.noticeDeparture(7, false); len(departures(w)) != 0 {
		t.Fatal("a departure noticed twice was crossed twice")
	}
	crossRoster(net, 1, leaves, arrives)
	mesh.Link(1, 7)
	if got := lose(); !slices.Equal(got, []event.ParticipantDepartedPayload{plain}) {
		t.Fatalf("the link lost behind a scheduled arrival crossed %+v, want %+v", got, plain)
	}

	w, net, _, mesh = linkedHolderWorld(t)
	port = mesh.Node(1)
	crossRoster(net, 1, leaves)
	applyRoster(w, net)
	crossRoster(net, 1, arrives)
	if net.drainWith(port, port.Drain); len(departures(w)) != 0 || port.Connected(7) {
		t.Fatalf("the link a departure closed crossed another (still linked: %v)", port.Connected(7))
	}
	mesh.Link(1, 7)
	if got := lose(); !slices.Equal(got, []event.ParticipantDepartedPayload{plain}) {
		t.Fatalf("a link opened since, and lost, crossed %+v, want %+v", got, plain)
	}
}

// TestASupersedingRunLeavesItsPlaceToTheRunItReplaced: until its own arrival
// applies, a run that took back a place whose cursor is live drives nothing and
// crosses nothing, and what its identity crossed applies as a peer's. Its arrival
// with no departure of its identity behind it ends that, and no other.
func TestASupersedingRunLeavesItsPlaceToTheRunItReplaced(t *testing.T) {
	w, net := returnedWorld(t)
	if drives(w) || !w.Session().Superseding {
		t.Fatalf("a run superseding (%v) drives the cursor it took back: %v", w.Session().Superseding, drives(w))
	}
	move := event.GameEvent{Type: event.EventCursorMoveRequest, Domain: core.DomainPlayer,
		Payload: &event.CursorMoveRequestPayload{Entity: w.Resources.Player.Slot(2), X: 9, Y: 9}}
	if !net.Cross(move) || len(net.scheduled) != 0 || net.crossSeq != 0 {
		t.Fatalf("a superseding run crossed %d artifacts, its sequence at %d", len(net.scheduled), net.crossSeq)
	}
	// What the run it replaced crossed, committed and returned, is no echo of its own
	body, err := event.EncodeWireBatch(event.WireBatch{Source: 7, ProducedTick: 1, Committed: true,
		Frames: []event.ScheduledWireFrame{{Frame: mustFrame(t, move, 1), ApplyTick: net.lastApplyTick}}})
	if err != nil {
		t.Fatal(err)
	}
	net.scheduleCrossings(1, body)
	if len(net.scheduled) != 1 {
		t.Fatalf("the crossing of the run it replaced was scheduled %d times, want once, as a peer's", len(net.scheduled))
	}
	local := net.statAppliedLocal.Load()
	net.applyDue(net.lastApplyTick)
	if w.Resources.Event.Queue.Consume(); net.statAppliedLocal.Load() != local {
		t.Fatal("the crossing of the run it replaced applied as this run's own")
	}

	crossRoster(net, 1, leaves, arrives, leaves)
	if applyRoster(w, net); drives(w) || !w.Session().Superseding {
		t.Fatalf("an arrival with its departure behind it ended the supersession (%v, drives %v)", !w.Session().Superseding, drives(w))
	}
	crossRoster(net, 1, arrives)
	if applyRoster(w, net); !drives(w) || w.Session().Superseding || w.Resources.Network.Superseding.Load() {
		t.Fatalf("after its own arrival the run drives its cursor %v, superseding %v", drives(w), w.Session().Superseding)
	}
}

// TestAWorldInstalledPastItsArrivalEndsASupersession: a world installed at or past
// the tick the run's own arrival applies at holds that arrival, so the cursor is
// the run's to drive from the install, or from the arrival's late frames; a world
// short of it, or an arrival its identity's departure follows, leaves it.
func TestAWorldInstalledPastItsArrivalEndsASupersession(t *testing.T) {
	batch := func(net *NetworkSystem, tick uint64, changes ...rosterChange) []byte {
		t.Helper()
		var frames []event.ScheduledWireFrame
		for i, c := range changes {
			ev := event.GameEvent{Type: event.EventParticipantDeparted, Origin: event.OriginSession, Domain: core.DomainPlayer,
				Payload: &event.ParticipantDepartedPayload{Participant: 7, Slot: 2}}
			if c.arrives {
				ev.Type, ev.Payload = event.EventParticipantJoined, &event.ParticipantJoinedPayload{Participant: 7, Slot: 2}
			}
			frames = append(frames, event.ScheduledWireFrame{Frame: mustFrame(t, ev, uint64(i+1)), ApplyTick: tick})
		}
		body, err := event.EncodeWireBatch(event.WireBatch{Source: 1, ProducedTick: 1, Committed: true, Frames: frames})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	for name, tc := range map[string]struct {
		changes   []rosterChange
		installed uint64 // the tick of the world installed, the arrival applying at 5
		late      bool   // the arrival's frames reach the run after the install
		want      bool
	}{
		"at its tick":                      {changes: []rosterChange{leaves, arrives}, installed: 5, want: true},
		"past its tick, its frames late":   {changes: []rosterChange{leaves, arrives}, installed: 8, late: true, want: true},
		"short of its tick":                {changes: []rosterChange{leaves, arrives}, installed: 4},
		"its departure behind the arrival": {changes: []rosterChange{leaves, arrives, leaves}, installed: 5},
	} {
		w, net := returnedWorld(t)
		if !tc.late {
			net.scheduleCrossings(1, batch(net, 5, tc.changes...))
		}
		net.AdoptSnapshot(tc.installed, 1, nil)
		if tc.late {
			net.scheduleCrossings(1, batch(net, 5, tc.changes...))
		}
		if got := drives(w); got != tc.want || w.Session().Superseding == tc.want {
			t.Fatalf("%s: the run drives its cursor %v, superseding %v; want it driven %v", name, got, w.Session().Superseding, tc.want)
		}
	}
}

// mustFrame is ev as the wire carries it, under sequence seq.
func mustFrame(t *testing.T, ev event.GameEvent, seq uint64) event.WireFrame {
	t.Helper()
	frame, encErr := event.NewWireFrame(ev)
	if encErr != "" {
		t.Fatalf("encode %v: %s", ev.Type, encErr)
	}
	frame.Seq = seq
	return frame
}

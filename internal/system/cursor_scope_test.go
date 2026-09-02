package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// Two per-instance effects the shared FSM raises — the drain pause and the
// grayout — now name the cursor they belong to.
//
// The region that raises them is shared: every instance runs the same machine and
// enters the same state, so an effect with no owner reaches every participant.
// That is right for a storm, which is one encounter everybody is inside, and wrong
// for a quasar, which is fused from one cursor's drains. These pin both halves —
// the scoped effect reaching only its owner, and the session-wide form still
// reaching everyone — and the overlap between them, which a single boolean got
// wrong in both directions.

// scopeWorld is one instance with two rostered cursors: slot 0 driven here, slot 1
// simulated somewhere else. It is the smallest world in which "my cursor" and
// "some other participant's cursor" are different entities.
func scopeWorld(t *testing.T) (*engine.World, core.Entity, core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())

	cursors := NewCursorSystem(w).(*CursorSystem)
	for slot, control := range map[uint8]component.ControlKind{
		0: component.ControlHuman, 1: component.ControlRemote,
	} {
		cursors.HandleEvent(event.GameEvent{
			Type: event.EventCursorSpawnRequest,
			Payload: &event.CursorSpawnRequestPayload{
				X: 10 + int(slot)*4, Y: 6, Slot: slot, Control: uint8(control),
			},
		})
	}
	w.Resources.Event.Queue.Consume()

	local, remote := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	if local == 0 || remote == 0 {
		t.Fatalf("roster = (%d, %d), want two cursors", uint64(local), uint64(remote))
	}
	if w.Resources.Player.Entity != local {
		t.Fatalf("this instance drives %d, want slot 0's cursor %d",
			uint64(w.Resources.Player.Entity), uint64(local))
	}
	return w, local, remote
}

// scopeEvent is one scoped local effect, as the FSM's EmitEvent action produces it.
func scopeEvent(et event.EventType, owner core.Entity) event.GameEvent {
	return event.GameEvent{Type: et, Payload: &event.CursorScopePayload{Entity: owner}}
}

// TestADrainPauseFollowsTheCursorItNames is the defect this replaces: one
// participant's quasar stopped every participant's drains.
func TestADrainPauseFollowsTheCursorItNames(t *testing.T) {
	w, local, remote := scopeWorld(t)
	drains := NewDrainSystem(w).(*DrainSystem)

	if drains.spawningPaused() {
		t.Fatal("a fresh drain system is paused")
	}

	drains.HandleEvent(scopeEvent(event.EventDrainPause, remote))
	if drains.spawningPaused() {
		t.Fatal("another participant's quasar paused this instance's drains")
	}

	drains.HandleEvent(scopeEvent(event.EventDrainPause, local))
	if !drains.spawningPaused() {
		t.Fatal("this cursor's own quasar did not pause its drains")
	}

	// A resume naming the other participant leaves this instance's hold alone.
	drains.HandleEvent(scopeEvent(event.EventDrainResume, remote))
	if !drains.spawningPaused() {
		t.Fatal("another participant's quasar exit resumed this instance's drains")
	}

	drains.HandleEvent(scopeEvent(event.EventDrainResume, local))
	if drains.spawningPaused() {
		t.Fatal("this cursor's own quasar exit did not resume its drains")
	}
}

// TestASessionWideDrainPauseStillReachesEveryCursor keeps the storm, the tower and
// the reset paths working: an action that names no cursor belongs to nobody, and
// every participant is inside it.
func TestASessionWideDrainPauseStillReachesEveryCursor(t *testing.T) {
	w, _, _ := scopeWorld(t)
	drains := NewDrainSystem(w).(*DrainSystem)

	// The shape an action with no payload table produces: EmitEvent allocates the
	// registered prototype, so the entity is zero rather than the payload absent.
	drains.HandleEvent(scopeEvent(event.EventDrainPause, 0))
	if !drains.spawningPaused() {
		t.Fatal("a session-wide pause did not reach this instance")
	}
	drains.HandleEvent(scopeEvent(event.EventDrainResume, 0))
	if drains.spawningPaused() {
		t.Fatal("a session-wide resume did not release this instance")
	}

	// And a payload of another shape, or none at all, is the session-wide form too.
	drains.HandleEvent(event.GameEvent{Type: event.EventDrainPause})
	if !drains.spawningPaused() {
		t.Fatal("a pause with no payload was not read as session-wide")
	}
}

// TestOverlappingPausesAreHeldSeparately is what a single boolean could not do: a
// quasar inside a storm, and a quasar's exit that must not resume drains the storm
// is still holding.
func TestOverlappingPausesAreHeldSeparately(t *testing.T) {
	w, local, _ := scopeWorld(t)
	drains := NewDrainSystem(w).(*DrainSystem)

	drains.HandleEvent(scopeEvent(event.EventDrainPause, 0))     // a storm
	drains.HandleEvent(scopeEvent(event.EventDrainPause, local)) // a quasar inside it
	drains.HandleEvent(scopeEvent(event.EventDrainResume, local))
	if !drains.spawningPaused() {
		t.Fatal("a quasar's exit resumed drains the session-wide region still holds")
	}

	drains.HandleEvent(scopeEvent(event.EventDrainResume, 0))
	if drains.spawningPaused() {
		t.Fatal("the session-wide resume left a hold behind")
	}
}

// TestAResumeWithNoOwnerClearsEveryHold pins the reset path: a region terminating
// everything, or a new game, must not leave a hold nothing will ever release.
func TestAResumeWithNoOwnerClearsEveryHold(t *testing.T) {
	w, local, remote := scopeWorld(t)
	drains := NewDrainSystem(w).(*DrainSystem)

	drains.HandleEvent(scopeEvent(event.EventDrainPause, local))
	drains.HandleEvent(scopeEvent(event.EventDrainPause, remote))
	drains.HandleEvent(scopeEvent(event.EventDrainResume, 0))
	if drains.spawningPaused() || len(drains.pausedFor) != 0 || drains.pausedAll {
		t.Fatalf("a session-wide resume left %d owner holds and pausedAll=%v",
			len(drains.pausedFor), drains.pausedAll)
	}

	// A reset clears them too, without a resume arriving at all.
	drains.HandleEvent(scopeEvent(event.EventDrainPause, local))
	drains.Init()
	if drains.spawningPaused() {
		t.Fatal("a reset left this instance's drains paused")
	}
}

// TestMoreHoldsThanTheRosterFallBackToPausing is the overflow rule. A hold set
// wider than the roster means one is leaking, and the safe answer is the pause
// rather than a silently dropped hold nothing would release.
func TestMoreHoldsThanTheRosterFallBackToPausing(t *testing.T) {
	w, _, _ := scopeWorld(t)
	drains := NewDrainSystem(w).(*DrainSystem)

	for i := range parameter.MaxPlayers + 2 {
		drains.HandleEvent(scopeEvent(event.EventDrainPause, core.Entity(1000+i)))
	}
	if !drains.spawningPaused() {
		t.Fatal("overflowing the hold set left drains spawning")
	}
	if len(drains.pausedFor) > parameter.MaxPlayers {
		t.Fatalf("the hold set grew to %d, past the roster bound %d",
			len(drains.pausedFor), parameter.MaxPlayers)
	}
}

// TestGrayoutFollowsTheCursorItNames is the same rule for the screen effect.
func TestGrayoutFollowsTheCursorItNames(t *testing.T) {
	w, local, remote := scopeWorld(t)
	transient := NewTransientSystem(w).(*TransientSystem)

	transient.HandleEvent(scopeEvent(event.EventGrayoutStart, remote))
	if w.Resources.View.Grayout.Active {
		t.Fatal("another participant's quasar greyed out this instance's screen")
	}

	transient.HandleEvent(scopeEvent(event.EventGrayoutStart, local))
	if !w.Resources.View.Grayout.Active {
		t.Fatal("this cursor's own quasar did not grey out its screen")
	}

	transient.HandleEvent(scopeEvent(event.EventGrayoutEnd, remote))
	if !w.Resources.View.Grayout.Active {
		t.Fatal("another participant's quasar exit cleared this instance's grayout")
	}

	transient.HandleEvent(scopeEvent(event.EventGrayoutEnd, local))
	if w.Resources.View.Grayout.Active {
		t.Fatal("this cursor's own quasar exit did not clear its grayout")
	}

	// Session-wide still reaches everyone, which is what a reset emits.
	transient.HandleEvent(scopeEvent(event.EventGrayoutStart, 0))
	if !w.Resources.View.Grayout.Active {
		t.Fatal("a session-wide grayout did not reach this instance")
	}
	transient.HandleEvent(scopeEvent(event.EventGrayoutEnd, 0))
	if w.Resources.View.Grayout.Active {
		t.Fatal("a session-wide grayout end did not reach this instance")
	}
}

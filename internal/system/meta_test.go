package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// A scenario's ask for a play mode waits on the context for the router, which alone
// moves a live run's mode; an ask for any other mode is none, and so is one a replay
// makes, whose recording journals the change itself.
func TestMetaLeavesAPlayModeAskForTheRouter(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	meta := ready(NewMetaSystem(ctx)).(*MetaSystem)
	ask := func(m core.GameMode) {
		meta.HandleEvent(event.GameEvent{Type: event.EventModeRequest, Payload: &event.ModeChangedPayload{Mode: m}})
	}
	ask(core.ModeInsert)
	if m, ok := ctx.TakeModeAsk(); !ok || m != core.ModeInsert {
		t.Fatalf("an ask for Insert left %v (%v), want Insert", m, ok)
	}
	if _, ok := ctx.TakeModeAsk(); ok {
		t.Fatal("a taken ask was there to take again")
	}
	ask(core.ModeSearch)
	ask(core.ModeCommand)
	if m, ok := ctx.TakeModeAsk(); ok {
		t.Fatalf("asks for a search and the command line left %v, want none", m)
	}
	w.FollowJournal()
	if ask(core.ModeNormal); ctx.GetMode() != core.ModeNormal {
		t.Fatalf("a replay's ask moved the mode to %v", ctx.GetMode())
	}
	if _, ok := ctx.TakeModeAsk(); ok {
		t.Fatal("a replay kept an ask its recording answers")
	}
}

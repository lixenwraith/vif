package engine

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

// TestAPMCountsGesturesNotEvents is the rule a mouse sweep broke: a pointer placing the
// cursor every tick counts by how far it travels, never more than once a placement;
// varied keys faster than a player acts read the ceiling, and one key hammered does not.
func TestAPMCountsGesturesNotEvents(t *testing.T) {
	perSecond := uint64(time.Second / parameter.GameUpdateInterval)
	ceiling := uint64(60 * parameter.APMMaxPerSecond)
	musicAPM := func(admit func(*GameState, int)) uint64 {
		gs := NewGameState()
		for i := range int(7 * perSecond) {
			admit(gs, i)
			gs.UpdateAPM(SimTime(gs.IncrementGameTicks(), parameter.GameUpdateInterval))
		}
		return gs.GetMusicAPM()
	}
	pointer := func(step int) func(*GameState, int) {
		return func(gs *GameState, i int) {
			gs.MovePointer(i*step%100, 0)
			gs.AdmitAction(0)
		}
	}

	// A column a tick is a slow reposition: a keystroke per APMPointerCells columns
	want := 60 * perSecond / parameter.APMPointerCells
	if got := musicAPM(pointer(1)); got+12 < want || got > want+12 {
		t.Errorf("slow pointer reads %d APM, want about %d", got, want)
	}
	if got, want := musicAPM(pointer(parameter.APMPointerCells+1)), uint64(60*parameter.APMPointerPerSecond); got != want {
		t.Errorf("fast pointer reads %d APM, want the pointer's share %d", got, want)
	}
	if got := musicAPM(pointer(0)); got != 0 {
		t.Errorf("a pointer that never moves reads %d APM", got)
	}
	if got := musicAPM(func(gs *GameState, i int) { gs.AdmitAction(1 + uint64(i%2)) }); got != ceiling {
		t.Errorf("two keys in turn every tick read %d APM, want the ceiling %d", got, ceiling)
	}
	if got := musicAPM(func(gs *GameState, _ int) { gs.AdmitAction(1) }); got >= parameter.TierNormalAPM {
		t.Errorf("one key hammered every tick reads %d APM, want it calm", got)
	}
}

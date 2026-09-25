package engine

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TestAPMCountsGesturesNotEvents is the rule a mouse sweep broke: a pointer placing the
// cursor every tick is travel, not twenty actions a second, and input arriving faster
// than a player acts reads the per-second ceiling.
func TestAPMCountsGesturesNotEvents(t *testing.T) {
	perSecond := uint64(time.Second / parameter.GameUpdateInterval)
	musicAPM := func(admit func(*GameState)) uint64 {
		gs := NewGameState()
		for range 7 * perSecond {
			admit(gs)
			gs.UpdateAPM(SimTime(gs.IncrementGameTicks(), parameter.GameUpdateInterval))
		}
		return gs.GetMusicAPM()
	}

	sweep := musicAPM(func(gs *GameState) { gs.AdmitAction(true) })
	if limit := 60 * perSecond / parameter.APMPointerTicks; sweep == 0 || sweep > limit {
		t.Errorf("pointer sweep reads %d APM, want 1..%d", sweep, limit)
	}
	storm := musicAPM(func(gs *GameState) { gs.AdmitAction(false) })
	if limit := uint64(60 * parameter.APMMaxPerSecond); storm != limit {
		t.Errorf("an input storm reads %d APM, want the ceiling %d", storm, limit)
	}
}

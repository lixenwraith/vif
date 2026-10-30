package engine

import (
	"math"
	"testing"

	"github.com/lixenwraith/vif/internal/parameter"
)

// TestSetupLevelRecordsOnlyBoundsTheGridHolds pins that Config and the grid
// describe one map. Clamping only inside the grid would leave map logic reading
// bounds no cell exists for.
func TestSetupLevelRecordsOnlyBoundsTheGridHolds(t *testing.T) {
	t.Parallel()
	w := NewWorld()
	NewGameContextWithClock(w, 80, 24, NewManualClock())

	w.SetupLevel(math.MaxInt32, math.MaxInt32, false, false, false)

	cfg := w.Resources.Config
	if cfg.MapWidth > parameter.MaxMapWidth || cfg.MapHeight > parameter.MaxMapHeight ||
		cfg.MapWidth*cfg.MapHeight > parameter.MaxMapCells {
		t.Fatalf("config records a %dx%d map the grid cannot hold", cfg.MapWidth, cfg.MapHeight)
	}
	gw, gh := w.Positions.Width, w.Positions.Height
	if gw != cfg.MapWidth || gh != cfg.MapHeight {
		t.Fatalf("config says %dx%d, grid says %dx%d", cfg.MapWidth, cfg.MapHeight, gw, gh)
	}
}

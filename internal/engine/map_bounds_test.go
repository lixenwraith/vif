package engine

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TestClampMapSizeBoundsEveryShape covers the three ways a requested map can be
// illegal: too wide, too tall, and legal on both axes but too large as a product.
func TestClampMapSizeBoundsEveryShape(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		w, h int
	}{
		{"ordinary", 160, 50},
		{"at the cell ceiling", parameter.DefaultGridWidth, parameter.DefaultGridHeight},
		{"too wide", parameter.MaxMapWidth * 4, 10},
		{"too tall", 10, parameter.MaxMapHeight * 4},
		{"legal axes, illegal product", parameter.MaxMapWidth, parameter.MaxMapHeight},
		{"overflows int on multiply", math.MaxInt32, math.MaxInt32},
		{"maximum int", math.MaxInt, math.MaxInt},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w, h := ClampMapSize(tt.w, tt.h)
			if w < 1 || h < 1 {
				t.Fatalf("clamped %dx%d to %dx%d, which is not a map", tt.w, tt.h, w, h)
			}
			if w > parameter.MaxMapWidth || h > parameter.MaxMapHeight {
				t.Fatalf("clamped %dx%d to %dx%d, outside %dx%d",
					tt.w, tt.h, w, h, parameter.MaxMapWidth, parameter.MaxMapHeight)
			}
			if w*h > parameter.MaxMapCells {
				t.Fatalf("clamped %dx%d to %d cells, ceiling is %d", tt.w, tt.h, w*h, parameter.MaxMapCells)
			}
		})
	}
}

// TestClampMapSizeLeavesALegalMapAlone is the non-vacuous half: a clamp that
// reshaped an ordinary map would be a bug rather than a defence.
func TestClampMapSizeLeavesALegalMapAlone(t *testing.T) {
	t.Parallel()
	for _, tt := range [][2]int{{80, 24}, {160, 50}, {200, 60}, {1000, 100}} {
		if w, h := ClampMapSize(tt[0], tt[1]); w != tt[0] || h != tt[1] {
			t.Fatalf("clamped a legal %dx%d to %dx%d", tt[0], tt[1], w, h)
		}
	}
}

// TestTheGridSurvivesAHostileResize is the defect this bounds.
//
// The dimensions reach make() from a LevelSetup payload, which is replicated and
// therefore reachable from any participant. Before the clamp, a width and height
// whose product overflows int panicked in the allocator, and under the crash
// handler that is the process.
func TestTheGridSurvivesAHostileResize(t *testing.T) {
	t.Parallel()
	g := NewSpatialGrid(80, 24)
	for _, tt := range [][2]int{
		{math.MaxInt32, math.MaxInt32},
		{math.MaxInt, math.MaxInt},
		{1 << 31, 1 << 31},
		{-1, -1},
		{0, 0},
		{parameter.MaxMapWidth * 100, parameter.MaxMapHeight * 100},
	} {
		g.Resize(tt[0], tt[1]) // must not panic
		if len(g.Cells) > parameter.MaxMapCells {
			t.Fatalf("resize to %dx%d allocated %d cells", tt[0], tt[1], len(g.Cells))
		}
		if g.Width*g.Height != len(g.Cells) {
			t.Fatalf("resize to %dx%d left %dx%d describing %d cells",
				tt[0], tt[1], g.Width, g.Height, len(g.Cells))
		}
	}
}

// TestSetupLevelRecordsOnlyBoundsTheGridHolds pins that Config and the grid
// describe one map. Clamping only inside the grid would leave map logic reading
// bounds no cell exists for.
func TestSetupLevelRecordsOnlyBoundsTheGridHolds(t *testing.T) {
	t.Parallel()
	w := NewWorld()
	NewGameContextWithClock(w, 80, 24, NewManualClock())

	w.SetupLevel(math.MaxInt32, math.MaxInt32, false, false)

	cfg := w.Resources.Config
	if cfg.MapWidth > parameter.MaxMapWidth || cfg.MapHeight > parameter.MaxMapHeight ||
		cfg.MapWidth*cfg.MapHeight > parameter.MaxMapCells {
		t.Fatalf("config records a %dx%d map the grid cannot hold", cfg.MapWidth, cfg.MapHeight)
	}
	gw, gh := w.Positions.GridDimensions()
	if gw != cfg.MapWidth || gh != cfg.MapHeight {
		t.Fatalf("config says %dx%d, grid says %dx%d", cfg.MapWidth, cfg.MapHeight, gw, gh)
	}
}

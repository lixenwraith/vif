package engine

import (
	"math"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
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

// TestSetupLevelDropsWhatAShrinkLeavesOffTheMap: a level setup that keeps its
// entities still ends the ones past the new bounds, as a crop does, and spares
// one protected from death.
func TestSetupLevelDropsWhatAShrinkLeavesOffTheMap(t *testing.T) {
	t.Parallel()
	w := NewWorld()
	NewGameContextWithClock(w, 80, 24, NewManualClock())
	place := func(x, y int) core.Entity {
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
		return e
	}
	inside, outside, protected := place(10, 5), place(70, 20), place(75, 22)
	w.Components.Protection.SetComponent(protected, component.ProtectionComponent{Mask: component.ProtectFromDeath})

	w.SetupLevel(40, 12, false, false, false)

	dying := func(e core.Entity) bool { return w.Components.Death.HasEntity(e) }
	if dying(inside) || !dying(outside) || dying(protected) {
		t.Fatalf("death tagged inside %v, outside %v, protected %v; want only the outside one",
			dying(inside), dying(outside), dying(protected))
	}
}

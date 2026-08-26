package engine

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

func TestPositionTelemetryReportsCellSaturationAndOverflow(t *testing.T) {
	w := NewWorld()
	_ = NewGameContextWithClock(w, 80, 24, NewManualClock())
	reg := w.Resources.Status

	var first core.Entity
	w.RunSafe(func() {
		for i := range parameter.MaxEntitiesPerCell + 1 {
			e := w.CreateEntity(core.DomainShared)
			if i == 0 {
				first = e
			}
			w.Positions.SetPosition(e, component.PositionComponent{X: 4, Y: 5})
		}
		w.Positions.SetPosition(first, component.PositionComponent{X: 4, Y: 5})
		w.Positions.PublishTelemetry()
	})

	if got := reg.Ints.Get("spatial.cell_saturations").Load(); got != 1 {
		t.Fatalf("cell saturations = %d, want 1", got)
	}
	if got := reg.Ints.Get("spatial.cell_overflows").Load(); got != 1 {
		t.Fatalf("cell overflows = %d, want 1", got)
	}
	if got := reg.Ints.Get("spatial.max_cell_occupancy").Load(); got != parameter.MaxEntitiesPerCell {
		t.Fatalf("max occupancy = %d, want %d", got, parameter.MaxEntitiesPerCell)
	}
}

// TestPlayerBudgetRejectIsCountedSeparately proves a spent player budget is not cell exhaustion.
func TestPlayerBudgetRejectIsCountedSeparately(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 40, 24, NewManualClock())
	reg := w.Resources.Status

	const excess = 4
	for range parameter.ReservedPlayerPerCell + excess {
		e := w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(e, component.PositionComponent{X: 3, Y: 3})
	}
	if got := reg.Ints.Get("spatial.player_budget_rejects").Load(); got != excess {
		t.Fatalf("player budget rejects = %d, want %d", got, excess)
	}
	if got := reg.Ints.Get("spatial.cell_overflows").Load(); got != 0 {
		t.Fatalf("cell overflows = %d, want 0 while the shared half is free", got)
	}

	// The shared half must still accept its full complement
	wantShared := int64(parameter.MaxEntitiesPerCell - parameter.ReservedPlayerPerCell)
	for range wantShared {
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: 3, Y: 3})
	}
	w.Positions.PublishTelemetry()
	if got := reg.Ints.Get("spatial.indexed_shared").Load(); got != wantShared {
		t.Fatalf("indexed shared = %d, want %d", got, wantShared)
	}
	if got := reg.Ints.Get("spatial.cell_overflows").Load(); got != 0 {
		t.Fatalf("cell overflows = %d, want 0 after filling the shared half exactly", got)
	}
}

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
			e := w.CreateEntity()
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

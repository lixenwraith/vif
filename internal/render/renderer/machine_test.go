package renderer

import (
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestTheStateOverlayLabelsEachMachineSpeciesInstance: while the overlay is on, a
// body a machine steps reads its state's name, with how far through a timed entry
// it is, centred on the row above it, or under it on the map's top row
func TestTheStateOverlayLabelsEachMachineSpeciesInstance(t *testing.T) {
	w, game := glyphWorld()
	config := w.Resources.Config
	w.Resources.Machines[component.SpeciesSwarm] = []string{"chase", "lock"}
	w.Resources.Machines[component.SpeciesDataFirst] = []string{"Aim", "Hold"}
	w.Resources.Behaviors = []component.BehaviorPrototype{{Footprint: vmath.Footprint{Width: 3, Height: 2, HeaderX: 1}}}

	swarm, ship := w.CreateEntity(core.DomainShared), w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(swarm, component.PositionComponent{X: 20, Y: 10})
	w.Components.Swarm.SetComponent(swarm, component.SwarmComponent{Machine: machine.Instance[component.SwarmState]{
		State: component.SwarmStateLock, Elapsed: 500 * time.Millisecond, Duration: 2 * time.Second}})
	w.Positions.SetPosition(ship, component.PositionComponent{X: 8, Y: 0})
	w.Components.Behavior.SetComponent(ship, component.BehaviorComponent{Machine: machine.Instance[component.BehaviorState]{State: 1}})

	r := NewMachineRenderer(game)
	if r.IsVisible() {
		t.Fatal("the overlay shows before it is turned on")
	}
	game.OverlayMachine.Store(true)
	if !r.IsVisible() {
		t.Fatal("the overlay stays hidden once turned on")
	}
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	r.Render(render.RenderContext{MapWidth: config.MapWidth, MapHeight: config.MapHeight,
		ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}, buf)

	foot := component.Footprints[component.SpeciesSwarm]
	for _, want := range []struct {
		text string
		x, y int
	}{
		{"lock 25%", 20 - foot.HeaderX + (foot.Width-8)/2, 10 - foot.HeaderY - 1},
		{"Hold", 7, 2},
	} {
		for i, ch := range want.text {
			if got := buf.CellAt(want.x+i, want.y); got.Rune != ch || got.Fg != visual.RgbHudValue {
				t.Fatalf("%q: (%d,%d) drew %q in %v, want %q in %v", want.text, want.x+i, want.y, got.Rune, got.Fg, ch, visual.RgbHudValue)
			}
		}
	}
}

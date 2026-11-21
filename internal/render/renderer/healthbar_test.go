package renderer

import (
	"cmp"
	"slices"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func TestKrakenHealthBarStaysVisibleCenteredAndScalesInBothColorModes(t *testing.T) {
	for _, mode := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256} {
		w := engine.NewWorld()
		game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
		w.Resources.Config.ColorMode = mode
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: 20, Y: 12})
		w.Components.Kraken.SetComponent(e, component.KrakenComponent{})
		motion := component.KineticComponent{}
		motion.PreciseX, motion.PreciseY = 20.5, 12.5
		w.Components.Kinetic.SetComponent(e, motion)
		layers := []render.Registration{
			{Renderer: NewHealthBarRenderer(game), Priority: render.PriorityHealthBar},
			{Renderer: NewKrakenRenderer(game), Priority: render.PriorityKraken},
		}
		slices.SortFunc(layers, func(a, b render.Registration) int { return cmp.Compare(a.Priority, b.Priority) })
		ctx := render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}
		for _, hp := range []int{parameter.KrakenInitialHP, parameter.KrakenInitialHP / 2, 1} {
			w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntityKraken, HitPoints: hp})
			buf := render.NewRenderBuffer(mode, 40, 24)
			for _, layer := range layers {
				layer.Renderer.Render(ctx, buf)
			}
			want := (hp*visual.KrakenHealthBarWidth + parameter.KrakenInitialHP - 1) / parameter.KrakenInitialHP
			count := 0
			for y := range 24 {
				for x := range 40 {
					cell := buf.CellAt(x, y)
					if cell.Rune != visual.HealthBarChar {
						continue
					}
					count++
					if y != 12 || x < 20-want/2 || x >= 20-want/2+want {
						t.Fatalf("health bar shifted from center: (%d,%d)", x, y)
					}
				}
			}
			if count != want {
				t.Fatalf("health %d: %d bar cells, want %d", hp, count, want)
			}
		}
	}
}

// TestAKrakensHealthBarSpansTheHitPointsItRoseWith: a Kraken spawned with hit points
// of its own fills its bar with them, so half of them draw half the bar
func TestAKrakensHealthBarSpansTheHitPointsItRoseWith(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	e := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(e, component.PositionComponent{X: 20, Y: 12})
	w.Components.Kraken.SetComponent(e, component.KrakenComponent{MaxHitPoints: 120})
	w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntityKraken, HitPoints: 60})

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 40, 24)
	NewHealthBarRenderer(game).Render(render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}, buf)
	count := 0
	for y := range 24 {
		for x := range 40 {
			if buf.CellAt(x, y).Rune == visual.HealthBarChar {
				count++
			}
		}
	}
	if want := (visual.KrakenHealthBarWidth + 1) / 2; count != want {
		t.Fatalf("60 of 120 hit points draw %d bar cells, want %d", count, want)
	}
}

// TestADataSpeciesHealthBarSpansItsPrototype: the bar measures the prototype's
// footprint and hit points, not the swarm row the species fights as
func TestADataSpeciesHealthBarSpansItsPrototype(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	w.Resources.Behaviors = []component.BehaviorPrototype{{Name: "warden", Footprint: vmath.Footprint{Width: 6, Height: 2, HeaderX: 1}, HitPoints: 40}}
	e := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(e, component.PositionComponent{X: 20, Y: 12})
	w.Components.Behavior.SetComponent(e, component.BehaviorComponent{})
	w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm, HitPoints: 20})

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 40, 24)
	NewHealthBarRenderer(game).Render(render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}, buf)
	var cells [][2]int
	for y := range 24 {
		for x := range 40 {
			if buf.CellAt(x, y).Rune == visual.HealthBarChar {
				cells = append(cells, [2]int{x, y})
			}
		}
	}
	// Half of six cells, above the footprint's left edge
	if want := [][2]int{{19, 11}, {20, 11}, {21, 11}}; !slices.Equal(cells, want) {
		t.Fatalf("bar cells %v, want %v", cells, want)
	}
}

// TestAPinnedHealthBarStandsOnTheViewsTopRow: a Devourer's bar spans a share of the
// view's top row, centred, filled for the hit points it keeps of those it rose with
// and dotted for the rest, on a backing darker than what it stands over
func TestAPinnedHealthBarStandsOnTheViewsTopRow(t *testing.T) {
	for _, mode := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256} {
		w := engine.NewWorld()
		game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
		w.Resources.Config.ColorMode = mode
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{})
		w.Components.Devourer.SetComponent(e, component.DevourerComponent{MaxHitPoints: 1000})
		w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntityDevourer, HitPoints: 500})

		buf := render.NewRenderBuffer(mode, 40, 24)
		flesh := visual.RgbDevourerFlesh[3].Color
		for x := range 40 {
			buf.SetWithBg(x, 0, ' ', flesh, flesh)
		}
		NewHealthBarRenderer(game).Render(render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}, buf)
		view := 40.0
		width := int(view * visual.HealthBarPinnedShare)
		x0, filled := (40-width)/2, (width+1)/2
		for y := range 24 {
			for x := range 40 {
				cell := buf.CellAt(x, y)
				want := rune(0)
				switch {
				case y != 0 || x < x0 || x >= x0+width:
				case x < x0+filled:
					want = visual.HealthBarChar
				default:
					want = visual.HealthBarLostChar
				}
				if got := cell.Rune; (got == visual.HealthBarChar || got == visual.HealthBarLostChar || want != 0) && got != want && (want != 0 || got != ' ') {
					t.Fatalf("mode %d: cell %d,%d shows %q, want %q", mode, x, y, got, want)
				}
				if want != 0 && int(cell.Bg.R)+int(cell.Bg.G)+int(cell.Bg.B) >= int(flesh.R)+int(flesh.G)+int(flesh.B) {
					t.Fatalf("mode %d: cell %d,%d stands on no backing", mode, x, y)
				}
			}
		}
	}
}

// TestABarItsBodyCannotShowWholeSlidesIntoTheView: a body wider and taller than the
// view leaves its bar no side, so the bar stands on the view's top row, cut to its
// width, where before it stood below the view
func TestABarItsBodyCannotShowWholeSlidesIntoTheView(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	w.Resources.Behaviors = []component.BehaviorPrototype{{Name: "bulk", Footprint: vmath.Footprint{Width: 60, Height: 30, HeaderX: 30, HeaderY: 15}, HitPoints: 40}}
	e := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(e, component.PositionComponent{X: 20, Y: 12})
	w.Components.Behavior.SetComponent(e, component.BehaviorComponent{})
	w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm, HitPoints: 40})

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 40, 24)
	NewHealthBarRenderer(game).Render(render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}, buf)
	for y := range 24 {
		for x := range 40 {
			if bar := buf.CellAt(x, y).Rune == visual.HealthBarChar; bar != (y == 0) {
				t.Fatalf("cell %d,%d bar %v", x, y, bar)
			}
		}
	}
}

package renderer

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// TestADataSpeciesDrawsTheFrameItShowsInItsStateColour: each member takes its cell
// of the current frame, coloured by hit flash, then enrage, then the prototype
func TestADataSpeciesDrawsTheFrameItShowsInItsStateColour(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	orange := color.RGB{R: 200, G: 120, B: 40}
	w.Resources.Behaviors = []component.BehaviorPrototype{{
		Name: "warden", Width: 2, Height: 1, HeaderX: 1, HitPoints: 40,
		Frames: [][]rune{[]rune("ab"), []rune("cd")}, Color: orange,
	}}
	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 11, Y: 5})
	var members []component.MemberEntry
	for col := range 2 {
		m := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(m, component.PositionComponent{X: 10 + col, Y: 5})
		members = append(members, component.MemberEntry{Entity: m, OffsetX: col - 1})
	}
	w.Components.Header.SetComponent(header, component.HeaderComponent{Behavior: component.BehaviorData, MemberEntries: members})
	w.Components.Behavior.SetComponent(header, component.BehaviorComponent{PatternIndex: 1})
	combat := component.CombatComponent{CombatEntityType: component.CombatEntitySwarm, HitPoints: 40}

	draw := func() (terminal.Cell, terminal.Cell) {
		w.Components.Combat.SetComponent(header, combat)
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 40, 24)
		NewBehaviorRenderer(game).Render(render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}, buf)
		return buf.CellAt(10, 5), buf.CellAt(11, 5)
	}
	if left, right := draw(); left.Rune != 'c' || right.Rune != 'd' || left.Fg != orange {
		t.Fatalf("drew %q%q in %v, want the second frame in the prototype's colour", left.Rune, right.Rune, left.Fg)
	}
	combat.IsEnraged = true
	if left, _ := draw(); left.Fg != visual.RgbCombatEnraged {
		t.Fatalf("enraged in %v, want %v", left.Fg, visual.RgbCombatEnraged)
	}
	combat.RemainingHitFlash = parameter.CombatHitFlashDuration / 2
	if left, _ := draw(); left.Fg != hitFlashColor(combat.RemainingHitFlash) {
		t.Fatalf("hit while enraged in %v, want the hit flash", left.Fg)
	}
}

// TestADataSpeciesInABeamStateDrawsTheTeleportLine: a state flagged Beam draws the
// swarm's teleport line from where the header stands to the cell it stored, and no
// line without it
func TestADataSpeciesInABeamStateDrawsTheTeleportLine(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	w.Resources.Behaviors = []component.BehaviorPrototype{{Name: "raider", Width: 2, Height: 1, HeaderX: 1, HitPoints: 20}}
	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 5, Y: 5})
	b := component.BehaviorComponent{TargetX: 30, TargetY: 5}
	b.Machine.Duration = parameter.SwarmTeleportDuration
	b.Machine.Elapsed = parameter.SwarmTeleportDuration / 2 // the hold, where the whole line shows

	draw := func() terminal.Cell {
		w.Components.Behavior.SetComponent(header, b)
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 40, 24)
		NewTeleportLineRenderer(game).Render(render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}, buf)
		return buf.CellAt(17, 5)
	}
	if mid := draw(); mid.Bg != (color.RGB{}) {
		t.Fatalf("a state without the flag drew %v midway, want no line", mid.Bg)
	}
	b.Beaming = true
	if mid := draw(); mid.Bg != visual.RgbSwarmTeleport {
		t.Fatalf("a state flagged Beam drew %v midway, want the teleport line's %v", mid.Bg, visual.RgbSwarmTeleport)
	}
}

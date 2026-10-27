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
// of the current frame, its glyph in its palette entry, else the look's colour, over
// its background; the hit flash, then enrage, tints every glyph
func TestADataSpeciesDrawsTheFrameItShowsInItsStateColour(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	orange, teal, navy := color.RGB{R: 200, G: 120, B: 40}, color.RGB{G: 160, B: 160}, color.RGB{B: 80}
	look, err := visual.CompileLook(2, 1, visual.LookArt{
		Fg: []color.RGB{teal}, Bg: []color.RGB{navy}, Color: orange,
		Frames: []visual.LookFrame{
			{Glyphs: []string{"ab"}},
			{Glyphs: []string{"cd"}, Fg: []string{"0 "}, Bg: []string{" 0"}, Attr: []string{"B "}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	header, combat := wardenAt(w, component.BehaviorPrototype{Name: "warden", Width: 2, Height: 1, HeaderX: 1, HitPoints: 40, Look: look})

	draw := func() (terminal.Cell, terminal.Cell) {
		w.Components.Combat.SetComponent(header, *combat)
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 40, 24)
		NewBehaviorRenderer(game).Render(render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}, buf)
		return buf.CellAt(10, 5), buf.CellAt(11, 5)
	}
	left, right := draw()
	if left.Rune != 'c' || right.Rune != 'd' {
		t.Fatalf("drew %q%q, want the second frame", left.Rune, right.Rune)
	}
	if left.Fg != teal || left.Attrs != terminal.AttrBold || right.Fg != orange || right.Bg != navy {
		t.Fatalf("drew c in %v (%v) and d in %v over %v, want c bold in its entry and d in the look's colour over its background",
			left.Fg, left.Attrs, right.Fg, right.Bg)
	}
	combat.IsEnraged = true
	if left, right := draw(); left.Fg != visual.RgbCombatEnraged || right.Fg != visual.RgbCombatEnraged || right.Bg != navy {
		t.Fatalf("enraged in %v and %v over %v, want every glyph in %v over its background", left.Fg, right.Fg, right.Bg, visual.RgbCombatEnraged)
	}
	combat.RemainingHitFlash = parameter.CombatHitFlashDuration / 2
	if left, _ := draw(); left.Fg != hitFlashColor(combat.RemainingHitFlash) {
		t.Fatalf("hit while enraged in %v, want the hit flash", left.Fg)
	}
}

// TestADataSpeciesOnAConsoleDrawsItsConsolePair: a 256-colour host draws a look with
// a console pair in it, every glyph in its foreground and every background in its
// background, and shows a hit by reversing the glyph
func TestADataSpeciesOnAConsoleDrawsItsConsolePair(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorMode256
	look, err := visual.CompileLook(2, 1, visual.LookArt{
		Fg: []color.RGB{{G: 160, B: 160}}, Bg: []color.RGB{{B: 80}}, Fg256: 14, Bg256: 4, Console: true,
		Frames: []visual.LookFrame{{Glyphs: []string{"c "}, Fg: []string{"0 "}, Bg: []string{"00"}, Attr: []string{"B "}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	header, combat := wardenAt(w, component.BehaviorPrototype{Name: "warden", Width: 2, Height: 1, HeaderX: 1, HitPoints: 40, Look: look})

	draw := func() (terminal.Cell, terminal.Cell) {
		w.Components.Combat.SetComponent(header, *combat)
		buf := render.NewRenderBuffer(terminal.ColorMode256, 40, 24)
		NewBehaviorRenderer(game).Render(render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}, buf)
		return buf.CellAt(10, 5), buf.CellAt(11, 5)
	}
	left, right := draw()
	if left.Rune != 'c' || left.Fg.R != 14 || left.Attrs != terminal.AttrFg256|terminal.AttrBg256|terminal.AttrBold {
		t.Fatalf("drew %q in %d (%v), want c bold in the console foreground", left.Rune, left.Fg.R, left.Attrs)
	}
	if right.Rune != 0 || right.Bg.R != 4 || right.Attrs != terminal.AttrBg256 {
		t.Fatalf("drew %q over %d (%v) where the frame has no glyph, want the console background alone", right.Rune, right.Bg.R, right.Attrs)
	}
	combat.RemainingHitFlash = parameter.CombatHitFlashDuration / 2
	if left, _ := draw(); left.Fg.R != 14 || left.Attrs&terminal.AttrReverse == 0 {
		t.Fatalf("hit in %d (%v), want the console foreground reversed", left.Fg.R, left.Attrs)
	}
}

// wardenAt stands a two-cell instance of the world's one program with its header at
// {11, 5} and the frame index at 1, returning the header and its combat to set
func wardenAt(w *engine.World, p component.BehaviorPrototype) (core.Entity, *component.CombatComponent) {
	w.Resources.Behaviors = []component.BehaviorPrototype{p}
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
	return header, &component.CombatComponent{CombatEntityType: component.CombatEntitySwarm, HitPoints: 40}
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

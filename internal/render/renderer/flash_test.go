package renderer

import (
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestEffectBatchesFadeBetweenTicks: a batch's cells are drawn as far through the
// fade as its deadline stands at the frame's moment, part of a tick past the tick
// that aged it: a flash's glyph in the removal flash's color, a fadeout's cell in
// its own colors over the background, its glyph alone where the cell is not solid,
// and its ground alone under a flash's glyph, which keeps its color
func TestEffectBatchesFadeBetweenTicks(t *testing.T) {
	w, game := glyphWorld()
	tick := engine.SimEpoch.Add(time.Second)
	w.Resources.Time.PaceTime, w.Resources.Time.DeltaTime = tick, 50*time.Millisecond
	red, green, blue := color.RGB{R: 200}, color.RGB{G: 200}, color.RGB{B: 100}

	flash, fadeout := w.CreateEntity(core.DomainPlayer), w.CreateEntity(core.DomainPlayer)
	w.Components.Flash.SetComponent(flash, component.FlashComponent{Cells: []component.EffectCell{{X: 3, Y: 4, Rune: 'f'}, {X: 6, Y: 4, Rune: 'h'}}})
	w.Components.Fadeout.SetComponent(fadeout, component.FadeoutComponent{Cells: []component.EffectCell{
		{X: 5, Y: 4, Rune: 'w', Fg: red, Bg: blue, Solid: true},
		{X: 6, Y: 4, Bg: blue, Solid: true},
		{X: 7, Y: 4, Rune: 'g', Fg: green},
	}})
	// Both a fifth through a half-second fade as the tick left them
	const duration, elapsed = 500 * time.Millisecond, 100 * time.Millisecond
	for _, e := range []core.Entity{flash, fadeout} {
		var timer component.TimerComponent
		timer.Slots[component.TimerDeadline] = component.Countdown{Remaining: duration - elapsed, Duration: duration}
		w.Components.Timer.SetComponent(e, timer)
	}

	for _, since := range []time.Duration{0, 25 * time.Millisecond, 80 * time.Millisecond} {
		config := w.Resources.Config
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
		ctx := render.RenderContext{GameTime: tick.Add(since), MapWidth: config.MapWidth,
			MapHeight: config.MapHeight, ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}
		NewFlashRenderer(game).Render(ctx, buf)
		NewFadeoutRenderer(game).Render(ctx, buf)

		// A frame stands a tick past its tick at most
		opacity := 1 - float64(elapsed+min(since, 50*time.Millisecond))/float64(duration)
		flashed, ground := color.Scale(visual.RgbRemovalFlash, opacity), color.Blend(visual.RgbBackground, color.Scale(blue, opacity), opacity)
		for _, want := range []struct {
			x      int
			r      rune
			fg, bg color.RGB
		}{
			{3, 'f', flashed, color.RGB{}},
			{5, 'w', color.Blend(color.RGB{}, color.Scale(red, opacity), opacity), ground},
			{6, 'h', flashed, ground},
			{7, 'g', color.Scale(green, opacity), color.RGB{}},
		} {
			if got := buf.CellAt(want.x, 4); got.Rune != want.r || got.Fg != want.fg || got.Bg != want.bg {
				t.Fatalf("%v past the tick, (%d,4) drew %q fg %v bg %v, want %q fg %v bg %v",
					since, want.x, got.Rune, got.Fg, got.Bg, want.r, want.fg, want.bg)
			}
		}
	}

	// A deadline that ran out draws nothing while its owner has yet to end it
	timer, _ := w.Components.Timer.GetPtr(flash)
	timer.Slots[component.TimerDeadline].Remaining = 0
	config := w.Resources.Config
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	NewFlashRenderer(game).Render(render.RenderContext{GameTime: tick, MapWidth: config.MapWidth,
		MapHeight: config.MapHeight, ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}, buf)
	if got := buf.CellAt(3, 4); got.Rune != 0 {
		t.Fatalf("a flash past its deadline drew %q", got.Rune)
	}
}

// TestFlightsTravelBetweenTicks: a flight's parts are drawn as far along as its
// deadline stands at the frame's moment. A scattered part runs fast and slows,
// dimming in its own color, and passes under a glyph already drawn; a converging
// part sets off from its start cell alone and is on its target as the flight ends
func TestFlightsTravelBetweenTicks(t *testing.T) {
	w, game := glyphWorld()
	tick := engine.SimEpoch.Add(time.Second)
	w.Resources.Time.PaceTime, w.Resources.Time.DeltaTime = tick, 50*time.Millisecond
	red, green := color.RGB{R: 200}, color.RGB{G: 200}
	config := w.Resources.Config
	draw := func(since time.Duration) *render.RenderBuffer {
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
		buf.SetFgOnly(20, 4, 'G', green, terminal.AttrNone)
		NewSpiritRenderer(game).Render(render.RenderContext{GameTime: tick.Add(since), MapWidth: config.MapWidth,
			MapHeight: config.MapHeight, ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight,
			CellAspect: vmath.RefAspect}, buf)
		return buf
	}
	flight := func(c component.SpiritComponent, duration, elapsed time.Duration) {
		e := w.CreateEntity(core.DomainPlayer)
		w.Components.Spirit.SetComponent(e, c)
		var timer component.TimerComponent
		timer.Slots[component.TimerDeadline] = component.Countdown{Remaining: duration - elapsed, Duration: duration}
		w.Components.Timer.SetComponent(e, timer)
	}

	// A part thrown 8 cells right, a fifth through as the tick left it, and one
	// still over the cell it left, where a glyph stands
	const duration, elapsed = 500 * time.Millisecond, 100 * time.Millisecond
	flight(component.SpiritComponent{Pattern: component.SpiritScatter, Parts: []component.SpiritPart{
		{FromX: 2.5, FromY: 4.5, ToX: 10.5, ToY: 4.5, Rune: 'a', Color: red},
		{FromX: 20.5, FromY: 4.5, ToX: 21.5, ToY: 4.5, Rune: 'b', Color: red},
	}}, duration, elapsed)
	for _, since := range []time.Duration{0, 25 * time.Millisecond} {
		buf := draw(since)
		progress := float64(elapsed+since) / float64(duration)
		x := int(2.5 + 8*(1-(1-progress)*(1-progress)))
		if got := buf.CellAt(x, 4); got.Rune != 'a' || got.Fg != color.Scale(red, 1-progress*progress) {
			t.Fatalf("%v past the tick, (%d,4) drew %q fg %v, want the part there, dimmed by its progress", since, x, got.Rune, got.Fg)
		}
		if got := buf.CellAt(20, 4); got.Rune != 'G' || got.Fg != green {
			t.Fatalf("%v past the tick, the glyph a part stands over drew %q fg %v", since, got.Rune, got.Fg)
		}
	}
	w.DestroyEntity(w.Components.Spirit.Entities()[0])

	converging := component.SpiritComponent{Parts: []component.SpiritPart{{FromX: 3.5, FromY: 2.5, ToX: 30.5, ToY: 10.5, Rune: 'x'}}}
	flight(converging, duration, 0)
	drawn := func(buf *render.RenderBuffer) (cells []vmath.Point) {
		for y := range config.MapHeight {
			for x := range config.MapWidth {
				if buf.CellAt(x, y).Rune == 'x' {
					cells = append(cells, vmath.Point{X: x, Y: y})
				}
			}
		}
		return cells
	}
	if got := drawn(draw(0)); len(got) != 1 || got[0] != (vmath.Point{X: 3, Y: 2}) {
		t.Fatalf("a flight setting off drew %v, want its head alone on its start cell", got)
	}
	w.DestroyEntity(w.Components.Spirit.Entities()[0])
	flight(converging, duration, duration-time.Nanosecond)
	if got := draw(0).CellAt(30, 10); got.Rune != 'x' {
		t.Fatalf("a flight about to end drew %q on its target", got.Rune)
	}
}

// TestAnAblativeMemberDrawsUntilItsOwnerDestroysIt: a pylon's, a tower's and a
// snake body's member with no hit points left still draws, in the color its kind
// shows at none, so its cell stays lit until its fade takes it over
func TestAnAblativeMemberDrawsUntilItsOwnerDestroysIt(t *testing.T) {
	w, game := glyphWorld()
	config := w.Resources.Config
	ctx := render.RenderContext{MapWidth: config.MapWidth, MapHeight: config.MapHeight,
		ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight, CellAspect: vmath.RefAspect}
	// stand is a one-member composite at (x, 4), its member overkilled
	stand := func(x int) (core.Entity, *component.HeaderComponent, core.Entity) {
		header, member := w.CreateEntity(core.DomainShared), w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(header, component.PositionComponent{X: x, Y: 4})
		w.Positions.SetPosition(member, component.PositionComponent{X: x, Y: 4})
		w.Components.Combat.SetComponent(member, component.CombatComponent{HitPoints: -3})
		w.Components.Header.SetComponent(header, component.HeaderComponent{
			Type: component.CompositeTypeAblative, MemberEntries: []component.MemberEntry{{Entity: member}}})
		h, _ := w.Components.Header.GetPtr(header)
		return header, h, member
	}
	lit := func(name string, x int, want color.RGB, draw func(buf *render.RenderBuffer)) {
		t.Helper()
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
		draw(buf)
		if got := buf.CellAt(x, 4); got.Bg != want {
			t.Fatalf("%s member with no hit points left drew %v, want %v", name, got.Bg, want)
		}
	}

	_, pylonHeader, _ := stand(3)
	pylon := &component.PylonComponent{SpawnX: 3, SpawnY: 4, RadiusX: 2, RadiusY: 1, MinHP: 5, MaxHP: 10}
	lit("a pylon's", 3, visual.RgbPylonCriticalBright, func(buf *render.RenderBuffer) {
		NewPylonRenderer(game).renderMembersTrueColor(ctx, buf, pylon, pylonHeader)
	})

	_, towerHeader, _ := stand(8)
	tower := &component.TowerComponent{SpawnX: 8, SpawnY: 4, RadiusX: 2, RadiusY: 1, MinHP: 5, MaxHP: 10, Type: component.TowerGold}
	lit("a tower's", 8, visual.TowerTypes[component.TowerGold].CriticalBright, func(buf *render.RenderBuffer) {
		NewTowerRenderer(game).renderMembersTrueColor(ctx, buf, tower, towerHeader, int(component.TowerGold))
	})

	body, _, segment := stand(13)
	w.Components.SnakeBody.SetComponent(body, component.SnakeBodyComponent{Segments: []component.SnakeSegment{{Connected: true}}})
	w.Components.SnakeMember.SetComponent(segment, component.SnakeMemberComponent{MaxHitPoints: 10})
	lit("a snake body's", 13, color.Scale(visual.RgbSnakeBodyBright, 0.3), func(buf *render.RenderBuffer) {
		NewSnakeRenderer(game).renderBodyTrueColor(ctx, buf, body)
	})
}

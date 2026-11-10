package system

import (
	"cmp"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

const bodyX, bodyY = 20, 10

// compositeWorld is a bare true-color world whose composite system takes requests
// by hand
func compositeWorld() (*engine.World, *CompositeSystem) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	return w, ready(NewCompositeSystem(w)).(*CompositeSystem)
}

// standBody stands a composite of the kind with its header at (bodyX, bodyY): one
// member a cell of foot, row by row
func standBody(w *engine.World, kind component.Behavior, foot vmath.Footprint) core.Entity {
	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: bodyX, Y: bodyY})
	h := component.HeaderComponent{Behavior: kind}
	for row := range foot.Height {
		for col := range foot.Width {
			m := w.CreateEntity(core.DomainShared)
			offX, offY := col-foot.HeaderX, row-foot.HeaderY
			w.Positions.SetPosition(m, component.PositionComponent{X: bodyX + offX, Y: bodyY + offY})
			w.Components.Member.SetComponent(m, component.MemberComponent{HeaderEntity: header})
			h.MemberEntries = append(h.MemberEntries, component.MemberEntry{Entity: m, OffsetX: offX, OffsetY: offY})
		}
	}
	w.Components.Header.SetComponent(header, h)
	return header
}

// bodyEnd is what a composite's end sent: each effect's cells in reading order and
// the death its members died
type bodyEnd struct {
	cells  map[event.EventType][]component.EffectCell
	effect component.DeathEffect
	died   int
}

func endBody(t *testing.T, w *engine.World, s *CompositeSystem, header core.Entity, effect component.DeathEffect) bodyEnd {
	t.Helper()
	s.HandleEvent(event.GameEvent{Type: event.EventCompositeDestroyRequest,
		Payload: &event.CompositeDestroyRequestPayload{HeaderEntity: header, Effect: effect}})
	return sentBy(t, w)
}

// sentBy consumes the queue as a body's end, refusing an effect that crosses
func sentBy(t *testing.T, w *engine.World) bodyEnd {
	t.Helper()
	end := bodyEnd{cells: map[event.EventType][]component.EffectCell{}}
	for _, ev := range w.Resources.Event.Queue.Consume() {
		switch p := ev.Payload.(type) {
		case *event.BatchPayload[component.EffectCell]:
			if ev.Domain != core.DomainPlayer {
				t.Fatalf("effect event %d is stamped %v, want this instance's alone", ev.Type, ev.Domain)
			}
			cells := slices.Clone(p.Entries)
			slices.SortFunc(cells, func(a, b component.EffectCell) int {
				return cmp.Or(cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
			})
			end.cells[ev.Type] = append(end.cells[ev.Type], cells...)
		case *event.DeathRequestPayload:
			end.effect, end.died = p.Effect, end.died+len(p.Entities)
		}
	}
	return end
}

// glyphRows is a body's glyphs in color at the cells foot covers, a zero rune none
func glyphRows(foot vmath.Footprint, c color.RGB, glyph func(row, col int) rune) (cells []component.EffectCell) {
	for row := range foot.Height {
		for col := range foot.Width {
			if r := glyph(row, col); r != 0 {
				cells = append(cells, component.EffectCell{X: bodyX - foot.HeaderX + col, Y: bodyY - foot.HeaderY + row, Rune: r, Fg: c})
			}
		}
	}
	return cells
}

// TestAGlyphBodyLeavesItsGlyphsInItsColor: a swarm and a quasar leave the glyphs
// of the frame they showed, where their members stood, in the color they stood in,
// enraged or not and never the hit flash's; a scatter throws them, a flash lights
// them and a fadeout fades them, the members dying silently either way
func TestAGlyphBodyLeavesItsGlyphsInItsColor(t *testing.T) {
	swarmFoot, quasarFoot := component.Footprints[component.SpeciesSwarm], component.Footprints[component.SpeciesQuasar]
	for _, c := range []struct {
		name    string
		kind    component.Behavior
		foot    vmath.Footprint
		dress   func(w *engine.World, header core.Entity)
		effect  component.DeathEffect
		sent    event.EventType
		want    []component.EffectCell
		console bool
	}{
		{name: "a swarm scatters its second frame", kind: component.BehaviorSwarm, foot: swarmFoot,
			dress: func(w *engine.World, h core.Entity) {
				w.Components.Swarm.SetComponent(h, component.SwarmComponent{PatternIndex: 1})
				w.Components.Combat.SetComponent(h, component.CombatComponent{RemainingHitFlash: 1})
			},
			effect: component.DeathScatter, sent: event.EventSpiritScatterRequest,
			want: glyphRows(swarmFoot, visual.RgbDrain, func(row, col int) rune { return visual.SwarmPatternChars[1][row][col] })},
		{name: "an enraged swarm fades in the enraged color", kind: component.BehaviorSwarm, foot: swarmFoot,
			dress: func(w *engine.World, h core.Entity) {
				w.Components.Swarm.SetComponent(h, component.SwarmComponent{})
				w.Components.Combat.SetComponent(h, component.CombatComponent{IsEnraged: true})
			},
			effect: component.DeathFadeout, sent: event.EventFadeoutSpawnBatch,
			want: glyphRows(swarmFoot, visual.RgbCombatEnraged, func(row, col int) rune { return visual.SwarmPatternChars[0][row][col] })},
		{name: "a console's swarm scatters the console's frame", kind: component.BehaviorSwarm, foot: swarmFoot,
			dress: func(w *engine.World, h core.Entity) {
				w.Components.Swarm.SetComponent(h, component.SwarmComponent{PatternIndex: 1})
			},
			effect: component.DeathScatter, sent: event.EventSpiritScatterRequest, console: true,
			want: glyphRows(swarmFoot, visual.RgbDrain, func(row, col int) rune { return visual.SwarmPatternChars256[1][row][col] })},
		{name: "a quasar flashes", kind: component.BehaviorQuasar, foot: quasarFoot,
			dress:  func(*engine.World, core.Entity) {},
			effect: component.DeathFlash, sent: event.EventFlashSpawnBatchRequest,
			want: glyphRows(quasarFoot, visual.RgbDrain, func(row, col int) rune { return visual.QuasarChars[row][col] })},
	} {
		w, composites := compositeWorld()
		if c.console {
			w.Resources.Config.ColorMode = terminal.ColorMode256
		}
		header := standBody(w, c.kind, c.foot)
		c.dress(w, header)

		end := endBody(t, w, composites, header, c.effect)
		if got := end.cells[c.sent]; len(end.cells) != 1 || !slices.Equal(got, c.want) {
			t.Fatalf("%s: sent %+v, want event %d alone with %+v", c.name, end.cells, c.sent, c.want)
		}
		if end.effect != component.DeathSilent || end.died != c.foot.Width*c.foot.Height {
			t.Fatalf("%s: %d members died effect %d, want all %d silently", c.name, end.died, end.effect, c.foot.Width*c.foot.Height)
		}
		if w.Components.Header.HasEntity(header) {
			t.Fatalf("%s: the head outlived its composite", c.name)
		}
	}
}

// TestALookBodyLeavesItsGlyphsAndItsGrounds: an eye leaves the frame it showed, in
// its look's palettes or its console pair: a flash lights the glyphs as the grounds
// fade out under them, and a fadeout fades each cell whole
func TestALookBodyLeavesItsGlyphsAndItsGrounds(t *testing.T) {
	foot := component.Footprints[component.SpeciesEye]
	art, look := &visual.EyeTypeVisuals[0], &visual.EyeLooks[0]
	// The void eye's second frame: its pupil at the header, lit in entry 5 over
	// ground entry 1
	const frame = 1
	stand := func(console bool) (*engine.World, *CompositeSystem, core.Entity) {
		w, composites := compositeWorld()
		if console {
			w.Resources.Config.ColorMode = terminal.ColorMode256
		}
		header := standBody(w, component.BehaviorEye, foot)
		w.Components.Eye.SetComponent(header, component.EyeComponent{FrameIndex: frame + len(look.Frames)})
		w.Components.Combat.SetComponent(header, component.CombatComponent{IsEnraged: true})
		return w, composites, header
	}
	at := func(cells []component.EffectCell, x, y int) component.EffectCell {
		i := slices.IndexFunc(cells, func(c component.EffectCell) bool { return c.X == x && c.Y == y })
		if i < 0 {
			t.Fatalf("no cell at (%d, %d) of %+v", x, y, cells)
		}
		return cells[i]
	}
	all := foot.Width * foot.Height

	w, composites, header := stand(false)
	end := endBody(t, w, composites, header, component.DeathFlash)
	glyphs, grounds := end.cells[event.EventFlashSpawnBatchRequest], end.cells[event.EventFadeoutSpawnBatch]
	if len(glyphs) != all || len(grounds) != all || len(end.cells) != 2 {
		t.Fatalf("a flashing eye sent %+v, want %d glyphs to the flash and %d grounds to the fadeout", end.cells, all, all)
	}
	if got, want := at(glyphs, bodyX, bodyY), (component.EffectCell{X: bodyX, Y: bodyY, Rune: 'o', Fg: art.Fg[5]}); got != want {
		t.Fatalf("the pupil flashes as %+v, want %+v: its own entry, an eye never drawn enraged", got, want)
	}
	if got, want := at(grounds, bodyX, bodyY), (component.EffectCell{X: bodyX, Y: bodyY, Bg: art.Bg[1], Solid: true}); got != want {
		t.Fatalf("the pupil's ground fades as %+v, want %+v", got, want)
	}

	w, composites, header = stand(false)
	end = endBody(t, w, composites, header, component.DeathFadeout)
	cells := end.cells[event.EventFadeoutSpawnBatch]
	if len(cells) != all || len(end.cells) != 1 {
		t.Fatalf("a fading eye sent %+v, want its %d cells to the fadeout alone", end.cells, all)
	}
	if got, want := at(cells, bodyX, bodyY), (component.EffectCell{X: bodyX, Y: bodyY, Rune: 'o', Fg: art.Fg[5], Bg: art.Bg[1], Solid: true}); got != want {
		t.Fatalf("the pupil fades as %+v, want %+v", got, want)
	}

	w, composites, header = stand(true)
	end = endBody(t, w, composites, header, component.DeathFadeout)
	want := component.EffectCell{X: bodyX, Y: bodyY, Rune: 'o', Fg: visual.Palette256RGB(art.Fg256), Bg: visual.Palette256RGB(art.Bg256), Solid: true}
	if got := at(end.cells[event.EventFadeoutSpawnBatch], bodyX, bodyY); got != want {
		t.Fatalf("a console's pupil fades as %+v, want its console pair %+v", got, want)
	}
}

// TestADataSpeciesLeavesItsPrototypesLook: a data species leaves the frame it
// showed of its prototype's look over its own footprint, every glyph in the enraged
// color while its state holds that flag
func TestADataSpeciesLeavesItsPrototypesLook(t *testing.T) {
	frame := func(row, col int) rune { return []rune([]string{"▀▄▄▀", "▄▀▀▄"}[row])[col] }
	for _, c := range []struct {
		enraged bool
		want    color.RGB
	}{{false, color.RGB{R: 200, G: 120, B: 40}}, {true, visual.RgbCombatEnraged}} {
		ctx, _, header := newWardenWorld(t, wardenX+4, wardenY)
		w := ctx.World
		w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
		b, _ := w.Components.Behavior.GetPtr(header)
		b.PatternIndex = 3 // the second of its two frames
		combat, _ := w.Components.Combat.GetPtr(header)
		combat.IsEnraged = c.enraged

		end := endBody(t, w, ready(NewCompositeSystem(w)).(*CompositeSystem), header, component.DeathScatter)
		want := glyphRows(w.Resources.Behaviors[b.Program].Footprint, c.want, frame)
		if got := end.cells[event.EventSpiritScatterRequest]; len(end.cells) != 1 || !slices.Equal(got, want) {
			t.Fatalf("enraged %v: a warden scattered %+v, want %+v", c.enraged, end.cells, want)
		}
	}
}

// TestAnAblativeBodyFadesCellByCell: the cell of a member its owner found dead
// fades out where it stood, in the color its kind goes critical in, as its entry
// is tombstoned; the members left fade with the body's end, a snake's body behind
// its head; and a gold's member, a glyph the typing lit, leaves nothing here
func TestAnAblativeBodyFadesCellByCell(t *testing.T) {
	w, composites := compositeWorld()
	foot := vmath.Footprint{Width: 3, Height: 1, HeaderX: 1}
	ground := func(x int, c color.RGB) component.EffectCell {
		return component.EffectCell{X: x, Y: bodyY, Bg: c, Solid: true}
	}
	chip := func(header core.Entity, i int) bodyEnd {
		h, _ := w.Components.Header.GetPtr(header)
		member := h.MemberEntries[i].Entity
		composites.HandleEvent(event.GameEvent{Type: event.EventCompositeMemberDestroyed,
			Payload: &event.CompositeMemberDestroyedPayload{HeaderEntity: header, MemberEntity: member}})
		if h.MemberEntries[i].Entity != 0 || !h.Dirty || w.Components.Member.HasEntity(member) {
			t.Fatalf("member %d outlived its chip: entry %+v, dirty %v", i, h.MemberEntries[i], h.Dirty)
		}
		return sentBy(t, w)
	}
	ablative := func(kind component.Behavior) core.Entity {
		header := standBody(w, kind, foot)
		h, _ := w.Components.Header.GetPtr(header)
		h.Type = component.CompositeTypeAblative
		return header
	}

	pylon := ablative(component.BehaviorPylon)
	w.Components.Pylon.SetComponent(pylon, component.PylonComponent{})
	end := chip(pylon, 0)
	if got, want := end.cells[event.EventFadeoutSpawnBatch], []component.EffectCell{ground(bodyX-1, visual.RgbPylonCriticalBright)}; len(end.cells) != 1 || !slices.Equal(got, want) {
		t.Fatalf("a pylon's chip sent %+v, want the fadeout %+v", end.cells, want)
	}

	tower := ablative(component.BehaviorTower)
	w.Components.Tower.SetComponent(tower, component.TowerComponent{Type: component.TowerGold})
	end = chip(tower, 2)
	if got, want := end.cells[event.EventFadeoutSpawnBatch], []component.EffectCell{ground(bodyX+1, visual.TowerTypes[component.TowerGold].CriticalBright)}; !slices.Equal(got, want) {
		t.Fatalf("a gold tower's chip faded %+v, want %+v", got, want)
	}

	circle := ablative(component.BehaviorStorm)
	w.Components.StormCircle.SetComponent(circle, component.StormCircleComponent{Index: int(component.StormCircleBlue)})
	end = chip(circle, 1)
	if got, want := end.cells[event.EventFadeoutSpawnBatch], []component.EffectCell{ground(bodyX, visual.StormCircleColors[component.StormCircleBlue])}; !slices.Equal(got, want) {
		t.Fatalf("the blue circle's chip faded %+v, want %+v", got, want)
	}

	body := ablative(component.BehaviorSnake)
	w.Components.SnakeBody.SetComponent(body, component.SnakeBodyComponent{})
	chip(body, 1)
	end = endBody(t, w, composites, body, component.DeathFadeout)
	want := []component.EffectCell{ground(bodyX-1, visual.RgbSnakeBodyBright), ground(bodyX+1, visual.RgbSnakeBodyBright)}
	if got := end.cells[event.EventFadeoutSpawnBatch]; len(end.cells) != 1 || !slices.Equal(got, want) || end.effect != component.DeathSilent || end.died != 2 {
		t.Fatalf("a snake's body ended with %+v and %d deaths of effect %d, want its two cells left fading %+v", end.cells, end.died, end.effect, want)
	}

	gold := standBody(w, component.BehaviorGold, foot)
	if end = chip(gold, 0); len(end.cells) != 0 {
		t.Fatalf("a gold's typed member sent %+v, want nothing", end.cells)
	}
	// A gold's glyphs are its members' own, which the death system reads one by one
	if end = endBody(t, w, composites, gold, component.DeathFlash); len(end.cells) != 0 || end.effect != component.DeathFlash || end.died != 2 {
		t.Fatalf("a gold ended with %+v and %d deaths of effect %d, want its two members left dying the flash", end.cells, end.died, end.effect)
	}
}

// TestAKilledShellBurstsFromItsOutline: a pylon, a tower and a storm circle killed
// throw shards from the outline their glow stood on, in the glow's color and a
// console's in the color of its cells, as the cells left fade out; a body without
// a shell, and any other end, throws none
func TestAKilledShellBurstsFromItsOutline(t *testing.T) {
	const radiusX, radiusY = 6, 3
	stand := func(console bool, kind component.Behavior, foot vmath.Footprint) (*engine.World, *CompositeSystem, core.Entity) {
		w, composites := compositeWorld()
		if console {
			w.Resources.Config.ColorMode = terminal.ColorMode256
		}
		header := standBody(w, kind, foot)
		switch kind {
		case component.BehaviorPylon:
			w.Components.Pylon.SetComponent(header, component.PylonComponent{RadiusX: radiusX, RadiusY: radiusY})
		case component.BehaviorTower:
			w.Components.Tower.SetComponent(header, component.TowerComponent{RadiusX: radiusX, RadiusY: radiusY, Type: component.TowerViolet})
		case component.BehaviorStorm:
			w.Components.StormCircle.SetComponent(header, component.StormCircleComponent{Index: int(component.StormCircleRed)})
		case component.BehaviorSnake:
			w.Components.SnakeBody.SetComponent(header, component.SnakeBodyComponent{})
		}
		return w, composites, header
	}
	// ring checks that shards stand round the body's middle on an outline of the
	// radii, a spacing apart, in one color and the shade glyphs
	ring := func(name string, shards []component.EffectCell, rx, ry float64, c color.RGB, glyphs []rune) {
		t.Helper()
		if want := int(vmath.TwoPi * (rx + vmath.RefAspect.Circular(ry)) / 2 / visual.ShellShardSpacing); len(shards) != want || want == 0 {
			t.Fatalf("%s: %d shards, want %d", name, len(shards), want)
		}
		invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)
		left, right, top, bottom := bodyX, bodyX, bodyY, bodyY
		for _, shard := range shards {
			// A cell holding a point of the outline is within half a cell of it either way
			if d := vmath.EllipseDistSq(float64(shard.X-bodyX), float64(shard.Y-bodyY), invRxSq, invRySq); d < 0.6 || d > 1.5 {
				t.Fatalf("%s: shard %+v stands %.2f of the way out, want it on the outline", name, shard, d)
			}
			if shard.Fg != c || !slices.Contains(glyphs, shard.Rune) || shard.Solid {
				t.Fatalf("%s: shard %+v, want a glyph of %q in %v", name, shard, string(glyphs), c)
			}
			left, right, top, bottom = min(left, shard.X), max(right, shard.X), min(top, shard.Y), max(bottom, shard.Y)
		}
		// The scatter throws from the middle of what it is sent, which is the body's
		if left+right != 2*bodyX || top+bottom != 2*bodyY {
			t.Fatalf("%s: shards span (%d, %d) to (%d, %d), want them round (%d, %d)", name, left, top, right, bottom, bodyX, bodyY)
		}
	}

	w, composites, header := stand(false, component.BehaviorPylon, vmath.Footprint{})
	end := endBody(t, w, composites, header, component.DeathScatter)
	if len(end.cells) != 1 {
		t.Fatalf("a killed pylon sent %+v, want its burst alone", end.cells)
	}
	ring("pylon", end.cells[event.EventSpiritScatterRequest], radiusX, radiusY, visual.RgbPylonGlow, visual.DensityChars[:])

	w, composites, header = stand(false, component.BehaviorTower, vmath.Footprint{})
	end = endBody(t, w, composites, header, component.DeathScatter)
	ring("tower", end.cells[event.EventSpiritScatterRequest], radiusX, radiusY, visual.TowerTypes[component.TowerViolet].GlowColor, visual.DensityChars[:])

	// A circle ended with cells left: they fade out under its burst
	w, composites, header = stand(false, component.BehaviorStorm, vmath.Footprint{Width: 2, Height: 1})
	end = endBody(t, w, composites, header, component.DeathScatter)
	red := visual.StormCircleColors[component.StormCircleRed]
	ring("storm circle", end.cells[event.EventSpiritScatterRequest], parameter.StormCircleRadiusX, parameter.StormCircleRadiusY, red, visual.DensityChars[:])
	if want := []component.EffectCell{{X: bodyX, Y: bodyY, Bg: red, Solid: true}, {X: bodyX + 1, Y: bodyY, Bg: red, Solid: true}}; !slices.Equal(end.cells[event.EventFadeoutSpawnBatch], want) || end.died != 2 || end.effect != component.DeathSilent {
		t.Fatalf("a circle's cells left ended as %+v with %d deaths of effect %d, want %+v fading and both silent", end.cells[event.EventFadeoutSpawnBatch], end.died, end.effect, want)
	}

	w, composites, header = stand(true, component.BehaviorPylon, vmath.Footprint{})
	end = endBody(t, w, composites, header, component.DeathScatter)
	ring("a console's pylon", end.cells[event.EventSpiritScatterRequest], radiusX, radiusY, visual.Palette256RGB(visual.Pylon256Critical), visual.Density256Chars[:])

	for _, c := range []struct {
		name   string
		kind   component.Behavior
		effect component.DeathEffect
	}{
		{"a cancelled pylon", component.BehaviorPylon, component.DeathSilent},
		{"a fading pylon", component.BehaviorPylon, component.DeathFadeout},
		{"a flashing pylon", component.BehaviorPylon, component.DeathFlash},
		{"a snake's body", component.BehaviorSnake, component.DeathScatter},
	} {
		w, composites, header = stand(false, c.kind, vmath.Footprint{})
		if end = endBody(t, w, composites, header, c.effect); len(end.cells) != 0 {
			t.Fatalf("%s sent %+v, want nothing", c.name, end.cells)
		}
	}
}

// endsAsked steps the system a tick and returns the composite ends it asked for and
// the kills it published
func endsAsked(w *engine.World, s engine.System) (ends []event.CompositeDestroyRequestPayload, kills int) {
	w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
		engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
	s.Update()
	return endsIn(w)
}

func endsIn(w *engine.World) (ends []event.CompositeDestroyRequestPayload, kills int) {
	for _, ev := range w.Resources.Event.Queue.Consume() {
		switch p := ev.Payload.(type) {
		case *event.CompositeDestroyRequestPayload:
			ends = append(ends, *p)
		case *event.SpeciesKilledPayload:
			kills++
		}
	}
	return ends, kills
}

// fell takes the last hit point of every member an ablative composite has
func fell(w *engine.World, header core.Entity) {
	h, _ := w.Components.Header.GetPtr(header)
	for _, m := range h.MemberEntries {
		combat, _ := w.Components.Combat.GetPtr(m.Entity)
		combat.HitPoints = 0
	}
}

// TestEachSpeciesEndLeavesItsEffect: a kill asks the composite's end with the
// species' kill effect, an expiry with the fadeout and a cancel with none
func TestEachSpeciesEndLeavesItsEffect(t *testing.T) {
	type world struct {
		w      *engine.World
		s      engine.System
		header core.Entity
	}
	stand := func(build func(*engine.World) engine.System, request event.GameEvent, headers func(*engine.World) []core.Entity) func(t *testing.T) world {
		return func(t *testing.T) world {
			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
			s := ready(build(w))
			s.(event.Handler).HandleEvent(request)
			if len(headers(w)) != 1 {
				t.Fatalf("%d bodies stood, want one", len(headers(w)))
			}
			w.Resources.Event.Queue.Consume()
			return world{w, s, headers(w)[0]}
		}
	}
	warden := func(death string) func(t *testing.T) world {
		return func(t *testing.T) world {
			ctx, s, header := newProgramWorld(t, strings.Replace(wardenProgram, "[prototype]\n", "[prototype]\n"+death, 1), wardenX+4, wardenY)
			return world{ctx.World, s, header}
		}
	}
	one := func(header core.Entity, effect component.DeathEffect) []event.CompositeDestroyRequestPayload {
		return []event.CompositeDestroyRequestPayload{{HeaderEntity: header, Effect: effect}}
	}

	for _, c := range []struct {
		name   string
		stand  func(t *testing.T) world
		cancel event.GameEvent
		kill   component.DeathEffect
		expiry bool
		// latched is a species that holds its own end, whatever becomes of the request
		latched bool
		// chipped is an ablative kind, killed as its last member is found dead
		chipped bool
	}{
		{name: "swarm", kill: component.DeathScatter, expiry: true,
			stand: stand(NewSwarmSystem, event.GameEvent{Type: event.EventSwarmSpawnRequest, Payload: &event.SwarmSpawnRequestPayload{X: 40, Y: 20}},
				func(w *engine.World) []core.Entity { return w.Components.Swarm.Entities() }),
			cancel: event.GameEvent{Type: event.EventSwarmCancelRequest}},
		{name: "quasar", kill: component.DeathScatter,
			stand: stand(NewQuasarSystem, event.GameEvent{Type: event.EventQuasarSpawnRequest, Payload: &event.QuasarSpawnRequestPayload{X: 40, Y: 20}},
				func(w *engine.World) []core.Entity { return w.Components.Quasar.Entities() }),
			cancel: event.GameEvent{Type: event.EventQuasarCancelRequest}},
		{name: "eye", kill: component.DeathFlash, latched: true,
			stand: stand(NewEyeSystem, event.GameEvent{Type: event.EventEyeSpawnRequest, Payload: &event.EyeSpawnRequestPayload{X: 40, Y: 20, RouteID: -1}},
				func(w *engine.World) []core.Entity { return w.Components.Eye.Entities() }),
			cancel: event.GameEvent{Type: event.EventEyeCancelRequest}},
		{name: "pylon", kill: component.DeathScatter, chipped: true,
			stand: stand(NewPylonSystem, event.GameEvent{Type: event.EventPylonSpawnRequest, Payload: &event.PylonSpawnRequestPayload{X: 40, Y: 20}},
				func(w *engine.World) []core.Entity { return w.Components.Pylon.Entities() }),
			cancel: event.GameEvent{Type: event.EventPylonCancelRequest}},
		{name: "tower", kill: component.DeathScatter, chipped: true,
			stand: stand(NewTowerSystem, event.GameEvent{Type: event.EventTowerSpawnRequest, Payload: &event.TowerSpawnRequestPayload{X: 40, Y: 20}},
				func(w *engine.World) []core.Entity { return w.Components.Tower.Entities() }),
			cancel: event.GameEvent{Type: event.EventTowerCancelRequest}},
		{name: "a data species naming no death", kill: component.DeathScatter, expiry: true, stand: warden(""),
			cancel: event.GameEvent{Type: event.EventBehaviorCancelRequest, Payload: &event.BehaviorCancelRequestPayload{}}},
		{name: "a data species naming the flash", kill: component.DeathFlash, expiry: true, stand: warden("death = \"flash\"\n"),
			cancel: event.GameEvent{Type: event.EventBehaviorCancelRequest, Payload: &event.BehaviorCancelRequestPayload{}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := c.stand(t)
			if c.chipped {
				fell(b.w, b.header)
			} else {
				combat, _ := b.w.Components.Combat.GetPtr(b.header)
				combat.HitPoints = 0
			}
			if ends, kills := endsAsked(b.w, b.s); kills != 1 || !slices.Equal(ends, one(b.header, c.kill)) {
				t.Fatalf("a kill published %d kills and asked %+v, want one and %+v", kills, ends, one(b.header, c.kill))
			}
			// The composite goes at the tick's settle: until then it is asked no more
			if ends, kills := endsAsked(b.w, b.s); c.latched && (kills != 0 || len(ends) != 0) {
				t.Fatalf("a dying %s published %d kills and asked %+v again", c.name, kills, ends)
			}

			b = c.stand(t)
			b.s.(event.Handler).HandleEvent(c.cancel)
			if ends, kills := endsIn(b.w); kills != 0 || !slices.Equal(ends, one(b.header, component.DeathSilent)) {
				t.Fatalf("a cancel published %d kills and asked %+v, want %+v", kills, ends, one(b.header, component.DeathSilent))
			}

			if !c.expiry {
				return
			}
			b = c.stand(t)
			timers := ready(NewTimerSystem(b.w))
			b.w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
			armTimer(b.w, b.header, component.TimerDeadline, parameter.GameUpdateInterval)
			timers.Update()
			if ends, kills := endsAsked(b.w, b.s); kills != 0 || !slices.Equal(ends, one(b.header, component.DeathFadeout)) {
				t.Fatalf("an expiry published %d kills and asked %+v, want %+v", kills, ends, one(b.header, component.DeathFadeout))
			}
		})
	}
}

// TestAKilledSnakesHeadLeavesItsEffectAsItsBodyFades: the head leaves the snake's
// kill effect, the body fades out behind it and the root goes silently; a cancel
// leaves nothing of either
func TestAKilledSnakesHeadLeavesItsEffectAsItsBodyFades(t *testing.T) {
	stand := func() (*engine.World, *SnakeSystem, *component.SnakeComponent, core.Entity) {
		w := engine.NewWorld()
		engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
		s := ready(NewSnakeSystem(w)).(*SnakeSystem)
		s.HandleEvent(event.GameEvent{Type: event.EventSnakeSpawnRequest, Payload: &event.SnakeSpawnRequestPayload{X: 40, Y: 20}})
		w.Resources.Event.Queue.Consume()
		root := w.Components.Snake.Entities()[0]
		snake, _ := w.Components.Snake.GetPtr(root)
		if snake.HeadEntity == 0 || snake.BodyEntity == 0 {
			t.Fatalf("the snake stood as %+v, want a head and a body", snake)
		}
		return w, s, snake, root
	}
	asked := func(snake *component.SnakeComponent, root core.Entity, head, body component.DeathEffect) []event.CompositeDestroyRequestPayload {
		return []event.CompositeDestroyRequestPayload{{HeaderEntity: snake.BodyEntity, Effect: body},
			{HeaderEntity: snake.HeadEntity, Effect: head}, {HeaderEntity: root}}
	}

	w, s, snake, root := stand()
	snake.IsShielded = false
	combat, _ := w.Components.Combat.GetPtr(snake.HeadEntity)
	combat.HitPoints = 0
	want := asked(snake, root, component.KillEffects[component.SpeciesSnake], component.DeathFadeout)
	if ends, kills := endsAsked(w, s); kills != 1 || !slices.Equal(ends, want) {
		t.Fatalf("a killed snake published %d kills and asked %+v, want one and %+v", kills, ends, want)
	}

	w, s, snake, root = stand()
	s.HandleEvent(event.GameEvent{Type: event.EventSnakeCancelRequest})
	want = asked(snake, root, component.DeathSilent, component.DeathSilent)
	if ends, kills := endsIn(w); kills != 0 || !slices.Equal(ends, want) {
		t.Fatalf("a cancelled snake published %d kills and asked %+v, want %+v", kills, ends, want)
	}
}

// TestAKilledStormCircleLeavesItsEffect: a circle whose last member is found dead
// ends with the storm's kill effect, the storm killed with its last circle; a
// cancel leaves nothing of any
func TestAKilledStormCircleLeavesItsEffect(t *testing.T) {
	stand := func() (*engine.World, *StormSystem, [component.StormCircleCount]core.Entity) {
		w := engine.NewWorld()
		engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
		s := ready(NewStormSystem(w)).(*StormSystem)
		s.HandleEvent(event.GameEvent{Type: event.EventStormSpawnRequest})
		w.Resources.Event.Queue.Consume()
		storm, ok := w.Components.Storm.GetPtr(s.rootEntity)
		if !ok {
			t.Fatal("no storm stood")
		}
		return w, s, storm.Circles
	}
	kill := component.KillEffects[component.SpeciesStorm]

	w, s, circles := stand()
	fell(w, circles[component.StormCircleRed])
	want := []event.CompositeDestroyRequestPayload{{HeaderEntity: circles[component.StormCircleRed], Effect: kill}}
	if ends, kills := endsAsked(w, s); kills != 0 || !slices.Equal(ends, want) {
		t.Fatalf("a killed circle published %d kills and asked %+v, want none and %+v", kills, ends, want)
	}
	fell(w, circles[component.StormCircleGreen])
	fell(w, circles[component.StormCircleBlue])
	want = []event.CompositeDestroyRequestPayload{{HeaderEntity: circles[component.StormCircleGreen], Effect: kill},
		{HeaderEntity: circles[component.StormCircleBlue], Effect: kill}}
	if ends, kills := endsAsked(w, s); kills != 1 || !slices.Equal(ends, want) {
		t.Fatalf("the last circles published %d kills and asked %+v, want one and %+v", kills, ends, want)
	}

	w, s, _ = stand()
	s.HandleEvent(event.GameEvent{Type: event.EventStormCancelRequest})
	ends, kills := endsIn(w)
	if kills != 0 || len(ends) != component.StormCircleCount+1 || slices.ContainsFunc(ends, func(p event.CompositeDestroyRequestPayload) bool { return p.Effect != component.DeathSilent }) {
		t.Fatalf("a cancelled storm published %d kills and asked %+v, want its circles and its root ended silently", kills, ends)
	}
}

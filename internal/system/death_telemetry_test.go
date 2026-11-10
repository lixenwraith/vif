package system

import (
	"slices"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter/visual"
)

func TestDeathTelemetrySeparatesEffectTypes(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	deaths := ready(NewDeathSystem(w)).(*DeathSystem)

	spawn := func(x int) core.Entity {
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: 3})
		w.Components.Sigil.SetComponent(e, component.SigilComponent{Rune: 'x'})
		return e
	}
	flashEntity := spawn(2)
	silentEntity := spawn(3)
	batchEntity := spawn(4)
	protectedEntity := spawn(5)
	w.Components.Protection.SetComponent(protectedEntity, component.ProtectionComponent{Mask: component.ProtectFromDeath})

	handle := func(effect component.DeathEffect, entities ...core.Entity) {
		p := event.AcquireDeathRequest(effect)
		p.Entities = append(p.Entities, entities...)
		deaths.HandleEvent(event.GameEvent{Type: event.EventDeathBatch, Payload: p})
	}
	handle(component.DeathFlash, flashEntity)
	handle(component.DeathSilent, silentEntity, batchEntity, protectedEntity)

	reg := w.Resources.Status
	for key, want := range map[string]int64{
		"death.batch_count":          2,
		"death.batch_entities_total": 4,
		"death.batch_size_max":       3,
		"death.batch_silent":         1,
		"death.batch_flash":          1,
		"death.protected_rejects":    1,
		"death.killed":               3,
	} {
		if got := reg.Ints.Get(key).Load(); got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
}

func TestDeathBatchesParticleEffectWithBehavior(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	deaths := ready(NewDeathSystem(w)).(*DeathSystem)

	entity := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(entity, component.PositionComponent{X: 7, Y: 8})
	w.Components.Glyph.SetComponent(entity, component.GlyphComponent{Rune: 'x'})

	p := event.AcquireDeathRequest(component.DeathParticle)
	p.Behavior = component.ParticleBlossom
	p.Entities = append(p.Entities, entity)
	deaths.HandleEvent(event.GameEvent{Type: event.EventDeathBatch, Payload: p})

	if _, ok := w.Components.Glyph.GetComponent(entity); ok {
		t.Fatal("source entity survived particle-effect death")
	}
	events := w.Resources.Event.Queue.Consume()
	if len(events) != 1 || events[0].Type != event.EventParticleSpawnBatch {
		t.Fatalf("effect events = %#v, want one EventParticleSpawnBatch", events)
	}
	batch, ok := events[0].Payload.(*event.BatchPayload[event.ParticleSpawnEntry])
	if !ok {
		t.Fatalf("effect payload = %T, want particle batch", events[0].Payload)
	}
	defer event.ParticleBatchPool.Release(batch)
	if len(batch.Entries) != 1 {
		t.Fatalf("particle entries = %d, want 1", len(batch.Entries))
	}
	entry := batch.Entries[0]
	if entry.Behavior != component.ParticleBlossom || entry.X != 7 || entry.Y != 8 || entry.Char != 'x' || !entry.SkipStartCell {
		t.Fatalf("particle entry = %+v", entry)
	}
	if got := w.Resources.Status.Ints.Get("death.batch_blossom").Load(); got != 1 {
		t.Fatalf("death.batch_blossom = %d, want 1", got)
	}
}

// TestWallsLeaveTheCellsTheyDrew: a wall dying with the flash lights the glyph it
// drew alone, and one dying with the fadeout fades what it drew: its background,
// under its glyph where it drew one, in the colors a palette wall names by index
func TestWallsLeaveTheCellsTheyDrew(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	deaths := ready(NewDeathSystem(w)).(*DeathSystem)
	wall := func(x int, c component.WallComponent) core.Entity {
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: 3})
		w.Components.Wall.SetComponent(e, c)
		return e
	}
	red, blue := color.RGB{R: 200}, color.RGB{B: 90}
	kill := func(effect component.DeathEffect, batch event.EventType, walls ...core.Entity) []component.EffectCell {
		p := event.AcquireDeathRequest(effect)
		p.Entities = append(p.Entities, walls...)
		deaths.HandleEvent(event.GameEvent{Type: event.EventDeathBatch, Payload: p})
		events := w.Resources.Event.Queue.Consume()
		if len(events) != 1 || events[0].Type != batch {
			t.Fatalf("effect %d sent %+v, want one batch", effect, events)
		}
		return events[0].Payload.(*event.BatchPayload[component.EffectCell]).Entries
	}

	got := kill(component.DeathFlash, event.EventFlashSpawnBatchRequest,
		wall(2, component.WallComponent{Rune: '#', FgColor: red, RenderFg: true}))
	if want := []component.EffectCell{{X: 2, Y: 3, Rune: '#'}}; !slices.Equal(got, want) {
		t.Fatalf("a glyph wall flashed %+v, want %+v", got, want)
	}

	got = kill(component.DeathFadeout, event.EventFadeoutSpawnBatch,
		wall(4, component.WallComponent{Rune: '#', FgColor: red, BgColor: blue, RenderFg: true, RenderBg: true}),
		wall(5, component.WallComponent{Rune: '#', BgColor: blue, RenderBg: true}),
		wall(6, component.WallComponent{BgColor: color.RGB{R: 21}, RenderBg: true, Attrs: terminal.AttrBg256}))
	want := []component.EffectCell{
		{X: 4, Y: 3, Rune: '#', Fg: red, Bg: blue, Solid: true},
		{X: 5, Y: 3, Bg: blue, Solid: true},
		{X: 6, Y: 3, Bg: visual.Palette256RGB(21), Solid: true},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("walls faded %+v, want %+v", got, want)
	}
	if n := w.Components.Wall.CountEntities(); n != 0 {
		t.Fatalf("%d walls outlived their death", n)
	}
}

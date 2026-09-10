package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

func TestDeathTelemetrySeparatesEffectTypes(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	deaths := NewDeathSystem(w).(*DeathSystem)

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

	handle := func(effect event.EventType, entities ...core.Entity) {
		p := event.AcquireDeathRequest(effect)
		p.Entities = append(p.Entities, entities...)
		deaths.HandleEvent(event.GameEvent{Type: event.EventDeathBatch, Payload: p})
	}
	handle(event.EventFlashSpawnOneRequest, flashEntity)
	handle(event.EventNone, silentEntity, batchEntity, protectedEntity)

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
	deaths := NewDeathSystem(w).(*DeathSystem)

	entity := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(entity, component.PositionComponent{X: 7, Y: 8})
	w.Components.Glyph.SetComponent(entity, component.GlyphComponent{Rune: 'x'})

	p := event.AcquireDeathRequest(event.EventParticleSpawnOne)
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

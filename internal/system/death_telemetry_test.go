package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

func TestDeathTelemetrySeparatesPackedFallbackAndBatchPaths(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	deaths := NewDeathSystem(w).(*DeathSystem)

	spawn := func(x int) core.Entity {
		e := w.CreateEntity()
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: 3})
		w.Components.Sigil.SetComponent(e, component.SigilComponent{Rune: 'x'})
		return e
	}
	packedEntity := spawn(2)
	fallbackEntity := spawn(3)
	batchEntity := spawn(4)
	protectedEntity := spawn(5)
	w.Components.Protection.SetComponent(protectedEntity, component.ProtectionComponent{Mask: component.ProtectFromDeath})

	packed := (uint64(event.EventFlashSpawnOneRequest) << 48) | uint64(packedEntity)
	deaths.HandleEvent(event.GameEvent{Type: event.EventDeathOne, Payload: packed})
	deaths.HandleEvent(event.GameEvent{Type: event.EventDeathOne, Payload: fallbackEntity})
	p := event.AcquireDeathRequest(0)
	p.Entities = append(p.Entities, batchEntity, protectedEntity)
	deaths.HandleEvent(event.GameEvent{Type: event.EventDeathBatch, Payload: p})

	reg := w.Resources.Status
	for key, want := range map[string]int64{
		"death.one_packed":           1,
		"death.one_fallback":         1,
		"death.batch_count":          1,
		"death.batch_entities_total": 2,
		"death.batch_silent":         1,
		"death.protected_rejects":    1,
		"death.killed":               3,
	} {
		if got := reg.Ints.Get(key).Load(); got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
}

package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// LightningSystem manages lightning visual effect lifecycle
// Supports both timed (auto-despawnLightning) and tracked (manual despawnLightning) modes
type LightningSystem struct {
	world *engine.World
	res   engine.Resources

	lightningStore *engine.Store[component.LightningComponent]

	enabled bool
}

func NewLightningSystem(world *engine.World) engine.System {
	s := &LightningSystem{
		world: world,
		res:   engine.GetResources(world),

		lightningStore: engine.GetStore[component.LightningComponent](world),
	}
	s.initLocked()
	return s
}

func (s *LightningSystem) Init() {
	s.initLocked()
}

func (s *LightningSystem) initLocked() {
	s.enabled = true
}

func (s *LightningSystem) Priority() int {
	// After quasar, before render
	return constant.PriorityLightning
}

func (s *LightningSystem) Update() {
	if !s.enabled {
		return
	}

	entities := s.lightningStore.All()
	if len(entities) == 0 {
		return
	}

	deltaTime := s.res.Time.DeltaTime
	var toDestroy []core.Entity

	for _, e := range entities {
		lc, ok := s.lightningStore.Get(e)
		if !ok {
			continue
		}

		// Duration == 0 means tracked mode (manual despawnLightning)
		if lc.Duration == 0 {
			continue
		}

		// Decrement remaining time
		lc.Remaining -= deltaTime
		if lc.Remaining <= 0 {
			toDestroy = append(toDestroy, e)
		} else {
			s.lightningStore.Set(e, lc)
		}
	}

	for _, e := range toDestroy {
		s.lightningStore.Remove(e)
		s.world.DestroyEntity(e)
	}
}

func (s *LightningSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventLightningSpawn,
		event.EventLightningUpdate,
		event.EventLightningDespawn,
		event.EventGameReset,
	}
}

func (s *LightningSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.destroyAll()
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventLightningSpawn:
		if p, ok := ev.Payload.(*event.LightningSpawnPayload); ok {
			s.spawnLightning(p)
		}

	case event.EventLightningUpdate:
		if p, ok := ev.Payload.(*event.LightningUpdatePayload); ok {
			s.updateTarget(p)
		}

	case event.EventLightningDespawn:
		if owner, ok := ev.Payload.(core.Entity); ok {
			s.despawnLightning(owner)
		}
	}
}

func (s *LightningSystem) spawnLightning(p *event.LightningSpawnPayload) {
	e := s.world.CreateEntity()

	lc := component.LightningComponent{
		Owner:     p.Owner,
		OriginX:   p.OriginX,
		OriginY:   p.OriginY,
		TargetX:   p.TargetX,
		TargetY:   p.TargetY,
		ColorType: p.ColorType,
		Duration:  p.Duration,
		Remaining: p.Duration,
	}

	// TODO: track properly
	// Tracked mode: Duration=0 signals manual lifecycle
	if p.Tracked {
		lc.Duration = 0
		lc.Remaining = time.Hour // Effectively infinite for renderer check
	}

	s.lightningStore.Set(e, lc)
}

func (s *LightningSystem) updateTarget(p *event.LightningUpdatePayload) {
	// Find lightning by owner
	for _, e := range s.lightningStore.All() {
		lc, ok := s.lightningStore.Get(e)
		if !ok || lc.Owner != p.Owner {
			continue
		}
		lc.TargetX = p.TargetX
		lc.TargetY = p.TargetY
		s.lightningStore.Set(e, lc)
		return
	}
}

func (s *LightningSystem) despawnLightning(owner core.Entity) {
	// Find and destroy all lightning owned by this entity
	for _, e := range s.lightningStore.All() {
		lc, ok := s.lightningStore.Get(e)
		if !ok || lc.Owner != owner {
			continue
		}
		s.lightningStore.Remove(e)
		s.world.DestroyEntity(e)
	}
}

func (s *LightningSystem) destroyAll() {
	entities := s.lightningStore.All()
	for _, e := range entities {
		s.lightningStore.Remove(e)
		s.world.DestroyEntity(e)
	}
}
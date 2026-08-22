package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// LightningSystem manages lightning visual effect lifecycle
// Supports both timed (auto-despawnLightning) and tracked (manual despawnLightning) modes
type LightningSystem struct {
	world *engine.World

	rng *vmath.FastRand // Seed generation for new lightnings

	enabled bool
}

func NewLightningSystem(world *engine.World) engine.System {
	s := &LightningSystem{
		world: world,
	}
	s.Init()
	return s
}

func (s *LightningSystem) Init() {
	s.rng = s.world.Rand(s.Name())
	s.enabled = true
}

// Name returns system's name
func (s *LightningSystem) Name() string {
	return "lightning"
}

func (s *LightningSystem) Priority() int {
	// After quasar, before render
	return parameter.PriorityLightning
}

func (s *LightningSystem) Update() {
	if !s.enabled {
		return
	}

	lightnings := s.world.Components.Lightning
	if lightnings.CountEntities() == 0 {
		return
	}

	deltaTime := s.world.Resources.Time.DeltaTime
	var toDestroy []core.Entity

	for _, e := range lightnings.Entities() {
		lc, ok := lightnings.GetPtr(e)
		if !ok {
			continue
		}

		// Advance animation frame for tracked mode (dancing effect)
		if lc.Duration == 0 {
			lc.AnimFrame++
			continue // Tracked mode: no duration decrement
		}

		// Non-tracked: decrement remaining time
		lc.Remaining -= deltaTime
		if lc.Remaining <= 0 {
			toDestroy = append(toDestroy, e)
		}
	}

	s.world.DestroyEntitiesBatch(toDestroy)
}

func (s *LightningSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventLightningSpawnRequest,
		event.EventLightningUpdateRequest,
		event.EventLightningDespawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *LightningSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.destroyAll()
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventLightningSpawnRequest:
		if p, ok := ev.Payload.(*event.LightningSpawnRequestPayload); ok {
			s.spawnLightning(p)
		}

	case event.EventLightningUpdateRequest:
		if p, ok := ev.Payload.(*event.LightningUpdateRequestPayload); ok {
			s.updateTarget(p)
		}

	case event.EventLightningDespawnRequest:
		if p, ok := ev.Payload.(*event.LightningDespawnRequestPayload); ok {
			s.despawnLightning(p.Owner, p.TargetEntity)
		}
	}
}

func (s *LightningSystem) spawnLightning(p *event.LightningSpawnRequestPayload) {
	e := s.world.CreateEntity()

	// Generate seed if not provided
	pathSeed := p.PathSeed
	if pathSeed == 0 {
		pathSeed = s.rng.Next()
	}

	lc := component.LightningComponent{
		Owner:        p.Owner,
		OriginX:      p.OriginX,
		OriginY:      p.OriginY,
		TargetX:      p.TargetX,
		TargetY:      p.TargetY,
		OriginEntity: p.OriginEntity,
		TargetEntity: p.TargetEntity,
		ColorType:    p.ColorType,
		PathSeed:     pathSeed,
		AnimFrame:    0,
		Duration:     p.Duration,
		Remaining:    p.Duration,
	}

	// Tracked mode: Duration=0 signals manual lifecycle
	if p.Tracked {
		lc.Duration = 0
		lc.Remaining = time.Hour // Effectively infinite for renderer check
	}

	s.world.Components.Lightning.SetComponent(e, lc)
}

func (s *LightningSystem) updateTarget(p *event.LightningUpdateRequestPayload) {
	// Find lightning by owner
	lightnings := s.world.Components.Lightning
	for _, e := range lightnings.Entities() {
		lc, ok := lightnings.GetPtr(e)
		if !ok || lc.Owner != p.Owner {
			continue
		}
		lc.TargetX = p.TargetX
		lc.TargetY = p.TargetY
		return
	}
}

// despawnLightning removes lightning matching criteria
// target=0 removes all lightning from owner, otherwise only matching target
func (s *LightningSystem) despawnLightning(owner, target core.Entity) {
	lightnings := s.world.Components.Lightning
	var toDestroy []core.Entity
	for _, lightningEntity := range lightnings.Entities() {
		lightningComp, ok := lightnings.GetPtr(lightningEntity)
		if !ok || lightningComp.Owner != owner {
			continue
		}
		if target != 0 && lightningComp.TargetEntity != target {
			continue
		}
		toDestroy = append(toDestroy, lightningEntity)
	}
	s.world.DestroyEntitiesBatch(toDestroy)
}

func (s *LightningSystem) destroyAll() {
	// Batch destruction mutates the live lightning slice, so detach it first.
	entities := s.world.Components.Lightning.GetAllEntities()
	s.world.DestroyEntitiesBatch(entities)
}

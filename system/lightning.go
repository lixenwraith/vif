package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
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
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
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

	entities := s.world.Components.Lightning.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	deltaTime := s.world.Resources.Time.DeltaTime
	var toDestroy []core.Entity

	for _, e := range entities {
		lc, ok := s.world.Components.Lightning.GetComponent(e)
		if !ok {
			continue
		}

		// Advance animation frame for tracked mode (dancing effect)
		if lc.Duration == 0 {
			lc.AnimFrame++
			s.world.Components.Lightning.SetComponent(e, lc)
			continue // Tracked mode: no duration decrement
		}

		// Non-tracked: decrement remaining time
		lc.Remaining -= deltaTime
		if lc.Remaining <= 0 {
			toDestroy = append(toDestroy, e)
		} else {
			s.world.Components.Lightning.SetComponent(e, lc)
		}
	}

	for _, e := range toDestroy {
		s.world.Components.Lightning.RemoveEntity(e)
		s.world.DestroyEntity(e)
	}
}

func (s *LightningSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventLightningSpawnRequest,
		event.EventLightningUpdate,
		event.EventLightningDespawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *LightningSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
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

	case event.EventLightningUpdate:
		if p, ok := ev.Payload.(*event.LightningUpdatePayload); ok {
			s.updateTarget(p)
		}

	case event.EventLightningDespawnRequest:
		if p, ok := ev.Payload.(*event.LightningDespawnPayload); ok {
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

func (s *LightningSystem) updateTarget(p *event.LightningUpdatePayload) {
	// Find lightning by owner
	for _, e := range s.world.Components.Lightning.GetAllEntities() {
		lc, ok := s.world.Components.Lightning.GetComponent(e)
		if !ok || lc.Owner != p.Owner {
			continue
		}
		lc.TargetX = p.TargetX
		lc.TargetY = p.TargetY
		s.world.Components.Lightning.SetComponent(e, lc)
		return
	}
}

// despawnLightning removes lightning matching criteria
// target=0 removes all lightning from owner, otherwise only matching target
func (s *LightningSystem) despawnLightning(owner, target core.Entity) {
	for _, lightningEntity := range s.world.Components.Lightning.GetAllEntities() {
		lightningComp, ok := s.world.Components.Lightning.GetComponent(lightningEntity)
		if !ok || lightningComp.Owner != owner {
			continue
		}
		if target != 0 && lightningComp.TargetEntity != target {
			continue
		}
		s.world.Components.Lightning.RemoveEntity(lightningEntity)
		s.world.DestroyEntity(lightningEntity)
	}
}

func (s *LightningSystem) destroyAll() {
	entities := s.world.Components.Lightning.GetAllEntities()
	for _, e := range entities {
		s.world.Components.Lightning.RemoveEntity(e)
		s.world.DestroyEntity(e)
	}
}
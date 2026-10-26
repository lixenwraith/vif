package system

import (
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// LightningSystem manages lightning visual effect lifecycle
// Supports both timed (auto-despawnLightning) and tracked (manual despawnLightning) modes
type LightningSystem struct {
	world *engine.World

	rng *vmath.FastRand // Seed generation for new lightnings

	toggle
}

func NewLightningSystem(world *engine.World) engine.System {
	s := &LightningSystem{
		world: world,
	}
	return s
}

func (s *LightningSystem) Init() {
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
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
	for _, e := range lightnings.Entities() {
		if lc, ok := lightnings.GetPtr(e); ok && lc.Tracked {
			lc.AnimFrame++
		}
	}
}

func (s *LightningSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventLightningSpawnRequest,
		event.EventLightningUpdateRequest,
		event.EventLightningDespawnRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *LightningSystem) HandleEvent(ev event.GameEvent) {

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
	e := s.world.CreateEntity(core.DomainPlayer)

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
		Tracked:      p.Tracked,
	}
	s.world.Components.Lightning.SetComponent(e, lc)

	// A tracked bolt lives on a lease its owner renews each tick it updates the
	// target: an install that removes the owner or ends its zap writes the shared
	// world without the despawn (D-6), and the lapsed lease retires the bolt.
	life := p.Duration
	if p.Tracked {
		life = parameter.LightningTrackedLease
	}
	// An unset duration still retires the bolt on the first pass instead of leaving it idle
	armTimer(s.world, e, component.TimerLifetime, max(life, time.Nanosecond))
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
		if lc.Tracked {
			armTimer(s.world, e, component.TimerLifetime, parameter.LightningTrackedLease)
		}
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

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

// MaterializeSystem plays each materialize over its footprint until its deadline,
// then sends its completion and, for one carrying a species, the request landing it
type MaterializeSystem struct {
	world *engine.World

	toggle
}

// NewMaterializeSystem creates a new materialize system
func NewMaterializeSystem(world *engine.World) engine.System {
	s := &MaterializeSystem{
		world: world,
	}

	return s
}

// Init resets session state for new game
func (s *MaterializeSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *MaterializeSystem) Name() string {
	return "materialize"
}

// Priority returns the system's priority
func (s *MaterializeSystem) Priority() int {
	return parameter.PriorityMaterialize
}

// EventTypes returns event types handled
func (s *MaterializeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMaterializeRequest,
		event.EventMetaSystemCommandRequest,
	}
}

// HandleEvent processes requests to spawn visual effects
func (s *MaterializeSystem) HandleEvent(ev event.GameEvent) {
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

	if payload, ok := ev.Payload.(*event.MaterializeRequestPayload); ok {
		s.spawnMaterializeEffect(ev.Domain, payload)
	}
}

// Update updates materialize spawner entities and triggers spawn completion events
func (s *MaterializeSystem) Update() {
	if !s.enabled {
		return
	}

	materializes := s.world.Components.Materialize
	if materializes.CountEntities() == 0 {
		return
	}

	var toDestroy []core.Entity
	for _, matEntity := range materializes.Entities() {
		matComp, ok := materializes.GetPtr(matEntity)
		if !ok {
			continue
		}

		// The materialize runs on its deadline timer, armed with its request's duration
		if timer, ok := s.world.Components.Timer.GetPtr(matEntity); ok && timer.Expired(component.TimerDeadline) {
			// The completion carries the domain of the entity it completes, so a
			// gated shared spawn and a drain materialize stay distinguishable (D-7).
			s.world.PushEventDomain(event.EventMaterializeComplete, &event.MaterializeCompletedPayload{
				X:    matComp.X,
				Y:    matComp.Y,
				Type: matComp.Type,
			}, matEntity.Domain())
			if matComp.Species != component.SpeciesNone {
				s.land(matEntity.Domain(), &event.SpeciesLandRequestPayload{Species: matComp.Species, X: matComp.X, Y: matComp.Y})
			}
			toDestroy = append(toDestroy, matEntity)
			continue
		}
	}

	s.world.DestroyEntitiesBatch(toDestroy)
}

// land asks for the species a materialize carried: a Shared one ran on every
// instance, each landing its own copy, and a player one ran here alone, so its
// landing is the D-3 artifact that crosses
func (s *MaterializeSystem) land(domain core.Domain, p *event.SpeciesLandRequestPayload) {
	if domain == core.DomainShared {
		s.world.PushEventDomain(event.EventSpeciesLandRequest, p, core.DomainShared)
		return
	}
	s.world.PushCrossing(event.EventSpeciesLandRequest, p)
}

// spawnMaterializeEffect creates one materialize entity in the request's domain over
// the footprint it resolves, its header held on the map, lasting the request's
// duration or, when that is not positive, its style's
func (s *MaterializeSystem) spawnMaterializeEffect(domain core.Domain, p *event.MaterializeRequestPayload) {
	style, ok := s.world.Resources.MaterializeStyle(p.Type)
	if !ok {
		return
	}
	foot := p.Footprint
	if !foot.Valid() {
		foot = s.world.Resources.Footprint(p.Species)
	}
	if !foot.Valid() {
		foot = vmath.CellFootprint
	}
	config := s.world.Resources.Config
	entity := s.world.CreateEntity(domain)
	s.world.Components.Materialize.SetComponent(entity, component.MaterializeComponent{
		X:         max(0, min(p.X, config.MapWidth-1)),
		Y:         max(0, min(p.Y, config.MapHeight-1)),
		Footprint: foot,
		Type:      p.Type,
		Species:   p.Species,
	})
	duration := style.Duration()
	if p.DurationMs > 0 {
		duration = time.Duration(p.DurationMs) * time.Millisecond
	}
	armTimer(s.world, entity, component.TimerDeadline, duration)
}

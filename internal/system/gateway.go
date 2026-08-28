package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// GatewaySystem manages gateway entity lifecycle and timed spawn emission
// Gateways accumulate delta time and emit species spawn requests at configured intervals
// Anchor liveness validated each tick — gateway despawns if anchor is destroyed
type GatewaySystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

func NewGatewaySystem(world *engine.World) engine.System {
	s := &GatewaySystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("gateway.active")
	s.statCount = world.Resources.Status.Ints.Get("gateway.count")

	s.Init()
	return s
}

func (s *GatewaySystem) Init() {
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

func (s *GatewaySystem) Name() string {
	return "gateway"
}

func (s *GatewaySystem) Priority() int {
	return parameter.PriorityGateway
}

func (s *GatewaySystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameResetRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGatewaySpawnRequest,
		event.EventGatewayDespawnRequest,
	}
}

func (s *GatewaySystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventGatewaySpawnRequest:
		if payload, ok := ev.Payload.(*event.GatewaySpawnRequestPayload); ok {
			s.handleSpawnRequest(payload)
		}

	case event.EventGatewayDespawnRequest:
		if payload, ok := ev.Payload.(*event.GatewayDespawnRequestPayload); ok {
			s.handleDespawnRequest(payload.AnchorEntity)
		}
	}
}

func (s *GatewaySystem) handleSpawnRequest(payload *event.GatewaySpawnRequestPayload) {
	anchorEntity := payload.AnchorEntity

	// Validate anchor exists and has position
	anchorPos, ok := s.world.Positions.GetPosition(anchorEntity)
	if !ok {
		return
	}

	// Enforce single gateway per anchor
	gatewayEntities := s.world.Components.Gateway.Entities()
	for _, e := range gatewayEntities {
		if gw, ok := s.world.Components.Gateway.GetPtr(e); ok {
			if gw.AnchorEntity == anchorEntity {
				return
			}
		}
	}

	baseInterval := time.Duration(payload.BaseIntervalMs) * time.Millisecond
	if baseInterval <= 0 {
		baseInterval = parameter.GatewayDefaultInterval
	}

	rateMultiplier := payload.RateMultiplier
	if rateMultiplier <= 0 {
		rateMultiplier = parameter.GatewayDefaultRateMultiplier
	}

	minInterval := time.Duration(payload.MinIntervalMs) * time.Millisecond
	if minInterval <= 0 {
		minInterval = parameter.GatewayDefaultMinInterval
	}

	rateAccelInterval := time.Duration(payload.RateAccelIntervalMs) * time.Millisecond

	gwComp := component.GatewayComponent{
		AnchorEntity:      anchorEntity,
		Species:           component.SpeciesType(payload.Species),
		SubType:           payload.SubType,
		GroupID:           payload.GroupID,
		PopulationID:      payload.PopulationID,
		BaseInterval:      baseInterval,
		Accumulated:       0,
		Active:            true,
		RateMultiplier:    rateMultiplier,
		RateAccelInterval: rateAccelInterval,
		RateAccelElapsed:  0,
		MinInterval:       minInterval,
		OffsetX:           payload.OffsetX,
		OffsetY:           payload.OffsetY,
	}

	entity := s.world.CreateEntity(core.DomainShared)
	if payload.UseRouteGraph {
		gwComp.RouteDistID = uint32(entity)
	}
	s.world.Components.Gateway.SetComponent(entity, gwComp)

	if payload.UseRouteGraph {
		s.world.PushEvent(event.EventRouteGraphRequest, &event.RouteGraphRequestPayload{
			RouteGraphID:  uint32(entity),
			SourceX:       anchorPos.X + payload.OffsetX,
			SourceY:       anchorPos.Y + payload.OffsetY,
			TargetGroupID: payload.GroupID,
		})
	}
}

func (s *GatewaySystem) handleDespawnRequest(anchorEntity core.Entity) {
	gateways := s.world.Components.Gateway
	var matched core.Entity
	for _, e := range gateways.Entities() {
		gw, ok := gateways.GetPtr(e)
		if !ok {
			continue
		}
		if gw.AnchorEntity == anchorEntity {
			matched = e
			break
		}
	}
	if matched != 0 {
		s.despawnGateway(matched, anchorEntity)
	}
}

func (s *GatewaySystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	// despawnGateway destroys the current gateway immediately, so preserve the
	// tick-start entity order while mutating surviving components in place.
	gateways := s.world.Components.Gateway
	gatewayEntities := gateways.GetAllEntities()

	activeCount := 0

	for _, gwEntity := range gatewayEntities {
		gw, ok := gateways.GetPtr(gwEntity)
		if !ok {
			continue
		}

		anchorEntity := gw.AnchorEntity

		// Anchor liveness check
		anchorPos, anchorAlive := s.world.Positions.GetPosition(anchorEntity)
		if !anchorAlive {
			s.despawnGateway(gwEntity, anchorEntity)
			continue
		}

		if !gw.Active {
			activeCount++
			continue
		}

		// Rate acceleration
		if gw.RateAccelInterval > 0 && gw.RateMultiplier != 1.0 {
			gw.RateAccelElapsed += dt
			for gw.RateAccelElapsed >= gw.RateAccelInterval {
				gw.RateAccelElapsed -= gw.RateAccelInterval
				gw.BaseInterval = time.Duration(float64(gw.BaseInterval) * gw.RateMultiplier)
				if gw.BaseInterval < gw.MinInterval {
					gw.BaseInterval = gw.MinInterval
				}
			}
		}

		// Spawn accumulation
		gw.Accumulated += dt
		if gw.Accumulated >= gw.BaseInterval {
			gw.Accumulated -= gw.BaseInterval

			// Clamp overflow to prevent burst spawning after lag
			if gw.Accumulated > gw.BaseInterval {
				gw.Accumulated = 0
			}

			spawnX := anchorPos.X + gw.OffsetX
			spawnY := anchorPos.Y + gw.OffsetY

			s.emitSpawnEvent(gw.Species, gw.SubType, spawnX, spawnY, gw.GroupID, gw.RouteDistID, gw.PopulationID)
		}

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// emitSpawnEvent routes to the appropriate species spawn request event
// routeDistID enables per-route assignment from bandit pool
func (s *GatewaySystem) emitSpawnEvent(species component.SpeciesType, subType uint8, x, y int, groupID uint8, routeDistID uint32, populationID uint32) {
	switch species {
	case component.SpeciesEye:
		// Sample GA if available
		var evalID uint64
		var genes []float64
		if s.world.Resources.Genetics != nil {
			// Periodic probe keeps all phenotype bins under evaluation
			if s.rng.Float64() < parameter.GAScoutRate {
				genes, evalID = s.world.Resources.Genetics.SampleScout(uint8(species), populationID)
			} else {
				genes, evalID = s.world.Resources.Genetics.Sample(uint8(species), populationID)
			}
		}

		// 1. Resolve phenotype
		eyeType := component.EyeType(subType) // Fallback to base configuration
		if evalID != 0 && len(genes) > 0 {
			eyeType = component.EyeType(
				parameter.GeneBounds.Bin(genes[0], parameter.EyeTypeCount))
		}

		// 2. Pop route using the resolved phenotype
		routeID := -1
		if routeDistID != 0 && s.world.Resources.Adaptation != nil {
			routeID = s.world.Resources.Adaptation.PopRoute(routeDistID, uint8(eyeType))
		}

		s.world.PushEvent(event.EventEyeSpawnRequest, &event.EyeSpawnRequestPayload{
			EvalID:        evalID,
			Genes:         genes,
			X:             x,
			Y:             y,
			Type:          eyeType,
			TargetGroupID: groupID,
			RouteGraphID:  routeDistID,
			RouteID:       routeID,
		})

	case component.SpeciesSnake:
		s.world.PushEvent(event.EventSnakeSpawnRequest, &event.SnakeSpawnRequestPayload{
			X:            x,
			Y:            y,
			SegmentCount: parameter.SnakeMaxSegments,
		})
	}
}

func (s *GatewaySystem) despawnGateway(gwEntity core.Entity, anchorEntity core.Entity) {
	// Let AdaptationSystem coordinate graph and resource draining
	s.world.PushEvent(event.EventGatewayDespawned, &event.GatewayDespawnedPayload{
		GatewayEntity: gwEntity,
		AnchorEntity:  anchorEntity,
	})
	s.world.DestroyEntity(gwEntity)
}

package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// GatewaySystem keeps the gateways: each accumulates time and asks its species'
// system for a spawn at its pace, sends one at once when asked, and goes with its
// anchor
type GatewaySystem struct {
	world    *engine.World
	gateways []core.Entity // the tick's gateways, detached: despawnGateway destroys mid-iteration

	rng *vmath.FastRand

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	toggle
}

func NewGatewaySystem(world *engine.World) engine.System {
	s := &GatewaySystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("gateway.active")
	s.statCount = world.Resources.Status.Ints.Get("gateway.count")

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
		event.EventMetaSystemCommandRequest,
		event.EventGatewaySpawnRequest,
		event.EventGatewayDespawnRequest,
		event.EventGatewayHoldRequest,
		event.EventGatewaySendRequest,
	}
}

func (s *GatewaySystem) HandleEvent(ev event.GameEvent) {
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

	case event.EventGatewayHoldRequest:
		if payload, ok := ev.Payload.(*event.GatewayHoldRequestPayload); ok {
			d := time.Duration(payload.DurationMs) * time.Millisecond
			s.world.Components.Gateway.Each(func(_ core.Entity, gw *component.GatewayComponent) bool {
				if payload.AnchorEntity == 0 || gw.AnchorEntity == payload.AnchorEntity {
					gw.Held = max(gw.Held, d)
				}
				return true
			})
		}

	case event.EventGatewaySendRequest:
		// One of the species now, a hold or its own pace notwithstanding
		if payload, ok := ev.Payload.(*event.GatewaySendRequestPayload); ok {
			if e, gw := s.gatewayOn(payload.AnchorEntity); gw != nil {
				if at, ok := s.world.Positions.GetPosition(gw.AnchorEntity); ok {
					s.emitSpawnEvent(e, payload.Species, uint8(payload.SubType), at.X+gw.OffsetX, at.Y+gw.OffsetY, gw.GroupID, 0, false)
				}
			}
		}
	}
}

// gatewayOn is the gateway an anchor carries, nil for none
func (s *GatewaySystem) gatewayOn(anchor core.Entity) (core.Entity, *component.GatewayComponent) {
	for _, e := range s.world.Components.Gateway.Entities() {
		if gw, ok := s.world.Components.Gateway.GetPtr(e); ok && gw.AnchorEntity == anchor {
			return e, gw
		}
	}
	return 0, nil
}

// handleSpawnRequest stands a gateway on an anchor, one to an anchor: a request for
// an anchor that carries one re-arms it with the species and pace it names, what it
// has accumulated, its hold and its route kept
func (s *GatewaySystem) handleSpawnRequest(payload *event.GatewaySpawnRequestPayload) {
	anchorEntity := payload.AnchorEntity

	// Validate anchor exists and has position
	anchorPos, ok := s.world.Positions.GetPosition(anchorEntity)
	if !ok {
		return
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

	gwComp := component.GatewayComponent{
		AnchorEntity:      anchorEntity,
		Species:           payload.Species,
		SubType:           uint8(payload.SubType),
		GroupID:           payload.GroupID,
		BaseInterval:      baseInterval,
		RateMultiplier:    rateMultiplier,
		RateAccelInterval: time.Duration(payload.RateAccelIntervalMs) * time.Millisecond,
		MinInterval:       minInterval,
		OffsetX:           payload.OffsetX,
		OffsetY:           payload.OffsetY,
		Wave:              payload.Wave,
	}
	if e, standing := s.gatewayOn(anchorEntity); standing != nil {
		gwComp.Accumulated, gwComp.Held, gwComp.RouteDistID = standing.Accumulated, standing.Held, standing.RouteDistID
		gwComp.WaveOut = standing.WaveOut
		s.world.Components.Gateway.SetComponent(e, gwComp)
		return
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

// waveLives reports whether a body of a wave this gateway sent still lives
func (s *GatewaySystem) waveLives(gateway core.Entity) (alive bool) {
	s.world.Components.Behavior.Each(func(_ core.Entity, b *component.BehaviorComponent) bool {
		alive = b.Wave && b.Gateway == gateway
		return !alive
	})
	return alive
}

func (s *GatewaySystem) handleDespawnRequest(anchorEntity core.Entity) {
	if e, gw := s.gatewayOn(anchorEntity); gw != nil {
		s.despawnGateway(e, anchorEntity)
	}
}

func (s *GatewaySystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	gateways := s.world.Components.Gateway
	s.gateways = append(s.gateways[:0], gateways.Entities()...)

	activeCount := 0

	for _, gwEntity := range s.gateways {
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

		if gw.WaveOut && !gw.Wave && !s.waveLives(gwEntity) {
			gw.WaveOut = false
			s.world.PushEvent(event.EventGatewayWaveDefeated, &event.GatewayWavePayload{
				AnchorEntity: anchorEntity, X: anchorPos.X, Y: anchorPos.Y,
			})
		}

		if gw.Held > 0 {
			gw.Held = max(gw.Held-dt, 0)
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

			s.emitSpawnEvent(gwEntity, gw.Species, gw.SubType, spawnX, spawnY, gw.GroupID, gw.RouteDistID, gw.Wave)
			gw.WaveOut = gw.WaveOut || gw.Wave && gw.Species >= component.SpeciesDataFirst
		}

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// emitSpawnEvent asks the species' own system for a spawn, a data species' by the
// land request its program answers, marked one of a wave or not; routeDistID draws
// an eye's route from its pool
func (s *GatewaySystem) emitSpawnEvent(gateway core.Entity, species component.SpeciesType, subType uint8, x, y int, groupID uint8, routeDistID uint32, wave bool) {
	switch species {
	case component.SpeciesEye:
		// Sample GA if available
		var evalID uint64
		var genes []float64
		if s.world.Resources.Genetics != nil {
			// Periodic probe keeps all phenotype bins under evaluation
			if s.rng.Float64() < parameter.GAScoutRate {
				genes, evalID = s.world.Resources.Genetics.SampleScout(uint8(species))
			} else {
				genes, evalID = s.world.Resources.Genetics.Sample(uint8(species))
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

	case component.SpeciesSwarm:
		s.world.PushEvent(event.EventSwarmSpawnRequest, &event.SwarmSpawnRequestPayload{
			X:             x,
			Y:             y,
			TargetGroupID: groupID,
			Gateway:       gateway,
		})

	default:
		if species >= component.SpeciesDataFirst {
			s.world.PushEvent(event.EventSpeciesLandRequest, &event.SpeciesLandRequestPayload{
				Species: species, X: x, Y: y, Gateway: gateway, Wave: wave,
			})
		}
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

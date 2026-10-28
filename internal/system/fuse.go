package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// FuseSystem orchestrates the visual and timing transition of entity fusions and manages fusion source system
// Every spawn it plays rides what plays it, so it holds nothing: a drain pair's swarm lands as the spirits its
// drains fly in arrive, and a quasar, which no drains fly in, as its materialize ends.
// A fusion is player-domain: it consumes drains and their visuals; the crossing is the geometry-only spawn request.
type FuseSystem struct {
	world *engine.World

	rng *vmath.FastRand

	statSpawnFailures *atomic.Int64
	statDisabled      *atomic.Int64
	toggle
}

// NewFuseSystem creates a new fuse system
func NewFuseSystem(world *engine.World) engine.System {
	s := &FuseSystem{
		world: world,
	}
	s.statSpawnFailures = world.Resources.Status.Ints.Get("fuse.spawn_failures")
	s.statDisabled = world.Resources.Status.Ints.Get("fuse.disabled_rejects")
	return s
}

func (s *FuseSystem) Init() {
	s.statSpawnFailures.Store(0)
	s.statDisabled.Store(0)
	// Spirit scatter is a player-domain effect, so the draw must not touch the shared stream
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.enabled = true
}

// Name returns system's name
func (s *FuseSystem) Name() string {
	return "fuse"
}

func (s *FuseSystem) Priority() int {
	return parameter.PriorityFuse
}

func (s *FuseSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFuseQuasarRequest,
		event.EventFuseSwarmRequest,
		event.EventMaterializeComplete,
		event.EventSpiritArrived,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *FuseSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		if ev.Type == event.EventFuseQuasarRequest || ev.Type == event.EventFuseSwarmRequest {
			s.statDisabled.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventFuseQuasarRequest:
		owner := core.Entity(0)
		if p, ok := ev.Payload.(*event.FuseQuasarRequestPayload); ok {
			owner = p.Entity
		}
		// Nil is retained for debug/backward compatibility. In a live session it
		// elects the coordinator, so a shared legacy trigger still cannot fan out.
		if owner == 0 {
			if session := s.world.Session(); session.Live && !session.Coordinator() {
				return
			}
			owner = s.world.Resources.Player.Entity
		}
		if s.world.ResolveOwnedCursor(owner) != 0 && !s.quasarMaterializing() {
			s.handleQuasarFuse()
		}

	case event.EventFuseSwarmRequest:
		if payload, ok := ev.Payload.(*event.FuseSwarmRequestPayload); ok {
			s.handleSwarmFuse(payload.DrainA, payload.DrainB, payload.Effect)
		}

	case event.EventMaterializeComplete:
		// The completion carries its materialize's domain (D-7), and a fusion's ran on
		// this instance alone
		if payload, ok := ev.Payload.(*event.MaterializeCompletedPayload); ok && ev.Domain != core.DomainShared {
			s.landFusion(payload.Species, payload.X, payload.Y)
		}

	case event.EventSpiritArrived:
		// A fusion's spirits ran on this instance alone, as its materialize does
		if payload, ok := ev.Payload.(*event.SpiritArrivedPayload); ok && ev.Domain != core.DomainShared {
			s.landFusion(payload.Species, payload.X, payload.Y)
		}
	}
}

// Update is empty: a fusion's spirits or materialize run it, and their end lands it
func (s *FuseSystem) Update() {}

// quasarMaterializing reports a quasar fusion in flight, whose materialize is its pending spawn
func (s *FuseSystem) quasarMaterializing() bool {
	materializes := s.world.Components.Materialize
	for _, e := range materializes.Entities() {
		if m, ok := materializes.GetPtr(e); ok && e.Domain() == core.DomainPlayer && m.Species == component.SpeciesQuasar {
			return true
		}
	}
	return false
}

// fuseInto plays a fusion over area, and the species lands as what plays it ends:
// the spirits the sources fly in, unless the effect is the materialize or no
// source flies, else a materialize naming the species its completion lands. Either
// is this instance's own: a shared one would exist only on the producer.
func (s *FuseSystem) fuseInto(species component.SpeciesType, effect event.FuseEffect, sources []vmath.Point, area vmath.Area) {
	if effect != event.FuseEffectMaterialize && len(sources) > 0 {
		s.effectSpiritArea(species, sources, area, component.SpiritCyan)
		return
	}
	s.world.PushLocal(event.EventMaterializeAreaRequest, &event.MaterializeAreaRequestPayload{
		X:          area.X,
		Y:          area.Y,
		AreaWidth:  area.Width,
		AreaHeight: area.Height,
		Type:       component.SpawnTypeSwarm,
		Species:    species,
	})
}

// effectSpiritArea flies one spirit per source into the target area, player-domain
// visuals; every spirit flies one time, and the first names the species its arrival
// lands on the area, so the spawn rides it
func (s *FuseSystem) effectSpiritArea(species component.SpeciesType, sources []vmath.Point, area vmath.Area, c component.SpiritColor) {
	for i, src := range sources {
		dest := area.DistributePoint(i, s.rng)

		request := &event.SpiritSpawnRequestPayload{
			StartX:    src.X,
			StartY:    src.Y,
			TargetX:   dest.X,
			TargetY:   dest.Y,
			Char:      visual.DrainChar,
			BaseColor: c,
		}
		if i == 0 {
			request.Species, request.LandX, request.LandY = species, area.X, area.Y
		}
		s.world.PushLocal(event.EventSpiritSpawnRequest, request)
	}
}

// killDrains reports and retires the consumed drains; the batch is domain-pure, so a
// shared record never names a player entity.
func (s *FuseSystem) killDrains(drains []core.Entity) {
	if len(drains) == 0 {
		return
	}
	// The scope stamps the spirit and effect pushes below; EmitDeath takes its own
	// domain from the entities it destroys.
	s.world.WithDomain(core.DomainPlayer, func() {
		for _, drainEntity := range drains {
			killX, killY := -1, -1
			if pos, ok := s.world.Positions.GetPosition(drainEntity); ok {
				killX, killY = pos.X, pos.Y
			}
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:  drainEntity,
				Species: component.SpeciesDrain,
				X:       killX,
				Y:       killY,
			})
			s.world.PushCrossing(event.EventDrainDefeated, &event.DrainDefeatedPayload{
				Entity: s.world.Resources.Player.Entity,
			})
		}
		event.EmitDeath(s.world.Resources.Event.Queue, 0, drains...)
	})
}

func (s *FuseSystem) handleSwarmFuse(drainA, drainB core.Entity, effect event.FuseEffect) {
	posA, okA := s.world.Positions.GetPosition(drainA)
	posB, okB := s.world.Positions.GetPosition(drainB)
	if !okA || !okB {
		s.statSpawnFailures.Add(1)
		return
	}

	midX := (posA.X + posB.X) / 2
	midY := (posA.Y + posB.Y) / 2

	// The nearest footprint inside the map free of walls a spawn is kept from
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaInside(
		midX, midY,
		parameter.SwarmWidth, parameter.SwarmHeight,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		s.statSpawnFailures.Add(1)
		return // No valid position - cancel fusion
	}

	s.killDrains([]core.Entity{drainA, drainB})

	sources := []vmath.Point{{X: posA.X, Y: posA.Y}, {X: posB.X, Y: posB.Y}}
	area := vmath.Area{X: topLeftX, Y: topLeftY, Width: parameter.SwarmWidth, Height: parameter.SwarmHeight}
	s.fuseInto(component.SpeciesSwarm, effect, sources, area)
}

// handleQuasarFuse consumes every drain into one quasar at their centroid
func (s *FuseSystem) handleQuasarFuse() {
	drainEntities := s.world.Components.Drain.Entities()
	sources := make([]vmath.Point, 0, len(drainEntities))

	for _, drainEntity := range drainEntities {
		if pos, ok := s.world.Positions.GetPosition(drainEntity); ok {
			sources = append(sources, vmath.Point{X: pos.X, Y: pos.Y})
		}
	}

	// Calculate centroid
	sumX, sumY := 0, 0
	for _, p := range sources {
		sumX += p.X
		sumY += p.Y
	}

	var cX, cY int
	if len(sources) > 0 {
		cX = sumX / len(sources)
		cY = sumY / len(sources)
	} else {
		config := s.world.Resources.Config
		cX = config.MapWidth / 2
		cY = config.MapHeight / 2
	}

	// The nearest footprint inside the map free of walls a spawn is kept from
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaInside(
		cX, cY,
		parameter.QuasarWidth, parameter.QuasarHeight,
		parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		s.statSpawnFailures.Add(1)
		return // No valid position - cancel fusion
	}

	area := vmath.Area{X: topLeftX, Y: topLeftY, Width: parameter.QuasarWidth, Height: parameter.QuasarHeight}
	s.fuseInto(component.SpeciesQuasar, event.FuseEffectMaterialize, sources, area)
	s.killDrains(drainEntities)
}

// landFusion crosses the spawn a fusion's spirit or materialize held, its header on the
// footprint whose top-left is x, y. The species spawn is the shared result, and the
// request is the D-3 artifact that carries it — stamped player because the fuse
// produced it, which is what tells it from the same type a shared storm pushes.
func (s *FuseSystem) landFusion(species component.SpeciesType, x, y int) {
	switch species {
	case component.SpeciesQuasar:
		s.world.PushCrossing(event.EventQuasarSpawnRequest, &event.QuasarSpawnRequestPayload{
			X: x + parameter.QuasarHeaderOffsetX,
			Y: y + parameter.QuasarHeaderOffsetY,
		})
	case component.SpeciesSwarm:
		s.world.PushCrossing(event.EventSwarmSpawnRequest, &event.SwarmSpawnRequestPayload{
			X: x + parameter.SwarmHeaderOffsetX,
			Y: y + parameter.SwarmHeaderOffsetY,
		})
	}
}

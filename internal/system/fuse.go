package system

import (
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// FuseSystem plays fusions and holds nothing: a request's sources go into its target
// species, which rides what plays the fusion, the spirits its sources fly in or a
// materialize over the target's footprint, and lands as that ends. A fusion of
// drains is player-domain: it consumes drains and their visuals, and its carrier's
// landing is what crosses.
type FuseSystem struct {
	world *engine.World

	rng *vmath.FastRand

	statSpawnFailures *atomic.Int64
	statDisabled      *atomic.Int64
	toggle
}

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

func (s *FuseSystem) Name() string {
	return "fuse"
}

func (s *FuseSystem) Priority() int {
	return parameter.PriorityFuse
}

func (s *FuseSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFuseRequest,
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
		if ev.Type == event.EventFuseRequest {
			s.statDisabled.Add(1)
		}
		return
	}

	if payload, ok := ev.Payload.(*event.FuseRequestPayload); ok {
		s.fuse(payload)
	}
}

// Update is empty: a fusion's spirits or materialize run it, and their end lands it
func (s *FuseSystem) Update() {}

// fuse plays one request: its sources consumed and its target placed on the nearest
// footprint free of walls a spawn is kept from at their centroid, at the map's
// centre when a sweep finds none. A request naming entities fails when one is gone;
// a sweep plays on the instance its cursor elects, and waits for one into its
// target in flight.
func (s *FuseSystem) fuse(p *event.FuseRequestPayload) {
	if !s.lands(p.Target) {
		s.statSpawnFailures.Add(1)
		return
	}
	var consumed []core.Entity
	if len(p.Entities) > 0 {
		var ok bool
		if consumed, ok = s.named(p.Entities); !ok {
			s.statSpawnFailures.Add(1)
			return
		}
	} else {
		if !s.elected(p.Cursor) || s.inFlight(p.Target) {
			return
		}
		consumed = s.swept(p.Sources)
	}

	sources := make([]vmath.Point, 0, len(consumed))
	for _, e := range consumed {
		pos, _ := s.world.Positions.GetPosition(e)
		sources = append(sources, vmath.Point{X: pos.X, Y: pos.Y})
	}
	x, y := s.world.Resources.Config.MapWidth/2, s.world.Resources.Config.MapHeight/2
	if len(sources) > 0 {
		x, y = 0, 0
		for _, src := range sources {
			x, y = x+src.X, y+src.Y
		}
		x, y = x/len(sources), y/len(sources)
	}

	foot := s.world.Resources.Footprint(p.Target)
	left, top, found := s.world.Positions.FindFreeAreaInside(x, y, foot, component.WallBlockSpawn, 0)
	if !found {
		s.statSpawnFailures.Add(1)
		return
	}
	s.killDrains(consumed)
	s.fuseInto(p.Target, p.Effect, sources, left+foot.HeaderX, top+foot.HeaderY)
}

// lands reports a species a land request stands: one with a body, in the Shared domain
func (s *FuseSystem) lands(species component.SpeciesType) bool {
	r := s.world.Resources
	return r.Footprint(species).Valid() && r.SpeciesDomain(species) == core.DomainShared
}

// named reads a request's entities as its sources, false when one is no standing drain
func (s *FuseSystem) named(entities []core.Entity) ([]core.Entity, bool) {
	for _, e := range entities {
		if !s.world.Components.Drain.HasEntity(e) || !s.world.Positions.HasPosition(e) {
			return nil, false
		}
	}
	return entities, true
}

// elected reports this instance simulates the cursor whose drains a sweep takes;
// 0 is the coordinator's in a live session, so a Shared trigger naming none fuses
// on one instance
func (s *FuseSystem) elected(cursor core.Entity) bool {
	if cursor == 0 {
		if session := s.world.Session(); session.Live && !session.Coordinator() {
			return false
		}
		cursor = s.world.Resources.Player.Entity
	}
	return s.world.ResolveOwnedCursor(cursor) != 0
}

// inFlight reports a fusion into the species in flight on this instance, whose
// carrier is its pending spawn
func (s *FuseSystem) inFlight(species component.SpeciesType) bool {
	for _, e := range s.world.Components.Materialize.Entities() {
		if m, ok := s.world.Components.Materialize.GetPtr(e); ok && e.Domain() == core.DomainPlayer && m.Species == species {
			return true
		}
	}
	for _, e := range s.world.Components.Spirit.Entities() {
		if sp, ok := s.world.Components.Spirit.GetPtr(e); ok && e.Domain() == core.DomainPlayer && sp.Species == species {
			return true
		}
	}
	return false
}

// swept takes each row's drains, the oldest first, every one when its count is 0;
// a row naming another species sweeps nothing
func (s *FuseSystem) swept(rows []event.FuseSource) []core.Entity {
	standing := slices.DeleteFunc(s.world.Components.Drain.GetAllEntities(), func(e core.Entity) bool {
		return !s.world.Positions.HasPosition(e)
	})
	slices.Sort(standing)
	var drains []core.Entity
	for _, row := range rows {
		if row.Species != component.SpeciesDrain {
			continue
		}
		n := len(standing)
		if row.Count > 0 {
			n = min(n, row.Count)
		}
		drains, standing = append(drains, standing[:n]...), standing[n:]
	}
	return drains
}

// fuseInto plays a fusion over the species' footprint with its header on (x, y), and
// the species lands as what plays it ends: the spirits the sources fly in, unless
// the effect is the materialize or no source flies, else a materialize naming the
// species, in a data species' own style. Either is this instance's own, a shared
// one existing only on the producer, so its landing crosses.
func (s *FuseSystem) fuseInto(species component.SpeciesType, effect event.FuseEffect, sources []vmath.Point, x, y int) {
	if effect != event.FuseEffectMaterialize && len(sources) > 0 {
		s.effectSpiritArea(species, sources, x, y, component.SpiritCyan)
		return
	}
	style := component.SpawnTypeSwarm
	if species >= component.SpeciesDataFirst {
		style = component.SpawnTypeDataFirst + component.SpawnType(species-component.SpeciesDataFirst)
	}
	s.world.PushLocal(event.EventMaterializeRequest, &event.MaterializeRequestPayload{
		X:       x,
		Y:       y,
		Type:    style,
		Species: species,
	})
}

// effectSpiritArea flies one spirit per source into the species' footprint with its
// header on (x, y), player-domain visuals; every spirit flies one time, and the
// first names the species its arrival lands there, so the spawn rides it
func (s *FuseSystem) effectSpiritArea(species component.SpeciesType, sources []vmath.Point, x, y int, c component.SpiritColor) {
	area := s.world.Resources.Footprint(species).At(x, y)
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
			request.Species, request.LandX, request.LandY = species, x, y
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

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
// species, which rides the spirits they fly in or a materialize over its footprint
// and lands as that ends. A Shared request's composites fuse alike on every instance
// by a Shared carrier; a fusion of drains alone is the elected participant's, whose
// carrier crosses.
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
		s.fuse(ev.Domain == core.DomainShared, payload)
	}
}

// Update is empty: a fusion's spirits or materialize run it, and their end lands it
func (s *FuseSystem) Update() {}

// fuse plays one request: its sources consumed and its target placed on the nearest
// footprint free of walls a spawn is kept from at their centroid, the composites'
// alone when it takes any, and at the map's centre when a sweep finds none. It fails
// naming what its domain cannot take, an entity twice or one no longer standing; a
// sweep's elected participant takes no drains while a spawn of the target flies.
func (s *FuseSystem) fuse(shared bool, p *event.FuseRequestPayload) {
	if !s.lands(p.Target) || !s.takes(shared, p) {
		s.statSpawnFailures.Add(1)
		return
	}
	var drains, composites []core.Entity
	elected := false
	switch {
	case len(p.Entities) == 0:
		composites = s.sweepComposites(p.Sources)
		if elected = s.elected(p.Cursor) && !s.inFlight(p.Target); elected {
			drains = s.sweepDrains(p.Sources)
		}
	case shared:
		composites = p.Entities
	default:
		drains, elected = p.Entities, true
	}
	carrier := core.DomainShared // composites stand alike everywhere, so every instance lands them
	if len(composites) == 0 {
		if !elected {
			return
		}
		carrier = core.DomainPlayer
	}

	sources := append(s.positions(composites), s.positions(drains)...)
	placed := sources
	if len(composites) > 0 {
		// Every instance places a Shared request alike, and a drain is one participant's
		placed = sources[:len(composites)]
	}
	x, y := s.world.Resources.Config.MapWidth/2, s.world.Resources.Config.MapHeight/2
	if len(placed) > 0 {
		x, y = 0, 0
		for _, src := range placed {
			x, y = x+src.X, y+src.Y
		}
		x, y = x/len(placed), y/len(placed)
	}

	foot := s.world.Resources.Footprint(p.Target)
	corner, found := s.world.Positions.FindFreeAreaInside(vmath.Point{X: x, Y: y}, foot, component.WallBlockSpawn, 0)
	if !found {
		s.statSpawnFailures.Add(1)
		return
	}
	s.consume(composites)
	s.killDrains(drains)
	s.fuseInto(p.Target, p.Effect, sources, corner.X+foot.HeaderX, corner.Y+foot.HeaderY, carrier)
}

// lands reports a species a land request stands: one with a body, in the Shared domain
func (s *FuseSystem) lands(species component.SpeciesType) bool {
	r := s.world.Resources
	return r.Footprint(species).Valid() && r.SpeciesDomain(species) == core.DomainShared
}

// takes reports a request names only sources its domain takes, each once: a
// participant's its drains alone, by entity or row, and a Shared one, every
// instance's alike, composites by entity, whose ids every instance shares, and
// drains by row only
func (s *FuseSystem) takes(shared bool, p *event.FuseRequestPayload) bool {
	for i, e := range p.Entities {
		switch {
		case slices.Contains(p.Entities[:i], e),
			shared && s.composite(e) == component.SpeciesNone,
			!shared && (!s.world.Components.Drain.HasEntity(e) || !s.world.Positions.HasPosition(e)):
			return false
		}
	}
	return len(p.Entities) > 0 || shared || !slices.ContainsFunc(p.Sources, func(row event.FuseSource) bool {
		return row.Species != component.SpeciesDrain
	})
}

// composite reports the species of a composite a fusion takes, a unit one at its
// root, standing; SpeciesNone for any other entity
func (s *FuseSystem) composite(e core.Entity) component.SpeciesType {
	h, ok := s.world.Components.Header.GetPtr(e)
	if !ok || h.Type != component.CompositeTypeUnit || h.ParentHeader != 0 || !s.world.Positions.HasPosition(e) {
		return component.SpeciesNone
	}
	// A body at no hit points is its own system's kill to report
	if c, ok := s.world.Components.Combat.GetPtr(e); ok && c.HitPoints <= 0 {
		return component.SpeciesNone
	}
	if b, ok := s.world.Components.Behavior.GetPtr(e); ok && h.Behavior == component.BehaviorData {
		if programs := s.world.Resources.Behaviors; int(b.Program) < len(programs) {
			return programs[b.Program].Species
		}
		return component.SpeciesNone
	}
	return component.BehaviorSpecies[h.Behavior]
}

// elected reports this instance simulates the cursor whose drains a sweep takes: the
// request's, or for 0 the first participant's, read off the roster every instance
// holds alike at the request's tick, so a Shared trigger naming none takes drains on
// one instance, a host that drives no cursor's session included
func (s *FuseSystem) elected(cursor core.Entity) bool {
	if cursor == 0 {
		cursor = s.firstParticipant()
	}
	return s.world.ResolveOwnedCursor(cursor) != 0
}

// firstParticipant is the cursor in the lowest slot whose participant plays, a
// Wraith's being away; zero with nobody playing
func (s *FuseSystem) firstParticipant() core.Entity {
	for i := range parameter.MaxPlayers {
		if e := s.world.Resources.Player.Slot(uint8(i)); e != 0 && !s.world.Components.Wraith.HasEntity(e) {
			return e
		}
	}
	return 0
}

// inFlight reports a spawn of the species in flight on this instance: a materialize
// or spirit naming it, this participant's carrier or a Shared one
func (s *FuseSystem) inFlight(species component.SpeciesType) bool {
	for _, e := range s.world.Components.Materialize.Entities() {
		if m, ok := s.world.Components.Materialize.GetPtr(e); ok && m.Species == species {
			return true
		}
	}
	for _, e := range s.world.Components.Spirit.Entities() {
		if sp, ok := s.world.Components.Spirit.GetPtr(e); ok && sp.Species == species {
			return true
		}
	}
	return false
}

// sweepDrains takes each drain row's drains, the oldest first, every one when its
// count is 0
func (s *FuseSystem) sweepDrains(rows []event.FuseSource) []core.Entity {
	standing := slices.DeleteFunc(s.world.Components.Drain.GetAllEntities(), func(e core.Entity) bool {
		return !s.world.Positions.HasPosition(e)
	})
	slices.Sort(standing)
	var taken []core.Entity
	for _, row := range rows {
		if row.Species != component.SpeciesDrain {
			continue
		}
		n := len(standing)
		if row.Count > 0 {
			n = min(n, row.Count)
		}
		taken, standing = append(taken, standing[:n]...), standing[n:]
	}
	return taken
}

// sweepComposites takes each other row's composites of its species, the oldest
// first, every one when its count is 0; a species no composite of which a fusion
// takes, the pylon's, sweeps nothing
func (s *FuseSystem) sweepComposites(rows []event.FuseSource) []core.Entity {
	standing := slices.DeleteFunc(s.world.Components.Header.GetAllEntities(), func(e core.Entity) bool {
		return s.composite(e) == component.SpeciesNone
	})
	slices.Sort(standing)
	var taken []core.Entity
	for _, row := range rows {
		if row.Species == component.SpeciesDrain {
			continue
		}
		n := 0
		standing = slices.DeleteFunc(standing, func(e core.Entity) bool {
			if (row.Count > 0 && n == row.Count) || s.composite(e) != row.Species {
				return false
			}
			n++
			taken = append(taken, e)
			return true
		})
	}
	return taken
}

// positions is where each source stands, a composite by its header
func (s *FuseSystem) positions(sources []core.Entity) []vmath.Point {
	points := make([]vmath.Point, 0, len(sources))
	for _, e := range sources {
		pos, _ := s.world.Positions.GetPosition(e)
		points = append(points, vmath.Point{X: pos.X, Y: pos.Y})
	}
	return points
}

// fuseInto plays a fusion over the species' footprint with its header on (x, y): the
// spirits the sources fly in, unless the effect is the materialize or no source
// flies, else a materialize naming the species, in a data species' own style. The
// first spirit or the materialize carries the species in the carrier's domain and
// lands it as that ends: a Shared one on every instance, a player one by crossing.
func (s *FuseSystem) fuseInto(species component.SpeciesType, effect event.FuseEffect, sources []vmath.Point, x, y int, carrier core.Domain) {
	if effect != event.FuseEffectMaterialize && len(sources) > 0 {
		s.effectSpiritArea(species, sources, x, y, component.SpiritCyan, carrier)
		return
	}
	style := component.SpawnTypeSwarm
	if species >= component.SpeciesDataFirst {
		style = component.SpawnTypeDataFirst + component.SpawnType(species-component.SpeciesDataFirst)
	}
	s.world.PushEventDomain(event.EventMaterializeRequest, &event.MaterializeRequestPayload{
		X:       x,
		Y:       y,
		Type:    style,
		Species: species,
	}, carrier)
}

// effectSpiritArea flies one spirit per source into the species' footprint with its
// header on (x, y), every spirit one time: the first, in the carrier's domain, names
// the species its arrival lands there, so the spawn rides it, and the rest are
// player-domain visuals
func (s *FuseSystem) effectSpiritArea(species component.SpeciesType, sources []vmath.Point, x, y int, c component.SpiritColor, carrier core.Domain) {
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
		domain := core.DomainPlayer
		if i == 0 {
			request.Species, request.LandX, request.LandY = species, x, y
			domain = carrier
		}
		s.world.PushEventDomain(event.EventSpiritSpawnRequest, request, domain)
	}
}

// consume retires a Shared request's composites, alike on every instance: silent and
// unrewarded, as a crush is, so no kill reports them
func (s *FuseSystem) consume(composites []core.Entity) {
	for _, e := range composites {
		s.world.PushEventDomain(event.EventCompositeDestroyRequest,
			&event.CompositeDestroyRequestPayload{HeaderEntity: e}, core.DomainShared)
	}
}

// killDrains reports and retires the consumed drains; the batch is domain-pure, so a
// shared record never names a player entity.
func (s *FuseSystem) killDrains(drains []core.Entity) {
	if len(drains) == 0 {
		return
	}
	// The scope stamps the kills player; EmitDeath takes its own domain from the
	// entities it destroys.
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
		event.EmitDeath(s.world.Resources.Event.Queue, component.DeathSilent, drains...)
	})
}

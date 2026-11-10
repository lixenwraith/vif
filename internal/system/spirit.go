package system

import (
	"slices"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SpiritSystem flies glyphs as light. The parts a tick sends travel as one flight,
// which ends at its deadline: converging ones spiral in to their targets, where a
// flight naming a species lands it, so the spawn rides its spirit, and scattered
// ones are a body thrown out from where it stood.
// Stamped: a converging flight is created in the requesting event's domain (D-7)
type SpiritSystem struct {
	world *engine.World

	ended []core.Entity
	// spare is the storage of the flights that ended, which the next ones fill
	spare [][]component.SpiritPart
	// rng throws a scattered body's parts, reseeded for each body
	rng vmath.FastRand

	toggle
}

func NewSpiritSystem(world *engine.World) engine.System {
	s := &SpiritSystem{
		world: world,
	}
	return s
}

func (s *SpiritSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *SpiritSystem) Name() string {
	return "spirit"
}

func (s *SpiritSystem) Priority() int {
	return parameter.PrioritySpirit
}

func (s *SpiritSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSpiritSpawnRequest,
		event.EventSpiritScatterRequest,
		event.EventSpiritDespawnRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *SpiritSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventMetaSystemCommandRequest:
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && payload.SystemName == s.Name() {
			s.enabled = payload.Enabled
		}

	case event.EventSpiritSpawnRequest:
		if payload, ok := ev.Payload.(*event.SpiritSpawnRequestPayload); ok && s.enabled {
			s.converge(payload, ev.Domain)
		}

	case event.EventSpiritScatterRequest:
		if batch, ok := ev.Payload.(*event.BatchPayload[component.EffectCell]); ok {
			if s.enabled {
				s.scatter(batch.Entries)
			}
			event.EffectBatchPool.Release(batch)
		}

	case event.EventSpiritDespawnRequest:
		if s.enabled {
			// Batch destruction mutates the live spirit slice, so detach it first
			s.world.DestroyEntitiesBatch(s.world.Components.Spirit.GetAllEntities())
		}
	}
}

// Update ends the flights whose deadline has run, landing the species each one
// carried; a disabled system's end and land nothing
func (s *SpiritSystem) Update() {
	spirits := s.world.Components.Spirit
	s.ended = s.ended[:0]
	for _, e := range spirits.Entities() {
		if !timerRunning(s.world, e, component.TimerDeadline) {
			s.ended = append(s.ended, e)
		}
	}
	// Store order may differ between instances after a correction (D-8); the
	// landings must not carry that difference into spawn order
	slices.Sort(s.ended)
	for _, e := range s.ended {
		spirit, ok := spirits.GetPtr(e)
		if !ok {
			continue
		}
		if s.enabled && spirit.Species != component.SpeciesNone {
			s.land(e.Domain(), &event.SpeciesLandRequestPayload{Species: spirit.Species, X: spirit.LandX, Y: spirit.LandY})
		}
		s.spare = append(s.spare, spirit.Parts[:0])
	}
	s.world.DestroyEntitiesBatch(s.ended)
}

// land asks for the species a flight carried: a Shared one flew on every instance,
// each landing its own copy, and a player one flew here alone, so its landing is
// the D-3 artifact that crosses
func (s *SpiritSystem) land(domain core.Domain, p *event.SpeciesLandRequestPayload) {
	if domain == core.DomainShared {
		s.world.PushEventDomain(event.EventSpeciesLandRequest, p, core.DomainShared)
		return
	}
	s.world.PushCrossing(event.EventSpeciesLandRequest, p)
}

// converge flies one part from its start cell to its target, every part one time
// so they arrive together
func (s *SpiritSystem) converge(p *event.SpiritSpawnRequestPayload, domain core.Domain) {
	part := component.SpiritPart{Rune: p.Char}
	part.FromX, part.FromY = (vmath.Point{X: p.StartX, Y: p.StartY}).Center()
	part.ToX, part.ToY = (vmath.Point{X: p.TargetX, Y: p.TargetY}).Center()

	flight := s.flight(component.SpiritComponent{
		Color:   p.BaseColor,
		Species: p.Species,
		LandX:   p.LandX,
		LandY:   p.LandY,
	}, domain, parameter.SpiritAnimationDuration)
	flight.Parts = append(flight.Parts, part)
}

// scatter throws a body's glyphs out from its middle on this tick's scatter flight:
// each away from the middle, turned and carried on by draws of its own
func (s *SpiritSystem) scatter(cells []component.EffectCell) {
	if len(cells) == 0 {
		return
	}
	left, top, right, bottom := cells[0].X, cells[0].Y, cells[0].X, cells[0].Y
	for i := range cells {
		left, right = min(left, cells[i].X), max(right, cells[i].X)
		top, bottom = min(top, cells[i].Y), max(bottom, cells[i].Y)
	}
	// The middle and the reach are measured with heights in the units of a width,
	// so a body is thrown as far up as across on the screen
	aspect := s.world.Resources.Config.CellAspect
	midX, midY := float64(left+right+1)/2, float64(top+bottom+1)/2
	reach := visual.SpiritScatterReach +
		visual.SpiritScatterGrowth*vmath.Magnitude(midX-float64(left), aspect.Circular(midY-float64(top)))

	s.rng.Reseed(vmath.MixChain(uint64(left), uint64(top), s.world.Resources.Game.State.GetGameTicks()))
	flight := s.flight(component.SpiritComponent{Pattern: component.SpiritScatter}, core.DomainPlayer, parameter.SpiritScatterDuration)
	for i := range cells {
		cell := &cells[i]
		if cell.Rune == 0 {
			continue
		}
		fromX, fromY := (vmath.Point{X: cell.X, Y: cell.Y}).Center()
		dirX, dirY := vmath.Normalize2D(fromX-midX, aspect.Circular(fromY-midY))
		turn := visual.SpiritScatterTurn
		if dirX == 0 && dirY == 0 {
			// The middle cell has no way out of its own: any way is its way
			dirX, turn = 1, vmath.TwoPi/2
		}
		dirX, dirY = vmath.RotateVector(dirX, dirY, (2*s.rng.Float64()-1)*turn)
		throw := reach + s.rng.Float64()*visual.SpiritScatterSpread
		flight.Parts = append(flight.Parts, component.SpiritPart{
			FromX: fromX,
			FromY: fromY,
			ToX:   fromX + dirX*throw,
			ToY:   fromY + aspect.FromCircular(dirY*throw),
			Rune:  cell.Rune,
			Color: cell.Fg,
		})
	}
}

// flight is the flight parts like these join: the one this tick opened for their
// pattern, color and domain, or a new one. A flight naming a species is its own,
// being the pending spawn a capture carries.
func (s *SpiritSystem) flight(like component.SpiritComponent, domain core.Domain, d time.Duration) *component.SpiritComponent {
	spirits, timers := s.world.Components.Spirit, s.world.Components.Timer
	if like.Species == component.SpeciesNone {
		for _, e := range spirits.Entities() {
			spirit, ok := spirits.GetPtr(e)
			if !ok || e.Domain() != domain || spirit.Pattern != like.Pattern || spirit.Color != like.Color ||
				spirit.Species != component.SpeciesNone {
				continue
			}
			// A deadline no tick has aged yet is this tick's
			if timer, ok := timers.GetPtr(e); ok && timer.Slots[component.TimerDeadline].Remaining == d {
				return spirit
			}
		}
	}

	if n := len(s.spare); n > 0 {
		like.Parts, s.spare = s.spare[n-1], s.spare[:n-1]
	}
	e := s.world.CreateEntity(domain)
	spirits.SetComponent(e, like)
	armTimer(s.world, e, component.TimerDeadline, d)
	spirit, _ := spirits.GetPtr(e)
	return spirit
}

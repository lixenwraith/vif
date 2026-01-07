package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ExplosionSystem handles explosion triggering and glyph-to-dust transformation
// Visual rendering is handled by ExplosionRenderer
type ExplosionSystem struct {
	world *engine.World

	statTriggered *atomic.Int64
	statConverted *atomic.Int64

	enabled bool
}

func NewExplosionSystem(world *engine.World) engine.System {
	s := &ExplosionSystem{
		world: world,
	}

	s.statTriggered = world.Resource.Status.Ints.Get("explosion.triggered")
	s.statConverted = world.Resource.Status.Ints.Get("explosion.converted")

	s.Init()
	return s
}

func (s *ExplosionSystem) Init() {
	s.statTriggered.Store(0)
	s.statConverted.Store(0)
	s.enabled = true
}

func (s *ExplosionSystem) Priority() int {
	return constant.PriorityExplosion
}

func (s *ExplosionSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFireSpecialRequest,
		event.EventExplosionRequest,
		event.EventGameReset,
	}
}

func (s *ExplosionSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventFireSpecialRequest:
		// Fire explosions at ALL dust positions
		dustEntities := s.world.Component.Dust.All()
		if len(dustEntities) == 0 {
			return
		}

		// Collect unique positions (multiple dust can share cell)
		type pos struct{ x, y int }
		positions := make(map[pos]bool, len(dustEntities))

		for _, e := range dustEntities {
			if p, ok := s.world.Position.Get(e); ok {
				positions[pos{p.X, p.Y}] = true
			}
		}

		// Destroy all dust first (they become explosions)
		event.EmitDeathBatch(s.world.Resource.Event.Queue, 0, dustEntities, s.world.Resource.Time.FrameNumber)

		// Trigger explosion at each unique position
		for p := range positions {
			s.triggerExplosion(p.x, p.y, constant.ExplosionRadius)
		}

	case event.EventExplosionRequest:
		if p, ok := ev.Payload.(*event.ExplosionRequestPayload); ok {
			radius := p.Radius
			if radius == 0 {
				radius = constant.ExplosionRadius
			}
			s.triggerExplosion(p.X, p.Y, radius)
		}
	}
}

func (s *ExplosionSystem) triggerExplosion(centerX, centerY int, radius int64) {
	config := s.world.Resource.Config
	frame := s.world.Resource.Time.FrameNumber

	// Bounding box in grid cells (aspect-corrected: visual Y is 2x grid Y)
	radiusCells := vmath.ToInt(radius)
	radiusCellsY := radiusCells / 2 // Aspect correction for bounding box

	minX := centerX - radiusCells
	maxX := centerX + radiusCells
	minY := centerY - radiusCellsY
	maxY := centerY + radiusCellsY

	// Clamp to screen
	if minX < 0 {
		minX = 0
	}
	if maxX >= config.GameWidth {
		maxX = config.GameWidth - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= config.GameHeight {
		maxY = config.GameHeight - 1
	}

	// Precompute radius squared for containment check
	radiusSq := vmath.Mul(radius, radius)

	// Collect candidates
	type candidate struct {
		entity core.Entity
		x, y   int
		char   rune
		level  component.GlyphLevel
	}
	candidates := make([]candidate, 0, 64)

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			// Aspect-corrected distance check
			dx := vmath.FromInt(x - centerX)
			dy := vmath.FromInt(y - centerY)
			dyCirc := vmath.ScaleToCircular(dy)
			distSq := vmath.CircleDistSq(dx, dyCirc)

			if distSq > radiusSq {
				continue
			}

			entities := s.world.Position.GetAllAt(x, y)
			for _, e := range entities {
				// Filter: must have Glyph, must not be composite member
				glyph, hasGlyph := s.world.Component.Glyph.Get(e)
				if !hasGlyph {
					continue
				}
				if s.world.Component.Member.Has(e) {
					continue
				}

				candidates = append(candidates, candidate{
					entity: e,
					x:      x,
					y:      y,
					char:   glyph.Rune,
					level:  glyph.Level,
				})
			}
		}
	}

	if len(candidates) == 0 {
		// Still create visual even with no targets
		s.createVisualEntity(centerX, centerY, radius)
		s.statTriggered.Add(1)
		return
	}

	// Batch death (silent - no flash, dust handles visual)
	deathEntities := make([]core.Entity, len(candidates))
	for i, c := range candidates {
		deathEntities[i] = c.entity
	}
	event.EmitDeathBatch(s.world.Resource.Event.Queue, 0, deathEntities, frame)

	// Batch dust spawn
	dustBatch := event.AcquireDustSpawnBatch()
	for _, c := range candidates {
		dustBatch.Entries = append(dustBatch.Entries, event.DustSpawnEntry{
			X:     c.x,
			Y:     c.y,
			Char:  c.char,
			Level: c.level,
		})
	}
	s.world.PushEvent(event.EventDustSpawnBatch, dustBatch)

	// Create visual explosion entity
	s.createVisualEntity(centerX, centerY, radius)

	s.statTriggered.Add(1)
	s.statConverted.Add(int64(len(candidates)))
}

func (s *ExplosionSystem) createVisualEntity(centerX, centerY int, radius int64) {
	entity := s.world.CreateEntity()

	s.world.Component.Explosion.Set(entity, component.ExplosionComponent{
		CenterX:       centerX,
		CenterY:       centerY,
		MaxRadius:     radius,
		CurrentRadius: 0,
		Duration:      constant.ExplosionDuration,
		Age:           0,
	})
}

func (s *ExplosionSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resource.Time.DeltaTime
	entities := s.world.Component.Explosion.All()

	for _, entity := range entities {
		exp, ok := s.world.Component.Explosion.Get(entity)
		if !ok {
			continue
		}

		exp.Age += dt

		if exp.Age >= exp.Duration {
			s.world.Component.Explosion.Remove(entity)
			s.world.DestroyEntity(entity)
			continue
		}

		// Expand radius: linear interpolation from 0 to MaxRadius
		progress := vmath.FromFloat(float64(exp.Age) / float64(exp.Duration))
		exp.CurrentRadius = vmath.Mul(exp.MaxRadius, progress)

		s.world.Component.Explosion.Set(entity, exp)
	}
}
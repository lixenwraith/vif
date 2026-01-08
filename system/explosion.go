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

// ExplosionCenter represents a single explosion for rendering
// Written by ExplosionSystem, read by ExplosionRenderer
type ExplosionCenter struct {
	X, Y      int
	Radius    int64 // Q32.32 cells
	Intensity int64 // Q32.32, Scale = 1.0 base
	Age       int64 // Nanoseconds since spawn
}

// Package-level state for renderer access
// System writes, renderer reads - no synchronization needed (single-threaded game loop)
var (
	ExplosionCenters      []ExplosionCenter                            // Active slice view
	ExplosionDurationNano int64                                        // For decay calculation
	explosionBacking      [constant.ExplosionCenterCap]ExplosionCenter // Pre-allocated storage
)

// ExplosionSystem handles explosion triggering and glyph-to-dust transformation
type ExplosionSystem struct {
	world *engine.World

	activeCount int   // Number of active centers
	baseRadius  int64 // Default radius Q32.32
	radiusCap   int64 // Maximum radius after merges Q32.32

	// Reusable buffers to avoid allocation in hot path
	entityBuf    []core.Entity
	dustEntryBuf []event.DustSpawnEntry

	statTriggered *atomic.Int64
	statConverted *atomic.Int64
	statMerged    *atomic.Int64

	enabled bool
}

func NewExplosionSystem(world *engine.World) engine.System {
	s := &ExplosionSystem{
		world: world,
	}

	s.statTriggered = world.Resource.Status.Ints.Get("explosion.triggered")
	s.statConverted = world.Resource.Status.Ints.Get("explosion.converted")
	s.statMerged = world.Resource.Status.Ints.Get("explosion.merged")

	s.Init()
	return s
}

func (s *ExplosionSystem) Init() {
	s.activeCount = 0
	s.baseRadius = constant.ExplosionFieldRadius
	s.radiusCap = constant.ExplosionRadiusCapFixed

	// Initialize package-level state
	ExplosionDurationNano = constant.ExplosionFieldDuration.Nanoseconds()
	ExplosionCenters = explosionBacking[:0]

	// Reset buffers
	s.entityBuf = make([]core.Entity, 0, 256)
	s.dustEntryBuf = make([]event.DustSpawnEntry, 0, 256)

	s.statTriggered.Store(0)
	s.statConverted.Store(0)
	s.statMerged.Store(0)
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
		s.fireFromDust()

	case event.EventExplosionRequest:
		if p, ok := ev.Payload.(*event.ExplosionRequestPayload); ok {
			radius := p.Radius
			if radius == 0 {
				radius = s.baseRadius
			}
			s.addCenter(p.X, p.Y, radius)
		}
	}
}

func (s *ExplosionSystem) fireFromDust() {
	dustEntities := s.world.Component.Dust.All()
	if len(dustEntities) == 0 {
		return
	}

	type pos struct{ x, y int }
	positions := make(map[pos]bool, len(dustEntities))

	for _, e := range dustEntities {
		if p, ok := s.world.Position.Get(e); ok {
			positions[pos{p.X, p.Y}] = true
		}
	}

	event.EmitDeathBatch(s.world.Resource.Event.Queue, 0, dustEntities, s.world.Resource.Time.FrameNumber)

	for p := range positions {
		s.addCenter(p.x, p.y, s.baseRadius)
	}
}

func (s *ExplosionSystem) addCenter(x, y int, radius int64) {
	centerX := vmath.FromInt(x)
	centerY := vmath.FromInt(y)

	// Merge check
	for i := 0; i < s.activeCount; i++ {
		c := &explosionBacking[i]
		dx := centerX - vmath.FromInt(c.X)
		dy := centerY - vmath.FromInt(c.Y)
		distSq := vmath.Mul(dx, dx) + vmath.Mul(dy, dy)

		if distSq <= constant.ExplosionMergeThresholdSq {
			c.Age = 0
			c.Intensity += constant.ExplosionIntensityBoost
			if c.Intensity > constant.ExplosionIntensityCap {
				c.Intensity = constant.ExplosionIntensityCap
			}
			newRadius := c.Radius
			if radius > newRadius {
				newRadius = radius
			}
			newRadius += constant.ExplosionRadiusBoost
			if newRadius > s.radiusCap {
				newRadius = s.radiusCap
			}
			c.Radius = newRadius

			s.statMerged.Add(1)
			return
		}
	}

	// No merge - add new center
	var idx int
	if s.activeCount < constant.ExplosionCenterCap {
		idx = s.activeCount
		s.activeCount++
	} else {
		// Overflow: overwrite oldest
		idx = 0
		maxAge := explosionBacking[0].Age
		for i := 1; i < constant.ExplosionCenterCap; i++ {
			if explosionBacking[i].Age > maxAge {
				maxAge = explosionBacking[i].Age
				idx = i
			}
		}
	}

	explosionBacking[idx] = ExplosionCenter{
		X:         x,
		Y:         y,
		Radius:    radius,
		Intensity: vmath.Scale,
		Age:       0,
	}

	// Update exported slice
	ExplosionCenters = explosionBacking[:s.activeCount]

	s.transformGlyphs(x, y, radius)
	s.statTriggered.Add(1)
}

// TODO: this conversion must be done in dust system, doing it here results in no telemetry and duplicate logic
func (s *ExplosionSystem) transformGlyphs(centerX, centerY int, radius int64) {
	config := s.world.Resource.Config
	frame := s.world.Resource.Time.FrameNumber

	// Radius is horizontal cells; Vertical is half that to maintain aspect ratio
	radiusCells := vmath.ToInt(radius)
	radiusCellsY := radiusCells / 2

	minX := centerX - radiusCells
	maxX := centerX + radiusCells
	minY := centerY - radiusCellsY
	maxY := centerY + radiusCellsY

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

	radiusSq := vmath.Mul(radius, radius)

	// Clear reuse buffers
	s.entityBuf = s.entityBuf[:0]
	s.dustEntryBuf = s.dustEntryBuf[:0]

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := vmath.FromInt(x - centerX)
			dy := vmath.FromInt(y - centerY)
			dyCirc := vmath.ScaleToCircular(dy)
			distSq := vmath.CircleDistSq(dx, dyCirc)

			if distSq > radiusSq {
				continue
			}

			entities := s.world.Position.GetAllAt(x, y)
			for _, e := range entities {
				if s.world.Component.Member.Has(e) {
					continue
				}

				glyph, hasGlyph := s.world.Component.Glyph.Get(e)
				if !hasGlyph {
					continue
				}

				// Set death component for deduplication
				if s.world.Component.Death.Has(e) {
					continue
				}
				s.world.Component.Death.Set(e, component.DeathComponent{})

				// Append to local buffers
				s.entityBuf = append(s.entityBuf, e)
				s.dustEntryBuf = append(s.dustEntryBuf, event.DustSpawnEntry{
					X:     x,
					Y:     y,
					Char:  glyph.Rune,
					Level: glyph.Level,
				})
			}
		}
	}

	if len(s.entityBuf) == 0 {
		return
	}

	// Use buffered entities for death batch
	event.EmitDeathBatch(s.world.Resource.Event.Queue, 0, s.entityBuf, frame)

	// Use buffered entries for dust spawn
	// We must acquire a pool object for the event payload, but we copy from our buffer
	dustBatch := event.AcquireDustSpawnBatch()
	dustBatch.Entries = append(dustBatch.Entries, s.dustEntryBuf...)

	s.world.PushEvent(event.EventDustSpawnBatch, dustBatch)

	s.statConverted.Add(int64(len(s.entityBuf)))
}

func (s *ExplosionSystem) Update() {
	if !s.enabled || s.activeCount == 0 {
		return
	}

	dtNano := s.world.Resource.Time.DeltaTime.Nanoseconds()

	write := 0
	for i := 0; i < s.activeCount; i++ {
		explosionBacking[i].Age += dtNano
		if explosionBacking[i].Age < ExplosionDurationNano {
			if write != i {
				explosionBacking[write] = explosionBacking[i]
			}
			write++
		}
	}
	s.activeCount = write
	ExplosionCenters = explosionBacking[:s.activeCount]
}
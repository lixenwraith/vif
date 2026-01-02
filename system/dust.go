package system

// @lixen: #dev{feature[dust(render,system)]}

import (
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// DustSystem manages orbital dust particles created from glyph transformation
// Dust orbits cursor with chase behavior on large cursor movements
type DustSystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	dustStore   *engine.Store[component.DustComponent]
	glyphStore  *engine.Store[component.GlyphComponent]
	memberStore *engine.Store[component.MemberComponent]
	sigilStore  *engine.Store[component.SigilComponent]

	// Event state tracking
	quasarActive bool

	// Cursor tracking for chase detection
	lastCursorX int
	lastCursorY int

	// Random source for orbit radius and direction
	rng *vmath.FastRand

	// Telemetry
	statCreated   *atomic.Int64
	statActive    *atomic.Int64
	statDestroyed *atomic.Int64

	enabled bool
}

func NewDustSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &DustSystem{
		world: world,
		res:   res,

		dustStore:   engine.GetStore[component.DustComponent](world),
		glyphStore:  engine.GetStore[component.GlyphComponent](world),
		memberStore: engine.GetStore[component.MemberComponent](world),
		sigilStore:  engine.GetStore[component.SigilComponent](world),

		rng: vmath.NewFastRand(uint32(res.Time.RealTime.UnixNano())),

		statCreated:   res.Status.Ints.Get("dust.created"),
		statActive:    res.Status.Ints.Get("dust.active"),
		statDestroyed: res.Status.Ints.Get("dust.destroyed"),
	}
	s.initLocked()
	return s
}

func (s *DustSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

func (s *DustSystem) initLocked() {
	s.quasarActive = false
	s.lastCursorX = 0
	s.lastCursorY = 0
	s.statCreated.Store(0)
	s.statActive.Store(0)
	s.statDestroyed.Store(0)
	s.enabled = true
}

func (s *DustSystem) Priority() int {
	return constant.PriorityDust
}

func (s *DustSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventQuasarSpawned,
		event.EventQuasarDestroyed,
		event.EventGoldComplete,
		event.EventGameReset,
	}
}

func (s *DustSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventQuasarSpawned:
		s.mu.Lock()
		s.quasarActive = true
		s.mu.Unlock()

	case event.EventQuasarDestroyed:
		s.mu.Lock()
		s.quasarActive = false
		s.mu.Unlock()

	case event.EventGoldComplete:
		s.mu.RLock()
		active := s.quasarActive
		s.mu.RUnlock()

		if active {
			s.transformGlyphsToDust()
		}
	}
}

// transformGlyphsToDust converts all non-composite glyphs to dust entities
func (s *DustSystem) transformGlyphsToDust() {
	cursorEntity := s.res.Cursor.Entity
	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	// Cache glyph data before destruction
	type glyphData struct {
		entity core.Entity
		x, y   int
		char   rune
		level  component.GlyphLevel
	}

	glyphEntities := s.glyphStore.All()
	toTransform := make([]glyphData, 0, len(glyphEntities))

	for _, entity := range glyphEntities {
		// Skip composite members
		if s.memberStore.Has(entity) {
			continue
		}

		pos, hasPos := s.world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		glyph, hasGlyph := s.glyphStore.Get(entity)
		if !hasGlyph {
			continue
		}

		toTransform = append(toTransform, glyphData{
			entity: entity,
			x:      pos.X,
			y:      pos.Y,
			char:   glyph.Rune,
			level:  glyph.Level,
		})
	}

	if len(toTransform) == 0 {
		return
	}

	// Emit batch death with flash effect
	deathEntities := make([]core.Entity, len(toTransform))
	for i, gd := range toTransform {
		deathEntities[i] = gd.entity
	}
	event.EmitDeathBatch(s.res.Events.Queue, event.EventFlashRequest, deathEntities, s.res.Time.FrameNumber)

	// Create dust entities at cached positions
	for _, gd := range toTransform {
		s.spawnDust(gd.x, gd.y, gd.char, gd.level, cursorPos.X, cursorPos.Y)
	}

	s.statCreated.Add(int64(len(toTransform)))
}

// spawnDust creates a single dust entity with orbital initialization
func (s *DustSystem) spawnDust(x, y int, char rune, level component.GlyphLevel, cursorX, cursorY int) {
	entity := s.world.CreateEntity()

	// Random orbit radius in [min, max]
	radiusRange := int(constant.DustOrbitRadiusMax - constant.DustOrbitRadiusMin)
	orbitRadius := constant.DustOrbitRadiusMin
	if radiusRange > 0 {
		orbitRadius += int32(s.rng.Intn(radiusRange))
	}

	// Position relative to cursor for orbital calculation
	dx := vmath.FromInt(x - cursorX)
	dy := vmath.FromInt(y - cursorY)

	// Initial tangential velocity for orbit, random direction
	clockwise := s.rng.Intn(2) == 0
	vx, vy := vmath.OrbitalInsert(dx, dy, constant.DustAttractionBase, clockwise)

	// Scale to initial speed
	mag := vmath.Magnitude(vx, vy)
	if mag > 0 {
		vx = vmath.Mul(vmath.Div(vx, mag), constant.DustInitialSpeed)
		vy = vmath.Mul(vmath.Div(vy, mag), constant.DustInitialSpeed)
	}

	// Grid position
	s.world.Positions.Set(entity, component.PositionComponent{X: x, Y: y})

	// Dust component with kinetic state
	s.dustStore.Set(entity, component.DustComponent{
		KineticState: component.KineticState{
			PreciseX: vmath.FromInt(x),
			PreciseY: vmath.FromInt(y),
			VelX:     vx,
			VelY:     vy,
		},
		Level:       level,
		OrbitRadius: orbitRadius,
		ChaseBoost:  vmath.Scale,
		Rune:        char,
		LastIntX:    x,
		LastIntY:    y,
	})

	// Sigil for rendering - map level to grayscale
	var color component.SigilColor
	switch level {
	case component.GlyphDark:
		color = component.SigilDustDark
	case component.GlyphNormal:
		color = component.SigilDustNormal
	case component.GlyphBright:
		color = component.SigilDustBright
	default:
		color = component.SigilDustNormal
	}

	s.sigilStore.Set(entity, component.SigilComponent{
		Rune:  char,
		Color: color,
	})
}

func (s *DustSystem) Update() {
	if !s.enabled {
		return
	}

	dustEntities := s.dustStore.All()
	if len(dustEntities) == 0 {
		s.statActive.Store(0)
		return
	}

	cursorEntity := s.res.Cursor.Entity
	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	// Detect cursor jump for chase boost
	s.mu.Lock()
	cursorDeltaX := cursorPos.X - s.lastCursorX
	cursorDeltaY := cursorPos.Y - s.lastCursorY
	s.lastCursorX = cursorPos.X
	s.lastCursorY = cursorPos.Y
	s.mu.Unlock()

	// Check if cursor moved significantly
	cursorDist := vmath.DistanceApprox(vmath.FromInt(cursorDeltaX), vmath.FromInt(cursorDeltaY))
	applyChaseBoost := cursorDist > vmath.FromInt(constant.DustChaseThreshold)

	dtFixed := vmath.FromFloat(s.res.Time.DeltaTime.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	config := s.res.Config
	cursorXFixed := vmath.FromInt(cursorPos.X)
	cursorYFixed := vmath.FromInt(cursorPos.Y)

	for _, entity := range dustEntities {
		dust, ok := s.dustStore.Get(entity)
		if !ok {
			continue
		}

		// Position relative to cursor - transform to circular space for physics
		dx := dust.PreciseX - cursorXFixed
		dy := dust.PreciseY - cursorYFixed
		dyCirc := vmath.ScaleToCircular(dy)

		// Chase boost: apply on large cursor delta, decay over time
		if applyChaseBoost {
			dust.ChaseBoost = constant.DustChaseBoost
		} else if dust.ChaseBoost > vmath.Scale {
			decay := vmath.Mul(constant.DustChaseDecay, dtFixed)
			dust.ChaseBoost -= decay
			if dust.ChaseBoost < vmath.Scale {
				dust.ChaseBoost = vmath.Scale
			}
		}

		// All physics in circular space
		// Transform velocity to circular space
		velYCirc := vmath.ScaleToCircular(dust.VelY)

		// Orbital attraction toward cursor (boosted) in circular space
		attraction := vmath.Mul(constant.DustAttractionBase, dust.ChaseBoost)
		ax, ay := vmath.OrbitalAttraction(dx, dyCirc, attraction)

		// Manual integration: accel → velocity → damp → position
		// Update velocity in circular space
		dust.VelX += vmath.Mul(ax, dtFixed)
		velYCirc += vmath.Mul(ay, dtFixed)

		// Dampen radial velocity to circularize orbit (circular space)
		dust.VelX, velYCirc = vmath.OrbitalDamp(
			dust.VelX, velYCirc,
			dx, dyCirc,
			constant.DustDamping, dtFixed,
		)

		// Transform velocity back to display (elliptical) space
		dust.VelY = vmath.ScaleFromCircular(velYCirc)

		// Integrate position
		dust.PreciseX += vmath.Mul(dust.VelX, dtFixed)
		dust.PreciseY += vmath.Mul(dust.VelY, dtFixed)

		newX := vmath.ToInt(dust.PreciseX)
		newY := vmath.ToInt(dust.PreciseY)

		// Boundary reflection with damping
		if newX < 0 {
			newX = 0
			dust.PreciseX = 0
			dust.VelX = -dust.VelX / 2
		} else if newX >= config.GameWidth {
			newX = config.GameWidth - 1
			dust.PreciseX = vmath.FromInt(newX)
			dust.VelX = -dust.VelX / 2
		}

		if newY < 0 {
			newY = 0
			dust.PreciseY = 0
			dust.VelY = -dust.VelY / 2
		} else if newY >= config.GameHeight {
			newY = config.GameHeight - 1
			dust.PreciseY = vmath.FromInt(newY)
			dust.VelY = -dust.VelY / 2
		}

		// Update grid position if cell changed
		if newX != dust.LastIntX || newY != dust.LastIntY {
			dust.LastIntX = newX
			dust.LastIntY = newY
			s.world.Positions.Set(entity, component.PositionComponent{X: newX, Y: newY})
		}

		s.dustStore.Set(entity, dust)
	}

	s.statActive.Store(int64(len(dustEntities)))
}
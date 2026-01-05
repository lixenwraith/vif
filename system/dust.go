package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// DustSystem manages orbital dust particles created from glyph transformation
// Dust orbits cursor with chase behavior on large cursor movements
type DustSystem struct {
	world *engine.World

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
	s := &DustSystem{
		world: world,
	}

	s.rng = vmath.NewFastRand(uint32(world.Resource.Time.RealTime.UnixNano()))

	s.statCreated = world.Resource.Status.Ints.Get("dust.created")
	s.statActive = world.Resource.Status.Ints.Get("dust.active")
	s.statDestroyed = world.Resource.Status.Ints.Get("dust.destroyed")

	s.Init()
	return s
}

func (s *DustSystem) Init() {
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
		s.quasarActive = true

	case event.EventQuasarDestroyed:
		s.quasarActive = false

	case event.EventGoldComplete:
		active := s.quasarActive

		if active {
			s.transformGlyphsToDust()
		}
	}
}

// transformGlyphsToDust converts all non-composite glyphs to dust entities
func (s *DustSystem) transformGlyphsToDust() {
	cursorEntity := s.world.Resource.Cursor.Entity
	cursorPos, ok := s.world.Position.Get(cursorEntity)
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

	glyphEntities := s.world.Component.Glyph.All()
	toTransform := make([]glyphData, 0, len(glyphEntities))

	for _, entity := range glyphEntities {
		// Skip composite members
		if s.world.Component.Member.Has(entity) {
			continue
		}

		pos, hasPos := s.world.Position.Get(entity)
		if !hasPos {
			continue
		}

		glyph, hasGlyph := s.world.Component.Glyph.Get(entity)
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
	event.EmitDeathBatch(s.world.Resource.Event.Queue, 0, deathEntities, s.world.Resource.Time.FrameNumber)

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
	s.world.Position.Set(entity, component.PositionComponent{X: x, Y: y})

	// Dust component with kinetic state
	s.world.Component.Dust.Set(entity, component.DustComponent{
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

	// Protection from drain and quasar destruction
	s.world.Component.Protection.Set(entity, component.ProtectionComponent{
		Mask:      component.ProtectFromDrain,
		ExpiresAt: 0, // Permanent
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

	s.world.Component.Sigil.Set(entity, component.SigilComponent{
		Rune:  char,
		Color: color,
	})
}
func (s *DustSystem) Update() {
	if !s.enabled {
		return
	}

	dustEntities := s.world.Component.Dust.All()
	if len(dustEntities) == 0 {
		s.statActive.Store(0)
		return
	}

	cursorEntity := s.world.Resource.Cursor.Entity
	cursorPos, ok := s.world.Position.Get(cursorEntity)
	if !ok {
		return
	}

	// Fetch energy once for attraction gating
	energyComp, _ := s.world.Component.Energy.Get(cursorEntity)
	cursorEnergy := energyComp.Current.Load()
	hasAttraction := cursorEnergy != 0

	// Shield data for collision energy reward
	shield, shieldOk := s.world.Component.Shield.Get(cursorEntity)
	shieldActive := shieldOk && shield.Active

	// Heat for energy calculation
	var heat int
	if hc, ok := s.world.Component.Heat.Get(cursorEntity); ok {
		heat = int(hc.Current.Load())
	}

	// Detect cursor jump for chase boost
	cursorDeltaX := cursorPos.X - s.lastCursorX
	cursorDeltaY := cursorPos.Y - s.lastCursorY
	s.lastCursorX = cursorPos.X
	s.lastCursorY = cursorPos.Y

	// Check if cursor moved significantly
	cursorDist := vmath.DistanceApprox(vmath.FromInt(cursorDeltaX), vmath.FromInt(cursorDeltaY))
	applyChaseBoost := cursorDist > vmath.FromInt(constant.DustChaseThreshold)

	dtFixed := vmath.FromFloat(s.world.Resource.Time.DeltaTime.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	config := s.world.Resource.Config
	cursorXFixed := vmath.FromInt(cursorPos.X)
	cursorYFixed := vmath.FromInt(cursorPos.Y)
	now := s.world.Resource.Time.GameTime

	// Dynamic speed multiplier based on entity count
	// Increased boost scaling (4x) to make low-count survival more frantic/powerful
	dustCount := len(dustEntities)
	boostMax := vmath.Mul(constant.DustBoostMax, vmath.FromInt(4))
	speedMultiplier := vmath.ExpDecayScaled(dustCount, boostMax)

	// Track destroyed count for telemetry
	var destroyedCount int64
	// Batch death candidates to reduce event pressure and GC
	deathCandidates := make([]core.Entity, 0, 32)

	for _, entity := range dustEntities {
		dust, ok := s.world.Component.Dust.Get(entity)
		if !ok {
			continue
		}

		// Store old position for swept collision
		oldX, oldY := dust.PreciseX, dust.PreciseY

		// Position relative to cursor - transform to circular space for physics
		dx := dust.PreciseX - cursorXFixed
		dy := dust.PreciseY - cursorYFixed
		dyCirc := vmath.ScaleToCircular(dy)

		// Pre-compute shield containment for this dust
		dustInsideShield := shieldActive && vmath.EllipseContains(dx, dy, shield.InvRxSq, shield.InvRySq)

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

		// Energy-gated attraction and damping
		if hasAttraction {
			// All physics in circular space
			velYCirc := vmath.ScaleToCircular(dust.VelY)

			// Dynamic orbital attraction toward cursor (boosted, circular space, speed multiplier)
			attraction := vmath.Mul(constant.DustAttractionBase, dust.ChaseBoost)
			attraction = vmath.Mul(attraction, speedMultiplier)
			ax, ay := vmath.OrbitalAttraction(dx, dyCirc, attraction)

			// Update velocity in circular space
			dust.VelX += vmath.Mul(ax, dtFixed)
			velYCirc += vmath.Mul(ay, dtFixed)

			// Dynamic dampen radial velocity to circularize orbit (circular space, scales with speed multiplier)
			effectiveDamping := vmath.Mul(constant.DustDamping, speedMultiplier)
			dust.VelX, velYCirc = vmath.OrbitalDamp(
				dust.VelX, velYCirc,
				dx, dyCirc,
				effectiveDamping, dtFixed,
			)

			// Transform velocity back to display (elliptical) space
			dust.VelY = vmath.ScaleFromCircular(velYCirc)
		}
		// When hasAttraction == false: velocity unchanged, dust scatters on momentum

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

		// Soft shield-edge redirection
		if shieldActive && dust.WasInsideShield && !dustInsideShield {
			redirectX, redirectY := vmath.Normalize2D(-dx, -dy)
			dust.VelX += vmath.Mul(redirectX, constant.DustShieldRedirect)
			dust.VelY += vmath.Mul(redirectY, constant.DustShieldRedirect)
		}
		dust.WasInsideShield = dustInsideShield

		// Swept collision detection
		destroyDust := false
		var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

		vmath.Traverse(oldX, oldY, dust.PreciseX, dust.PreciseY, func(x, y int) bool {
			// Skip previous frame's cell
			if x == dust.LastIntX && y == dust.LastIntY {
				return true
			}

			// Bounds check
			if x < 0 || x >= config.GameWidth || y < 0 || y >= config.GameHeight {
				return true
			}

			n := s.world.Position.GetAllAtInto(x, y, collisionBuf[:])
			for i := 0; i < n; i++ {
				target := collisionBuf[i]
				if target == 0 || target == entity {
					continue
				}

				// 1. Skip entities already marked for death this frame
				// This prevents duplicate event emission and processing when multiple dust particles hit the same target
				if s.world.Component.Death.Has(target) {
					continue
				}

				// 2. Drain deflection - dust passes through, imparts momentum
				if s.world.Component.Drain.Has(target) {
					if drain, ok := s.world.Component.Drain.Get(target); ok {
						physics.ApplyCollision(
							&drain.KineticState,
							dust.VelX, dust.VelY,
							&physics.DustToDrain,
							s.rng,
							now,
						)
						s.world.Component.Drain.Set(target, drain)
					}
					continue // Dust passes through
				}

				// 3. Quasar member deflection - dust passes through, imparts momentum
				if member, ok := s.world.Component.Member.Get(target); ok {
					if header, hOk := s.world.Component.Header.Get(member.AnchorID); hOk {
						if header.BehaviorID == component.BehaviorQuasar {
							if quasar, qOk := s.world.Component.Quasar.Get(member.AnchorID); qOk {
								// Get hit offset from anchor
								anchorPos, _ := s.world.Position.Get(member.AnchorID)
								offsetX := x - anchorPos.X
								offsetY := y - anchorPos.Y

								physics.ApplyOffsetCollision(
									&quasar.KineticState,
									dust.VelX, dust.VelY,
									offsetX, offsetY,
									&physics.DustToQuasar,
									s.rng,
									now,
								)
								s.world.Component.Quasar.Set(member.AnchorID, quasar)
							}
						}
					}
					continue // Dust passes through composites
				}

				// 3. Blossom/Decay - dust destroys target
				if s.world.Component.Blossom.Has(target) || s.world.Component.Decay.Has(target) {
					// Mark as dead immediately to prevent double-processing by other dust
					s.world.Component.Death.Set(target, component.DeathComponent{})
					deathCandidates = append(deathCandidates, target)
					continue // Dust survives
				}

				// 4. Glyph collision
				glyph, ok := s.world.Component.Glyph.Get(target)
				if !ok {
					continue
				}

				// Level match required
				if glyph.Level != dust.Level {
					continue
				}

				// Glyph die
				s.world.Component.Death.Set(target, component.DeathComponent{})
				deathCandidates = append(deathCandidates, target)

				// Dust survives if inside shield, dies otherwise
				if !dustInsideShield {
					destroyDust = true
				}

				// Energy reward when protected dust destroys glyph
				if dustInsideShield {
					var energyDelta int
					switch glyph.Type {
					case component.GlyphGreen:
						energyDelta = heat
					case component.GlyphBlue:
						energyDelta = heat * 2
					case component.GlyphRed:
						energyDelta = -heat
					}
					if energyDelta != 0 {
						s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
							Delta: energyDelta,
						})
					}
				}

				if destroyDust {
					return false // Stop traversal
				}
				// Continue traversal if dust survived (inside shield or Wave kill)
			}

			return true // Continue traversal
		})

		if destroyDust {
			s.world.Component.Death.Set(entity, component.DeathComponent{})
			destroyedCount++
			continue // Skip grid sync for dying entity
		}

		// Update grid position if cell changed
		if newX != dust.LastIntX || newY != dust.LastIntY {
			dust.LastIntX = newX
			dust.LastIntY = newY
			s.world.Position.Set(entity, component.PositionComponent{X: newX, Y: newY})
		}

		s.world.Component.Dust.Set(entity, dust)
	}

	// Emit batch death events with Flash effect
	// Death system will extract the character from components for the visual effect
	if len(deathCandidates) > 0 {
		event.EmitDeathBatch(s.world.Resource.Event.Queue, event.EventFlashRequest, deathCandidates, s.world.Resource.Time.FrameNumber)
	}

	s.statActive.Store(int64(len(dustEntities)))
}
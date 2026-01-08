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

	s.rng = vmath.NewFastRand(uint64(world.Resource.Time.RealTime.UnixNano()))

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
		event.EventDustSpawnOne,
		event.EventDustSpawnBatch,
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
	case event.EventDustSpawnOne:
		if p, ok := ev.Payload.(*event.DustSpawnPayload); ok {
			cursorEntity := s.world.Resource.Cursor.Entity
			cursorPos, ok := s.world.Position.Get(cursorEntity)
			if !ok {
				return
			}
			s.spawnDust(p.X, p.Y, p.Char, p.Level, cursorPos.X, cursorPos.Y)
			s.statCreated.Add(1)
		}

	case event.EventDustSpawnBatch:
		// Optimized batch handling with CommitForce and shared logic
		if p, ok := ev.Payload.(*event.DustSpawnBatchPayload); ok {
			count := len(p.Entries)
			if count == 0 {
				event.ReleaseDustSpawnBatch(p)
				return
			}

			cursorEntity := s.world.Resource.Cursor.Entity
			cursorPos, ok := s.world.Position.Get(cursorEntity)
			if !ok {
				event.ReleaseDustSpawnBatch(p)
				return
			}

			// OPTIMIZATION: Use PositionBatch to lock the spatial grid once for all new entities
			posBatch := s.world.Position.BeginBatch()

			for i := 0; i < count; i++ {
				entry := p.Entries[i]
				entity := s.world.CreateEntity()

				// Use helper for physics/component generation
				dust, prot, sigil := s.prepareDustComponents(entry.X, entry.Y, entry.Char, entry.Level, cursorPos.X, cursorPos.Y)

				// Add to batches
				posBatch.Add(entity, component.PositionComponent{X: entry.X, Y: entry.Y})
				s.world.Component.Dust.Set(entity, dust)
				s.world.Component.Protection.Set(entity, prot)
				s.world.Component.Sigil.Set(entity, sigil)
			}

			// Force commit because dust often spawns on top of dying glyphs (DeathSystem runs later)
			posBatch.CommitForce()

			s.statCreated.Add(int64(count))
			event.ReleaseDustSpawnBatch(p)
		}

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

func (s *DustSystem) Update() {
	if !s.enabled {
		return
	}

	dustEntities := s.world.Component.Dust.All()
	if len(dustEntities) == 0 {
		s.statActive.Store(0)
		return
	}

	// 1. PRE-FETCH Context Data (Cursor, Energy, etc.)
	// Must do this BEFORE locking Position to avoid deadlock
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

	// Chase boost on cursor jump
	cursorDeltaX := cursorPos.X - s.lastCursorX
	cursorDeltaY := cursorPos.Y - s.lastCursorY
	s.lastCursorX = cursorPos.X
	s.lastCursorY = cursorPos.Y

	cursorDisplacement := vmath.DistanceApprox(vmath.FromInt(cursorDeltaX), vmath.FromInt(cursorDeltaY))
	applyChaseBoost := cursorDisplacement > vmath.FromInt(constant.DustChaseThreshold)

	// 2. SETUP Physics Constants
	dtFixed := vmath.FromFloat(s.world.Resource.Time.DeltaTime.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	config := s.world.Resource.Config
	cursorXFixed := vmath.FromInt(cursorPos.X)
	cursorYFixed := vmath.FromInt(cursorPos.Y)
	now := s.world.Resource.Time.GameTime

	// 3. LOCK Spatial Grid (Global Batch Lock)
	// Critical optimization: eliminates ~4000 mutex ops per frame
	s.world.Position.Lock()
	defer s.world.Position.Unlock()

	var destroyedCount int64
	deathCandidates := make([]core.Entity, 0, 32)
	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	// 4. MAIN LOOP
	for _, entity := range dustEntities {
		dust, ok := s.world.Component.Dust.Get(entity)
		if !ok {
			continue
		}

		// --- Position and Shield State ---
		dx := dust.PreciseX - cursorXFixed
		dy := dust.PreciseY - cursorYFixed

		dustInsideShield := shieldActive && vmath.EllipseContains(dx, dy, shield.InvRxSq, shield.InvRySq)

		// --- Per-Particle Jitter (always active) ---
		jitterAngle := int64(s.rng.Intn(vmath.LUTSize)) << (vmath.Shift - 10)
		dust.VelX += vmath.Mul(vmath.Cos(jitterAngle), constant.DustJitter)
		dust.VelY += vmath.Mul(vmath.Sin(jitterAngle), constant.DustJitter)

		// --- Orbital Physics (only when energy != 0 / shield active) ---
		if hasAttraction {
			// Chase boost decay
			if applyChaseBoost {
				dust.ChaseBoost = constant.DustChaseBoost
			} else if dust.ChaseBoost > vmath.Scale {
				decay := vmath.Mul(constant.DustChaseDecay, dtFixed)
				dust.ChaseBoost -= decay
				if dust.ChaseBoost < vmath.Scale {
					dust.ChaseBoost = vmath.Scale
				}
			}

			// Equilibrium-seeking force toward target orbit radius
			// Scale Y to circular space for visually circular orbit
			stiffness := vmath.Mul(constant.DustAttractionBase, dust.ChaseBoost)
			dyCirc := vmath.ScaleToCircular(dy)
			ax, ayCirc := vmath.OrbitalEquilibrium(dx, dyCirc, dust.OrbitRadius, stiffness)

			dust.VelX += vmath.Mul(ax, dtFixed)
			dust.VelY += vmath.Mul(vmath.ScaleFromCircular(ayCirc), dtFixed)

			// Orbital damping (converts radial velocity to tangential)
			velYCirc := vmath.ScaleToCircular(dust.VelY)
			dust.VelX, velYCirc = vmath.OrbitalDamp(
				dust.VelX, velYCirc,
				dx, dyCirc,
				constant.DustDamping, dtFixed,
			)
			dust.VelY = vmath.ScaleFromCircular(velYCirc)
		}

		// --- Global Drag (v² model) ---
		speed := vmath.Magnitude(dust.VelX, dust.VelY)
		if speed > 0 {
			dragAmount := vmath.Mul(vmath.Mul(constant.DustGlobalDrag, speed), dtFixed)
			if dragAmount > vmath.Scale {
				dragAmount = vmath.Scale
			}
			scaleFactor := vmath.Scale - dragAmount
			dust.VelX = vmath.Mul(dust.VelX, scaleFactor)
			dust.VelY = vmath.Mul(dust.VelY, scaleFactor)
		}

		// --- Position Integration ---
		prevX, prevY := dust.PreciseX, dust.PreciseY

		dust.PreciseX += vmath.Mul(dust.VelX, dtFixed)
		dust.PreciseY += vmath.Mul(dust.VelY, dtFixed)

		newX := vmath.ToInt(dust.PreciseX)
		newY := vmath.ToInt(dust.PreciseY)

		// Boundary reflection
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

		// --- Zero-Allocation Collision Traversal ---

		// Only traverse if we actually moved or need to check current cell
		if newX != dust.LastIntX || newY != dust.LastIntY {
			traverser := vmath.NewGridTraverser(prevX, prevY, dust.PreciseX, dust.PreciseY)
			destroyDust := false

			for traverser.Next() {
				currX, currY := traverser.Pos()

				// Skip cell from previous frame to avoid re-triggering logic
				if currX == dust.LastIntX && currY == dust.LastIntY {
					continue
				}

				// Check bounds (Traverser might step OOB temporarily)
				if currX < 0 || currX >= config.GameWidth || currY < 0 || currY >= config.GameHeight {
					continue
				}

				// Safe unsafe-access (we hold Lock)
				n := s.world.Position.GetAllAtIntoUnsafe(currX, currY, collisionBuf[:])

				for i := 0; i < n; i++ {
					target := collisionBuf[i]
					if target == 0 || target == entity {
						continue
					}

					if s.world.Component.Death.Has(target) {
						continue
					}

					// --- Drain interaction ---
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
						continue
					}

					// --- Quasar interaction ---
					if member, ok := s.world.Component.Member.Get(target); ok {
						if header, hOk := s.world.Component.Header.Get(member.AnchorID); hOk {
							if header.BehaviorID == component.BehaviorQuasar {
								if quasar, qOk := s.world.Component.Quasar.Get(member.AnchorID); qOk {
									// Center-of-mass collision, no offset calculation
									physics.ApplyCollision(
										&quasar.KineticState,
										dust.VelX, dust.VelY,
										&physics.DustToQuasar,
										s.rng,
										now,
									)
									s.world.Component.Quasar.Set(member.AnchorID, quasar)
								}
							}
						}
						continue
					}

					// --- Blossom/Decay interaction ---
					if s.world.Component.Blossom.Has(target) || s.world.Component.Decay.Has(target) {
						s.world.Component.Death.Set(target, component.DeathComponent{})
						deathCandidates = append(deathCandidates, target)
						continue
					}

					// --- Glyph interaction ---

					// Prerequisite 1: Dust itself must be inside shield to interact with glyphs
					if !dustInsideShield {
						continue
					}

					glyph, ok := s.world.Component.Glyph.Get(target)
					if !ok {
						continue
					}

					// Prerequisite 2: Target Glyph must also be inside shield (handles shield entry edge cases, e.g. fast-moving dust)
					gDx := vmath.FromInt(currX) - cursorXFixed
					gDy := vmath.FromInt(currY) - cursorYFixed
					if !vmath.EllipseContains(gDx, gDy, shield.InvRxSq, shield.InvRySq) {
						continue
					}

					shouldKillGlyph := false
					shouldKillDust := false

					if cursorEnergy > 0 {
						if glyph.Type == component.GlyphBlue {
							shouldKillGlyph = true
							shouldKillDust = true
						} else if glyph.Type == component.GlyphRed {
							shouldKillDust = true
						}
					} else if cursorEnergy < 0 {
						if glyph.Type == component.GlyphRed {
							shouldKillGlyph = true
							shouldKillDust = true
						} else if glyph.Type == component.GlyphBlue {
							shouldKillDust = true
						}
					}
					// Green, Gold, and Zero Energy: No interaction

					if shouldKillGlyph {
						s.world.Component.Death.Set(target, component.DeathComponent{})
						deathCandidates = append(deathCandidates, target)
						s.world.PushEvent(event.EventEnergyGlyphConsumed, &event.GlyphConsumedPayload{
							Type:  glyph.Type,
							Level: glyph.Level,
						})
					}

					if shouldKillDust {
						destroyDust = true
						break // Dust destroyed, stop checking other entities in this cell
					}
				}

				if destroyDust {
					break
				}
			}

			if destroyDust {
				s.world.Component.Death.Set(entity, component.DeathComponent{})
				destroyedCount++
				continue
			}
		}

		if newX != dust.LastIntX || newY != dust.LastIntY {
			dust.LastIntX = newX
			dust.LastIntY = newY
			// Use Unsafe Move (we hold the lock)
			s.world.Position.MoveUnsafe(entity, component.PositionComponent{X: newX, Y: newY})
		}

		s.world.Component.Dust.Set(entity, dust)
	}

	if len(deathCandidates) > 0 {
		event.EmitDeathBatch(s.world.Resource.Event.Queue, event.EventFlashRequest, deathCandidates, s.world.Resource.Time.FrameNumber)
	}

	s.statActive.Store(int64(len(dustEntities)))
	s.statDestroyed.Add(destroyedCount)
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

	// Use batch creation for transformation dust
	posBatch := s.world.Position.BeginBatch()

	for _, gd := range toTransform {
		entity := s.world.CreateEntity()
		dust, prot, sigil := s.prepareDustComponents(gd.x, gd.y, gd.char, gd.level, cursorPos.X, cursorPos.Y)

		posBatch.Add(entity, component.PositionComponent{X: gd.x, Y: gd.y})
		s.world.Component.Dust.Set(entity, dust)
		s.world.Component.Protection.Set(entity, prot)
		s.world.Component.Sigil.Set(entity, sigil)
	}

	posBatch.CommitForce()
	s.statCreated.Add(int64(len(toTransform)))
}

// prepareDustComponents calculates physics and component state for a new dust particle
func (s *DustSystem) prepareDustComponents(x, y int, char rune, level component.GlyphLevel, cursorX, cursorY int) (
	component.DustComponent,
	component.ProtectionComponent,
	component.SigilComponent,
) {
	// Random orbit radius in [min, max]
	radiusRange := int(constant.DustOrbitRadiusMax - constant.DustOrbitRadiusMin)
	orbitRadius := constant.DustOrbitRadiusMin
	if radiusRange > 0 {
		orbitRadius += int64(s.rng.Intn(radiusRange))
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

	// Dust component
	dust := component.DustComponent{
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
	}

	// Protection
	prot := component.ProtectionComponent{
		Mask:      component.ProtectFromDrain,
		ExpiresAt: 0, // Permanent
	}

	// Sigil for rendering
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

	sigil := component.SigilComponent{
		Rune:  char,
		Color: color,
	}

	return dust, prot, sigil
}

// spawnDust creates a single dust entity with orbital initialization
func (s *DustSystem) spawnDust(x, y int, char rune, level component.GlyphLevel, cursorX, cursorY int) {
	entity := s.world.CreateEntity()
	dust, prot, sigil := s.prepareDustComponents(x, y, char, level, cursorX, cursorY)

	s.world.Position.Set(entity, component.PositionComponent{X: x, Y: y})
	s.world.Component.Dust.Set(entity, dust)
	s.world.Component.Protection.Set(entity, prot)
	s.world.Component.Sigil.Set(entity, sigil)
}

// spawnDustBatched creates a single dust entity adding it to the provided PositionBatch
// Identical to spawnDust but delegates Position setting to the batch
func (s *DustSystem) spawnDustBatched(batch *engine.PositionBatch, x, y int, char rune, level component.GlyphLevel, cursorX, cursorY int) {
	entity := s.world.CreateEntity()

	// Random orbit radius in [min, max]
	radiusRange := int(constant.DustOrbitRadiusMax - constant.DustOrbitRadiusMin)
	orbitRadius := constant.DustOrbitRadiusMin
	if radiusRange > 0 {
		orbitRadius += int64(s.rng.Intn(radiusRange))
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

	// BATCHED: Add to position batch instead of setting directly
	batch.Add(entity, component.PositionComponent{X: x, Y: y})

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
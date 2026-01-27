package system

import (
	"sync/atomic"
	"time"

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

	s.statCreated = world.Resources.Status.Ints.Get("dust.created")
	s.statActive = world.Resources.Status.Ints.Get("dust.active")
	s.statDestroyed = world.Resources.Status.Ints.Get("dust.destroyed")

	s.Init()
	return s
}

func (s *DustSystem) Init() {
	s.lastCursorX = 0
	s.lastCursorY = 0
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statCreated.Store(0)
	s.statActive.Store(0)
	s.statDestroyed.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *DustSystem) Name() string {
	return "dust"
}

func (s *DustSystem) Priority() int {
	return constant.PriorityDust
}

func (s *DustSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDustSpawnOneRequest,
		event.EventDustSpawnBatchRequest,
		event.EventDustAllRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *DustSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventDustSpawnOneRequest:
		if p, ok := ev.Payload.(*event.DustSpawnOneRequestPayload); ok {
			if p.Level == component.GlyphDark {
				return
			}
			cursorEntity := s.world.Resources.Cursor.Entity
			cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
			if !ok {
				return
			}
			s.spawnDust(p.X, p.Y, p.Char, p.Level, cursorPos.X, cursorPos.Y)
			s.statCreated.Add(1)
		}

	case event.EventDustSpawnBatchRequest:
		// Optimized batch handling with CommitForce and shared logic
		if p, ok := ev.Payload.(*event.DustSpawnBatchRequestPayload); ok {
			count := len(p.Entries)
			if count == 0 {
				event.ReleaseDustSpawnBatch(p)
				return
			}

			cursorEntity := s.world.Resources.Cursor.Entity
			cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
			if !ok {
				event.ReleaseDustSpawnBatch(p)
				return
			}

			// OPTIMIZATION: Use PositionBatch to lock the spatial grid once for all new entities
			posBatch := s.world.Positions.BeginBatch()

			for i := 0; i < count; i++ {
				entry := p.Entries[i]
				if entry.Level == component.GlyphDark {
					continue
				}
				entity := s.world.CreateEntity()
				s.setDustComponents(entity, entry.X, entry.Y, entry.Char, entry.Level, cursorPos.X, cursorPos.Y)

				// Add components to batch entry entity
				posBatch.Add(entity, component.PositionComponent{X: entry.X, Y: entry.Y})
			}

			// Force commit because dust often spawns on top of dying glyphs (DeathSystem runs later)
			posBatch.CommitForce()

			s.statCreated.Add(int64(count))
			event.ReleaseDustSpawnBatch(p)
		}

	case event.EventDustAllRequest:
		s.transformGlyphsToDust()
	}
}

func (s *DustSystem) Update() {
	if !s.enabled {
		return
	}

	dustEntities := s.world.Components.Dust.GetAllEntities()
	if len(dustEntities) == 0 {
		s.statActive.Store(0)
		return
	}

	// 1. PRE-FETCH Context Data (Cursor, Energy, etc.) BEFORE Positions lock to avoid deadlock
	cursorEntity := s.world.Resources.Cursor.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	// Fetch energy for attraction
	var cursorEnergy int64
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if ok {
		cursorEnergy = energyComp.Current
	}
	hasAttraction := cursorEnergy != 0

	// Shield data for collision energy reward
	shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := ok && shield.Active

	// Chase boost on cursor jump
	cursorDeltaX := cursorPos.X - s.lastCursorX
	cursorDeltaY := cursorPos.Y - s.lastCursorY
	s.lastCursorX = cursorPos.X
	s.lastCursorY = cursorPos.Y

	cursorDisplacement := vmath.DistanceApprox(vmath.FromInt(cursorDeltaX), vmath.FromInt(cursorDeltaY))
	applyChaseBoost := cursorDisplacement > vmath.FromInt(constant.DustChaseThreshold)

	// 2. Setup Physics Constants
	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	// Cursor position precise adjustment at the center of the cell to avoid skewed render
	cursorXFixed := vmath.FromInt(cursorPos.X) + vmath.Scale>>1
	cursorYFixed := vmath.FromInt(cursorPos.Y) + vmath.Scale>>1

	// 3. LOCK Spatial Grid (Optimization: Global Batch Lock)
	s.world.Positions.Lock()
	defer s.world.Positions.Unlock()

	var destroyedCount int64
	deathCandidates := make([]core.Entity, 0, 32)
	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	// 4. MAIN LOOP
	for _, dustEntity := range dustEntities {
		dustComp, ok := s.world.Components.Dust.GetComponent(dustEntity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(dustEntity)
		if !ok {
			continue
		}

		// --- Positions and Shield State ---
		dx := kineticComp.PreciseX - cursorXFixed
		dy := kineticComp.PreciseY - cursorYFixed

		dustInsideShield := shieldActive && vmath.EllipseContains(dx, dy, shield.InvRxSq, shield.InvRySq)

		// --- Per-Particle Jitter (always active) ---
		jitterAngle := int64(s.rng.Intn(vmath.LUTSize)) << (vmath.Shift - 10)
		kineticComp.VelX += vmath.Mul(vmath.Cos(jitterAngle), constant.DustJitter)
		kineticComp.VelY += vmath.Mul(vmath.Sin(jitterAngle), constant.DustJitter)

		// --- Orbital Physics (only when energy != 0 / shield active) ---
		if hasAttraction {
			// Chase boost decay
			if applyChaseBoost {
				dustComp.ChaseBoost = constant.DustChaseBoost
			} else if dustComp.ChaseBoost > vmath.Scale {
				decay := vmath.Mul(constant.DustChaseDecay, dtFixed)
				// TODO: move chase boost to kinetic
				dustComp.ChaseBoost -= decay
				if dustComp.ChaseBoost < vmath.Scale {
					dustComp.ChaseBoost = vmath.Scale
				}
			}

			// Equilibrium-seeking force toward target orbit radius
			// Scale Y to circular space for visually circular orbit
			stiffness := vmath.Mul(constant.DustAttractionBase, dustComp.ChaseBoost)
			dyCirc := vmath.ScaleToCircular(dy)
			ax, ayCirc := vmath.OrbitalEquilibrium(dx, dyCirc, dustComp.OrbitRadius, stiffness)

			kineticComp.VelX += vmath.Mul(ax, dtFixed)
			kineticComp.VelY += vmath.Mul(vmath.ScaleFromCircular(ayCirc), dtFixed)

			// Orbital damping (converts radial velocity to tangential)
			velYCirc := vmath.ScaleToCircular(kineticComp.VelY)
			kineticComp.VelX, velYCirc = vmath.OrbitalDamp(
				kineticComp.VelX, velYCirc,
				dx, dyCirc,
				constant.DustDamping, dtFixed,
			)
			kineticComp.VelY = vmath.ScaleFromCircular(velYCirc)
		}

		// --- Global Drag (v² model) ---
		speed := vmath.Magnitude(kineticComp.VelX, kineticComp.VelY)
		if speed > 0 {
			dragAmount := vmath.Mul(vmath.Mul(constant.DustGlobalDrag, speed), dtFixed)
			if dragAmount > vmath.Scale {
				dragAmount = vmath.Scale
			}
			scaleFactor := vmath.Scale - dragAmount
			kineticComp.VelX = vmath.Mul(kineticComp.VelX, scaleFactor)
			kineticComp.VelY = vmath.Mul(kineticComp.VelY, scaleFactor)
		}

		// --- Positions Integration ---
		prevX, prevY := kineticComp.PreciseX, kineticComp.PreciseY

		kineticComp.PreciseX += vmath.Mul(kineticComp.VelX, dtFixed)
		kineticComp.PreciseY += vmath.Mul(kineticComp.VelY, dtFixed)

		newX := vmath.ToInt(kineticComp.PreciseX)
		newY := vmath.ToInt(kineticComp.PreciseY)

		gameWidth := s.world.Resources.Config.GameWidth
		gameHeight := s.world.Resources.Config.GameHeight

		// Boundary reflection
		if newX < 0 {
			newX = 0
			kineticComp.PreciseX = 0
			kineticComp.VelX = -kineticComp.VelX / 2
		} else if newX >= gameWidth {
			newX = gameWidth - 1
			kineticComp.PreciseX = vmath.FromInt(newX)
			kineticComp.VelX = -kineticComp.VelX / 2
		}

		if newY < 0 {
			newY = 0
			kineticComp.PreciseY = 0
			kineticComp.VelY = -kineticComp.VelY / 2
		} else if newY >= gameHeight {
			newY = gameHeight - 1
			kineticComp.PreciseY = vmath.FromInt(newY)
			kineticComp.VelY = -kineticComp.VelY / 2
		}

		// --- Zero-Allocation Collision Traversal ---

		// Only traverse if we actually moved or need to check current cell
		if newX != dustComp.LastIntX || newY != dustComp.LastIntY {
			traverser := vmath.NewGridTraverser(prevX, prevY, kineticComp.PreciseX, kineticComp.PreciseY)
			destroyDust := false

			for traverser.Next() {
				currX, currY := traverser.Pos()

				// Skip cell from previous frame to avoid re-triggering logic
				if currX == dustComp.LastIntX && currY == dustComp.LastIntY {
					continue
				}

				// Check bounds (Traverser might step OOB)
				if currX < 0 || currX >= gameWidth || currY < 0 || currY >= gameHeight {
					continue
				}

				// Safe unsafe-access (we hold Lock)
				n := s.world.Positions.GetAllAtIntoUnsafe(currX, currY, collisionBuf[:])

				for i := 0; i < n; i++ {
					target := collisionBuf[i]
					if target == 0 || target == dustEntity {
						continue
					}

					if s.world.Components.Death.HasEntity(target) {
						continue
					}

					// --- Drain interaction ---
					// TODO: fix later
					if s.world.Components.Drain.HasEntity(target) {
						if drainKineticComp, ok := s.world.Components.Kinetic.GetComponent(target); ok {
							physics.ApplyCollision(
								&drainKineticComp.Kinetic,
								kineticComp.VelX, kineticComp.VelY,
								&physics.DustToDrain,
								s.rng,
							)
							s.world.Components.Kinetic.SetComponent(target, drainKineticComp)
						}
						continue
					}

					// --- Quasar interaction ---
					if member, ok := s.world.Components.Member.GetComponent(target); ok {
						if headerComp, ok := s.world.Components.Header.GetComponent(member.HeaderEntity); ok {
							if quasarKineticComp, ok := s.world.Components.Kinetic.GetComponent(member.HeaderEntity); ok {
								if headerComp.Behavior == component.BehaviorQuasar {
									// Center-of-mass collision, no offset calculation
									physics.ApplyCollision(
										&quasarKineticComp.Kinetic,
										kineticComp.VelX, kineticComp.VelY,
										&physics.DustToQuasar,
										s.rng,
									)
									s.world.Components.Kinetic.SetComponent(member.HeaderEntity, quasarKineticComp)
								}
							}
						}
					}

					// --- Blossom/Decay interaction ---

					if s.world.Components.Blossom.HasEntity(target) || s.world.Components.Decay.HasEntity(target) {
						s.world.Components.Death.SetComponent(target, component.DeathComponent{})
						deathCandidates = append(deathCandidates, target)
						if !dustInsideShield {
							destroyDust = true
							break
						}
					}

					// --- Glyph interaction ---

					// Prerequisite 1: Dust itself must be inside shield to interact with glyphs
					if !dustInsideShield {
						continue
					}

					glyphComp, ok := s.world.Components.Glyph.GetComponent(target)
					if !ok {
						continue
					}

					// Prerequisite 2: Target Glyph must also be inside shield (handles shield entry edge cases, e.g. fast-moving dustComp)
					gDx := vmath.FromInt(currX) - cursorXFixed
					gDy := vmath.FromInt(currY) - cursorYFixed
					if !vmath.EllipseContains(gDx, gDy, shield.InvRxSq, shield.InvRySq) {
						continue
					}

					shouldKillGlyph := false
					shouldKillDust := false

					if cursorEnergy > 0 {
						if glyphComp.Type == component.GlyphBlue {
							shouldKillGlyph = true
							shouldKillDust = true
						} else if glyphComp.Type == component.GlyphRed {
							shouldKillDust = true
						}
					} else if cursorEnergy < 0 {
						if glyphComp.Type == component.GlyphRed {
							shouldKillGlyph = true
							shouldKillDust = true
						} else if glyphComp.Type == component.GlyphBlue {
							shouldKillDust = true
						}
					}
					// Green, Gold, and Zero Energy: No interaction

					if shouldKillGlyph {
						s.world.Components.Death.SetComponent(target, component.DeathComponent{})
						deathCandidates = append(deathCandidates, target)
						s.world.PushEvent(event.EventEnergyGlyphConsumed, &event.GlyphConsumedPayload{
							Type:  glyphComp.Type,
							Level: glyphComp.Level,
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
				s.world.Components.Death.SetComponent(dustEntity, component.DeathComponent{})
				destroyedCount++
				continue
			}
		}

		if newX != dustComp.LastIntX || newY != dustComp.LastIntY {
			dustComp.LastIntX = newX
			dustComp.LastIntY = newY
			// Use Unsafe Move (we hold the lock)
			s.world.Positions.MoveUnsafe(dustEntity, component.PositionComponent{X: newX, Y: newY})
		}

		// --- Color Update ---
		sigilComp, ok := s.world.Components.Sigil.GetComponent(dustEntity)
		if !ok {
			continue
		}
		timerComp, ok := s.world.Components.Timer.GetComponent(dustEntity)
		if !ok {
			deathCandidates = append(deathCandidates, dustEntity)
		}

		if sigilComp.Color == component.SigilDustBright && timerComp.Remaining < constant.DustTimerNormal {
			sigilComp.Color = component.SigilDustNormal
			s.world.Components.Sigil.SetComponent(dustEntity, sigilComp)
		} else if sigilComp.Color == component.SigilDustNormal && timerComp.Remaining < constant.DustTimerDark {
			sigilComp.Color = component.SigilDustDark
			s.world.Components.Sigil.SetComponent(dustEntity, sigilComp)
		}

		s.world.Components.Dust.SetComponent(dustEntity, dustComp)
		s.world.Components.Kinetic.SetComponent(dustEntity, kineticComp)
	}

	if len(deathCandidates) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashRequest, deathCandidates)
	}

	s.statActive.Store(int64(len(dustEntities)))
	s.statDestroyed.Add(destroyedCount)
}

// transformGlyphsToDust converts all non-composite glyphs to dust entities
func (s *DustSystem) transformGlyphsToDust() {
	cursorEntity := s.world.Resources.Cursor.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
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

	glyphEntities := s.world.Components.Glyph.GetAllEntities()
	toTransform := make([]glyphData, 0, len(glyphEntities))
	toFlashKill := make([]core.Entity, len(glyphEntities))

	for _, glyphEntity := range glyphEntities {
		// Skip composite members
		if s.world.Components.Member.HasEntity(glyphEntity) {
			continue
		}

		glyphComp, ok := s.world.Components.Glyph.GetComponent(glyphEntity)
		if !ok {
			continue
		}
		if glyphComp.Level == component.GlyphDark {
			toFlashKill = append(toFlashKill, glyphEntity)
			continue
		}

		glyphPos, ok := s.world.Positions.GetPosition(glyphEntity)
		if !ok {
			continue
		}

		toTransform = append(toTransform, glyphData{
			entity: glyphEntity,
			x:      glyphPos.X,
			y:      glyphPos.Y,
			char:   glyphComp.Rune,
			level:  glyphComp.Level,
		})
	}

	// Emit batch death with flash effect (no transform)
	if len(toFlashKill) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashRequest, toFlashKill)
	}

	if len(toTransform) == 0 {
		return
	}

	// Emit batch death with no effect (transform)
	deathEntities := make([]core.Entity, len(toTransform))
	for i, gd := range toTransform {
		deathEntities[i] = gd.entity
	}
	event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, deathEntities)

	// Use batch creation for transformation dust
	posBatch := s.world.Positions.BeginBatch()

	for _, gd := range toTransform {
		entity := s.world.CreateEntity()
		s.setDustComponents(entity, gd.x, gd.y, gd.char, gd.level, cursorPos.X, cursorPos.Y)

		posBatch.Add(entity, component.PositionComponent{X: gd.x, Y: gd.y})
	}

	posBatch.CommitForce()
	s.statCreated.Add(int64(len(toTransform)))
}

// setDustComponents calculates physics and component state for a new dust particle
func (s *DustSystem) setDustComponents(entity core.Entity, x, y int, char rune, level component.GlyphLevel, cursorX, cursorY int) {
	// Random orbit radius in [min, max]
	radiusRange := int(constant.DustOrbitRadiusMax - constant.DustOrbitRadiusMin)
	orbitRadius := constant.DustOrbitRadiusMin
	if radiusRange > 0 {
		orbitRadius += int64(s.rng.Intn(radiusRange))
	}

	// Positions relative to cursor for orbital calculation
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
	dustComp := component.DustComponent{
		OrbitRadius: orbitRadius,
		ChaseBoost:  vmath.Scale,
		LastIntX:    x,
		LastIntY:    y,
	}

	// Kinetic component
	kinetic := core.Kinetic{
		PreciseX: vmath.FromInt(x),
		PreciseY: vmath.FromInt(y),
		VelX:     vx,
		VelY:     vy,
	}
	kineticComp := component.KineticComponent{kinetic}

	// Protection component
	protComp := component.ProtectionComponent{
		Mask: component.ProtectFromDrain,
	}

	// Sigil for rendering
	remaining, color := s.dustProperties(level)

	sigilComp := component.SigilComponent{
		Rune:  char,
		Color: color,
	}

	timerComp := component.TimerComponent{Remaining: remaining}

	s.world.Components.Dust.SetComponent(entity, dustComp)
	s.world.Components.Kinetic.SetComponent(entity, kineticComp)
	s.world.Components.Protection.SetComponent(entity, protComp)
	s.world.Components.Sigil.SetComponent(entity, sigilComp)
	s.world.Components.Timer.SetComponent(entity, timerComp)
}

// spawnDust creates a single dust entity with orbital initialization
func (s *DustSystem) spawnDust(x, y int, char rune, level component.GlyphLevel, cursorX, cursorY int) {
	entity := s.world.CreateEntity()
	s.setDustComponents(entity, x, y, char, level, cursorX, cursorY)

	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})
}

func (s *DustSystem) dustProperties(level component.GlyphLevel) (time.Duration, component.SigilColor) {
	switch level {
	case component.GlyphDark:
		return constant.DustTimerDark, component.SigilDustDark
	case component.GlyphNormal:
		return constant.DustTimerNormal, component.SigilDustNormal
	case component.GlyphBright:
		return constant.DustTimerBright, component.SigilDustBright
	default:
		return constant.DustTimerNormal, component.SigilDustNormal
	}
}
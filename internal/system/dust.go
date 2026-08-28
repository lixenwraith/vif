package system

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// collisionContext caches the tick's drain cells; dust never enumerates the grid.
// One drain per cell: co-located drains destroy each other in the same tick.
type collisionContext struct {
	drains   map[uint64]core.Entity
	impulses map[core.Entity]impulseAcc
}

type impulseAcc struct {
	vx, vy float64
	hits   int
}

func posKey(x, y int) uint64 {
	return uint64(x)<<32 | uint64(uint32(y))
}

// glyphTransform caches a glyph's render state before its entity is destroyed
type glyphTransform struct {
	x, y  int
	char  rune
	level component.GlyphLevel
}

// DustSystem manages orbital dust particles created from glyph transformation
// Dust orbits cursor with chase behavior on large cursor movements
type DustSystem struct {
	world *engine.World

	// Cursor tracking for chase detection
	lastCursorX int
	lastCursorY int

	// Random source for orbit radius and direction
	rng *vmath.FastRand

	// Stagger tick for distributing chase boost activation (cycles 0-2)
	staggerTick uint8

	// Per-tick collision scratch; refilled in place, never reallocated
	collisionCtx collisionContext
	deathBuf     []core.Entity

	// Glyph→dust scratch; storm collisions can request this many times a second
	transformBuf []glyphTransform
	destroyBuf   []core.Entity
	flashBuf     []core.Entity

	// Detonation scratch
	centerCells map[uint64]struct{}
	centerBuf   []event.ExplosionCenterEntry
	blast       blastArea

	// Telemetry
	statCreated             *atomic.Int64
	statActive              *atomic.Int64
	statDestroyed           *atomic.Int64
	statWallCollisions      *atomic.Int64
	statBoundaryReflections *atomic.Int64
	statGridSteps           *atomic.Int64
	statCentersDropped      *atomic.Int64
	rejects                 rejectionTelemetry
	buffers                 bufferTelemetry

	enabled bool
}

// Buffer telemetry slots for DustSystem
const (
	bufDustDeath = iota
	bufDustTransform
	bufDustFlash
	bufDustImpulses
	bufDustCenters
)

func NewDustSystem(world *engine.World) engine.System {
	s := &DustSystem{
		world: world,
	}

	s.statCreated = world.Resources.Status.Ints.Get("dust.created")
	s.statActive = world.Resources.Status.Ints.Get("dust.active")
	s.statDestroyed = world.Resources.Status.Ints.Get("dust.destroyed")
	s.statWallCollisions = world.Resources.Status.Ints.Get("dust.wall_collisions")
	s.statBoundaryReflections = world.Resources.Status.Ints.Get("dust.boundary_reflections")
	s.statGridSteps = world.Resources.Status.Ints.Get("dust.grid_steps")
	s.statCentersDropped = world.Resources.Status.Ints.Get("dust.centers_dropped")
	s.rejects = newRejectionTelemetry(world.Resources.Status, "dust")
	s.buffers = newBufferTelemetry(world.Resources.Status, "dust",
		"death", "transform", "flash", "impulses", "centers")

	s.Init()
	return s
}

func (s *DustSystem) Init() {
	s.lastCursorX = 0
	s.lastCursorY = 0
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.staggerTick = 0
	s.collisionCtx = collisionContext{
		drains:   make(map[uint64]core.Entity, 16),
		impulses: make(map[core.Entity]impulseAcc, 16),
	}
	s.centerCells = make(map[uint64]struct{}, 256)
	s.centerBuf = make([]event.ExplosionCenterEntry, 0, parameter.ExplosionCenterCap)
	s.deathBuf = make([]core.Entity, 0, 32)
	s.transformBuf = make([]glyphTransform, 0, 256)
	s.destroyBuf = make([]core.Entity, 0, 256)
	s.flashBuf = make([]core.Entity, 0, 64)
	s.statCreated.Store(0)
	s.statActive.Store(0)
	s.statDestroyed.Store(0)
	s.statWallCollisions.Store(0)
	s.statBoundaryReflections.Store(0)
	s.statGridSteps.Store(0)
	s.statCentersDropped.Store(0)
	s.rejects.Reset()
	s.buffers.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *DustSystem) Name() string {
	return "dust"
}

// Domain reports player: it draws the player stream and creates player entities.
func (s *DustSystem) Domain() engine.SystemDomain { return engine.SystemPlayer }

// Requires nothing: detonation crosses to explosion when it is present.
func (s *DustSystem) Requires() engine.SystemDependencies {
	return engine.Optional("explosion")
}

func (s *DustSystem) Priority() int {
	return parameter.PriorityDust
}

func (s *DustSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDustSpawnOneRequest,
		event.EventDustSpawnBatchRequest,
		event.EventFireSpecialRequest,
		event.EventDustAllRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *DustSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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

	// A pooled payload is released on every path, disabled included
	if !s.enabled {
		s.rejects.disabled.Add(1)
		if p, ok := ev.Payload.(*event.BatchPayload[event.DustSpawnEntry]); ok {
			event.DustBatchPool.Release(p)
		}
		return
	}

	switch ev.Type {
	case event.EventDustSpawnOneRequest:
		if p, ok := ev.Payload.(*event.DustSpawnOneRequestPayload); ok {
			if p.Level == component.GlyphDark {
				return
			}
			cursorEntity := s.world.Resources.Player.Entity
			cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
			if !ok {
				return
			}
			s.spawnDust(p.X, p.Y, p.Char, p.Level, cursorPos.X, cursorPos.Y)
			s.statCreated.Add(1)
		}

	case event.EventDustSpawnBatchRequest:
		// Optimized batch handling with CommitForce and shared logic
		if p, ok := ev.Payload.(*event.BatchPayload[event.DustSpawnEntry]); ok {
			count := len(p.Entries)
			if count == 0 {
				event.DustBatchPool.Release(p)
				return
			}

			cursorEntity := s.world.Resources.Player.Entity
			cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
			if !ok {
				event.DustBatchPool.Release(p)
				return
			}

			// OPTIMIZATION: Use PositionBatch to lock the spatial grid once for all new entities
			posBatch := s.world.Positions.BeginBatch()

			created := 0
			for i := range count {
				entry := p.Entries[i]
				if entry.Level == component.GlyphDark {
					continue
				}
				entity := s.world.CreateEntity(core.DomainPlayer)
				s.setDustComponents(entity, entry.X, entry.Y, entry.Char, entry.Level, cursorPos.X, cursorPos.Y)

				// Set components to batch entry entity
				posBatch.Add(entity, component.PositionComponent{X: entry.X, Y: entry.Y})
				created++
			}

			// Force commit because dust often spawns on top of dying glyphs (DeathSystem runs later)
			posBatch.CommitForce()

			s.statCreated.Add(int64(created))
			event.DustBatchPool.Release(p)
		}

	case event.EventFireSpecialRequest:
		if p, ok := ev.Payload.(*event.FireSpecialRequestPayload); ok {
			if cursor := s.world.ResolveCursor(p.Entity); cursor != 0 {
				s.detonateDust(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}

	case event.EventDustAllRequest:
		if cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity); ok {
			s.convertGlyphs(cursorPos.X, cursorPos.Y, nil)
		}
	}
}

func (s *DustSystem) Update() {
	if !s.enabled {
		return
	}

	dusts := s.world.Components.Dust
	if dusts.CountEntities() == 0 {
		s.statActive.Store(0)
		return
	}

	// 1. PRE-FETCH Context Data (Cursor, Energy, etc.) BEFORE Positions lock to avoid deadlock
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	// Fetch energy for attraction
	var cursorEnergy int64
	energyComp, ok := s.world.Components.Energy.GetPtr(cursorEntity)
	if ok {
		cursorEnergy = energyComp.Current
	}
	hasAttraction := cursorEnergy != 0

	// Build collision context for this tick
	collisionCtx := s.buildCollisionContext()

	// Chase boost on cursor jump
	cursorDeltaX := cursorPos.X - s.lastCursorX
	cursorDeltaY := cursorPos.Y - s.lastCursorY
	s.lastCursorX = cursorPos.X
	s.lastCursorY = cursorPos.Y

	cursorDisplacement := vmath.MagnitudeF(float64(cursorDeltaX), float64(cursorDeltaY))
	applyChaseBoost := cursorDisplacement > parameter.DustChaseThreshold

	// Stagger tick advancement on cursor jump
	if applyChaseBoost {
		s.staggerTick = (s.staggerTick + 1) % 3
	}

	// 2. Setup Physics Constants
	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	const (
		baseStiffness    = parameter.DustAttractionBase
		boostedStiffness = parameter.DustAttractionBase * parameter.DustChaseBoost
	)

	// Cursor position precise adjustment at the center of the cell to avoid skewed render
	cursorCenterX, cursorCenterY := vmath.Point{X: cursorPos.X, Y: cursorPos.Y}.CenterF()

	// 3. LOCK Spatial Grid (Optimization: Global Batch Lock)
	s.world.Positions.Lock()
	defer s.world.Positions.Unlock()

	s.deathBuf = s.deathBuf[:0]

	// 4. MAIN LOOP
	for _, dustEntity := range dusts.Entities() {
		dustComp, ok := dusts.GetPtr(dustEntity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetPtr(dustEntity)
		if !ok {
			continue
		}

		// --- Positions relative to cursor (orbital physics input) ---
		dx := kineticComp.PreciseX - cursorCenterX
		dy := kineticComp.PreciseY - cursorCenterY

		// --- Per-Particle Jitter (always active) ---
		jitterAngle := s.rng.Float64() * vmath.TwoPi
		kineticComp.VelX += vmath.CosF(jitterAngle) * parameter.DustJitter
		kineticComp.VelY += vmath.SinF(jitterAngle) * parameter.DustJitter

		// --- Orbital Physics (only when energy != 0 / shield active) ---
		if hasAttraction {
			// Staggered chase boost: only activate for matching group
			if applyChaseBoost && dustComp.ResponseGroup == s.staggerTick {
				dustComp.ChaseBoost = parameter.DustChaseBoost
			} else if dustComp.ChaseBoost > 1.0 {
				dustComp.ChaseBoost = max(dustComp.ChaseBoost-parameter.DustChaseDecay*dtSec, 1.0)
			}

			// Equilibrium-seeking force toward target orbit radius
			// Scale Y to circular space for visually circular orbit
			stiffness := baseStiffness
			if dustComp.ChaseBoost > 1.0 {
				// Interpolate: base + (boosted - base) * (boost - 1) / (maxBoost - 1)
				boostFactor := dustComp.ChaseBoost - 1.0
				stiffness = baseStiffness + (boostedStiffness-baseStiffness)*
					(boostFactor/(parameter.DustChaseBoost-1.0))
			}

			dyCirc := vmath.ScaleToCircularF(dy)
			ax, ayCirc := physics.OrbitalEquilibrium(dx, dyCirc, dustComp.OrbitRadius, stiffness)

			kineticComp.VelX += ax * dtSec
			kineticComp.VelY += vmath.ScaleFromCircularF(ayCirc) * dtSec

			// Orbital damping (converts radial velocity to tangential)
			velYCirc := vmath.ScaleToCircularF(kineticComp.VelY)
			kineticComp.VelX, velYCirc = physics.OrbitalDamp(
				kineticComp.VelX, velYCirc,
				dx, dyCirc,
				parameter.DustDamping, dtSec,
			)
			kineticComp.VelY = vmath.ScaleFromCircularF(velYCirc)
		}

		// --- Global Drag (v² model) ---
		physics.ApplyQuadraticDrag(&kineticComp.Kinetic, parameter.DustGlobalDrag, dtSec)

		// --- Positions Integration ---
		prevX, prevY := kineticComp.PreciseX, kineticComp.PreciseY
		newX, newY := physics.IntegratePosition(&kineticComp.Kinetic, dtSec)

		gameWidth := s.world.Resources.Config.MapWidth
		gameHeight := s.world.Resources.Config.MapHeight

		// Boundary reflection
		rx := physics.ReflectBoundsDampedX(&kineticComp.Kinetic, 0, gameWidth, parameter.DustWallRestitution)
		ry := physics.ReflectBoundsDampedY(&kineticComp.Kinetic, 0, gameHeight, parameter.DustWallRestitution)
		if rx || ry {
			s.statBoundaryReflections.Add(1)
			newX, newY = physics.GridPos(&kineticComp.Kinetic)
		}

		// --- Collision Traversal with Wall Check ---
		lastSafeX, lastSafeY := dustComp.LastIntX, dustComp.LastIntY
		hitWall := false

		if newX != dustComp.LastIntX || newY != dustComp.LastIntY {
			traverser := vmath.NewGridTraverserF(prevX, prevY, kineticComp.PreciseX, kineticComp.PreciseY)

			for traverser.Next() {
				s.statGridSteps.Add(1)
				currX, currY := traverser.Pos()

				// Skip cell from previous frame
				if currX == dustComp.LastIntX && currY == dustComp.LastIntY {
					continue
				}

				// OOB safety (defensive, boundary reflection should handle)
				if currX < 0 || currX >= gameWidth || currY < 0 || currY >= gameHeight {
					continue
				}

				// Wall collision - reflect and stop (BEFORE entity checks)
				if s.world.Positions.HasBlockingWallAt(currX, currY, component.WallBlockParticle) {
					s.statWallCollisions.Add(1)
					if currX != lastSafeX {
						physics.ReflectVelocityX(&kineticComp.Kinetic, parameter.DustWallRestitution)
					}
					if currY != lastSafeY {
						physics.ReflectVelocityY(&kineticComp.Kinetic, parameter.DustWallRestitution)
					}
					kineticComp.PreciseX, kineticComp.PreciseY = vmath.Point{X: lastSafeX, Y: lastSafeY}.CenterF()
					hitWall = true
					break
				}

				// Update last safe position for wall reflection
				lastSafeX, lastSafeY = currX, currY

				// Drain contact: the only player-domain body dust interacts with
				if target, hit := collisionCtx.drains[posKey(currX, currY)]; hit && target != dustEntity {
					if !s.world.Components.Death.HasEntity(target) {
						impulseX, impulseY := physics.ImpulseFromProfile(
							kineticComp.VelX, kineticComp.VelY,
							&profile.DustToDrain, s.rng,
						)
						collisionCtx.accumulateImpulse(target, impulseX, impulseY)
					}
				}

			}

			// Apply wall reflection
			if hitWall {
				newX, newY = lastSafeX, lastSafeY
			}
		}

		if newX != dustComp.LastIntX || newY != dustComp.LastIntY {
			dustComp.LastIntX = newX
			dustComp.LastIntY = newY
			// Use Unsafe Move (we hold the lock)
			s.world.Positions.MoveUnsafe(dustEntity, component.PositionComponent{X: newX, Y: newY})
		}

		// --- Color Update ---
		sigilComp, ok := s.world.Components.Sigil.GetPtr(dustEntity)
		if !ok {
			continue
		}
		timerComp, ok := s.world.Components.Timer.GetPtr(dustEntity)
		if !ok {
			s.deathBuf = append(s.deathBuf, dustEntity)
			continue
		}

		if sigilComp.Color == visual.RgbDustBright && timerComp.Remaining < parameter.DustTimerNormal {
			sigilComp.Color = visual.RgbDustNormal
		} else if sigilComp.Color == visual.RgbDustNormal && timerComp.Remaining < parameter.DustTimerDark {
			sigilComp.Color = visual.RgbDustDark
		}
	}

	// Apply batched collision impulses
	s.buffers.Observe(bufDustImpulses, len(collisionCtx.impulses))
	s.applyAccumulatedImpulses(collisionCtx)

	if len(s.deathBuf) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, s.deathBuf...)
	}

	s.statActive.Store(int64(dusts.CountEntities()))
	s.statDestroyed.Add(int64(len(s.deathBuf)))
	s.buffers.Observe(bufDustDeath, len(s.deathBuf))
}

// buildCollisionContext refills the tick's drain cell lookup in place
func (s *DustSystem) buildCollisionContext() *collisionContext {
	ctx := &s.collisionCtx
	clear(ctx.drains)
	clear(ctx.impulses)

	for _, e := range s.world.Components.Drain.Entities() {
		if pos, ok := s.world.Positions.GetPosition(e); ok {
			ctx.drains[posKey(pos.X, pos.Y)] = e
		}
	}
	return ctx
}

// accumulateImpulse adds velocity delta to target's accumulator
func (ctx *collisionContext) accumulateImpulse(target core.Entity, vx, vy float64) {
	acc := ctx.impulses[target]
	acc.vx += vx
	acc.vy += vy
	acc.hits++
	ctx.impulses[target] = acc
}

// applyAccumulatedImpulses applies batched impulses to kinetic components
func (s *DustSystem) applyAccumulatedImpulses(ctx *collisionContext) {
	for entity, acc := range ctx.impulses {
		if acc.hits == 0 {
			continue
		}
		kc, ok := s.world.Components.Kinetic.GetPtr(entity)
		if !ok {
			continue
		}

		// Scale impulse by hit count with diminishing returns: sqrt(hits)
		// Prevents excessive knockback from dust swarm while preserving impact
		scaleFactor := math.Sqrt(float64(acc.hits))
		kc.VelX += acc.vx / scaleFactor
		kc.VelY += acc.vy / scaleFactor
	}
}

// setDustComponents calculates physics and component state for a new dust particle
func (s *DustSystem) setDustComponents(entity core.Entity, x, y int, char rune, level component.GlyphLevel, cursorX, cursorY int) {
	// Random orbit radius in [min, max]
	orbitRadius := parameter.DustOrbitRadiusMin +
		s.rng.Float64()*(parameter.DustOrbitRadiusMax-parameter.DustOrbitRadiusMin)

	// Position relative to cursor center for orbital calculation
	cursorCenterX, cursorCenterY := vmath.Point{X: cursorX, Y: cursorY}.CenterF()
	spawnX, spawnY := vmath.Point{X: x, Y: y}.CenterF()
	dx := spawnX - cursorCenterX
	dy := spawnY - cursorCenterY

	// Initial tangential velocity for orbit, random direction
	clockwise := s.rng.Intn(2) == 0
	vx, vy := physics.OrbitalInsert(dx, dy, parameter.DustAttractionBase, clockwise)

	// Scale to initial speed
	if dirX, dirY := vmath.Normalize2DF(vx, vy); dirX != 0 || dirY != 0 {
		vx = dirX * parameter.DustInitialSpeed
		vy = dirY * parameter.DustInitialSpeed
	}

	// Dust component
	dustComp := component.DustComponent{
		OrbitRadius:   orbitRadius,
		ChaseBoost:    1.0,
		LastIntX:      x,
		LastIntY:      y,
		ResponseGroup: uint8(s.rng.Intn(3)),
	}

	// Kinetic component
	kinetic := physics.Kinetic{
		PreciseX: spawnX,
		PreciseY: spawnY,
		VelX:     vx,
		VelY:     vy,
	}
	kineticComp := component.KineticComponent{Kinetic: kinetic}

	// Protection component
	protComp := component.ProtectionComponent{
		Mask: component.ProtectFromSpecies,
	}

	// Sigil for rendering
	remaining, c := s.dustProperties(level)

	sigilComp := component.SigilComponent{
		Rune:  char,
		Color: c,
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
	entity := s.world.CreateEntity(core.DomainPlayer)
	s.setDustComponents(entity, x, y, char, level, cursorX, cursorY)

	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})
}

// detonateDust converts every dust particle into an explosion center, resolves the
// player-domain half of the blast, then hands the geometry to ExplosionSystem.
func (s *DustSystem) detonateDust(cursor core.Entity) {
	dusts := s.world.Components.Dust.Entities()
	if len(dusts) == 0 {
		return
	}

	cursorPos, ok := s.world.Positions.GetPosition(cursor)
	if !ok {
		return
	}

	// One center per occupied cell, collected in dense store order so merge order is stable
	clear(s.centerCells)
	s.centerBuf = s.centerBuf[:0]
	s.destroyBuf = append(s.destroyBuf[:0], dusts...)
	for _, e := range s.destroyBuf {
		pos, ok := s.world.Positions.GetPosition(e)
		if !ok {
			continue
		}
		key := posKey(pos.X, pos.Y)
		if _, seen := s.centerCells[key]; seen {
			continue
		}
		s.centerCells[key] = struct{}{}
		if len(s.centerBuf) == parameter.ExplosionCenterCap {
			s.statCentersDropped.Add(1)
			continue
		}
		s.centerBuf = append(s.centerBuf, event.ExplosionCenterEntry{X: pos.X, Y: pos.Y})
	}
	s.buffers.Observe(bufDustCenters, len(s.centerBuf))

	// DustSystem owns dust lifecycle, so destruction needs no death-system round trip
	destroyed := len(s.destroyBuf)
	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.destroyBuf = s.destroyBuf[:0]
	s.statDestroyed.Add(int64(destroyed))

	if len(s.centerBuf) == 0 {
		return
	}
	s.blast.reset(s.centerBuf, parameter.ExplosionFieldRadius)

	s.convertGlyphs(cursorPos.X, cursorPos.Y, &s.blast)
	strikePlayerTargets(s.world, cursor, &s.blast, component.CombatAttackExplosion)

	// The crossing: geometry and combat family only, no player entity reference
	p := event.AcquireExplosionBatchRequest()
	p.Entity = cursor
	p.Radius = parameter.ExplosionFieldRadius
	p.Duration = parameter.ExplosionFieldDuration
	p.Type = event.ExplosionTypeDust
	p.Attack = component.CombatAttackExplosion
	p.Centers = append(p.Centers, s.centerBuf...)
	s.world.PushEvent(event.EventExplosionBatchRequest, p)
}

// convertGlyphs turns loose glyphs into dust and flashes the dark ones.
// A nil area converts the whole field; composite members are never converted.
func (s *DustSystem) convertGlyphs(cursorX, cursorY int, area *blastArea) {
	glyphEntities := s.world.Components.Glyph.Entities()
	if len(glyphEntities) == 0 {
		return
	}

	s.transformBuf = s.transformBuf[:0]
	s.flashBuf = s.flashBuf[:0]
	s.destroyBuf = s.destroyBuf[:0]

	for _, glyphEntity := range glyphEntities {
		// Glyph mechanics are player-domain; a shared glyph is a gold member
		if glyphEntity.Domain() != core.DomainPlayer {
			continue
		}
		if s.world.Components.Member.HasEntity(glyphEntity) {
			continue
		}
		glyphComp, ok := s.world.Components.Glyph.GetPtr(glyphEntity)
		if !ok {
			continue
		}

		// Dark glyphs carry no dust, so they die through the death API for the flash
		if glyphComp.Level == component.GlyphDark {
			if area != nil {
				pos, ok := s.world.Positions.GetPosition(glyphEntity)
				if !ok || !area.contains(pos.X, pos.Y) {
					continue
				}
			}
			s.flashBuf = append(s.flashBuf, glyphEntity)
			continue
		}

		glyphPos, ok := s.world.Positions.GetPosition(glyphEntity)
		if !ok {
			continue
		}
		if area != nil && !area.contains(glyphPos.X, glyphPos.Y) {
			continue
		}
		s.destroyBuf = append(s.destroyBuf, glyphEntity)
		s.transformBuf = append(s.transformBuf, glyphTransform{
			x: glyphPos.X, y: glyphPos.Y, char: glyphComp.Rune, level: glyphComp.Level,
		})
	}
	s.buffers.Observe(bufDustTransform, len(s.transformBuf))
	s.buffers.Observe(bufDustFlash, len(s.flashBuf))

	if len(s.flashBuf) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, s.flashBuf...)
	}
	if len(s.transformBuf) == 0 {
		return
	}

	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.destroyBuf = s.destroyBuf[:0]

	posBatch := s.world.Positions.BeginBatch()
	for _, gt := range s.transformBuf {
		entity := s.world.CreateEntity(core.DomainPlayer)
		s.setDustComponents(entity, gt.x, gt.y, gt.char, gt.level, cursorX, cursorY)
		posBatch.Add(entity, component.PositionComponent{X: gt.x, Y: gt.y})
	}
	posBatch.CommitForce()

	s.statCreated.Add(int64(len(s.transformBuf)))
}

// TODO: move to parameter/particle.go and visual/color.go?
func (s *DustSystem) dustProperties(level component.GlyphLevel) (time.Duration, color.RGB) {
	switch level {
	case component.GlyphDark:
		return parameter.DustTimerDark, visual.RgbDustDark
	case component.GlyphNormal:
		return parameter.DustTimerNormal, visual.RgbDustNormal
	case component.GlyphBright:
		return parameter.DustTimerBright, visual.RgbDustBright
	default:
		return parameter.DustTimerNormal, visual.RgbDustNormal
	}
}

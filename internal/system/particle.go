package system

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// particleBehaviorOrder is the update order: dust, then decay before blossom
var particleBehaviorOrder = [...]component.ParticleBehavior{
	component.ParticleDust,
	component.ParticleDecay,
	component.ParticleBlossom,
}

// particleAnchor names what a behavior's particles orbit
type particleAnchor uint8

const (
	anchorNone   particleAnchor = iota
	anchorCursor                // the local cursor while its energy is nonzero
	anchorKraken                // the nearest Kraken while it spins or charges
)

// particleOrbit tunes the spring holding a particle on its orbit
type particleOrbit struct {
	stiffness            float64 // pull toward the orbit radius (1/s²)
	radiusMin, radiusMax float64 // circular-space cells
	damping              float64 // radial damping (1/s)
	drag                 float64 // quadratic drag while orbiting (1/cell)
	speed                float64 // tangential speed on entering orbit (cells/s)
}

// particleProfile is everything that differs between behaviors. A falling
// behavior enters orbit with a tangential kick and leaves it back into its fall;
// dust orbits from spawn and only loses the pull while its anchor lapses.
type particleProfile struct {
	fall     float64          // spawn and gravity direction: 1 down, -1 up, 0 none
	color    [3]color.RGB     // by glyph level
	lifetime [3]time.Duration // by glyph level; 0 is no timer, an infinite life
	protect  component.ProtectionFlags
	runeSwap float64 // chance of a new rune on entering a cell
	bounce   float64 // restitution off walls and bounds; 0 destroys the particle instead
	jitter   float64 // random velocity added every tick (cells/s)
	drag     float64 // quadratic drag while not orbiting (1/cell)
	anchor   particleAnchor
	orbit    particleOrbit
}

var particleProfiles = [component.ParticleBehaviorCount]particleProfile{
	component.ParticleDecay: {
		fall:     1,
		color:    [3]color.RGB{visual.RgbDecay, visual.RgbDecay, visual.RgbDecay},
		runeSwap: parameter.ParticleChangeChance,
		anchor:   anchorKraken,
		orbit: particleOrbit{
			stiffness: parameter.DecayOrbitStiffness,
			radiusMin: parameter.DecayOrbitRadiusMin, radiusMax: parameter.DecayOrbitRadiusMax,
			damping: parameter.DecayOrbitDamping, drag: parameter.DecayOrbitDrag, speed: parameter.DecayOrbitSpeed,
		},
	},
	component.ParticleBlossom: {
		fall:     -1,
		color:    [3]color.RGB{visual.RgbBlossom, visual.RgbBlossom, visual.RgbBlossom},
		runeSwap: parameter.ParticleChangeChance,
	},
	component.ParticleDust: {
		color:    [3]color.RGB{visual.RgbDustDark, visual.RgbDustNormal, visual.RgbDustBright},
		lifetime: [3]time.Duration{parameter.DustTimerDark, parameter.DustTimerNormal, parameter.DustTimerBright},
		protect:  component.ProtectFromSpecies,
		bounce:   parameter.DustWallRestitution,
		jitter:   parameter.DustJitter,
		drag:     parameter.DustGlobalDrag,
		anchor:   anchorCursor,
		orbit: particleOrbit{
			stiffness: parameter.DustAttractionBase,
			radiusMin: parameter.DustOrbitRadiusMin, radiusMax: parameter.DustOrbitRadiusMax,
			damping: parameter.DustDamping, drag: parameter.DustGlobalDrag, speed: parameter.DustInitialSpeed,
		},
	},
}

// particleBehaviorState keeps behavior-specific collision bookkeeping and
// diagnostics separate while all particles share their system RNG.
type particleBehaviorState struct {
	hitThisFrame       map[core.Entity]bool
	processedGridCells map[int]bool

	statCount            *atomic.Int64
	statApplied          *atomic.Int64
	statWallCollisions   *atomic.Int64
	statBoundaryHits     *atomic.Int64
	statGridSteps        *atomic.Int64
	statProtectedRejects *atomic.Int64
	buffers              bufferTelemetry
}

// orbitCenter is one anchor this tick, in precise cells; spin is the turn a
// falling particle takes on entering its orbit
type orbitCenter struct {
	x, y, spin float64
}

type impulseAcc struct {
	vx, vy float64
	hits   int
}

func posKey(x, y int) uint64 {
	return uint64(x)<<32 | uint64(uint32(y))
}

// ParticleSystem moves dust, decay and blossom particles and detonates dust.
type ParticleSystem struct {
	world *engine.World
	rng   *vmath.FastRand

	behavior [component.ParticleBehaviorCount]particleBehaviorState
	entities []core.Entity
	deathBuf []core.Entity
	centers  []orbitCenter

	// The dust chase boost follows cursor jumps, one stagger group (0-2) per jump
	lastCursorX, lastCursorY int
	staggerTick              uint8

	// Dust's drain contacts: one drain per cell, as co-located drains destroy each other
	drains   map[uint64]core.Entity
	impulses map[core.Entity]impulseAcc

	// Conversion and detonation scratch; refilled in place
	transformBuf []event.ParticleSpawnEntry
	destroyBuf   []core.Entity
	flashBuf     []core.Entity
	centerCells  map[uint64]struct{}
	centerBuf    []event.ExplosionCenterEntry
	blast        blastArea

	statDustCreated    *atomic.Int64
	statDustDestroyed  *atomic.Int64
	statCentersDropped *atomic.Int64
	rejects            rejectionTelemetry
	dustBuffers        bufferTelemetry

	toggle
}

// Buffer telemetry slots for dust conversion and detonation
const (
	bufDustTransform = iota
	bufDustFlash
	bufDustImpulses
	bufDustCenters
)

// NewParticleSystem creates the unified particle system.
func NewParticleSystem(world *engine.World) engine.System {
	s := &ParticleSystem{
		world:        world,
		entities:     make([]core.Entity, 0, 256),
		deathBuf:     make([]core.Entity, 0, 128),
		drains:       make(map[uint64]core.Entity, 16),
		impulses:     make(map[core.Entity]impulseAcc, 16),
		transformBuf: make([]event.ParticleSpawnEntry, 0, 256),
		destroyBuf:   make([]core.Entity, 0, 256),
		flashBuf:     make([]core.Entity, 0, 64),
		centerCells:  make(map[uint64]struct{}, 256),
		centerBuf:    make([]event.ExplosionCenterEntry, 0, parameter.ExplosionCenterCap),
	}

	reg := world.Resources.Status
	for _, behavior := range particleBehaviorOrder {
		state := &s.behavior[behavior]
		state.hitThisFrame = make(map[core.Entity]bool)
		state.processedGridCells = make(map[int]bool)

		name := behavior.String()
		state.statCount = reg.Ints.Get(name + ".count")
		state.statApplied = reg.Ints.Get(name + ".applied")
		state.statWallCollisions = reg.Ints.Get(name + ".wall_collisions")
		state.statBoundaryHits = reg.Ints.Get(name + ".boundary_hits")
		state.statGridSteps = reg.Ints.Get(name + ".grid_steps")
		state.statProtectedRejects = reg.Ints.Get(name + ".protected_rejects")
		state.buffers = newBufferTelemetry(reg, name, "hit_entities", "processed_cells")
	}
	s.statDustCreated = reg.Ints.Get("dust.created")
	s.statDustDestroyed = reg.Ints.Get("dust.destroyed")
	s.statCentersDropped = reg.Ints.Get("dust.centers_dropped")
	s.rejects = newRejectionTelemetry(reg, "particle")
	s.dustBuffers = newBufferTelemetry(reg, "dust", "transform", "flash", "impulses", "centers")

	s.Init()
	return s
}

// Init resets session state for a new game.
func (s *ParticleSystem) Init() {
	s.entities = s.entities[:0]
	s.deathBuf = s.deathBuf[:0]
	s.centers = s.centers[:0]
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.lastCursorX, s.lastCursorY, s.staggerTick = 0, 0, 0
	clear(s.drains)
	clear(s.impulses)
	for _, behavior := range particleBehaviorOrder {
		state := &s.behavior[behavior]
		clear(state.hitThisFrame)
		clear(state.processedGridCells)
		state.statCount.Store(0)
		state.statApplied.Store(0)
		state.statWallCollisions.Store(0)
		state.statBoundaryHits.Store(0)
		state.statGridSteps.Store(0)
		state.statProtectedRejects.Store(0)
		state.buffers.Reset()
	}
	s.statDustCreated.Store(0)
	s.statDustDestroyed.Store(0)
	s.statCentersDropped.Store(0)
	s.rejects.Reset()
	s.dustBuffers.Reset()
	s.enabled = true
}

// Name returns the system's registry name.
func (s *ParticleSystem) Name() string {
	return "particle"
}

// Priority returns the system's update priority.
func (s *ParticleSystem) Priority() int {
	return parameter.PriorityParticle
}

// EventTypes returns the particle events handled by the system.
func (s *ParticleSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventParticleWave,
		event.EventParticleSpawnOne,
		event.EventParticleSpawnBatch,
		event.EventDustAllRequest,
		event.EventFireSpecialRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent resolves generic particle requests by their behavior field.
func (s *ParticleSystem) HandleEvent(ev event.GameEvent) {
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
		if batch, ok := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry]); ok {
			event.ParticleBatchPool.Release(batch)
		}
		return
	}

	switch ev.Type {
	case event.EventParticleWave:
		if payload, ok := ev.Payload.(*event.ParticleWavePayload); ok {
			s.spawnWave(payload)
		}

	case event.EventParticleSpawnOne:
		if p, ok := ev.Payload.(*event.ParticleSpawnPayload); ok {
			s.spawnRequested(event.ParticleSpawnEntry{
				Behavior: p.Behavior, X: p.X, Y: p.Y, Char: p.Char, Level: p.Level, SkipStartCell: p.SkipStartCell,
			})
		}

	case event.EventParticleSpawnBatch:
		if batch, ok := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry]); ok {
			for i := range batch.Entries {
				s.spawnRequested(batch.Entries[i])
			}
			event.ParticleBatchPool.Release(batch)
		}

	case event.EventFireSpecialRequest:
		if p, ok := ev.Payload.(*event.FireSpecialRequestPayload); ok {
			if cursor := s.world.ResolveOwnedCursor(p.Entity); cursor != 0 {
				s.detonateDust(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}

	case event.EventDustAllRequest:
		if cursorPos, ok := s.world.LocalCursor(); ok {
			s.convertGlyphs(cursorPos.X, cursorPos.Y, nil, nil)
		}
	}
}

// Update advances dust, then decay, then blossom. A detached reusable entity
// list permits immediate removals from the shared component store.
func (s *ParticleSystem) Update() {
	if !s.enabled {
		return
	}

	particles := s.world.Components.Particle
	if particles.CountEntities() > 0 {
		s.entities = append(s.entities[:0], particles.Entities()...)
		var present [component.ParticleBehaviorCount]bool
		for _, entity := range s.entities {
			if particle, ok := particles.GetPtr(entity); ok {
				present[particle.Behavior] = true
			}
		}
		for _, behavior := range particleBehaviorOrder {
			if present[behavior] {
				s.updateBehavior(behavior)
			}
		}
	}

	var counts [component.ParticleBehaviorCount]int64
	for _, entity := range particles.Entities() {
		particle, ok := particles.GetComponent(entity)
		if ok && particle.Behavior.Valid() {
			counts[particle.Behavior]++
		}
	}
	for _, behavior := range particleBehaviorOrder {
		s.behavior[behavior].statCount.Store(counts[behavior])
	}
}

// spawnRequested spawns a requested particle. Dust orbits the local cursor, so it
// needs one, and a dark glyph carries no dust.
func (s *ParticleSystem) spawnRequested(e event.ParticleSpawnEntry) {
	if e.Behavior != component.ParticleDust {
		s.spawnOne(e, 0, 0)
		return
	}
	if e.Level == component.GlyphDark {
		return
	}
	cursor, ok := s.world.LocalCursor()
	if !ok {
		return
	}
	s.spawnOne(e, cursor.X, cursor.Y)
	s.statDustCreated.Add(1)
}

// spawnOne creates one particle. A falling one takes its fall; any other enters
// orbit at once around the cursor cell (cx, cy).
func (s *ParticleSystem) spawnOne(e event.ParticleSpawnEntry, cx, cy int) {
	if !e.Behavior.Valid() {
		return
	}
	prof := &particleProfiles[e.Behavior]
	level := min(max(e.Level, component.GlyphDark), component.GlyphBright)

	particle := component.ParticleComponent{
		Behavior: e.Behavior, Rune: e.Char, LastIntX: -1, LastIntY: -1, ChaseBoost: 1,
	}
	if e.SkipStartCell {
		particle.LastIntX, particle.LastIntY = e.X, e.Y
	}
	preciseX, preciseY := vmath.Point{X: e.X, Y: e.Y}.CenterF()
	kinetic := physics.Kinetic{PreciseX: preciseX, PreciseY: preciseY}
	if prof.fall != 0 {
		kinetic.VelY = prof.fall * s.fallSpeed()
		kinetic.AccelY = prof.fall * parameter.ParticleAcceleration
	} else {
		// Draw order: radius, turn, stagger group
		particle.OrbitRadius = prof.orbit.radiusMin + s.rng.Float64()*(prof.orbit.radiusMax-prof.orbit.radiusMin)
		spin := 1.0
		if s.rng.Intn(2) == 0 {
			spin = -1
		}
		centerX, centerY := vmath.Point{X: cx, Y: cy}.CenterF()
		kinetic.VelX, kinetic.VelY = orbitKick(preciseX-centerX, vmath.ScaleToCircularF(preciseY-centerY), prof.orbit.speed, spin)
		particle.ResponseGroup = uint8(s.rng.Intn(3))
		particle.Orbiting = true
	}

	entity := s.world.CreateEntity(core.DomainPlayer)
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: e.X, Y: e.Y})
	s.world.Components.Particle.SetComponent(entity, particle)
	s.world.Components.Kinetic.SetComponent(entity, component.KineticComponent{Kinetic: kinetic})
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{Rune: e.Char, Color: prof.color[level]})
	if prof.protect != 0 {
		s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{Mask: prof.protect})
	}
	if life := prof.lifetime[level]; life > 0 {
		armTimer(s.world, entity, component.TimerLifetime, life)
	}
}

// spawnWave creates one particle per column at a falling behavior's starting edge,
// across the map or the columns of the button the payload names
func (s *ParticleSystem) spawnWave(p *event.ParticleWavePayload) {
	behavior := p.Behavior
	if !behavior.Valid() || particleProfiles[behavior].fall == 0 {
		return
	}
	config := s.world.Resources.Config
	y := 0
	if particleProfiles[behavior].fall < 0 {
		y = config.MapHeight - 1
	}
	from, to := 0, config.MapWidth
	if p.Button != 0 {
		from, to = 0, 0
		buttons := s.world.Components.Button
		buttons.Each(func(_ core.Entity, b *component.ButtonComponent) bool {
			if b.Slot != p.Button {
				return true
			}
			x, _, w, _ := component.ButtonRect(b.Slot, buttons.CountEntities(), config.MapWidth, config.MapHeight)
			from, to = max(x, 0), min(x+w, config.MapWidth)
			return false
		})
	}
	for column := from; column < to; column++ {
		char := parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
		s.spawnOne(event.ParticleSpawnEntry{Behavior: behavior, X: column, Y: y, Char: char}, 0, 0)
	}
}

func (s *ParticleSystem) fallSpeed() float64 {
	return parameter.ParticleMinSpeed + s.rng.Float64()*(parameter.ParticleMaxSpeed-parameter.ParticleMinSpeed)
}

// orbitKick is a tangential velocity of speed around a centre at circular-space
// offset (dx, dy), turning the way the Kraken's legs do for a positive spin
func orbitKick(dx, dy, speed, spin float64) (vx, vy float64) {
	tx, ty := vmath.Normalize2DF(vmath.PerpendicularF(dx, dy))
	return tx * speed * spin, vmath.ScaleFromCircularF(ty * speed * spin)
}

// updateBehavior integrates every particle of one behavior: anchor pull or fall,
// then a swept path that applies the behavior's contact rule to each entered cell.
func (s *ParticleSystem) updateBehavior(behavior component.ParticleBehavior) {
	prof := &particleProfiles[behavior]
	state := &s.behavior[behavior]
	clear(state.processedGridCells)
	clear(state.hitThisFrame)
	s.deathBuf = s.deathBuf[:0]
	if behavior == component.ParticleDust {
		s.collectDrains()
	}

	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), parameter.MaxSimulationDeltaSeconds)
	gameWidth := s.world.Resources.Config.MapWidth
	gameHeight := s.world.Resources.Config.MapHeight
	centers, chase := s.anchors(prof.anchor)
	particles := s.world.Components.Particle

	for _, entity := range s.entities {
		particle, ok := particles.GetComponent(entity)
		if !ok || particle.Behavior != behavior {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(entity)
		if !ok {
			continue
		}
		kinetic := &kineticComp.Kinetic

		if prof.jitter > 0 {
			angle := s.rng.Float64() * vmath.TwoPi
			kinetic.VelX += vmath.CosF(angle) * prof.jitter
			kinetic.VelY += vmath.SinF(angle) * prof.jitter
		}
		center, anchored := nearestCenter(centers, kinetic.PreciseX, kinetic.PreciseY)
		s.steer(prof, &particle, kinetic, center, anchored, chase, dtSec)

		prevX, prevY := kinetic.PreciseX, kinetic.PreciseY
		curX, curY := physics.Integrate(kinetic, dtSec)
		if prof.bounce > 0 {
			rx := physics.ReflectBoundsDampedX(kinetic, 0, gameWidth, prof.bounce)
			ry := physics.ReflectBoundsDampedY(kinetic, 0, gameHeight, prof.bounce)
			if rx || ry {
				state.statBoundaryHits.Add(1)
				curX, curY = physics.GridPos(kinetic)
			}
		}

		destroyParticle := false
		if curX != particle.LastIntX || curY != particle.LastIntY {
			lastSafe := vmath.PointAtF(prevX, prevY)
			lastSafeX, lastSafeY := lastSafe.X, lastSafe.Y
			traverser := vmath.NewGridTraverserF(prevX, prevY, kinetic.PreciseX, kinetic.PreciseY)
			for traverser.Next() {
				state.statGridSteps.Add(1)
				x, y := traverser.Pos()
				if x == particle.LastIntX && y == particle.LastIntY {
					continue
				}
				if s.world.Positions.IsOutOfBounds(x, y) {
					if prof.bounce == 0 {
						state.statBoundaryHits.Add(1)
						destroyParticle = true
						break
					}
					continue // bound reflection already holds a bouncing particle in
				}
				if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockParticle) {
					state.statWallCollisions.Add(1)
					if prof.bounce == 0 {
						destroyParticle = true
						break
					}
					if x != lastSafeX {
						physics.ReflectVelocityX(kinetic, prof.bounce)
					}
					if y != lastSafeY {
						physics.ReflectVelocityY(kinetic, prof.bounce)
					}
					kinetic.PreciseX, kinetic.PreciseY = vmath.Point{X: lastSafeX, Y: lastSafeY}.CenterF()
					curX, curY = lastSafeX, lastSafeY
					break
				}
				lastSafeX, lastSafeY = x, y
				if s.touch(behavior, state, entity, kinetic, x, y, gameWidth) {
					destroyParticle = true
					break
				}
			}
		}

		if destroyParticle {
			// Collision, wall and boundary removal share one immediate path with no death effect
			s.world.DestroyEntity(entity)
			continue
		}

		if particle.LastIntX != curX || particle.LastIntY != curY {
			if prof.runeSwap > 0 && s.rng.Float64() < prof.runeSwap {
				particle.Rune = parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
				if sigil, ok := s.world.Components.Sigil.GetPtr(entity); ok {
					sigil.Rune = particle.Rune
				}
			}
			particle.LastIntX = curX
			particle.LastIntY = curY
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: curX, Y: curY})
		}
		s.fade(prof, entity)

		particles.SetComponent(entity, particle)
		s.world.Components.Kinetic.SetComponent(entity, kineticComp)
	}

	switch behavior {
	case component.ParticleDust:
		s.dustBuffers.Observe(bufDustImpulses, len(s.impulses))
		s.applyDrainImpulses()
	case component.ParticleDecay:
		if len(s.deathBuf) > 0 {
			event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, s.deathBuf...)
		}
	}
	state.buffers.Observe(0, len(state.hitThisFrame))
	state.buffers.Observe(1, len(state.processedGridCells))
}

// anchors resolves this tick's orbit centres for one anchor rule; chase reports
// a cursor jump, which boosts the stagger group it selects
func (s *ParticleSystem) anchors(anchor particleAnchor) (centers []orbitCenter, chase bool) {
	s.centers = s.centers[:0]
	switch anchor {
	case anchorCursor:
		// The visible cursor: the D-18 prediction while this instance's placements are outstanding
		cursorPos, ok := s.world.LocalCursor()
		if !ok {
			return s.centers, false
		}
		dx, dy := cursorPos.X-s.lastCursorX, cursorPos.Y-s.lastCursorY
		s.lastCursorX, s.lastCursorY = cursorPos.X, cursorPos.Y
		if vmath.MagnitudeF(float64(dx), float64(dy)) > parameter.DustChaseThreshold {
			chase = true
			s.staggerTick = (s.staggerTick + 1) % 3
		}
		// Energy of either sign pulls, as it raises the shield
		if energy, ok := s.world.Components.Energy.GetComponent(s.world.Resources.Player.Entity); ok && energy.Current != 0 {
			x, y := vmath.Point{X: cursorPos.X, Y: cursorPos.Y}.CenterF()
			s.centers = append(s.centers, orbitCenter{x: x, y: y})
		}
	case anchorKraken:
		for _, e := range s.world.Components.Kraken.Entities() {
			kraken, ok := s.world.Components.Kraken.GetComponent(e)
			if !ok || (kraken.State != component.KrakenSpin && kraken.State != component.KrakenMove) {
				continue
			}
			body, ok := s.world.Components.Kinetic.GetComponent(e)
			if !ok {
				continue
			}
			s.centers = append(s.centers, orbitCenter{x: body.PreciseX, y: body.PreciseY, spin: kraken.TurnDir})
		}
	}
	return s.centers, chase
}

// nearestCenter returns the centre closest to (x, y) in circular space
func nearestCenter(centers []orbitCenter, x, y float64) (orbitCenter, bool) {
	best, bestDist := orbitCenter{}, math.Inf(1)
	for _, c := range centers {
		dx, dy := x-c.x, vmath.ScaleToCircularF(y-c.y)
		if d := dx*dx + dy*dy; d < bestDist {
			best, bestDist = c, d
		}
	}
	return best, len(centers) > 0
}

// steer applies an anchor's pull, or a falling particle's release back into its fall
func (s *ParticleSystem) steer(prof *particleProfile, p *component.ParticleComponent, k *physics.Kinetic,
	c orbitCenter, anchored, chase bool, dtSec float64) {
	drag := prof.drag
	switch {
	case anchored:
		// Orbit physics runs in circular space so the orbit looks round on terminal cells
		dx, dy := k.PreciseX-c.x, vmath.ScaleToCircularF(k.PreciseY-c.y)
		if !p.Orbiting && prof.fall != 0 {
			p.OrbitRadius = prof.orbit.radiusMin + s.rng.Float64()*(prof.orbit.radiusMax-prof.orbit.radiusMin)
			k.VelX, k.VelY = orbitKick(dx, dy, prof.orbit.speed, c.spin)
		}
		if chase && p.ResponseGroup == s.staggerTick {
			p.ChaseBoost = parameter.DustChaseBoost
		} else if p.ChaseBoost > 1 {
			p.ChaseBoost = max(p.ChaseBoost-parameter.DustChaseDecay*dtSec, 1)
		}

		ax, ay := physics.OrbitalEquilibrium(dx, dy, p.OrbitRadius, prof.orbit.stiffness*max(p.ChaseBoost, 1))
		k.VelX += ax * dtSec
		k.VelY += vmath.ScaleFromCircularF(ay) * dtSec

		// Radial damping turns radial velocity into tangential
		velYCirc := vmath.ScaleToCircularF(k.VelY)
		k.VelX, velYCirc = physics.OrbitalDamp(k.VelX, velYCirc, dx, dy, prof.orbit.damping, dtSec)
		k.VelY = vmath.ScaleFromCircularF(velYCirc)
		drag = prof.orbit.drag
	case p.Orbiting && prof.fall != 0:
		k.VelX = 0
		k.VelY = prof.fall * s.fallSpeed()
	}
	p.Orbiting = anchored
	if drag > 0 {
		physics.ApplyQuadraticDrag(k, drag, dtSec)
	}
}

// touch applies the behavior's contact rule to one entered cell and reports
// whether the particle is consumed
func (s *ParticleSystem) touch(behavior component.ParticleBehavior, state *particleBehaviorState,
	entity core.Entity, k *physics.Kinetic, x, y, gameWidth int) bool {
	if behavior == component.ParticleDust {
		// A drain is the only body dust interacts with
		if target, hit := s.drains[posKey(x, y)]; hit && !s.world.Components.Death.HasEntity(target) {
			impulseX, impulseY := physics.ImpulseFromProfile(k.VelX, k.VelY, &profile.DustToDrain, s.rng)
			acc := s.impulses[target]
			acc.vx += impulseX
			acc.vy += impulseY
			acc.hits++
			s.impulses[target] = acc
			state.statApplied.Add(1)
		}
		return false
	}

	flatIdx := y*gameWidth + x
	if state.processedGridCells[flatIdx] {
		return false
	}
	state.processedGridCells[flatIdx] = true
	var collisionBuf [parameter.MaxEntitiesPerCell]core.Entity
	targets := collisionBuf[:s.world.Positions.GetAllEntitiesAtInto(x, y, collisionBuf[:])]
	if behavior == component.ParticleDecay {
		return s.processDecayTargets(state, entity, targets)
	}
	return s.processBlossomTargets(state, entity, targets)
}

// fade steps a timed particle's colour down one level once its remaining life
// falls below the lower level's lifetime
func (s *ParticleSystem) fade(prof *particleProfile, entity core.Entity) {
	timer, ok := s.world.Components.Timer.GetPtr(entity)
	if !ok || !timer.Running(component.TimerLifetime) {
		return
	}
	sigil, ok := s.world.Components.Sigil.GetPtr(entity)
	if !ok {
		return
	}
	remaining := timer.Slots[component.TimerLifetime].Remaining
	for level := component.GlyphBright; level > component.GlyphDark; level-- {
		if sigil.Color == prof.color[level] && remaining < prof.lifetime[level-1] {
			sigil.Color = prof.color[level-1]
			return
		}
	}
}

// collectDrains refills the tick's drain cell lookup; dust never enumerates the grid
func (s *ParticleSystem) collectDrains() {
	clear(s.drains)
	clear(s.impulses)
	for _, e := range s.world.Components.Drain.Entities() {
		if pos, ok := s.world.Positions.GetPosition(e); ok {
			s.drains[posKey(pos.X, pos.Y)] = e
		}
	}
}

// applyDrainImpulses applies each drain's dust hits at once, scaled by sqrt(hits)
// so a swarm of dust cannot fling a drain
func (s *ParticleSystem) applyDrainImpulses() {
	for entity, acc := range s.impulses {
		kc, ok := s.world.Components.Kinetic.GetPtr(entity)
		if !ok {
			continue
		}
		scale := math.Sqrt(float64(acc.hits))
		kc.VelX += acc.vx / scale
		kc.VelY += acc.vy / scale
	}
}

// annihilateOpposingParticle applies the one collision rule shared by decay
// and blossom: the pair consumes each other immediately and without a death
// effect. Other behavior pairings remain available for future rules.
func (s *ParticleSystem) annihilateOpposingParticle(behavior component.ParticleBehavior, target core.Entity) bool {
	other, ok := s.world.Components.Particle.GetComponent(target)
	if !ok || !((behavior == component.ParticleDecay && other.Behavior == component.ParticleBlossom) ||
		(behavior == component.ParticleBlossom && other.Behavior == component.ParticleDecay)) {
		return false
	}
	s.world.DestroyEntity(target)
	return true
}

func (s *ParticleSystem) processDecayTargets(state *particleBehaviorState, particleEntity core.Entity, targets []core.Entity) bool {
	for _, target := range targets {
		if target == 0 || target == particleEntity || state.hitThisFrame[target] {
			continue
		}
		if s.annihilateOpposingParticle(component.ParticleDecay, target) {
			return true
		}

		if s.world.Components.Nugget.HasEntity(target) {
			s.world.PushLocal(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
			event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, target)
		} else {
			// Glyph mechanics are Player-domain; a Shared glyph is a gold member.
			if target.Domain() != core.DomainPlayer {
				continue
			}
			if s.shouldDieByDecay(target) {
				s.deathBuf = append(s.deathBuf, target)
			} else {
				s.applyDecayToCharacter(state, target)
			}
		}

		state.hitThisFrame[target] = true
	}
	return false
}

// processBlossomTargets returns true when the current blossom is consumed.
func (s *ParticleSystem) processBlossomTargets(state *particleBehaviorState, particleEntity core.Entity, targets []core.Entity) bool {
	for _, target := range targets {
		if target == 0 || target == particleEntity {
			continue
		}
		// Glyph mechanics are Player-domain; a Shared glyph is a gold member.
		if target.Domain() != core.DomainPlayer || state.hitThisFrame[target] {
			continue
		}
		if s.annihilateOpposingParticle(component.ParticleBlossom, target) {
			return true
		}

		destroy := s.applyBlossomToCharacter(state, target)
		state.hitThisFrame[target] = true
		if destroy {
			return true
		}
	}
	return false
}

func (s *ParticleSystem) shouldDieByDecay(entity core.Entity) bool {
	glyph, ok := s.world.Components.Glyph.GetComponent(entity)
	return ok && glyph.Level == component.GlyphDark && glyph.Type == component.GlyphRed
}

func (s *ParticleSystem) applyDecayToCharacter(state *particleBehaviorState, entity core.Entity) {
	glyph, ok := s.world.Components.Glyph.GetPtr(entity)
	if !ok {
		return
	}
	if protection, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protection.Mask&component.ProtectFromParticle != 0 {
			state.statProtectedRejects.Add(1)
			return
		}
	}

	if glyph.Level > component.GlyphDark {
		glyph.Level--
	} else {
		switch glyph.Type {
		case component.GlyphBlue:
			glyph.Type = component.GlyphGreen
			glyph.Level = component.GlyphBright
		case component.GlyphGreen:
			glyph.Type = component.GlyphRed
			glyph.Level = component.GlyphBright
		default:
			event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, entity)
		}
	}
	state.statApplied.Add(1)
}

func (s *ParticleSystem) applyBlossomToCharacter(state *particleBehaviorState, entity core.Entity) bool {
	glyph, ok := s.world.Components.Glyph.GetPtr(entity)
	if !ok {
		return false
	}
	if protection, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protection.Mask&component.ProtectFromParticle != 0 {
			state.statProtectedRejects.Add(1)
			return false
		}
	}

	if glyph.Type == component.GlyphRed {
		return true
	}
	if glyph.Level < component.GlyphBright {
		glyph.Level++
		state.statApplied.Add(1)
	}
	return false
}

// Conversion commits before center collection; no queued spawn can miss this blast.
func (s *ParticleSystem) detonateDust(cursor core.Entity) {
	// An unaffordable blast converts nothing, so the gate precedes the conversion.
	heat, ok := s.world.Components.Heat.GetPtr(cursor)
	if !ok || heat.Current+heat.Overheat < parameter.SpecialAttackHeatCost {
		return
	}
	cursorPos, ok := s.world.Positions.GetPosition(cursor)
	if !ok {
		return
	}
	polarity := component.GlyphBlue
	if energy, ok := s.world.Components.Energy.GetPtr(cursor); ok && energy.Current < 0 {
		polarity = component.GlyphRed
	}
	s.convertGlyphs(cursorPos.X, cursorPos.Y, nil, func(glyph *component.GlyphComponent) bool {
		return glyph.Level == component.GlyphDark &&
			(glyph.Type == component.GlyphGreen || glyph.Type == polarity)
	})

	s.destroyBuf = s.destroyBuf[:0]
	for _, e := range s.world.Components.Particle.Entities() {
		if particle, ok := s.world.Components.Particle.GetPtr(e); ok && particle.Behavior == component.ParticleDust {
			s.destroyBuf = append(s.destroyBuf, e)
		}
	}
	if len(s.destroyBuf) == 0 {
		return
	}

	// One center per occupied cell, collected in dense store order so merge order is stable
	clear(s.centerCells)
	s.centerBuf = s.centerBuf[:0]
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
	s.dustBuffers.Observe(bufDustCenters, len(s.centerBuf))

	// This system owns the particle lifecycle, so destruction needs no death-system round trip
	destroyed := len(s.destroyBuf)
	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.destroyBuf = s.destroyBuf[:0]
	s.statDustDestroyed.Add(int64(destroyed))

	if len(s.centerBuf) == 0 {
		return
	}
	s.blast.reset(s.centerBuf, parameter.ExplosionFieldRadius)

	// Glyphs caught in the blast seed the next detonation's dust
	s.convertGlyphs(cursorPos.X, cursorPos.Y, &s.blast, nil)

	s.world.PushLocal(event.EventHeatSpendRequest, &event.HeatSpendRequestPayload{
		Entity: cursor, Amount: parameter.SpecialAttackHeatCost,
	})
	strikePlayerTargets(s.world, cursor, &s.blast, component.CombatAttackExplosion, false)
	s.world.PushLocal(event.EventExplosionVisualBatchRequest, &event.ExplosionVisualBatchRequestPayload{
		Centers: append([]event.ExplosionCenterEntry(nil), s.centerBuf...),
		Radius:  parameter.ExplosionFieldRadius, Duration: parameter.ExplosionFieldDuration,
		Type: event.ExplosionTypeDust,
	})

	// The crossing carries combat geometry. Presentation stays with this producer.
	p := event.AcquireExplosionBatchRequest()
	p.Entity = cursor
	p.Radius = parameter.ExplosionFieldRadius
	p.Attack = component.CombatAttackExplosion
	p.Centers = append(p.Centers, s.centerBuf...)
	s.world.PushCrossing(event.EventExplosionBatchRequest, p)
}

// A nil area converts the whole field; a nil filter converts every level and
// flashes the dark glyphs instead, since they carry no dust.
func (s *ParticleSystem) convertGlyphs(cursorX, cursorY int, area *blastArea, match func(*component.GlyphComponent) bool) {
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
		if s.world.Components.Member.HasEntity(glyphEntity) || s.world.Components.Death.HasEntity(glyphEntity) {
			continue
		}
		glyphComp, ok := s.world.Components.Glyph.GetPtr(glyphEntity)
		if !ok {
			continue
		}

		if match != nil && !match(glyphComp) {
			continue
		}

		glyphPos, ok := s.world.Positions.GetPosition(glyphEntity)
		if !ok {
			continue
		}
		if area != nil {
			if _, _, inside := area.find(glyphPos.X, glyphPos.Y); !inside {
				continue
			}
		}
		if match == nil && glyphComp.Level == component.GlyphDark {
			s.flashBuf = append(s.flashBuf, glyphEntity)
			continue
		}

		s.destroyBuf = append(s.destroyBuf, glyphEntity)
		s.transformBuf = append(s.transformBuf, event.ParticleSpawnEntry{
			Behavior: component.ParticleDust, X: glyphPos.X, Y: glyphPos.Y,
			Char: glyphComp.Rune, Level: glyphComp.Level, SkipStartCell: true,
		})
	}
	s.dustBuffers.Observe(bufDustTransform, len(s.transformBuf))
	s.dustBuffers.Observe(bufDustFlash, len(s.flashBuf))

	if len(s.flashBuf) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, s.flashBuf...)
	}
	if len(s.transformBuf) == 0 {
		return
	}

	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.destroyBuf = s.destroyBuf[:0]
	for _, entry := range s.transformBuf {
		s.spawnOne(entry, cursorX, cursorY)
	}
	s.statDustCreated.Add(int64(len(s.transformBuf)))
}

type particleState struct {
	cursorX, cursorY int
	stagger          uint8
}

func (s *ParticleSystem) CopyState() any {
	return particleState{s.lastCursorX, s.lastCursorY, s.staggerTick}
}

func (s *ParticleSystem) RestoreState(v any) error {
	c := v.(particleState)
	s.lastCursorX, s.lastCursorY, s.staggerTick = c.cursorX, c.cursorY, c.stagger
	return nil
}

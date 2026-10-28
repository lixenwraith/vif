package system

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// StormSystem manages the storm boss entity lifecycle
// Storm is a 3-part composite boss with 3D orbital physics
// Each circle is an independent sub-header that can be destroyed individually
// Spawned on EventStormSpawnRequest, which the scenario FSM emits (StormSetup)
type StormSystem struct {
	world *engine.World

	// Root storm entity (owns 3 circle headers)
	rootEntity core.Entity

	// Random source
	rng *vmath.FastRand

	// Precomputed ellipse cell offsets for wall collision
	ellipseOffsets []vmath.Point

	// Reusable map
	memberExcludeSet map[core.Entity]struct{}

	graphs [component.StormCircleCount]*machine.Graph[component.StormCircleState, *circleCtx] // by circle type
	ctx    circleCtx                                                                          // the circle being stepped, refilled per circle

	// Telemetry
	statActive           *atomic.Bool
	statCircleCount      *atomic.Int64
	statGreenActiveFrame *atomic.Int64
	statRedActiveFrame   *atomic.Int64
	statBlueActiveFrame  *atomic.Int64
	statNudges           *atomic.Int64
	statProtected        *atomic.Int64
	statProtectedPlayer  *atomic.Int64
	lifecycle            lifecycleTelemetry
	buffers              bufferTelemetry
	motion               bounceTelemetry
	sweep                cellSweep

	toggle
}

func NewStormSystem(world *engine.World) engine.System {
	s := &StormSystem{
		world: world,
	}

	s.memberExcludeSet = make(map[core.Entity]struct{}, 256)
	s.buffers = newBufferTelemetry(world.Resources.Status, "storm", "ellipse_offsets", "member_excludes")

	// Precompute ellipse cell offsets for wall collision checks
	s.buildEllipseOffsets()

	s.statActive = world.Resources.Status.Bools.Get("storm.active")
	s.statCircleCount = world.Resources.Status.Ints.Get("storm.circle_count")
	s.statGreenActiveFrame = world.Resources.Status.Ints.Get("storm.green_active_frames")
	s.statRedActiveFrame = world.Resources.Status.Ints.Get("storm.red_active_frames")
	s.statBlueActiveFrame = world.Resources.Status.Ints.Get("storm.blue_active_frames")
	s.statNudges = world.Resources.Status.Ints.Get("storm.nudge_count")
	s.statProtected = world.Resources.Status.Ints.Get("storm.protected_rejects")
	s.statProtectedPlayer = world.Resources.Status.Ints.Get("storm.protected_player_rejects")
	s.lifecycle = newLifecycleTelemetry(world.Resources.Status, "storm")
	s.motion = newBounceTelemetry(world.Resources.Status, "storm")
	for i := range circleVariants {
		s.graphs[i] = newCircleGraph(s, &circleVariants[i])
	}

	return s
}

// circleVariant is one circle colour's attack cycle: a circle waits out its
// cooldown, then attacks once it is convex, and the row times both and names what
// the attack runs
type circleVariant struct {
	initial, attack, repeat time.Duration                  // the spawn's cooldown, the attack, the cooldown after one
	flags                   uint64                         // Active's flags beside circleAttacking
	enter, tick             func(*StormSystem, *circleCtx) // Active's hooks
	cue                     uint16                         // ‰ of the attack at which materializeIn runs; 0 for none
}

// circleVariants is indexed by StormCircleType
var circleVariants = [component.StormCircleCount]circleVariant{
	component.StormCircleGreen: {
		initial: parameter.StormGreenInitialCooldown, attack: parameter.StormGreenRepeatInterval,
		repeat: parameter.StormGreenRepeatInterval, tick: (*StormSystem).pulse,
	},
	component.StormCircleRed: {
		initial: parameter.StormRedInitialCooldown, attack: parameter.StormRedBurstDuration,
		repeat: parameter.StormRedPostAttackDelay, flags: circleArmed, tick: (*StormSystem).burst,
	},
	component.StormCircleBlue: {
		initial: parameter.StormBlueInitialCooldown, attack: parameter.StormBlueEffectDuration,
		repeat: parameter.StormBlueRepeatCooldown, enter: (*StormSystem).findLanding, tick: (*StormSystem).glow,
		cue: parameter.StormBlueMaterializePermille,
	},
}

// Storm circle flags, which updateCircleAttacks writes to the components after every step
const (
	circleAttacking uint64 = 1 << iota // held in the convex, so not invulnerable
	circleArmed                        // the turret mount fires
)

// circleCtx is what one circle's step reads and its hooks write through
type circleCtx struct {
	entity    core.Entity
	circle    *component.StormCircleComponent
	x, y      int // the circle's cell
	hasCursor bool
	stunned   bool
}

// circleHeld suspends a circle with no cursor to attack or under a stun
func circleHeld(c *circleCtx) bool { return !c.hasCursor || c.stunned }

func circleConvex(c *circleCtx) bool { return !concave(c.circle) }

// newCircleGraph builds one colour's machine: a timed cooldown that ends in the
// attack once the circle is convex, and the attack, which re-enters the cooldown
// with the repeat duration. The spawn enters the cooldown with the initial one.
func newCircleGraph(s *StormSystem, v *circleVariant) *machine.Graph[component.StormCircleState, *circleCtx] {
	type edge = machine.Edge[component.StormCircleState, *circleCtx]
	type target = machine.Target[component.StormCircleState, *circleCtx]
	bind := func(hook func(*StormSystem, *circleCtx)) func(*circleCtx) {
		if hook == nil {
			return nil
		}
		return func(c *circleCtx) { hook(s, c) }
	}
	active := machine.State[*circleCtx]{
		Name:     "active",
		Duration: v.attack,
		Flags:    circleAttacking | v.flags,
		Enter:    bind(v.enter),
		Tick:     bind(v.tick),
	}
	if v.cue > 0 {
		active.Cues = []machine.Cue[*circleCtx]{{AtPermille: v.cue, Do: s.materializeIn}}
	}

	return machine.Must(machine.New[component.StormCircleState, *circleCtx](nil).
		State(component.StormCircleCooldown, machine.State[*circleCtx]{Name: "cooldown", Duration: v.repeat}).
		State(component.StormCircleActive, active).
		Suspend(circleHeld).
		// An expired cooldown waits for the convex, where a circle attacks from
		Timeout(component.StormCircleCooldown, edge{
			To:       []target{{State: component.StormCircleActive, Guard: circleConvex}},
			Fallback: machine.Hold,
		}).
		Timeout(component.StormCircleActive, edge{To: []target{{State: component.StormCircleCooldown}}}).
		Build())
}

func (s *StormSystem) Init() {
	s.rootEntity = 0
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	clear(s.memberExcludeSet)
	s.statActive.Store(false)
	s.statCircleCount.Store(0)
	s.statGreenActiveFrame.Store(0)
	s.statRedActiveFrame.Store(0)
	s.statBlueActiveFrame.Store(0)
	s.statNudges.Store(0)
	s.statProtected.Store(0)
	s.statProtectedPlayer.Store(0)
	s.lifecycle.Reset()
	s.buffers.Reset()
	s.motion.Reset()
	s.buffers.Observe(0, len(s.ellipseOffsets))
	s.enabled = true
}

func (s *StormSystem) Name() string {
	return "storm"
}

func (s *StormSystem) Priority() int {
	return parameter.PriorityStorm
}

func (s *StormSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventStormSpawnRequest,
		event.EventStormCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventMaterializeComplete,
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *StormSystem) HandleEvent(ev event.GameEvent) {

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}
	if ev.Type == event.EventSpeciesKilled {
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && payload.Species == component.SpeciesStorm {
			s.lifecycle.RecordKill(s.world, payload.KillerEntity)
		}
		return
	}

	if !s.enabled {
		if ev.Type == event.EventStormSpawnRequest {
			s.lifecycle.spawnFailures.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventStormSpawnRequest:
		if s.rootEntity == 0 {
			s.spawnStorm()
		} else {
			s.lifecycle.spawnFailures.Add(1)
		}

	case event.EventStormCancelRequest:
		if s.rootEntity != 0 {
			s.lifecycle.despawned.Add(1)
			s.terminateStorm()
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorStorm && payload.RemainingCount == 0 {
				s.handleCircleBreach(payload.HeaderEntity)
			}
		}

	case event.EventMaterializeComplete:
		// A blue attack's materialize is the only Shared one naming the swarm, and the
		// swarm it carries lands as it ends, on its footprint
		if payload, ok := ev.Payload.(*event.MaterializeCompletedPayload); ok &&
			ev.Domain == core.DomainShared && payload.Species == component.SpeciesSwarm {
			s.world.PushEvent(event.EventSwarmSpawnRequest, &event.SwarmSpawnRequestPayload{
				X: payload.X + parameter.SwarmHeaderOffsetX,
				Y: payload.Y + parameter.SwarmHeaderOffsetY,
			})
		}
	}
}

func (s *StormSystem) Update() {
	if !s.enabled || s.rootEntity == 0 {
		return
	}

	// When termination is requested, liveness edits intentionally are not
	// committed; retain the detached root value across that early return.
	stormComp, ok := s.world.Components.Storm.GetComponent(s.rootEntity)
	if !ok {
		s.rootEntity = 0
		s.statActive.Store(false)
		return
	}

	// Check liveness via Header existence (CompositeSystem authority)
	for i := range component.StormCircleCount {
		if stormComp.CirclesAlive[i] && !s.world.Components.Header.HasEntity(stormComp.Circles[i]) {
			stormComp.CirclesAlive[i] = false
		}
	}

	// Check if all circles dead
	aliveCount := s.AliveCount(&stormComp)
	if aliveCount == 0 {
		s.terminateStorm()
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtSec := min(dt.Seconds(), parameter.MaxSimulationDeltaSeconds)

	// Process each alive circle
	s.updateCirclePhysics(&stormComp, dtSec)
	s.updateCircleDamageImmunity(&stormComp, dt)
	s.updateCircleAttacks(&stormComp, dt)
	s.processCircleMemberCombat(&stormComp)
	s.handleCircleInteractions(&stormComp)

	s.world.Components.Storm.SetComponent(s.rootEntity, stormComp)
	s.statCircleCount.Store(int64(aliveCount))
}

// stormSnapshot is this system's D-19 record. The live storm's root decides future
// shared outcomes and lives outside any store: a receiver whose FSM was moved past
// StormSetup by an install never ran the spawn, so without it it holds the storm's
// entities and simulates none of them. A blue attack's swarm waits in its
// materialize, which the stores carry.
type stormSnapshot struct {
	RootEntity core.Entity `json:"root_entity"`
}

// SaveShared carries the storm's private state (D-19).
func (s *StormSystem) SaveShared() ([]byte, error) {
	return json.Marshal(stormSnapshot{RootEntity: s.rootEntity})
}

// LoadShared adopts a captured storm and republishes what derives from it.
func (s *StormSystem) LoadShared(data []byte) error {
	var snap stormSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("storm: %w", err)
	}
	s.rootEntity = snap.RootEntity

	alive := 0
	if stormComp, ok := s.world.Components.Storm.GetComponent(s.rootEntity); ok {
		alive = s.AliveCount(&stormComp)
	} else {
		s.rootEntity = 0
	}
	s.statActive.Store(s.rootEntity != 0)
	s.statCircleCount.Store(int64(alive))
	return nil
}

// buildEllipseOffsets populates the LUT of cell offsets inside the circle ellipse
func (s *StormSystem) buildEllipseOffsets() {
	rx, ry := parameter.StormCircleRadiusX, s.world.Resources.Config.CellAspect.Y(parameter.StormCircleRadiusY)
	radiusX, radiusY := int(rx), int(ry)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)

	// Preallocate approximate capacity: pi * rx * ry
	capacity := int(3.2 * float64(radiusX) * float64(radiusY))
	s.ellipseOffsets = make([]vmath.Point, 0, capacity)

	for y := -radiusY; y <= radiusY; y++ {
		for x := -radiusX; x <= radiusX; x++ {
			if vmath.EllipseContains(float64(x), float64(y), invRxSq, invRySq) {
				s.ellipseOffsets = append(s.ellipseOffsets, vmath.Point{X: x, Y: y})
			}
		}
	}
}

// AliveCount returns number of living circles
func (s *StormSystem) AliveCount(c *component.StormComponent) int {
	count := 0
	for _, alive := range c.CirclesAlive {
		if alive {
			count++
		}
	}
	return count
}

// spawnStorm creates the root header and 3 circle sub-headers
func (s *StormSystem) spawnStorm() {
	config := s.world.Resources.Config
	centerX := config.MapWidth / 2
	centerY := config.MapHeight / 2
	centerPX, centerPY := (vmath.Point{X: centerX, Y: centerY}).Center()

	// Pre-calculate circle spawn parameters
	angleOffsets := [3]float64{0, vmath.TwoPi / 3, 2 * vmath.TwoPi / 3}
	zOffsets := [3]float64{-1.0, 0.0, 1.0}
	initialRadius := parameter.StormInitialRadius
	initialSpeed := parameter.StormInitialSpeed
	baseZ := parameter.StormZMid

	type circleSpawnInfo struct {
		gridX, gridY int
		pos3D        vmath.Vec3
		vel3D        vmath.Vec3
	}

	var circleInfos [component.StormCircleCount]circleSpawnInfo

	// Every circle's Z jitter is drawn before any placement is validated: the loop
	// below abandons the whole spawn on the first circle that finds no room, and the
	// wall set it searches is Shared state a correction repairs (D-8).
	var zJitter [component.StormCircleCount]float64
	for i := range zJitter {
		zJitter[i] = float64(s.rng.Intn(6)-3) * 0.8
	}

	// 1. Calculate target positions and validate all circles
	for i := range component.StormCircleCount {
		angle := angleOffsets[i]
		offsetX := initialRadius * vmath.Cos(angle)
		offsetY := initialRadius * vmath.Sin(angle) * 0.5 // Terminal aspect ratio

		target := vmath.PointAt(centerPX+offsetX, centerPY+offsetY)
		targetX, targetY := target.X, target.Y

		// Find valid position via spiral search
		foundX, foundY, found := s.findCirclePosition(targetX, targetY)
		if !found {
			s.lifecycle.spawnFailures.Add(1)
			return // Abort entire spawn - one circle failed
		}

		// Circle center at the cell center, matching every other spawn path
		centerPX, centerPY := vmath.Point{X: foundX, Y: foundY}.Center()

		circleInfos[i] = circleSpawnInfo{
			gridX: foundX,
			gridY: foundY,
			pos3D: vmath.Vec3{
				X: centerPX,
				Y: centerPY,
				Z: baseZ + zOffsets[i]*parameter.StormZSpawnOffset,
			},
			vel3D: vmath.Vec3{
				X: -initialSpeed * vmath.Sin(angle),
				Y: initialSpeed * vmath.Cos(angle) * 0.5,
				Z: zJitter[i],
			},
		}
	}

	// 2. Clear all spawn areas (validation passed)
	for i := range component.StormCircleCount {
		s.clearCircleSpawnArea(circleInfos[i].gridX, circleInfos[i].gridY)
	}

	// 3. Create entities
	rootEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Components.Protection.SetComponent(rootEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	stormComp := component.StormComponent{}

	for i := range component.StormCircleCount {
		circleEntity := s.createCircleHeader(rootEntity, i, circleInfos[i].pos3D, circleInfos[i].vel3D)
		stormComp.Circles[i] = circleEntity
		stormComp.CirclesAlive[i] = true
	}

	// Root header component linking circles
	rootMembers := make([]component.MemberEntry, component.StormCircleCount)
	for i := range component.StormCircleCount {
		rootMembers[i] = component.MemberEntry{
			Entity:  stormComp.Circles[i],
			OffsetX: 0,
			OffsetY: 0,
		}
	}

	s.world.Components.Header.SetComponent(rootEntity, component.HeaderComponent{
		Behavior:      component.BehaviorStorm,
		Type:          component.CompositeTypeContainer,
		MemberEntries: rootMembers,
	})

	s.world.Components.Storm.SetComponent(rootEntity, stormComp)

	s.rootEntity = rootEntity
	s.statActive.Store(true)
	s.statCircleCount.Store(component.StormCircleCount)

	// Announce the fully initialized species instance.
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity:      rootEntity,
		Species:     component.SpeciesStorm,
		X:           -1,
		Y:           -1,
		MemberCount: len(rootMembers),
	})
	s.lifecycle.spawned.Add(1)
}

// findCirclePosition finds the centre nearest the target, the target first, whose
// elliptical footprint lies inside the map clear of walls a spawn is kept from
func (s *StormSystem) findCirclePosition(targetX, targetY int) (int, int, bool) {
	return s.world.Positions.SpiralSearch(targetX, targetY, 0, s.isCirclePositionValid)
}

// isCirclePositionValid checks if all ellipse member cells at this center are valid
func (s *StormSystem) isCirclePositionValid(centerX, centerY int) bool {
	config := s.world.Resources.Config

	for _, off := range s.ellipseOffsets {
		cellX := centerX + off.X
		cellY := centerY + off.Y

		if cellX < 0 || cellX >= config.MapWidth || cellY < 0 || cellY >= config.MapHeight {
			return false
		}
		if s.world.Positions.HasBlockingWallAt(cellX, cellY, component.WallBlockSpawn) {
			return false
		}
	}

	return true
}

// clearCircleSpawnArea empties a circle footprint in both domains (D-12)
func (s *StormSystem) clearCircleSpawnArea(centerX, centerY int) {
	s.sweep.reset()
	for _, off := range s.ellipseOffsets {
		s.sweep.collect(s.world, centerX+off.X, centerY+off.Y, func(e core.Entity) bool {
			return speciesClearable(s.world, e, s.statProtected, s.statProtectedPlayer)
		})
	}
	s.sweep.destroy(s.world)
}

// createCircleHeader builds a single circle sub-header entity
func (s *StormSystem) createCircleHeader(
	parentEntity core.Entity,
	index int,
	pos3D, vel3D vmath.Vec3,
) core.Entity {
	cell := vmath.PointAt(pos3D.X, pos3D.Y)

	circleEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(circleEntity, component.PositionComponent{X: cell.X, Y: cell.Y})

	// Circle headers are protected
	s.world.Components.Protection.SetComponent(circleEntity, component.ProtectionComponent{
		// Mask: component.ProtectAll ^ component.ProtectFromDeath,
		Mask: component.ProtectAll,
	})

	// Storm circle component (3D physics + attack state)
	s.world.Components.StormCircle.SetComponent(circleEntity, component.StormCircleComponent{
		Pos3D: pos3D,
		Vel3D: vel3D,
		Index: index,
	})

	if component.StormCircleType(index) == component.StormCircleRed {
		s.world.Components.Mount.SetComponent(circleEntity, component.MountComponent{
			Weapon:   component.WeaponTurret,
			Trigger:  component.MountArmed,
			Interval: parameter.StormRedFireInterval,
			Muzzle:   parameter.StormCircleRadiusX * parameter.StormRedBulletSpawnMargin,
		})
	}

	// Kinetic component for 2D collision compatibility
	s.world.Components.Kinetic.SetComponent(circleEntity, component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: pos3D.X,
			PreciseY: pos3D.Y,
			// Seeded so the first impulse-absorption delta is zero
			VelX: vel3D.X,
			VelY: vel3D.Y,
		},
	})

	// Combat component
	s.world.Components.Combat.SetComponent(circleEntity, component.CombatComponent{
		OwnerEntity:      circleEntity,
		CombatEntityType: component.CombatEntityStorm,
		HitPoints:        0, // Damage routed to members via ablative model
	})

	// Generate members
	members := s.createCircleMembers(circleEntity, cell.X, cell.Y)

	// Header component linking back to root
	s.world.Components.Header.SetComponent(circleEntity, component.HeaderComponent{
		Behavior:      component.BehaviorStorm,
		Type:          component.CompositeTypeAblative,
		MemberEntries: members,
		ParentHeader:  parentEntity,
	})

	// Backlink as member of root
	s.world.Components.Member.SetComponent(circleEntity, component.MemberComponent{
		HeaderEntity: parentEntity,
	})

	// The machine starts in the colour's first cooldown once the circle is built
	circleComp, _ := s.world.Components.StormCircle.GetPtr(circleEntity)
	s.ctx = circleCtx{entity: circleEntity, circle: circleComp}
	s.graphs[index].Start(&circleComp.Machine, &s.ctx, component.StormCircleCooldown, circleVariants[index].initial)

	return circleEntity
}

// createCircleMembers builds a single circle's member entities
func (s *StormSystem) createCircleMembers(headerEntity core.Entity, headerX, headerY int) []component.MemberEntry {
	members := make([]component.MemberEntry, 0, len(s.ellipseOffsets))

	for _, off := range s.ellipseOffsets {
		memberEntity := s.world.CreateEntity(core.DomainShared)

		s.world.Positions.SetPosition(memberEntity, component.PositionComponent{
			X: headerX + off.X,
			Y: headerY + off.Y,
		})

		s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
			Mask: component.ProtectFromParticle | component.ProtectFromSpecies,
		})

		// Ablative health: per-member HP for combat damage
		s.world.Components.Combat.SetComponent(memberEntity, component.CombatComponent{
			OwnerEntity:      headerEntity,
			CombatEntityType: component.CombatEntityStorm,
			HitPoints:        parameter.CombatInitialHPStormMember,
		})

		s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
			HeaderEntity: headerEntity,
		})

		members = append(members, component.MemberEntry{
			Entity:  memberEntity,
			OffsetX: off.X,
			OffsetY: off.Y,
		})
	}
	return members
}

// concave reports whether a circle sits behind the vulnerability boundary
func concave(c *component.StormCircleComponent) bool {
	return c.Pos3D.Z >= parameter.StormZMid
}

// collectAndDestroyWallsInEllipse finds walls in ellipse footprint, emits despawn requests, returns true if any found
func (s *StormSystem) collectAndDestroyWallsInEllipse(centerX, centerY int) bool {
	found := false

	for _, off := range s.ellipseOffsets {
		cellX := centerX + off.X
		cellY := centerY + off.Y

		if s.world.Positions.HasBlockingWallAt(cellX, cellY, component.WallBlockKinetic) {
			s.world.PushEvent(event.EventWallDespawnRequest, &event.WallDespawnRequestPayload{
				X:      cellX,
				Y:      cellY,
				Width:  1,
				Height: 1,
			})
			found = true
		}
	}

	return found
}

// updateCirclePhysics handles 3D gravitational orbits and inter-circle collision
func (s *StormSystem) updateCirclePhysics(stormComp *component.StormComponent, dtSec float64) {
	config := s.world.Resources.Config

	// Collect alive circles
	type circleState struct {
		circle  *component.StormCircleComponent
		entity  core.Entity
		stunned bool
	}

	// StormCircle lifecycle changes are event-queued, so all direct pointers stay
	// valid through the physics, collision, and synchronization phases below. The
	// population is fixed at three, so keep this hot-path buffer on the stack.
	var circleBuffer [component.StormCircleCount]circleState
	circleCount := 0
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}
		circleComp, ok := s.world.Components.StormCircle.GetPtr(stormComp.Circles[i])
		if !ok {
			stormComp.CirclesAlive[i] = false
			continue
		}
		combatComp, ok := s.world.Components.Combat.GetPtr(stormComp.Circles[i])
		circleBuffer[circleCount] = circleState{
			entity:  stormComp.Circles[i],
			circle:  circleComp,
			stunned: ok && combatComp.Status[component.StatusStun].Remaining > 0,
		}
		circleCount++
	}

	if circleCount == 0 {
		return
	}
	circles := circleBuffer[:circleCount]

	// Precompute center-aligned boundary limits accounting for ellipse radius.
	minX, minY := (vmath.Point{X: 0, Y: 0}).Center()
	maxX, maxY := (vmath.Point{X: config.MapWidth - 1, Y: config.MapHeight - 1}).Center()
	boundMinX := minX + parameter.StormBoundaryInsetX
	boundMaxX := maxX - parameter.StormBoundaryInsetX
	boundMinY := minY + parameter.StormBoundaryInsetY
	boundMaxY := maxY - parameter.StormBoundaryInsetY

	for i := range circles {
		// 1. Stunned circles: skip physics, velocity already zeroed by combat system
		if circles[i].stunned {
			continue
		}
		motion := physics.BounceStats{Steps: 1}

		// 2. Fold combat/dust knockback from the 2D Kinetic into Vel3D
		s.absorbExternalImpulse(circles[i].entity, circles[i].circle)

		// 3. Accumulate gravitational acceleration with repulsion
		var accelX, accelY, accelZ float64
		for j := range circles {
			if i == j {
				continue
			}
			accel := physics.GravitationalAccelWithRepulsion3D(
				circles[i].circle.Pos3D,
				circles[j].circle.Pos3D,
				profile.MassStorm,
				parameter.StormGravity,
				parameter.StormRepulsionRadius,
				parameter.StormRepulsionStrength,
			)
			accelX += accel.X
			accelY += accel.Y
			accelZ += accel.Z
		}

		// 4. Z-axis equilibrium spring: accelZ += stiffness * (zMid - z)
		// Provides restoring force toward vulnerability boundary
		zDelta := parameter.StormZMid - circles[i].circle.Pos3D.Z
		accelZ += parameter.StormZEquilibriumStiffness * zDelta

		// 5. Integrate velocity
		circles[i].circle.Vel3D.X += accelX * dtSec
		circles[i].circle.Vel3D.Y += accelY * dtSec
		circles[i].circle.Vel3D.Z += accelZ * dtSec

		// 6. Apply damping
		circles[i].circle.Vel3D = vmath.V3DampDt(circles[i].circle.Vel3D, parameter.StormDamping, dtSec)

		// 7. Clamp velocity
		circles[i].circle.Vel3D = vmath.V3ClampMagnitude(circles[i].circle.Vel3D, parameter.StormMaxVelocity)

		// 8. Axis-separated position integration with collision

		// --- X Axis ---
		oldPosX := circles[i].circle.Pos3D.X
		circles[i].circle.Pos3D.X += circles[i].circle.Vel3D.X * dtSec

		// Boundary check X
		if circles[i].circle.Pos3D.X < boundMinX {
			motion.BoundaryReflections++
			circles[i].circle.Pos3D.X = boundMinX
			if circles[i].circle.Vel3D.X < 0 {
				circles[i].circle.Vel3D.X = -circles[i].circle.Vel3D.X * parameter.StormRestitution
			}
		} else if circles[i].circle.Pos3D.X > boundMaxX {
			motion.BoundaryReflections++
			circles[i].circle.Pos3D.X = boundMaxX
			if circles[i].circle.Vel3D.X > 0 {
				circles[i].circle.Vel3D.X = -circles[i].circle.Vel3D.X * parameter.StormRestitution
			}
		} else {
			// Wall check X (only if within bounds)
			cell := vmath.PointAt(circles[i].circle.Pos3D.X, circles[i].circle.Pos3D.Y)
			if s.collectAndDestroyWallsInEllipse(cell.X, cell.Y) {
				motion.WallCollisions++
				circles[i].circle.Pos3D.X = oldPosX
				circles[i].circle.Vel3D.X = -circles[i].circle.Vel3D.X * parameter.StormRestitution
			}
		}

		// --- Y Axis ---
		oldPosY := circles[i].circle.Pos3D.Y
		circles[i].circle.Pos3D.Y += circles[i].circle.Vel3D.Y * dtSec

		// Boundary check Y
		if circles[i].circle.Pos3D.Y < boundMinY {
			motion.BoundaryReflections++
			circles[i].circle.Pos3D.Y = boundMinY
			if circles[i].circle.Vel3D.Y < 0 {
				circles[i].circle.Vel3D.Y = -circles[i].circle.Vel3D.Y * parameter.StormRestitution
			}
		} else if circles[i].circle.Pos3D.Y > boundMaxY {
			motion.BoundaryReflections++
			circles[i].circle.Pos3D.Y = boundMaxY
			if circles[i].circle.Vel3D.Y > 0 {
				circles[i].circle.Vel3D.Y = -circles[i].circle.Vel3D.Y * parameter.StormRestitution
			}
		} else {
			// Wall check Y (uses potentially updated X position)
			cell := vmath.PointAt(circles[i].circle.Pos3D.X, circles[i].circle.Pos3D.Y)
			if s.collectAndDestroyWallsInEllipse(cell.X, cell.Y) {
				motion.WallCollisions++
				circles[i].circle.Pos3D.Y = oldPosY
				circles[i].circle.Vel3D.Y = -circles[i].circle.Vel3D.Y * parameter.StormRestitution
			}
		}

		// --- Z Axis (depth bounds only, no walls) ---
		circles[i].circle.Pos3D.Z += circles[i].circle.Vel3D.Z * dtSec
		if physics.ReflectAxis3D(&circles[i].circle.Pos3D.Z, &circles[i].circle.Vel3D.Z,
			parameter.StormZMin, parameter.StormZMax, parameter.StormRestitution) {
			motion.BoundaryReflections++
		}

		// --- ATTACK PHYSICS OVERRIDE ---
		// If attacking, physically trap the circle in the convex
		if circles[i].circle.Machine.State == component.StormCircleActive {
			// Define a boundary slightly in front of the Mid point to ensure IsConvex returns true
			zLimit := parameter.StormZMid - 1.0

			if circles[i].circle.Pos3D.Z > zLimit {
				circles[i].circle.Pos3D.Z = zLimit

				// Kill outward (receding) momentum, but allow it to move further forward
				if circles[i].circle.Vel3D.Z > 0 {
					circles[i].circle.Vel3D.Z = 0
				}
			}
		}
		s.motion.Record(motion)
	}

	// Inter-circle collision (skip stunned circles)
	for i := range len(circles) {
		if circles[i].stunned {
			continue
		}
		for j := i + 1; j < len(circles); j++ {
			if circles[j].stunned {
				continue
			}
			s.resolveCircleCollision(circles[i].circle, circles[j].circle)
		}
	}

	// Sync 3D position to 2D components
	for i := range circles {
		circle := circles[i].circle
		circleEntity := circles[i].entity

		cell := vmath.PointAt(circle.Pos3D.X, circle.Pos3D.Y)
		newGridX, newGridY := cell.X, cell.Y

		// Update grid position
		if pos, ok := s.world.Positions.GetPosition(circleEntity); ok {
			if pos.X != newGridX || pos.Y != newGridY {
				s.processCircleCollisions(circleEntity, newGridX, newGridY)
				s.world.Positions.SetPosition(circleEntity, component.PositionComponent{X: newGridX, Y: newGridY})
			}
		}

		// Update kinetic for 2D collision compatibility
		if kinetic, ok := s.world.Components.Kinetic.GetPtr(circleEntity); ok {
			kinetic.PreciseX = circle.Pos3D.X
			kinetic.PreciseY = circle.Pos3D.Y
			kinetic.VelX = circle.Vel3D.X
			kinetic.VelY = circle.Vel3D.Y
		}
	}
}

// absorbExternalImpulse folds knockback written to the 2D Kinetic into the
// authoritative 3D velocity. The end-of-tick sync leaves Kinetic.Vel equal to
// Vel3D.XY, so any difference is an external impulse from combat or dust.
// Z is untouched: knockback sources act in the terminal plane.
func (s *StormSystem) absorbExternalImpulse(circleEntity core.Entity, circle *component.StormCircleComponent) {
	kinetic, ok := s.world.Components.Kinetic.GetPtr(circleEntity)
	if !ok {
		return
	}

	deltaX := kinetic.VelX - circle.Vel3D.X
	deltaY := kinetic.VelY - circle.Vel3D.Y
	if deltaX == 0 && deltaY == 0 {
		return
	}

	circle.Vel3D.X += deltaX
	circle.Vel3D.Y += deltaY
}

// resolveCircleCollision handles elastic collision between two circles
func (s *StormSystem) resolveCircleCollision(a, b *component.StormCircleComponent) {
	minDist := parameter.StormCircleCollisionRadius * 2
	if vmath.V3MagSq(vmath.V3Sub(b.Pos3D, a.Pos3D)) >= minDist*minDist {
		return
	}

	// SeparateOverlap3D already rejects non-overlapping and coincident pairs
	physics.SeparateOverlap3D(
		&a.Pos3D, &b.Pos3D,
		parameter.StormCircleCollisionRadius, parameter.StormCircleCollisionRadius,
		profile.MassStorm, profile.MassStorm,
	)

	// Elastic collision response (in-place modification)
	collided := physics.ElasticCollision3D(
		&a.Pos3D, &b.Pos3D,
		&a.Vel3D, &b.Vel3D,
		profile.MassStorm, profile.MassStorm,
		parameter.StormRestitution,
	)
	if collided {
		s.world.PushLocal(event.EventDustAllRequest, nil)
	}
}

// processCircleCollisions destroys non-protected entities at a circle's footprint
func (s *StormSystem) processCircleCollisions(circleEntity core.Entity, newGridX, newGridY int) {
	// Build member exclusion set
	headerComp, hasHeader := s.world.Components.Header.GetPtr(circleEntity)
	clear(s.memberExcludeSet)
	s.memberExcludeSet[circleEntity] = struct{}{}
	if hasHeader {
		for _, m := range headerComp.MemberEntries {
			if m.Entity != 0 {
				s.memberExcludeSet[m.Entity] = struct{}{}
			}
		}
	}
	s.buffers.Observe(1, len(s.memberExcludeSet))

	s.sweep.reset()
	for _, off := range s.ellipseOffsets {
		s.sweep.collect(s.world, newGridX+off.X, newGridY+off.Y, func(e core.Entity) bool {
			if _, excluded := s.memberExcludeSet[e]; excluded {
				return false
			}
			if s.world.Components.Cursor.HasEntity(e) || s.world.Components.Wall.HasEntity(e) {
				return false
			}
			if prot, ok := s.world.Components.Protection.GetPtr(e); ok &&
				(prot.Mask&component.ProtectFromSpecies != 0 || prot.Mask == component.ProtectAll) {
				countSpeciesProtected(e, s.statProtected, s.statProtectedPlayer)
				return false
			}
			return true
		})
	}

	// Personal nuggets receive a local lifecycle notification before the player batch dies.
	for _, e := range s.sweep.player {
		if s.world.Components.Nugget.HasEntity(e) {
			s.world.PushLocal(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: e})
		}
	}
	s.sweep.emit(s.world, event.EventFlashSpawnOneRequest)
}

// processCircleMemberCombat scans members for HP<=0 and routes deaths through CompositeSystem, storm system it the combat-based lifecycle authority
func (s *StormSystem) processCircleMemberCombat(stormComp *component.StormComponent) {
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		headerComp, ok := s.world.Components.Header.GetPtr(circleEntity)
		if !ok {
			continue
		}

		// Scan members for combat deaths
		var deadMembers []core.Entity
		livingCount := 0

		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}

			combatComp, ok := s.world.Components.Combat.GetPtr(member.Entity)
			if !ok {
				// Storm members always have CombatComponent; absence = dead
				continue
			}

			if combatComp.HitPoints <= 0 {
				deadMembers = append(deadMembers, member.Entity)
			} else {
				livingCount++
			}
		}

		// Emit deaths for members with HP<=0
		for _, memberEntity := range deadMembers {
			s.world.PushEvent(event.EventCompositeMemberDestroyed, &event.CompositeMemberDestroyedPayload{
				HeaderEntity: circleEntity,
				MemberEntity: memberEntity,
			})
		}

		// Circle destruction: trigger when no living members remain
		if livingCount == 0 && stormComp.CirclesAlive[i] {
			s.destroyCircle(stormComp, i)
		}
	}
}

// destroyCircle handles individual circle death
func (s *StormSystem) destroyCircle(stormComp *component.StormComponent, index int) {
	circleEntity := stormComp.Circles[index]
	var killerEntity core.Entity
	if combatComp, ok := s.world.Components.Combat.GetComponent(circleEntity); ok {
		killerEntity = combatComp.LastDamagedBy
	}

	// Get position for event; the final circle may already be positionless.
	posX, posY := -1, -1
	if pos, ok := s.world.Positions.GetPosition(circleEntity); ok {
		posX, posY = pos.X, pos.Y
	}

	// Mark as dead
	stormComp.CirclesAlive[index] = false

	// Destroy circle header via composite system
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: circleEntity,
		Effect:       0, // Silent destruction
	})

	// Check if all dead
	if s.AliveCount(stormComp) == 0 {
		// The root is the species identity; circles are implementation details.
		s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
			Entity:       s.rootEntity,
			KillerEntity: killerEntity,
			Species:      component.SpeciesStorm,
			X:            posX,
			Y:            posY,
		})
	}
}

// handleCircleBreach retires a circle whose last member died outside combat.
// A partial loss is ordinary ablative damage and never reaches here: retiring on
// one would strand the survivors, since render, physics and member reaping all
// key off CirclesAlive.
func (s *StormSystem) handleCircleBreach(headerEntity core.Entity) {
	if s.rootEntity == 0 {
		return
	}

	stormComp, ok := s.world.Components.Storm.GetPtr(s.rootEntity)
	if !ok {
		return
	}

	// Find which circle was breached
	for i := range component.StormCircleCount {
		if stormComp.Circles[i] == headerEntity && stormComp.CirclesAlive[i] {
			stormComp.CirclesAlive[i] = false

			// The flag alone leaves the header entity live and combat-resolvable
			s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
				HeaderEntity: headerEntity,
				Effect:       0,
			})

			if s.AliveCount(stormComp) == 0 {
				// The last circle disappeared through lifecycle cleanup rather than
				// credited combat. Negative coordinates suppress loot placement.
				s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
					Entity:  s.rootEntity,
					Species: component.SpeciesStorm,
					X:       -1,
					Y:       -1,
				})
			}

			return
		}
	}
}

// handleCircleInteractions strikes the cursors each live circle touches
func (s *StormSystem) handleCircleInteractions(stormComp *component.StormComponent) {
	for i := range component.StormCircleCount {
		if stormComp.CirclesAlive[i] {
			strikeContacts(s.world, stormComp.Circles[i], profile.Contact[component.SpeciesStorm], true)
		}
	}
}

// updateCircleDamageImmunity sets immunity for concave circles and handles anti-deadlock nudge
func (s *StormSystem) updateCircleDamageImmunity(stormComp *component.StormComponent, dt time.Duration) {
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		circleComp, ok := s.world.Components.StormCircle.GetPtr(circleEntity)
		if !ok {
			continue
		}

		if !concave(circleComp) {
			circleComp.InvulnerableElapsed = 0
			continue
		}

		// Nudge out of a stalled concave orbit, then restart the accumulator
		circleComp.InvulnerableElapsed += dt
		if circleComp.InvulnerableElapsed > parameter.StormInvulnerabilityMaxDuration {
			circleComp.Vel3D.Z -= parameter.StormInvulnerabilityNudge
			circleComp.InvulnerableElapsed = 0
			s.statNudges.Add(1)
		}

		headerComp, ok := s.world.Components.Header.GetPtr(circleEntity)
		if !ok {
			continue
		}
		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}
			memberCombat, ok := s.world.Components.Combat.GetPtr(member.Entity)
			if !ok {
				continue
			}
			memberCombat.Status[component.StatusWard] = component.Status{Remaining: parameter.CombatDamageImmunityDuration}
		}
	}
}

// updateCircleAttacks steps each circle's attack machine and writes its outputs
func (s *StormSystem) updateCircleAttacks(stormComp *component.StormComponent, dt time.Duration) {
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		circleComp, ok := s.world.Components.StormCircle.GetPtr(circleEntity)
		if !ok {
			continue
		}

		circlePos, ok := s.world.Positions.GetPosition(circleEntity)
		if !ok {
			continue
		}

		_, _, _, hasCursor := ClosestCursor(s.world, circlePos.X, circlePos.Y)
		combatComp, ok := s.world.Components.Combat.GetPtr(circleEntity)
		s.ctx = circleCtx{
			entity:    circleEntity,
			circle:    circleComp,
			x:         circlePos.X,
			y:         circlePos.Y,
			hasCursor: hasCursor,
			stunned:   ok && combatComp.Status[component.StatusStun].Remaining > 0,
		}
		flags, suspended := s.graphs[circleComp.Index].Step(&circleComp.Machine, &s.ctx, dt)

		// The state's outputs, written on every step: a held turret fires nothing
		circleComp.IsInvulnerable = concave(circleComp) && flags&circleAttacking == 0
		if mount, ok := s.world.Components.Mount.GetPtr(circleEntity); ok {
			mount.Armed = flags&circleArmed != 0 && !suspended
		}
	}
}

// stormGreenDamage is what the green circle's pulse costs each cursor inside it
var stormGreenDamage = component.CursorDamage{
	EnergyDrain: parameter.StormGreenDamageEnergy,
	HeatDelta:   -parameter.StormGreenDamageHeat,
}

// pulse strikes the cursors inside the green's area, every tick of its attack
func (s *StormSystem) pulse(c *circleCtx) {
	s.statGreenActiveFrame.Add(1)

	invRxSq, invRySq := vmath.EllipseInvRadiiSq(parameter.StormGreenRadiusX,
		s.world.Resources.Config.CellAspect.Y(parameter.StormGreenRadiusY))
	strikeCursorsIn(s.world, func(x, y int) bool {
		return vmath.EllipseContainsPoint(x, y, c.x, c.y, invRxSq, invRySq)
	}, stormGreenDamage)
}

// burst counts the red's attack ticks; its armed turret mount fires them
func (s *StormSystem) burst(*circleCtx) { s.statRedActiveFrame.Add(1) }

// glow counts the blue's attack ticks; its cue materializes the swarm in
func (s *StormSystem) glow(*circleCtx) { s.statBlueActiveFrame.Add(1) }

// findLanding opens a blue attack: it draws the direction, then searches for room
// there, so a search that fails has still drawn (D-8)
func (s *StormSystem) findLanding(c *circleCtx) {
	angle := s.rng.Float64() * vmath.TwoPi
	distance := parameter.StormBlueSpawnDistance

	circlePX, circlePY := (vmath.Point{X: c.x, Y: c.y}).Center()
	target := vmath.PointAt(
		circlePX+distance*vmath.Cos(angle),
		circlePY+distance*vmath.Sin(angle)*0.5,
	)

	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaInside(
		target.X, target.Y,
		parameter.SwarmWidth, parameter.SwarmHeight,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	c.circle.LandingFound = found
	c.circle.AttackTargetX = topLeftX + parameter.SwarmHeaderOffsetX
	c.circle.AttackTargetY = topLeftY + parameter.SwarmHeaderOffsetY
}

// materializeIn materializes a swarm onto the landing its attack found: a cue, so
// once an attack. The materialize names the species, so the swarm lands as it ends.
func (s *StormSystem) materializeIn(c *circleCtx) {
	if !c.circle.LandingFound {
		return
	}
	s.world.PushEvent(event.EventMaterializeAreaRequest, &event.MaterializeAreaRequestPayload{
		X:          c.circle.AttackTargetX - parameter.SwarmHeaderOffsetX,
		Y:          c.circle.AttackTargetY - parameter.SwarmHeaderOffsetY,
		AreaWidth:  parameter.SwarmWidth,
		AreaHeight: parameter.SwarmHeight,
		Type:       component.SpawnTypeSwarm,
		Species:    component.SpeciesSwarm,
	})
}

// terminateStorm destroys the entire storm entity
func (s *StormSystem) terminateStorm() {
	if s.rootEntity == 0 {
		return
	}

	stormComp, ok := s.world.Components.Storm.GetComponent(s.rootEntity)
	if ok {
		// Keyed on the entity, not on CirclesAlive: a circle retired without its
		// header destroyed would outlive the storm, unrendered and still targetable
		for i := range component.StormCircleCount {
			if s.world.Components.Header.HasEntity(stormComp.Circles[i]) {
				s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
					HeaderEntity: stormComp.Circles[i],
					Effect:       0,
				})
			}
		}
	}

	// Destroy root
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: s.rootEntity,
		Effect:       0,
	})

	// A materialize in flight would land its swarm after the storm
	var landing []core.Entity
	for _, e := range s.world.Components.Materialize.Entities() {
		if m, ok := s.world.Components.Materialize.GetPtr(e); ok && e.Domain() == core.DomainShared && m.Species == component.SpeciesSwarm {
			landing = append(landing, e)
		}
	}
	event.EmitDeath(s.world.Resources.Event.Queue, 0, landing...)

	s.rootEntity = 0
	s.statActive.Store(false)
	s.statCircleCount.Store(0)
}

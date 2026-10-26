package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// QuasarSystem manages quasar boss entity lifecycle
// Quasar is a 3x5 composite that tracks cursor, zaps when cursor exits range
type QuasarSystem struct {
	world *engine.World

	graph *machine.Graph[component.QuasarState, *quasarCtx]
	ctx   quasarCtx // the quasar being stepped, refilled per entity

	// Telemetry
	statActive          *atomic.Bool
	statCount           *atomic.Int64
	statProtected       *atomic.Int64
	statProtectedPlayer *atomic.Int64
	lifecycle           lifecycleTelemetry
	motion              bounceTelemetry
	sweep               cellSweep

	toggle
}

// NewQuasarSystem creates a new quasar system
func NewQuasarSystem(world *engine.World) engine.System {
	s := &QuasarSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("quasar.active")
	s.statCount = world.Resources.Status.Ints.Get("quasar.count")
	s.statProtected = world.Resources.Status.Ints.Get("quasar.protected_rejects")
	s.statProtectedPlayer = world.Resources.Status.Ints.Get("quasar.protected_player_rejects")
	s.lifecycle = newLifecycleTelemetry(world.Resources.Status, "quasar")
	s.motion = newBounceTelemetry(world.Resources.Status, "quasar")
	s.graph = newQuasarGraph(s, nil)

	return s
}

// Quasar flags, which Update writes to the components after every step
const (
	quasarEnraged  uint64 = 1 << iota // combat's enrage, which refuses kinetic knockback
	quasarShielded                    // shield up and ward held: no damage, no stun
)

// quasarCtx is what one quasar's step reads and its hooks write through
type quasarCtx struct {
	entity  core.Entity
	quasar  *component.QuasarComponent
	stunned bool
	inRange bool // the nearest cursor is inside the zap circle, or there is none to zap
	dt      time.Duration
	dtSec   float64 // dt capped against tunneling

	// The zap probe's resolution: the header and the cursor the lightning aims at
	originX, originY int
	targetX, targetY int
}

// newQuasarGraph builds the quasar's machine: homing, a timed charge while the
// cursor stays out of range, then a shielded zap until it comes back. A stun cancels
// a charge or a zap and freezes the quasar; observe may be nil.
func newQuasarGraph(s *QuasarSystem, observe machine.Observer[component.QuasarState]) *machine.Graph[component.QuasarState, *quasarCtx] {
	type edge = machine.Edge[component.QuasarState, *quasarCtx]
	type target = machine.Target[component.QuasarState, *quasarCtx]
	active := machine.MaskOf(component.QuasarCharging, component.QuasarZapping)
	homing := []target{{State: component.QuasarHoming}}

	return machine.Must(machine.New[component.QuasarState, *quasarCtx](observe).
		State(component.QuasarHoming, machine.State[*quasarCtx]{
			Name: "homing",
			Tick: s.home,
		}).
		State(component.QuasarCharging, machine.State[*quasarCtx]{
			Name:     "charging",
			Duration: parameter.QuasarChargeDuration,
			Flags:    quasarEnraged,
			Enter:    s.enterCharging,
			Exit:     s.exitCharging,
			Tick:     s.home,
		}).
		State(component.QuasarZapping, machine.State[*quasarCtx]{
			Name:  "zapping",
			Flags: quasarEnraged | quasarShielded,
			Probe: s.probeZap,
			Enter: s.enterZapping,
			Exit:  s.exitZapping,
			Tick:  s.zap,
		}).
		Global(edge{From: active, Guard: quasarStunned, To: homing}).
		Global(edge{From: active, Guard: quasarInRange, To: homing}).
		Suspend(quasarStunned).
		PreTick(component.QuasarHoming, edge{Guard: quasarOutOfRange, To: []target{{State: component.QuasarCharging}}}).
		// A zap that cannot aim falls back to homing, so a charge always ends
		Timeout(component.QuasarCharging, edge{To: []target{{State: component.QuasarZapping}, {State: component.QuasarHoming}}}).
		Build())
}

func quasarStunned(c *quasarCtx) bool    { return c.stunned }
func quasarInRange(c *quasarCtx) bool    { return c.inRange }
func quasarOutOfRange(c *quasarCtx) bool { return !c.inRange }

func (s *QuasarSystem) Init() {
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.statProtected.Store(0)
	s.statProtectedPlayer.Store(0)
	s.lifecycle.Reset()
	s.motion.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *QuasarSystem) Name() string {
	return "quasar"
}

func (s *QuasarSystem) Priority() int {
	return parameter.PriorityQuasar
}

func (s *QuasarSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventQuasarSpawnRequest,
		event.EventQuasarCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *QuasarSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}
	if ev.Type == event.EventSpeciesKilled {
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && payload.Species == component.SpeciesQuasar {
			s.lifecycle.RecordKill(s.world, payload.KillerEntity)
		}
		return
	}

	if !s.enabled {
		if ev.Type == event.EventQuasarSpawnRequest {
			s.lifecycle.spawnFailures.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventQuasarSpawnRequest:
		if payload, ok := ev.Payload.(*event.QuasarSpawnRequestPayload); ok {
			s.spawnQuasar(payload.X, payload.Y)
		}

	case event.EventQuasarCancelRequest:
		// Cancel all quasars
		entities := s.world.Components.Quasar.Entities()
		s.lifecycle.despawned.Add(int64(len(entities)))
		for _, entity := range entities {
			s.terminateQuasar(entity)
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if s.world.Components.Quasar.HasEntity(payload.HeaderEntity) {
				s.lifecycle.despawned.Add(1)
				s.terminateQuasar(payload.HeaderEntity)
			}
		}
	}
}

func (s *QuasarSystem) Update() {
	if !s.enabled {
		return
	}

	quasars := s.world.Components.Quasar
	quasarEntities := quasars.Entities()
	activeCount := 0

	// Zap range calculations, dynamic resize based on map
	width := s.world.Resources.Config.MapWidth
	height := s.world.Resources.Config.MapHeight
	currentRadius := float64(max(width/2, height))

	dt := s.world.Resources.Time.DeltaTime
	// Cap delta to prevent tunneling
	dtSec := min(dt.Seconds(), parameter.MaxSimulationDeltaSeconds)

	for _, headerEntity := range quasarEntities {
		quasarComp, ok := quasars.GetPtr(headerEntity)
		if !ok {
			s.lifecycle.despawned.Add(1)
			s.terminateQuasar(headerEntity)
			continue
		}
		quasarComp.ZapRadius = currentRadius

		// Combat sync
		combatComp, ok := s.world.Components.Combat.GetPtr(headerEntity)
		if !ok {
			continue
		}

		// Hitpoint check
		if combatComp.HitPoints <= 0 {
			killX, killY := -1, -1
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				killX, killY = headerPos.X, headerPos.Y
			}
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:       headerEntity,
				KillerEntity: combatComp.LastDamagedBy,
				Species:      component.SpeciesQuasar,
				X:            killX,
				Y:            killY,
			})
			s.terminateQuasar(headerEntity)
			continue
		}

		s.ctx = quasarCtx{
			entity:  headerEntity,
			quasar:  quasarComp,
			stunned: combatComp.Status[component.StatusStun].Remaining > 0,
			inRange: s.isCursorInZapRange(headerEntity, quasarComp),
			dt:      dt,
			dtSec:   dtSec,
		}
		flags, suspended := s.graph.Step(&quasarComp.Machine, &s.ctx, dt)

		// The state's outputs, written on every step; enraged blocks kinetic via
		// the combat system
		shielded := flags&quasarShielded != 0
		combatComp.IsEnraged = flags&quasarEnraged != 0
		quasarComp.IsShielded = shielded
		if shield, ok := s.world.Components.Shield.GetPtr(headerEntity); ok {
			shield.Active = shielded
		}
		// The ward requires explicit refresh (not handled by IsEnraged).
		// A shield refuses every attacker, not one per window.
		if shielded {
			combatComp.Status[component.StatusWard] = component.Status{Remaining: parameter.CombatDamageImmunityDuration}
		}

		// Shield and cursor interaction, in every state; a stunned quasar strikes nothing
		if !suspended {
			strikeContacts(s.world, headerEntity, profile.Contact[component.SpeciesQuasar], true)
		}

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

func (s *QuasarSystem) spawnQuasar(targetX, targetY int) {
	// Trust fuse-validated position, cheap verification only
	headerX, headerY := targetX, targetY
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	// O(15) wall overlap check - fuse already validated, this catches edge cases
	if s.world.Positions.HasBlockingWallInArea(
		topLeftX, topLeftY,
		parameter.QuasarWidth, parameter.QuasarHeight,
		component.WallBlockSpawn,
	) {
		// Rare: wall appeared during animation, fallback to spiral
		var found bool
		topLeftX, topLeftY, found = s.world.Positions.FindFreeAreaSpiral(
			headerX, headerY,
			parameter.QuasarWidth, parameter.QuasarHeight,
			parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY,
			component.WallBlockSpawn,
			0,
		)
		if !found {
			s.lifecycle.spawnFailures.Add(1)
			return
		}
		headerX = topLeftX + parameter.QuasarHeaderOffsetX
		headerY = topLeftY + parameter.QuasarHeaderOffsetY
	}

	// Clear area and create composite
	s.clearQuasarSpawnArea(headerX, headerY)
	s.createQuasarComposite(headerX, headerY)
	s.lifecycle.spawned.Add(1)
}

// clearQuasarSpawnArea empties the quasar footprint in both domains (D-12)
func (s *QuasarSystem) clearQuasarSpawnArea(headerX, headerY int) {
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	s.sweep.reset()
	for row := range parameter.QuasarHeight {
		for col := range parameter.QuasarWidth {
			s.sweep.collect(s.world, topLeftX+col, topLeftY+row, func(e core.Entity) bool {
				return speciesClearable(s.world, e, s.statProtected, s.statProtectedPlayer)
			})
		}
	}
	s.sweep.emit(s.world, 0)
}

// createQuasarComposite builds the 3x5 quasar entity structure
func (s *QuasarSystem) createQuasarComposite(headerX, headerY int) core.Entity {
	// Calculate top-left from header position
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	// Create phantom head (controller entity)
	headerEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: headerX, Y: headerY})

	// Phantom head is indestructible through lifecycle
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// Set quasar components
	s.world.Components.Quasar.SetComponent(headerEntity, component.QuasarComponent{
		SpeedMultiplier: 1.0,
	})

	// Add ShieldComponent (inactive by default), uses pre-calculated config
	cfg := visual.ShieldConfigFor(component.ShieldTypeQuasar, s.world.Resources.Config.CellAspect)
	s.world.Components.Shield.SetComponent(headerEntity, component.ShieldComponent{
		Active:  false,
		Type:    component.ShieldTypeQuasar,
		RadiusX: cfg.RadiusX,
		RadiusY: cfg.RadiusY,
		InvRxSq: cfg.InvRxSq,
		InvRySq: cfg.InvRySq,
	})

	// Set combat component
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntityQuasar,
		HitPoints:        parameter.CombatInitialHPQuasar,
	})

	// Set kinetic component with centered position
	preciseX, preciseY := vmath.Point{X: headerX, Y: headerY}.Center()
	kinetic := physics.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
	}
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{Kinetic: kinetic})

	// Navigation component for flow field guidance around obstacles
	s.world.Components.Navigation.SetComponent(headerEntity, component.NavigationComponent{
		Width:         parameter.QuasarWidth,
		Height:        parameter.QuasarHeight,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
	})

	// Build member entities
	members := make([]component.MemberEntry, 0, parameter.QuasarWidth*parameter.QuasarHeight)

	for row := range parameter.QuasarHeight {
		for col := range parameter.QuasarWidth {
			memberX := topLeftX + col
			memberY := topLeftY + row

			// Calculate offset from header
			offsetX := col - parameter.QuasarHeaderOffsetX
			offsetY := row - parameter.QuasarHeaderOffsetY

			entity := s.world.CreateEntity(core.DomainShared)
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: memberX, Y: memberY})

			// MemberEntries are not from death, composite system manages lifecycle
			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromParticle | component.ProtectFromSpecies,
			})

			// Backlink to header
			s.world.Components.Member.SetComponent(entity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})

			members = append(members, component.MemberEntry{
				Entity:  entity,
				OffsetX: offsetX,
				OffsetY: offsetY,
			})
		}
	}

	// Set composite header on phantom head
	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorQuasar,
		Type:          component.CompositeTypeUnit,
		MemberEntries: members,
	})

	// The machine starts homing once every component it may read is in place
	quasarComp, _ := s.world.Components.Quasar.GetPtr(headerEntity)
	s.ctx = quasarCtx{entity: headerEntity, quasar: quasarComp}
	s.graph.Start(&quasarComp.Machine, &s.ctx, component.QuasarHoming, 0)

	// Announce the fully initialized species instance.
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity:      headerEntity,
		Species:     component.SpeciesQuasar,
		X:           headerX,
		Y:           headerY,
		MemberCount: len(members),
	})

	return headerEntity
}

// enterCharging telegraphs the zap with a splash that counts down the entry
func (s *QuasarSystem) enterCharging(c *quasarCtx) {
	s.world.PushLocal(event.EventSplashTimerRequest, &event.SplashTimerRequestPayload{
		AnchorEntity: c.entity,
		Color:        visual.RgbSplashCyan,
		MarginRight:  parameter.QuasarHeaderOffsetX + 1, // Accounting for anchor column
		MarginLeft:   parameter.QuasarHeaderOffsetX,
		MarginTop:    parameter.QuasarHeaderOffsetY,
		MarginBottom: parameter.QuasarHeaderOffsetY + 1, // Accounting for anchor row
		Duration:     c.quasar.Machine.Duration,
	})
}

// exitCharging ends the splash however the charge ends, so it never outlives it
func (s *QuasarSystem) exitCharging(c *quasarCtx) {
	s.world.PushLocal(event.EventSplashTimerCancel, &event.SplashTimerCancelPayload{
		AnchorEntity: c.entity,
	})
}

// home moves the quasar toward the cursor: the homing and charging tick
func (s *QuasarSystem) home(c *quasarCtx) {
	config := s.world.Resources.Config
	headerEntity, quasarComp, dtSec := c.entity, c.quasar, c.dtSec

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}

	kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity)
	if !ok {
		return
	}

	// Periodic speed scaling with cap (game logic, not physics), over movement time
	// only; the remainder carries into the next interval
	speedIncreaseInterval := time.Duration(parameter.QuasarSpeedIncreaseTicks) * parameter.GameUpdateInterval
	quasarComp.SpeedElapsed += c.dt
	for quasarComp.SpeedElapsed >= speedIncreaseInterval {
		quasarComp.SpeedElapsed -= speedIncreaseInterval
		quasarComp.SpeedMultiplier = min(
			quasarComp.SpeedMultiplier*(1.0+parameter.QuasarSpeedIncreasePercent),
			parameter.QuasarSpeedMultiplierMax)
	}

	// Group-based target resolution + navigation routing
	// (direct path vs flow field vs stuck fallback)
	targetX, targetY, usingDirectPath := ResolveMovementTarget(s.world, headerEntity, kineticComp)

	// Cornering drag
	turnSeverity := physics.TurnSeverity(&kineticComp.Kinetic, targetX, targetY,
		parameter.NavCorneringBase, 1.0)

	// Homing with arrival steering
	// Disable homing drag when navigating via flow field - cornering drag handles turns
	settled := physics.ApplyHomingScaled(
		&kineticComp.Kinetic,
		targetX, targetY,
		&profile.QuasarHoming,
		quasarComp.SpeedMultiplier,
		dtSec,
		usingDirectPath, // Only apply homing drag on direct path
	)

	// Apply cornering drag
	if turnSeverity > 0 {
		physics.ApplyLinearDrag(&kineticComp.Kinetic,
			turnSeverity*parameter.NavCorneringBrake, dtSec)
	}

	if settled {
		// Snap to exact target center
		baseX, baseY, baseOK := resolveBaseTarget(s.world, headerEntity)
		if baseOK {
			kineticComp.PreciseX, kineticComp.PreciseY = vmath.Point{X: baseX, Y: baseY}.Center()
			kineticComp.VelX = 0
			kineticComp.VelY = 0
			// Sync grid position if snap crossed cell boundary
			if headerPos.X != baseX || headerPos.Y != baseY {
				s.processCollisionsAtNewPositions(headerEntity, baseX, baseY)
				s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: baseX, Y: baseY})
			}
		}
		return
	}

	// Cap velocity before integration to prevent runaway from cumulative dust hits
	kineticComp.VelX, kineticComp.VelY = physics.CapSpeed(kineticComp.VelX, kineticComp.VelY, parameter.QuasarMaxSpeed)

	// Wall query to capture the specific mask and dimensions
	wallCheck := func(topLeftX, topLeftY int) bool {
		return s.world.Positions.HasBlockingWallInArea(
			topLeftX, topLeftY,
			parameter.QuasarWidth, parameter.QuasarHeight,
			component.WallBlockKinetic,
		)
	}

	// Calculate Valid Header Bounds (Header must stay within these grid coordinates)
	// Min: OffsetX
	// Max: Width - (QuasarWidth - OffsetX)
	minHeaderX := parameter.QuasarHeaderOffsetX
	maxHeaderX := config.MapWidth - (parameter.QuasarWidth - parameter.QuasarHeaderOffsetX)
	minHeaderY := parameter.QuasarHeaderOffsetY
	maxHeaderY := config.MapHeight - (parameter.QuasarHeight - parameter.QuasarHeaderOffsetY)

	// Integrate with Bounce
	newX, newY, motion := physics.IntegrateWithBounceStats(
		&kineticComp.Kinetic,
		dtSec,
		parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY,
		minHeaderX, maxHeaderX,
		minHeaderY, maxHeaderY,
		parameter.QuasarRestitution,
		wallCheck,
	)
	s.motion.Record(motion)

	// Update header position if cell changed
	if newX != headerPos.X || newY != headerPos.Y {
		s.processCollisionsAtNewPositions(headerEntity, newX, newY)
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
	}

}

// isCursorInZapRange checks if cursor is within zap ellipse centered on quasar
func (s *QuasarSystem) isCursorInZapRange(headerEntity core.Entity, quasarComp *component.QuasarComponent) bool {
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return true // Failsafe: don't zap if can't determine
	}

	_, cursorX, cursorY, ok := ClosestCursor(s.world, headerPos.X, headerPos.Y)
	if !ok {
		return true
	}

	// Inside visual circle = in range (no zap)
	dx := float64(cursorX - headerPos.X)
	dyCirc := s.world.Resources.Config.CellAspect.Circular(float64(cursorY - headerPos.Y))
	dist := vmath.Magnitude(dx, dyCirc)
	return dist <= quasarComp.ZapRadius
}

// probeZap admits a zap only with a header to fire from and a cursor to aim at
func (s *QuasarSystem) probeZap(c *quasarCtx) bool {
	headerPos, ok := s.world.Positions.GetPosition(c.entity)
	if !ok {
		return false
	}
	_, cursorX, cursorY, ok := ClosestCursor(s.world, headerPos.X, headerPos.Y)
	if !ok {
		return false
	}
	c.originX, c.originY = headerPos.X, headerPos.Y
	c.targetX, c.targetY = cursorX, cursorY
	return true
}

// enterZapping spawns tracked lightning aimed at the cursor the probe found
func (s *QuasarSystem) enterZapping(c *quasarCtx) {
	s.world.PushLocal(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
		Owner:        c.entity,
		OriginX:      c.originX,
		OriginY:      c.originY,
		TargetX:      c.targetX,
		TargetY:      c.targetY,
		OriginEntity: c.entity,
		ColorType:    component.LightningCyan,
		Duration:     parameter.QuasarZapDuration,
		Tracked:      true,
	})
}

// exitZapping despawns the lightning, a death's included; the shield follows the
// flags
func (s *QuasarSystem) exitZapping(c *quasarCtx) {
	s.world.PushEvent(event.EventLightningDespawnRequest, &event.LightningDespawnRequestPayload{Owner: c.entity})
}

// zap retargets the lightning; a zap costs a cursor what touching the quasar does
func (s *QuasarSystem) zap(c *quasarCtx) {
	if cursor := s.updateZapTarget(c.entity); cursor != 0 {
		strikeCursor(s.world, cursor, profile.Contact[component.SpeciesQuasar])
	}
}

// updateZapTarget tracks the nearest cursor and returns it for damage attribution.
func (s *QuasarSystem) updateZapTarget(headerEntity core.Entity) core.Entity {
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return 0
	}
	cursorEntity, cursorX, cursorY, ok := ClosestCursor(s.world, headerPos.X, headerPos.Y)
	if !ok {
		return 0
	}

	s.world.PushEvent(event.EventLightningUpdateRequest, &event.LightningUpdateRequestPayload{
		Owner:   headerEntity,
		TargetX: cursorX,
		TargetY: cursorY,
	})
	return cursorEntity
}

// processCollisionsAtNewPositions destroys entities at quasar's destination.
// Walls are not exempt here: a moving quasar plows through them.
func (s *QuasarSystem) processCollisionsAtNewPositions(headerEntity core.Entity, headerX, headerY int) {
	header, ok := s.world.Components.Header.GetPtr(headerEntity)
	if !ok {
		return
	}

	// Build set of member entity IDs for exclusion
	memberSet := make(map[core.Entity]bool, len(header.MemberEntries)+1)
	memberSet[headerEntity] = true
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			memberSet[m.Entity] = true
		}
	}

	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	s.sweep.reset()
	for row := range parameter.QuasarHeight {
		for col := range parameter.QuasarWidth {
			s.sweep.collect(s.world, topLeftX+col, topLeftY+row, func(e core.Entity) bool {
				if memberSet[e] || s.world.Components.Cursor.HasEntity(e) {
					return false
				}
				if prot, ok := s.world.Components.Protection.GetPtr(e); ok &&
					prot.Mask&component.ProtectFromSpecies != 0 {
					countSpeciesProtected(e, s.statProtected, s.statProtectedPlayer)
					return false
				}
				return true
			})
		}
	}

	// Personal nuggets receive a local lifecycle notification before the player batch dies.
	for _, e := range s.sweep.player {
		if s.world.Components.Nugget.HasEntity(e) {
			s.world.PushLocal(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: e})
		}
	}
	s.sweep.emit(s.world, event.EventFlashSpawnOneRequest)
}

// terminateQuasar ends a specific quasar: its state's exit, then the composite
func (s *QuasarSystem) terminateQuasar(headerEntity core.Entity) {
	if headerEntity == 0 {
		return
	}

	quasarComp, ok := s.world.Components.Quasar.GetPtr(headerEntity)
	if !ok {
		return
	}

	// The exit ends what the state started: the splash, or the lightning, which
	// would otherwise linger after the quasar's death
	s.ctx = quasarCtx{entity: headerEntity, quasar: quasarComp}
	s.graph.Stop(&quasarComp.Machine, &s.ctx)

	// Delegate composite destruction to CompositeSystem
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}

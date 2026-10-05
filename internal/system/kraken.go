package system

import (
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// Footprint states of one map cell in KrakenSystem.grid
const (
	footNone uint8 = iota
	footCovered
	footHeld // covered, and a member already stands on it
)

type KrakenSystem struct {
	world *engine.World
	toggle
	rng vmath.FastRand

	// The footprint syncMembers last built, as a cell list and a map-sized grid
	cells        []vmath.Point
	grid         []uint8
	gridW, gridH int
	spare, idle  []int // member indexes without a cell this tick: still positioned, or not

	sweep  cellSweep
	glyphs []core.Entity
}

func NewKrakenSystem(world *engine.World) engine.System {
	s := &KrakenSystem{world: world}
	s.Init()
	return s
}

func (s *KrakenSystem) Name() string  { return "kraken" }
func (s *KrakenSystem) Priority() int { return parameter.PriorityKraken }

func (s *KrakenSystem) Init() {
	s.enabled = true
	s.cells = s.cells[:0]
	clear(s.grid)
}

func (s *KrakenSystem) EventTypes() []event.EventType {
	return []event.EventType{event.EventKrakenSpawnRequest, event.EventKrakenCancelRequest,
		event.EventGameResetRequest, event.EventMetaSystemCommandRequest}
}

func (s *KrakenSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameResetRequest:
		s.terminateAll()
		s.Init()
	case event.EventMetaSystemCommandRequest:
		if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && p.SystemName == s.Name() {
			s.enabled = p.Enabled
		}
	case event.EventKrakenCancelRequest:
		s.terminateAll()
	case event.EventKrakenSpawnRequest:
		if s.enabled {
			p, _ := ev.Payload.(*event.KrakenSpawnRequestPayload)
			if p == nil {
				p = &event.KrakenSpawnRequestPayload{}
			}
			s.spawn(p)
		}
	}
}

func (s *KrakenSystem) spawn(p *event.KrakenSpawnRequestPayload) {
	cfg := s.world.Resources.Config
	x, y := p.X, p.Y
	if x == 0 && y == 0 {
		x, y = cfg.MapWidth/2, cfg.MapHeight/2
	}
	rx, ry := int(parameter.KrakenBodyRadius*2), int(parameter.KrakenBodyRadius)
	x, y = max(rx, min(x, cfg.MapWidth-rx-1)), max(ry, min(y, cfg.MapHeight-ry-1))
	px, py := (vmath.Point{X: x, Y: y}).CenterF()
	if !s.bodyFits(px, py) {
		s.world.PushEvent(event.EventKrakenSpawnFailed, nil)
		return
	}
	e := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	s.world.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectAll ^ component.ProtectFromDeath})
	s.world.Components.Kinetic.SetComponent(e, component.KineticComponent{Kinetic: physics.Kinetic{PreciseX: px, PreciseY: py}})
	s.world.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity: e, CombatEntityType: component.CombatEntityKraken, HitPoints: parameter.KrakenInitialHP,
	})
	s.world.Components.Kraken.SetComponent(e, component.KrakenComponent{
		DirX: 1, AttackLegs: 1,
	})
	s.world.Components.Header.SetComponent(e, component.HeaderComponent{
		Behavior: component.BehaviorKraken, Type: component.CompositeTypeUnit, SkipPositionSync: true,
	})
	k, _ := s.world.Components.Kraken.GetPtr(e)
	s.seed(e)
	s.wait(k)
	s.syncMembers(e, k, px, py)
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity: e, Species: component.SpeciesKraken, X: x, Y: y, MemberCount: len(s.cells),
	})
}

func (s *KrakenSystem) Update() {
	if !s.enabled {
		return
	}
	dt := s.world.Resources.Time.DeltaTime
	seconds := min(dt.Seconds(), parameter.MaxSimulationDeltaSeconds)
	for _, e := range s.world.Components.Kraken.Entities() {
		combat, ok := s.world.Components.Combat.GetPtr(e)
		if !ok {
			continue
		}
		if combat.HitPoints <= 0 {
			pos, _ := s.world.Positions.GetPosition(e)
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity: e, Species: component.SpeciesKraken, KillerEntity: combat.LastDamagedBy, X: pos.X, Y: pos.Y,
			})
			s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{HeaderEntity: e})
			continue
		}
		k, _ := s.world.Components.Kraken.GetPtr(e)
		motion, ok := s.world.Components.Kinetic.GetPtr(e)
		if !ok {
			continue
		}
		cfg := s.world.Resources.Config
		if cfg.MapWidth < int(parameter.KrakenBodyRadius*4)+1 || cfg.MapHeight < int(parameter.KrakenBodyRadius*2)+1 {
			s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{HeaderEntity: e})
			s.world.PushEvent(event.EventKrakenSpawnFailed, nil)
			continue
		}
		k.Time += seconds
		s.seed(e)
		k.StateRemaining -= dt
		if k.StateRemaining <= 0 {
			s.chooseState(k, motion.PreciseX, motion.PreciseY)
		}
		s.animate(k, motion, seconds)
		cell := vmath.PointAtF(motion.PreciseX, motion.PreciseY)
		s.world.Positions.SetPosition(e, component.PositionComponent{X: cell.X, Y: cell.Y})
		s.syncMembers(e, k, motion.PreciseX, motion.PreciseY)
		s.interact()
	}
}

func (s *KrakenSystem) chooseState(k *component.KrakenComponent, x, y float64) {
	switch k.State {
	case component.KrakenIdle:
		action := component.KrakenAttack
		if k.ActionStreak == 0 {
			if s.rng.Intn(2) != 0 {
				action = component.KrakenSpin
			}
		} else {
			action = k.LastAction
			if k.ActionStreak >= 2 || s.rng.Intn(75+50) < 75 {
				if action == component.KrakenAttack {
					action = component.KrakenSpin
				} else {
					action = component.KrakenAttack
				}
			}
		}
		// A cursor already at the padded destination needs a leg attack.
		if action == component.KrakenSpin && !s.aimCharge(k, x, y) {
			action = component.KrakenAttack
		}
		if action == k.LastAction {
			k.ActionStreak = min(k.ActionStreak+1, 2)
		} else {
			k.LastAction, k.ActionStreak = action, 1
		}
		if action == component.KrakenAttack {
			k.State, k.StateRemaining = component.KrakenAttack, parameter.KrakenAttackDuration
			k.AttackLegs = 1 - k.AttackLegs
		} else {
			k.State, k.StateRemaining = component.KrakenSpin, parameter.KrakenSpinDuration
			k.TurnDir = float64(s.rng.Intn(2)*2 - 1)
		}
	case component.KrakenSpin:
		k.State, k.StateRemaining = component.KrakenMove, parameter.KrakenMoveDuration
	default:
		s.wait(k)
	}
}

func (s *KrakenSystem) seed(e core.Entity) {
	// A transition's draw count must not depend on another Kraken's predicted death.
	seed := vmath.DeriveSeed(s.world.Resources.Rand.DomainRoot(core.DomainShared), s.Name())
	s.rng.Reseed(vmath.Mix64(seed ^ uint64(e) ^ s.world.Resources.Game.State.GetGameTicks()*0x9E3779B97F4A7C15))
}

func (s *KrakenSystem) wait(k *component.KrakenComponent) {
	k.State = component.KrakenIdle
	k.StateRemaining = parameter.KrakenWaitMin + time.Duration(s.rng.Float64()*float64(parameter.KrakenWaitMax-parameter.KrakenWaitMin))
	k.IdleTurnRemaining = 0
}

func (s *KrakenSystem) aimCharge(k *component.KrakenComponent, x, y float64) bool {
	var targets [parameter.MaxPlayers]vmath.Point
	count := 0
	for slot := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(slot))
		if pos, ok := s.world.Positions.GetPosition(cursor); cursor != 0 && ok {
			targets[count] = vmath.Point{X: pos.X, Y: pos.Y}
			count++
		}
	}
	k.TargetX, k.TargetY = x, y
	if count == 0 {
		return false
	}
	cfg := s.world.Resources.Config
	rx, ry := parameter.KrakenBodyRadius*2, parameter.KrakenBodyRadius
	start := s.rng.Intn(count)
	for i := range count {
		tx, ty := targets[(start+i)%count].CenterF()
		tx = max(rx+0.5, min(tx, float64(cfg.MapWidth)-rx-0.5))
		ty = max(ry+0.5, min(ty, float64(cfg.MapHeight)-ry-0.5))
		// Walls are demolished on contact, so only map bounds pad the destination.
		dx, dy := tx-x, ty-y
		if math.Hypot(dx, dy*2) < 1 {
			continue
		}
		k.TargetX, k.TargetY = tx, ty
		dist := math.Hypot(dx, dy)
		k.DirX, k.DirY = dx/dist, dy/dist
		return true
	}
	return false
}

func (s *KrakenSystem) animate(k *component.KrakenComponent, motion *component.KineticComponent, dt float64) {
	x, y := motion.PreciseX, motion.PreciseY
	rot, moving := parameter.KrakenRotSpeed, 0.0
	switch k.State {
	case component.KrakenIdle:
		k.IdleTurnRemaining -= time.Duration(dt * float64(time.Second))
		if k.IdleTurnRemaining <= 0 {
			k.TurnDir = float64(s.rng.Intn(3) - 1)
			k.IdleTurnRemaining = parameter.KrakenIdleTurnMin + time.Duration(s.rng.Float64()*float64(parameter.KrakenIdleTurnMax-parameter.KrakenIdleTurnMin))
		}
		rot = k.TurnDir * parameter.KrakenIdleRotSpeed
	case component.KrakenAttack:
		rot = 0
	case component.KrakenMove:
		moving, rot = 1, k.TurnDir*parameter.KrakenSpinRotSpeed
		dx, dy := k.TargetX-x, k.TargetY-y
		if dist := math.Hypot(dx, dy*2); dist > parameter.KrakenMoveSpeed*dt {
			x += dx / dist * parameter.KrakenMoveSpeed * dt
			y += dy / dist * parameter.KrakenMoveSpeed * dt
		} else {
			x, y = k.TargetX, k.TargetY
			k.StateRemaining = 0
		}
	case component.KrakenSpin:
		rot = k.TurnDir * parameter.KrakenSpinRotSpeed
	}
	oldX, oldY := motion.PreciseX, motion.PreciseY
	s.moveBody(motion, x, y)
	if k.State == component.KrakenMove && dt > 0 && math.Hypot(motion.PreciseX-oldX, motion.PreciseY-oldY) < 1e-6 {
		k.StateRemaining = 0 // A map resize must not leave a charge stalled at the new boundary.
	}
	// Like a pylon, Kraken is a push source, never an external impulse recipient.
	motion.VelX, motion.VelY = 0, 0
	k.RotSpeed += (rot - k.RotSpeed) * min(dt*parameter.KrakenRotResponse, 1)
	k.Angle = math.Mod(k.Angle+k.RotSpeed*dt, 2*math.Pi)
	k.MoveBlend += (moving - k.MoveBlend) * min(dt*4, 1)
	if k.State == component.KrakenAttack {
		k.AttackT = max(0, math.Sin((1-k.StateRemaining.Seconds()/parameter.KrakenAttackDuration.Seconds())*math.Pi))
	} else {
		k.AttackT = max(0, k.AttackT-dt*5)
	}
}

func (s *KrakenSystem) bodyFits(x, y float64) bool {
	rx, ry := parameter.KrakenBodyRadius*2, parameter.KrakenBodyRadius
	cfg := s.world.Resources.Config
	if x-rx < 0.5 || x+rx > float64(cfg.MapWidth)-0.5 || y-ry < 0.5 || y+ry > float64(cfg.MapHeight)-0.5 {
		return false
	}
	return true
}

func (s *KrakenSystem) moveBody(motion *component.KineticComponent, x, y float64) {
	// Walls do not constrain the body; a clamp also recovers from a map shrink.
	cfg := s.world.Resources.Config
	rx, ry := parameter.KrakenBodyRadius*2, parameter.KrakenBodyRadius
	motion.PreciseX = max(rx+0.5, min(x, float64(cfg.MapWidth)-rx-0.5))
	motion.PreciseY = max(ry+0.5, min(y, float64(cfg.MapHeight)-ry-0.5))
}

// syncMembers rebuilds the footprint and stands one member on each cell. A member
// keeps a cell the footprint still covers, so the spatial grid changes only along the
// moving outline and a missile homing on a member does not see it jump between legs.
func (s *KrakenSystem) syncMembers(e core.Entity, k *component.KrakenComponent, x, y float64) {
	s.resetFootprint()
	s.addDisc(x, y, parameter.KrakenBodyRadius*2)
	k.TentacleSamples(x, y, func(cx, cy, radius, _ float64, _ bool) { s.addDisc(cx, cy, radius) })
	header, ok := s.world.Components.Header.GetPtr(e)
	if !ok {
		return
	}
	ox, oy := int(x), int(y)
	s.spare, s.idle = s.spare[:0], s.idle[:0]
	for i := range header.MemberEntries {
		member := &header.MemberEntries[i]
		if member.Entity == 0 {
			continue
		}
		pos, ok := s.world.Positions.GetPosition(member.Entity)
		if !ok {
			s.idle = append(s.idle, i)
			continue
		}
		if s.covers(pos.X, pos.Y) {
			if c := &s.grid[pos.Y*s.gridW+pos.X]; *c == footCovered {
				*c = footHeld
				member.OffsetX, member.OffsetY = pos.X-ox, pos.Y-oy
				continue
			}
		}
		s.spare = append(s.spare, i)
	}
	// Positioned spares move before idle ones join, so fewer members enter and leave the grid.
	positioned := len(s.spare)
	s.spare = append(s.spare, s.idle...)
	next := 0
	for _, cell := range s.cells {
		if s.world.Positions.HasBlockingWallAt(cell.X, cell.Y, 0) {
			s.world.PushEvent(event.EventWallDespawnRequest, &event.WallDespawnRequestPayload{
				X: cell.X, Y: cell.Y, Width: 1, Height: 1,
			})
		}
		c := &s.grid[cell.Y*s.gridW+cell.X]
		if *c == footHeld {
			continue
		}
		*c = footHeld
		for next < len(s.spare) && !s.world.Components.Member.HasEntity(header.MemberEntries[s.spare[next]].Entity) {
			next++ // destroyed outside this system; composite reaps the entry
		}
		var member *component.MemberEntry
		if next < len(s.spare) {
			member = &header.MemberEntries[s.spare[next]]
			next++
		} else {
			entity := s.world.CreateEntity(core.DomainShared)
			s.world.Components.Member.SetComponent(entity, component.MemberComponent{HeaderEntity: e})
			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromParticle | component.ProtectFromSpecies,
			})
			header.MemberEntries = append(header.MemberEntries, component.MemberEntry{Entity: entity})
			member = &header.MemberEntries[len(header.MemberEntries)-1]
		}
		member.OffsetX, member.OffsetY = cell.X-ox, cell.Y-oy
		s.world.Positions.SetPosition(member.Entity, component.PositionComponent{X: cell.X, Y: cell.Y})
	}
	// Retain spare identities to avoid reallocating ECS entities as the legs contract.
	for _, i := range s.spare[next:max(next, positioned)] {
		s.world.Positions.RemoveEntity(header.MemberEntries[i].Entity)
	}
}

// resetFootprint clears the previous footprint, or resizes the grid to the map
func (s *KrakenSystem) resetFootprint() {
	cfg := s.world.Resources.Config
	if cfg.MapWidth != s.gridW || cfg.MapHeight != s.gridH {
		s.gridW, s.gridH = cfg.MapWidth, cfg.MapHeight
		n := max(0, s.gridW*s.gridH)
		if cap(s.grid) < n {
			s.grid = make([]uint8, n)
		} else {
			s.grid = s.grid[:n]
			clear(s.grid)
		}
	} else {
		for _, c := range s.cells {
			s.grid[c.Y*s.gridW+c.X] = footNone
		}
	}
	s.cells = s.cells[:0]
}

func (s *KrakenSystem) covers(x, y int) bool {
	return x >= 0 && x < s.gridW && y >= 0 && y < s.gridH && s.grid[y*s.gridW+x] != footNone
}

func (s *KrakenSystem) addDisc(x, y, radius float64) {
	w, h := s.gridW, s.gridH
	// Off-map samples remain animation geometry, without hitboxes or cell sweeps.
	if x+radius < 0.5 || x-radius > float64(w)-0.5 || y+radius/2 < 0.5 || y-radius/2 > float64(h)-0.5 {
		return
	}
	for cy := max(0, int(math.Ceil(y-radius/2-0.5))); cy <= min(h-1, int(math.Floor(y+radius/2-0.5))); cy++ {
		row := s.grid[cy*w : (cy+1)*w]
		for cx := max(0, int(math.Ceil(x-radius-0.5))); cx <= min(w-1, int(math.Floor(x+radius-0.5))); cx++ {
			dx, dy := float64(cx)+0.5-x, (float64(cy)+0.5-y)*2
			if dx*dx+dy*dy <= radius*radius && row[cx] == footNone {
				row[cx] = footCovered
				s.cells = append(s.cells, vmath.Point{X: cx, Y: cy})
			}
		}
	}
}

// interact clears what the footprint covers and strikes the cursors it touches
func (s *KrakenSystem) interact() {
	s.sweep.reset()
	s.glyphs = s.glyphs[:0]
	clearable := func(target core.Entity) bool {
		if !speciesClearable(s.world, target, nil, nil) {
			return false
		}
		if s.world.Components.Nugget.HasEntity(target) {
			s.world.PushLocal(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
			return true
		}
		if s.world.Components.Glyph.HasEntity(target) {
			if target.Domain() == core.DomainPlayer {
				s.glyphs = append(s.glyphs, target)
				return false
			}
			return true // Shared glyphs are gold members, regardless of character.
		}
		return false
	}
	for _, cell := range s.cells {
		s.sweep.collect(s.world, cell.X, cell.Y, clearable)
	}
	s.sweep.emit(s.world, event.EventFlashSpawnOneRequest)
	event.EmitParticleDeath(s.world.Resources.Event.Queue, component.ParticleDecay, s.glyphs...)

	damage := profile.Contact[component.SpeciesKraken]
	for slot := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(slot))
		if cursor == 0 || !s.world.SimulatesLocally(cursor) {
			continue
		}
		if pos, ok := s.world.Positions.GetPosition(cursor); ok && s.touches(cursor, pos.X, pos.Y) {
			strikeCursor(s.world, cursor, damage)
		}
	}
}

// touches reports contact as composites have it: an active shield wherever the
// footprint enters its ellipse, a bare cursor only on a covered cell.
func (s *KrakenSystem) touches(cursor core.Entity, x, y int) bool {
	shield, ok := s.world.Components.Shield.GetPtr(cursor)
	if !ok || !shield.Active {
		return s.covers(x, y)
	}
	rx, ry := int(shield.RadiusX), int(shield.RadiusY)
	for cy := y - ry; cy <= y+ry; cy++ {
		for cx := x - rx; cx <= x+rx; cx++ {
			if s.covers(cx, cy) && vmath.EllipseContainsPointF(cx, cy, x, y, shield.InvRxSq, shield.InvRySq) {
				return true
			}
		}
	}
	return false
}

func (s *KrakenSystem) terminateAll() {
	for _, e := range s.world.Components.Kraken.Entities() {
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{HeaderEntity: e})
	}
}

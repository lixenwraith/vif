package system

import (
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

type KrakenSystem struct {
	world   *engine.World
	enabled bool
	rng     vmath.FastRand
	cells   []vmath.Point
	seen    map[vmath.Point]bool
	sweep   cellSweep
	glyphs  []core.Entity
}

func NewKrakenSystem(world *engine.World) engine.System {
	s := &KrakenSystem{world: world, seen: make(map[vmath.Point]bool)}
	s.Init()
	return s
}

func (s *KrakenSystem) Name() string  { return "kraken" }
func (s *KrakenSystem) Priority() int { return parameter.PriorityKraken }

func (s *KrakenSystem) Init() {
	s.enabled = true
	s.cells = s.cells[:0]
	clear(s.seen)
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
		s.interact(e)
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
	k.RotSpeed += (rot - k.RotSpeed) * min(dt*4, 1)
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

func (s *KrakenSystem) syncMembers(e core.Entity, k *component.KrakenComponent, x, y float64) {
	s.cells = s.cells[:0]
	clear(s.seen)
	s.addDisc(x, y, parameter.KrakenBodyRadius*2)
	k.TentacleSamples(x, y, func(cx, cy, radius, _ float64, _ bool) { s.addDisc(cx, cy, radius) })
	header, ok := s.world.Components.Header.GetPtr(e)
	if !ok {
		return
	}
	for i, cell := range s.cells {
		if s.world.Positions.HasBlockingWallAt(cell.X, cell.Y, 0) {
			s.world.PushEvent(event.EventWallDespawnRequest, &event.WallDespawnRequestPayload{
				X: cell.X, Y: cell.Y, Width: 1, Height: 1,
			})
		}
		if i == len(header.MemberEntries) {
			member := s.world.CreateEntity(core.DomainShared)
			s.world.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: e})
			s.world.Components.Protection.SetComponent(member, component.ProtectionComponent{
				Mask: component.ProtectFromParticle | component.ProtectFromSpecies,
			})
			header.MemberEntries = append(header.MemberEntries, component.MemberEntry{Entity: member})
		}
		member := &header.MemberEntries[i]
		member.OffsetX, member.OffsetY = cell.X-int(x), cell.Y-int(y)
		s.world.Positions.SetPosition(member.Entity, component.PositionComponent{X: cell.X, Y: cell.Y})
	}
	// Retain spare identities to avoid reallocating ECS entities as the legs contract.
	for _, member := range header.MemberEntries[len(s.cells):] {
		s.world.Positions.RemoveEntity(member.Entity)
	}
}

func (s *KrakenSystem) addDisc(x, y, radius float64) {
	cfg := s.world.Resources.Config
	// Off-map samples remain animation geometry, without hitboxes or cell sweeps.
	if x+radius < 0.5 || x-radius > float64(cfg.MapWidth)-0.5 || y+radius/2 < 0.5 || y-radius/2 > float64(cfg.MapHeight)-0.5 {
		return
	}
	for cy := max(0, int(math.Ceil(y-radius/2-0.5))); cy <= min(cfg.MapHeight-1, int(math.Floor(y+radius/2-0.5))); cy++ {
		for cx := max(0, int(math.Ceil(x-radius-0.5))); cx <= min(cfg.MapWidth-1, int(math.Floor(x+radius-0.5))); cx++ {
			dx, dy := float64(cx)+0.5-x, (float64(cy)+0.5-y)*2
			cell := vmath.Point{X: cx, Y: cy}
			if dx*dx+dy*dy <= radius*radius && !s.seen[cell] {
				s.seen[cell] = true
				s.cells = append(s.cells, cell)
			}
		}
	}
}

func (s *KrakenSystem) interact(e core.Entity) {
	s.sweep.reset()
	s.glyphs = s.glyphs[:0]
	for _, cell := range s.cells {
		s.sweep.collect(s.world, cell.X, cell.Y, func(target core.Entity) bool {
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
		})
	}
	s.sweep.emit(s.world, event.EventFlashSpawnOneRequest)
	event.EmitParticleDeath(s.world.Resources.Event.Queue, component.ParticleDecay, s.glyphs...)
	overlaps := CheckCursorOverlaps(s.world, e)
	for i := range overlaps.Count {
		o := &overlaps.Entries[i]
		if !s.world.SimulatesLocally(o.Cursor) {
			continue
		}
		if len(o.ShieldMembers) > 0 {
			s.world.PushLocal(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{Entity: o.Cursor, Value: parameter.KrakenShieldDrain})
		} else if o.OnCursor && !o.ShieldActive {
			s.world.PushLocal(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Entity: o.Cursor, Delta: -parameter.KrakenDamageHeat})
		}
	}
}

func (s *KrakenSystem) terminateAll() {
	for _, e := range s.world.Components.Kraken.Entities() {
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{HeaderEntity: e})
	}
}

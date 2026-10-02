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
	// Only the body needs free space; tentacles may cross walls and the map edge.
	rx, ry := int(parameter.KrakenBodyRadius*2), int(parameter.KrakenBodyRadius)
	x, y, found := s.world.Positions.FindFreeAreaSpiral(x, y, rx*2+1, ry*2+1, rx, ry, component.WallBlockSpawn, 0)
	if !found {
		s.world.PushEvent(event.EventKrakenSpawnFailed, nil)
		return
	}
	x, y = x+rx, y+ry
	px, py := (vmath.Point{X: x, Y: y}).CenterF()
	e := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	s.world.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectAll ^ component.ProtectFromDeath})
	s.world.Components.Kinetic.SetComponent(e, component.KineticComponent{Kinetic: physics.Kinetic{PreciseX: px, PreciseY: py}})
	s.world.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity: e, CombatEntityType: component.CombatEntityKraken, HitPoints: parameter.KrakenInitialHP,
	})
	s.world.Components.Kraken.SetComponent(e, component.KrakenComponent{
		StateRemaining: 2 * time.Second, DirX: 1, TurnDir: 1, RotSpeed: parameter.KrakenRotSpeed,
	})
	s.world.Components.Header.SetComponent(e, component.HeaderComponent{
		Behavior: component.BehaviorKraken, Type: component.CompositeTypeUnit, SkipPositionSync: true,
	})
	k, _ := s.world.Components.Kraken.GetPtr(e)
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
		k.Time += seconds
		k.StateRemaining -= dt
		if k.StateRemaining <= 0 {
			s.chooseState(e, k, motion.PreciseX, motion.PreciseY)
		}
		s.animate(k, motion, seconds)
		cell := vmath.PointAtF(motion.PreciseX, motion.PreciseY)
		s.world.Positions.SetPosition(e, component.PositionComponent{X: cell.X, Y: cell.Y})
		s.syncMembers(e, k, motion.PreciseX, motion.PreciseY)
		s.interact(e)
	}
}

func (s *KrakenSystem) chooseState(e core.Entity, k *component.KrakenComponent, x, y float64) {
	// A transition's draw count must not depend on another Kraken's predicted death.
	seed := vmath.DeriveSeed(s.world.Resources.Rand.DomainRoot(core.DomainShared), s.Name())
	s.rng.Reseed(vmath.Mix64(seed ^ uint64(e) ^ s.world.Resources.Game.State.GetGameTicks()*0x9E3779B97F4A7C15))
	r := s.rng.Float64()
	switch {
	case r < 0.30:
		k.State, k.StateRemaining = component.KrakenAttack, parameter.KrakenAttackDuration
		k.AttackLegs = s.rng.Intn(2)
	case r < 0.60:
		k.State, k.StateRemaining = component.KrakenMove, parameter.KrakenMoveDuration
		cfg := s.world.Resources.Config
		k.TargetX = float64(cfg.MapWidth) * (0.2 + s.rng.Float64()*0.6)
		k.TargetY = float64(cfg.MapHeight) * (0.2 + s.rng.Float64()*0.6)
		dx, dy := k.TargetX-x, k.TargetY-y
		if dist := math.Hypot(dx, dy); dist > 0.001 {
			dx, dy = dx/dist, dy/dist
			k.TurnDir = 1
			if k.DirX*dy-k.DirY*dx < 0 {
				k.TurnDir = -1
			}
			k.DirX, k.DirY = dx, dy
		}
	case r < 0.75:
		k.State, k.StateRemaining = component.KrakenSpin, parameter.KrakenSpinDuration
		k.TurnDir = float64(s.rng.Intn(2)*2 - 1)
	default:
		k.State = component.KrakenIdle
		k.StateRemaining = time.Duration((1 + s.rng.Float64()*2) * float64(time.Second))
	}
}

func (s *KrakenSystem) animate(k *component.KrakenComponent, motion *component.KineticComponent, dt float64) {
	x, y := motion.PreciseX, motion.PreciseY
	rot, moving := parameter.KrakenRotSpeed, 0.0
	switch k.State {
	case component.KrakenIdle:
		cfg := s.world.Resources.Config
		x += (float64(cfg.MapWidth)/2 - x) * 0.2 * dt
		y += (float64(cfg.MapHeight)/2 - y) * 0.2 * dt
	case component.KrakenAttack:
		rot = 0
	case component.KrakenMove:
		moving, rot = 1, k.TurnDir*(parameter.KrakenRotSpeed+0.5)
		dx, dy := k.TargetX-x, k.TargetY-y
		if dist := math.Hypot(dx, dy*2); dist > parameter.KrakenMoveSpeed*dt {
			x += dx / dist * parameter.KrakenMoveSpeed * dt
			y += dy / dist * parameter.KrakenMoveSpeed / 2 * dt
		} else {
			k.StateRemaining = 0
		}
	case component.KrakenSpin:
		rot = k.TurnDir * (parameter.KrakenRotSpeed + 0.6)
	}
	s.moveBody(motion, x, y)
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
	for cy := int(math.Ceil(y - ry - 0.5)); cy <= int(y+ry-0.5); cy++ {
		for cx := int(math.Ceil(x - rx - 0.5)); cx <= int(x+rx-0.5); cx++ {
			dx, dy := (float64(cx)+0.5-x)/rx, (float64(cy)+0.5-y)/ry
			if dx*dx+dy*dy <= 1 && s.world.Positions.HasBlockingWallAt(cx, cy, component.WallBlockKinetic) {
				return false
			}
		}
	}
	return true
}

func (s *KrakenSystem) moveBody(motion *component.KineticComponent, x, y float64) {
	dx, dy := x-motion.PreciseX, y-motion.PreciseY
	steps := max(1, int(math.Ceil(max(math.Abs(dx), math.Abs(dy))*2)))
	for range steps {
		if nx := motion.PreciseX + dx/float64(steps); s.bodyFits(nx, motion.PreciseY) {
			motion.PreciseX = nx
		}
		if ny := motion.PreciseY + dy/float64(steps); s.bodyFits(motion.PreciseX, ny) {
			motion.PreciseY = ny
		}
	}
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

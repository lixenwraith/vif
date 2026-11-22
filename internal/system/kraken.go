package system

import (
	"math"
	"slices"
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

type KrakenSystem struct {
	world *engine.World
	toggle
	rng vmath.FastRand

	foot   footprint // the one syncMembers last built
	sweep  cellSweep
	glyphs []core.Entity
}

func NewKrakenSystem(world *engine.World) engine.System {
	s := &KrakenSystem{world: world}
	return s
}

func (s *KrakenSystem) Name() string  { return "kraken" }
func (s *KrakenSystem) Priority() int { return parameter.PriorityKraken }

func (s *KrakenSystem) Init() {
	s.enabled = true
	s.foot.cells = s.foot.cells[:0]
	clear(s.foot.grid)
}

func (s *KrakenSystem) EventTypes() []event.EventType {
	return []event.EventType{event.EventKrakenSpawnRequest, event.EventSpeciesLandRequest,
		event.EventKrakenCancelRequest, event.EventMetaSystemCommandRequest}
}

func (s *KrakenSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
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
	case event.EventSpeciesLandRequest:
		// A land request naming the kraken is its spawn request at its defaults
		if p, ok := ev.Payload.(*event.SpeciesLandRequestPayload); ok && s.enabled && p.Species == component.SpeciesKraken {
			s.spawn(&event.KrakenSpawnRequestPayload{X: p.X, Y: p.Y})
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
	px, py := (vmath.Point{X: x, Y: y}).Center()
	if !s.bodyFits(px, py) {
		s.world.PushEvent(event.EventKrakenSpawnFailed, nil)
		return
	}
	hp := p.HP
	if hp <= 0 {
		hp = parameter.KrakenInitialHP
	}
	e := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	s.world.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectAll ^ component.ProtectFromDeath})
	s.world.Components.Kinetic.SetComponent(e, component.KineticComponent{Kinetic: physics.Kinetic{PreciseX: px, PreciseY: py}})
	s.world.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity: e, CombatEntityType: component.CombatEntityKraken, HitPoints: hp,
	})
	s.world.Components.Kraken.SetComponent(e, component.KrakenComponent{
		MaxHitPoints: hp, AttackLegs: 1,
	})
	s.world.Components.Header.SetComponent(e, component.HeaderComponent{
		Behavior: component.BehaviorKraken, Type: component.CompositeTypeUnit, SkipPositionSync: true,
	})
	k, _ := s.world.Components.Kraken.GetPtr(e)
	s.seed(e)
	s.wait(k)
	s.syncMembers(e, k, px, py)
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity: e, Species: component.SpeciesKraken, X: x, Y: y, MemberCount: len(s.foot.cells),
	})
}

func (s *KrakenSystem) Update() {
	if !s.enabled {
		return
	}
	dt := s.world.Resources.Time.DeltaTime
	seconds := min(dt.Seconds(), parameter.MaxSimulationDeltaSeconds)
	s.world.Resources.KrakenShape.Reset()
	for _, e := range s.world.Components.Kraken.Entities() {
		combat, ok := s.world.Components.Combat.GetPtr(e)
		if !ok {
			continue
		}
		k, _ := s.world.Components.Kraken.GetPtr(e)
		motion, ok := s.world.Components.Kinetic.GetPtr(e)
		if combat.HitPoints <= 0 {
			pos, _ := s.world.Positions.GetPosition(e)
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity: e, Species: component.SpeciesKraken, KillerEntity: combat.LastDamagedBy, X: pos.X, Y: pos.Y,
			})
			// Its members leave nothing: what it leaves is the pose it died in, burning away
			if ok {
				s.world.PushLocal(event.EventKrakenBurnRequest, &component.KrakenBurn{
					Entity: e, Kraken: *k, X: motion.PreciseX, Y: motion.PreciseY,
				})
			}
			s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{HeaderEntity: e})
			continue
		}
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
		cell := vmath.PointAt(motion.PreciseX, motion.PreciseY)
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
	s.rng.Reseed(vmath.MixChain(seed, uint64(e), s.world.Resources.Game.State.GetGameTicks()))
}

func (s *KrakenSystem) wait(k *component.KrakenComponent) {
	k.State = component.KrakenIdle
	k.StateRemaining = parameter.KrakenWaitMin + time.Duration(s.rng.Float64()*float64(parameter.KrakenWaitMax-parameter.KrakenWaitMin))
	k.IdleTurnRemaining = 0
}

// aimCharge locks the charge on the farthest cursor, measured as the body moves, so it
// crosses the most ground; equally far cursors are a random pick. Walls are demolished
// on contact, so only map bounds pad the destination.
func (s *KrakenSystem) aimCharge(k *component.KrakenComponent, x, y float64) bool {
	pick := s.rng.Float64() // drawn whatever the roster holds, so positions never shift the stream
	cfg := s.world.Resources.Config
	rx, ry := parameter.KrakenBodyRadius*2, parameter.KrakenBodyRadius
	var ties [parameter.MaxPlayers][2]float64
	count, farthest := 0, 1.0 // a cursor within a cell of the body is already covered
	for slot := range parameter.MaxPlayers {
		pos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Slot(uint8(slot)))
		if !ok {
			continue
		}
		tx, ty := vmath.Point{X: pos.X, Y: pos.Y}.Center()
		tx = max(rx+0.5, min(tx, float64(cfg.MapWidth)-rx-0.5))
		ty = max(ry+0.5, min(ty, float64(cfg.MapHeight)-ry-0.5))
		d := (tx-x)*(tx-x) + 4*(ty-y)*(ty-y)
		if d < farthest {
			continue
		}
		if d > farthest {
			farthest, count = d, 0
		}
		ties[count] = [2]float64{tx, ty}
		count++
	}
	if count == 0 {
		k.TargetX, k.TargetY = x, y
		return false
	}
	target := ties[int(pick*float64(count))]
	k.TargetX, k.TargetY = target[0], target[1]
	return true
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

// syncMembers rebuilds the footprint and stands one member on each cell
func (s *KrakenSystem) syncMembers(e core.Entity, k *component.KrakenComponent, x, y float64) {
	cfg := s.world.Resources.Config
	s.foot.reset(cfg.MapWidth, cfg.MapHeight)
	s.addDisk(x, y, parameter.KrakenBodyRadius*2)
	for _, p := range s.world.Resources.KrakenShape.Build(e, k, x, y) {
		s.addDisk(p.X, p.Y, p.Radius)
	}
	s.foot.stand(s.world, e, int(x), int(y))
}

func (s *KrakenSystem) addDisk(x, y, radius float64) {
	f := &s.foot
	w, h := f.gridW, f.gridH
	// Off-map samples remain animation geometry, without hitboxes or cell sweeps.
	if x+radius < vmath.CellCenter || x-radius > float64(w)-vmath.CellCenter ||
		y+radius/parameter.KrakenRowSquash < vmath.CellCenter || y-radius/parameter.KrakenRowSquash > float64(h)-vmath.CellCenter {
		return
	}
	x0, x1 := max(0, int(math.Ceil(x-radius-vmath.CellCenter))), min(w-1, int(math.Floor(x+radius-vmath.CellCenter)))
	y0, y1 := max(0, int(math.Ceil(y-radius/parameter.KrakenRowSquash-vmath.CellCenter))), min(h-1, int(math.Floor(y+radius/parameter.KrakenRowSquash-vmath.CellCenter)))
	if x0 > x1 || y0 > y1 {
		return
	}
	// Every box cell is written past the list and kept only when the disk covers it
	// first, so the loop takes no branch per cell
	cells := slices.Grow(f.cells, (x1-x0+1)*(y1-y0+1))
	n := len(cells)
	cells = cells[:cap(cells)]
	for cy := y0; cy <= y1; cy++ {
		row := f.grid[cy*w+x0 : cy*w+x1+1]
		dy := (float64(cy) + vmath.CellCenter - y) * parameter.KrakenRowSquash
		column := float64(x0) + vmath.CellCenter
		for i := range row {
			dx := column - x
			column++
			covered := footNone
			if dx*dx+dy*dy <= radius*radius {
				covered = footCovered
			}
			first := covered
			if row[i] != footNone {
				first = footNone
			}
			row[i] = max(row[i], covered)
			cells[n] = vmath.Point{X: x0 + i, Y: cy}
			n += int(first)
		}
	}
	f.cells = cells[:n]
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
	for _, cell := range s.foot.cells {
		s.sweep.collect(s.world, cell.X, cell.Y, clearable)
	}
	s.sweep.emit(s.world, component.DeathFlash)
	event.EmitParticleDeath(s.world.Resources.Event.Queue, component.ParticleDecay, s.glyphs...)

	damage := profile.Contact[component.SpeciesKraken]
	for slot := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(slot))
		if cursor == 0 || !s.world.SimulatesLocally(cursor) {
			continue
		}
		if pos, ok := s.world.Positions.GetPosition(cursor); ok && s.foot.touches(s.world, cursor, pos.X, pos.Y) {
			strikeCursor(s.world, cursor, damage)
		}
	}
}

func (s *KrakenSystem) terminateAll() {
	for _, e := range s.world.Components.Kraken.Entities() {
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{HeaderEntity: e})
	}
}

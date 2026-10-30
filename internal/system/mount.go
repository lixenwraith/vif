package system

import (
	"cmp"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// MountSystem fires the weapons Shared hosts carry. Every instance advances each
// mount identically from Shared state; a pulse it fires is Shared, any other shot
// that instance's own presentation, and a hit on a cursor counts only on the
// cursor's owner.
type MountSystem struct {
	world *engine.World

	// A shot's draw is seeded from the tick and its host, never a stream, so neither
	// store order nor a correction's replay can hand it to another shot (D-8)
	sharedRoot uint64
	rng        vmath.FastRand

	statCount    *atomic.Int64
	statFired    *atomic.Int64
	statRejects  *atomic.Int64
	statDisabled *atomic.Int64

	toggle
}

func NewMountSystem(world *engine.World) engine.System {
	s := &MountSystem{world: world}

	reg := world.Resources.Status
	s.statCount = reg.Ints.Get("mount.count")
	s.statFired = reg.Ints.Get("mount.fired")
	s.statRejects = reg.Ints.Get("mount.host_rejects")
	s.statDisabled = reg.Ints.Get("mount.disabled_rejects")

	return s
}

func (s *MountSystem) Init() {
	s.sharedRoot = vmath.DeriveSeed(s.world.Resources.Rand.DomainRoot(core.DomainShared), s.Name())
	s.statCount.Store(0)
	s.statFired.Store(0)
	s.statRejects.Store(0)
	s.statDisabled.Store(0)
	s.enabled = true
}

func (s *MountSystem) Name() string { return "mount" }

func (s *MountSystem) Priority() int { return parameter.PriorityMount }

func (s *MountSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMountRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *MountSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}
	if !s.enabled {
		s.statDisabled.Add(1)
		return
	}
	if p, ok := ev.Payload.(*event.MountRequestPayload); ok {
		s.attach(p)
	}
}

// attach puts one weapon on a host. A cursor's weapons are its loadout (D-13), and
// a Player-domain entity cannot carry Shared state.
func (s *MountSystem) attach(p *event.MountRequestPayload) {
	_, placed := s.world.Positions.GetPosition(p.Host)
	pulse := p.Pulse
	if !placed || p.Host.Domain() != core.DomainShared || s.world.Components.Cursor.HasEntity(p.Host) ||
		p.Weapon < 0 || p.Weapon >= component.WeaponCount || p.Lane < 0 || p.Lane > len(vmath.Octants) ||
		pulse.Radius < 0 || pulse.DurationMs < 0 || pulse.GrowMs < 0 || pulse.HitIntervalMs < 0 ||
		pulse.Style >= component.PulseStyleCount || pulse.Attack < 0 || pulse.Attack >= component.CombatAttackTypeCount {
		s.statRejects.Add(1)
		return
	}
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	spec := &component.WeaponSpecs[p.Weapon]
	interval := time.Duration(p.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = spec.Cooldown
	}
	reach := float64(p.Range)
	if reach <= 0 {
		reach = spec.HostedRange
	}
	width := p.Width
	if width <= 0 {
		width = parameter.RayWidth
	}
	s.world.Components.Mount.SetComponent(p.Host, component.MountComponent{
		Weapon:   p.Weapon,
		Trigger:  component.MountAuto,
		Interval: interval,
		Range:    reach,
		Muzzle:   p.Muzzle,
		Damage:   component.CursorDamage{EnergyDrain: p.Energy, HeatDelta: -p.Heat},
		Pulse: component.PulseSpec{
			Radius: pulse.Radius, Duration: ms(pulse.DurationMs), Sustained: pulse.Sustained,
			Grow: ms(pulse.GrowMs), HitInterval: ms(pulse.HitIntervalMs), Style: pulse.Style,
			Attack: pulse.Attack, Charged: pulse.Charged,
		},
		Lane:  uint8(p.Lane),
		Width: width,
	})
	// A ray the replaced mount had in flight would have nothing left to end it, while
	// its pulse ends once the mount names none; the new weapon starts ready
	s.world.Components.Ray.RemoveEntity(p.Host, false)
	if timer, ok := s.world.Components.Timer.GetPtr(p.Host); ok {
		timer.Slots[component.TimerCooldown] = component.Countdown{}
	}
}

func (s *MountSystem) Update() {
	if !s.enabled {
		return
	}
	dt := s.world.Resources.Time.DeltaTime
	mounts := s.world.Components.Mount
	s.statCount.Store(int64(mounts.CountEntities()))

	// Mounts are independent of one another, so store order decides nothing shared
	for _, host := range mounts.Entities() {
		m, ok := mounts.GetPtr(host)
		if !ok {
			continue
		}
		pos, ok := s.world.Positions.GetPosition(host)
		if !ok {
			continue
		}
		cursor := s.aim(m, pos)
		if component.WeaponSpecs[m.Weapon].Delivery == component.DeliveryRay {
			s.cycleRay(host, cursor, m, pos, dt)
			continue
		}
		if cursor == 0 || timerRunning(s.world, host, component.TimerCooldown) || (m.Trigger == component.MountArmed && !m.Armed) ||
			s.world.Components.Pulse.HasEntity(m.Live) {
			continue
		}
		s.fire(host, cursor, m, pos)
		armTimer(s.world, host, component.TimerCooldown, m.Interval)
		s.statFired.Add(1)
	}
}

// aim tracks the nearest cursor in range, from Shared positions only (D-1, D-18);
// distance is aspect-corrected and a tie goes to the lower roster slot
func (s *MountSystem) aim(m *component.MountComponent, pos component.PositionComponent) core.Entity {
	var aimed core.Entity
	best := -1.0
	aspect := s.world.Resources.Config.CellAspect
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		cp, ok := s.world.Positions.GetPosition(cursor)
		if !ok {
			continue
		}
		distSq := vmath.CircleDistSq(float64(cp.X-pos.X), aspect.Circular(float64(cp.Y-pos.Y)))
		if m.Range > 0 && distSq > m.Range*m.Range {
			continue
		}
		if best < 0 || distSq < best {
			best, aimed = distSq, cursor
			m.AimX, m.AimY = cp.X, cp.Y
		}
	}
	m.HasAim = aimed != 0
	return aimed
}

// muzzle is the sub-cell point a shot leaves from: the host's centre, pushed Muzzle
// cells toward the aim (half that vertically)
func muzzle(m *component.MountComponent, pos component.PositionComponent) (float64, float64) {
	cx, cy := vmath.Point{X: pos.X, Y: pos.Y}.Center()
	dx, dy := float64(m.AimX-pos.X), float64(m.AimY-pos.Y)
	dist := vmath.Magnitude(dx, dy)
	if m.Muzzle == 0 || dist < 1 {
		return cx, cy
	}
	return cx + m.Muzzle*dx/dist, cy + m.Muzzle/2*dy/dist
}

// fire discharges one mount at the cursor it aims at; every request is local
// presentation or a hit the cursor's owner alone applies, and a pulse, a Shared
// entity riding the host, lands its hits the same way
func (s *MountSystem) fire(host, cursor core.Entity, m *component.MountComponent, pos component.PositionComponent) {
	spec := &component.WeaponSpecs[m.Weapon]
	damage := mountDamage(m)
	originX, originY := muzzle(m, pos)
	origin := vmath.PointAt(originX, originY)

	switch spec.Delivery {
	case component.DeliveryLightning:
		strikeCursor(s.world, cursor, damage)
		s.world.PushLocal(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
			Owner:        host,
			OriginX:      origin.X,
			OriginY:      origin.Y,
			TargetX:      m.AimX,
			TargetY:      m.AimY,
			TargetEntity: cursor,
			ColorType:    component.LightningRed,
			Duration:     parameter.LightningZapDuration,
		})

	case component.DeliveryMissile:
		s.world.PushLocal(event.EventMissileSpawnRequest, &event.MissileSpawnRequestPayload{
			OwnerEntity: host,
			OriginX:     origin.X,
			OriginY:     origin.Y,
			Count:       1,
			Targets:     []core.Entity{cursor},
			HitEntities: []core.Entity{cursor},
			Hostile:     true,
			Damage:      damage,
		})

	case component.DeliveryBullet:
		dx, dy := float64(m.AimX-pos.X), float64(m.AimY-pos.Y)
		dist := vmath.Magnitude(dx, dy)
		if dist < 1 {
			return
		}
		spread := (s.shotRand(host).Float64() - 0.5) * 2 * parameter.TurretSpreadHalfAngle
		dirX, dirY := vmath.RotateVector(dx/dist, dy/dist, spread)
		s.world.PushLocal(event.EventBulletSpawnRequest, &event.BulletSpawnRequestPayload{
			OriginX:     originX,
			OriginY:     originY,
			VelX:        dirX * parameter.TurretBulletSpeed,
			VelY:        dirY * parameter.TurretBulletSpeed,
			Owner:       host,
			MaxLifetime: parameter.TurretBulletLifetime,
			Hostile:     true,
			Damage:      damage,
		})

	case component.DeliveryPulse:
		m.Live = s.world.CreateEntity(core.DomainShared)
		s.world.Components.Pulse.SetComponent(m.Live, component.PulseComponent{
			PulseSpec: mountPulse(m), X: pos.X, Y: pos.Y, Anchor: host,
			Hostile: true, Damage: damage, Palette: component.PaletteHostile,
		})
	}
}

// mountDamage is what a mount's hit costs a cursor, each zero field the kind's
func mountDamage(m *component.MountComponent) component.CursorDamage {
	kind := component.WeaponSpecs[m.Weapon].HostedDamage
	return component.CursorDamage{
		EnergyDrain: cmp.Or(m.Damage.EnergyDrain, kind.EnergyDrain),
		HeatDelta:   cmp.Or(m.Damage.HeatDelta, kind.HeatDelta),
	}
}

// mountPulse is the pulse a mount fires, each zero field the kind's
func mountPulse(m *component.MountComponent) component.PulseSpec {
	spec := &component.WeaponSpecs[m.Weapon]
	own, kind := m.Pulse, spec.Pulse
	return component.PulseSpec{
		Radius:      cmp.Or(own.Radius, kind.Radius),
		Duration:    cmp.Or(own.Duration, kind.Duration),
		Sustained:   own.Sustained || kind.Sustained,
		Grow:        cmp.Or(own.Grow, kind.Grow),
		HitInterval: cmp.Or(own.HitInterval, kind.HitInterval),
		Style:       cmp.Or(own.Style, kind.Style),
		Attack:      cmp.Or(own.Attack, kind.Attack, spec.Attack),
		Charged:     own.Charged || kind.Charged,
	}
}

// cycleRay runs a ray mount's warn, fire, rest cycle on the host's RayComponent.
// The ray is laid when the warning starts and holds until rest, so a cursor sees
// where it will strike; a laned ray cycles whether or not a cursor is in range.
func (s *MountSystem) cycleRay(host, cursor core.Entity, m *component.MountComponent, pos component.PositionComponent, dt time.Duration) {
	rays := s.world.Components.Ray
	ray, ok := rays.GetPtr(host)
	if !ok {
		if timerRunning(s.world, host, component.TimerCooldown) || (m.Trigger == component.MountArmed && !m.Armed) {
			return
		}
		lane, ok := layLane(s.world, m, pos, cursor)
		if !ok {
			return
		}
		rays.SetComponent(host, component.RayComponent{
			Ray: lane, Phase: component.RayWarning,
			Remaining: parameter.RayWarning, Duration: parameter.RayWarning,
			HitInterval: parameter.RayHitInterval, Scale: 1, Palette: component.PaletteHostile,
		})
		s.statFired.Add(1)
		return
	}

	ray.Remaining -= dt
	switch {
	case ray.Phase == component.RayWarning && ray.Remaining <= 0:
		ray.Phase, ray.Remaining, ray.Duration, ray.HitTimer =
			component.RayFiring, parameter.RayFiring, parameter.RayFiring, 0
	case ray.Phase == component.RayWarning:
		return
	case ray.Remaining <= 0:
		rays.RemoveEntity(host, false)
		armTimer(s.world, host, component.TimerCooldown, m.Interval)
		return
	}
	if ray.HitTimer -= dt; ray.HitTimer <= 0 {
		strikeCursorsIn(s.world, ray.Ray.Contains, mountDamage(m))
		ray.HitTimer = ray.HitInterval
	}
}

// layLane lays a mount's ray from its host: along its fixed lane, or straight at the
// cursor it aims at, Width cells across
func layLane(w *engine.World, m *component.MountComponent, pos component.PositionComponent, cursor core.Entity) (vmath.Ray, bool) {
	var dx, dy float64
	switch {
	case m.Lane > 0:
		dx, dy = float64(vmath.Octants[m.Lane-1][0]), float64(vmath.Octants[m.Lane-1][1])
	case cursor != 0:
		dx, dy = float64(m.AimX-pos.X), float64(m.AimY-pos.Y)
	}
	if dx == 0 && dy == 0 {
		return vmath.Ray{}, false
	}
	half := max(m.Width-1, 0) / 2
	ray := vmath.Ray{X: pos.X, Y: pos.Y, DX: dx, DY: dy, Near: half, Far: half}
	ray.Length = traceRay(w, ray)
	return ray, true
}

// shotRand seeds one host's draw for this tick
func (s *MountSystem) shotRand(host core.Entity) *vmath.FastRand {
	tick := s.world.Resources.Game.State.GetGameTicks()
	s.rng.Reseed(vmath.MixChain(s.sharedRoot, tick, uint64(host)))
	return &s.rng
}

func (s *MountSystem) SaveLocal() ([]byte, error) { return json.Marshal(s.sharedRoot) }

func (s *MountSystem) LoadLocal(data []byte) error {
	if err := json.Unmarshal(data, &s.sharedRoot); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	return nil
}

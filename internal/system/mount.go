package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// MountSystem fires the weapons Shared hosts carry. Every instance advances each
// mount identically from Shared state; what it fires is that instance's own
// presentation, and a hit on a cursor counts only on the cursor's owner.
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

	enabled bool
}

func NewMountSystem(world *engine.World) engine.System {
	s := &MountSystem{world: world}

	reg := world.Resources.Status
	s.statCount = reg.Ints.Get("mount.count")
	s.statFired = reg.Ints.Get("mount.fired")
	s.statRejects = reg.Ints.Get("mount.host_rejects")
	s.statDisabled = reg.Ints.Get("mount.disabled_rejects")

	s.Init()
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
		event.EventGameResetRequest,
	}
}

func (s *MountSystem) HandleEvent(ev event.GameEvent) {
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
	if !placed || p.Host.Domain() != core.DomainShared || s.world.Components.Cursor.HasEntity(p.Host) ||
		p.Weapon < 0 || p.Weapon >= component.WeaponCount || p.Lane < 0 || p.Lane > len(vmath.Octants) {
		s.statRejects.Add(1)
		return
	}
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
		width = parameter.BeamWidth
	}
	s.world.Components.Mount.SetComponent(p.Host, component.MountComponent{
		Weapon:   p.Weapon,
		Trigger:  component.MountAuto,
		Interval: interval,
		Range:    reach,
		Muzzle:   p.Muzzle,
		Lane:     uint8(p.Lane),
		Width:    width,
	})
	// A beam the replaced mount had in flight would have nothing left to end it
	s.world.Components.Beam.RemoveEntity(p.Host, false)
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
		m.Cooldown = max(m.Cooldown-dt, 0)
		pos, ok := s.world.Positions.GetPosition(host)
		if !ok {
			continue
		}
		cursor := s.aim(m, pos)
		if component.WeaponSpecs[m.Weapon].Delivery == component.DeliveryBeam {
			s.cycleBeam(host, cursor, m, pos, dt)
			continue
		}
		if cursor == 0 || m.Cooldown > 0 || (m.Trigger == component.MountArmed && !m.Armed) {
			continue
		}
		s.fire(host, cursor, m, pos)
		m.Cooldown = m.Interval
		s.statFired.Add(1)
	}
}

// aim tracks the nearest cursor in range, from Shared positions only (D-1, D-18);
// distance is aspect-corrected and a tie goes to the lower roster slot
func (s *MountSystem) aim(m *component.MountComponent, pos component.PositionComponent) core.Entity {
	var aimed core.Entity
	best := -1.0
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		cp, ok := s.world.Positions.GetPosition(cursor)
		if !ok {
			continue
		}
		distSq := vmath.CircleDistSqF(float64(cp.X-pos.X), vmath.ScaleToCircularF(float64(cp.Y-pos.Y)))
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
	cx, cy := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
	dx, dy := float64(m.AimX-pos.X), float64(m.AimY-pos.Y)
	dist := vmath.MagnitudeF(dx, dy)
	if m.Muzzle == 0 || dist < 1 {
		return cx, cy
	}
	return cx + m.Muzzle*dx/dist, cy + m.Muzzle/2*dy/dist
}

// fire discharges one mount at the cursor it aims at; every request is local
// presentation or a hit the cursor's owner alone applies
func (s *MountSystem) fire(host, cursor core.Entity, m *component.MountComponent, pos component.PositionComponent) {
	spec := &component.WeaponSpecs[m.Weapon]
	originX, originY := muzzle(m, pos)
	origin := vmath.PointAtF(originX, originY)

	switch spec.Delivery {
	case component.DeliveryLightning:
		strikeCursor(s.world, cursor, spec.HostedDamage)
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
			Damage:      spec.HostedDamage,
		})

	case component.DeliveryBullet:
		dx, dy := float64(m.AimX-pos.X), float64(m.AimY-pos.Y)
		dist := vmath.MagnitudeF(dx, dy)
		if dist < 1 {
			return
		}
		spread := (s.shotRand(host).Float64() - 0.5) * 2 * parameter.TurretSpreadHalfAngle
		dirX, dirY := vmath.RotateVectorF(dx/dist, dy/dist, spread)
		s.world.PushLocal(event.EventBulletSpawnRequest, &event.BulletSpawnRequestPayload{
			OriginX:     originX,
			OriginY:     originY,
			VelX:        dirX * parameter.TurretBulletSpeed,
			VelY:        dirY * parameter.TurretBulletSpeed,
			Owner:       host,
			MaxLifetime: parameter.TurretBulletLifetime,
			Hostile:     true,
			Damage:      spec.HostedDamage,
		})

	case component.DeliveryPulse:
		var ring blastArea
		ring.resetOne(pos.X, pos.Y, parameter.PulseRadiusX)
		strikeCursorsIn(s.world, ring.contains, spec.HostedDamage)
		s.world.PushLocal(event.EventPulseVisualRequest, &event.PulseVisualRequestPayload{
			X: pos.X, Y: pos.Y, Palette: component.PaletteHostile,
		})
	}
}

// cycleBeam runs a beam mount's warn, fire, rest cycle on the host's BeamComponent.
// The ray is laid when the warning starts and holds until rest, so a cursor sees
// where it will strike; a laned beam cycles whether or not a cursor is in range.
func (s *MountSystem) cycleBeam(host, cursor core.Entity, m *component.MountComponent, pos component.PositionComponent, dt time.Duration) {
	beams := s.world.Components.Beam
	beam, ok := beams.GetPtr(host)
	if !ok {
		if m.Cooldown > 0 || (m.Trigger == component.MountArmed && !m.Armed) {
			return
		}
		ray, ok := layLane(s.world, m, pos, cursor)
		if !ok {
			return
		}
		beams.SetComponent(host, component.BeamComponent{
			Ray: ray, Phase: component.BeamWarning,
			Remaining: parameter.BeamWarning, Duration: parameter.BeamWarning,
			HitInterval: parameter.BeamHitInterval, Scale: 1, Palette: component.PaletteHostile,
		})
		s.statFired.Add(1)
		return
	}

	beam.Remaining -= dt
	switch {
	case beam.Phase == component.BeamWarning && beam.Remaining <= 0:
		beam.Phase, beam.Remaining, beam.Duration, beam.HitTimer =
			component.BeamFiring, parameter.BeamFiring, parameter.BeamFiring, 0
	case beam.Phase == component.BeamWarning:
		return
	case beam.Remaining <= 0:
		beams.RemoveEntity(host, false)
		m.Cooldown = m.Interval
		return
	}
	if beam.HitTimer -= dt; beam.HitTimer <= 0 {
		strikeCursorsIn(s.world, beam.Ray.Contains, component.WeaponSpecs[m.Weapon].HostedDamage)
		beam.HitTimer = beam.HitInterval
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
	s.rng.Reseed(vmath.Mix64(s.sharedRoot ^ tick*0x9E3779B97F4A7C15 ^ uint64(host)*0xD6E8FEB86659FD93))
	return &s.rng
}

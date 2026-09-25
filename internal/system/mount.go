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
		p.Weapon < 0 || p.Weapon >= component.WeaponCount {
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
	s.world.Components.Mount.SetComponent(p.Host, component.MountComponent{
		Weapon:   p.Weapon,
		Trigger:  component.MountAuto,
		Interval: interval,
		Range:    reach,
		Muzzle:   p.Muzzle,
	})
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

	case component.DeliveryPulse:
		var ring blastArea
		ring.resetOne(pos.X, pos.Y, parameter.PulseRadiusX)
		strikeCursorsInBlast(s.world, &ring, spec.HostedDamage)
		s.world.PushLocal(event.EventPulseVisualRequest, &event.PulseVisualRequestPayload{
			X: pos.X, Y: pos.Y, Palette: component.PaletteHostile,
		})
	}
}

package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// CursorSystem owns the cursor roster: lifecycle and placement.
// Sole writer of cursor positions, so input, replay, bots and the network share one path.
type CursorSystem struct {
	world *engine.World

	statCount         *atomic.Int64
	statLocal         *atomic.Int64
	statCursorRejects *atomic.Int64
	statSpawnFailures *atomic.Int64
	statSlotEntity    [parameter.MaxPlayers]*atomic.Int64
	statSlotControl   [parameter.MaxPlayers]*atomic.Int64
}

// NewCursorSystem creates the cursor system
func NewCursorSystem(world *engine.World) engine.System {
	s := &CursorSystem{world: world}

	reg := world.Resources.Status
	s.statCount = reg.Ints.Get("player.count")
	s.statLocal = reg.Ints.Get("player.local")
	s.statCursorRejects = reg.Ints.Get("player.cursor_rejects")
	s.statSpawnFailures = reg.Ints.Get("player.spawn_failures")
	for i := range parameter.MaxPlayers {
		s.statSlotEntity[i] = reg.Ints.Get(status.PlayerKey(i, "entity"))
		s.statSlotControl[i] = reg.Ints.Get(status.PlayerKey(i, "control"))
	}

	s.Init()
	return s
}

// Init resets per-session state; the roster is cleared with the world it indexes
func (s *CursorSystem) Init() {
	s.statCount.Store(0)
	s.statCursorRejects.Store(0)
	s.statSpawnFailures.Store(0)
	s.publishRoster()
}

// Name returns system's name
func (s *CursorSystem) Name() string { return "cursor" }

// Priority returns the system's priority
func (s *CursorSystem) Priority() int { return parameter.PriorityCursor }

// Update is empty: cursor lifecycle and placement are entirely event-driven
func (s *CursorSystem) Update() {}

// EventTypes returns the event types CursorSystem handles.
// EventMetaSystemCommandRequest is absent deliberately: a disabled cursor system
// would strand every cursor, so it carries no toggle.
func (s *CursorSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameResetRequest,
		event.EventCursorSpawnRequest,
		event.EventCursorDespawnRequest,
		event.EventCursorMoveRequest,
		event.EventCursorSetLocalRequest,
	}
}

// HandleEvent routes cursor lifecycle and placement requests
func (s *CursorSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
		return
	}
	switch ev.Type {
	case event.EventCursorSpawnRequest:
		if p, ok := ev.Payload.(*event.CursorSpawnRequestPayload); ok {
			s.spawn(p)
		}
	case event.EventCursorDespawnRequest:
		if p, ok := ev.Payload.(*event.CursorDespawnRequestPayload); ok {
			s.despawn(p)
		}
	case event.EventCursorMoveRequest:
		if p, ok := ev.Payload.(*event.CursorMoveRequestPayload); ok {
			s.move(p)
		}

	case event.EventCursorSetLocalRequest:
		if p, ok := ev.Payload.(*event.CursorSetLocalPayload); ok {
			s.setLocal(p.Slot)
		}
	}
}

// move applies a placement and announces it. The producer owns validation; the clamp
// here only stops a stale request from stranding a cursor off-grid.
func (s *CursorSystem) move(p *event.CursorMoveRequestPayload) {
	e := s.world.ResolveCursor(p.Entity)
	if e == 0 {
		s.statCursorRejects.Add(1)
		return
	}
	pos, ok := s.world.Positions.GetPosition(e)
	if !ok {
		return
	}

	config := s.world.Resources.Config
	x := max(0, min(p.X, config.MapWidth-1))
	y := max(0, min(p.Y, config.MapHeight-1))

	// Announce unconditionally: a same-cell request is a reconcile (resize, level setup)
	// and consumers still need it; the store write is what elides
	if x != pos.X || y != pos.Y {
		s.world.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	}
	s.world.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{Entity: e, X: x, Y: y})
}

// spawn creates one cursor in a roster slot, announcing success or refusal
func (s *CursorSystem) spawn(p *event.CursorSpawnRequestPayload) {
	roster := s.world.Resources.Player

	slot := p.Slot
	if p.Auto {
		free, ok := roster.FreeSlot()
		if !ok {
			s.fail("roster full")
			return
		}
		slot = free
	}
	if int(slot) >= parameter.MaxPlayers || roster.Slot(slot) != 0 {
		s.fail("slot unavailable")
		return
	}

	config := s.world.Resources.Config
	x, y := p.X, p.Y
	if p.Center {
		x, y = config.MapWidth/2, config.MapHeight/2
	}
	x, y, ok := s.world.ResolveFreeCell(
		max(0, min(x, config.MapWidth-1)), max(0, min(y, config.MapHeight-1)),
		component.WallBlockCursor)
	if !ok {
		s.fail("no free cell")
		return
	}

	e := s.build(slot, x, y, component.ControlKind(p.Control), p.PeerID)
	roster.Bind(slot, e)
	s.world.UpdateBoundsRadius()
	s.publishRoster()

	vlog.Info("app", "msg", "cursor spawn", "entity", uint64(e), "slot", int(slot), "x", x, "y", y)
	s.world.PushEvent(event.EventCursorSpawned, &event.CursorSpawnedPayload{Entity: e, X: x, Y: y, Slot: slot})
	s.world.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{Entity: e, X: x, Y: y})
}

// despawn destroys the cursors a request selects
func (s *CursorSystem) despawn(p *event.CursorDespawnRequestPayload) {
	switch {
	case p.All:
		for i := range parameter.MaxPlayers {
			s.destroy(uint8(i))
		}
	case p.Entity != 0:
		if c, ok := s.world.Components.Cursor.GetComponent(p.Entity); ok {
			s.destroy(c.Slot)
		}
	default:
		s.destroy(p.Slot)
	}
	s.publishRoster()
}

// destroy removes one rostered cursor. The entity carries ProtectAll, so its owning
// system destroys it directly rather than through the death path.
func (s *CursorSystem) destroy(slot uint8) {
	roster := s.world.Resources.Player
	e := roster.Slot(slot)
	if e == 0 {
		return
	}
	roster.Unbind(slot)
	s.world.DestroyEntity(e)

	vlog.Info("app", "msg", "cursor despawn", "entity", uint64(e), "slot", int(slot))
	s.world.PushEvent(event.EventCursorDespawned, &event.CursorDespawnedPayload{Entity: e, Slot: slot})
}

// build creates one cursor entity with its full component set
func (s *CursorSystem) build(slot uint8, x, y int, control component.ControlKind, peerID uint32) core.Entity {
	w := s.world
	e := w.CreateEntity()

	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	w.Components.Cursor.SetComponent(e, component.CursorComponent{Slot: slot, Control: control, PeerID: peerID})
	w.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectAll})
	w.Components.Ping.SetComponent(e, component.PingComponent{ShowCrosshair: true})
	w.Components.Heat.SetComponent(e, component.HeatComponent{})
	w.Components.Energy.SetComponent(e, component.EnergyComponent{})
	w.Components.Shield.SetComponent(e, component.ShieldComponent{
		RadiusX:       parameter.PlayerShieldRadiusX,
		RadiusY:       parameter.PlayerShieldRadiusY,
		LastDrainTime: w.Resources.Time.GameTime,
	})
	w.Components.Boost.SetComponent(e, component.BoostComponent{})
	w.Components.Weapon.SetComponent(e, component.WeaponComponent{})
	w.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity:      e,
		CombatEntityType: component.CombatEntityCursor,
		HitPoints:        100,
	})
	return e
}

// fail reports a spawn refusal so the FSM can retry
func (s *CursorSystem) fail(reason string) {
	s.statSpawnFailures.Add(1)
	vlog.Warn("app", "msg", "cursor spawn failed", "reason", reason)
	s.world.PushEvent(event.EventCursorSpawnFailed, nil)
}

// setLocal rebinds the followed slot and re-announces its position so the camera re-anchors
func (s *CursorSystem) setLocal(slot uint8) {
	roster := s.world.Resources.Player
	if int(slot) >= parameter.MaxPlayers || roster.LocalSlot() == slot {
		return
	}
	roster.SetLocal(slot)
	s.world.UpdateBoundsRadius()
	s.publishRoster()

	vlog.Info("app", "msg", "cursor local", "slot", int(slot), "entity", uint64(roster.Entity))
	s.world.PushEvent(event.EventCursorLocalChanged, &event.CursorSetLocalPayload{Slot: slot})
	if pos, ok := s.world.Positions.GetPosition(roster.Entity); ok {
		s.world.PushEvent(event.EventCursorMoved,
			&event.CursorMovedPayload{Entity: roster.Entity, X: pos.X, Y: pos.Y})
	}
}

// publishRoster mirrors slot occupancy; called on every lifecycle change
func (s *CursorSystem) publishRoster() {
	roster := s.world.Resources.Player
	for i := range parameter.MaxPlayers {
		e := roster.Slot(uint8(i))
		s.statSlotEntity[i].Store(int64(e))
		var control int64 = -1
		if c, ok := s.world.Components.Cursor.GetComponent(e); ok {
			control = int64(c.Control)
		}
		s.statSlotControl[i].Store(control)
	}
	s.statCount.Store(int64(roster.Count()))
	s.statLocal.Store(int64(roster.LocalSlot()))
}

package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
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

// Update ages every Wraith's wait on every instance; cursor lifecycle and placement
// are event-driven
func (s *CursorSystem) Update() {
	dt := s.world.Resources.Time.DeltaTime
	s.world.Components.Wraith.Each(func(_ core.Entity, w *component.WraithComponent) bool {
		w.Waited += dt
		return true
	})
}

// EventTypes returns the event types CursorSystem handles.
// EventMetaSystemCommandRequest is absent deliberately: a disabled cursor system
// would strand every cursor, so it carries no toggle.
func (s *CursorSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCursorSpawnRequest,
		event.EventCursorDespawnRequest,
		event.EventCursorWraithRequest,
		event.EventCursorMoveRequest,
		event.EventCursorSetLocalRequest,
	}
}

// HandleEvent routes cursor lifecycle and placement requests
func (s *CursorSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventCursorSpawnRequest:
		if p, ok := ev.Payload.(*event.CursorSpawnRequestPayload); ok {
			s.spawn(p)
		}
	case event.EventCursorDespawnRequest:
		if p, ok := ev.Payload.(*event.CursorDespawnRequestPayload); ok {
			s.despawn(p)
		}
	case event.EventCursorWraithRequest:
		if p, ok := ev.Payload.(*event.CursorWraithRequestPayload); ok {
			s.wraith(p.Slot, 0)
		}
	case event.EventCursorMoveRequest:
		if p, ok := ev.Payload.(*event.CursorMoveRequestPayload); ok {
			s.move(p, ev.CrossingSeq != 0)
		}

	case event.EventCursorSetLocalRequest:
		if p, ok := ev.Payload.(*event.CursorSetLocalPayload); ok {
			s.setLocal(p.Slot)
		}
	}
}

// move applies a placement and announces it. The producer owns validation; the clamp
// here only stops a stale request from stranding a cursor off-grid. Own marks a
// placement this instance's barrier applied from its own crossing.
func (s *CursorSystem) move(p *event.CursorMoveRequestPayload, own bool) {
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

	// The applied cell is the authority. It settles one D-18 prediction if this
	// instance requested it, and replaces the queue if it did not — a level setup,
	// a wall push-out or a peer's reset places the local cursor too.
	s.world.ReconcileLocalCursor(e, x, y, own)

	s.world.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{Entity: e, X: x, Y: y})
}

// spawn creates one cursor in a roster slot, announcing success or refusal
func (s *CursorSystem) spawn(p *event.CursorSpawnRequestPayload) {
	roster := s.world.Resources.Player
	roster.SetInitialResources(p.Heat, p.Energy)

	// A full reset rebuilds the session roster through the normal scripted boot
	// request. The request supplies the configuration template; the pending entries
	// supply only membership and per-instance control assignments. A Wraith is
	// rebuilt like any cursor and turns again, its wait going on from the last game's,
	// so no run of resets keeps it.
	if entries, count, local := roster.TakeRestore(); count > 0 {
		roster.SetLocal(local)
		for i := range count {
			entry := entries[i]
			q := *p
			q.Slot = entry.Slot
			q.Auto = false
			q.Control = uint8(entry.Control)
			q.PeerID, q.Holder, q.Claim = entry.PeerID, entry.Holder, entry.Claim
			s.spawnOne(&q)
			if entry.Wraith {
				s.wraith(entry.Slot, entry.Waited)
			}
		}
		return
	}
	s.spawnOne(p)
}

// spawnOne creates one cursor in a roster slot, announcing success or refusal.
func (s *CursorSystem) spawnOne(p *event.CursorSpawnRequestPayload) {
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
	if int(slot) >= parameter.MaxPlayers {
		s.fail("slot unavailable")
		return
	}
	if e := roster.Slot(slot); e != 0 {
		c, _ := s.world.Components.Cursor.GetComponent(e)
		if w, away := s.world.Components.Wraith.GetComponent(e); away && c.PeerID == p.PeerID {
			s.reclaim(e, slot, w, p)
			return
		}
		s.fail("slot unavailable")
		return
	}

	config := s.world.Resources.Config
	if config.CursorsOffMap {
		s.rosterOffMap(slot, p)
		return
	}
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

	e := s.build(slot, p)
	s.equip(e, x, y, p.Heat, p.Energy)
	roster.Bind(slot, e)
	s.world.UpdateBoundsRadius()
	s.publishRoster()

	s.world.Log().Info("app", "msg", "cursor spawned", "entity", uint64(e), "slot", int(slot), "x", x, "y", y)
	s.world.PushEvent(event.EventCursorSpawned, &event.CursorSpawnedPayload{Entity: e, X: x, Y: y, Slot: slot})
	s.world.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{Entity: e, X: x, Y: y})
}

// rosterOffMap rosters a cursor that is its slot, owner and control alone, for a map
// that places none: nothing draws, moves, targets or drains it, and a player holding
// it plays through the keys and buttons the scenario takes. It announces no cell.
func (s *CursorSystem) rosterOffMap(slot uint8, p *event.CursorSpawnRequestPayload) {
	e := s.build(slot, p)
	s.world.Resources.Player.Bind(slot, e)
	s.publishRoster()

	s.world.Log().Info("app", "msg", "cursor rostered off the map", "entity", uint64(e), "slot", int(slot))
	s.world.PushEvent(event.EventCursorSpawned, &event.CursorSpawnedPayload{Entity: e, X: -1, Y: -1, Slot: slot})
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

	s.world.Log().Info("app", "msg", "cursor despawned", "entity", uint64(e), "slot", int(slot))
	s.world.PushEvent(event.EventCursorDespawned, &event.CursorDespawnedPayload{Entity: e, Slot: slot})
}

// wraith turns one rostered cursor Wraith, having waited the given game time: it
// keeps its entity, slot and owner, and gives up everything the world reaches it
// through, the cell it stood on included, which it remembers to be drawn at and to
// come back to.
func (s *CursorSystem) wraith(slot uint8, waited time.Duration) {
	w := s.world
	e := w.Resources.Player.Slot(slot)
	if e == 0 || w.Components.Wraith.HasEntity(e) {
		return
	}
	x, y := -1, -1
	if pos, ok := w.Positions.GetPosition(e); ok {
		x, y = pos.X, pos.Y
	}
	s.strip(e)
	w.Components.Wraith.SetComponent(e, component.WraithComponent{Waited: waited, X: x, Y: y})
	s.publishRoster()

	w.Log().Info("app", "msg", "cursor turned wraith", "entity", uint64(e), "slot", int(slot), "x", x, "y", y)
	w.PushEvent(event.EventCursorDespawned, &event.CursorDespawnedPayload{Entity: e, Slot: slot})
}

// reclaim gives a Wraith back to its participant: equipped again at the free cell
// nearest the one it held, or the map's centre for one it held off the map, with
// what its player brought back of the game it left over the template.
func (s *CursorSystem) reclaim(e core.Entity, slot uint8, wraith component.WraithComponent, p *event.CursorSpawnRequestPayload) {
	w := s.world
	config := w.Resources.Config
	x, y := -1, -1
	if !config.CursorsOffMap {
		x, y = wraith.X, wraith.Y
		if x < 0 {
			x, y = config.MapWidth/2, config.MapHeight/2
		}
		var placed bool
		x, y, placed = w.ResolveFreeCell(
			max(0, min(x, config.MapWidth-1)), max(0, min(y, config.MapHeight-1)),
			component.WallBlockCursor)
		if !placed {
			s.fail("no free cell")
			return
		}
	}
	w.Components.Wraith.RemoveEntity(e)
	if c, ok := w.Components.Cursor.GetPtr(e); ok {
		c.Control = component.ControlKind(p.Control)
	}
	if x >= 0 {
		s.equip(e, x, y, p.Heat, p.Energy)
		if r := p.Record; r != nil {
			s.restore(e, r)
		}
		w.UpdateBoundsRadius()
	}
	s.publishRoster()

	w.Log().Info("app", "msg", "cursor taken back", "entity", uint64(e), "slot", int(slot), "x", x, "y", y)
	w.PushEvent(event.EventCursorSpawned, &event.CursorSpawnedPayload{Entity: e, X: x, Y: y, Slot: slot})
	if x >= 0 {
		w.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{Entity: e, X: x, Y: y})
	}
}

// restore writes what a returning player brought back of the game it left over the
// template its cursor was equipped with.
func (s *CursorSystem) restore(e core.Entity, r *event.CursorRecord) {
	c := s.world.Components
	if energy, ok := c.Energy.GetPtr(e); ok {
		energy.Current = r.Energy
	}
	if heat, ok := c.Heat.GetPtr(e); ok {
		heat.Current = r.Heat
	}
	if combat, ok := c.Combat.GetPtr(e); ok {
		combat.HitPoints = r.HitPoints
	}
	if weapon, ok := c.Weapon.GetPtr(e); ok {
		for wt := range min(component.WeaponCount, component.WeaponType(len(r.Charges))) {
			weapon.Charges[wt] = r.Charges[wt]
		}
	}
}

// build creates the entity a slot holds before anything places it: its slot, its
// owner and what drives it. An off-map cursor and a Wraith are this alone.
func (s *CursorSystem) build(slot uint8, p *event.CursorSpawnRequestPayload) core.Entity {
	w := s.world
	e := w.CreateEntity(core.DomainShared)
	w.Components.Cursor.SetComponent(e, component.CursorComponent{
		Slot: slot, Control: component.ControlKind(p.Control), PeerID: p.PeerID, Holder: p.Holder, Claim: p.Claim})
	w.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectAll})
	return e
}

// equip puts a built cursor on the map with its full playing set; strip takes the
// same set away again, so a component one adds the other removes.
func (s *CursorSystem) equip(e core.Entity, x, y, heat, energy int) {
	w := s.world
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	w.Components.CursorView.SetComponent(e, component.CursorViewComponent{})
	w.Components.Ping.SetComponent(e, component.PingComponent{ShowCrosshair: true})
	w.Components.Heat.SetComponent(e, component.HeatComponent{Current: heat})
	w.Components.Energy.SetComponent(e, component.EnergyComponent{Current: int64(energy)})
	w.Components.Shield.SetComponent(e, component.ShieldComponent{
		RadiusX: parameter.PlayerShieldRadiusX,
		RadiusY: w.Resources.Config.CellAspect.Y(parameter.PlayerShieldRadiusY),
	})
	w.Components.Boost.SetComponent(e, component.BoostComponent{})
	// A fresh cursor holds one charge of every innate kind
	var loadout component.WeaponComponent
	for wt, spec := range component.WeaponSpecs {
		if spec.Innate {
			loadout.Charges[wt] = 1
		}
	}
	w.Components.Weapon.SetComponent(e, loadout)
	w.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity:      e,
		CombatEntityType: component.CombatEntityCursor,
		HitPoints:        100,
	})
}

func (s *CursorSystem) strip(e core.Entity) {
	w := s.world
	w.Positions.RemoveEntity(e)
	w.Components.CursorView.RemoveEntity(e)
	w.Components.Ping.RemoveEntity(e)
	w.Components.Heat.RemoveEntity(e)
	w.Components.Energy.RemoveEntity(e)
	w.Components.Shield.RemoveEntity(e)
	w.Components.Boost.RemoveEntity(e)
	w.Components.Weapon.RemoveEntity(e)
	w.Components.Combat.RemoveEntity(e)
}

// fail reports a spawn refusal so the FSM can retry
func (s *CursorSystem) fail(reason string) {
	s.statSpawnFailures.Add(1)
	s.world.Log().Warn("app", "msg", "cursor spawn failed", "reason", reason)
	s.world.PushEvent(event.EventCursorSpawnFailed, nil)
}

// Rebinding emits LocalCursorChanged instead of CursorMoved so following a slot
// does not dirty shared navigation state (D-17).
func (s *CursorSystem) setLocal(slot uint8) {
	roster := s.world.Resources.Player
	if roster.LocalSlot() == slot {
		return
	}
	if int(slot) >= parameter.MaxPlayers && slot != parameter.NoPlayerSlot {
		return
	}
	prev := roster.Entity
	roster.SetLocal(slot)
	s.world.ClearBoundsRadius(prev)
	s.world.UpdateBoundsRadius()
	s.publishRoster()

	s.world.Log().Info("app", "msg", "local cursor bound", "slot", int(slot), "entity", uint64(roster.Entity))
	s.world.PushLocal(event.EventCursorLocalChanged, &event.CursorSetLocalPayload{Slot: slot})
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

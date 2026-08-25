package engine

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// World contains all entities and their components using typed stores
type World struct {
	// === Immutable after init ===
	// Pointers set once in NewWorld; the state they reference is
	// update-mutex guarded, the pointers themselves are not

	Resources  *Resource
	Components Component
	Positions  *Position

	// systems is appended only by AddSystem during single-threaded
	// construction, then frozen by Seal. UpdateLocked ranges it every tick
	// without synchronization, so runtime registration is not permitted
	systems []System
	sealed  atomic.Bool

	// === Update-mutex guarded ===
	// Entity ids, component stores, positions and masks.
	// The tick, event, input and render paths all acquire it.
	// There is no inner locking.

	// nextEntityID counts per domain; index with core.Domain. Both start at 1
	// so ID 0 is never issued and stays the "no entity" value.
	nextEntityID  [core.DomainCount]uint64
	componentMask map[core.Entity]uint64
	updateMutex   UpdateMutex

	// === Self-synchronized ===
	// Readable from any goroutine, including the post-tick telemetry tail
	// that runs after the update mutex is released

	createdCount   atomic.Int64
	destroyedCount atomic.Int64

	// origin tags events pushed while a non-simulation producer drives the world.
	// Written only under updateMutex via WithOrigin; atomic because lock-free
	// pushers read it. CI guard: WithOrigin must not appear outside a locked path.
	origin atomic.Int32

	// domain tags events pushed by a system serving both domains. Written only
	// under updateMutex via WithDomain; atomic because lock-free pushers read it.
	domain atomic.Int32
}

// NewWorld creates a new ECS world with dynamic component store support
func NewWorld() *World {
	w := &World{
		componentMask: make(map[core.Entity]uint64, 16384), // reasonable small screen size that doesn't require increase
		Resources:     &Resource{Rand: NewRandResource(0)}, // app overwrites with the run seed
		systems:       make([]System, 0),
	}
	for d := range w.nextEntityID {
		w.nextEntityID[d] = 1
	}

	initComponents(w)

	return w
}

// CreateEntity reserves a new entity ID in the given domain
// Caller holds updateMutex (all creation paths: systems, event handlers)
func (w *World) CreateEntity(d core.Domain) core.Entity {
	id := w.nextEntityID[d]
	w.nextEntityID[d]++
	// Mirror the count so telemetry never reads nextEntityID off-lock
	w.createdCount.Add(1)
	return core.MakeEntity(d, id)
}

// WithOrigin runs fn with PushEvent tagging its events as origin, restoring the
// previous tag. Caller MUST hold updateMutex: no system may run inside fn.
func (w *World) WithOrigin(o event.Origin, fn func()) {
	prev := w.origin.Swap(int32(o))
	defer w.origin.Store(prev)
	fn()
}

// WithDomain runs fn with PushEvent tagging its events as domain d, restoring the
// previous tag. Caller MUST hold updateMutex: no system may run inside fn.
func (w *World) WithDomain(d core.Domain, fn func()) {
	prev := w.domain.Swap(int32(d))
	defer w.domain.Store(prev)
	fn()
}

// Domain returns the ambient producer domain
func (w *World) Domain() core.Domain { return core.Domain(w.domain.Load()) }

// AddComponentMask marks a component bit as active for the specified entity
// Callers MUST hold updateMutex, matching removeEntity/wipeAll.
func (w *World) AddComponentMask(e core.Entity, bit uint64) {
	if domainAudit.Load() {
		auditComponentDomain(e, bit)
	}
	w.componentMask[e] |= bit
}

// TODO: for DEBUG
// GetComponentMask returns the entity signature and whether the entity is tracked
// Caller MUST hold updateMutex
func (w *World) GetComponentMask(e core.Entity) (uint64, bool) {
	bit, ok := w.componentMask[e]
	return bit, ok
}

// RemoveComponentMask clears a component bit for the specified entity
// Caller MUST hold updateMutex
func (w *World) RemoveComponentMask(e core.Entity, bit uint64) {
	w.componentMask[e] &^= bit // &^= clears unconditionally
}

// DestroyEntity removes all components associated with an entity
// Caller guarantees entity doesn't have ProtectAll
func (w *World) DestroyEntity(e core.Entity) {
	w.removeEntity(e)
	w.destroyedCount.Add(1)
}

// DestroyEntitiesBatch removes entities without protection checks
// Caller guarantees no entity has ProtectAll - use for known-safe bulk operations
func (w *World) DestroyEntitiesBatch(entities []core.Entity) {
	if len(entities) == 0 {
		return
	}
	w.removeEntitiesBatch(entities)
	w.destroyedCount.Add(int64(len(entities)))
}

// Clear removes all entities and components from the world
// Caller MUST hold updateMutex: nextEntityID is update-mutex state
// Session counters and the cursor roster reset with the entities they describe
func (w *World) Clear() {
	for d := range w.nextEntityID {
		w.nextEntityID[d] = 1
	}
	w.createdCount.Store(0)
	w.destroyedCount.Store(0)
	w.Positions.ResetTelemetry()
	w.Resources.Player.Clear()
	w.wipeAll()
}

// AddSystem adds a system to the world and sorts by priority
// Construction only; panics once Seal has frozen the set
func (w *World) AddSystem(system System) {
	if w.sealed.Load() {
		panic("engine: AddSystem after Seal")
	}

	w.systems = append(w.systems, system)

	// Sort by priority (bubble sort, small N)
	for i := range len(w.systems) - 1 {
		for j := range len(w.systems) - i - 1 {
			if w.systems[j].Priority() > w.systems[j+1].Priority() {
				w.systems[j], w.systems[j+1] = w.systems[j+1], w.systems[j]
			}
		}
	}
}

// Seal freezes the system set; called by ClockScheduler.Start before the
// scheduler and event goroutines begin ranging it
func (w *World) Seal() {
	w.sealed.Store(true)
}

// HasSystem reports whether a system with the given name is registered
// Validation source for command-mode and config system references
func (w *World) HasSystem(name string) bool {
	for _, s := range w.systems {
		if s.Name() == name {
			return true
		}
	}
	return false
}

// Systems returns a copy of all registered systems
// Used by ClockScheduler for event handler auto-registration
func (w *World) Systems() []System {
	result := make([]System, len(w.systems))
	copy(result, w.systems)
	return result
}

// RunSafe executes a function while holding the world's update lock
func (w *World) RunSafe(fn func()) {
	w.updateMutex.Lock()
	defer w.updateMutex.Unlock()
	fn()
}

// Lock acquires a lock on the world's update mutex
func (w *World) Lock() {
	w.updateMutex.Lock()
}

// TryLock attempts to acquire the update mutex without blocking
// Returns true if lock acquired, false if already held
func (w *World) TryLock() bool {
	return w.updateMutex.TryLock()
}

// Unlock releases the update mutex
func (w *World) Unlock() {
	w.updateMutex.Unlock()
}

// Update runs all systems sequentially
func (w *World) Update() {
	w.RunSafe(func() {
		w.UpdateLocked()
	})
}

// UpdateLocked runs all systems assuming the caller already holds updateMutex
func (w *World) UpdateLocked() {
	for _, system := range w.systems {
		system.Update()
	}
}

// Rand returns the labelled RNG stream for a domain in the current session.
// Single seeding entry point for systems: never seed from a clock.
func (w *World) Rand(d core.Domain, label string) *vmath.FastRand {
	return w.Resources.Rand.Stream(d, label)
}

// PushEvent emits a game event carrying the ambient producer and domain tags. HOT-PATH for all systems communication
func (w *World) PushEvent(eventType event.EventType, payload any) {
	w.pushEvent(eventType, payload, event.Origin(w.origin.Load()), core.Domain(w.domain.Load()))
}

// PushEventFull emits with explicit origin and domain tags, for replay and
// transport, which restore both from a record rather than from the ambient tags
func (w *World) PushEventFull(eventType event.EventType, payload any, origin event.Origin, domain core.Domain) {
	w.pushEvent(eventType, payload, origin, domain)
}

// PushEventOrigin emits with an explicit origin tag, for producers outside any WithOrigin scope
func (w *World) PushEventOrigin(eventType event.EventType, payload any, origin event.Origin) {
	w.pushEvent(eventType, payload, origin, core.Domain(w.domain.Load()))
}

// PushEventDomain emits with an explicit domain tag, for producers outside any WithDomain scope
func (w *World) PushEventDomain(eventType event.EventType, payload any, domain core.Domain) {
	w.pushEvent(eventType, payload, event.Origin(w.origin.Load()), domain)
}

// pushEvent is the shared emit body; trace depth is measured from here
func (w *World) pushEvent(eventType event.EventType, payload any, origin event.Origin, domain core.Domain) {
	if w.Resources.Event.Queue == nil {
		return // Not yet initialized
	}

	if vlog.On("push", vlog.LevelTrace) {
		vlog.Trace("push", vlog.LevelTrace, 4, "msg", "push", "ev", event.GetEventName(eventType))
	}

	w.Resources.Event.Queue.Push(event.GameEvent{
		Type:    eventType,
		Payload: payload,
		Origin:  origin,
		Domain:  domain,
	})
}

// CreatedCount returns total entities created this session
// Lock-free: safe from any goroutine, including the post-tick telemetry tail
func (w *World) CreatedCount() int64 {
	return w.createdCount.Load()
}

// DestroyedCount returns total entities destroyed
func (w *World) DestroyedCount() int64 {
	return w.destroyedCount.Load()
}

// === Base Entities ===

// UpdateBoundsRadius recomputes ping bounds for every cursor from mode and shield state
// Caller MUST hold updateMutex
func (w *World) UpdateBoundsRadius() {
	visual := w.Resources.Game.State.GetMode() == core.ModeVisual

	w.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
		ping, ok := w.Components.Ping.GetPtr(e)
		if !ok {
			return true
		}
		shield, hasShield := w.Components.Shield.GetComponent(e)
		if !visual || !hasShield || !shield.Active {
			ping.BoundsActive = false
			return true
		}
		ping.BoundsRadiusX = int(shield.RadiusX) / parameter.PingBoundFactor
		ping.BoundsRadiusY = int(shield.RadiusY) / parameter.PingBoundFactor
		ping.BoundsActive = true
		return true
	})
}

// GetPingAbsoluteBounds returns the local cursor's absolute bounds; zero when no cursor exists
func (w *World) GetPingAbsoluteBounds() PingAbsoluteBounds {
	return w.PingAbsoluteBoundsOf(w.Resources.Player.Entity)
}

// PingAbsoluteBoundsOf derives absolute bounds for one cursor from its position and stored radius
func (w *World) PingAbsoluteBoundsOf(e core.Entity) PingAbsoluteBounds {
	pos, ok := w.Positions.GetPosition(e)
	if !ok {
		return PingAbsoluteBounds{}
	}

	ping, ok := w.Components.Ping.GetComponent(e)
	if !ok || !ping.BoundsActive {
		return PingAbsoluteBounds{MinX: pos.X, MaxX: pos.X, MinY: pos.Y, MaxY: pos.Y}
	}

	config := w.Resources.Config
	return PingAbsoluteBounds{
		MinX:   max(0, pos.X-ping.BoundsRadiusX),
		MaxX:   min(config.MapWidth-1, pos.X+ping.BoundsRadiusX),
		MinY:   max(0, pos.Y-ping.BoundsRadiusY),
		MaxY:   min(config.MapHeight-1, pos.Y+ping.BoundsRadiusY),
		Active: true,
	}
}

// === Validation ===

// ResolveFreeCell returns the nearest cell to (x, y) that the mask does not block
// Reports false when no escape route exists within half the map
func (w *World) ResolveFreeCell(x, y int, mask component.WallBlockMask) (int, int, bool) {
	if !w.Positions.IsBlocked(x, y, mask) {
		return x, y, true
	}

	config := w.Resources.Config
	maxRadius := max(config.MapWidth, config.MapHeight) / 2

	newX, newY, found := w.Positions.FindFreeFromPattern(
		x, y, 1, 1, PatternCardinalFirst, 1, maxRadius, true, mask, nil,
	)
	if !found {
		return x, y, false
	}
	return newX, newY, true
}

// PushEntityFromBlocked relocates a non-cursor entity to the nearest valid position
// Cursor placement is CursorSystem-owned: resolve the cell, then emit a move request
func (w *World) PushEntityFromBlocked(entity core.Entity, mask component.WallBlockMask) (int, int, bool) {
	pos, ok := w.Positions.GetPosition(entity)
	if !ok {
		return 0, 0, false
	}

	newX, newY, found := w.ResolveFreeCell(pos.X, pos.Y, mask)
	if !found || (newX == pos.X && newY == pos.Y) {
		return pos.X, pos.Y, false
	}

	w.Positions.SetPosition(entity, component.PositionComponent{X: newX, Y: newY})
	return newX, newY, true
}

// ResolveCursor validates that e names a live cursor. Commands must carry an
// explicit entity; zero is never rewritten to the local cursor.
// Caller MUST hold updateMutex
func (w *World) ResolveCursor(e core.Entity) core.Entity {
	if e == 0 || !w.Components.Cursor.HasEntity(e) {
		return 0
	}
	return e
}

// CursorSlot returns the roster slot a cursor entity occupies
func (w *World) CursorSlot(e core.Entity) (uint8, bool) {
	c, ok := w.Components.Cursor.GetComponent(e)
	if !ok || int(c.Slot) >= parameter.MaxPlayers {
		return 0, false
	}
	return c.Slot, true
}

// === Level ===

// SetupLevel reconfigures map dimensions and optionally clears entities
// Respects Protection component - entities with ProtectAll survive
// Repositions cursor if outside new bounds
func (w *World) SetupLevel(width, height int, clearEntities bool, cropOnResize bool) {
	config := w.Resources.Config

	// Update map dimensions
	config.MapWidth = width
	config.MapHeight = height

	// Apply explicit crop behavior
	config.CropOnResize = cropOnResize

	// Grid tracks map dimensions; grow-only backing makes this cheap
	w.Positions.ResizeGrid(width, height)

	// Reset camera to origin
	config.CameraX = 0
	config.CameraY = 0

	if clearEntities {
		w.clearNonProtectedEntities()
	}

	// Clamp every cursor into the new bounds; CursorSystem applies and announces
	w.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
		pos, ok := w.Positions.GetPosition(e)
		if !ok {
			return true
		}
		x, y, _ := w.ResolveFreeCell(
			max(0, min(pos.X, width-1)), max(0, min(pos.Y, height-1)), component.WallBlockCursor)
		w.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{Entity: e, X: x, Y: y})
		return true
	})
}

// TODO: process through death, new mask for combat entity persistence
// clearNonProtectedEntities destroys all entities except those with ProtectAll
func (w *World) clearNonProtectedEntities() {
	// Collect entities to destroy (avoid mutation during iteration)
	var toDestroy []core.Entity

	allEntities := w.Positions.AllEntities()
	for _, e := range allEntities {
		// Check protection
		if prot, ok := w.Components.Protection.GetComponent(e); ok {
			if prot.Mask == component.ProtectAll {
				continue
			}
		}
		toDestroy = append(toDestroy, e)
	}

	w.removeEntitiesBatch(toDestroy)

	w.destroyedCount.Add(int64(len(toDestroy)))
}

// === Debug ===

// DebugPrint prints a message in status bar via meta system
func (w *World) DebugPrint(msg string) {
	w.PushEvent(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{
		Message:          msg,
		Duration:         0,
		DurationOverride: true,
	})
}

package engine

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// World contains all entities and their components using typed stores
type World struct {
	nextEntityID core.Entity
	// mu           sync.RWMutex
	mu WorldMutex

	Resources *Resource

	Components Component
	Positions  *Position

	systems []System
	// updateMutex sync.Mutex
	updateMutex UpdateMutex

	// Stats
	destroyedCount atomic.Int64
}

// NewWorld creates a new ECS world with dynamic component store support
func NewWorld() *World {
	w := &World{
		nextEntityID: 1,
		Resources:    &Resource{},
		systems:      make([]System, 0),
	}

	initComponents(w)

	return w
}

// CreateEntity reserves a new entity ID
func (w *World) CreateEntity() core.Entity {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.nextEntityID
	w.nextEntityID++
	return id
}

// DestroyEntity removes all components associated with an entity
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
func (w *World) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextEntityID = 1
	w.wipeAll()
}

// AddSystem adds a systems to the world and sorts by priority
func (w *World) AddSystem(system System) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.systems = append(w.systems, system)

	// Sort by priority (bubble sort, small N)
	for i := 0; i < len(w.systems)-1; i++ {
		for j := 0; j < len(w.systems)-i-1; j++ {
			if w.systems[j].Priority() > w.systems[j+1].Priority() {
				w.systems[j], w.systems[j+1] = w.systems[j+1], w.systems[j]
			}
		}
	}
}

// Systems returns a copy of all registered systems
// Used by ClockScheduler for event handler auto-registration
func (w *World) Systems() []System {
	w.mu.RLock()
	defer w.mu.RUnlock()
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
	w.mu.RLock()
	systems := make([]System, len(w.systems))
	copy(systems, w.systems)
	w.mu.RUnlock()

	for _, system := range systems {
		system.Update()
	}
}

// PushEvent emits a game event using direct cached pointers. HOT-PATH for all systems communication
func (w *World) PushEvent(eventType event.EventType, payload any) {
	if w.Resources.Event.Queue == nil {
		return // Not yet initialized
	}

	w.Resources.Event.Queue.Push(event.GameEvent{
		Type:    eventType,
		Payload: payload,
	})
}

// CreatedCount returns total entities ever created
func (w *World) CreatedCount() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return int64(w.nextEntityID) - 1
}

// DestroyedCount returns total entities destroyed
func (w *World) DestroyedCount() int64 {
	return w.destroyedCount.Load()
}

// === Base Entities ===

// CreateCursorEntity handles cursor entity and resource creation, and components attachment to cursor entity
func (w *World) CreateCursorEntity() {
	// 1. Create cursor entity at the center of the screen
	cursorEntity := w.CreateEntity()
	w.Positions.SetPosition(cursorEntity, component.PositionComponent{
		X: w.Resources.Config.MapWidth / 2,
		Y: w.Resources.Config.MapHeight / 2,
	})

	// 2. Setup cursor resource
	w.Resources.Player = &PlayerResource{
		Entity: cursorEntity,
	}

	// 3. Set cursor component
	w.Components.Cursor.SetComponent(cursorEntity, component.CursorComponent{})

	// 4. Set protection component, make cursor indestructible
	w.Components.Protection.SetComponent(cursorEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	// 5. Set position component
	w.Components.Ping.SetComponent(cursorEntity, component.PingComponent{
		ShowCrosshair: true,
		GridActive:    false,
		GridRemaining: 0,
	})

	// 6. Set heat component
	w.Components.Heat.SetComponent(cursorEntity, component.HeatComponent{})

	// 7. Set energy component
	w.Components.Energy.SetComponent(cursorEntity, component.EnergyComponent{})

	// 8. Set shield component
	w.Components.Shield.SetComponent(cursorEntity, component.ShieldComponent{
		RadiusX:       vmath.FromFloat(parameter.ShieldRadiusXFloat),
		RadiusY:       vmath.FromFloat(parameter.ShieldRadiusYFloat),
		MaxOpacity:    parameter.ShieldMaxOpacity,
		LastDrainTime: w.Resources.Time.GameTime,
	})

	// 9. Set boost component
	w.Components.Boost.SetComponent(cursorEntity, component.BoostComponent{})

	// 10. Set weapon component
	w.Components.Weapon.SetComponent(cursorEntity, component.WeaponComponent{
		Active:   make(map[component.WeaponType]bool),
		Cooldown: make(map[component.WeaponType]time.Duration),
	})

	// 11. Set combat component
	w.Components.Combat.SetComponent(cursorEntity, component.CombatComponent{
		OwnerEntity:      cursorEntity,
		CombatEntityType: component.CombatEntityCursor,
		HitPoints:        100,
	})
}

// UpdateBoundsRadius recomputes and stores cursor bounds from current state
func (w *World) UpdateBoundsRadius() {
	player := w.Resources.Player
	mode := w.Resources.Game.State.GetMode()

	if mode != core.ModeVisual {
		player.SetBounds(PingBounds{Active: false})
		return
	}

	shield, ok := w.Components.Shield.GetComponent(player.Entity)
	if !ok || !shield.Active {
		player.SetBounds(PingBounds{Active: false})
		return
	}

	player.SetBounds(PingBounds{
		RadiusX: vmath.ToInt(shield.RadiusX) / parameter.PingBoundFactor,
		RadiusY: vmath.ToInt(shield.RadiusY) / parameter.PingBoundFactor,
		Active:  true,
	})
}

// GetPingAbsoluteBounds returns absolute coordinates computed from cursor position and stored radius
func (w *World) GetPingAbsoluteBounds() PingAbsoluteBounds {
	bounds := w.Resources.Player.GetBounds()

	cursorPos, ok := w.Positions.GetPosition(w.Resources.Player.Entity)
	if !ok {
		return PingAbsoluteBounds{}
	}

	if !bounds.Active {
		return PingAbsoluteBounds{
			MinX: cursorPos.X, MaxX: cursorPos.X,
			MinY: cursorPos.Y, MaxY: cursorPos.Y,
			Active: false,
		}
	}

	config := w.Resources.Config

	return PingAbsoluteBounds{
		MinX:   max(0, cursorPos.X-bounds.RadiusX),
		MaxX:   min(config.MapWidth-1, cursorPos.X+bounds.RadiusX),
		MinY:   max(0, cursorPos.Y-bounds.RadiusY),
		MaxY:   min(config.MapHeight-1, cursorPos.Y+bounds.RadiusY),
		Active: true,
	}
}

// === Validation ===

// PushEntityFromBlocked relocates entity to nearest valid position if blocked
// Returns (newX, newY, moved) where moved=true if position changed
// For cursor: mask=WallBlockCursor; for kinetic entities: mask=WallBlockKinetic
func (w *World) PushEntityFromBlocked(entity core.Entity, mask component.WallBlockMask) (int, int, bool) {
	pos, ok := w.Positions.GetPosition(entity)
	if !ok {
		return 0, 0, false
	}

	if !w.Positions.IsBlocked(pos.X, pos.Y, mask) {
		return pos.X, pos.Y, false // Not blocked
	}

	config := w.Resources.Config
	maxRadius := max(config.MapWidth, config.MapHeight) / 2

	newX, newY, found := w.Positions.FindFreeFromPattern(
		pos.X, pos.Y,
		1, 1, // Single cell entity
		PatternCardinalFirst,
		1, maxRadius,
		true, // Aspect correct
		mask,
		nil, // No additional check
	)

	if !found {
		return pos.X, pos.Y, false // No escape route (catastrophic)
	}

	w.Positions.SetPosition(entity, component.PositionComponent{X: newX, Y: newY})
	return newX, newY, true
}

// === Level ===

// SetupLevel reconfigures map dimensions and optionally clears entities
// Respects Protection component - entities with ProtectAll survive
// Repositions cursor if outside new bounds
func (w *World) SetupLevel(width, height int, clearEntities bool) {
	config := w.Resources.Config

	// Update map dimensions
	config.MapWidth = width
	config.MapHeight = height

	// Set crop mode based on map vs viewport relationship
	// If map matches viewport, enable crop (normal mode)
	// If map exceeds viewport, disable crop (level mode)
	config.CropOnResize = (width <= config.ViewportWidth && height <= config.ViewportHeight)

	// Reset camera to origin
	config.CameraX = 0
	config.CameraY = 0

	if clearEntities {
		w.clearNonProtectedEntities()
	}

	// Reposition cursor if OOB
	cursorEntity := w.Resources.Player.Entity
	if pos, ok := w.Positions.GetPosition(cursorEntity); ok {
		newX := max(0, min(pos.X, width-1))
		newY := max(0, min(pos.Y, height-1))
		if newX != pos.X || newY != pos.Y {
			w.Positions.SetPosition(cursorEntity, component.PositionComponent{X: newX, Y: newY})
		}

		// Emit cursor moved to trigger camera adjustment for new level
		w.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{X: newX, Y: newY})
	}
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

	// Destroy collected entities
	for _, e := range toDestroy {
		w.removeEntity(e)
	}

	w.destroyedCount.Add(int64(len(toDestroy)))
}

// === Debug ===

// DebugPrint prints a message in status bar via meta system
func (w *World) DebugPrint(msg string) {
	w.PushEvent(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{Message: msg})
}
package engine

import (
	"reflect"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/events"
)

// ResourceStore is a thread-safe container for global game resources
// It allows systems to access shared data (Time, Config, Input) without
// coupling to the GameContext
type ResourceStore struct {
	mu        sync.RWMutex
	resources map[reflect.Type]any
}

// NewResourceStore creates a new empty resource store
func NewResourceStore() *ResourceStore {
	return &ResourceStore{
		resources: make(map[reflect.Type]any),
	}
}

// Add registers or updates a resource in the store
// T must be the pointer type of the resource struct to ensure addressability if mutation is needed,
// or the struct type if read-only behavior is desired (though pointers are recommended for consistency)
func AddResource[T any](rs *ResourceStore, resource T) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	t := reflect.TypeOf(resource)
	rs.resources[t] = resource
}

// Get retrieves a resource of type T from the store
// Returns the zero value of T and false if not found
func GetResource[T any](rs *ResourceStore) (T, bool) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// Get the type of T (we need to pass a dummy value to reflect.TypeOf if we don't have an instance)
	// However, we can use a pointer to T to get the type
	var target T
	t := reflect.TypeOf(target)

	val, ok := rs.resources[t]
	if !ok {
		return target, false
	}

	return val.(T), true
}

// MustGetResource retrieves a resource or panics if missing
// Useful for core resources (Time, Config) that must exist
func MustGetResource[T any](rs *ResourceStore) T {
	res, ok := GetResource[T](rs)
	if !ok {
		var target T
		panic("Required resource not found: " + reflect.TypeOf(target).String())
	}
	return res
}

// --- Core Resources ---

// TimeResource wraps time data for systems
// It is updated by the GameContext/ClockScheduler at the start of a frame/tick
type TimeResource struct {
	// GameTime is the current time in the game world (affected by pause)
	GameTime time.Time

	// RealTime is the wall-clock time (unaffected by pause)
	RealTime time.Time

	// DeltaTime is the duration since the last update
	DeltaTime time.Duration

	// FrameNumber is the current frame count
	FrameNumber int64
}

// ConfigResource holds static or semi-static configuration data
type ConfigResource struct {
	ScreenWidth  int
	ScreenHeight int
	GameWidth    int
	GameHeight   int
	GameX        int
	GameY        int
}

// InputResource holds the current input state and mode
// This decouples systems from raw terminal events and InputHandler internal logic
type InputResource struct {
	GameMode       int // Maps to GameMode constants (Normal, Insert, etc)
	CommandText    string
	SearchText     string
	PendingCommand string // Partial command buffer (e.g., "d2" while waiting for motion)
	IsPaused       bool
}

// Constants for mapping integer GameMode in InputResource (Avoiding circular import with engine/game.go)
const (
	ResourceModeNormal  = 0
	ResourceModeInsert  = 1
	ResourceModeSearch  = 2
	ResourceModeCommand = 3
)

// RenderConfig holds configuration for the rendering pipeline
// This decouples renderers from specific terminal implementations and allows dynamic adjustment
type RenderConfig struct {
	// Color Configuration
	ColorMode uint8 // 0=256, 1=TrueColor

	// Post-Processing: Grayout (Desaturation)
	GrayoutDuration time.Duration
	GrayoutMask     uint8

	// Post-Processing: Dim (Brightness Reduction)
	DimFactor float64
	DimMask   uint8
}

// EventQueueResource wraps the event queue for system access
type EventQueueResource struct {
	Queue *events.EventQueue
}

// AudioResource wraps the audio player interface
type AudioResource struct {
	Player AudioPlayer
}

// GameStateResource wraps GameState for read access by systems
type GameStateResource struct {
	State *GameState
}

// CursorResource holds the cursor entity reference
type CursorResource struct {
	Entity core.Entity
}

// CoreResources provides cached pointers to singleton resources
// Initialized once per system to eliminate runtime map lookups
type CoreResources struct {
	Time   *TimeResource
	Config *ConfigResource
	State  *GameStateResource
	Cursor *CursorResource
	Input  *InputResource
	Events *EventQueueResource
	Status *status.Registry
	ZIndex *ZIndexResolver
}

// GetCoreResources populates CoreResources from the world's resource store
// Call once during system construction; pointers remain valid for application lifetime
func GetCoreResources(w *World) CoreResources {
	res := CoreResources{
		Time:   MustGetResource[*TimeResource](w.Resources),
		Config: MustGetResource[*ConfigResource](w.Resources),
		State:  MustGetResource[*GameStateResource](w.Resources),
		Cursor: MustGetResource[*CursorResource](w.Resources),
		Input:  MustGetResource[*InputResource](w.Resources),
		Events: MustGetResource[*EventQueueResource](w.Resources),
		Status: MustGetResource[*status.Registry](w.Resources),
	}

	// ZIndex is optional during early bootstrap (before resolver created)
	if zRes, ok := GetResource[*ZIndexResolver](w.Resources); ok {
		res.ZIndex = zRes
	}

	return res
}

// Update modifies TimeResource fields in-place (zero allocation)
// Must be called under world lock to prevent races with system reads
func (tr *TimeResource) Update(gameTime, realTime time.Time, deltaTime time.Duration, frameNumber int64) {
	tr.GameTime = gameTime
	tr.RealTime = realTime
	tr.DeltaTime = deltaTime
	tr.FrameNumber = frameNumber
}

// Update modifies InputResource fields in-place (zero allocation)
// Must be called under world lock to prevent races with system reads
func (ir *InputResource) Update(gameMode int, commandText, searchText, pendingCommand string, isPaused bool) {
	ir.GameMode = gameMode
	ir.CommandText = commandText
	ir.SearchText = searchText
	ir.PendingCommand = pendingCommand
	ir.IsPaused = isPaused
}
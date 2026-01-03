package engine


import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/status"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// // Resource is a thread-safe container for global game resources
// // It allows system to access shared data (Time, Config, Input) without
// // coupling to the GameContext
// type Resource struct {
// 	mu        sync.RWMutex
// 	resources map[reflect.Type]any
// }
//
// // NewResourceStore creates a new empty resource store
// func NewResourceStore() *Resource {
// 	return &Resource{
// 		resources: make(map[reflect.Type]any),
// 	}
// }
//
// // Set registers or updates a resource in the store
// // T must be the pointer type of the resource struct to ensure addressability if mutation is needed,
// // or the struct type if read-only behavior is desired (though pointers are recommended for consistency)
// func SetResource[T any](rs *Resource, resource T) {
// 	rs.mu.Lock()
// 	defer rs.mu.Unlock()
// 	t := reflect.TypeOf(resource)
// 	rs.resources[t] = resource
// }
//
// // Get retrieves a resource of type T from the store
// // Returns the zero value of T and false if not found
// func GetResource[T any](rs *Resource) (T, bool) {
// 	rs.mu.RLock()
// 	defer rs.mu.RUnlock()
//
// 	// Get the type of T (we need to pass a dummy value to reflect.TypeOf if we don't have an instance)
// 	// However, we can use a pointer to T to get the type
// 	var target T
// 	t := reflect.TypeOf(target)
//
// 	val, ok := rs.resources[t]
// 	if !ok {
// 		return target, false
// 	}
//
// 	return val.(T), true
// }
//
// // MustGetResource retrieves a resource or panics if missing
// // Useful for core resources (Time, Config) that must exist
// func MustGetResource[T any](rs *Resource) T {
// 	res, ok := GetResource[T](rs)
// 	if !ok {
// 		var target T
// 		panic("Required resource not found: " + reflect.TypeOf(target).String())
// 	}
// 	return res
// }
//
// // === Cached Resource ===

// Resources holds singleton game resources, initialized during GameContext creation, accessed via World.Resources
type Resource struct {
	// World Resource
	Time      *TimeResource
	Config    *ConfigResource
	GameState *GameStateResource
	Cursor    *CursorResource
	Event     *EventQueueResource
	Render    *RenderConfig

	// Telemetry
	Status *status.Registry

	// Bridged resources from services
	Content *ContentResource
	Audio   *AudioResource
	Network *NetworkResource
}

// ServiceBridge routes a service-contributed resource to its typed field
func (r *Resource) ServiceBridge(res any) {
	switch v := res.(type) {
	case *AudioResource:
		r.Audio = v
	case *ContentResource:
		r.Content = v
	case *NetworkResource:
		r.Network = v
	}
}

// === World Resource ===

// TimeResource wraps time data for system
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

// Update modifies TimeResource fields in-place (zero allocation)
// Must be called under world lock to prevent races with system reads
func (tr *TimeResource) Update(gameTime, realTime time.Time, deltaTime time.Duration, frameNumber int64) {
	tr.GameTime = gameTime
	tr.RealTime = realTime
	tr.DeltaTime = deltaTime
	tr.FrameNumber = frameNumber
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

// RenderConfig holds configuration for the rendering pipeline
// This decouples renderers from specific terminal implementations and allows dynamic adjustment
type RenderConfig struct {
	// Color Configuration
	ColorMode terminal.ColorMode // 0=256, 1=TrueColor
}

// EventQueueResource wraps the event queue for system access
type EventQueueResource struct {
	Queue *event.EventQueue
}

// GameStateResource wraps GameState for read access by system
type GameStateResource struct {
	State *GameState
}

// CursorResource holds the cursor entity reference
type CursorResource struct {
	Entity core.Entity
}

// === Bridged Resource from Service ===

// ContentProvider defines the interface for content access
// Matches content.Service public API
type ContentProvider interface {
	CurrentContent() *core.PreparedContent
	NotifyConsumed(count int)
}

// ContentResource wraps a ContentProvider for the Resource
type ContentResource struct {
	Provider ContentProvider
}

// AudioPlayer defines the minimal audio interface used by game system
type AudioPlayer interface {
	Play(core.SoundType) bool
	ToggleMute() bool
	IsMuted() bool
	IsRunning() bool
}

// AudioResource wraps the audio player interface
type AudioResource struct {
	Player AudioPlayer
}

// NetworkProvider defines the interface for network access
type NetworkProvider interface {
	Send(peerID uint32, msgType uint8, payload []byte) bool
	Broadcast(msgType uint8, payload []byte)
	PeerCount() int
	IsRunning() bool
}

// NetworkResource wraps network provider for ECS access
type NetworkResource struct {
	Transport NetworkProvider
}
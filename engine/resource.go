package engine
// @lixen: #dev{feat[drain(render,system)]}

import (
	"reflect"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/status"
	"github.com/lixenwraith/vi-fighter/terminal"
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

// Set registers or updates a resource in the store
// T must be the pointer type of the resource struct to ensure addressability if mutation is needed,
// or the struct type if read-only behavior is desired (though pointers are recommended for consistency)
func SetResource[T any](rs *ResourceStore, resource T) {
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

// === Cached Resources ===

// Resources provides cached pointers to singleton resources
// Initialized once per system to eliminate runtime map lookups
type Resources struct {
	// World Resources
	Time   *TimeResource
	Config *ConfigResource
	State  *GameStateResource
	Cursor *CursorResource
	Events *EventQueueResource
	ZIndex *ZIndexResolver

	// Telemetry
	Status *status.Registry

	// Bridged resources from Services
	Content *ContentResource
	Audio   *AudioResource
	Network *NetworkResource
}

// GetResources populates Resources from the world's resource store
// Call once during system construction; pointers remain valid for application lifetime
func GetResources(w *World) Resources {
	res := Resources{
		Time:   MustGetResource[*TimeResource](w.Resources),
		Config: MustGetResource[*ConfigResource](w.Resources),
		Cursor: MustGetResource[*CursorResource](w.Resources),
		State:  MustGetResource[*GameStateResource](w.Resources),
		Events: MustGetResource[*EventQueueResource](w.Resources),
		Status: MustGetResource[*status.Registry](w.Resources),
		ZIndex: MustGetResource[*ZIndexResolver](w.Resources),
	}

	// Bridged resources from services: fail-fast for required, graceful for optional
	res.Content = MustGetResource[*ContentResource](w.Resources)

	if audioRes, ok := GetResource[*AudioResource](w.Resources); ok && audioRes.Player != nil {
		res.Audio = audioRes
	}

	if netRes, ok := GetResource[*NetworkResource](w.Resources); ok {
		res.Network = netRes
	}

	return res
}

// === World Resources ===

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

// GameStateResource wraps GameState for read access by systems
type GameStateResource struct {
	State *GameState
}

// CursorResource holds the cursor entity reference
type CursorResource struct {
	Entity core.Entity
}

// === Bridged Resources from Service ===

// ContentProvider defines the interface for content access
// Matches content.Service public API
type ContentProvider interface {
	CurrentContent() *core.PreparedContent
	NotifyConsumed(count int)
}

// ContentResource wraps a ContentProvider for the ResourceStore
type ContentResource struct {
	Provider ContentProvider
}

// AudioPlayer defines the minimal audio interface used by game systems
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
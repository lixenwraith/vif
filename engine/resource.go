package engine

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/navigation"
	"github.com/lixenwraith/vi-fighter/status"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Resources holds singleton game resources, initialized during GameContext creation, accessed via World.Resources
type Resource struct {
	// World Resource
	Time   *TimeResource
	Config *ConfigResource
	Game   *GameStateResource
	Player *PlayerResource
	Event  *EventQueueResource

	// Targeting
	Target *TargetResource
	// Route graphs for multi-path navigation
	RouteGraph *RouteGraphResource

	// Transient visual effects
	Transient *TransientResource

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

// === World Resources ===

// TimeResource is time data snapshot for systems and is updated by ClockScheduler at the start of a tick
type TimeResource struct {
	// GameTime is the current time in the game world (affected by pause)
	GameTime time.Time

	// RealTime is the wall-clock time (unaffected by pause)
	RealTime time.Time

	// DeltaTime is the duration since the last update
	DeltaTime time.Duration
}

// Update modifies TimeResource fields in-place, Must be called under world lock
func (tr *TimeResource) Update(gameTime, realTime time.Time, deltaTime time.Duration) {
	tr.GameTime = gameTime
	tr.RealTime = realTime
	tr.DeltaTime = deltaTime
}

// ConfigResource holds static or semi-static configuration data
type ConfigResource struct {
	// Map Dimensions (simulation bounds)
	// Defines playable area within the fixed spatial grid
	MapWidth  int `toml:"map_width"`
	MapHeight int `toml:"map_height"`

	// Viewport Dimensions (render window)
	// Terminal-derived visible area; may differ from Map
	ViewportWidth  int `toml:"viewport_width"`
	ViewportHeight int `toml:"viewport_height"`

	// Camera Position (top-left corner of viewport in map coordinates)
	// When Map > Viewport: scrollable, clamped to [0, Map - Viewport]
	// When Map <= Viewport: fixed at 0, map centered by renderer
	CameraX int `toml:"camera_x"`
	CameraY int `toml:"camera_y"`

	// CropOnResize controls terminal resize behavior
	// true: Map resizes to match Viewport, OOB entities destroyed
	// false: Map persists, Viewport/Camera updated, entities preserved
	CropOnResize bool `toml:"crop_on_resize"`

	// ColorMode for rendering pipeline (256-color vs TrueColor)
	// Set after terminal initialization
	ColorMode terminal.ColorMode `toml:"color_mode"`
}

// EventQueueResource wraps the event queue for systems access
type EventQueueResource struct {
	Queue *event.EventQueue
}

// PingBounds holds the boundaries for ping crosshair and operations
type PingBounds struct {
	RadiusX int  // Half-width from cursor (0 = single column)
	RadiusY int  // Half-height from cursor (0 = single row)
	Active  bool // True when visual mode + shield active
}

// PingAbsoluteBounds holds absolute coordinates derived from cursor position and radius
type PingAbsoluteBounds struct {
	MinX, MaxX int
	MinY, MaxY int
	Active     bool
}

// GameStateResource wraps GameState for read access by systems
type GameStateResource struct {
	State *GameState
}

// PlayerResource holds the player cursor entity and derived state
type PlayerResource struct {
	Entity core.Entity
	bounds atomic.Pointer[PingBounds]
}

// GetBounds returns current bounds snapshot (lock-free read)
func (pr *PlayerResource) GetBounds() PingBounds {
	if b := pr.bounds.Load(); b != nil {
		return *b
	}
	return PingBounds{}
}

// SetBounds atomically updates bounds
func (pr *PlayerResource) SetBounds(b PingBounds) {
	pr.bounds.Store(&b)
}

// MaxTargetsPerGroup sets the hard limit for concurrent anchors in a single target group
const MaxTargetsPerGroup = 8

// TargetData holds coordinates and entity ID for a single target instance
type TargetData struct {
	Entity core.Entity
	PosX   int
	PosY   int
}

// TargetGroupState holds resolved navigation targets for a group
type TargetGroupState struct {
	Type    component.TargetType
	Targets [MaxTargetsPerGroup]TargetData
	Count   int
	Valid   bool // False if target entity destroyed or uninitialized
}

// TargetResource provides per-group target resolution accessible by all systems
// Written by NavigationSystem, read by species systems
type TargetResource struct {
	Groups [component.MaxTargetGroups]TargetGroupState
}

// GetGroup returns the resolved target state for a group
// Group 0 is always cursor; uninitialized groups return zero-value (Valid=false)
func (tr *TargetResource) GetGroup(groupID uint8) TargetGroupState {
	if int(groupID) >= len(tr.Groups) {
		return TargetGroupState{}
	}
	return tr.Groups[groupID]
}

// SetGroup configures a target group
func (tr *TargetResource) SetGroup(groupID uint8, state TargetGroupState) {
	if int(groupID) < len(tr.Groups) {
		tr.Groups[groupID] = state
	}
}

// RouteGraphResource provides route graph storage accessible by NavigationSystem and GeneticSystem
// Written by systems that compute route graphs (future: GatewaySystem), read by NavigationSystem
// Keyed by opaque uint32 ID assigned at computation time
type RouteGraphResource struct {
	graphs map[uint32]*navigation.RouteGraph
}

// Get returns the route graph for the given ID, or nil
func (r *RouteGraphResource) Get(id uint32) *navigation.RouteGraph {
	if r == nil || r.graphs == nil {
		return nil
	}
	return r.graphs[id]
}

// Set stores a route graph under the given ID
func (r *RouteGraphResource) Set(id uint32, rg *navigation.RouteGraph) {
	if r.graphs == nil {
		r.graphs = make(map[uint32]*navigation.RouteGraph)
	}
	r.graphs[id] = rg
}

// Remove deletes a route graph by ID
func (r *RouteGraphResource) Remove(id uint32) {
	if r.graphs != nil {
		delete(r.graphs, id)
	}
}

// Clear removes all route graphs (used on regraph/reset)
func (r *RouteGraphResource) Clear() {
	if r.graphs != nil {
		clear(r.graphs)
	}
}

// === Bridged Resources from Service ===

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

// AudioPlayer defines the audio interface used by game systems
type AudioPlayer interface {
	// Global Pause
	SetPaused(paused bool)

	// Sound effects
	Play(core.SoundType) bool
	ToggleEffectMute() bool
	IsEffectMuted() bool
	IsRunning() bool

	// Music playback control
	ToggleMusicMute() bool
	IsMusicMuted() bool
	StartMusic()
	StopMusic()

	// Sequencer control
	SetMusicBPM(bpm int)
	SetMusicSwing(amount float64)
	SetMusicVolume(vol float64)
	SetBeatPattern(pattern core.PatternID, crossfadeSamples int, quantize bool)
	SetMelodyPattern(pattern core.PatternID, root int, crossfadeSamples int, quantize bool)
	TriggerMelodyNote(note int, velocity float64, durationSamples int, instr core.InstrumentType)
	ResetMusic()
	IsMusicPlaying() bool
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
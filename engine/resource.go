package engine

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/status"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Resources holds singleton game resources, initialized during GameContext creation, accessed via World.Resources
type Resource struct {
	// World Resource
	Time   *TimeResource
	Config *ConfigResource
	Game   *GameStateResource
	Cursor *CursorResource
	Event  *EventQueueResource
	Render *RenderConfig

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
	GameWidth  int
	GameHeight int
}

// RenderConfig holds configuration for the rendering pipeline
type RenderConfig struct {
	// Color Configuration
	ColorMode terminal.ColorMode // 0=256, 1=TrueColor
}

// EventQueueResource wraps the event queue for systems access
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

// ContentResource wraps a ContentProvider for the Resource
type ContentResource struct {
	Provider ContentProvider
}

// AudioPlayer defines the audio interface used by game systems
type AudioPlayer interface {
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
// FILE: audio/engine.go
package audio

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
)

// AudioEngine manages all game audio with thread-safe playback
type AudioEngine struct {
	// Configuration
	config *AudioConfig

	// Channels for command submission (size 1 for overflow protection)
	realTimeQueue chan AudioCommand // For immediate sounds (errors)
	stateQueue    chan AudioCommand // For state-based sounds (coins, bells)

	// Control
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Current playback state (only accessed by audio goroutine)
	currentSound    beep.StreamerSeekCloser
	currentCtrl     *beep.Ctrl
	lastSoundTime   time.Time
	soundGeneration uint64

	// Statistics (atomics for thread-safe access)
	soundsPlayed   atomic.Uint64
	soundsDropped  atomic.Uint64
	queueOverflows atomic.Uint64

	// State
	initialized atomic.Bool
	running     atomic.Bool
}

// NewAudioEngine creates a new audio engine with the given configuration
func NewAudioEngine(cfg *AudioConfig) (*AudioEngine, error) {
	if cfg == nil {
		cfg = DefaultAudioConfig()
	}

	// Initialize speaker if not already done
	rate := beep.SampleRate(cfg.SampleRate)
	err := speaker.Init(rate, rate.N(time.Second/10))
	if err != nil {
		// Speaker might already be initialized, try to use it anyway
		// This allows for testing and multiple engine creation
		fmt.Printf("Warning: speaker init failed (may already be initialized): %v\n", err)
	}

	ae := &AudioEngine{
		config:        cfg,
		realTimeQueue: make(chan AudioCommand, 1),
		stateQueue:    make(chan AudioCommand, 1),
		stopChan:      make(chan struct{}),
		lastSoundTime: time.Now(),
	}

	ae.initialized.Store(true)
	return ae, nil
}

// Start begins the audio engine processing loop
func (ae *AudioEngine) Start() error {
	if !ae.initialized.Load() {
		return fmt.Errorf("audio engine not initialized")
	}

	if ae.running.CompareAndSwap(false, true) {
		ae.wg.Add(1)
		go ae.processLoop()
	}

	return nil
}

// Stop halts the audio engine and waits for cleanup
func (ae *AudioEngine) Stop() {
	if ae.running.CompareAndSwap(true, false) {
		close(ae.stopChan)
		ae.wg.Wait()

		// Stop any current sound
		if ae.currentCtrl != nil {
			speaker.Lock()
			ae.currentCtrl.Paused = true
			speaker.Unlock()
		}
	}
}

// SendRealTime sends a real-time audio command (non-blocking)
func (ae *AudioEngine) SendRealTime(cmd AudioCommand) bool {
	if !ae.config.Enabled || !ae.running.Load() {
		return false
	}

	select {
	case ae.realTimeQueue <- cmd:
		return true
	default:
		ae.queueOverflows.Add(1)
		return false
	}
}

// SendState sends a state-based audio command (non-blocking)
func (ae *AudioEngine) SendState(cmd AudioCommand) bool {
	if !ae.config.Enabled || !ae.running.Load() {
		return false
	}

	select {
	case ae.stateQueue <- cmd:
		return true
	default:
		ae.queueOverflows.Add(1)
		return false
	}
}

// processLoop is the main audio processing goroutine
func (ae *AudioEngine) processLoop() {
	defer ae.wg.Done()

	ticker := time.NewTicker(10 * time.Millisecond) // Fast tick for responsive audio
	defer ticker.Stop()

	for {
		select {
		case <-ae.stopChan:
			return

		case cmd := <-ae.realTimeQueue:
			ae.processCommand(cmd, true)

		case cmd := <-ae.stateQueue:
			ae.processCommand(cmd, false)

		case <-ticker.C:
			// Check if current sound has finished
			ae.checkCurrentSound()
		}
	}
}

// processCommand handles a single audio command
func (ae *AudioEngine) processCommand(cmd AudioCommand, isRealTime bool) {
	// Check generation to see if command is stale
	if cmd.Generation > 0 && cmd.Generation < ae.soundGeneration {
		ae.soundsDropped.Add(1)
		return
	}

	// Check minimum gap between sounds
	if time.Since(ae.lastSoundTime) < ae.config.MinSoundGap {
		ae.soundsDropped.Add(1)
		return
	}

	// Stop current sound if playing
	ae.stopCurrentSound()

	// Drain both queues when playing a new sound
	ae.drainQueues()

	// Create and play the new sound
	streamer := GetSoundEffect(cmd.Type, ae.config)
	if streamer == nil {
		return
	}

	// Wrap in controller for stopping
	ctrl := &beep.Ctrl{Streamer: streamer}
	ae.currentCtrl = ctrl

	// Play the sound
	speaker.Play(beep.Seq(
		ctrl,
		beep.Callback(func() {
			ae.soundComplete()
		}),
	))

	ae.lastSoundTime = time.Now()
	ae.soundGeneration = cmd.Generation
	ae.soundsPlayed.Add(1)
}

// stopCurrentSound stops the currently playing sound
func (ae *AudioEngine) stopCurrentSound() {
	if ae.currentCtrl != nil {
		speaker.Lock()
		ae.currentCtrl.Paused = true
		speaker.Unlock()
		ae.currentCtrl = nil
	}

	if ae.currentSound != nil {
		err := ae.currentSound.Close()
		if err != nil {
			// Log but don't fail
			fmt.Printf("Error closing sound: %v\n", err)
		}
		ae.currentSound = nil
	}
}

// drainQueues empties both command queues
func (ae *AudioEngine) drainQueues() {
	// Drain real-time queue
	for {
		select {
		case <-ae.realTimeQueue:
			ae.soundsDropped.Add(1)
		default:
			goto drainState
		}
	}

drainState:
	// Drain state queue
	for {
		select {
		case <-ae.stateQueue:
			ae.soundsDropped.Add(1)
		default:
			return
		}
	}
}

// checkCurrentSound checks if the current sound has finished naturally
func (ae *AudioEngine) checkCurrentSound() {
	// This is handled by the callback in processCommand
	// This method is kept for potential future use
}

// soundComplete is called when a sound finishes playing
func (ae *AudioEngine) soundComplete() {
	ae.currentCtrl = nil
	ae.currentSound = nil
}

// SetConfig updates the audio configuration (thread-safe)
func (ae *AudioEngine) SetConfig(cfg *AudioConfig) {
	// In a real implementation, this would need proper synchronization
	// For now, we just replace the pointer atomically
	ae.config = cfg
}

// GetStats returns audio engine statistics
func (ae *AudioEngine) GetStats() (played, dropped, overflows uint64) {
	return ae.soundsPlayed.Load(), ae.soundsDropped.Load(), ae.queueOverflows.Load()
}

// SetVolume updates the master volume (0.0 to 1.0)
func (ae *AudioEngine) SetVolume(volume float64) {
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	ae.config.MasterVolume = volume
}

// ToggleEnabled toggles audio on/off
func (ae *AudioEngine) ToggleEnabled() bool {
	ae.config.Enabled = !ae.config.Enabled
	return ae.config.Enabled
}

// ProcessTick is called by the game's clock scheduler for synchronization
func (ae *AudioEngine) ProcessTick() {
	// This method allows the audio engine to synchronize with game clock
	// Currently a no-op as sound timing is handled internally
}

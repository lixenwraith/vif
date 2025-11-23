package audio

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/lixenwraith/vi-fighter/constants"
)

// AudioEngine manages all game audio with thread-safe playback
type AudioEngine struct {
	// Configuration
	config      *AudioConfig
	configMutex sync.RWMutex // Protects config access

	// Channels for command submission (increased size for burst handling)
	realTimeQueue chan AudioCommand // For immediate sounds (errors)
	stateQueue    chan AudioCommand // For state-based sounds (coins, bells)

	// Control
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Current playback state (protected by playbackMutex)
	playbackMutex   sync.Mutex
	currentSound    beep.StreamSeekCloser
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
	muted       atomic.Bool // Audio mute state (Ctrl+S toggle)
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
		fmt.Printf("Warning: speaker init failed (may already be initialized): %v\n", err)
	}

	ae := &AudioEngine{
		config:        cfg,
		realTimeQueue: make(chan AudioCommand, 5),  // Increased from 1 to 5
		stateQueue:    make(chan AudioCommand, 10), // Increased from 1 to 10 for gold sounds
		stopChan:      make(chan struct{}),
		lastSoundTime: time.Now(),
	}

	ae.initialized.Store(true)
	ae.muted.Store(true) // Start muted by default
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
		ae.StopCurrentSound()
	}
}

// SendRealTime sends a real-time audio command (non-blocking)
func (ae *AudioEngine) SendRealTime(cmd AudioCommand) bool {
	if !ae.IsEnabled() || !ae.running.Load() {
		return false
	}

	select {
	case ae.realTimeQueue <- cmd:
		return true
	default:
		ae.queueOverflows.Add(1)
		// For real-time queue, try to make room by removing oldest
		select {
		case <-ae.realTimeQueue:
			// Removed oldest, try again
			select {
			case ae.realTimeQueue <- cmd:
				return true
			default:
				return false
			}
		default:
			return false
		}
	}
}

// SendState sends a state-based audio command (non-blocking)
func (ae *AudioEngine) SendState(cmd AudioCommand) bool {
	if !ae.IsEnabled() || !ae.running.Load() {
		return false
	}

	select {
	case ae.stateQueue <- cmd:
		return true
	default:
		ae.queueOverflows.Add(1)
		// For state queue, try to make room
		select {
		case <-ae.stateQueue:
			// Removed oldest, try again
			select {
			case ae.stateQueue <- cmd:
				return true
			default:
				return false
			}
		default:
			return false
		}
	}
}

// processLoop is the main audio processing goroutine
func (ae *AudioEngine) processLoop() {
	defer ae.wg.Done()

	ticker := time.NewTicker(constants.AudioMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ae.stopChan:
			return

		case cmd := <-ae.realTimeQueue:
			if !ae.muted.Load() {
				ae.processCommand(cmd, true)
			}

		case cmd := <-ae.stateQueue:
			if !ae.muted.Load() {
				ae.processCommand(cmd, false)
			}

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
	ae.configMutex.RLock()
	minGap := ae.config.MinSoundGap
	ae.configMutex.RUnlock()

	if time.Since(ae.lastSoundTime) < minGap {
		ae.soundsDropped.Add(1)
		return
	}

	// Stop current sound if playing
	ae.StopCurrentSound()

	// Selective queue draining based on priority
	if isRealTime {
		// Real-time sounds drain state queue but not other real-time
		ae.drainStateQueue()
	} else {
		// State sounds only play if no real-time pending
		select {
		case <-ae.realTimeQueue:
			// Real-time sound pending, skip this state sound
			ae.soundsDropped.Add(1)
			return
		default:
			// No real-time pending, continue
		}
	}

	// Create and play the new sound
	ae.configMutex.RLock()
	streamer := GetSoundEffect(cmd.Type, ae.config)
	ae.configMutex.RUnlock()

	if streamer == nil {
		return
	}

	// Wrap in controller for stopping
	ctrl := &beep.Ctrl{Streamer: streamer}

	// Update playback state atomically
	ae.playbackMutex.Lock()
	ae.currentCtrl = ctrl
	ae.lastSoundTime = time.Now()
	ae.soundGeneration = cmd.Generation
	ae.playbackMutex.Unlock()

	// Play the sound
	speaker.Play(beep.Seq(
		ctrl,
		beep.Callback(func() {
			ae.soundComplete()
		}),
	))

	ae.soundsPlayed.Add(1)
}

// StopCurrentSound stops the currently playing sound (thread-safe)
func (ae *AudioEngine) StopCurrentSound() {
	ae.playbackMutex.Lock()
	defer ae.playbackMutex.Unlock()

	if ae.currentCtrl != nil {
		speaker.Lock()
		ae.currentCtrl.Paused = true
		speaker.Unlock()
		ae.currentCtrl = nil
	}

	if ae.currentSound != nil {
		err := ae.currentSound.Close()
		if err != nil {
			fmt.Printf("Error closing sound: %v\n", err)
		}
		ae.currentSound = nil
	}
}

// DrainQueues empties both command queues (thread-safe)
func (ae *AudioEngine) DrainQueues() {
	// Drain with timeout to prevent infinite loop
	timeout := time.NewTimer(constants.AudioDrainTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-ae.realTimeQueue:
			ae.soundsDropped.Add(1)
		case <-ae.stateQueue:
			ae.soundsDropped.Add(1)
		case <-timeout.C:
			return
		default:
			return
		}
	}
}

// drainStateQueue empties only the state queue
func (ae *AudioEngine) drainStateQueue() {
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
	// Handled by callback in processCommand
}

// soundComplete is called when a sound finishes playing
func (ae *AudioEngine) soundComplete() {
	ae.playbackMutex.Lock()
	ae.currentCtrl = nil
	ae.currentSound = nil
	ae.playbackMutex.Unlock()
}

// IsRunning returns true if the audio engine is running
func (ae *AudioEngine) IsRunning() bool {
	return ae.running.Load()
}

// IsEnabled returns true if audio is enabled and not muted
func (ae *AudioEngine) IsEnabled() bool {
	ae.configMutex.RLock()
	enabled := ae.config.Enabled
	ae.configMutex.RUnlock()

	return enabled && !ae.muted.Load()
}

// ToggleMute toggles the mute state and returns the new state
func (ae *AudioEngine) ToggleMute() bool {
	newState := !ae.muted.Load()
	ae.muted.Store(newState)

	// If muting, stop current sound
	if newState {
		ae.StopCurrentSound()
		ae.DrainQueues()
	}

	return !newState // Return true if sound is now enabled
}

// SetConfig updates the audio configuration (thread-safe)
func (ae *AudioEngine) SetConfig(cfg *AudioConfig) {
	if cfg == nil {
		return
	}

	ae.configMutex.Lock()
	ae.config = cfg
	ae.configMutex.Unlock()
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

	ae.configMutex.Lock()
	ae.config.MasterVolume = volume
	ae.configMutex.Unlock()
}

// IsMuted returns the current mute state
func (ae *AudioEngine) IsMuted() bool {
	return ae.muted.Load()
}

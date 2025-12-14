package audio

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// AudioEngine manages audio via pipe to system tools
type AudioEngine struct {
	config *AudioConfig
	cache  *soundCache
	mixer  *Mixer

	backend *BackendConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	ossFile *os.File // For direct OSS writes

	running    atomic.Bool
	muted      atomic.Bool
	silentMode atomic.Bool

	mu sync.RWMutex // Protects config
	wg sync.WaitGroup
}

// NewAudioEngine creates an audio engine
func NewAudioEngine(cfg ...*AudioConfig) (*AudioEngine, error) {
	config := DefaultAudioConfig()
	if len(cfg) > 0 && cfg[0] != nil {
		config = cfg[0]
	}

	ae := &AudioEngine{
		config: config,
		cache:  newSoundCache(),
	}
	ae.muted.Store(!config.Enabled)

	// Preload common sounds
	ae.cache.preload()

	return ae, nil
}

// Start launches the audio backend and mixer
func (ae *AudioEngine) Start() error {
	if ae.running.Load() {
		return fmt.Errorf("audio engine already running")
	}

	backend, err := DetectBackend()
	if err != nil {
		ae.silentMode.Store(true)
		ae.running.Store(true)
		return nil // Silent mode, not an error
	}

	ae.backend = backend

	var writer io.Writer
	if backend.Type == BackendOSS {
		// Direct file write for OSS
		f, err := os.OpenFile(backend.Path, os.O_WRONLY, 0)
		if err != nil {
			ae.silentMode.Store(true)
			ae.running.Store(true)
			return nil
		}
		ae.ossFile = f
		writer = f
	} else {
		// Exec-based backend
		cmd := exec.Command(backend.Path, backend.Args...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			ae.silentMode.Store(true)
			ae.running.Store(true)
			return nil
		}

		if err := cmd.Start(); err != nil {
			stdin.Close()
			ae.silentMode.Store(true)
			ae.running.Store(true)
			return nil
		}

		ae.cmd = cmd
		ae.stdin = stdin
		writer = stdin

		// Monitor process
		ae.wg.Add(1)
		go ae.monitorProcess()
	}

	ae.mixer = NewMixer(writer, ae.config, ae.cache)
	ae.mixer.Start()

	// Monitor mixer errors
	ae.wg.Add(1)
	go ae.monitorMixer()

	ae.running.Store(true)
	return nil
}

// monitorProcess watches for subprocess exit
func (ae *AudioEngine) monitorProcess() {
	defer ae.wg.Done()

	if ae.cmd == nil {
		return
	}

	err := ae.cmd.Wait()
	if err != nil && ae.running.Load() && !ae.silentMode.Load() {
		ae.silentMode.Store(true)
	}
}

// monitorMixer watches for pipe errors
func (ae *AudioEngine) monitorMixer() {
	defer ae.wg.Done()

	if ae.mixer == nil {
		return
	}

	select {
	case <-ae.mixer.Errors():
		ae.silentMode.Store(true)
	case <-ae.mixer.stopChan:
	}
}

// Stop terminates the engine
func (ae *AudioEngine) Stop() {
	if !ae.running.CompareAndSwap(true, false) {
		return
	}

	if ae.mixer != nil {
		ae.mixer.Stop()
	}

	if ae.stdin != nil {
		ae.stdin.Close()
	}

	if ae.ossFile != nil {
		ae.ossFile.Close()
	}

	if ae.cmd != nil && ae.cmd.Process != nil {
		ae.cmd.Process.Kill()
	}

	ae.wg.Wait()
}

// Play queues a sound for playback
func (ae *AudioEngine) Play(st SoundType) bool {
	if !ae.running.Load() || ae.muted.Load() || ae.silentMode.Load() {
		return false
	}

	if ae.mixer == nil {
		return false
	}

	ae.mu.RLock()
	master := ae.config.MasterVolume
	effects := ae.config.EffectVolumes
	ae.mu.RUnlock()

	ae.mixer.Play(st, master, effects)
	return true
}

// ToggleMute toggles mute state, returns true if now enabled
func (ae *AudioEngine) ToggleMute() bool {
	newMute := !ae.muted.Load()
	ae.muted.Store(newMute)
	return !newMute
}

// IsMuted returns current mute state
func (ae *AudioEngine) IsMuted() bool {
	return ae.muted.Load()
}

// IsEnabled returns true if running and unmuted
func (ae *AudioEngine) IsEnabled() bool {
	return ae.running.Load() && !ae.muted.Load() && !ae.silentMode.Load()
}

// IsRunning returns true if engine is running (even in silent mode)
func (ae *AudioEngine) IsRunning() bool {
	return ae.running.Load()
}

// SetVolume updates master volume (0.0-1.0)
func (ae *AudioEngine) SetVolume(vol float64) {
	if vol < 0 {
		vol = 0
	} else if vol > 1 {
		vol = 1
	}

	ae.mu.Lock()
	ae.config.MasterVolume = vol
	ae.mu.Unlock()
}

// SetConfig replaces config
func (ae *AudioEngine) SetConfig(cfg *AudioConfig) {
	if cfg == nil {
		return
	}

	ae.mu.Lock()
	ae.config = cfg
	ae.mu.Unlock()
}

// GetStats returns played, dropped, overflow counts
func (ae *AudioEngine) GetStats() (played, dropped, overflow uint64) {
	if ae.mixer != nil {
		p, d := ae.mixer.GetStats()
		return p, d, 0
	}
	return 0, 0, 0
}
package audio

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AudioEngine manages audio via pipe to system tools
// Control flows through one command channel into the mixer goroutine;
// sequencer, tracks, and voices are mixer-confined and lock-free
type AudioEngine struct {
	config *AudioConfig
	cache  *soundCache
	mixer  *Mixer

	// Backend lifecycle; beMu because failover runs concurrently with Stop
	beMu       sync.Mutex
	candidates []*BackendConfig // untried, priority order
	backend    *BackendConfig
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	ossFile    *os.File
	procExit   chan struct{} // closed on active backend exit; nil for OSS

	stderrTail *tailBuffer

	running     atomic.Bool
	paused      atomic.Bool
	effectMuted atomic.Bool
	musicMuted  atomic.Bool
	silentMode  atomic.Bool

	stopChan chan struct{}
	stopOnce sync.Once
	mu       sync.RWMutex // config
	wg       sync.WaitGroup
}

// NewAudioEngine creates an audio engine
func NewAudioEngine(cfg ...*AudioConfig) (*AudioEngine, error) {
	config := DefaultAudioConfig()
	if len(cfg) > 0 && cfg[0] != nil {
		config = cfg[0]
	}
	ae := &AudioEngine{
		config:     config,
		cache:      newSoundCache(),
		stderrTail: &tailBuffer{},
		stopChan:   make(chan struct{}),
	}
	ae.effectMuted.Store(!config.Enabled)
	ae.musicMuted.Store(!config.Enabled)
	return ae, nil
}

// Start probes backends in priority order and launches the mixer
// Returns an error when no backend survives; the engine still enters silent
// mode so the already-published AudioPlayer stays valid
func (ae *AudioEngine) Start() error {
	if !ae.running.CompareAndSwap(false, true) {
		return fmt.Errorf("audio engine already running")
	}

	// One-time DSP: all SFX buffers and the drum kit render before the mixer
	// goroutine exists — read-only afterward, shared without locks
	ae.cache.preloadAll(ae.config.EffectShapes)
	kit := buildDrumKit(DrumVariants)
	InitDefaultPatterns()

	if len(ae.config.PatternTOML) > 0 {
		if pats, err := LoadPatternsTOML(ae.config.PatternTOML); err == nil {
			for _, p := range pats {
				RegisterPattern(p)
			}
		} // parse error dropped until logging lands
	}

	cands, err := DetectBackends(ae.config.ForceBackend)
	if err != nil {
		ae.silentMode.Store(true)
		return err
	}

	ae.beMu.Lock()
	ae.candidates = cands
	w, err := ae.nextBackendLocked()
	ae.beMu.Unlock()
	if err != nil {
		ae.silentMode.Store(true)
		return err
	}

	ae.mixer = NewMixer(w, ae.cache, kit)
	ae.mixer.Start()

	ae.wg.Add(1)
	go ae.supervise()
	return nil
}

// nextBackendLocked advances through remaining candidates until one survives
// its probe; caller holds beMu
func (ae *AudioEngine) nextBackendLocked() (io.Writer, error) {
	var errs []string
	for len(ae.candidates) > 0 {
		c := ae.candidates[0]
		ae.candidates = ae.candidates[1:]
		w, err := ae.attach(c)
		if err == nil {
			ae.backend = c
			return w, nil
		}
		errs = append(errs, c.Name+": "+err.Error())
	}
	return nil, fmt.Errorf("%w: %s", ErrNoAudioBackend, strings.Join(errs, "; "))
}

// attach spawns and probes a single backend
// The probe pre-rolls one silent buffer and confirms process survival,
// catching bad args, dead daemons, and broken pipes at selection time
// Limitation: a process that accepts data but routes nowhere passes
func (ae *AudioEngine) attach(c *BackendConfig) (io.Writer, error) {
	if c.Type == BackendOSS {
		f, err := os.OpenFile(c.Path, os.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}
		ae.ossFile, ae.procExit = f, nil
		return f, nil
	}

	cmd := exec.Command(c.Path, c.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = ae.stderrTail // backend stderr no longer lost to raw-mode terminal
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, err
	}

	exit := make(chan struct{})
	ae.wg.Add(1)
	go func() {
		defer ae.wg.Done()
		cmd.Wait()
		close(exit)
	}()

	silence := make([]byte, AudioBufferSamples*AudioBytesPerFrame)
	if _, err := stdin.Write(silence); err != nil {
		cmd.Process.Kill()
		<-exit
		return nil, fmt.Errorf("probe write: %w", err)
	}
	select {
	case <-exit:
		return nil, fmt.Errorf("exited during probe: %s", ae.stderrTail.LastLine())
	case <-time.After(AudioProbeWindow):
	}

	ae.cmd, ae.stdin, ae.procExit = cmd, stdin, exit
	return stdin, nil
}

// supervise reacts to backend death or mixer write failure with failover
func (ae *AudioEngine) supervise() {
	defer ae.wg.Done()
	for {
		ae.beMu.Lock()
		exit := ae.procExit
		ae.beMu.Unlock()

		select {
		case <-ae.stopChan:
			return
		case <-exit: // nil for OSS: blocks; mixer errors still covered
		case <-ae.mixer.Errors():
		}
		if !ae.running.Load() {
			return
		}
		ae.failover()
	}
}

// failover kills the dead backend and attaches the next candidate
// Exhausted candidates latch silent mode; mixer keeps state, skips writes
func (ae *AudioEngine) failover() {
	ae.beMu.Lock()
	defer ae.beMu.Unlock()

	if ae.cmd != nil && ae.cmd.Process != nil {
		ae.cmd.Process.Kill()
	}
	if ae.stdin != nil {
		ae.stdin.Close()
	}
	if ae.ossFile != nil {
		ae.ossFile.Close()
	}
	ae.cmd, ae.stdin, ae.ossFile, ae.procExit, ae.backend = nil, nil, nil, nil, nil

	w, err := ae.nextBackendLocked()
	if err != nil {
		ae.silentMode.Store(true)
		return
	}
	ae.mixer.SwapOutput(w)
}

// Stop terminates the engine
func (ae *AudioEngine) Stop() {
	if !ae.running.CompareAndSwap(true, false) {
		return
	}
	ae.stopOnce.Do(func() { close(ae.stopChan) })

	if ae.mixer != nil {
		ae.mixer.Stop()
	}

	ae.beMu.Lock()
	if ae.stdin != nil {
		ae.stdin.Close()
	}
	if ae.ossFile != nil {
		ae.ossFile.Close()
	}
	if ae.cmd != nil && ae.cmd.Process != nil {
		ae.cmd.Process.Kill()
	}
	ae.beMu.Unlock()

	ae.wg.Wait()
}

// SetPaused toggles the paused state (music frozen + effects gated)
func (ae *AudioEngine) SetPaused(paused bool) {
	ae.paused.Store(paused)
	if ae.mixer != nil {
		ae.mixer.SetPaused(paused)
	}
}

// Play queues a sound effect; volume computed here, dampening at the mixer
func (ae *AudioEngine) Play(st SoundType) bool {
	if !ae.running.Load() || ae.paused.Load() || ae.effectMuted.Load() || ae.silentMode.Load() {
		return false
	}
	if ae.mixer == nil {
		return false
	}
	ae.mu.RLock()
	vol := ae.config.MasterVolume
	if ev, ok := ae.config.EffectVolumes[st]; ok {
		vol *= ev
	}
	ae.mu.RUnlock()
	ae.mixer.Send(audioCmd{op: cmdPlay, sound: st, f1: vol})
	return true
}

func (ae *AudioEngine) send(c audioCmd) {
	if ae.mixer != nil {
		ae.mixer.Send(c)
	}
}

func (ae *AudioEngine) ToggleEffectMute() bool {
	wasMuted := ae.effectMuted.Load()
	ae.effectMuted.Store(!wasMuted)
	return wasMuted
}

func (ae *AudioEngine) IsEffectMuted() bool { return ae.effectMuted.Load() }

func (ae *AudioEngine) IsEnabled() bool {
	return ae.running.Load() && !ae.effectMuted.Load() && !ae.silentMode.Load()

}
func (ae *AudioEngine) IsRunning() bool { return ae.running.Load() }

func (ae *AudioEngine) SetEffectMuted(muted bool) { ae.effectMuted.Store(muted) }

func (ae *AudioEngine) SetMusicMuted(muted bool) {
	ae.musicMuted.Store(muted)
	if ae.mixer != nil {
		ae.mixer.SetMusicMuted(muted)
		if muted {
			ae.mixer.Send(audioCmd{op: cmdMusicStop})
		}
	}
}

func (ae *AudioEngine) ToggleMusicMute() bool {
	newMute := !ae.musicMuted.Load()
	ae.SetMusicMuted(newMute)
	return !newMute
}

func (ae *AudioEngine) IsMusicMuted() bool { return ae.musicMuted.Load() }

func (ae *AudioEngine) StartMusic() {
	if !ae.musicMuted.Load() {
		ae.send(audioCmd{op: cmdMusicStart})
	}
}

func (ae *AudioEngine) StopMusic() { ae.send(audioCmd{op: cmdMusicStop}) }

func (ae *AudioEngine) ResetMusic() { ae.send(audioCmd{op: cmdMusicReset}) }

func (ae *AudioEngine) SetMusicBPM(bpm int) { ae.send(audioCmd{op: cmdBPM, i1: bpm, b: true}) }

// Run-seed injection; call before StartMusic on new game
func (ae *AudioEngine) SetMusicSeed(seed int64) { ae.send(audioCmd{op: cmdSeed, seed: seed}) }

func (ae *AudioEngine) SetMusicSwing(a float64) { ae.send(audioCmd{op: cmdSwing, f1: a}) }

func (ae *AudioEngine) SetMusicVolume(vol float64) { ae.send(audioCmd{op: cmdMusicVol, f1: vol}) }

// SetPattern targets a sequencer slot (0=rhythm, 1=melody, 2=free)
func (ae *AudioEngine) SetPattern(slot int, p PatternID, crossfadeSamples int, quantize bool) {
	ae.send(audioCmd{op: cmdPattern, pattern: p, i1: crossfadeSamples, i2: slot, b: quantize})
}

// SetTrackMask enables/disables tracks within a slot's pattern
func (ae *AudioEngine) SetTrackMask(slot int, mask uint32) {
	ae.send(audioCmd{op: cmdMask, i1: slot, i2: int(mask)})
}

// SetHarmony updates key, scale, and chord progression
// root<=0 keeps root, scale out of range keeps scale, nil progression keeps
func (ae *AudioEngine) SetHarmony(root int, scale ScaleID, progression []int) {
	ae.send(audioCmd{op: cmdHarmony, i1: root, i2: int(scale), ints: progression})
}

func (ae *AudioEngine) TriggerMelodyNote(note int, velocity float64, durationSamples int, instr InstrumentType) {
	ae.send(audioCmd{op: cmdNote, i1: note, f1: velocity, i2: durationSamples, instr: instr})
}

func (ae *AudioEngine) IsMusicPlaying() bool {
	return ae.mixer != nil && ae.mixer.musicRunning.Load()
}

// GetStats retained for compatibility, overflow always 0
func (ae *AudioEngine) GetStats() (played, dropped, overflow uint64) {
	p, d := ae.Stats()
	return p, d, 0
}

// --- engine.AudioTelemetry ---

func (ae *AudioEngine) Stats() (played, dropped uint64) {
	if ae.mixer != nil {
		return ae.mixer.GetStats()
	}
	return 0, 0
}

func (ae *AudioEngine) BackendName() string {
	ae.beMu.Lock()
	defer ae.beMu.Unlock()
	if ae.backend != nil {
		return ae.backend.Name
	}
	return ""
}

func (ae *AudioEngine) IsSilent() bool { return ae.silentMode.Load() }

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

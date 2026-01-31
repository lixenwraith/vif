package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// activeSound tracks a playing sound instance
type activeSound struct {
	buffer floatBuffer
	pos    int
	volume float64
}

// Mixer handles mixing and output
type Mixer struct {
	output io.Writer
	cache  *soundCache
	config *AudioConfig

	playQueue chan playRequest
	stopChan  chan struct{}
	stopped   atomic.Bool
	paused    atomic.Bool

	// Accessed only by mix goroutine
	active []activeSound

	// Music
	sequencer  *Sequencer
	musicMuted atomic.Bool

	// Stats
	statsMu sync.Mutex
	played  uint64
	dropped uint64

	// Error signaling
	errChan chan error
}

type playRequest struct {
	sound  core.SoundType
	volume float64
}

// NewMixer creates a mixer writing to out
func NewMixer(out io.Writer, cfg *AudioConfig, cache *soundCache) *Mixer {
	return &Mixer{
		output:    out,
		config:    cfg,
		cache:     cache,
		playQueue: make(chan playRequest, 32),
		stopChan:  make(chan struct{}),
		active:    make([]activeSound, 0, 8),
		errChan:   make(chan error, 1),
		sequencer: NewSequencer(parameter.DefaultBPM),
	}
}

// SetPaused sets the pause state
func (m *Mixer) SetPaused(paused bool) {
	m.paused.Store(paused)
}

// Start begins the mixing loop
func (m *Mixer) Start() {
	go m.loop()
}

// Stop signals the mixer to halt
func (m *Mixer) Stop() {
	if m.stopped.CompareAndSwap(false, true) {
		close(m.stopChan)
	}
}

// Play queues a sound with computed volume
func (m *Mixer) Play(st core.SoundType, masterVol float64, effectVols map[core.SoundType]float64) {
	if m.stopped.Load() {
		return
	}

	vol := masterVol
	if ev, ok := effectVols[st]; ok {
		vol *= ev
	}

	// Get dampening factor for rapid-fire sounds
	_, dampen := m.cache.getWithDampening(st)
	vol *= dampen

	select {
	case m.playQueue <- playRequest{sound: st, volume: vol}:
	default:
		m.statsMu.Lock()
		m.dropped++
		m.statsMu.Unlock()
	}
}

// Errors returns channel for pipe errors
func (m *Mixer) Errors() <-chan error {
	return m.errChan
}

// SetMusicMuted sets music mute state
func (m *Mixer) SetMusicMuted(muted bool) {
	m.musicMuted.Store(muted)
	if muted && m.sequencer != nil {
		m.sequencer.Stop()
	}
}

// IsMusicMuted returns music mute state
func (m *Mixer) IsMusicMuted() bool {
	return m.musicMuted.Load()
}

// Sequencer returns the sequencer for direct control
func (m *Mixer) Sequencer() *Sequencer {
	return m.sequencer
}

// StartMusic starts the sequencer
func (m *Mixer) StartMusic() {
	if m.sequencer != nil && !m.musicMuted.Load() {
		m.sequencer.Start()
	}
}

// StopMusic stops the sequencer
func (m *Mixer) StopMusic() {
	if m.sequencer != nil {
		m.sequencer.Stop()
	}
}

// loop is the main mixing goroutine
func (m *Mixer) loop() {
	ticker := time.NewTicker(parameter.AudioBufferDuration)
	defer ticker.Stop()

	samplesPerTick := parameter.AudioBufferSamples
	mixBuf := make([]float64, samplesPerTick)
	outBytes := make([]byte, samplesPerTick*parameter.AudioBytesPerFrame)

	// Pause smoothing
	pauseGain := 1.0

	for {
		select {
		case <-m.stopChan:
			return

		case req := <-m.playQueue:
			// If paused, drop new requests immediately to prevent queue buildup
			if m.paused.Load() {
				m.statsMu.Lock()
				m.dropped++
				m.statsMu.Unlock()
				continue
			}

			buf := m.cache.get(req.sound)
			if len(buf) > 0 {
				m.active = append(m.active, activeSound{
					buffer: buf,
					pos:    0,
					volume: req.volume,
				})
				m.statsMu.Lock()
				m.played++
				m.statsMu.Unlock()
			}
			// Drain additional queued requests
			m.drainQueue(4)

		case <-ticker.C:
			// Clear mix buffer
			for i := range mixBuf {
				mixBuf[i] = 0
			}

			isPaused := m.paused.Load()

			// Handle Pause Fading (prevents clicks)
			if isPaused {
				if pauseGain > 0 {
					pauseGain -= 0.2 // 250ms fade out (at 50ms tick)
					if pauseGain < 0 {
						pauseGain = 0
					}
				}
			} else {
				if pauseGain < 1.0 {
					pauseGain += 0.2 // 250ms fade in
					if pauseGain > 1.0 {
						pauseGain = 1.0
					}
				}
			}

			// Only process audio logic if we are audible or fading out
			if pauseGain > 0.001 {
				// Generate music (if not effectMuted and sequencer is running)
				// If paused, Generate() is not called, freezing sequencer time, keeping the beat aligned for resume
				if !isPaused && !m.musicMuted.Load() && m.sequencer != nil {
					m.sequencer.Generate(mixBuf)
				}

				// Mix active sound effects
				// Even if paused, letting tails play out or fade via pauseGain
				if len(m.active) > 0 {
					m.active = m.mixActive(mixBuf, samplesPerTick)
				}

				// Apply pause gain
				if pauseGain < 1.0 {
					for i := range mixBuf {
						mixBuf[i] *= pauseGain
					}
				}
			}

			// Convert to int16 bytes with soft limiting
			floatToBytes(mixBuf, outBytes)

			// Write to output
			if _, err := m.output.Write(outBytes); err != nil {
				select {
				case m.errChan <- fmt.Errorf("%w: %v", ErrPipeClosed, err):
				default:
				}
				return
			}
		}
	}
}

// drainQueue processes up to n additional queued requests
func (m *Mixer) drainQueue(n int) {
	for i := 0; i < n; i++ {
		select {
		case req := <-m.playQueue:
			buf := m.cache.get(req.sound)
			if len(buf) > 0 {
				m.active = append(m.active, activeSound{
					buffer: buf,
					pos:    0,
					volume: req.volume,
				})
				m.statsMu.Lock()
				m.played++
				m.statsMu.Unlock()
			}
		default:
			return
		}
	}
}

// mixActive mixes all active sounds into buf, returns remaining sounds
func (m *Mixer) mixActive(buf []float64, samples int) []activeSound {
	remaining := m.active[:0]

	for i := range m.active {
		s := &m.active[i]
		for j := 0; j < samples && s.pos < len(s.buffer); j++ {
			buf[j] += s.buffer[s.pos] * s.volume
			s.pos++
		}
		if s.pos < len(s.buffer) {
			remaining = append(remaining, *s)
		}
	}

	return remaining
}

// floatToBytes converts float64 mono to interleaved stereo int16 LE bytes
// Applies soft limiting before hard clip
func floatToBytes(in []float64, out []byte) {
	for i, v := range in {
		// Soft limiter (tanh-style)
		if v > 0.8 {
			v = 0.8 + 0.2*(1.0-1.0/(1.0+(v-0.8)*5.0))
		} else if v < -0.8 {
			v = -0.8 - 0.2*(1.0-1.0/(1.0+(-v-0.8)*5.0))
		}

		// Hard clip
		if v > 1.0 {
			v = 1.0
		} else if v < -1.0 {
			v = -1.0
		}

		i16 := int16(v * 32767)
		idx := i * 4
		binary.LittleEndian.PutUint16(out[idx:], uint16(i16))   // L
		binary.LittleEndian.PutUint16(out[idx+2:], uint16(i16)) // R
	}
}

// GetStats returns played and dropped counts
func (m *Mixer) GetStats() (played, dropped uint64) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	return m.played, m.dropped
}
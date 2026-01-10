package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
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

	// Accessed only by mix goroutine
	active []activeSound

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
	}
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

// loop is the main mixing goroutine
func (m *Mixer) loop() {
	ticker := time.NewTicker(constant.AudioBufferDuration)
	defer ticker.Stop()

	samplesPerTick := constant.AudioBufferSamples
	mixBuf := make([]float64, samplesPerTick)
	outBytes := make([]byte, samplesPerTick*constant.AudioBytesPerFrame)

	for {
		select {
		case <-m.stopChan:
			return

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
			// Drain additional queued requests
			m.drainQueue(4)

		case <-ticker.C:
			if len(m.active) == 0 {
				// Write silence to keep pipe alive
				for i := range outBytes {
					outBytes[i] = 0
				}
			} else {
				// ClearAllComponent mix buffer
				for i := range mixBuf {
					mixBuf[i] = 0
				}

				// Mix active sounds
				m.active = m.mixActive(mixBuf, samplesPerTick)

				// Convert to int16 bytes with soft limiting
				floatToBytes(mixBuf, outBytes)
			}

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
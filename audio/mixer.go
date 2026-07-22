package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync/atomic"
	"time"
)

// activeSound tracks a playing sound instance
type activeSound struct {
	st     SoundID // per-type polyphony accounting
	buffer floatBuffer
	pos    int
	volume float64
}

// Mixer is the audio thread. Fields below the confinement marker are touched
// only by the mix goroutine; all mutation arrives as audioCmd values
type Mixer struct {
	cmds     chan audioCmd
	stopChan chan struct{}
	stopped  atomic.Bool

	paused       atomic.Bool
	musicMuted   atomic.Bool
	musicRunning atomic.Bool // query mirror for IsMusicPlaying

	played  atomic.Uint64
	dropped atomic.Uint64
	errChan chan error

	// --- mix-goroutine confined ---
	output    io.Writer
	outBroken bool
	cache     *soundCache
	sequencer *Sequencer
	active    []activeSound

	sfxVar   []uint32 // variant rotation counters, indexed by SoundID
	lastPlay []time.Time
	rapidVol []float64

	pauseGain float64
	duckGain  float64
	musicBuf  []float64
	sfxBuf    []float64
}

// NewMixer creates a mixer writing to out
func NewMixer(out io.Writer, cache *soundCache, kit *drumKit) *Mixer {
	m := &Mixer{
		output:    out,
		cache:     cache,
		cmds:      make(chan audioCmd, 256),
		stopChan:  make(chan struct{}),
		errChan:   make(chan error, 1),
		sequencer: NewSequencer(DefaultBPM, kit),
		active:    make([]activeSound, 0, 8),
		pauseGain: 1.0,
		duckGain:  1.0,
	}
	n := cache.count()
	m.sfxVar = make([]uint32, n)
	m.lastPlay = make([]time.Time, n)
	m.rapidVol = make([]float64, n)
	for i := range m.rapidVol {
		m.rapidVol[i] = 1.0
	}
	return m
}

// Send enqueues a command from any goroutine; full queue drops
func (m *Mixer) Send(c audioCmd) {
	if m.stopped.Load() {
		return
	}
	select {
	case m.cmds <- c:
	default:
		if c.op == cmdPlay {
			m.dropped.Add(1)
		}
	}
}

// SwapOutput redirects backend output; applied at the next tick
func (m *Mixer) SwapOutput(w io.Writer) { m.Send(audioCmd{op: cmdSwapOutput, w: w}) }

func (m *Mixer) SetPaused(p bool)        { m.paused.Store(p) }
func (m *Mixer) SetMusicMuted(mute bool) { m.musicMuted.Store(mute) }
func (m *Mixer) IsMusicMuted() bool      { return m.musicMuted.Load() }
func (m *Mixer) Errors() <-chan error    { return m.errChan }

func (m *Mixer) Start() { go m.loop() }

func (m *Mixer) Stop() {
	if m.stopped.CompareAndSwap(false, true) {
		close(m.stopChan)
	}
}

func (m *Mixer) GetStats() (played, dropped uint64) {
	return m.played.Load(), m.dropped.Load()
}

// onePoleCoef converts a smoothing time constant to a per-sample coefficient
func onePoleCoef(tau time.Duration) float64 {
	return 1.0 - math.Exp(-1.0/(tau.Seconds()*float64(AudioSampleRate)))
}

// loop: drain commands, render, write — one pass per buffer tick
func (m *Mixer) loop() {
	ticker := time.NewTicker(AudioBufferDuration)
	defer ticker.Stop()

	n := AudioBufferSamples
	m.musicBuf = make([]float64, n)
	m.sfxBuf = make([]float64, n)
	mixBuf := make([]float64, n)
	outBytes := make([]byte, n*AudioBytesPerFrame)

	// Pause fade is per-sample (was 5×0.2 staircase across ticks)
	pauseStep := 1.0 / (AudioPauseFade.Seconds() * float64(AudioSampleRate))
	duckAtk := onePoleCoef(MusicDuckAttack)
	duckRel := onePoleCoef(MusicDuckRelease)

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.drainCmds()
			m.renderTick(mixBuf, outBytes, n, pauseStep, duckAtk, duckRel)
		}
	}
}

func (m *Mixer) drainCmds() {
	for {
		select {
		case c := <-m.cmds:
			m.apply(c)
		default:
			return
		}
	}
}

func (m *Mixer) apply(c audioCmd) {
	switch c.op {
	case cmdPlay:
		m.startSound(c.sound, c.f1)
	case cmdBPM:
		m.sequencer.SetBPM(c.i1, c.b)
	case cmdSwing:
		m.sequencer.SetSwing(c.f1)
	case cmdMusicVol:
		m.sequencer.SetVolume(c.f1)
	case cmdPattern:
		m.sequencer.SetPattern(int(c.slot), c.pattern, c.i1, c.b)
	case cmdMask:
		m.sequencer.SetMask(int(c.slot), uint32(c.i2))
	case cmdHarmony:
		m.sequencer.SetHarmonyCfg(c.i1, ScaleID(c.i2), c.ints)
	case cmdNote:
		m.sequencer.TriggerNote(c.i1, c.f1, c.i2, c.instr)
	case cmdMusicStart:
		m.sequencer.Start()
		m.musicRunning.Store(true)
	case cmdMusicStop:
		m.sequencer.Stop()
		m.musicRunning.Store(false)
	case cmdMusicReset:
		m.sequencer.Reset()
		m.musicRunning.Store(false)
	case cmdSwapOutput:
		m.output = c.w
		m.outBroken = false
	case cmdSeed:
		m.sequencer.Reseed(c.seed)
	case cmdArrangement:
		m.sequencer.SetArrangement(c.tier, Arrangement{Rhythm: c.pattern, Melody: PatternID(c.i2)})
	case cmdIntensity:
		m.sequencer.SetIntensity(c.tier, c.i1, c.b, c.reveal)
	}
}

// startSound applies rapid-fire dampening and polyphony caps, then activates a voice on a rotated variant
func (m *Mixer) startSound(st SoundID, vol float64) {
	if m.paused.Load() {
		m.dropped.Add(1)
		return
	}
	if st <= 0 || int(st) >= len(m.sfxVar) {
		m.dropped.Add(1)
		return
	}
	vars := m.cache.variants(st)
	if len(vars) == 0 {
		m.dropped.Add(1)
		return
	}
	buf := vars[m.sfxVar[st]%uint32(len(vars))]
	m.sfxVar[st]++

	now := time.Now()
	elapsed := now.Sub(m.lastPlay[st])
	if elapsed < RapidFireCooldown {
		m.rapidVol[st] *= RapidFireDecay
		if m.rapidVol[st] < RapidFireMinVolume {
			m.rapidVol[st] = RapidFireMinVolume
		}
	} else {
		// gradual recovery replaces instant reset — sustained fire
		// at real cadence previously reset to full volume every shot
		rec := elapsed.Seconds() / RapidFireRecovery.Seconds()
		if rec > 1 {
			rec = 1
		}
		m.rapidVol[st] += (1.0 - m.rapidVol[st]) * rec
	}
	m.lastPlay[st] = now

	ns := activeSound{st: st, buffer: buf, volume: vol * m.rapidVol[st]}

	// Same-type cap: steal the most-progressed sibling (deep in its decay tail)
	cnt, victim := 0, -1
	for i := range m.active {
		if m.active[i].st != st {
			continue
		}
		cnt++
		if victim < 0 || m.active[i].pos > m.active[victim].pos {
			victim = i
		}
	}
	if cnt >= MaxSFXPerType {
		m.active[victim] = ns
		m.played.Add(1)
		return
	}
	if len(m.active) >= MaxActiveSFX {
		g := 0
		for i := range m.active {
			if m.active[i].pos > m.active[g].pos {
				g = i
			}
		}
		m.active[g] = ns
		m.played.Add(1)
		return
	}
	m.active = append(m.active, ns)
	m.played.Add(1)
}

// renderTick renders one buffer: music bus, SFX bus, duck, pause, limit, write
func (m *Mixer) renderTick(mixBuf []float64, outBytes []byte, n int, pauseStep, duckAtk, duckRel float64) {
	isPaused := m.paused.Load()

	clear(m.musicBuf)
	clear(m.sfxBuf)

	// Music freezes under pause: sequencer position holds for aligned resume
	if !isPaused && !m.musicMuted.Load() && m.sequencer.IsRunning() {
		m.sequencer.Generate(m.musicBuf)
	}

	// SFX tails always render; pause gain fades them out
	sfxLive := len(m.active) > 0
	if sfxLive {
		m.active = m.mixActive(m.sfxBuf, n)
	}

	// sidechain duck — music dips under active effects
	duckTarget := 1.0
	if sfxLive {
		duckTarget = MusicDuckAmount
	}

	for i := 0; i < n; i++ {
		coef := duckRel
		if duckTarget < m.duckGain {
			coef = duckAtk
		}
		m.duckGain += (duckTarget - m.duckGain) * coef

		if isPaused {
			if m.pauseGain > 0 {
				m.pauseGain -= pauseStep
				if m.pauseGain < 0 {
					m.pauseGain = 0
				}
			}
		} else if m.pauseGain < 1 {
			m.pauseGain += pauseStep
			if m.pauseGain > 1 {
				m.pauseGain = 1
			}
		}

		mixBuf[i] = (m.musicBuf[i]*m.duckGain + m.sfxBuf[i]) * m.pauseGain
	}

	floatToBytes(mixBuf, outBytes)

	if m.outBroken {
		return // keep rendering state; supervisor swaps output or latches silent
	}
	if _, err := m.output.Write(outBytes); err != nil {
		// Mixer doesn't exit on write error — flags broken output,
		// continues; failover restores audio via cmdSwapOutput
		m.outBroken = true
		select {
		case m.errChan <- fmt.Errorf("%w: %v", ErrPipeClosed, err):
		default:
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

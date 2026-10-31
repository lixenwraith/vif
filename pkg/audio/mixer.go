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
	done     chan struct{}
	started  atomic.Bool
	stopped  atomic.Bool

	paused       atomic.Bool
	musicMuted   atomic.Bool
	musicRunning atomic.Bool // query mirror for IsMusicPlaying

	played  atomic.Uint64
	dropped atomic.Uint64
	errChan chan error

	period time.Duration // one mixer pass, set at construction

	// --- mix-goroutine confined ---
	output    io.Writer
	outBroken bool
	cache     *soundCache
	kit       *kit
	sequencer *Sequencer
	active    []activeSound

	sfxVar   []uint32 // variant rotation counters, indexed by SoundID
	lastPlay []time.Time
	rapidVol []float64

	pauseGain  float64
	duckGain   float64
	limitEnv   float64   // the limiter's peak follower
	limitGain  float64   // the gain it has ramped to
	limitBuf   []float64 // the look-ahead delay
	limitPos   int
	musicBuf   []float64
	sfxBuf     []float64
	musicTap   io.Writer
	musicBytes []byte
}

// NewMixer creates a mixer writing to out
func NewMixer(out io.Writer, cache *soundCache, k *kit, period time.Duration) *Mixer {
	m := &Mixer{
		period:    period,
		output:    out,
		cache:     cache,
		kit:       k,
		cmds:      make(chan audioCmd, 256),
		stopChan:  make(chan struct{}),
		done:      make(chan struct{}),
		errChan:   make(chan error, 1),
		sequencer: NewSequencer(DefaultBPM, k),
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

// Send enqueues a command from any goroutine. Reports false when the queue is
// full and the command was dropped; play commands also bump the drop counter.
func (m *Mixer) Send(c audioCmd) bool {
	if m.stopped.Load() {
		return false
	}
	select {
	case m.cmds <- c:
		return true
	default:
		if c.op == cmdPlay {
			m.dropped.Add(1)
		}
		return false
	}
}

// SwapOutput redirects backend output; applied at the next tick
func (m *Mixer) SwapOutput(w io.Writer) { m.Send(audioCmd{op: cmdSwapOutput, w: w}) }

func (m *Mixer) SetPaused(p bool)        { m.paused.Store(p) }
func (m *Mixer) SetMusicMuted(mute bool) { m.musicMuted.Store(mute) }
func (m *Mixer) IsMusicMuted() bool      { return m.musicMuted.Load() }
func (m *Mixer) Errors() <-chan error    { return m.errChan }

func (m *Mixer) Start() {
	if m.started.CompareAndSwap(false, true) {
		go m.loop()
	}
}

func (m *Mixer) Stop() {
	if m.stopped.CompareAndSwap(false, true) {
		close(m.stopChan)
	}
}

// Wait blocks until the mix goroutine has returned, reporting whether it did so
// within timeout (<= 0 polls); true at once if Start was never called. A true
// return is a happens-before edge against every mixer-confined write. A false one
// means the goroutine is still inside output.Write on a backend that stopped
// draining: the caller must not touch mixer-confined state or reset the registries.
func (m *Mixer) Wait(timeout time.Duration) bool {
	if !m.started.Load() {
		return true
	}
	if timeout <= 0 {
		select {
		case <-m.done:
			return true
		default:
			return false
		}
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-m.done:
		return true
	case <-t.C:
		return false
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
	defer close(m.done)
	ticker := time.NewTicker(m.period)
	defer ticker.Stop()

	n := bufferFrames(m.period)
	m.musicBuf = make([]float64, n)
	m.sfxBuf = make([]float64, n)
	m.musicBytes = make([]byte, n*AudioBytesPerFrame)
	m.limitBuf = make([]float64, LimiterLookahead)
	m.limitGain = 1
	mixBuf := make([]float64, n)
	outBytes := make([]byte, n*AudioBytesPerFrame)

	// Pause fade steps per sample, not per tick
	pauseStep := 1.0 / (AudioPauseFade.Seconds() * float64(AudioSampleRate))
	duckAtk := onePoleCoef(MusicDuckAttack)
	duckRel := onePoleCoef(MusicDuckRelease)
	limRel := math.Exp(-1.0 / (LimiterRelease.Seconds() * float64(AudioSampleRate)))

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			// select picks at random once a slow tick leaves both cases ready
			if m.stopped.Load() {
				return
			}
			m.drainCmds()
			m.renderTick(mixBuf, outBytes, n, pauseStep, duckAtk, duckRel, limRel)
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
	case cmdSlotMute:
		m.sequencer.SetSlotMuted(int(c.slot), c.b)
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
	case cmdIntensity:
		m.sequencer.SetIntensity(c.tier, c.i1, c.b, c.reveal)
	case cmdReloadSound:
		m.growTables(c.sound)
		m.cache.replace(c.sound, c.bufs)
		m.kit.rebind(c.sound, c.bufs)
	case cmdReloadPattern:
		m.sequencer.ReloadPattern(c.pattern)
	case cmdReloadVoice:
		m.kit.setVoice(c.instr, c.voice)
	case cmdPlayBuffer:
		m.startBuffer(c.buf, c.f1)
	case cmdAutoFill:
		m.sequencer.autoFill = c.b
	case cmdMusicTap:
		m.musicTap = c.w
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
		// recovery is in proportion to the gap, so sustained fire at
		// real cadence does not return to full volume every shot
		rec := elapsed.Seconds() / RapidFireRecovery.Seconds()
		if rec > 1 {
			rec = 1
		}
		m.rapidVol[st] += (1.0 - m.rapidVol[st]) * rec
	}
	m.lastPlay[st] = now

	m.admit(activeSound{st: st, buffer: buf, volume: vol * m.rapidVol[st]})
}

// renderTick renders one buffer: music bus, sfx bus, duck, pause, limit, write
func (m *Mixer) renderTick(mixBuf []float64, outBytes []byte, n int, pauseStep, duckAtk, duckRel, limRel float64) {
	isPaused := m.paused.Load()

	clear(m.musicBuf)
	clear(m.sfxBuf)

	// Music plays out the pause fade, then freezes: position holds for an aligned resume
	if (!isPaused || m.pauseGain > 0) && !m.musicMuted.Load() && m.sequencer.IsRunning() {
		m.sequencer.Generate(m.musicBuf)
		if m.musicTap != nil {
			floatToBytes(m.musicBuf, m.musicBytes)
			_, _ = m.musicTap.Write(m.musicBytes) // a failed tap costs the recording, not playback
		}
	}

	// Sfx tails always render; pause gain fades them out
	sfxLive := len(m.active) > 0
	if sfxLive {
		m.active = m.mixActive(m.sfxBuf, n)
	}

	// sidechain duck — music dips under sounding sfx
	duckTarget := 1.0
	if sfxLive {
		duckTarget = MusicDuckAmount
	}

	for i := range n {
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

		v := (m.musicBuf[i]*m.duckGain + m.sfxBuf[i]) * m.pauseGain
		// The limiter follows the peak now and ramps toward its gain by at most
		// a look-ahead's worth a sample, applied to the sample a look-ahead ago
		if a := math.Abs(v); a > m.limitEnv {
			m.limitEnv = a
		} else {
			m.limitEnv *= limRel
		}
		target := 1.0
		if m.limitEnv > LimiterCeiling {
			target = LimiterCeiling / m.limitEnv
		}
		const step = 1.0 / LimiterLookahead
		if target < m.limitGain {
			m.limitGain = max(target, m.limitGain-step)
		} else {
			m.limitGain = min(target, m.limitGain+step)
		}
		mixBuf[i] = m.limitBuf[m.limitPos] * m.limitGain
		m.limitBuf[m.limitPos] = v
		m.limitPos = (m.limitPos + 1) % LimiterLookahead
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

// floatToBytes converts float64 mono to interleaved stereo int16 LE bytes, hard
// clipping what the limiter let through
func floatToBytes(in []float64, out []byte) {
	for i, v := range in {
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

// growTables extends the per-SoundID tables to cover id. Reachable only via
// cmdReloadSound for a sound defined after Start.
func (m *Mixer) growTables(id SoundID) {
	n := int(id) + 1
	if n <= len(m.sfxVar) {
		return
	}
	m.sfxVar = append(m.sfxVar, make([]uint32, n-len(m.sfxVar))...)
	m.lastPlay = append(m.lastPlay, make([]time.Time, n-len(m.lastPlay))...)
	for len(m.rapidVol) < n {
		m.rapidVol = append(m.rapidVol, 1.0)
	}
}

// admit inserts a voice under the per-type and global caps, stealing the
// most-progressed sibling (deepest into its decay tail). Shared by the registry
// and preview paths.
func (m *Mixer) admit(ns activeSound) {
	cnt, victim := 0, -1
	for i := range m.active {
		if m.active[i].st != ns.st {
			continue
		}
		cnt++
		if victim < 0 || m.active[i].pos > m.active[victim].pos {
			victim = i
		}
	}
	switch {
	case cnt >= MaxSfxPerSound:
		m.active[victim] = ns
	case len(m.active) >= MaxActiveSfx:
		g := 0
		for i := range m.active {
			if m.active[i].pos > m.active[g].pos {
				g = i
			}
		}
		m.active[g] = ns
	default:
		m.active = append(m.active, ns)
	}
	m.played.Add(1)
}

// startBuffer plays an unregistered buffer. st is SoundNone, so previews
// contend only with each other for the per-type cap; rapid-fire dampening and
// variant rotation do not apply.
func (m *Mixer) startBuffer(buf floatBuffer, vol float64) {
	if m.paused.Load() || len(buf) == 0 {
		m.dropped.Add(1)
		return
	}
	m.admit(activeSound{st: SoundNone, buffer: buf, volume: vol})
}

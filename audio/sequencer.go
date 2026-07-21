package audio

import (
	"math"
	"math/rand/v2"
)

type patternTransition struct {
	pattern          PatternID
	targetBar        int64
	crossfadeSamples int
}

// slotState owns the A/B PatternPlayer pair for one layer
// During a fade: nxt receives step triggers and ramps in; cur stops receiving
// triggers and its active voices ring out under the complementary gain
type slotState struct {
	cur, nxt   *PatternPlayer
	curID      PatternID
	nxtID      PatternID
	pending    *patternTransition
	xPos, xLen int
	fading     bool

	revealN     int       // >0: per-bar progressive track reveal on incoming pattern
	fillSavedID PatternID // slot 2: restored after auto-fill bar
	inFill      bool
}

func (ss *slotState) player() *PatternPlayer {
	if ss.fading {
		return ss.nxt
	}
	return ss.cur
}

func (ss *slotState) activeID() PatternID {
	if ss.fading {
		return ss.nxtID
	}
	return ss.curID
}

// snap force-completes an in-flight fade
func (ss *slotState) snap() {
	if !ss.fading {
		return
	}
	ss.cur.Reset()
	ss.cur, ss.nxt = ss.nxt, ss.cur
	ss.curID = ss.nxtID
	ss.fading = false
}

// sample renders the equal-power (√t / √(1−t)) mix of the pair
func (ss *slotState) sample() float64 {
	if !ss.fading {
		return ss.cur.Sample()
	}
	t := float64(ss.xPos) / float64(ss.xLen)
	out := ss.cur.Sample()*math.Sqrt(1.0-t) + ss.nxt.Sample()*math.Sqrt(t)
	ss.xPos++
	if ss.xPos >= ss.xLen {
		ss.snap()
	}
	return out
}

const seqStream = 0x9E3779B97F4A7C15 // fixed PCG stream selector; seed varies per run

// Sequencer maintains tempo, harmony, and N crossfading pattern layers
// Mixer-goroutine confined: no synchronization
type Sequencer struct {
	bpm            int
	pendingBPM     int // applied at next bar boundary; 0 = none
	samplesPerStep int
	swing          float64
	volume         float64
	startGain      float64
	startInc       float64

	currentStep int64
	samplePos   int64
	barCount    int64

	harmony  *harmony
	slots    [MusicSlots]slotState
	gains    [MusicSlots]float64
	rng      *rand.Rand
	gen      *melodyGen
	autoFill bool

	running bool
}

func NewSequencer(bpm int, kit *drumKit) *Sequencer {
	s := &Sequencer{
		harmony:  newHarmony(),
		gains:    [MusicSlots]float64{0.7, 0.5, 0.5},
		rng:      rand.New(rand.NewPCG(1, seqStream)),
		gen:      newMelodyGen(), // pattern registered in InitDefaultPatterns before mixer exists
		volume:   1.0,
		autoFill: true,
	}
	for i := range s.slots {
		s.slots[i].cur = NewPatternPlayer(kit)
		s.slots[i].nxt = NewPatternPlayer(kit)
	}
	s.SetBPM(bpm, false)
	return s
}

// Reseed re-keys the musical rng
// Determinism scope: same seed + same command schedule (bar-aligned) = same music;
// wall-clock command arrival relative to bars is not replayed
func (s *Sequencer) Reseed(seed int64) {
	s.rng = rand.New(rand.NewPCG(uint64(seed), seqStream))
}

func (s *Sequencer) SetBPM(bpm int, quantize bool) {
	if bpm < MinBPM {
		bpm = MinBPM
	} else if bpm > MaxBPM {
		bpm = MaxBPM
	}
	// bar-quantized tempo application removes mid-bar step-grid lurch
	if quantize && s.running {
		s.pendingBPM = bpm
		return
	}
	s.bpm = bpm
	s.samplesPerStep = SamplesPerStep(bpm)
}

func (s *Sequencer) applyPendingBPM() {
	if s.pendingBPM > 0 {
		s.bpm = s.pendingBPM
		s.samplesPerStep = SamplesPerStep(s.bpm)
		s.pendingBPM = 0
	}
}

func (s *Sequencer) SetSwing(a float64) {
	// literal 0.5 → MaxSwing
	if a < 0 {
		a = 0
	} else if a > MaxSwing {
		a = MaxSwing
	}
	s.swing = a
}

func (s *Sequencer) SetVolume(v float64) {
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	s.volume = v
}

// Generate fills buffer with mixed layer output (additive)
func (s *Sequencer) Generate(buf []float64) {
	if !s.running {
		return
	}
	for i := range buf {
		// live samplesPerStep read — pending BPM applies mid-buffer at bars
		spS := int64(s.samplesPerStep)
		effectiveStepLen := spS
		if s.swing > 0 {
			so := int64(float64(spS) * s.swing)
			if s.currentStep%2 == 0 {
				effectiveStepLen += so
			} else {
				effectiveStepLen -= so
			}
		}

		if s.samplePos >= effectiveStepLen {
			s.samplePos = 0
			s.currentStep = (s.currentStep + 1) % int64(MaxPatternLen)

			if s.currentStep%int64(StepsPerBar) == 0 {
				s.barCount++
				s.harmony.advanceBar()
				s.applyPendingBPM()
				s.applyPendingTransitions()
				s.updateReveal()
				s.updateFill()
				s.updateMelodyGen()
			}
			s.triggerStep(int(s.currentStep))
		}

		var mix float64
		for si := range s.slots {
			mix += s.slots[si].sample() * s.gains[si]
		}
		if s.startGain < 1 {
			s.startGain += s.startInc
			if s.startGain > 1 {
				s.startGain = 1
			}
		}
		buf[i] += mix * s.volume * s.startGain
		s.samplePos++
	}
}

func (s *Sequencer) triggerStep(step int) {
	for si := range s.slots {
		s.slots[si].player().TriggerStep(step, s.samplesPerStep, s.harmony, s.rng)
	}
}

// startTransition begins an equal-power fade; returns false when it snapped
// A silent source has no tail to declick — immediate full-gain entry fixes
// attenuated first bars at music start
func (s *Sequencer) startTransition(slot int, id PatternID, fade int) bool {
	ss := &s.slots[slot]
	ss.snap()
	if ss.cur.patternData == nil { // silent-source snap
		ss.cur.SetPattern(id)
		ss.curID = id
		return false
	}
	if fade < MinCrossfadeSamples {
		fade = MinCrossfadeSamples // declick floor: never hard-cut voice tails
	}
	ss.nxt.SetPattern(id)
	ss.nxtID = id
	ss.xPos, ss.xLen = 0, fade
	ss.fading = true
	return true
}

func (s *Sequencer) applyPendingTransitions() {
	for si := range s.slots {
		ss := &s.slots[si]
		p := ss.pending
		if p == nil || (p.targetBar >= 0 && p.targetBar > s.barCount) {
			continue
		}
		faded := s.startTransition(si, p.pattern, p.crossfadeSamples)
		ss.pending = nil
		// reveal only when a fade actually started
		if faded && p.crossfadeSamples >= SamplesPerBar(s.bpm) {
			if pat := GetPattern(p.pattern); pat != nil && len(pat.Tracks) > 1 {
				ss.revealN = 1
				ss.nxt.SetMask(1)
			}
		}
	}
}

func (s *Sequencer) updateReveal() {
	for si := range s.slots {
		ss := &s.slots[si]
		if ss.revealN == 0 {
			continue
		}
		pat := GetPattern(ss.activeID())
		if pat == nil {
			ss.revealN = 0
			continue
		}
		ss.revealN++
		if ss.revealN >= len(pat.Tracks) {
			ss.revealN = 0
			ss.player().SetMask(^uint32(0))
		} else {
			ss.player().SetMask((1 << uint(ss.revealN)) - 1)
		}
	}
}

// updateFill runs the slot-2 auto-fill: fill bar on the last bar of each phrase,
// previous pattern restored on the downbeat; skipped while slot 2 is user-driven
func (s *Sequencer) updateFill() {
	if !s.autoFill || len(fillIDs) == 0 {
		return
	}
	ss := &s.slots[2]
	if ss.pending != nil || ss.revealN > 0 {
		return
	}
	switch s.barCount % FillEveryBars {
	case FillEveryBars - 1:
		if !ss.inFill {
			ss.fillSavedID = ss.activeID()
			ss.inFill = true
			s.startTransition(2, fillIDs[s.rng.IntN(len(fillIDs))], MinCrossfadeSamples)
		}
	case 0:
		if ss.inFill {
			ss.inFill = false
			s.startTransition(2, ss.fillSavedID, MinCrossfadeSamples)
		}
	}
}

// updateMelodyGen regenerates the generative lead once per bar while active
// Unquantized starts play bass-only until the next bar seeds the lead
func (s *Sequencer) updateMelodyGen() {
	if s.slots[1].activeID() == PatternMelodyGen {
		s.gen.regenerate(int(s.barCount%PhraseBars), s.harmony, s.rng)
	}
}

func (s *Sequencer) SetPattern(slot int, p PatternID, crossfadeSamples int, quantize bool) {
	if slot < 0 || slot >= MusicSlots {
		return
	}
	ss := &s.slots[slot]
	ss.revealN = 0
	if !quantize {
		ss.pending = nil
		s.startTransition(slot, p, crossfadeSamples)
		return
	}
	ss.pending = &patternTransition{pattern: p, targetBar: s.barCount + 1, crossfadeSamples: crossfadeSamples}
}

func (s *Sequencer) SetMask(slot int, mask uint32) {
	if slot >= 0 && slot < MusicSlots {
		s.slots[slot].revealN = 0 // external control overrides reveal automation
		s.slots[slot].player().SetMask(mask)
	}
}

func (s *Sequencer) SetHarmonyCfg(root int, scale ScaleID, prog []int) {
	s.harmony.set(root, scale, prog)
}

// TriggerNote routes external MIDI note requests to the melody slot pool
func (s *Sequencer) TriggerNote(note int, velocity float64, durationSamples int, instr InstrumentType) {
	s.slots[1].player().TriggerNoteMIDI(note, velocity, durationSamples, instr)
}

func (s *Sequencer) Start() {
	if !s.running {
		s.running = true
		s.triggerStep(0)
		// master fade-in — patterns enter at full internal gain (no
		// equal-power-from-silence attenuation), the bus rides up under them
		s.startGain = 0
		s.startInc = 1.0 / (MusicStartFadeIn.Seconds() * float64(AudioSampleRate))
	}
}

func (s *Sequencer) Stop()           { s.running = false }
func (s *Sequencer) IsRunning() bool { return s.running }

func (s *Sequencer) Reset() {
	s.running = false
	s.currentStep, s.samplePos, s.barCount = 0, 0, 0
	s.pendingBPM = 0
	s.harmony.reset()
	for si := range s.slots {
		ss := &s.slots[si]
		ss.cur.Reset()
		ss.nxt.Reset()
		ss.curID, ss.nxtID = PatternSilence, PatternSilence
		ss.pending = nil
		ss.fading = false
		ss.xPos, ss.xLen = 0, 0
		ss.revealN = 0
		ss.inFill = false
	}
}

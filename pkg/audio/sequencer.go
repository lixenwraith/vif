package audio

import (
	"math/rand/v2"
	"sync/atomic"
)

type patternTransition struct {
	pattern          PatternID
	targetBar        int64
	crossfadeSamples int
	reveal           bool // per-bar track build-up on arrival
}

// slotState owns the A/B PatternPlayer pair for one layer
// During a fade: nxt receives step triggers at full level; cur stops receiving
// triggers and its active voices ring out under a falling gain
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
	drawn       bool // the tier's pool chose this slot's pattern, so a phrase may vary it
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

// sample renders a transition: the incoming pattern at full level from its first
// trigger, over the outgoing one's tails fading out. The outgoing pattern stops
// triggering when the transition starts, so a fade-in would only have swallowed
// the incoming downbeat.
func (ss *slotState) sample() float64 {
	if !ss.fading {
		return ss.cur.Sample()
	}
	t := float64(ss.xPos) / float64(ss.xLen)
	out := ss.cur.Sample()*(1-t) + ss.nxt.Sample()
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
	pendingBPM     int // applied at next beat boundary; 0 = none
	samplesPerStep int
	swing          float64
	volume         float64
	startGain      float64
	startInc       float64

	currentStep int64
	samplePos   int64
	barCount    int64

	harmony *harmony
	slots   [MusicSlots]slotState
	gains   [MusicSlots]float64
	rng     *rand.Rand
	gen     *melodyGen
	// pos is the transport readout: bar<<8 | step. Written once per step by
	// Generate on the mixer goroutine. Relaxed on purpose — a reader that
	// catches it mid-store is one 16th note stale, below the refresh rate of
	// anything that would ask.
	pos      atomic.Uint64
	slotPat  [MusicSlots]atomic.Int32
	autoFill bool

	// The drawn arrangement, set at engine Start; the group the tier draws from and the
	// phrases it has held, with its index published for telemetry
	arr          arrangement
	group        int
	groupPhrases int
	groupPub     atomic.Int32
	tier         Intensity

	running bool
}

func NewSequencer(bpm int, kit *drumKit) *Sequencer {
	s := &Sequencer{
		harmony:  newHarmony(),
		gains:    [MusicSlots]float64{0.7, 0.5, 0.5},
		rng:      rand.New(rand.NewPCG(1, seqStream)),
		gen:      newMelodyGen(), // pattern registered at engine Start before the mixer exists
		volume:   1.0,
		autoFill: true,
	}
	for i := range s.slots {
		s.slots[i].cur = NewPatternPlayer(kit)
		s.slots[i].nxt = NewPatternPlayer(kit)
	}
	s.setGroup(-1)
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
	// beat-quantized: a tempo change never splits a beat, and a slewed ramp moves
	// in steps of a beat rather than lurching once a bar
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
		// live samplesPerStep read — pending BPM applies mid-buffer at beats
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

			if s.currentStep%int64(StepsPerBeat) == 0 {
				s.applyPendingBPM()
			}
			if s.currentStep%int64(StepsPerBar) == 0 {
				s.barCount++
				s.harmony.advanceBar()
				// Reveals advance before transitions arm new ones, which then hold
				// their first bar as armed
				s.updateReveal()
				s.applyPendingTransitions()
				s.updateVariation()
				s.updateFill()
				s.updateMelodyGen()
			}
			s.triggerStep(int(s.currentStep))
			s.publish()
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
		ss.pending = nil
		s.beginTransition(si, p.pattern, p.crossfadeSamples, p.reveal)
	}
}

// beginTransition swaps a slot now. The outgoing pattern's reveal runs until here,
// so a quantized request never leaves it frozen part-built for the rest of its bar.
// Reveal is requested explicitly; inferring it from the crossfade made build-ups
// tempo-dependent.
func (s *Sequencer) beginTransition(slot int, id PatternID, fade int, reveal bool) {
	ss := &s.slots[slot]
	ss.revealN = 0
	if s.startTransition(slot, id, fade) && reveal {
		s.armReveal(ss, id)
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
// The fill comes from the group and tier drawing, or the first group before any has.
func (s *Sequencer) updateFill() {
	if !s.autoFill {
		return
	}
	ss := &s.slots[2]
	if ss.pending != nil || ss.revealN > 0 {
		return
	}
	switch s.barCount % FillEveryBars {
	case FillEveryBars - 1:
		fills := s.arr.pool(max(s.group, 0), s.tier, RoleFill)
		if !ss.inFill && len(fills) > 0 {
			ss.fillSavedID = ss.activeID()
			ss.inFill = true
			s.startTransition(2, fills[s.rng.IntN(len(fills))], s.beatFade())
		}
	case 0:
		if ss.inFill {
			ss.inFill = false
			s.startTransition(2, ss.fillSavedID, s.beatFade())
		}
	}
}

// updateVariation moves a held tier on at each phrase downbeat. After GroupPhrases
// in one group, both drawn slots move together to another group covering the tier,
// behind the fill that closed the phrase; otherwise one drawn slot, melody and rhythm
// in turn, swaps to another member of its pool, leaving one mid-transition alone.
func (s *Sequencer) updateVariation() {
	if s.barCount%PhraseBars != 0 || s.group < 0 {
		return
	}
	if s.groupPhrases++; s.groupPhrases >= GroupPhrases {
		if g := s.otherGroup(s.tier); g >= 0 {
			s.setGroup(g)
			for slot := range 2 {
				if s.slots[slot].drawn {
					s.setPattern(slot, s.draw(s.arr.pool(g, s.tier, Role(slot+1))), s.beatFade(), false, false)
				}
			}
			return
		}
	}
	slot := int(s.barCount/PhraseBars) % 2
	ss := &s.slots[slot]
	pool := s.arr.pool(s.group, s.tier, Role(slot+1))
	if !ss.drawn || len(pool) < 2 || ss.pending != nil || ss.fading || ss.revealN > 0 {
		return
	}
	// Uniform over the other members: the last stands in for a draw of the current
	i := s.rng.IntN(len(pool) - 1)
	if pool[i] == ss.activeID() {
		i = len(pool) - 1
	}
	s.startTransition(slot, pool[i], s.beatFade())
}

// beatFade lets the outgoing pattern's tails ring out over one beat, as a player
// would let a held chord go; the incoming one is at full level from its first hit
func (s *Sequencer) beatFade() int { return s.samplesPerStep * StepsPerBeat }

// otherGroup draws a group other than the current one that covers the tier; -1 if none
func (s *Sequencer) otherGroup(t Intensity) int {
	n := 0
	for g := range s.arr.groups {
		if g != s.group && s.arr.covers(g, t) {
			n++
		}
	}
	if n == 0 {
		return -1
	}
	k := s.rng.IntN(n)
	for g := range s.arr.groups {
		if g != s.group && s.arr.covers(g, t) {
			if k == 0 {
				return g
			}
			k--
		}
	}
	return -1
}

func (s *Sequencer) setGroup(g int) {
	s.group, s.groupPhrases = g, 0
	s.groupPub.Store(int32(g))
}

// updateMelodyGen regenerates the generative lead once per bar while active
// Unquantized starts play bass-only until the next bar seeds the lead
func (s *Sequencer) updateMelodyGen() {
	if s.slots[1].activeID() == PatternMelodyGen {
		s.gen.regenerate(int(s.barCount%PhraseBars), s.rng)
	}
}

// SetPattern places a pattern explicitly, which phrase variation then leaves alone
func (s *Sequencer) SetPattern(slot int, p PatternID, crossfadeSamples int, quantize bool) {
	if slot >= 0 && slot < MusicSlots {
		s.slots[slot].drawn = false
	}
	s.setPattern(slot, p, crossfadeSamples, quantize, false)
}

// setPattern schedules or applies a slot transition; reveal requests the
// per-bar track build-up on the incoming pattern
func (s *Sequencer) setPattern(slot int, p PatternID, fade int, quantize, reveal bool) {
	if slot < 0 || slot >= MusicSlots {
		return
	}
	ss := &s.slots[slot]
	if !quantize {
		ss.pending = nil
		s.beginTransition(slot, p, fade, reveal)
		s.publish()
		return
	}
	ss.pending = &patternTransition{
		pattern: p, targetBar: s.barCount + 1, crossfadeSamples: fade, reveal: reveal,
	}
}

// armReveal masks the incoming pattern to as many tracks as the outgoing one was
// sounding, at least one, and updateReveal admits one more per bar: a build-up only
// ever adds to what played before it. A silent-source snap has nothing to build from.
func (s *Sequencer) armReveal(ss *slotState, id PatternID) {
	pat := GetPattern(id)
	if pat == nil {
		return
	}
	if n := max(ss.cur.heard(), 1); n < len(pat.Tracks) {
		ss.revealN = n
		ss.nxt.SetMask(1<<n - 1)
	}
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
		s.publish()
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
	s.tier = IntensityCalm
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
		ss.drawn = false
	}
	s.setGroup(-1)
	s.publish()
}

// SetIntensity draws the tier's rhythm and melody from the current group's pools,
// moving to another group only when this one does not cover the tier. A slot already
// sounding its draw keeps playing, rather than restarting under a crossfade and a
// reveal that would strip it back to one track.
func (s *Sequencer) SetIntensity(t Intensity, crossfadeSamples int, quantize, reveal bool) {
	if t < 0 || t >= IntensityCount {
		return
	}
	s.tier = t
	if !s.arr.covers(s.group, t) {
		if g := s.otherGroup(t); g >= 0 {
			s.setGroup(g)
		}
	}
	for slot := range 2 {
		ss := &s.slots[slot]
		ss.drawn = true
		if id := s.draw(s.arr.pool(s.group, t, Role(slot+1))); id != ss.activeID() {
			s.setPattern(slot, id, crossfadeSamples, quantize, reveal)
		} else {
			ss.pending = nil
		}
	}
}

// draw picks one pool member; a single member costs no rng, so a fixed
// arrangement plays the same music for a given seed as it always has
func (s *Sequencer) draw(pool []PatternID) PatternID {
	switch len(pool) {
	case 0:
		return PatternSilence
	case 1:
		return pool[0]
	}
	return pool[s.rng.IntN(len(pool))]
}

// ReloadPattern re-resolves any slot pointing at id after the registry replaced
// the pattern. Mixer-goroutine confined.
func (s *Sequencer) ReloadPattern(id PatternID) {
	for si := range s.slots {
		ss := &s.slots[si]
		if ss.curID == id {
			ss.cur.refresh(id)
		}
		if ss.nxtID == id {
			ss.nxt.refresh(id)
		}
	}
	// melodyGen writes into the registered Pattern's lead track in place, so a
	// replacement leaves it holding the superseded struct.
	if id == PatternMelodyGen && s.gen != nil {
		s.gen.pat = GetPattern(id)
	}
}

// publish exports the playhead and the per-slot sounding pattern. Three
// separate atomics: a reader can pair a step from before a transition with a
// pattern from after it. That is skew in a readout, not a property anything
// depends on.
func (s *Sequencer) publish() {
	s.pos.Store(uint64(s.barCount)<<8 | uint64(s.currentStep)&0xff)
	for si := range s.slots {
		s.slotPat[si].Store(int32(s.slots[si].activeID()))
	}
}

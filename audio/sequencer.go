package audio

import (
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// Sequencer maintains tempo and generates continuous music output
type Sequencer struct {
	bpm            atomic.Int32
	samplesPerStep atomic.Int64
	swing          atomic.Int64 // Q16.16 fixed point, 0.0-0.5

	currentStep atomic.Int64
	samplePos   atomic.Int64 // Position within current step
	barCount    atomic.Int64 // Bars elapsed (for quantized transitions)

	beatTrack   *BeatTrack
	melodyTrack *MelodyTrack

	// Transition state
	transitionMu  sync.Mutex
	pendingBeat   *patternTransition
	pendingMelody *melodyTransition

	running atomic.Bool
	volume  atomic.Int64 // Q16.16 fixed point, 0.0-1.0
}

type patternTransition struct {
	pattern          core.PatternID
	targetBar        int64 // -1 = immediate
	crossfadeSamples int
}

type melodyTransition struct {
	pattern          core.PatternID
	rootNote         int
	targetBar        int64
	crossfadeSamples int
}

// NewSequencer creates a sequencer at given BPM
func NewSequencer(bpm int) *Sequencer {
	s := &Sequencer{
		beatTrack:   NewBeatTrack(),
		melodyTrack: NewMelodyTrack(),
	}
	s.SetBPM(bpm)
	s.volume.Store(1 << 16) // 1.0 in Q16.16
	return s
}

// SetBPM updates tempo
func (s *Sequencer) SetBPM(bpm int) {
	if bpm < parameter.MinBPM {
		bpm = parameter.MinBPM
	} else if bpm > parameter.MaxBPM {
		bpm = parameter.MaxBPM
	}
	s.bpm.Store(int32(bpm))
	s.samplesPerStep.Store(int64(parameter.SamplesPerStep(bpm)))
}

// SetSwing sets shuffle amount (0.0 = straight, 0.5 = max shuffle)
func (s *Sequencer) SetSwing(amount float64) {
	if amount < 0 {
		amount = 0
	} else if amount > 0.5 {
		amount = 0.5
	}
	s.swing.Store(int64(amount * (1 << 16)))
}

// SetVolume sets music volume (0.0-1.0)
func (s *Sequencer) SetVolume(vol float64) {
	if vol < 0 {
		vol = 0
	} else if vol > 1 {
		vol = 1
	}
	s.volume.Store(int64(vol * (1 << 16)))
}

// Generate fills buffer with mixed beat+melody samples
func (s *Sequencer) Generate(buf []float64) {
	if !s.running.Load() {
		return
	}

	samplesPerStep := s.samplesPerStep.Load()
	swingQ16 := s.swing.Load()
	volQ16 := s.volume.Load()
	vol := float64(volQ16) / float64(1<<16)

	step := s.currentStep.Load()
	pos := s.samplePos.Load()

	for i := range buf {
		// Calculate effective step length with swing
		// Swing affects even steps: longer even, shorter odd
		effectiveStepLen := samplesPerStep
		if swingQ16 > 0 && step%2 == 0 {
			swingOffset := (samplesPerStep * swingQ16) >> 16
			effectiveStepLen += swingOffset
		} else if swingQ16 > 0 {
			swingOffset := (samplesPerStep * swingQ16) >> 16
			effectiveStepLen -= swingOffset
		}

		// Check step boundary
		if pos >= effectiveStepLen {
			pos = 0
			step = (step + 1) % int64(parameter.MaxPatternLen)
			s.currentStep.Store(step)

			// Bar boundary check
			if step%int64(parameter.StepsPerBar) == 0 {
				s.barCount.Add(1)
				s.checkPendingTransitions()
			}

			// Trigger instruments at step boundary
			s.beatTrack.TriggerStep(int(step))
			s.melodyTrack.TriggerStep(int(step))
		}

		// Generate and mix tracks
		sample := s.beatTrack.Sample() + s.melodyTrack.Sample()
		buf[i] += sample * vol

		pos++
	}
	s.samplePos.Store(pos)
}

func (s *Sequencer) checkPendingTransitions() {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	bar := s.barCount.Load()

	if s.pendingBeat != nil && (s.pendingBeat.targetBar < 0 || s.pendingBeat.targetBar <= bar) {
		s.beatTrack.SetPattern(s.pendingBeat.pattern, s.pendingBeat.crossfadeSamples)
		s.pendingBeat = nil
	}

	if s.pendingMelody != nil && (s.pendingMelody.targetBar < 0 || s.pendingMelody.targetBar <= bar) {
		s.melodyTrack.SetPattern(s.pendingMelody.pattern, s.pendingMelody.rootNote, s.pendingMelody.crossfadeSamples)
		s.pendingMelody = nil
	}
}

// SetBeatPattern queues beat pattern change
// quantize: wait for next bar boundary
func (s *Sequencer) SetBeatPattern(p core.PatternID, crossfadeSamples int, quantize bool) {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	targetBar := int64(-1)
	if quantize {
		targetBar = s.barCount.Load() + 1
	}

	s.pendingBeat = &patternTransition{
		pattern:          p,
		targetBar:        targetBar,
		crossfadeSamples: crossfadeSamples,
	}

	if !quantize {
		s.beatTrack.SetPattern(p, crossfadeSamples)
		s.pendingBeat = nil
	}
}

// SetMelodyPattern queues melody pattern change
func (s *Sequencer) SetMelodyPattern(p core.PatternID, root int, crossfadeSamples int, quantize bool) {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	targetBar := int64(-1)
	if quantize {
		targetBar = s.barCount.Load() + 1
	}

	s.pendingMelody = &melodyTransition{
		pattern:          p,
		rootNote:         root,
		targetBar:        targetBar,
		crossfadeSamples: crossfadeSamples,
	}

	if !quantize {
		s.melodyTrack.SetPattern(p, root, crossfadeSamples)
		s.pendingMelody = nil
	}
}

// TriggerNote triggers immediate note on melody track
func (s *Sequencer) TriggerNote(note int, velocity float64, durationSamples int, instr core.InstrumentType) {
	s.melodyTrack.TriggerNote(note, velocity, durationSamples, instr)
}

// Start begins sequencer
func (s *Sequencer) Start() {
	if s.running.CompareAndSwap(false, true) {
		// Trigger step 0 on start to avoid skipping first beat
		s.beatTrack.TriggerStep(0)
		s.melodyTrack.TriggerStep(0)
	}
}

// Stop halts sequencer
func (s *Sequencer) Stop() {
	s.running.Store(false)
}

// IsRunning returns sequencer state
func (s *Sequencer) IsRunning() bool {
	return s.running.Load()
}

// Reset clears all state
func (s *Sequencer) Reset() {
	s.running.Store(false)
	s.currentStep.Store(0)
	s.samplePos.Store(0)
	s.barCount.Store(0)
	s.beatTrack.Reset()
	s.melodyTrack.Reset()

	s.transitionMu.Lock()
	s.pendingBeat = nil
	s.pendingMelody = nil
	s.transitionMu.Unlock()
}
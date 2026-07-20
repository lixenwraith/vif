package audio

import (
	"math/rand"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

type patternTransition struct {
	pattern          core.PatternID
	targetBar        int64
	crossfadeSamples int
}

type slotState struct {
	player  *PatternPlayer
	pending *patternTransition
}

// Sequencer maintains tempo, harmony, and N pattern layers
// Mixer-goroutine confined: no synchronization
type Sequencer struct {
	bpm            int
	samplesPerStep int
	swing          float64
	volume         float64

	currentStep int64
	samplePos   int64
	barCount    int64

	harmony *harmony
	slots   [parameter.MusicSlots]slotState
	gains   [parameter.MusicSlots]float64
	rng     *rand.Rand

	running bool
}

func NewSequencer(bpm int, kit *drumKit) *Sequencer {
	s := &Sequencer{
		harmony: newHarmony(),
		gains:   [parameter.MusicSlots]float64{0.7, 0.5, 0.5},
		rng:     rand.New(rand.NewSource(1)), // fixed seed; run-seed wiring in P3
		volume:  1.0,
	}
	for i := range s.slots {
		s.slots[i].player = NewPatternPlayer(kit)
	}
	s.SetBPM(bpm)
	return s
}

func (s *Sequencer) SetBPM(bpm int) {
	if bpm < parameter.MinBPM {
		bpm = parameter.MinBPM
	} else if bpm > parameter.MaxBPM {
		bpm = parameter.MaxBPM
	}
	s.bpm = bpm
	s.samplesPerStep = parameter.SamplesPerStep(bpm)
}

func (s *Sequencer) SetSwing(a float64) {
	if a < 0 {
		a = 0
	} else if a > 0.5 {
		a = 0.5
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
	spS := int64(s.samplesPerStep)
	swingOffset := int64(float64(spS) * s.swing)

	for i := range buf {
		effectiveStepLen := spS
		if swingOffset > 0 {
			if s.currentStep%2 == 0 {
				effectiveStepLen += swingOffset
			} else {
				effectiveStepLen -= swingOffset
			}
		}

		if s.samplePos >= effectiveStepLen {
			s.samplePos = 0
			s.currentStep = (s.currentStep + 1) % int64(parameter.MaxPatternLen)

			if s.currentStep%int64(parameter.StepsPerBar) == 0 {
				s.barCount++
				s.harmony.advanceBar()
				s.applyPendingTransitions()
			}
			s.triggerStep(int(s.currentStep))
		}

		var mix float64
		for si := range s.slots {
			mix += s.slots[si].player.Sample() * s.gains[si]
		}
		buf[i] += mix * s.volume
		s.samplePos++
	}
}

func (s *Sequencer) triggerStep(step int) {
	for si := range s.slots {
		s.slots[si].player.TriggerStep(step, s.samplesPerStep, s.harmony, s.rng)
	}
}

func (s *Sequencer) applyPendingTransitions() {
	for si := range s.slots {
		p := s.slots[si].pending
		if p != nil && (p.targetBar < 0 || p.targetBar <= s.barCount) {
			s.slots[si].player.SetPattern(p.pattern, p.crossfadeSamples)
			s.slots[si].pending = nil
		}
	}
}

func (s *Sequencer) SetPattern(slot int, p core.PatternID, crossfadeSamples int, quantize bool) {
	if slot < 0 || slot >= parameter.MusicSlots {
		return
	}
	if !quantize {
		s.slots[slot].player.SetPattern(p, crossfadeSamples)
		s.slots[slot].pending = nil
		return
	}
	s.slots[slot].pending = &patternTransition{pattern: p, targetBar: s.barCount + 1, crossfadeSamples: crossfadeSamples}
}

func (s *Sequencer) SetMask(slot int, mask uint32) {
	if slot >= 0 && slot < parameter.MusicSlots {
		s.slots[slot].player.SetMask(mask)
	}
}

func (s *Sequencer) SetHarmonyCfg(root int, scale core.ScaleID, prog []int) {
	s.harmony.set(root, scale, prog)
}

// TriggerNote routes external MIDI note requests to the melody slot pool
func (s *Sequencer) TriggerNote(note int, velocity float64, durationSamples int, instr core.InstrumentType) {
	s.slots[1].player.TriggerNoteMIDI(note, velocity, durationSamples, instr)
}

func (s *Sequencer) Start() {
	if !s.running {
		s.running = true
		s.triggerStep(0)
	}
}

func (s *Sequencer) Stop()           { s.running = false }
func (s *Sequencer) IsRunning() bool { return s.running }

func (s *Sequencer) Reset() {
	s.running = false
	s.currentStep, s.samplePos, s.barCount = 0, 0, 0
	s.harmony.reset()
	for si := range s.slots {
		s.slots[si].player.Reset()
		s.slots[si].pending = nil
	}
}

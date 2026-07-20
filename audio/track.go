package audio

import (
	"math/rand"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// PatternPlayer plays one Pattern layer; mixer-goroutine confined
// Owns its drum voices and tonal pool: no cross-slot stealing
type PatternPlayer struct {
	patternData *Pattern
	mask        uint32 // bit i enables track i

	drums         [core.InstrumentCount]*DrumVoice
	tonal         [parameter.MaxPolyphony]*TonalVoice
	stealStrategy core.VoiceStealStrategy

	fadePos int
	fadeLen int
}

func NewPatternPlayer(kit *drumKit) *PatternPlayer {
	p := &PatternPlayer{mask: ^uint32(0), stealStrategy: core.StealOldest}
	for i := core.InstrumentType(0); i <= core.InstrClap; i++ {
		p.drums[i] = NewDrumVoice(kit.variants[i])
	}
	for i := range p.tonal {
		p.tonal[i] = NewTonalVoice()
	}
	return p
}

// TriggerStep fires all events at the step position
func (p *PatternPlayer) TriggerStep(step, samplesPerStep int, h *harmony, rng *rand.Rand) {
	pat := p.patternData
	if pat == nil || pat.Steps <= 0 {
		return
	}
	local := step % pat.Steps

	for ti := range pat.Tracks {
		if p.mask&(1<<uint(ti)) == 0 {
			continue
		}
		tr := &pat.Tracks[ti]
		for _, ev := range tr.Events {
			if ev.Pos != local {
				continue
			}
			if ev.Prob > 0 && ev.Prob < 1 && rng.Float64() > ev.Prob {
				continue
			}
			if tr.Instr.IsDrum() {
				p.drums[tr.Instr].Trigger(VoiceParams{Velocity: ev.Vel})
				continue
			}
			dur := ev.Dur
			if dur <= 0 {
				dur = 1
			}
			midi := h.resolve(ev.Deg, ev.Oct, tr.FollowChord)
			p.TriggerNoteMIDI(midi, ev.Vel, dur*samplesPerStep, tr.Instr)
		}
	}
}

// TriggerNoteMIDI triggers a tonal note directly (pattern steps and external requests)
func (p *PatternPlayer) TriggerNoteMIDI(midi int, vel float64, durSamples int, instr core.InstrumentType) {
	v := p.allocateVoice(midi)
	if v == nil {
		return
	}
	v.Trigger(VoiceParams{Note: midi, Velocity: vel, Duration: durSamples, Instrument: instr})
}

func (p *PatternPlayer) allocateVoice(note int) *TonalVoice {
	for _, v := range p.tonal {
		if !v.Active() {
			return v
		}
	}
	switch p.stealStrategy {
	case core.StealOldest:
		var oldest *TonalVoice
		lowest := 2.0
		for _, v := range p.tonal {
			if v.EnvLevel() < lowest {
				lowest = v.EnvLevel()
				oldest = v
			}
		}
		return oldest
	case core.StealQuietest:
		var quietest *TonalVoice
		lowest := 2.0
		for _, v := range p.tonal {
			vol := v.EnvLevel() * v.velocity
			if vol < lowest {
				lowest = vol
				quietest = v
			}
		}
		return quietest
	case core.StealSameNote:
		for _, v := range p.tonal {
			if v.Note() == note {
				return v
			}
		}
		return nil
	default:
		return nil
	}
}

// Sample returns the mixed layer output with fade-in applied
func (p *PatternPlayer) Sample() float64 {
	var sum float64
	for i := core.InstrumentType(0); i <= core.InstrClap; i++ {
		sum += p.drums[i].Sample()
	}
	for _, v := range p.tonal {
		if v.Active() {
			sum += v.Sample()
		}
	}
	if p.fadeLen > 0 && p.fadePos < p.fadeLen {
		sum *= float64(p.fadePos) / float64(p.fadeLen)
		p.fadePos++
	}
	return sum
}

func (p *PatternPlayer) SetPattern(id core.PatternID, crossfadeSamples int) {
	p.patternData = GetPattern(id)
	p.mask = ^uint32(0)
	if crossfadeSamples > 0 {
		p.fadePos, p.fadeLen = 0, crossfadeSamples
	}
}

func (p *PatternPlayer) SetMask(mask uint32) { p.mask = mask }

func (p *PatternPlayer) Reset() {
	p.patternData = nil
	p.mask = ^uint32(0)
	p.fadePos, p.fadeLen = 0, 0
	for i := core.InstrumentType(0); i <= core.InstrClap; i++ {
		p.drums[i].Reset()
	}
	for _, v := range p.tonal {
		v.Reset()
	}
}

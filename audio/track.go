package audio

import (
	"math/rand/v2"
)

// pendingTrig is a humanization-delayed event awaiting its fire sample
type pendingTrig struct {
	wait  int
	midi  int
	vel   float64
	dur   int
	instr InstrumentType
}

// PatternPlayer plays one Pattern layer; mixer-goroutine confined
// Crossfading between patterns is owned by the sequencer's slot A/B pair;
// the per-player fade-in was removed — it hard-cut voice tails on SetPattern
type PatternPlayer struct {
	patternData *Pattern
	mask        uint32
	serial      uint64 // monotonic trigger counter for voice age

	// 2-voice round-robin per drum — retrigger rings out on the sibling
	drums  [InstrumentCount][2]*DrumVoice
	drumRR [InstrumentCount]uint8

	tonal         [MaxPolyphony]*TonalVoice
	stealStrategy VoiceStealStrategy

	// fixed-size micro-timing scheduler; allocation-free
	pend  [MaxPendingTrigs]pendingTrig
	pendN int
}

func NewPatternPlayer(kit *drumKit) *PatternPlayer {
	p := &PatternPlayer{mask: ^uint32(0), stealStrategy: StealLowest}
	for i := InstrumentType(0); i <= InstrClap; i++ {
		p.drums[i][0] = NewDrumVoice(kit.variants[i])
		p.drums[i][1] = NewDrumVoice(kit.variants[i])
	}
	for i := range p.tonal {
		p.tonal[i] = NewTonalVoice()
	}
	return p
}

// TriggerStep fires all events at the step position, applying humanization
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

			vel := ev.Vel
			delay := 0
			if tr.Humanize > 0 {
				vel += (rng.Float64()*2 - 1) * HumanizeVelJitter * tr.Humanize
				vel = min(max(vel, 0.05), 1.0)
				if tr.Instr != InstrKick { // kick anchors the grid: no lag
					delay = rng.IntN(int(tr.Humanize*HumanizeMaxDelaySamples) + 1)
				}
			}

			if tr.Instr.IsDrum() {
				if delay > 0 {
					p.schedule(0, vel, 0, tr.Instr, delay)
				} else {
					p.triggerDrum(tr.Instr, vel)
				}
				continue
			}
			dur := ev.Dur
			if dur <= 0 {
				dur = 1
			}
			midi := h.resolve(ev.Deg, ev.Oct, tr.FollowChord)
			if delay > 0 {
				p.schedule(midi, vel, dur*samplesPerStep, tr.Instr, delay)
			} else {
				p.TriggerNoteMIDI(midi, vel, dur*samplesPerStep, tr.Instr)
			}
		}
	}
}

func (p *PatternPlayer) schedule(midi int, vel float64, dur int, instr InstrumentType, wait int) {
	if p.pendN >= len(p.pend) {
		p.fire(midi, vel, dur, instr) // ring full: fire on-grid rather than drop
		return
	}
	p.pend[p.pendN] = pendingTrig{wait: wait, midi: midi, vel: vel, dur: dur, instr: instr}
	p.pendN++
}

func (p *PatternPlayer) fire(midi int, vel float64, dur int, instr InstrumentType) {
	if instr.IsDrum() {
		p.triggerDrum(instr, vel)
		return
	}
	p.TriggerNoteMIDI(midi, vel, dur, instr)
}

func (p *PatternPlayer) triggerDrum(instr InstrumentType, vel float64) {
	idx := p.drumRR[instr] & 1
	// Prefer an idle sibling so the ringing hit completes (declick)
	if p.drums[instr][idx].Active() && !p.drums[instr][idx^1].Active() {
		idx ^= 1
	}
	p.drums[instr][idx].Trigger(VoiceParams{Velocity: vel})
	p.drumRR[instr] = (idx + 1) & 1
}

// TriggerNoteMIDI triggers a tonal note directly (pattern steps and external requests)
func (p *PatternPlayer) TriggerNoteMIDI(midi int, vel float64, durSamples int, instr InstrumentType) {
	v := p.allocateVoice(midi)
	if v == nil {
		return
	}
	v.Trigger(VoiceParams{Note: midi, Velocity: vel, Duration: durSamples, Instrument: instr})
	p.serial++
	v.serial = p.serial // age stamp, survives Trigger
}

func (p *PatternPlayer) allocateVoice(note int) *TonalVoice {
	for _, v := range p.tonal {
		if !v.Active() {
			return v
		}
	}
	switch p.stealStrategy {
	case StealSameNote:
		for _, v := range p.tonal {
			if v.Note() == note {
				return v
			}
		}
		return nil

	case StealQuietest:
		var victim *TonalVoice
		lowest := 2.0
		for _, v := range p.tonal {
			w := v.EnvLevel() * v.velocity
			if v.releasing {
				w *= 0.25 // tails are the cheapest to lose
			}
			if w < lowest {
				lowest, victim = w, v
			}
		}
		return victim

	case StealLowest:
		// NOTE: oldest or lowest envelope level?
		var victim *TonalVoice
		oldest := ^uint64(0)
		for _, v := range p.tonal {
			if v.releasing && v.serial < oldest {
				oldest, victim = v.serial, v
			}
		}
		if victim != nil {
			return victim
		}
		for _, v := range p.tonal {
			if v.serial < oldest {
				oldest, victim = v.serial, v
			}
		}
		return victim

	default:
		return nil
	}
}

// Sample advances the micro-timing scheduler and returns the mixed layer output
func (p *PatternPlayer) Sample() float64 {
	for i := 0; i < p.pendN; {
		p.pend[i].wait--
		if p.pend[i].wait <= 0 {
			t := p.pend[i]
			p.pendN--
			p.pend[i] = p.pend[p.pendN]
			p.fire(t.midi, t.vel, t.dur, t.instr)
			continue
		}
		i++
	}

	var sum float64
	for i := InstrumentType(0); i <= InstrClap; i++ {
		sum += p.drums[i][0].Sample() + p.drums[i][1].Sample()
	}
	for _, v := range p.tonal {
		if v.Active() {
			sum += v.Sample()
		}
	}
	return sum
}

// SetPattern swaps pattern data; gain handling lives in the sequencer slot
func (p *PatternPlayer) SetPattern(id PatternID) {
	p.patternData = GetPattern(id)
	p.mask = ^uint32(0)
	p.pendN = 0
}

func (p *PatternPlayer) SetMask(mask uint32) { p.mask = mask }

func (p *PatternPlayer) Reset() {
	p.patternData = nil
	p.mask = ^uint32(0)
	p.pendN = 0
	for i := InstrumentType(0); i <= InstrClap; i++ {
		p.drums[i][0].Reset()
		p.drums[i][1].Reset()
	}
	for _, v := range p.tonal {
		v.Reset()
	}
}

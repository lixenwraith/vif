//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/lixenwraith/vif/pkg/audio"
	"github.com/lixenwraith/vif/pkg/genetic"
)

// tuneOptions sizes a search: the loops rendered and their bars, the generations
// run over a pool of candidates, the seed every draw follows, whether the loops
// are augmented, a room, a mix and random voices behind each, and whether parts
// are generated, beats, melodies and bass lines the bank never had
type tuneOptions struct {
	loops, bars, generations, pool int
	seed                           uint64
	aug, gen                       bool
	nowav                          bool           // an export keeps the frames and the labels only
	codec                          bool           // an export sends a third of its loops through a lossy codec and back
	tempos                         []int          // the tempos the loops cycle through; tuneTempos when nil
	part, parts                    int            // an export renders the loops i%parts == part; all when parts is 0
	skip                           func(int) bool // the loops drawn but not rendered
}

// tuneLoop is one rendered loop, what made it, the rip a perfect listener makes
// of it, and the voices its tonal tracks were played with
type tuneLoop struct {
	audio  []float64
	want   *audio.Rip
	slots  [audio.MusicSlots]audio.PatternID
	bpm    int
	root   int
	scale  audio.ScaleID
	voices map[string]*audio.VoiceDef
	tuning float64 // cents off A440 the loop was rendered at
}

// tempos spread the corpus over the sequencer's range
var tuneTempos = []int{96, 112, 128, 140, 156, 176}

func cmdTune(s *Session, a []string) error {
	o := tuneOptions{loops: 12, bars: 4, generations: 8, pool: 16, seed: 1}
	if len(a) > 0 {
		n, err := strconv.Atoi(a[0])
		if err != nil || n < 1 || n > 200 {
			return fmt.Errorf("generations %q: want 1 to 200", a[0])
		}
		o.generations = n
	}
	file := ""
	if len(a) > 1 {
		file = s.path(a[1])
	}
	return s.tune(o, file)
}

// corpus renders loops of the registered rhythm, melody and bass patterns, drawn
// by the seed over the tempos and the keys, each with its expected rip, all held
// at once: what a search over them wants
func (s *Session) corpus(o tuneOptions) ([]tuneLoop, error) {
	loops := make([]tuneLoop, 0, o.loops)
	err := s.eachLoop(o, func(_ int, l tuneLoop) error {
		loops = append(loops, l)
		return nil
	})
	return loops, err
}

// eachLoop renders the corpus's loops one at a time to visit, so an export of
// thousands holds one in memory
func (s *Session) eachLoop(o tuneOptions, visit func(int, tuneLoop) error) error {
	var byRole [audio.MusicSlots][]audio.PatternID
	for _, p := range audio.RegisteredPatterns() {
		if strings.HasPrefix(p.Name, "corpus_") {
			continue // a generated part of an earlier loop, not the bank's
		}
		if p.ID == audio.PatternMelodyGen {
			continue // the sequencer writes its lead bar by bar as it plays: no label reads it
		}
		switch p.Role {
		case audio.RoleRhythm, audio.RoleMelody, audio.RoleBass:
			byRole[p.Role-1] = append(byRole[p.Role-1], p.ID)
		}
	}
	if len(byRole[0]) == 0 || len(byRole[1]) == 0 || len(byRole[3]) == 0 {
		return errors.New("tune needs registered rhythm, melody and bass patterns (builtin pattern, apply)")
	}
	rng := rand.New(rand.NewPCG(o.seed, 0x5bd1e995))
	for i := range o.loops {
		var slots [audio.MusicSlots]audio.PatternID
		for slot, pool := range byRole {
			if len(pool) > 0 {
				slots[slot] = pool[rng.IntN(len(pool))]
			}
		}
		tempos := tuneTempos
		if len(o.tempos) > 0 {
			tempos = o.tempos
		}
		bpm := tempos[i%len(tempos)]
		root := audio.DefaultRootNote + rng.IntN(12) - 6
		scale := audio.ScaleID(rng.IntN(int(audio.ScaleCount)))
		if o.skip != nil && o.skip(i) {
			continue // drawn all the same, so the loops after it are an unbroken run's
		}
		// the augmentation and the generated parts draw from streams of their
		// own, so a loop's drawn patterns and key are the same with and without
		if o.gen {
			grng := rand.New(rand.NewPCG(o.seed^0x5a5a5a5a, uint64(i)+1))
			degrees := len(audio.ScaleIntervals(scale))
			for _, g := range []struct {
				slot int
				make func(*rand.Rand, int) *audio.Pattern
			}{{0, genBeat}, {1, genMelody}, {2, genAccompaniment}, {3, genBass}} {
				if grng.Float64() >= 0.6 {
					continue
				}
				p := g.make(grng, degrees)
				p.ID = audio.PatternIDByName(p.Name) // the last loop's, replaced in place
				if id, err := s.eng.DefinePattern(p); err == nil {
					slots[g.slot] = id
				}
			}
			// much of a recording's music has no drums or no bass, a solo piano
			// neither, where the bank's loops always had both under the melody
			if grng.Float64() < 0.3 {
				slots[0] = audio.PatternSilence
			}
			if grng.Float64() < 0.15 {
				slots[3] = audio.PatternSilence
			}
		}
		arng := rand.New(rand.NewPCG(o.seed^0xa5a5a5a5, uint64(i)+1))
		voices, restore := s.loopVoices(slots, o.aug, arng)
		cents := 0.0
		if o.aug {
			cents = arng.Float64()*100 - 50 // a recording's pitch standard, within a quarter tone of A440
		}
		s.eng.SetTuning(cents)
		buf, err := s.eng.RenderMusic(slots, bpm, o.bars, root, scale, nil, int64(i+1))
		s.eng.SetTuning(0)
		restore()
		if err != nil {
			return err
		}
		if o.aug {
			buf = degrade(buf, arng)
		}
		if err := visit(i, tuneLoop{audio: buf, want: audio.ExpectedRip(slots, bpm, o.bars, root, scale, nil), slots: slots, bpm: bpm, root: root, scale: scale, voices: voices, tuning: cents}); err != nil {
			return err
		}
	}
	return nil
}

// genBeat is a beat the bank never had: kicks on one and three with extras, the
// snare on two and four with a ghost now and then, hats on the eighths or the
// quarters, velocities drawn; registered under one name a loop at a time
func genBeat(rng *rand.Rand, _ int) *audio.Pattern {
	var kick, snare, hat []audio.Step
	for _, pos := range []int{0, 8} {
		kick = append(kick, audio.Step{Pos: pos, Vel: 0.9 + rng.Float64()*0.1})
	}
	for _, pos := range []int{3, 6, 10, 11, 14} {
		if rng.Float64() < 0.25 {
			kick = append(kick, audio.Step{Pos: pos, Vel: 0.6 + rng.Float64()*0.3})
		}
	}
	for _, pos := range []int{4, 12} {
		if rng.Float64() < 0.9 {
			snare = append(snare, audio.Step{Pos: pos, Vel: 0.8 + rng.Float64()*0.2})
		}
	}
	for _, pos := range []int{7, 15} {
		if rng.Float64() < 0.2 {
			snare = append(snare, audio.Step{Pos: pos, Vel: 0.4})
		}
	}
	every := 2 + 2*rng.IntN(2)
	for pos := 0; pos < 16; pos += every {
		if rng.Float64() < 0.85 {
			hat = append(hat, audio.Step{Pos: pos, Vel: 0.5 + rng.Float64()*0.3})
		}
	}
	p := &audio.Pattern{Name: "corpus_beat", Steps: 16, Role: audio.RoleRhythm}
	for _, tr := range []audio.Track{{Instr: audio.InstrKick, Events: kick}, {Instr: audio.InstrSnare, Events: snare}, {Instr: audio.InstrHihat, Events: hat}} {
		if len(tr.Events) > 0 {
			p.Tracks = append(p.Tracks, tr)
		}
	}
	return p
}

// genMelody is a melody the bank never had: a walk over the key's degrees within
// two octaves, lengths of one to four steps with a rest now and then, or in
// half the loops a slow line of two to eight, on a tonal instrument drawn from
// the registered ones
func genMelody(rng *rand.Rand, degrees int) *audio.Pattern {
	steps := 16 * (1 + rng.IntN(2))
	tonal := audio.TonalInstruments()
	instr := tonal[rng.IntN(len(tonal))]
	if instr == audio.InstrBass {
		instr = audio.InstrLead
	}
	lengths := []int{1, 1, 2, 2, 3, 4}
	if rng.Float64() < 0.5 {
		lengths = []int{2, 2, 4, 4, 6, 8}
	}
	deg, oct := rng.IntN(degrees), 1+rng.IntN(2)
	var evs []audio.Step
	for pos := 0; pos < steps; {
		dur := lengths[rng.IntN(len(lengths))]
		if rng.Float64() < 0.2 {
			pos += dur
			continue
		}
		deg += rng.IntN(5) - 2
		for deg < 0 {
			deg, oct = deg+degrees, oct-1
		}
		for deg >= degrees {
			deg, oct = deg-degrees, oct+1
		}
		oct = min(max(oct, 1), 2)
		evs = append(evs, audio.Step{Pos: pos, Vel: 0.5 + rng.Float64()*0.5, Deg: deg, Oct: oct, Dur: min(dur, steps-pos)})
		pos += dur
	}
	if len(evs) == 0 {
		evs = []audio.Step{{Pos: 0, Vel: 0.8, Deg: 0, Oct: 1, Dur: 4}}
	}
	soften(rng, evs, steps)
	return &audio.Pattern{Name: "corpus_melody", Steps: steps, Role: audio.RoleMelody, Tracks: []audio.Track{{Instr: instr, Events: evs}}}
}

// soften plays half of a third of the generated parts 6 to 18 dB softer, as a
// pianist plays a phrase, so the model hears soft strikes beside loud ones
func soften(rng *rand.Rand, evs []audio.Step, steps int) {
	if rng.Float64() >= 1.0/3 {
		return
	}
	gain, from := math.Pow(10, -(6+12*rng.Float64())/20), rng.IntN(steps/2+1)
	for i := range evs {
		if evs[i].Pos >= from && evs[i].Pos < from+steps/2 {
			evs[i].Vel *= gain
		}
	}
}

// genAccompaniment is an accompaniment the bank never had, for the free slot: a
// broken chord, the key's first, third and fifth with the seventh now and then,
// in eighths or sixteenths that change pitch from note to note, up, down or
// alternating, softer than a lead, on a tonal instrument drawn from the
// registered ones: the line that runs under a slow melody in much music, which
// the rip has to part from it
func genAccompaniment(rng *rand.Rand, degrees int) *audio.Pattern {
	steps := 16 * (1 + rng.IntN(2))
	tonal := audio.TonalInstruments()
	instr := tonal[rng.IntN(len(tonal))]
	if instr == audio.InstrBass {
		instr = audio.InstrLead
	}
	chord := []int{0, 2, 4}
	if rng.Float64() < 0.3 && degrees > 6 {
		chord = append(chord, 6)
	}
	dur := 1 + rng.IntN(2) // sixteenths or eighths
	base := 1 + rng.IntN(2)
	order := rng.IntN(3) // up, down, alternating
	var evs []audio.Step
	for i, pos := 0, 0; pos < steps; i, pos = i+1, pos+dur {
		k := i % len(chord)
		switch order {
		case 1:
			k = len(chord) - 1 - k
		case 2:
			if i%2 == 1 {
				k = len(chord) - 1 - i/2%len(chord)
			} else {
				k = i / 2 % len(chord)
			}
		}
		oct := base + i/len(chord)%2 // the chord in one octave, then the next
		deg := chord[k]
		if deg >= degrees {
			deg = 0
		}
		evs = append(evs, audio.Step{Pos: pos, Vel: 0.4 + rng.Float64()*0.3, Deg: deg, Oct: min(oct, 2), Dur: dur})
	}
	soften(rng, evs, steps)
	return &audio.Pattern{Name: "corpus_accompaniment", Steps: steps, Role: audio.RoleFill, Tracks: []audio.Track{{Instr: instr, Events: evs}}}
}

// genBass is a bass line the bank never had: the root and its neighbours below
// the fifth, in the root's octave, in halves, quarters and eighths
func genBass(rng *rand.Rand, degrees int) *audio.Pattern {
	steps := 16
	var evs []audio.Step
	for pos := 0; pos < steps; {
		dur := []int{2, 4, 4, 8}[rng.IntN(4)]
		deg := []int{0, 0, 0, 4, 3, 2}[rng.IntN(6)]
		if deg >= degrees {
			deg = 0
		}
		evs = append(evs, audio.Step{Pos: pos, Vel: 0.7 + rng.Float64()*0.3, Deg: deg, Oct: 0, Dur: min(dur, steps-pos)})
		pos += dur
	}
	return &audio.Pattern{Name: "corpus_bass", Steps: steps, Role: audio.RoleBass, Tracks: []audio.Track{{Instr: audio.InstrBass, Events: evs}}}
}

// loopVoices is the voice each tonal instrument the slotted patterns name plays
// with: the registered one, or under aug one of random genes near it, a third of
// the time on another wave, and in a third of the loops every release but the
// bass's three to ten times as long, as under a piano's pedal; registered for the
// render and put back by restore, so a corpus hears voices beyond the shipped ones
func (s *Session) loopVoices(slots [audio.MusicSlots]audio.PatternID, aug bool, rng *rand.Rand) (map[string]*audio.VoiceDef, func()) {
	voices := map[string]*audio.VoiceDef{}
	var originals []*audio.VoiceDef
	pedal := 1.0 // the labels stay the notes as played, however long they ring
	if aug && rng.Float64() < 1.0/3 {
		pedal = 3 + rng.Float64()*7
	}
	for _, id := range slots {
		p := audio.GetPattern(id)
		if p == nil {
			continue
		}
		for _, tr := range p.Tracks {
			if tr.Instr.IsDrum() {
				continue
			}
			name := audio.InstrumentName(tr.Instr)
			v := audio.VoiceDefByName(name)
			if v == nil || voices[name] != nil {
				continue
			}
			if !aug {
				voices[name] = v
				continue
			}
			genes := v.Genes()
			for i := range genes {
				genes[i] = min(max(genes[i]+rng.NormFloat64()*0.15, 0), 1)
			}
			wave := cmp.Or(v.Wave, "sine")
			if rng.Float64() < 0.3 {
				wave = audio.VoiceWaves[rng.IntN(len(audio.VoiceWaves))]
			}
			r := audio.VoiceFromGenes(wave, genes)
			if rng.Float64() < 0.15 { // distorted, as a guitar through an amp
				r.Drive, r.Level = 2+rng.Float64()*14, r.Level*0.6
			}
			if tr.Instr != audio.InstrBass {
				r.Release = min(r.Release*pedal, 3)
			}
			r.VibratoHz, r.VibratoCents, r.VibratoDelay, r.Vowel = v.VibratoHz, v.VibratoCents, v.VibratoDelay, v.Vowel // not genes; kept from the registered voice
			r.Name = name
			if audio.ValidateVoice(r) != nil {
				voices[name] = v
				continue
			}
			if _, err := s.eng.DefineVoice(r); err != nil {
				voices[name] = v
				continue
			}
			originals = append(originals, v)
			voices[name] = r
		}
	}
	return voices, func() {
		for _, v := range originals {
			s.eng.DefineVoice(v)
		}
	}
}

// degrade puts a room and a mix between a render and the frames a model hears,
// each drawn from rng so the model does not learn the synthesizer's clean
// output: a swell of up to 6 dB, for half the loops a low-pass, two equaliser
// bands, for a third a soft clip, for half a compressor, a booth's reverb or for
// a third a hall's further back, and for half a noise floor 40 to 65 dB down
func degrade(x []float64, rng *rand.Rand) []float64 {
	sr := float64(audio.AudioSampleRate)
	out := make([]float64, len(x))
	gain := math.Pow(10, (rng.Float64()*9-6)/20)
	swell, period, phase := rng.Float64()*6, (0.5+rng.Float64()*1.5)*float64(len(x)), rng.Float64()*2*math.Pi
	a := 1.0
	if rng.Float64() < 0.5 {
		a = 1 - math.Exp(-2*math.Pi*(4000+rng.Float64()*7000)/sr)
	}
	drive := 1.0
	if rng.Float64() < 1.0/3 {
		drive = 1.5 + rng.Float64()*2
	}
	var bands [2]func(float64) float64
	for k := range bands {
		bands[k] = audio.Peaking(80*math.Pow(100, rng.Float64()), rng.Float64()*12-6, 0.5+rng.Float64()*1.5)
	}
	var lp float64
	for i, v := range x {
		g := gain * math.Pow(10, swell*math.Sin(2*math.Pi*float64(i)/period+phase)/20)
		lp += a * (v*g - lp)
		out[i] = math.Tanh(drive*bands[1](bands[0](lp))) / math.Tanh(drive)
	}
	if rng.Float64() < 0.5 {
		compress(out, 2+rng.Float64()*4, 3+rng.Float64()*9, sr)
	}
	// a Schroeder reverb: four combs in parallel into two allpasses, at a decay
	// and a mix of the draw's
	rt, wet := 0.15+rng.Float64()*0.55, 0.05+rng.Float64()*0.25
	if rng.Float64() < 1.0/3 {
		rt, wet = 0.7+rng.Float64()*1.8, 0.03+rng.Float64()*0.12
	}
	combs := [4]int{int(0.0297 * sr), int(0.0371 * sr), int(0.0411 * sr), int(0.0437 * sr)}
	var lines [4][]float64
	var feedback [4]float64
	for k, n := range combs {
		lines[k] = make([]float64, n)
		feedback[k] = math.Pow(0.001, float64(n)/sr/rt)
	}
	aps := [2]int{int(0.005 * sr), int(0.0017 * sr)}
	var apLines [2][]float64
	for k, n := range aps {
		apLines[k] = make([]float64, n)
	}
	var rms float64
	for i := range out {
		dry := out[i]
		var sum float64
		for k := range combs {
			j := i % len(lines[k])
			y := lines[k][j]
			lines[k][j] = dry + y*feedback[k]
			sum += y
		}
		sum *= 0.25
		for k := range aps {
			j := i % len(apLines[k])
			buf := apLines[k][j]
			apLines[k][j] = sum + buf*0.5
			sum = buf - sum*0.5
		}
		out[i] = dry + wet*sum
		rms += out[i] * out[i]
	}
	rms = math.Sqrt(rms / float64(max(len(out), 1)))
	// a recording's hiss, which a studio's Billie Jean carries 39 dB under its
	// median frame, sits bare over a synthesizer's empty treble: half the loops have none
	noise := 0.0
	if rng.Float64() < 0.5 {
		noise = rms * math.Pow(10, (rng.Float64()*25-65)/20)
	}
	var peak float64
	for i := range out {
		out[i] += rng.NormFloat64() * noise
		peak = max(peak, math.Abs(out[i]))
	}
	if peak > 1 { // the room on top of the clip can pass full scale: turned down, not clipped hard
		for i := range out {
			out[i] /= peak
		}
	}
	return out
}

// compress turns down what rises over the loop's peak less under dB by ratio,
// on an envelope that rises in 5 ms and falls in 120 ms
func compress(x []float64, ratio, under, sr float64) {
	var peak float64
	for _, v := range x {
		peak = max(peak, math.Abs(v))
	}
	threshold := peak * math.Pow(10, -under/20)
	rise, fall := 1-math.Exp(-1/(0.005*sr)), 1-math.Exp(-1/(0.12*sr))
	var env float64
	for i, v := range x {
		if a := math.Abs(v); a > env {
			env += rise * (a - env)
		} else {
			env += fall * (a - env)
		}
		if env > threshold {
			x[i] = v * math.Pow(env/threshold, 1/ratio-1)
		}
	}
}

// corpusEvent is one labelled event of an exported loop, in frames of the rip's
// grid: a drum's hit, or a note with the wave and voice genes it was played with
type corpusEvent struct {
	Frame int       `json:"frame"`
	End   int       `json:"end"`
	Drum  int       `json:"drum"` // kick 0, snare 1, hihat 2; -1 for a note
	MIDI  int       `json:"midi"`
	Vel   float64   `json:"vel"`
	Wave  int       `json:"wave"` // index into the waves of the manifest; -1 unknown
	Voice []float64 `json:"voice,omitempty"`
}

// corpusLoop is an exported loop's label file
type corpusLoop struct {
	BPM    int           `json:"bpm"`
	Root   int           `json:"root"`
	Scale  int           `json:"scale"`
	Bars   int           `json:"bars"`
	Frames int           `json:"frames"`
	Tuning float64       `json:"tuning"` // cents off A440 the loop sounds at
	Events []corpusEvent `json:"events"`
	Source string        `json:"source,omitempty"` // the recording a loop was heard from
}

// corpusManifest names the corpus's layout for the trainer
type corpusManifest struct {
	Version    int      `json:"version"`
	Loops      int      `json:"loops"`
	Rate       int      `json:"rate"`
	Hop        int      `json:"hop"`
	MelBands   int      `json:"mel_bands"`
	MinMIDI    int      `json:"min_midi"`
	Pitches    int      `json:"pitches"`
	Drums      []string `json:"drums"`
	Waves      []string `json:"waves"`
	VoiceGenes int      `json:"voice_genes"`
}

func cmdCorpus(s *Session, a []string) error {
	if len(a) > 2 && a[1] == "from" {
		return s.corpusFrom(s.path(a[0]), s.path(a[2]), a[3:])
	}
	o := tuneOptions{loops: 24, bars: 4, seed: 1}
	for more := true; more && len(a) > 1; {
		n := len(a)
		switch {
		case a[n-1] == "aug":
			o.aug = true
		case a[n-1] == "gen":
			o.gen = true
		case a[n-1] == "nowav":
			o.nowav = true
		case a[n-1] == "codec":
			o.codec = true
		case n > 2 && a[n-2] == "tempos":
			tempos, err := parseTempos(a[n-1])
			if err != nil {
				return err
			}
			o.tempos = tempos
			a = a[:n-1]
		case n > 2 && a[n-2] == "part":
			k, of, _ := strings.Cut(a[n-1], "/")
			part, err1 := strconv.Atoi(k)
			parts, err2 := strconv.Atoi(of)
			if err1 != nil || err2 != nil || part < 0 || part >= parts {
				return fmt.Errorf("part %q: want k/n, k from 0 below n", a[n-1])
			}
			o.part, o.parts = part, parts
			a = a[:n-1]
		default:
			more = false
			continue
		}
		a = a[:len(a)-1]
	}
	for i, p := range []*int{&o.loops, &o.bars} {
		limit := 65536
		if i == 1 {
			limit = 64
		}
		if len(a) > i+1 {
			n, err := strconv.Atoi(a[i+1])
			if err != nil || n < 1 || n > limit {
				return fmt.Errorf("%q: want 1 to %d", a[i+1], limit)
			}
			*p = n
		}
	}
	return s.exportCorpus(s.path(a[0]), o)
}

// corpusFrom writes a corpus of recordings and the MIDI files they were played
// from, one loop each, listed by a CSV with audio_filename and midi_filename
// columns (split and duration when present, as E-GMD's metadata has them): the
// frames as heard, the notes as the MIDI plays them, the voices unknown. A loop
// on disk is kept and known by its source, so a run again goes on where it
// stopped and a new batch takes the numbers free after the loops there.
func (s *Session) corpusFrom(dir, list string, a []string) error {
	split, hours, codec := "", math.Inf(1), false
	for len(a) > 0 {
		if a[0] == "codec" {
			codec, a = true, a[1:]
			continue
		}
		if len(a) < 2 || a[0] != "split" && a[0] != "hours" {
			return fmt.Errorf("%q: want split <name>, hours <h> or codec", a[0])
		}
		if a[0] == "split" {
			split = a[1]
		} else if h, err := strconv.ParseFloat(a[1], 64); err != nil || h <= 0 {
			return fmt.Errorf("hours %q: want a positive number", a[1])
		} else {
			hours = h
		}
		a = a[2:]
	}
	f, err := os.Open(list)
	if err != nil {
		return err
	}
	rows, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("%s: no header", list)
	}
	col := map[string]int{}
	for i, name := range rows[0] {
		col[name] = i
	}
	wav, ok1 := col["audio_filename"]
	mid, ok2 := col["midi_filename"]
	if !ok1 || !ok2 {
		return fmt.Errorf("%s: want audio_filename and midi_filename columns", list)
	}
	rows, at := rows[1:], func(name string) string { return filepath.Join(filepath.Dir(list), name) }
	if c, ok := col["split"]; ok && split != "" {
		rows = slices.DeleteFunc(rows, func(r []string) bool { return r[c] != split })
	}
	// a share of a large set may be all that is on disk
	listed := len(rows)
	rows = slices.DeleteFunc(rows, func(r []string) bool {
		_, err1 := os.Stat(at(r[wav]))
		_, err2 := os.Stat(at(r[mid]))
		return err1 != nil || err2 != nil
	})
	missing := listed - len(rows)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	loopFile := func(i int) string { return filepath.Join(dir, fmt.Sprintf("loop_%03d", i)) }
	taken, held, frames := map[int]bool{}, map[string]bool{}, 0
	labels, _ := filepath.Glob(filepath.Join(dir, "loop_*.json"))
	for _, name := range labels {
		i, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(name), "loop_"), ".json"))
		data, rerr := os.ReadFile(name)
		var l corpusLoop
		if err != nil || rerr != nil || json.Unmarshal(data, &l) != nil {
			continue // a label cut short is written again
		}
		taken[i], held[l.Source], frames = true, true, frames+l.Frames
	}
	rows = slices.DeleteFunc(rows, func(r []string) bool { return held[r[wav]] })
	// the same draw every run, so a share of the list spans its composers and years
	rng := rand.New(rand.NewPCG(1, 2))
	rng.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
	type job struct {
		at    int
		row   []string
		score *audio.MIDIScore
	}
	var (
		jobs    []job
		skipped []string
		next    int
		sum     = float64(frames) * audio.RipFrameSec
	)
	for _, r := range rows {
		if sum >= hours*3600 {
			break
		}
		g, err := os.Open(at(r[mid]))
		if err != nil {
			return err
		}
		score, err := audio.ReadMIDI(g)
		g.Close()
		if errors.Is(err, audio.ErrMIDIUnlined) {
			skipped = append(skipped, r[mid])
			continue
		} else if err != nil {
			return fmt.Errorf("%s: %w", at(r[mid]), err)
		}
		for taken[next] {
			next++
		}
		jobs = append(jobs, job{next, r, score})
		taken[next] = true
		if c, ok := col["duration"]; ok {
			d, _ := strconv.ParseFloat(r[c], 64)
			sum += d
		}
	}
	loops := len(taken)
	for i := range taken {
		if i >= loops {
			return fmt.Errorf("%s: %s.json past %d loops: a loop below it was removed", dir, loopFile(i), loops)
		}
	}
	if loops == 0 {
		return fmt.Errorf("%s: no recordings listed for split %q, %d skipped", list, split, len(skipped))
	}
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		first error
		work  = make(chan job)
	)
	for range min(4, runtime.NumCPU()) { // a recording's samples are hundreds of megabytes
		wg.Go(func() {
			for j := range work {
				var coder *rand.Rand
				if codec {
					coder = rand.New(rand.NewPCG(0xc0dec, uint64(j.at)+1))
				}
				n, err := corpusRecording(loopFile(j.at), at(j.row[wav]), j.row[wav], j.score, coder)
				mu.Lock()
				frames += n
				first = cmp.Or(first, err)
				mu.Unlock()
			}
		})
	}
	for _, j := range jobs {
		work <- j
	}
	close(work)
	wg.Wait()
	if first != nil {
		return first
	}
	m := corpusManifest{Version: 1, Loops: loops, Rate: audio.RipRate, Hop: audio.RipHop, MelBands: audio.RipMelBands,
		MinMIDI: audio.RipMinMIDI, Pitches: audio.RipPitches, Drums: []string{"kick", "snare", "hihat"}, Waves: audio.VoiceWaves[:], VoiceGenes: audio.VoiceGeneCount}
	if err := writeJSON(filepath.Join(dir, "corpus.json"), m); err != nil {
		return err
	}
	how := ""
	if codec {
		how = ", a third through a lossy codec"
	}
	fmt.Fprintf(s.out, "wrote %d recording(s) to %s from %s%s, %d loop(s) and %.1f hours in all; %d listed were not on disk\n",
		len(jobs), dir, list, how, loops, float64(frames)*audio.RipFrameSec/3600, missing)
	if len(skipped) > 0 {
		fmt.Fprintf(s.out, "skipped %d whose MIDI has no notes and no kick, snare or hihat, as %s\n", len(skipped), skipped[0])
	}
	return nil
}

// corpusRecording writes a recording's frames, through a codec when coder is
// set, and its score's notes, hits and other strikes as one loop, the frames it
// holds returned: a strike of no line (a tom, a crash) is an onset, drum -1 and midi 0.
func corpusRecording(base, wav, source string, score *audio.MIDIScore, coder *rand.Rand) (int, error) {
	f, err := os.Open(wav)
	if err != nil {
		return 0, err
	}
	x, rate, err := audio.ReadWAV(f)
	f.Close()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", wav, err)
	}
	if coder != nil {
		if x, err = codecRoundTrip(x, rate, coder); err != nil {
			return 0, err
		}
	}
	mel := audio.MelFrames(x, rate)
	if err := writeMel(base+".mel", mel); err != nil {
		return 0, err
	}
	frame := func(sec float64) int { return int(sec/audio.RipFrameSec + 0.5) }
	lab := corpusLoop{Frames: len(mel), Source: source}
	for _, n := range score.Notes {
		lab.Events = append(lab.Events, corpusEvent{Frame: frame(n.Start), End: frame(n.End), Drum: -1, MIDI: n.MIDI, Vel: n.Vel, Wave: -1})
	}
	for _, h := range append(slices.Clip(score.Hits), score.Struck...) {
		lab.Events = append(lab.Events, corpusEvent{Frame: frame(h.Start), End: frame(h.Start), Drum: int(h.Drum), Vel: h.Vel, Wave: -1})
	}
	lab.Events = slices.DeleteFunc(lab.Events, func(e corpusEvent) bool { return e.Frame >= len(mel) })
	return len(mel), writeJSON(base+".json", lab)
}

// parseTempos reads a range lo-hi, six tempos spread over it, or a list a,b,c,
// each within the sequencer's range: the tempos a corpus cycles through.
func parseTempos(arg string) ([]int, error) {
	var tempos []int
	if lo, hi, ok := strings.Cut(arg, "-"); ok {
		a, err1 := strconv.Atoi(lo)
		b, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil || a > b {
			return nil, fmt.Errorf("tempos %q: want lo-hi or a,b,c", arg)
		}
		for i := range 6 {
			tempos = append(tempos, a+(b-a)*i/5)
		}
	} else {
		for _, f := range strings.Split(arg, ",") {
			n, err := strconv.Atoi(f)
			if err != nil {
				return nil, fmt.Errorf("tempos %q: want lo-hi or a,b,c", arg)
			}
			tempos = append(tempos, n)
		}
	}
	for _, t := range tempos {
		if t < audio.MinBPM || t > audio.MaxBPM {
			return nil, fmt.Errorf("tempos %q: %d outside %d to %d", arg, t, audio.MinBPM, audio.MaxBPM)
		}
	}
	return tempos, nil
}

// exportCorpus writes loops into dir: loop_NNN.wav as rendered, loop_NNN.mel as
// the log-mel frames a front-end model hears (frames, bands, then float32 rows,
// little-endian), loop_NNN.json as the events that sound in it with their frames
// and voices, and corpus.json naming the layout. A loop whose label is on disk is
// kept, so an export run again goes on where it stopped; a part writes no layout.
func (s *Session) exportCorpus(dir string, o tuneOptions) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	loopFile := func(i int) string { return filepath.Join(dir, fmt.Sprintf("loop_%03d", i)) }
	written, kept := 0, 0
	o.skip = func(i int) bool {
		if o.parts > 0 && i%o.parts != o.part {
			return true
		}
		_, err := os.Stat(loopFile(i) + ".json") // written last, so its loop is whole
		if err == nil {
			kept++
		}
		return err == nil
	}
	err := s.eachLoop(o, func(i int, l tuneLoop) error {
		written++
		base := loopFile(i)
		if o.codec {
			coded, err := codecRoundTrip(l.audio, audio.AudioSampleRate, rand.New(rand.NewPCG(o.seed^0xc0dec, uint64(i)+1)))
			if err != nil {
				return err
			}
			l.audio = coded
		}
		if !o.nowav {
			f, err := os.Create(base + ".wav")
			if err != nil {
				return err
			}
			werr := audio.WriteWAV(f, l.audio)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				return werr
			}
		}
		mel := audio.MelFrames(l.audio, audio.AudioSampleRate)
		if err := writeMel(base+".mel", mel); err != nil {
			return err
		}
		lab := corpusLoop{BPM: l.bpm, Root: l.root, Scale: int(l.scale), Bars: o.bars, Frames: len(mel), Tuning: l.tuning}
		stepSec := float64(audio.SamplesPerStep(l.bpm)) / audio.AudioSampleRate
		for _, e := range audio.ExpectedEvents(l.slots, o.bars, l.root, l.scale, nil) {
			start := float64(e.Bar*audio.StepsPerBar+e.Step) * stepSec
			ev := corpusEvent{Frame: int(start/audio.RipFrameSec + 0.5), Drum: -1, Vel: e.Vel, Wave: -1}
			if e.Instr.IsDrum() {
				if e.Instr >= audio.InstrClap {
					continue
				}
				ev.Drum, ev.End = int(e.Instr), ev.Frame
			} else {
				ev.MIDI = e.MIDI
				ev.End = int((start+float64(e.Dur)*stepSec)/audio.RipFrameSec + 0.5)
				if v := l.voices[audio.InstrumentName(e.Instr)]; v != nil {
					ev.Wave = max(slices.Index(audio.VoiceWaves[:], cmp.Or(v.Wave, "sine")), 0)
					ev.Voice = v.Genes()
				}
			}
			if ev.Frame < len(mel) {
				lab.Events = append(lab.Events, ev)
			}
		}
		return writeJSON(base+".json", lab)
	})
	if err != nil {
		return err
	}
	if o.parts == 0 {
		m := corpusManifest{Version: 1, Loops: o.loops, Rate: audio.RipRate, Hop: audio.RipHop, MelBands: audio.RipMelBands,
			MinMIDI: audio.RipMinMIDI, Pitches: audio.RipPitches, Drums: []string{"kick", "snare", "hihat"}, Waves: audio.VoiceWaves[:], VoiceGenes: audio.VoiceGeneCount}
		if err := writeJSON(filepath.Join(dir, "corpus.json"), m); err != nil {
			return err
		}
	}
	how := "as rendered"
	if o.aug {
		how = "through a room, a mix, voices of random genes and a tuning off A440"
	}
	if o.gen {
		how += ", with generated beats, melodies and bass lines"
	}
	if o.codec {
		how += ", a third through a lossy codec"
	}
	if kept > 0 {
		how += fmt.Sprintf("; %d were there", kept)
	}
	if o.parts > 0 {
		how += fmt.Sprintf("; part %d of %d, so no corpus.json", o.part, o.parts)
	}
	fmt.Fprintf(s.out, "wrote %d loop(s) of %d bar(s) to %s, %s\n", written, o.bars, dir, how)
	return nil
}

// corpusCodecs are what recordings are stored in. A model that has only heard
// lossless renders flips notes on the noise and smeared attacks they leave.
var corpusCodecs = []struct {
	name, ext string
	kbps      []int
}{{"libopus", "opus", []int{64, 96, 128, 160}}, {"libmp3lame", "mp3", []int{96, 128, 192}}, {"aac", "m4a", []int{96, 128, 192}}}

// codecRoundTrip sends a third of the loops through a codec at a drawn bit rate and back
func codecRoundTrip(x []float64, rate int, rng *rand.Rand) ([]float64, error) {
	if rng.IntN(3) != 0 {
		return x, nil
	}
	c := corpusCodecs[rng.IntN(len(corpusCodecs))]
	return codecPass(x, rate, c.name, c.ext, c.kbps[rng.IntN(len(c.kbps))])
}

// codecPass codes x and decodes it, the same length and in time, through a file
// since a container read from a pipe loses the codec's delay. Only a corpus needs ffmpeg.
func codecPass(x []float64, sampleRate int, name, ext string, kbps int) ([]float64, error) {
	tmp, err := os.CreateTemp("", "corpus-*."+ext)
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	rate := strconv.Itoa(sampleRate)
	raw := make([]byte, 8*len(x))
	for i, v := range x {
		binary.LittleEndian.PutUint64(raw[8*i:], math.Float64bits(v))
	}
	enc := exec.Command("ffmpeg", "-loglevel", "error", "-y", "-f", "f64le", "-ar", rate, "-ac", "1", "-i", "pipe:0", "-c:a", name, "-b:a", fmt.Sprintf("%dk", kbps), tmp.Name())
	enc.Stdin = bytes.NewReader(raw)
	if out, err := enc.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("codec %s needs ffmpeg: %w: %s", name, err, out)
	}
	out, err := exec.Command("ffmpeg", "-loglevel", "error", "-i", tmp.Name(), "-f", "f64le", "-ar", rate, "-ac", "1", "pipe:1").Output()
	if err != nil {
		return nil, fmt.Errorf("codec %s: %w", name, err)
	}
	y := make([]float64, len(x))
	for i := range min(len(y), len(out)/8) {
		y[i] = math.Float64frombits(binary.LittleEndian.Uint64(out[8*i:]))
	}
	return y, nil
}

func writeMel(file string, mel [][]float32) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	le := binary.LittleEndian
	bands := 0
	if len(mel) > 0 {
		bands = len(mel[0])
	}
	binary.Write(w, le, uint32(len(mel)))
	binary.Write(w, le, uint32(bands))
	for _, row := range mel {
		binary.Write(w, le, row)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeJSON(file string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}

func cmdBench(s *Session, a []string) error {
	o := tuneOptions{loops: 8, bars: 4, seed: 7, gen: true}
	verbose := len(a) > 0 && a[len(a)-1] == "verbose"
	if verbose {
		a = a[:len(a)-1]
	}
	if len(a) > 2 {
		return fmt.Errorf("%q: want verbose", a[2])
	}
	for i, p := range []*int{&o.loops, &o.bars} {
		if len(a) > i {
			n, err := strconv.Atoi(a[i])
			if err != nil || n < 1 || n > 256 {
				return fmt.Errorf("%q: want 1 to 256", a[i])
			}
			*p = n
		}
	}
	return s.bench(o, verbose)
}

// benchRow is one loop as one front end ripped it, for bench's verbose listing
type benchRow struct {
	what string // index, tempo, key, kind, generated parts and front end, in columns
	sc   audio.RipScore
	err  error
}

// bench grades the front ends the session has, the analysis and the model when
// one loaded, on fresh loops the engine renders with generated parts among the
// bank's, clean and augmented: the mean of each rip's score against the loop's
// expected rip, so a trained model is measured against the analysis before it
// is trusted with a recording; verbose then lists every loop and the worst three
func (s *Session) bench(o tuneOptions, verbose bool) error {
	type front struct {
		name string
		fe   audio.RipFrontEnd
	}
	fronts := []front{{"analysis", nil}}
	if s.frontEnd != nil {
		fronts = append(fronts, front{"model", s.frontEnd})
	}
	var rows []benchRow
	for _, aug := range []bool{false, true} {
		o.aug = aug
		loops, err := s.corpus(o)
		if err != nil {
			return err
		}
		kind := "clean"
		if aug {
			kind = "augmented"
		}
		for _, f := range fronts {
			var sum audio.RipScore
			n := 0
			for i, l := range loops {
				r, err := audio.Ripper{Params: s.rip, FrontEnd: f.fe}.Samples(l.audio, audio.AudioSampleRate)
				var sc audio.RipScore
				if err == nil {
					sc = audio.CompareRips(r, l.want)
					sum.Tempo, sum.Bars, sum.Drums = sum.Tempo+sc.Tempo, sum.Bars+sc.Bars, sum.Drums+sc.Drums
					sum.Melody, sum.Bass, sum.Total = sum.Melody+sc.Melody, sum.Bass+sc.Bass, sum.Total+sc.Total
					n++
				}
				if verbose {
					what := fmt.Sprintf("%3d %3d bpm %-3s %-14s %-9s %-23s %-8s", i, l.bpm, noteName(l.root), scaleName(l.scale), kind, generatedParts(l.slots), f.name)
					rows = append(rows, benchRow{what: what, sc: sc, err: err})
				}
			}
			if n == 0 {
				fmt.Fprintf(s.out, "%s on %s loops: no rip\n", f.name, kind)
				continue
			}
			d := float64(n)
			mean := audio.RipScore{Tempo: sum.Tempo / d, Bars: sum.Bars / d, Drums: sum.Drums / d, Melody: sum.Melody / d, Bass: sum.Bass / d, Total: sum.Total / d}
			fmt.Fprintf(s.out, "%s on %d %s loop(s): %s\n", f.name, n, kind, benchScore(mean))
		}
	}
	if !verbose {
		return nil
	}
	var ripped []benchRow
	for _, r := range rows {
		if r.err != nil {
			fmt.Fprintf(s.out, "loop  %s no rip (%v)\n", r.what, r.err)
			continue
		}
		fmt.Fprintf(s.out, "loop  %s %s\n", r.what, benchScore(r.sc))
		ripped = append(ripped, r)
	}
	slices.SortStableFunc(ripped, func(a, b benchRow) int { return cmp.Compare(a.sc.Total, b.sc.Total) })
	for _, r := range ripped[:min(3, len(ripped))] {
		fmt.Fprintf(s.out, "worst %s %s\n", r.what, benchScore(r.sc))
	}
	return nil
}

func benchScore(sc audio.RipScore) string {
	return fmt.Sprintf("%.3f (tempo %.2f bars %.2f drums %.2f melody %.2f bass %.2f)", sc.Total, sc.Tempo, sc.Bars, sc.Drums, sc.Melody, sc.Bass)
}

// generatedParts names the slots a loop's generated patterns fill, bank for none
func generatedParts(slots [audio.MusicSlots]audio.PatternID) string {
	var parts []string
	for k, id := range slots {
		if p := audio.GetPattern(id); p != nil && strings.HasPrefix(p.Name, "corpus_") {
			parts = append(parts, slotRoles[k])
		}
	}
	if len(parts) == 0 {
		return "bank"
	}
	return strings.Join(parts, "+")
}

// tune searches the rip's parameters: the current ones seed a pool the rest of
// which is drawn across the ranges, each candidate scored by how its rip of every
// loop compares with the loop's expected rip, and the best, when it beats the
// current, is written and listened with from then on.
func (s *Session) tune(o tuneOptions, file string) error {
	loops, err := s.corpus(o)
	if err != nil {
		return err
	}
	eval := func(genes []float64) float64 {
		p := audio.RipParamsFromGenes(genes)
		var sum float64
		for _, l := range loops {
			if r, err := audio.RipSamples(l.audio, audio.AudioSampleRate, p); err == nil {
				sum += audio.CompareRips(r, l.want).Total
			}
		}
		return sum / float64(len(loops))
	}
	ranges := audio.RipGeneRanges()
	bounds := make([]genetic.ParameterBounds, len(ranges))
	for i, r := range ranges {
		bounds[i] = genetic.ParameterBounds{Min: r[0], Max: r[1]}
	}
	start := s.rip.Genes()
	base := eval(start)
	fmt.Fprintf(s.out, "tuning over %d loop(s) of %d bar(s): the current parameters score %.3f\n", len(loops), o.bars, base)
	seeded := false
	init := func(rng *rand.Rand) []float64 {
		g := make([]float64, len(bounds))
		if !seeded {
			seeded = true
			copy(g, start)
			return g
		}
		for i, b := range bounds {
			g[i] = b.Min + rng.Float64()*(b.Max-b.Min)
		}
		return g
	}
	cfg := genetic.DefaultConfig()
	cfg.PoolSize, cfg.EliteCount, cfg.MaxIterations = o.pool, max(o.pool/8, 1), o.generations
	cfg.Parallelism, cfg.Seed = runtime.NumCPU(), o.seed
	cfg.PerturbationRate, cfg.PerturbationStrength = 0.25, 0.15
	e := genetic.NewEngine[[]float64, float64](eval, init,
		&genetic.TournamentSelector[[]float64, float64]{TournamentSize: 3},
		&genetic.UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
		&genetic.BoundedPerturbator{Bounds: bounds, Boundary: genetic.BoundaryReflect}, cfg)
	e.SetTerminator(func(p *genetic.Pool[[]float64, float64], iter int) bool {
		fmt.Fprintf(s.out, "generation %d: best %.3f mean %.3f\n", iter, p.Stats.BestScore, p.Stats.AverageScore)
		return false
	})
	pool, err := e.Run(context.Background())
	if err != nil {
		return err
	}
	best := pool.Members[0]
	if best.Score <= base {
		fmt.Fprintf(s.out, "nothing beat the current parameters (%.3f); they stay\n", base)
		return nil
	}
	p := audio.RipParamsFromGenes(best.Data)
	if file, err = saveFile(file, s.ripSrc, s.files.ripTarget); err != nil {
		return err
	}
	data, err := audio.MarshalRipParams(p)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	s.rip, s.ripSrc = p, file
	fmt.Fprintf(s.out, "wrote %s: score %.3f, was %.3f; rip listens with it from now on\n", file, best.Score, base)
	return nil
}

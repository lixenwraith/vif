package audio

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/lixenwraith/vif/pkg/audio/model"
)

// VoiceDef is one tonal instrument as authored: an oscillator stack through a
// resonant low-pass under a filter envelope into an amplitude ADSR, or a string,
// as a delay line (wave "string") or as its partials (wave "modal"). Times are
// seconds, frequencies Hz, sustain and levels linear; a zero field is unset and
// takes the neutral default its comment names.
type VoiceDef struct {
	Name string `toml:"name"`
	Desc string `toml:"desc,omitempty"`
	Wave string `toml:"wave,omitempty"` // sine (the default), saw, square, string or modal

	Unison  int     `toml:"unison,omitempty"`   // oscillators, 1..3; 0 = 1
	Detune  float64 `toml:"detune,omitempty"`   // frequency ratio between neighbouring oscillators; 0 = 1
	Sub     float64 `toml:"sub,omitempty"`      // level of a sine an octave below, added after the drive
	FMRatio float64 `toml:"fm_ratio,omitempty"` // 2-operator FM when fm_index > 0
	FMIndex float64 `toml:"fm_index,omitempty"`
	FMDecay float64 `toml:"fm_decay,omitempty"` // the index's own decay; 0 rides the amplitude envelope

	Bright float64 `toml:"bright,omitempty"` // string: 0 a dull string, 1 a wiry one
	Ring   float64 `toml:"ring,omitempty"`   // string: seconds a plucked string takes to die away

	// modal: partials caps the bank, strike_pos is where along the string the
	// hammer or finger lands, tilt the spectral slope (1 struck, 2 plucked), and
	// inharm the stretch B of a middle-C string, growing with pitch by inharm_track
	Partials    int     `toml:"partials,omitempty"`
	StrikePos   float64 `toml:"strike_pos,omitempty"`
	Tilt        float64 `toml:"tilt,omitempty"`
	Inharm      float64 `toml:"inharm,omitempty"`
	InharmTrack float64 `toml:"inharm_track,omitempty"`
	// modal: decay_time is the prompt decay at middle C, growing toward the bass by
	// decay_track; a partial at damp_hz decays twice as fast. The coupled pair is a
	// second string pair cents away, pair_level under the prompt sound and decaying
	// pair_slow times as fast; hammer_hz low-passes the strike at full velocity and
	// knock is the level of the contact noise
	DecayTime  float64 `toml:"decay_time,omitempty"`
	DecayTrack float64 `toml:"decay_track,omitempty"`
	DampHz     float64 `toml:"damp_hz,omitempty"`
	Pair       float64 `toml:"pair,omitempty"`
	PairLevel  float64 `toml:"pair_level,omitempty"`
	PairSlow   float64 `toml:"pair_slow,omitempty"`
	HammerHz   float64 `toml:"hammer_hz,omitempty"`
	Knock      float64 `toml:"knock,omitempty"`

	Cutoff    float64 `toml:"cutoff,omitempty"`     // Hz at rest
	KeyTrack  float64 `toml:"key_track,omitempty"`  // Hz added per Hz of the note
	EnvAmount float64 `toml:"env_amount,omitempty"` // Hz added at the filter envelope's peak
	EnvDecay  float64 `toml:"env_decay,omitempty"`  // filter envelope decay; 0 rides the amplitude envelope
	Q         float64 `toml:"q,omitempty"`          // resonance; 0 bypasses the filter

	Attack  float64 `toml:"attack,omitempty"`
	Decay   float64 `toml:"decay,omitempty"`
	Sustain float64 `toml:"sustain,omitempty"`
	Release float64 `toml:"release,omitempty"`
	Drive   float64 `toml:"drive,omitempty"` // tanh drive after the filter; 0 = clean
	Level   float64 `toml:"level,omitempty"` // 0 = 1
}

// VoiceWaves names the waves a voice may have, in the order a model's wave head
// scores them.
var VoiceWaves = [...]string{"sine", "saw", "square", "string", "modal"}

// voiceGene is one voice parameter as a search or a model sees it: its range,
// whether it is read on a log scale, and where it lives.
type voiceGene struct {
	name     string
	min, max float64
	log      bool
	at       func(*VoiceDef) *float64
}

// voiceGenes is every continuous voice parameter in gene order, the wave aside;
// the two counts are read as floats and rounded back.
var voiceGenes = []voiceGene{
	{"unison", 1, 3, false, nil},
	{"detune", 1, 1.02, false, func(v *VoiceDef) *float64 { return &v.Detune }},
	{"sub", 0, 1, false, func(v *VoiceDef) *float64 { return &v.Sub }},
	{"fm_ratio", 0, 8, false, func(v *VoiceDef) *float64 { return &v.FMRatio }},
	{"fm_index", 0, 8, false, func(v *VoiceDef) *float64 { return &v.FMIndex }},
	{"fm_decay", 0, 2, false, func(v *VoiceDef) *float64 { return &v.FMDecay }},
	{"bright", 0, 1, false, func(v *VoiceDef) *float64 { return &v.Bright }},
	{"ring", 0.05, 4, true, func(v *VoiceDef) *float64 { return &v.Ring }},
	{"partials", 4, 48, false, nil},
	{"strike_pos", 0.05, 0.5, false, func(v *VoiceDef) *float64 { return &v.StrikePos }},
	{"tilt", 0.5, 3, false, func(v *VoiceDef) *float64 { return &v.Tilt }},
	{"inharm", 1e-5, 1e-2, true, func(v *VoiceDef) *float64 { return &v.Inharm }},
	{"inharm_track", 0, 2, false, func(v *VoiceDef) *float64 { return &v.InharmTrack }},
	{"decay_time", 0.05, 4, true, func(v *VoiceDef) *float64 { return &v.DecayTime }},
	{"decay_track", 0, 1.5, false, func(v *VoiceDef) *float64 { return &v.DecayTrack }},
	{"damp_hz", 200, 8000, true, func(v *VoiceDef) *float64 { return &v.DampHz }},
	{"pair", 0, 10, false, func(v *VoiceDef) *float64 { return &v.Pair }},
	{"pair_level", 0, 0.6, false, func(v *VoiceDef) *float64 { return &v.PairLevel }},
	{"pair_slow", 0.1, 1, false, func(v *VoiceDef) *float64 { return &v.PairSlow }},
	{"hammer_hz", 500, 8000, true, func(v *VoiceDef) *float64 { return &v.HammerHz }},
	{"knock", 0, 0.5, false, func(v *VoiceDef) *float64 { return &v.Knock }},
	{"cutoff", 50, 10000, true, func(v *VoiceDef) *float64 { return &v.Cutoff }},
	{"key_track", 0, 8, false, func(v *VoiceDef) *float64 { return &v.KeyTrack }},
	{"env_amount", 0, 5000, false, func(v *VoiceDef) *float64 { return &v.EnvAmount }},
	{"env_decay", 0, 1, false, func(v *VoiceDef) *float64 { return &v.EnvDecay }},
	{"q", 0, 4, false, func(v *VoiceDef) *float64 { return &v.Q }},
	{"attack", 0.001, 1, true, func(v *VoiceDef) *float64 { return &v.Attack }},
	{"decay", 0.005, 2, true, func(v *VoiceDef) *float64 { return &v.Decay }},
	{"sustain", 0, 1, false, func(v *VoiceDef) *float64 { return &v.Sustain }},
	{"release", 0.005, 3, true, func(v *VoiceDef) *float64 { return &v.Release }},
	{"drive", 0, 4, false, func(v *VoiceDef) *float64 { return &v.Drive }},
	{"level", 0.1, 2, false, func(v *VoiceDef) *float64 { return &v.Level }},
}

// VoiceGeneCount is the length of a voice's gene vector
var VoiceGeneCount = len(voiceGenes)

func (g *voiceGene) get(v *VoiceDef) float64 {
	switch g.name {
	case "unison":
		return float64(v.Unison)
	case "partials":
		return float64(v.Partials)
	}
	return *g.at(v)
}

func (g *voiceGene) set(v *VoiceDef, x float64) {
	switch g.name {
	case "unison":
		v.Unison = int(math.Round(x))
	case "partials":
		v.Partials = int(math.Round(x))
	default:
		*g.at(v) = x
	}
}

// Genes is the voice's continuous parameters as a vector in [0, 1], each over its
// range, on a log scale where the ear hears one; the wave is not in it. A zero
// parameter the voice leaves unset reads as the range's floor.
func (d *VoiceDef) Genes() []float64 {
	out := make([]float64, len(voiceGenes))
	for i := range voiceGenes {
		g := &voiceGenes[i]
		x := min(max(g.get(d), g.min), g.max)
		if g.log {
			out[i] = math.Log(x/g.min) / math.Log(g.max/g.min)
		} else {
			out[i] = (x - g.min) / (g.max - g.min)
		}
	}
	return out
}

// VoiceFromGenes is Genes' inverse under a wave, each gene held to [0, 1]; the
// voice has no name.
func VoiceFromGenes(wave string, genes []float64) *VoiceDef {
	v := &VoiceDef{Wave: wave}
	for i := range voiceGenes {
		g := &voiceGenes[i]
		x := 0.0
		if i < len(genes) {
			x = min(max(genes[i], 0), 1)
		}
		if g.log {
			g.set(v, g.min*math.Pow(g.max/g.min, x))
		} else {
			g.set(v, g.min+x*(g.max-g.min))
		}
	}
	return v
}

// defaultVoice plays an instrument no voice defines: a plain sine.
var defaultVoice = VoiceDef{Name: "default", Wave: "sine", Unison: 1, Attack: 0.01, Decay: 0.1, Sustain: 0.5, Release: 0.2, Level: 0.6}

// Clone copies a voice an editor may mutate, audition and discard without
// touching the registered entry.
func (d *VoiceDef) Clone() *VoiceDef {
	if d == nil {
		return nil
	}
	c := *d
	return &c
}

// ValidateVoice rejects what would misbehave on the mixer goroutine: a drum's
// name, an unknown wave, a stack the voice's arrays cannot hold, a non-finite or
// runaway parameter.
func ValidateVoice(d *VoiceDef) error {
	if d == nil {
		return errors.New("voice: nil definition")
	}
	if d.Name == "" || len(d.Name) > MaxSoundNameLen || strings.ContainsAny(d.Name, "\x00\n\r\t") {
		return fmt.Errorf("voice %q: invalid name", d.Name)
	}
	e := func(f string, a ...any) error {
		return fmt.Errorf("voice %q: "+f, append([]any{d.Name}, a...)...)
	}
	if i, ok := model.InstrumentByName(d.Name); ok && i.IsDrum() {
		return e("names a drum")
	}
	switch d.Wave {
	case "", "sine", "saw", "square", "string", "modal":
	default:
		return e("unknown wave %q", d.Wave)
	}
	if d.Unison < 0 || d.Unison > maxUnison {
		return e("unison %d outside [0,%d]", d.Unison, maxUnison)
	}
	if d.Partials < 0 || d.Partials > maxPartials {
		return e("partials %d outside [0,%d]", d.Partials, maxPartials)
	}
	if d.Detune != 0 && (!finite(d.Detune) || d.Detune < 0.5 || d.Detune > 2) {
		return e("detune %v outside [0.5,2]", d.Detune)
	}
	for _, r := range []struct {
		n      string
		v, max float64
	}{
		{"sub", d.Sub, 1}, {"fm_ratio", d.FMRatio, 64}, {"fm_index", d.FMIndex, 64}, {"fm_decay", d.FMDecay, MaxSoundDuration},
		{"bright", d.Bright, 1}, {"ring", d.Ring, MaxSoundDuration},
		{"strike_pos", d.StrikePos, 0.5}, {"tilt", d.Tilt, 4}, {"inharm", d.Inharm, 0.1}, {"inharm_track", d.InharmTrack, 4},
		{"decay_time", d.DecayTime, MaxSoundDuration}, {"decay_track", d.DecayTrack, 4}, {"damp_hz", d.DampHz, nyquist},
		{"pair", d.Pair, 100}, {"pair_level", d.PairLevel, 1}, {"pair_slow", d.PairSlow, 1}, {"hammer_hz", d.HammerHz, nyquist}, {"knock", d.Knock, 1},
		{"cutoff", d.Cutoff, nyquist}, {"key_track", d.KeyTrack, 20}, {"env_amount", d.EnvAmount, nyquist}, {"env_decay", d.EnvDecay, MaxSoundDuration}, {"q", d.Q, 64},
		{"attack", d.Attack, MaxSoundDuration}, {"decay", d.Decay, MaxSoundDuration}, {"sustain", d.Sustain, 1}, {"release", d.Release, MaxSoundDuration},
		{"drive", d.Drive, 64}, {"level", d.Level, 4},
	} {
		if !finite(r.v) || r.v < 0 || r.v > r.max {
			return e("%s %v outside [0,%v]", r.n, r.v, r.max)
		}
	}
	return nil
}

// The voice registry: an instrument's voice by its InstrumentType, the drums' and
// an unset voice's nil. The model's tonal names hold their numbers from the start;
// a name a document adds takes the next.
var (
	voiceMu     sync.RWMutex
	voiceDefs   = make([]*VoiceDef, InstrumentCount)
	voiceByName = fixedVoiceNames()
)

func fixedVoiceNames() map[string]InstrumentType {
	m := make(map[string]InstrumentType, InstrumentCount)
	for i := InstrBass; i < InstrumentCount; i++ {
		m[i.String()] = i
	}
	return m
}

// RegisterVoice validates and installs a voice: a tonal instrument's name takes
// its place, a new name a new instrument. Voices are not frozen at Start as
// sounds are: the mixer's table grows with the command DefineVoice sends it.
func RegisterVoice(d *VoiceDef) (InstrumentType, error) {
	if err := ValidateVoice(d); err != nil {
		return 0, err
	}
	voiceMu.Lock()
	defer voiceMu.Unlock()
	id, ok := voiceByName[d.Name]
	if !ok {
		id = InstrumentType(len(voiceDefs))
		voiceDefs = append(voiceDefs, nil)
		voiceByName[d.Name] = id
	}
	voiceDefs[id] = d
	return id, nil
}

// VoiceDefByName returns the live registry entry, or nil. The pointer is the
// mixer's too: mutate a Clone, then DefineVoice it.
func VoiceDefByName(name string) *VoiceDef {
	voiceMu.RLock()
	defer voiceMu.RUnlock()
	if id, ok := voiceByName[name]; ok {
		return voiceDefs[id]
	}
	return nil
}

// RegisteredVoices snapshots the voices in instrument order, unset ones omitted.
func RegisteredVoices() []*VoiceDef {
	voiceMu.RLock()
	defer voiceMu.RUnlock()
	out := make([]*VoiceDef, 0, len(voiceDefs))
	for _, d := range voiceDefs {
		if d != nil {
			out = append(out, d)
		}
	}
	return out
}

// TonalInstruments lists the tonal instruments in order: the model's, then the
// voices documents added, defined or not.
func TonalInstruments() []InstrumentType {
	voiceMu.RLock()
	defer voiceMu.RUnlock()
	out := make([]InstrumentType, 0, len(voiceDefs)-int(InstrBass))
	for i := InstrBass; int(i) < len(voiceDefs); i++ {
		out = append(out, i)
	}
	return out
}

// InstrumentName is an instrument's name, a document's voice's for one it added.
func InstrumentName(i InstrumentType) string {
	if i >= 0 && i < InstrumentCount {
		return i.String()
	}
	voiceMu.RLock()
	defer voiceMu.RUnlock()
	if int(i) < len(voiceDefs) && voiceDefs[i] != nil {
		return voiceDefs[i].Name
	}
	return "unknown"
}

// instrumentKnown reports an instrument a pattern may name.
func instrumentKnown(i InstrumentType) bool {
	voiceMu.RLock()
	defer voiceMu.RUnlock()
	return i >= 0 && int(i) < len(voiceDefs)
}

// voiceTable snapshots the registry as a kit's table.
func voiceTable() []*VoiceDef {
	voiceMu.RLock()
	defer voiceMu.RUnlock()
	return slices.Clone(voiceDefs)
}

func resetVoiceRegistry() {
	voiceMu.Lock()
	voiceDefs = make([]*VoiceDef, InstrumentCount)
	voiceByName = fixedVoiceNames()
	voiceMu.Unlock()
}

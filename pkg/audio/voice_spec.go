package audio

import (
	"errors"
	"fmt"
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

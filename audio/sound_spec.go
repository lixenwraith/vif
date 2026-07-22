package audio

// Declarative sound specification.
//
// Conventions:
//   - Times are seconds, frequencies Hz, gains linear.
//   - A zero-valued field means "unset" and resolves to the neutral default
//     documented on the field (the SFXParams.norm convention: 0 -> 1.0).
//   - Order-sensitive structure lives in arrays, never maps, so Marshal
//     (which sorts keys) round-trips audio-identically.
//   - Source frequencies always track pitch variance; processor frequencies
//     only when track_pitch is set.

// SoundSpecFile is the root of a sound TOML document.
type SoundSpecFile struct {
	Include []string    `toml:"include,omitempty"`
	Sound   []*SoundDef `toml:"sound"`
}

// SoundDef is one renderable sound. Registration is by Name; a later
// definition replaces an earlier one wholesale, in place, keeping its SoundID.
type SoundDef struct {
	Name string `toml:"name"`

	// Desc is human annotation that survives Marshal. TOML comments do not:
	// the encoder emits from the decoded value and preserves neither comments nor
	// whitespace, so an editor's first save strips them. Anything an editor user
	// needs to see about a sound belongs here, not in a # comment.
	Desc string `toml:"desc,omitempty"`
	// Duration is the master buffer length. 0 derives it from the layers as
	// max(offset+length), which then requires every layer to set length.
	Duration float64 `toml:"duration,omitempty"`
	// Norm is the peak-normalization target; 0 = 0.9. Ignored when Raw.
	Norm float64 `toml:"norm,omitempty"`
	// Raw skips peak normalization; levels become the spec's responsibility.
	Raw bool `toml:"raw,omitempty"`
	// FixedLength pins every time value against SFXParams.Length and the
	// length variant walk. Pitch still varies.
	FixedLength bool `toml:"fixed_length,omitempty"`

	// Variants is the pre-rendered take count; 0 = SFXVariants.
	Variants int `toml:"variants,omitempty"`
	// PitchWalk / LengthWalk are peak-to-peak deviation across the variant
	// set; 0 = SFXPitchWalk / SFXDecayWalk.
	PitchWalk  float64 `toml:"pitch_walk,omitempty"`
	LengthWalk float64 `toml:"length_walk,omitempty"`

	Chain []Proc  `toml:"chain,omitempty"` // master chain, pre-normalization
	Bus   []Bus   `toml:"bus,omitempty"`
	Layer []Layer `toml:"layer"`
}

// Layer is one source through one processor chain, mixed into a bus.
type Layer struct {
	Name   string  `toml:"name,omitempty"`   // target for a later ref source
	Bus    string  `toml:"bus,omitempty"`    // "" = master
	Gain   float64 `toml:"gain,omitempty"`   // 0 = 1.0
	Offset float64 `toml:"offset,omitempty"` // seconds into the target buffer
	Length float64 `toml:"length,omitempty"` // 0 = duration - offset
	Source Source  `toml:"source"`
	Chain  []Proc  `toml:"chain,omitempty"`
}

// Bus is an intermediate sum with its own chain. Buses form a tree rooted at
// master; To names the parent. Required only when a nonlinear processor must
// see a partial sum.
type Bus struct {
	Name   string  `toml:"name"`
	To     string  `toml:"to,omitempty"` // "" = master
	Gain   float64 `toml:"gain,omitempty"`
	Offset float64 `toml:"offset,omitempty"`
	Chain  []Proc  `toml:"chain,omitempty"`
}

// Source is the tagged union of signal generators. Fields are shared across
// kinds where they mean the same thing; unrelated fields are rejected at load.
type Source struct {
	Kind string `toml:"kind"` // osc sweep fm noise impulse burst ref silence
	Wave string `toml:"wave,omitempty"`

	Freq    float64 `toml:"freq,omitempty"`
	FreqEnd float64 `toml:"freq_end,omitempty"` // sweep; 0 = Freq
	Curve   string  `toml:"curve,omitempty"`    // lin (default) | exp
	CurveK  float64 `toml:"curve_k,omitempty"`  // exp decay rate; 0 = 8

	Ratio      float64 `toml:"ratio,omitempty"`    // fm: modulator = Freq*Ratio
	ModFreq    float64 `toml:"mod_freq,omitempty"` // fm: absolute, overrides Ratio
	Index      float64 `toml:"index,omitempty"`
	IndexEnd   float64 `toml:"index_end,omitempty"`   // read only with IndexCurve
	IndexCurve string  `toml:"index_curve,omitempty"` // "" = constant index

	Density float64 `toml:"density,omitempty"` // impulse: P(spike) per sample

	Burst   *Burst `toml:"burst,omitempty"`
	Vibrato *LFO   `toml:"vibrato,omitempty"`

	Ref string `toml:"ref,omitempty"` // ref: name of an earlier layer
}

// Burst is a train of enveloped noise bursts.
type Burst struct {
	Count     int     `toml:"count,omitempty"`
	Len       float64 `toml:"len,omitempty"`
	Gap       float64 `toml:"gap,omitempty"`
	Tau       float64 `toml:"tau,omitempty"`        // per-burst decay; 0 = flat
	Decay     float64 `toml:"decay,omitempty"`      // amplitude drop per burst
	Jitter    float64 `toml:"jitter,omitempty"`     // gap randomization 0..1
	AmpJitter float64 `toml:"amp_jitter,omitempty"` // amplitude randomization 0..1
}

// LFO drives vibrato. Phase is in cycles: 0.25 makes the sine a cosine.
type LFO struct {
	Rate  float64 `toml:"rate,omitempty"`
	Depth float64 `toml:"depth,omitempty"`
	Phase float64 `toml:"phase,omitempty"`
}

// Proc is the tagged union of in-place buffer processors.
type Proc struct {
	Kind string `toml:"kind"` // lp hp bp sweepbp ar decay am ringmod shape clip gain

	Freq       float64 `toml:"freq,omitempty"`
	FreqEnd    float64 `toml:"freq_end,omitempty"` // sweepbp
	Q          float64 `toml:"q,omitempty"`        // 0 = 0.707
	TrackPitch bool    `toml:"track_pitch,omitempty"`

	Attack  float64 `toml:"attack,omitempty"`
	Release float64 `toml:"release,omitempty"`
	Tau     float64 `toml:"tau,omitempty"` // decay time constant

	Rate  float64 `toml:"rate,omitempty"`
	Depth float64 `toml:"depth,omitempty"`
	Phase float64 `toml:"phase,omitempty"`

	Drive  float64 `toml:"drive,omitempty"`
	Amount float64 `toml:"amount,omitempty"` // gain; 0 = 1.0
}

// Spec bounds. A spec is untrusted input from disk: these cap load-time
// structure and total render cost so Start cannot be stalled or OOMed.
const (
	MaxSoundDuration = 10.0
	MaxSoundLayers   = 32
	MaxSoundBuses    = 8
	MaxSoundChain    = 16
	MaxSoundVariants = 8
	MaxSoundNameLen  = 64
	MaxSoundIncludes = 32
	MaxIncludeDepth  = 8

	// maxRenderOps bounds sum(samples * (1+len(chain))) across every sound and
	// variant. ~67M sample-ops, roughly 4x the built-in set.
	maxRenderOps = 1 << 26
)

func (d *SoundDef) totalSeconds() float64 {
	if d.Duration > 0 {
		return d.Duration
	}
	var m float64
	for i := range d.Layer {
		if e := d.Layer[i].Offset + d.Layer[i].Length; e > m {
			m = e
		}
	}
	return m
}

func gainOf(g float64) float64 {
	if g == 0 {
		return 1.0
	}
	return g
}

func normOf(n float64) float64 {
	if n <= 0 {
		return 0.9
	}
	return n
}

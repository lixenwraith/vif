package audio

import (
	"errors"
	"fmt"
	"strings"
)

// Declarative pattern specification — the TOML face of Pattern/Track/Step.
//
// Two representations exist on purpose. Pattern is the render form: Instr is an
// InstrumentType read per event on the mixer goroutine. PatternDef is the
// authoring form: Instr is a name, every field carries a toml tag, and the key
// space survives enum reordering. Conversion is total in both directions and
// lossless except for PatternID, which is process-local and assigned at
// registration.

// PatternSpecFile is the root of a music TOML document.
type PatternSpecFile struct {
	Pattern []*PatternDef `toml:"pattern"`
}

// PatternDef is one authored pattern.
type PatternDef struct {
	Name  string     `toml:"name"`
	Desc  string     `toml:"desc,omitempty"`
	Steps int        `toml:"steps"`
	Track []TrackDef `toml:"track"`
}

// TrackDef is one instrument lane.
type TrackDef struct {
	Instr       string    `toml:"instr"`
	Desc        string    `toml:"desc,omitempty"`
	FollowChord bool      `toml:"follow_chord,omitempty"`
	Humanize    float64   `toml:"humanize,omitempty"` // 0 = quantized
	Event       []StepDef `toml:"event"`
}

// StepDef is one trigger. Pos and Vel are always emitted: position 0 is the
// downbeat and must round-trip, and an omitted velocity reads as silence.
type StepDef struct {
	Pos  int     `toml:"pos"`
	Vel  float64 `toml:"vel"`
	Deg  int     `toml:"deg,omitempty"`  // scale degree, tonal only
	Oct  int     `toml:"oct,omitempty"`  // octave offset, tonal only
	Dur  int     `toml:"dur,omitempty"`  // steps; 0 = 1
	Prob float64 `toml:"prob,omitempty"` // 0 or 1 = always
}

// Pattern converts an authored def to the render form, validating on the way.
// The result is safe to hand to the mixer goroutine.
func (d *PatternDef) Pattern() (*Pattern, error) {
	if d == nil {
		return nil, errors.New("pattern: nil definition")
	}
	if d.Name == "" {
		return nil, errors.New("pattern: empty name")
	}
	p := &Pattern{Name: d.Name, Desc: d.Desc, Steps: d.Steps}
	for i := range d.Track {
		td := &d.Track[i]
		instr, ok := InstrumentByName(td.Instr)
		if !ok {
			return nil, fmt.Errorf("pattern %q: track %d: unknown instrument %q", d.Name, i, td.Instr)
		}
		tr := Track{
			Instr:       instr,
			Desc:        td.Desc,
			FollowChord: td.FollowChord,
			Humanize:    td.Humanize,
		}
		if n := len(td.Event); n > 0 {
			tr.Events = make([]Step, n)
			for j := range td.Event {
				e := &td.Event[j]
				tr.Events[j] = Step{Pos: e.Pos, Vel: e.Vel, Deg: e.Deg, Oct: e.Oct, Dur: e.Dur, Prob: e.Prob}
			}
		}
		p.Tracks = append(p.Tracks, tr)
	}
	if err := ValidatePattern(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Def converts the render form back for marshaling or editing. ID is not
// represented: it is assigned at registration.
func (p *Pattern) Def() *PatternDef {
	if p == nil {
		return nil
	}
	d := &PatternDef{Name: p.Name, Desc: p.Desc, Steps: p.Steps}
	for i := range p.Tracks {
		tr := &p.Tracks[i]
		td := TrackDef{
			Instr:       tr.Instr.String(),
			Desc:        tr.Desc,
			FollowChord: tr.FollowChord,
			Humanize:    tr.Humanize,
		}
		if n := len(tr.Events); n > 0 {
			td.Event = make([]StepDef, n)
			for j := range tr.Events {
				e := &tr.Events[j]
				td.Event[j] = StepDef{Pos: e.Pos, Vel: e.Vel, Deg: e.Deg, Oct: e.Oct, Dur: e.Dur, Prob: e.Prob}
			}
		}
		d.Track = append(d.Track, td)
	}
	return d
}

// ValidatePattern rejects anything that could panic, stall, or produce a
// runaway voice on the mixer goroutine. It runs on the render form so it covers
// both TOML loading and in-memory construction by an editor.
//
// An empty Name is permitted: RegisterPattern already allows anonymous
// patterns. PatternDef.Pattern requires one, because a TOML pattern with no
// name cannot be overridden or referenced.
func ValidatePattern(p *Pattern) error {
	if p == nil {
		return errors.New("pattern: nil")
	}
	e := func(f string, a ...any) error {
		return fmt.Errorf("pattern %q: "+f, append([]any{p.Name}, a...)...)
	}
	if len(p.Name) > MaxPatternNameLen || strings.ContainsAny(p.Name, "\x00\n\r\t") {
		return e("invalid name")
	}
	if p.Steps <= 0 || p.Steps > MaxPatternLen {
		return e("steps %d outside [1,%d]", p.Steps, MaxPatternLen)
	}
	if len(p.Tracks) > MaxPatternTracks {
		return e("%d tracks, want at most %d", len(p.Tracks), MaxPatternTracks)
	}
	for i := range p.Tracks {
		tr := &p.Tracks[i]
		if tr.Instr < 0 || tr.Instr >= InstrumentCount {
			return e("track %d: instrument %d out of range", i, tr.Instr)
		}
		// TriggerStep computes rng.IntN(int(Humanize*HumanizeMaxDelaySamples)+1).
		// A negative Humanize makes that argument non-positive, which panics —
		// on the mixer goroutine, from data that came off disk.
		if !finite(tr.Humanize) || tr.Humanize < 0 || tr.Humanize > 1 {
			return e("track %d: humanize %v outside [0,1]", i, tr.Humanize)
		}
		if len(tr.Events) > MaxPatternEvents {
			return e("track %d: %d events, want at most %d", i, len(tr.Events), MaxPatternEvents)
		}
		for j := range tr.Events {
			ev := &tr.Events[j]
			if ev.Pos < 0 || ev.Pos >= p.Steps {
				return e("track %d event %d: pos %d outside [0,%d)", i, j, ev.Pos, p.Steps)
			}
			if !finite(ev.Vel) || ev.Vel < 0 || ev.Vel > 1 {
				return e("track %d event %d: vel %v outside [0,1]", i, j, ev.Vel)
			}
			if !finite(ev.Prob) || ev.Prob < 0 || ev.Prob > 1 {
				return e("track %d event %d: prob %v outside [0,1]", i, j, ev.Prob)
			}
			if ev.Dur < 0 || ev.Dur > MaxPatternLen {
				return e("track %d event %d: dur %d outside [0,%d]", i, j, ev.Dur, MaxPatternLen)
			}
			if ev.Deg < -MaxStepDegree || ev.Deg > MaxStepDegree {
				return e("track %d event %d: deg %d outside [%d,%d]", i, j, ev.Deg, -MaxStepDegree, MaxStepDegree)
			}
			if ev.Oct < MinStepOctave || ev.Oct > MaxStepOctave {
				return e("track %d event %d: oct %d outside [%d,%d]", i, j, ev.Oct, MinStepOctave, MaxStepOctave)
			}
		}
	}
	return nil
}

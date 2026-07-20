package audio

import (
	"fmt"

	"github.com/lixenwraith/toml"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

type stepDef struct {
	Pos  int     `toml:"pos"`
	Vel  float64 `toml:"vel"`
	Deg  int     `toml:"deg"`
	Oct  int     `toml:"oct"`
	Dur  int     `toml:"dur"`
	Prob float64 `toml:"prob"`
}

type trackDef struct {
	Instr       string    `toml:"instr"`
	FollowChord bool      `toml:"follow_chord"`
	Event       []stepDef `toml:"event"`
}

type patternDef struct {
	Name  string     `toml:"name"`
	Steps int        `toml:"steps"`
	Track []trackDef `toml:"track"`
}

type patternFile struct {
	Pattern []patternDef `toml:"pattern"`
}

var instrByName = map[string]core.InstrumentType{
	"kick": core.InstrKick, "hihat": core.InstrHihat, "snare": core.InstrSnare,
	"clap": core.InstrClap, "bass": core.InstrBass, "piano": core.InstrPiano,
	"pad": core.InstrPad,
}

// LoadPatternsTOML parses user pattern definitions
// Name collisions with built-ins override the built-in in place
func LoadPatternsTOML(data []byte) ([]*Pattern, error) {
	var pf patternFile
	if err := toml.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	out := make([]*Pattern, 0, len(pf.Pattern))
	for _, pd := range pf.Pattern {
		if pd.Name == "" || pd.Steps <= 0 || pd.Steps > parameter.MaxPatternLen {
			return nil, fmt.Errorf("pattern %q: invalid name or steps", pd.Name)
		}
		p := &Pattern{Name: pd.Name, Steps: pd.Steps}
		for _, td := range pd.Track {
			instr, ok := instrByName[td.Instr]
			if !ok {
				return nil, fmt.Errorf("pattern %q: unknown instrument %q", pd.Name, td.Instr)
			}
			tr := Track{Instr: instr, FollowChord: td.FollowChord}
			for _, sd := range td.Event {
				if sd.Pos < 0 || sd.Pos >= pd.Steps {
					return nil, fmt.Errorf("pattern %q: event pos %d out of range", pd.Name, sd.Pos)
				}
				tr.Events = append(tr.Events, Step{
					Pos: sd.Pos, Vel: sd.Vel, Deg: sd.Deg, Oct: sd.Oct, Dur: sd.Dur, Prob: sd.Prob,
				})
			}
			p.Tracks = append(p.Tracks, tr)
		}
		out = append(out, p)
	}
	return out, nil
}

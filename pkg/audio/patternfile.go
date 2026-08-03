package audio

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/lixenwraith/toml"
)

// LoadPatternDefs parses a music document into the authoring form without
// converting. Unknown keys are rejected: the decoder ignores them, which would
// turn "follow_chords" into a silent false. checkKeys reflects over the struct
// tags, so there is no schema to keep in sync.
func LoadPatternDefs(data []byte) ([]*PatternDef, error) {
	raw, err := toml.NewParser(data).Parse()
	if err != nil {
		return nil, fmt.Errorf("pattern spec: %w", err)
	}
	if err := checkKeys("", raw, reflect.TypeOf(PatternSpecFile{})); err != nil {
		return nil, fmt.Errorf("pattern spec: %w", err)
	}
	var f PatternSpecFile
	if err := toml.Decode(raw, &f); err != nil {
		return nil, fmt.Errorf("pattern spec: %w", err)
	}
	return f.Pattern, nil
}

// LoadPatternsTOML parses user pattern definitions into the render form. The
// returned slice holds every pattern that validated; err joins the ones that
// did not, so one bad pattern does not discard the file — parity with
// LoadSoundsTOML. Name collisions with built-ins override in place at
// RegisterPattern.
func LoadPatternsTOML(data []byte) ([]*Pattern, error) {
	defs, err := LoadPatternDefs(data)
	if err != nil {
		return nil, err
	}
	out := make([]*Pattern, 0, len(defs))
	var errs []error
	for _, d := range defs {
		p, err := d.Pattern()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, p)
	}
	return out, errors.Join(errs...)
}

// MarshalPatternDefs re-emits authored patterns as TOML. Arrays keep their
// order; scalar keys within a table are sorted. Reloading yields identical
// playback. Comments and whitespace are not preserved — annotation belongs in
// the desc fields, which are data.
func MarshalPatternDefs(defs []*PatternDef) ([]byte, error) {
	return toml.Marshal(PatternSpecFile{Pattern: defs})
}

// MarshalPatterns converts render-form patterns and emits them. Anonymous
// patterns are skipped: without a name they cannot be reloaded or overridden.
func MarshalPatterns(pats []*Pattern) ([]byte, error) {
	f := PatternSpecFile{Pattern: make([]*PatternDef, 0, len(pats))}
	for _, p := range pats {
		if p == nil || p.Name == "" {
			continue
		}
		f.Pattern = append(f.Pattern, p.Def())
	}
	return toml.Marshal(f)
}

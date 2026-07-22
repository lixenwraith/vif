package audio

import (
	"embed"
	"slices"
)

// Built-in specs ship as TOML rather than Go literals: they are the worked
// examples users copy, they exercise the loader on every Start, and they are
// the round-trip fixture. TestBuiltinSoundsRender buys off the loss of
// compile-time checking.
//
//go:embed builtin/*.toml
var builtinFS embed.FS

// builtinSoundFiles is the load order; a later file overrides an earlier one
// by name.
var builtinSoundFiles = []string{"builtin/sfx.toml", "builtin/drums.toml"}

// BuiltinSounds returns the embedded specs, parsed and validated. Editors use
// it to seed a document or to diff a user override against stock.
func BuiltinSounds() ([]*SoundDef, error) {
	return LoadSoundsFS(builtinFS, builtinSoundFiles...)
}

// BuiltinSoundFiles lists the embedded documents in load order.
func BuiltinSoundFiles() []string { return slices.Clone(builtinSoundFiles) }

// BuiltinSoundTOML returns a document's raw bytes. Editors that want to show
// the authored text use this rather than re-marshaling BuiltinSounds, which
// drops the comments.
func BuiltinSoundTOML(name string) ([]byte, error) { return builtinFS.ReadFile(name) }

func registerBuiltinSounds() error {
	defs, err := BuiltinSounds()
	if err != nil {
		return err
	}
	for _, d := range defs {
		if _, err := RegisterSound(d); err != nil {
			return err
		}
	}
	return nil
}

// BuiltinPatternDefs returns the built-in patterns in authoring form. Unlike
// BuiltinSounds it must register first: the built-ins are Go literals, not
// embedded TOML. Same preconditions as InitDefaultPatterns — setup goroutine,
// no mixer. Anonymous patterns are skipped; without a name they cannot be
// reloaded or overridden.
func BuiltinPatternDefs() []*PatternDef {
	InitDefaultPatterns()
	pats := RegisteredPatterns()
	out := make([]*PatternDef, 0, len(pats))
	for _, p := range pats {
		if p == nil || p.Name == "" {
			continue
		}
		out = append(out, p.Def())
	}
	return out
}

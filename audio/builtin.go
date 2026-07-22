package audio

import "embed"

// Built-in specs ship as TOML rather than Go literals: they are the worked
// examples users copy, they exercise the loader on every Start, and they are
// the round-trip fixture. TestBuiltinsLoadAndRender buys off the loss of
// compile-time checking.
//
//go:embed builtin/*.toml
var builtinFS embed.FS

func registerBuiltinSounds() error {
	defs, err := LoadSoundsFS(builtinFS, "builtin/sfx.toml", "builtin/drums.toml")
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

package event

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// DustSpawnEntry is a value type for batch dust spawning
type DustSpawnEntry struct {
	X     int                  `toml:"x"`
	Y     int                  `toml:"y"`
	Char  rune                 `toml:"char"`
	Level component.GlyphLevel `toml:"level"`
}

// FadeoutSpawnEntry is a value type for batch fadeout spawning
type FadeoutSpawnEntry struct {
	X       int
	Y       int
	Char    rune
	FgColor terminal.RGB
	BgColor terminal.RGB
}

// FlashSpawnEntry is a value type for batch flash spawning
type FlashSpawnEntry struct {
	X    int
	Y    int
	Char rune
}

// BlossomSpawnEntry is a value type for batch blossom spawning
type BlossomSpawnEntry struct {
	X             int
	Y             int
	Char          rune
	SkipStartCell bool
}

// DecaySpawnEntry is a value type for batch decay spawning
type DecaySpawnEntry struct {
	X             int
	Y             int
	Char          rune
	SkipStartCell bool
}
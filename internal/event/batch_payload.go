package event

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/component"
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
	FgColor color.RGB
	BgColor color.RGB
}

// FlashSpawnEntry is a value type for batch flash spawning
type FlashSpawnEntry struct {
	X    int
	Y    int
	Char rune
}

// ParticleSpawnEntry is a value type for behavior-selected batch particle spawning.
type ParticleSpawnEntry struct {
	Behavior      component.ParticleBehavior
	X             int
	Y             int
	Char          rune
	SkipStartCell bool
}

// ExplosionCenterEntry is one explosion center position
type ExplosionCenterEntry struct {
	X int `toml:"x"`
	Y int `toml:"y"`
}

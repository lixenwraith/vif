package event

import "github.com/lixenwraith/vif/internal/component"

// ParticleSpawnEntry is a value type for behavior-selected batch particle spawning.
type ParticleSpawnEntry struct {
	Behavior      component.ParticleBehavior
	X             int
	Y             int
	Char          rune
	Level         component.GlyphLevel
	SkipStartCell bool
}

// ExplosionCenterEntry is one explosion center position
type ExplosionCenterEntry struct {
	X int `toml:"x"`
	Y int `toml:"y"`
}

package parameter

import "github.com/lixenwraith/vi-fighter/pkg/audio/model"

// SoundSet is the game's sound-effect ID table. The zero value is model.SoundNone,
// which Play counts as a bad-ID rejection, so AudioService.Start treats an unresolved
// table as fatal. Written once in AudioService.Start before the tick and render
// goroutines exist, read-only afterward: no lock, nothing on the emit path.
type SoundSet struct {
	Error     model.SoundID
	Bell      model.SoundID
	Whoosh    model.SoundID
	Coin      model.SoundID
	Shield    model.SoundID
	Zap       model.SoundID
	Crackle   model.SoundID
	MetalHit  model.SoundID
	Explosion model.SoundID
	Bullet    model.SoundID
	Ring      model.SoundID
}

// Sfx is the resolved table. Emit sites read it at emit time, never cache a field
// at construction: manifest.BuildSystems runs before Hub.StartAll resolves it.
var Sfx SoundSet

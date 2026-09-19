package parameter

import "github.com/lixenwraith/vi-fighter/pkg/audio/model"

// SoundSet is the game's sound-effect ID table. The zero value is
// model.SoundNone throughout, which Play rejects — a build where ResolveSounds
// never ran, or where audio is disabled, is silent rather than broken.
//
// That silence is indistinguishable from a correctly muted engine at the emit
// site: AudioEngine.Play discards a SoundNone before any counter, so nothing
// in played/dropped moves. AudioService.Start therefore treats an unresolved
// table as fatal, and sfx_test.go asserts every field has a soundTable row.
//
// Write/read discipline: written once on the wiring goroutine in
// AudioService.Start, read-only afterward from the tick and render goroutines,
// which are created later. Happens-before via goroutine creation; no lock, no
// atomic, nothing on the emit path.
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

// Sfx is the resolved table. Emit sites read parameter.Sfx.Error.
//
// Systems must read through this variable at emit time, not cache a field into
// their own struct at construction: manifest.BuildSystems runs during
// App.init, before Hub.StartAll resolves the table.
var Sfx SoundSet

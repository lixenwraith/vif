package component

import (
	"slices"

	"github.com/lixenwraith/color"
)

// DeathEffect is the visual a death leaves where the dead stood
type DeathEffect uint8

const (
	DeathSilent   DeathEffect = iota
	DeathFlash                // each glyph lit at its cell in the removal flash's color
	DeathFadeout              // each cell fading out in its own colors
	DeathScatter              // each glyph thrown out from the body's middle, fading
	DeathParticle             // each glyph a particle, of the behavior its request names
	DeathEffectCount
)

// DeathEffectNames are what a document calls each DeathEffect
var DeathEffectNames = [DeathEffectCount]string{
	DeathSilent:   "none",
	DeathFlash:    "flash",
	DeathFadeout:  "fadeout",
	DeathScatter:  "scatter",
	DeathParticle: "particle",
}

// EffectCell is one cell an effect draws: its glyph in Fg, over Bg where Solid. A
// zero Rune is a background alone.
type EffectCell struct {
	X, Y   int
	Rune   rune
	Fg, Bg color.RGB
	Solid  bool
}

// FlashComponent holds the cells that flash away together, each one's glyph lit in
// the removal flash's color; its deadline times the flash
type FlashComponent struct {
	Cells []EffectCell
}

// DetachSnapshot returns a copy whose cells share no storage with this one;
// FlashSystem appends to them in place. See HeaderComponent.DetachSnapshot.
func (f FlashComponent) DetachSnapshot() FlashComponent {
	f.Cells = slices.Clone(f.Cells)
	return f
}

// FadeoutComponent holds the cells that fade out together, each in its own colors;
// its deadline times the fade
type FadeoutComponent struct {
	Cells []EffectCell
}

// DetachSnapshot returns a copy whose cells share no storage with this one;
// FadeoutSystem appends to them in place. See HeaderComponent.DetachSnapshot.
func (f FadeoutComponent) DetachSnapshot() FadeoutComponent {
	f.Cells = slices.Clone(f.Cells)
	return f
}

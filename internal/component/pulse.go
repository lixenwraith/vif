package component

import (
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// PulseStyle is how a pulse is drawn
type PulseStyle uint8

const (
	PulseRing   PulseStyle = iota // ripples running out to its edge, over what it strikes
	PulseGlow                     // a glow filling its area, under what it strikes
	PulseHidden                   // not drawn
	PulseWave                     // a flat band running out behind its edge, over what it strikes
	PulseStyleCount
)

// PulseStyleNames are what a document calls each PulseStyle
var PulseStyleNames = [PulseStyleCount]string{PulseRing: "ring", PulseGlow: "glow", PulseHidden: "hidden", PulseWave: "wave"}

// PulseSpec is a pulse's shape: its size and life, how often it strikes, its look and
// the family it strikes under. A kind's row holds its own; a mount overrides it field
// by field, zero taking the kind's.
type PulseSpec struct {
	Radius      float64       // horizontal cells at full size; vertical is half at RefAspect
	Duration    time.Duration // from firing to its end
	Sustained   bool          // lasts while its host carries the mount instead
	Grow        time.Duration // swells from Start to full size over this; zero is full at once
	Start       float64       // the share of Radius a growing pulse fires at; zero is PulseGrowStart
	HitInterval time.Duration // between strikes; zero strikes once, as it fires
	Style       PulseStyle
	Attack      CombatAttackType // the family it strikes under; zero is its kind's
	Charged     bool             // strikes as its kind at the charge cap
}

// DeathWave is the pulse a death wave runs out as, its Radius the reach from its
// cursor to the map's far corner: it strikes every tick what its edge has passed
var DeathWave = PulseSpec{
	Duration: parameter.DeathWaveGrow + parameter.DeathWaveLinger, Grow: parameter.DeathWaveGrow,
	Start: parameter.DeathWaveStart, HitInterval: parameter.GameUpdateInterval,
	Style: PulseWave, Attack: CombatAttackDeathWave,
}

// PulseComponent is one pulse, an entity of its firer's domain: a cursor's stays on
// the cell it fired from, a mount's rides its Shared host and lasts while that mount
// names it Live. Its weapon makes it; the pulse system ages it, strikes and ends it.
type PulseComponent struct {
	PulseSpec
	X, Y    int         // its center
	Owner   core.Entity // the firing cursor, credited with its hits; zero for a mount's
	Anchor  core.Entity // the host it rides; zero stays where it fired
	Hostile bool        // strikes cursors for Damage; a cursor's resolves against species
	Damage  CursorDamage
	Palette WeaponPalette

	Elapsed  time.Duration // since firing, less any time its host held it
	HitTimer time.Duration // until its next strike
}

// Size is the share of Radius it has swollen to, which it draws and strikes at
func (p *PulseComponent) Size() float64 {
	if p.Grow <= 0 || p.Elapsed >= p.Grow {
		return 1
	}
	start := p.Start
	if start <= 0 {
		start = parameter.PulseGrowStart
	}
	return start + (1-start)*float64(p.Elapsed)/float64(p.Grow)
}

// Progress runs from zero as it fires to one as it ends; a sustained pulse stays midway
func (p *PulseComponent) Progress() float64 {
	if p.Sustained || p.Duration <= 0 {
		return 0.5
	}
	return min(float64(p.Elapsed)/float64(p.Duration), 1)
}

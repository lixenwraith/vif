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
	PulseStyleCount
)

// PulseStyleNames are what a document calls each PulseStyle
var PulseStyleNames = [PulseStyleCount]string{PulseRing: "ring", PulseGlow: "glow", PulseHidden: "hidden"}

// PulseSpec is a pulse's shape: its size and life, how often it strikes, its look and
// the family it strikes under. A kind's row holds its own; a mount overrides it field
// by field, zero taking the kind's.
type PulseSpec struct {
	Radius      float64       // horizontal cells at full size; vertical is half at RefAspect
	Duration    time.Duration // from firing to its end
	Sustained   bool          // lasts while its host carries the mount instead
	Grow        time.Duration // swells from PulseGrowStart to full size over this; zero is full at once
	HitInterval time.Duration // between strikes; zero strikes once, as it fires
	Style       PulseStyle
	Attack      CombatAttackType // the family it strikes under; zero is its kind's
	Charged     bool             // strikes as its kind at the charge cap
}

// PulseComponent is one pulse, an entity of its firer's domain: a cursor's stays on
// the cell it fired from, a mount's rides its Shared host and lasts while that mount
// names it Live. Its weapon makes it; the pulse system ages it, strikes and ends it.
type PulseComponent struct {
	PulseSpec
	X, Y    int         // its centre
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
	return parameter.PulseGrowStart + (1-parameter.PulseGrowStart)*float64(p.Elapsed)/float64(p.Grow)
}

// Progress runs from zero as it fires to one as it ends; a sustained pulse stays midway
func (p *PulseComponent) Progress() float64 {
	if p.Sustained || p.Duration <= 0 {
		return 0.5
	}
	return min(float64(p.Elapsed)/float64(p.Duration), 1)
}

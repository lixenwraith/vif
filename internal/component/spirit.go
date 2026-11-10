package component

import (
	"slices"

	"github.com/lixenwraith/color"
)

type SpiritColor int

// Colors
const (
	SpiritRed SpiritColor = iota
	SpiritOrange
	SpiritYellow
	SpiritGreen
	SpiritCyan
	SpiritBlue
	SpiritMagenta
	SpiritWhite
)

// SpiritColorNames are what a document calls each SpiritColor
var SpiritColorNames = [...]string{
	SpiritRed:     "red",
	SpiritOrange:  "orange",
	SpiritYellow:  "yellow",
	SpiritGreen:   "green",
	SpiritCyan:    "cyan",
	SpiritBlue:    "blue",
	SpiritMagenta: "magenta",
	SpiritWhite:   "white",
}

// SpiritPattern is how a flight's parts travel
type SpiritPattern uint8

const (
	SpiritConverge SpiritPattern = iota // spiralling in to their targets, trailing light
	SpiritScatter                       // thrown out from where they stood, fading there
)

// SpiritPart is one glyph of a flight, travelling between two precise positions in
// cells. Color is a scattered part's own; a converging one cycles its flight's.
type SpiritPart struct {
	FromX, FromY float64
	ToX, ToY     float64
	Rune         rune
	Color        color.RGB
}

// SpiritComponent is one flight: the parts that set off on one tick and travel as
// one, timed by the flight's deadline
type SpiritComponent struct {
	Pattern SpiritPattern
	// Color is the gradient a converging flight's parts cycle through
	Color SpiritColor
	Parts []SpiritPart

	// Species is the species the flight's arrival lands, its header on LandX, LandY;
	// SpeciesNone lands nothing
	Species      SpeciesType
	LandX, LandY int
}

// DetachSnapshot returns a copy whose parts share no storage with this one;
// SpiritSystem appends to them in place. See HeaderComponent.DetachSnapshot.
func (s SpiritComponent) DetachSnapshot() SpiritComponent {
	s.Parts = slices.Clone(s.Parts)
	return s
}

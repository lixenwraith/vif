package component

// SigilComponent provides visual representation for non-typeable moving entities
// Used by: DrainSystem, BlossomSystem, CleanerSystem, DecaySystem
type SigilComponent struct {
	Rune  rune
	Color SigilColor
}

// SigilColor represents the visual color category
type SigilColor int

const (
	SigilDrain   SigilColor = iota // Light Cyan/drain color
	SigilBlossom                   // Light Pink/blossom color
	SigilCleaner                   // Yellow or Purple/cleaner color
	SigilDecay                     // Dark Cyan/decay color
)
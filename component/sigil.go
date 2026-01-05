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
	SigilNugget     SigilColor = iota // Orange nugget color
	SigilDrain                        // Light Cyan/drain color
	SigilBlossom                      // Light Pink/blossom color
	SigilDecay                        // Dark Cyan/decay color
	SigilDustDark                     // Dark gray dust particle
	SigilDustNormal                   // Mid-gray dust particle
	SigilDustBright                   // Light gray dust particle
)
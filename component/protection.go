package component

// ProtectionFlags defines immunity to specific game mechanics
// Uses bitmask pattern for composable protection
type ProtectionFlags uint8

const (
	// ProtectNone provides no immunity (default)
	ProtectNone ProtectionFlags = 0

	// ProtectFromDecay makes entity immune to decay characters
	ProtectFromDecay ProtectionFlags = 1 << iota

	// ProtectFromDrain makes entity immune to energy drain mechanic
	ProtectFromDrain

	// ProtectFromDelete makes entity immune to delete operators
	ProtectFromDelete

	// ProtectFromDeath makes entity immune to death (e.g. out-of-bounds cleanup)
	ProtectFromDeath

	// ProtectAll makes entity completely indestructible
	// World.DestroyEntity() will reject destruction
	ProtectAll ProtectionFlags = 0xFF
)

// Has checks if a specific protection flag is set
func (p ProtectionFlags) Has(flag ProtectionFlags) bool {
	return p&flag == flag
}

// ProtectionComponent provides immunity to game mechanics
type ProtectionComponent struct {
	// Mask defines which mechanics this entity is immune to
	Mask ProtectionFlags
}
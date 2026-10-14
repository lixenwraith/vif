package component

// ProtectionFlags defines immunity to specific game mechanics
// Uses bitmask pattern for composable protection
type ProtectionFlags uint8

const (
	// ProtectNone provides no immunity (default)
	ProtectNone ProtectionFlags = 0

	// ProtectFromParticle makes an entity immune to decay and blossom particles.
	ProtectFromParticle ProtectionFlags = 1 << iota

	// ProtectFromSpecies makes entity immune to species interactions (e.g. destruction by collision)
	ProtectFromSpecies

	// ProtectFromDelete makes entity with glyph component immune to delete operators
	ProtectFromDelete

	// ProtectFromDeath makes entity immune to death (e.g. out-of-bounds cleanup)
	ProtectFromDeath

	// ProtectAll makes an entity immune to every protected mechanic and survives a
	// level clear; World.DestroyEntity does not check it, so its callers must
	ProtectAll ProtectionFlags = 0xFF
)

// ProtectionComponent provides immunity to game mechanics
type ProtectionComponent struct {
	// Mask defines which mechanics this entity is immune to
	Mask ProtectionFlags
}

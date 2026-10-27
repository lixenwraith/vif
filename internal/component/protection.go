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

	// ProtectFromWall leaves a glyph where rock or a wall arrives and lets it drift
	// into them, hidden behind them. Read on glyph paths only, as ProtectAll holds it
	ProtectFromWall

	// ProtectFromTyping takes a glyph out of typing's reach: typing, motions and bots
	// look through it. Read on glyph paths only, as ProtectAll holds it
	ProtectFromTyping

	// ProtectAll makes an entity immune to every protected mechanic and lets it
	// survive a level clear; World.DestroyEntity does not check it, so its callers must
	ProtectAll ProtectionFlags = 0xFF

	// ProtectScenery is a glyph placed as scenery: rock, walls, species, deletes and
	// typing pass it by, while particles act on it as on any glyph
	ProtectScenery = ProtectFromSpecies | ProtectFromDelete | ProtectFromWall | ProtectFromTyping
)

// ProtectionComponent provides immunity to game mechanics
type ProtectionComponent struct {
	// Mask defines which mechanics this entity is immune to
	Mask ProtectionFlags
}

package component

// WallBlockMask defines what entity types a wall blocks
type WallBlockMask uint8

const (
	WallBlockNone     WallBlockMask = 0
	WallBlockCursor   WallBlockMask = 1 << 0
	WallBlockKinetic  WallBlockMask = 1 << 1 // Drain, Swarm, Quasar
	WallBlockParticle WallBlockMask = 1 << 2 // Decay, Blossom, Dust
	WallBlockSpawn    WallBlockMask = 1 << 3 // All entity spawning
	WallBlockAll      WallBlockMask = 0xFF
)

// Has checks if specific block flag is set
func (m WallBlockMask) Has(flag WallBlockMask) bool {
	return m&flag != 0
}

// WallComponent marks an entity as a wall/obstacle
type WallComponent struct {
	BlockMask WallBlockMask // What this wall blocks
}
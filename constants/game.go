package constants

import "time"

// Game Loop Timing Constants
const (
	// FrameUpdateInterval is the rendering frame rate interval (~60 FPS)
	FrameUpdateInterval = 16 * time.Millisecond

	// GameUpdateInterval is the game logic update interval (clock tick)
	GameUpdateInterval = 50 * time.Millisecond

	// InitialSpawnDelay is the delay before first character spawn
	InitialSpawnDelay = 2 * time.Second
)

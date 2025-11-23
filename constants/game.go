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

// Event Queue Constants
const (
	// EventQueueSize is the fixed capacity of the event ring buffer
	EventQueueSize = 256

	// EventBufferMask is the bitmask for fast modulo operations (256 - 1)
	EventBufferMask = 255
)

// Cleaner System Constants
const (
	// CleanerDeduplicationWindow is the number of frames to keep in the spawned map
	// for preventing duplicate cleaner spawns from the same event
	CleanerDeduplicationWindow = 10
)

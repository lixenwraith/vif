package components

// CursorComponent marks an entity as the player cursor (singleton).
// Contains cursor-specific state that was previously in GameContext.
type CursorComponent struct {
	// ErrorFlashEnd is game time (UnixNano) when error flash expires.
	// Zero value means no flash active.
	ErrorFlashEnd int64

	// HeatDisplay is cached heat value for rendering optimization.
	// Updated by CursorSystem each tick from GameState.Heat.
	HeatDisplay int
}

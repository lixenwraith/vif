package components

// CleanerComponent represents a horizontal line-clearing animation entity.
// Cleaners are triggered when a gold sequence is completed while the heat meter
// is already at maximum. They sweep across rows containing Red characters,
// removing them on contact while leaving Blue/Green characters unaffected.
type CleanerComponent struct {
	Row            int       // Fixed Y position (the row being cleaned)
	XPosition      float64   // Current horizontal position (float for smooth sub-pixel movement)
	Speed          float64   // Movement speed in pixels per second
	Direction      int       // Movement direction: 1 for L→R, -1 for R→L
	TrailPositions []float64 // Recent X positions for fade trail effect (FIFO queue)
	TrailMaxAge    float64   // Maximum age in seconds for trail to fade completely
}

package components

type PingComponent struct {
	// Crosshair (Ping) State
	ShowCrosshair  bool
	CrosshairColor ColorClass // Resolves to RGB per player/team

	// Grid (PingGrid) State
	GridActive bool
	GridTimer  float64 // Remaining time in seconds
	GridColor  ColorClass

	// Rendering Hints
	ContextAware bool // Enables dynamic blending (Dark on text / Light on empty)
}
package component

// PingComponent is a cursor's guidance: the crosshair through its cell, and the
// bounds Visual mode moves within while its shield stands.
type PingComponent struct {
	ShowCrosshair bool

	// Half-extents around the cursor cell
	BoundsRadiusX int
	BoundsRadiusY int
	BoundsActive  bool
}

package components

// OutOfBoundsComponent tags an entity that is outside the valid game area
// Used by CullSystem to safely remove entities after game logic has had a chance to react
type OutOfBoundsComponent struct{}
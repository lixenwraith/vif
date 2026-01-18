package component

// KineticComponent provides a reusable kinematic container for entities requiring sub-pixel motion
// Uses Q32.32 fixed-point arithmetic for deterministic integration and high-performance physics updates
type KineticComponent struct {
	Kinetic // PreciseX/Y, VelX/Y, AccelX/Y (int64 Q32.32)
}
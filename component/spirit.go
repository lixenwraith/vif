package component

type SpiritColor int

// Colors
const (
	SpiritRed SpiritColor = iota
	SpiritOrange
	SpiritYellow
	SpiritGreen
	SpiritCyan
	SpiritBlue
	SpiritMagenta
	SpiritWhite
)

type SpiritPattern int

const (
	SpiritPatternSpiral SpiritPattern = iota // Current behavior
	SpiritPatternLinear                      // Direct line with fade
	SpiritPatternWave                        // Sinusoidal oscillation
	SpiritPatternBounce                      // Damped oscillation toward target
	SpiritPatternOrbit                       // Partial orbit before converging
)

// SpiritComponent represents a converging visual effect entity
// Positions presence is at StartX/StartY to avoid target saturation
// Actual render position is calculated via Lerp from Start to Target
type SpiritComponent struct {
	// Starting position in Q32.32 (where the spirit spawned)
	StartX, StartY int64

	// Target position in Q32.32 (convergence point)
	TargetX, TargetY int64

	// Animation progress in Q32.32: 0 = start, Scale = complete
	Progress int64

	Pattern   SpiritPattern
	Amplitude int64 // (Q32.32, for wave/bounce magnitude)
	Frequency int64 // (Q32.32, oscillation rate)

	// Speed increment per tick in Q32.32 (distance-dependent)
	Speed int64

	// Total rotation angle in Q32.32 radians (Scale = 2pi)
	// Positive = CW, Negative = CCW
	Spin int64

	// Visual properties
	Rune       rune
	BaseColor  SpiritColor
	BlinkColor SpiritColor
}
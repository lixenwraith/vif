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
	// Starting precise position in cells (where the spirit spawned)
	StartX, StartY float64

	// Target precise position in cells (convergence point)
	TargetX, TargetY float64

	// Animation progress: 0.0 = start, 1.0 = complete
	Progress float64

	Pattern   SpiritPattern
	Amplitude float64 // Wave/bounce magnitude in cells
	Frequency float64 // Oscillation rate

	// Progress increment per tick (distance-independent)
	Speed float64

	// Total rotation angle in radians
	// Positive = CW, Negative = CCW
	Spin float64

	// Visual properties
	Rune       rune
	BaseColor  SpiritColor
	BlinkColor SpiritColor
}

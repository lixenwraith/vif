package profile

// MassF is an entity's collision mass in relative units
// Baseline: a single-cell entity is 1.0
type MassF = float64

// Entity masses; mirrors the Q32.32 table in mass.go
const (
	MassDustF      MassF = 0.1
	MassCursorF    MassF = 1.0
	MassCleanerF   MassF = 1.0
	MassDrainF     MassF = 1.0
	MassSwarmF     MassF = 2.0
	MassEyeF       MassF = 5.0
	MassSnakeBodyF MassF = 2.0
	MassSnakeHeadF MassF = 8.0
	MassQuasarF    MassF = 10.0
	MassExplosionF MassF = 10.0
	MassStormF     MassF = 100.0
	MassPylonF     MassF = 1000.0
)

// Mass ratio clamp; see mass.go for rationale
const (
	MassRatioMinF = 1.0 / 64.0
	MassRatioMaxF = 16.0
	SoftRatioMaxF = 2.0
)

// softRatioF is massRatioF under the tighter scatter bound
func softRatioF(impactor, target MassF) float64 {
	r := massRatioF(impactor, target)
	if r > SoftRatioMaxF {
		return SoftRatioMaxF
	}
	return r
}

// massRatioF returns impactor/target mass clamped to the usable band
func massRatioF(impactor, target MassF) float64 {
	r := impactor / target
	if r < MassRatioMinF {
		return MassRatioMinF
	}
	if r > MassRatioMaxF {
		return MassRatioMaxF
	}
	return r
}

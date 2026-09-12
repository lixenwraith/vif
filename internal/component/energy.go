package component

// EnergyComponent holds the energy state and visual blink state
type EnergyComponent struct {
	Current int64
}

// EnergyDeltaType identifies type of energy modification that should be applied
type EnergyDeltaType int

const (
	EnergyDeltaPenalty EnergyDeltaType = iota // Moves magnitude toward zero and clamps there
	EnergyDeltaReward                         // Grows magnitude without changing polarity
	EnergyDeltaSpend                          // Moves toward zero; an oversized spend can cross it
	EnergyDeltaPassive                        // Penalty that bypasses protection and clamps at zero
)

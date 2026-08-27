package component

// EnergyComponent holds the energy state and visual blink state
type EnergyComponent struct {
	Current int64
}

// EnergyDeltaType identifies type of energy modification that should be applied
type EnergyDeltaType int

const (
	EnergyDeltaPenalty EnergyDeltaType = iota // Penalties from interactions, absolute value decrease, clamp to zero
	EnergyDeltaReward                         // Reward from actions, absolute value increase
	EnergyDeltaSpend                          // Energy spent, convergent to zero and can cross zero
	EnergyDeltaPassive                        // Passive drain, bypasses ember/boost, convergent clamp to zero
)

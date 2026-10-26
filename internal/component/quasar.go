package component

import (
	"time"

	"github.com/lixenwraith/vif/pkg/machine"
)

// QuasarState is a quasar's place in its behavior machine
type QuasarState uint8

const (
	QuasarHoming   QuasarState = iota // tracks the nearest cursor inside its zap circle
	QuasarCharging                    // the cursor left the circle; the splash counts down to the zap
	QuasarZapping                     // lightning on the nearest cursor, shielded
)

// QuasarComponent holds quasar-specific runtime state, composite structure managed via HeaderComponent
type QuasarComponent struct {
	Machine machine.Instance[QuasarState]

	SpeedMultiplier float64       // Current speed scale factor (starts at 1.0)
	SpeedElapsed    time.Duration // Movement time toward the next speed increase

	IsShielded bool // Written from the machine's flags each step: damage and stun immunity, mirrored by the shield component

	// Dynamic resize support
	ZapRadius float64 // Visual radius of zap circle in cells (dynamic on resize)
}

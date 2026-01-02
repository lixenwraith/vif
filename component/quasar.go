package component

import (
	"time"
)

// @lixen: #dev{feature[dust(render,system)]}

// QuasarChars defines the 3×5 visual representation
var QuasarChars = [3][5]rune{
	{'╔', '═', '╦', '═', '╗'},
	{'╠', '═', '╬', '═', '╣'},
	{'╚', '═', '╩', '═', '╝'},
}

// QuasarComponent holds quasar-specific runtime state
// Composite structure managed via CompositeHeaderComponent
type QuasarComponent struct {
	KineticState // PreciseX/Y, VelX/Y, AccelX/Y (Q16.16)

	DeflectUntil        time.Time // Immunity from homing/drag after cleaner hit
	LastSpeedIncreaseAt time.Time // For periodic speed scaling

	SpeedMultiplier int32 // Q16.16, current speed scale factor (starts at Scale)

	IsOnCursor bool // True if any member overlaps cursor position
	IsZapping  bool // True if zapping cursor outside range

	// Charge phase state (delay before zapping)
	IsCharging      bool
	ChargeRemaining time.Duration
	ShieldActive    bool // Cleaner immunity during charge

	HitPoints         int
	HitFlashRemaining time.Duration
}
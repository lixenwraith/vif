package component

import (
	"time"
)

// QuasarChars defines the 3×5 visual representation
var QuasarChars = [3][5]rune{
	{'╔', '═', '╦', '═', '╗'},
	{'╠', '═', '╬', '═', '╣'},
	{'╚', '═', '╩', '═', '╝'},
}

// QuasarComponent holds quasar-specific runtime state, composite structure managed via HeaderComponent
type QuasarComponent struct {
	LastSpeedIncreaseAt time.Time // For periodic speed scaling

	SpeedMultiplier int64 // Q32.32, current speed scale factor (starts at Scale)

	// Quasar state
	IsZapping  bool // True if zapping cursor outside range
	IsCharging bool // True if charging to zap with cursor outside range
	IsShielded bool // True if shielded, equal to IsZapping, for render and future potential use

	// Charge phase state (delay before zapping)
	ChargeRemaining time.Duration

	// Dynamic resize support
	ZapRadius int64 // Q32.32, visual radius of zap circle (dynamic on resize)
}
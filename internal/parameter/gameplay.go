package parameter

import "time"

// Heat System
const (
	// HeatMax is the maximum value for the heat meter (100%)
	HeatMax = 100

	// HeatMaxOverheat is the maximum overheat to trigger overheat event
	HeatMaxOverheat = HeatMax // currently double heat amount

	// HeatTypingErrorPenalty is the heat penalty when wrong character typed in insert mode
	HeatTypingErrorPenalty = 10

	SpecialAttackHeatCost = 1

	// HeatBurstFlashDuration is the time burst flash indicator is shown on heat bar
	HeatBurstFlashDuration = 150 * time.Millisecond

	EmberDecayInterval = 250 * time.Millisecond
	EmberDecayAmount   = 1
)

// Energy System
const (
	// EnergyBlinkTimeout is the duration for energy blink
	EnergyBlinkTimeout = 200 * time.Millisecond

	// ErrorBlinkTimeout is the duration for error cursor flash
	ErrorBlinkTimeout = 200 * time.Millisecond
)

// Glyph Energy
const (
	EnergyBaseBlue  = 2
	EnergyBaseGreen = 1
	EnergyBaseRed   = -2
)

// Boost Mechanics
const (
	// BoostBaseDuration is the initial duration when boost is activated
	BoostBaseDuration = 500 * time.Millisecond

	// BoostExtensionDuration is how long each matching color character extends the boost
	BoostExtensionDuration = 500 * time.Millisecond
)

// God Mode
const (
	GodEnergyAmount = 100_000_000_000
)

// Character Spawn Logic
const (
	SpawnIntervalMs   = 1000
	MaxPlacementTries = 3
)

// Spawn Exclusion Zones
const (
	// CursorExclusionX is horizontal distance from cursor that blocks spawn
	CursorExclusionX = 5
	// CursorExclusionY is vertical distance from cursor that blocks spawn
	CursorExclusionY = 3
)

// Spawn Rate Management
const (
	// SpawnDensityLowThreshold is the entity density below which spawn rate increases
	SpawnDensityLowThreshold = 0.1 // Below: 2x spawn rate

	// SpawnDensityHighThreshold is the entity density above which spawn rate decreases
	SpawnDensityHighThreshold = 0.25 // Above: 0.5x spawn rate

	// Spawn rate multipliers
	SpawnRateFast   = 2.0  // Used when density < low threshold
	SpawnRateNormal = 1.0  // Used when density is between thresholds
	SpawnRateSlow   = 0.02 // Used when density > high threshold
)

// Positions Finding
const (
	// DrainSpawnMaxRetries is the maximum number of retries for finding valid drain spawn position
	DrainSpawnMaxRetries = 10

	// GoldSpawnMaxAttempts is the maximum number of attempts to find valid gold sequence position
	GoldSpawnMaxAttempts = 100

	// MotionJumpSearchMaxRadius bounds the fallback search for an unblocked map
	// origin, centre, or end target.
	MotionJumpSearchMaxRadius = 20
)

// Macro and Mouse
const (
	// MacroPlaybackInterval is delay between each macro intent during playback
	MacroPlaybackInterval = WeaponCooldownMain

	// MouseRepeatInterval is the delay between mouse events when click button is held
	MouseRepeatInterval = WeaponCooldownMain

	// AutoFireInterval is the delay between fire requests while auto-fire is active
	AutoFireInterval = WeaponCooldownMain

	// MenuHoldDuration is how long a left press held on the status bar summons the
	// configuration menu, as Ctrl-G does: a hold is input, so it runs on the wall clock [wall]
	MenuHoldDuration = 600 * time.Millisecond

	// MenuHoldSlack is how many cells a held press may drift and still be the same hold
	MenuHoldSlack = 1

	// MouseDoubleClickWindow is how soon a second left press on the same cell fires
	// special as well, for a finger with no right button; Android's own double-tap
	// timeout, and a cell apart is play however fast [wall]
	MouseDoubleClickWindow = 300 * time.Millisecond
)

// Initial input state, applied once at GameContext construction, changed state persist through game reset
const (
	// DefaultMouseFreeMode enables cursor tracking of bare mouse motion at startup
	DefaultMouseFreeMode = true
)

// Buttons: one row a scenario lays across the middle of the map
const (
	ButtonGap      = 2  // columns between neighbours and at each end of the row
	ButtonMaxWidth = 16 // a wide map leaves the row centred rather than stretched

	// A button takes up to a ButtonHeightShare-th of the map's height, enough at 80x24
	// for a splash digit's nine ink rows; ButtonMaxHeight leaves a row above and below
	ButtonHeightShare = 2
	ButtonMaxHeight   = 11

	// ButtonPressLight lights a pressed button; a pointer press holds it lit until release
	ButtonPressLight = 300 * time.Millisecond
)

package event

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// DeleteRangeType defines the scope of deletion
type DeleteRangeType int

const (
	DeleteRangeChar DeleteRangeType = iota
	DeleteRangeLine
)

// DeleteRequestPayload contains coordinates for deletion
type DeleteRequestPayload struct {
	RangeType DeleteRangeType
	StartX    int
	EndX      int
	StartY    int
	EndY      int
}

// ShieldDrainPayload contains energy drain amount from external sources
type ShieldDrainPayload struct {
	Amount int
}

// DirectionalCleanerPayload contains origin for 4-way cleaner spawn
type DirectionalCleanerPayload struct {
	OriginX int
	OriginY int
}

// CharacterTypedPayload captures keypress state for EnergySystem
type CharacterTypedPayload struct {
	Char rune
	X    int // Cursor position when typed
	Y    int
}

// CharacterTypedPayloadPool reduces GC pressure during high-frequency typing
var CharacterTypedPayloadPool = sync.Pool{
	New: func() any { return &CharacterTypedPayload{} },
}

// EnergyAddPayload contains energy delta
type EnergyAddPayload struct {
	Delta      int
	Spend      bool // True: bypasses boost protection
	Convergent bool // True: clamp at zero, cannot cross
}

// EnergySetPayload contains energy value
type EnergySetPayload struct {
	Value int
}

// EnergyBlinkPayload triggers visual blink state
type EnergyBlinkPayload struct {
	Type  uint32 // 0=error, 1=blue, 2=green, 3=red, 4=gold
	Level uint32 // 0=dark, 1=normal, 2=bright
}

// HeatAddPayload contains heat delta
type HeatAddPayload struct {
	Delta int
}

// TODO: add toml tags to all payloads so they can be used in FSM EmitEvent actions
// HeatSetPayload contains absolute heat value
type HeatSetPayload struct {
	Value int `toml:"value"`
}

// GoldEnablePayload controls gold spawning eligibility
type GoldEnablePayload struct {
	Enabled bool
}

// GoldSpawnedPayload anchors countdown timer to sequence position
type GoldSpawnedPayload struct {
	AnchorEntity core.Entity // Phantom head for entity-anchored timer
	OriginX      int
	OriginY      int
	Length       int
	Duration     time.Duration
}

// GoldCompletionPayload identifies which timer to destroy
type GoldCompletionPayload struct {
	AnchorEntity core.Entity
}

// SplashRequestPayload creates transient visual flash
type SplashRequestPayload struct {
	Text    string
	Color   component.SplashColor
	OriginX int // Origin position (usually cursor)
	OriginY int
}

// PingGridRequestPayload carries configuration for the ping grid activation
type PingGridRequestPayload struct {
	Duration time.Duration
}

// SpawnChangePayload carries configuration for spawn state
type SpawnChangePayload struct {
	Enabled bool
}

// TimerStartPayload configuration for a new lifecycle timer
type TimerStartPayload struct {
	Entity   core.Entity
	Duration time.Duration
}

// BoostActivatePayload contains boost activation parameters
type BoostActivatePayload struct {
	Duration time.Duration
}

// BoostExtendPayload contains boost extension parameters
type BoostExtendPayload struct {
	Duration time.Duration
}

// MaterializeRequestPayload contains parameters to start a visual spawn sequence
type MaterializeRequestPayload struct {
	X    int
	Y    int
	Type component.SpawnType
}

// SpawnCompletePayload carries details about a completed materialization
type SpawnCompletePayload struct {
	X    int
	Y    int
	Type component.SpawnType
}

// FlashRequestPayload contains parameters for destruction flash effect
type FlashRequestPayload struct {
	X    int
	Y    int
	Char rune
}

// NuggetCollectedPayload signals successful nugget collection
type NuggetCollectedPayload struct {
	Entity core.Entity
}

// NuggetDestroyedPayload signals external nugget destruction
type NuggetDestroyedPayload struct {
	Entity core.Entity
}

// SoundRequestPayload contains the sound type to play
type SoundRequestPayload struct {
	SoundType core.SoundType
}

// DeathRequestPayload contains batch death request
// EffectEvent: 0 = silent death, EventFlashRequest = flash, future: explosion, chain death
type DeathRequestPayload struct {
	Entities    []core.Entity
	EffectEvent EventType
}

// NetworkConnectPayload signals peer connection
type NetworkConnectPayload struct {
	PeerID uint32
}

// NetworkDisconnectPayload signals peer disconnection
type NetworkDisconnectPayload struct {
	PeerID uint32
}

// RemoteInputPayload contains input data from remote player
type RemoteInputPayload struct {
	PeerID  uint32
	Payload []byte // Encoded keystroke/intent
}

// StateSyncPayload contains state snapshot from peer
type StateSyncPayload struct {
	PeerID  uint32
	Seq     uint32
	Payload []byte // Encoded snapshot
}

// NetworkEventPayload contains a forwarded game event
type NetworkEventPayload struct {
	PeerID  uint32
	Payload []byte // Encoded GameEvent
}

// NetworkErrorPayload contains error information
type NetworkErrorPayload struct {
	PeerID uint32 // 0 for general errors
	Error  string
}

// MemberTypedPayload signals a composite member was typed
type MemberTypedPayload struct {
	AnchorID       core.Entity
	MemberEntity   core.Entity
	Char           rune
	RemainingCount int // Count of remaining live members after this one
}

// DecaySpawnPayload contains parameters to spawn a single decay entity
type DecaySpawnPayload struct {
	X             int
	Y             int
	Char          rune
	SkipStartCell bool // True: particle skips interaction at spawn position
}

// BlossomSpawnPayload contains parameters to spawn a single blossom entity
type BlossomSpawnPayload struct {
	X             int
	Y             int
	Char          rune
	SkipStartCell bool // True: particle skips interaction at spawn position
}

// CursorMovedPayload signals cursor position change for magnifier updates
type CursorMovedPayload struct {
	X int
	Y int
}

// QuasarSpawnedPayload contains quasar spawn data
type QuasarSpawnedPayload struct {
	AnchorEntity core.Entity
	OriginX      int
	OriginY      int
}

// SpiritSpawnPayload contains parameters to spawn a spirit entity
type SpiritSpawnPayload struct {
	StartX, StartY   int // Starting position (grid coordinates)
	TargetX, TargetY int // Target convergence position (grid coordinates)
	Char             rune
	BaseColor        terminal.RGB
	BlinkColor       terminal.RGB
}

// LightningSpawnPayload contains parameters to spawn a lightning entity
type LightningSpawnPayload struct {
	// TODO: this assumes one lightning per entity, change later to allow multiple
	Owner            core.Entity // Owning entity for lifecycle (required for tracked)
	OriginX, OriginY int
	TargetX, TargetY int
	ColorType        component.LightningColorType
	Duration         time.Duration
	Tracked          bool // If true, entity persists and target can be updated
}

// LightningUpdatePayload updates target position for tracked lightning
type LightningUpdatePayload struct {
	Owner            core.Entity
	TargetX, TargetY int
}

// QuasarChargeStartPayload contains parameters for quasar charge countdown
type QuasarChargeStartPayload struct {
	AnchorEntity core.Entity
	Duration     time.Duration
}

// QuasarChargeCancelPayload identifies which charge timer to destroy
type QuasarChargeCancelPayload struct {
	AnchorEntity core.Entity
}
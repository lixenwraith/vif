package event

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// TODO: future multi-level / network
// WorldClearPayload contains parameters for mass entity cleanup
type WorldClearPayload struct {
	KeepProtected bool   `toml:"keep_protected"` // Preserve entities with ProtectAll
	KeepCursor    bool   `toml:"keep_cursor"`    // Preserve cursor entity explicitly
	RegionTag     string `toml:"region_tag"`     // If set, only clear entities tagged with this
}

// TODO: future multi-level, debug
// SystemTogglePayload contains parameters for system activation control
type SystemTogglePayload struct {
	System string `toml:"system"` // System registry name
	Active bool   `toml:"active"` // Target activation state
}

// DeleteRangeType defines the scope of deletion
type DeleteRangeType int

const (
	DeleteRangeChar DeleteRangeType = iota
	DeleteRangeLine
)

// DeleteRequestPayload contains coordinates for deletion
type DeleteRequestPayload struct {
	RangeType DeleteRangeType `toml:"range_type"`
	StartX    int             `toml:"start_x"`
	EndX      int             `toml:"end_x"`
	StartY    int             `toml:"start_y"`
	EndY      int             `toml:"end_y"`
}

// ShieldDrainPayload contains energy drain amount from external sources
type ShieldDrainPayload struct {
	Amount int `toml:"amount"`
}

// DirectionalCleanerPayload contains origin for 4-way cleaner spawn
type DirectionalCleanerPayload struct {
	OriginX int `toml:"origin_x"`
	OriginY int `toml:"origin_y"`
}

// CharacterTypedPayload captures keypress and cursor state when character is typed
type CharacterTypedPayload struct {
	Char rune `toml:"char"`
	X    int  `toml:"x"`
	Y    int  `toml:"y"`
}

// CharacterTypedPayloadPool reduces GC pressure during high-frequency typing
var CharacterTypedPayloadPool = sync.Pool{
	New: func() any { return &CharacterTypedPayload{} },
}

// EnergyAddPayload contains energy delta
type EnergyAddPayload struct {
	Delta      int  `toml:"delta"`
	Spend      bool `toml:"spend"`      // True: bypasses boost protection
	Convergent bool `toml:"convergent"` // True: clamp at zero, cannot cross
}

// EnergySetPayload contains energy value
type EnergySetPayload struct {
	Value int `toml:"value"`
}

// GlyphConsumedPayload contains glyph data for centralized energy calculation
type GlyphConsumedPayload struct {
	Type  component.GlyphType  `toml:"type"`
	Level component.GlyphLevel `toml:"level"`
}

// EnergyBlinkPayload triggers visual blink state
type EnergyBlinkPayload struct {
	Type  uint32 `toml:"type"`  // 0=error, 1=blue, 2=green, 3=red, 4=gold
	Level uint32 `toml:"level"` // 0=dark, 1=normal, 2=bright
}

// HeatAddPayload contains heat delta
type HeatAddPayload struct {
	Delta int `toml:"delta"`
}

// HeatSetPayload contains absolute heat value
type HeatSetPayload struct {
	Value int `toml:"value"`
}

// GoldEnablePayload controls gold spawning eligibility
type GoldEnablePayload struct {
	Enabled bool `toml:"enabled"`
}

// GoldSpawnedPayload anchors countdown timer to sequence position
type GoldSpawnedPayload struct {
	HeaderEntity core.Entity   `toml:"header_entity"`
	OriginX      int           `toml:"origin_x"`
	OriginY      int           `toml:"origin_y"`
	Length       int           `toml:"length"`
	Duration     time.Duration `toml:"duration"`
}

// GoldCompletionPayload identifies which timer to destroy
type GoldCompletionPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
}

// SplashRequestPayload creates transient visual flash
type SplashRequestPayload struct {
	Text    string                `toml:"text"`
	Color   component.SplashColor `toml:"color"`
	OriginX int                   `toml:"origin_x"`
	OriginY int                   `toml:"origin_y"`
}

// PingGridRequestPayload carries configuration for the ping grid activation
type PingGridRequestPayload struct {
	Duration time.Duration `toml:"duration"`
}

// SpawnChangePayload carries configuration for spawn state
type SpawnChangePayload struct {
	Enabled bool `toml:"enabled"`
}

// TimerStartPayload configuration for a new lifecycle timer
type TimerStartPayload struct {
	Entity   core.Entity   `toml:"entity"`
	Duration time.Duration `toml:"duration"`
}

// BoostActivatePayload contains boost activation parameters
type BoostActivatePayload struct {
	Duration time.Duration `toml:"duration"`
}

// BoostExtendPayload contains boost extension parameters
type BoostExtendPayload struct {
	Duration time.Duration `toml:"duration"`
}

// MaterializeRequestPayload contains parameters to start a visual spawn sequence
type MaterializeRequestPayload struct {
	X    int                 `toml:"x"`
	Y    int                 `toml:"y"`
	Type component.SpawnType `toml:"type"`
}

// SpawnCompletePayload carries details about a completed materialization
type SpawnCompletePayload struct {
	X    int                 `toml:"x"`
	Y    int                 `toml:"y"`
	Type component.SpawnType `toml:"type"`
}

// FlashRequestPayload contains parameters for destruction flash effect
type FlashRequestPayload struct {
	X    int  `toml:"x"`
	Y    int  `toml:"y"`
	Char rune `toml:"char"`
}

// NuggetCollectedPayload signals successful nugget collection
type NuggetCollectedPayload struct {
	Entity core.Entity `toml:"entity"`
}

// NuggetDestroyedPayload signals external nugget destruction
type NuggetDestroyedPayload struct {
	Entity core.Entity `toml:"entity"`
}

// SoundRequestPayload contains the sound type to play
type SoundRequestPayload struct {
	SoundType core.SoundType `toml:"sound_type"`
}

// DeathRequestPayload contains batch death request
// EffectEvent: 0 = silent death, EventFlashRequest = flash, future: explosion, chain death
type DeathRequestPayload struct {
	Entities    []core.Entity `toml:"entities"`
	EffectEvent EventType     `toml:"effect_event"`
}

// NetworkConnectPayload signals peer connection
type NetworkConnectPayload struct {
	PeerID uint32 `toml:"peer_id"`
}

// NetworkDisconnectPayload signals peer disconnection
type NetworkDisconnectPayload struct {
	PeerID uint32 `toml:"peer_id"`
}

// RemoteInputPayload contains input data from remote player
type RemoteInputPayload struct {
	PeerID  uint32 `toml:"peer_id"`
	Payload []byte `toml:"payload"` // Encoded keystroke/intent
}

// StateSyncPayload contains state snapshot from peer
type StateSyncPayload struct {
	PeerID  uint32 `toml:"peer_id"`
	Seq     uint32 `toml:"seq"`
	Payload []byte `toml:"payload"` // Encoded snapshot
}

// NetworkEventPayload contains a forwarded game event
type NetworkEventPayload struct {
	PeerID  uint32 `toml:"peer_id"`
	Payload []byte `toml:"payload"` // Encoded GameEvent
}

// NetworkErrorPayload contains error information
type NetworkErrorPayload struct {
	PeerID uint32 `toml:"peer_id"`
	Error  string `toml:"error"` // 0 for general errors
}

// MemberTypedPayload signals a composite member was typed
type MemberTypedPayload struct {
	HeaderEntity   core.Entity `toml:"header_entity"`
	MemberEntity   core.Entity `toml:"member_entity"`
	Char           rune        `toml:"char"`
	RemainingCount int         `toml:"remaining_count"` // CountEntity of remaining live members after this one
}

// DecaySpawnPayload contains parameters to spawn a single decay entity
type DecaySpawnPayload struct {
	X             int  `toml:"x"`
	Y             int  `toml:"y"`
	Char          rune `toml:"char"`
	SkipStartCell bool `toml:"skip_start_cell"` // True: particle skips interaction at spawn position
}

// BlossomSpawnPayload contains parameters to spawn a single blossom entity
type BlossomSpawnPayload struct {
	X             int  `toml:"x"`
	Y             int  `toml:"y"`
	Char          rune `toml:"char"`
	SkipStartCell bool `toml:"skip_start_cell"` // True: particle skips interaction at spawn position
}

// CursorMovedPayload signals cursor position change for magnifier updates
type CursorMovedPayload struct {
	X int `toml:"x"`
	Y int `toml:"y"`
}

// QuasarSpawnedPayload contains quasar spawn data
type QuasarSpawnedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
	OriginX      int         `toml:"origin_x"`
	OriginY      int         `toml:"origin_y"`
}

// SpiritSpawnPayload contains parameters to spawn a spirit entity
type SpiritSpawnPayload struct {
	// Starting position (grid coordinates)
	StartX int `toml:"start_x"`
	StartY int `toml:"start_y"`
	// Target convergence position (grid coordinates)
	TargetX    int          `toml:"target_x"`
	TargetY    int          `toml:"target_y"`
	Char       rune         `toml:"char"`
	BaseColor  terminal.RGB `toml:"base_color"`
	BlinkColor terminal.RGB `toml:"blink_color"`
}

// LightningSpawnPayload contains parameters to spawn a lightning entity
type LightningSpawnPayload struct {
	// TODO: this assumes one lightning per entity, change later to allow multiple
	Owner     core.Entity                  `toml:"owner"` // Owning entity for lifecycle (required for tracked)
	OriginX   int                          `toml:"origin_x"`
	OriginY   int                          `toml:"origin_y"`
	TargetX   int                          `toml:"target_x"`
	TargetY   int                          `toml:"target_y"`
	ColorType component.LightningColorType `toml:"color_type"`
	Duration  time.Duration                `toml:"duration"`
	Tracked   bool                         `toml:"tracked"` // If true, entity persists and target can be updated
}

// LightningUpdatePayload updates target position for tracked lightning
type LightningUpdatePayload struct {
	Owner   core.Entity `toml:"owner"`
	TargetX int         `toml:"target_x"`
	TargetY int         `toml:"target_y"`
}

// QuasarChargeStartPayload contains parameters for quasar charge countdown
type QuasarChargeStartPayload struct {
	HeaderEntity core.Entity   `toml:"header_entity"`
	Duration     time.Duration `toml:"duration"`
}

// QuasarChargeCancelPayload identifies which charge timer to destroy
type QuasarChargeCancelPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
}

// ExplosionRequestPayload contains parameters for explosion effect
type ExplosionRequestPayload struct {
	X      int   `toml:"x"`
	Y      int   `toml:"y"`
	Radius int64 `toml:"radius"` // Q32.32, 0 = use default
}

// DustSpawnPayload contains parameters for single dust entity creation
type DustSpawnPayload struct {
	X     int                  `toml:"x"`
	Y     int                  `toml:"y"`
	Char  rune                 `toml:"char"`
	Level component.GlyphLevel `toml:"level"`
}

// DustSpawnEntry is a value type for batch dust spawning
// Must remain a struct (not pointer) to avoid GC pressure in pooled slices
type DustSpawnEntry struct {
	X     int                  `toml:"x"`
	Y     int                  `toml:"y"`
	Char  rune                 `toml:"char"`
	Level component.GlyphLevel `toml:"level"`
}

// DustSpawnBatchPayload contains batch dust spawn data
// Use AcquireDustSpawnBatch/ReleaseDustSpawnBatch for pooled allocation
type DustSpawnBatchPayload struct {
	Entries []DustSpawnEntry `toml:"entries"`
}
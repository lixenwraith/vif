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

// ShieldDrainRequestPayload contains energy drain amount from external sources
type ShieldDrainRequestPayload struct {
	Value int `toml:"value"`
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

// EnergyDeltaType identifies type of energy modification that should be applied
type EnergyDeltaType int

const (
	EnergyDeltaPenalty EnergyDeltaType = iota // Penalties from interactions, absolute value decrease, clamp to zero
	EnergyDeltaReward                         // Reward from actions, absolute value increase
	EnergyDeltaSpend                          // Energy spent, convergent to zero and can cross zero
)

// EnergyAddPayload contains energy delta
type EnergyAddPayload struct {
	Delta      int             `toml:"delta"`      // Positive or negative, sign ignored if flags except percentage is set
	Percentage bool            `toml:"percentage"` // True: percentage of current energy
	Type       EnergyDeltaType `toml:"type"`
}

// EnergySetPayload contains energy value
type EnergySetPayload struct {
	Value int `toml:"value"`
}

// // EnergyAddPercentPayload contains energy delta
// type EnergyAddPercentPayload struct {
// 	DeltaPercent int  `toml:"delta_percent"`
// 	Spend        bool `toml:"spend"`      // True: bypasses boost protection
// 	Convergent   bool `toml:"convergent"` // True: clamp at zero, cannot cross
// }

// GlyphConsumedPayload contains glyph data for centralized energy calculation
type GlyphConsumedPayload struct {
	Type  component.GlyphType  `toml:"type"`
	Level component.GlyphLevel `toml:"level"`
}

// EnergyBlinkPayload triggers visual blink state
type EnergyBlinkPayload struct {
	Type  int `toml:"type"`  // 0=error, 1=blue, 2=green, 3=red, 4=gold
	Level int `toml:"level"` // 0=dark, 1=normal, 2=bright
}

// HeatAddRequestPayload contains heat delta
type HeatAddRequestPayload struct {
	Delta int `toml:"delta"`
}

// HeatSetRequestPayload contains absolute heat value
type HeatSetRequestPayload struct {
	Value int `toml:"value"`
}

// GoldSpawnedPayload provides information about the spawned gold sequence
type GoldSpawnedPayload struct {
	HeaderEntity core.Entity   `toml:"header_entity"`
	Length       int           `toml:"length"`
	Duration     time.Duration `toml:"duration"`
}

// GoldCompletionPayload identifies which gold sequence is completed
type GoldCompletionPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
}

// SplashTimerPayload anchors countdown timer to sequence position
type SplashTimerRequestPayload struct {
	AnchorEntity core.Entity   `toml:"anchor_entity"`
	Color        terminal.RGB  `toml:"color"`
	OriginX      int           `toml:"origin_x"`
	OriginY      int           `toml:"origin_y"`
	MarginLeft   int           `toml:"margin_left"`
	MarginRight  int           `toml:"margin_right"`
	MarginTop    int           `toml:"margin_top"`
	MarginBottom int           `toml:"margin_bottom"`
	Duration     time.Duration `toml:"duration"`
}

// SplashTimerCancelPayload anchors countdown timer to sequence position
type SplashTimerCancelPayload struct {
	AnchorEntity core.Entity `toml:"anchor_entity"`
}

// PingGridRequestPayload carries configuration for the ping grid activation
type PingGridRequestPayload struct {
	Duration time.Duration `toml:"duration"`
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

// MaterializeAreaRequestPayload for area-based materialization (swarm, quasar)
type MaterializeAreaRequestPayload struct {
	X          int                 `toml:"x"`          // Top-left X
	Y          int                 `toml:"y"`          // Top-left Y
	AreaWidth  int                 `toml:"area_width"` // 0 or 1 = single cell
	AreaHeight int                 `toml:"area_height"`
	Type       component.SpawnType `toml:"type"`
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

// MusicStartPayload initializes music state
type MusicStartPayload struct {
	BPM           int                 `toml:"bpm"`
	Intensity     core.MusicIntensity `toml:"intensity"`
	BeatPattern   core.PatternID      `toml:"beat_pattern"`
	MelodyPattern core.PatternID      `toml:"melody_pattern"`
}

// BeatPatternRequestPayload requests beat pattern transition
type BeatPatternRequestPayload struct {
	Pattern        core.PatternID `toml:"pattern"`
	TransitionTime time.Duration  `toml:"transition_time"` // 0 = default
	Quantize       bool           `toml:"quantize"`        // Wait for bar boundary
}

// MelodyNoteRequestPayload triggers immediate note
type MelodyNoteRequestPayload struct {
	Note       int                 `toml:"note"`       // MIDI note number
	Velocity   float64             `toml:"velocity"`   // 0.0-1.0
	Duration   time.Duration       `toml:"duration"`   // 0 = use instrument default
	Instrument core.InstrumentType `toml:"instrument"` // 0 = default (piano)
}

// MelodyPatternRequestPayload requests melody pattern transition
type MelodyPatternRequestPayload struct {
	Pattern        core.PatternID `toml:"pattern"`
	RootNote       int            `toml:"root_note"` // MIDI note for pattern root
	TransitionTime time.Duration  `toml:"transition_time"`
	Quantize       bool           `toml:"quantize"`
}

// MusicIntensityPayload adjusts overall music intensity
type MusicIntensityPayload struct {
	Intensity      core.MusicIntensity `toml:"intensity"`
	TransitionTime time.Duration       `toml:"transition_time"`
}

// MusicTempoPayload adjusts BPM
type MusicTempoPayload struct {
	BPM            int           `toml:"bpm"`
	TransitionTime time.Duration `toml:"transition_time"` // Ramp duration
}

// DeathRequestPayload contains batch death request
// EffectEvent: 0 = silent death, EventFlashSpawnOneRequest = flash, future: explosion, chain death
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
	RemainingCount int         `toml:"remaining_count"` // CountEntities of remaining live members after this one
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
}

// SpiritSpawnRequestPayload contains parameters to spawn a spirit entity
type SpiritSpawnRequestPayload struct {
	// Starting position (grid coordinates)
	StartX int `toml:"start_x"`
	StartY int `toml:"start_y"`
	// Target convergence position (grid coordinates)
	TargetX   int                   `toml:"target_x"`
	TargetY   int                   `toml:"target_y"`
	Char      rune                  `toml:"char"`
	BaseColor component.SpiritColor `toml:"base_color"`
}

// LightningSpawnRequestPayload contains parameters to spawn a lightning entity
type LightningSpawnRequestPayload struct {
	Owner        core.Entity                  `toml:"owner"`
	OriginX      int                          `toml:"origin_x"`
	OriginY      int                          `toml:"origin_y"`
	TargetX      int                          `toml:"target_x"`
	TargetY      int                          `toml:"target_y"`
	OriginEntity core.Entity                  `toml:"origin_entity"` // 0 = use OriginX/Y as static
	TargetEntity core.Entity                  `toml:"target_entity"` // 0 = use TargetX/Y as static
	ColorType    component.LightningColorType `toml:"color_type"`
	Duration     time.Duration                `toml:"duration"`
	Tracked      bool                         `toml:"tracked"` // If true, entity persists and target can be updated
	PathSeed     uint64                       // 0 = system generates
}

// LightningUpdatePayload updates target position for tracked lightning
type LightningUpdatePayload struct {
	Owner   core.Entity `toml:"owner"`
	TargetX int         `toml:"target_x"`
	TargetY int         `toml:"target_y"`
}

// LightningDespawnPayload specifies lightning removal criteria
// Owner is required; TargetEntity=0 removes all lightning from owner
type LightningDespawnPayload struct {
	Owner        core.Entity `toml:"owner"`
	TargetEntity core.Entity `toml:"target_entity"` // 0 = all from owner
}

// ExplosionType differentiates visual and behavioral explosion variants
type ExplosionType uint8

const (
	ExplosionTypeDust    ExplosionType = iota // Converts glyphs to dust, cyan palette
	ExplosionTypeMissile                      // Visual only, warm palette
)

// ExplosionRequestPayload contains parameters for explosion effect
type ExplosionRequestPayload struct {
	X      int           `toml:"x"`
	Y      int           `toml:"y"`
	Radius int64         `toml:"radius"` // Q32.32, 0 = use default
	Type   ExplosionType `toml:"type"`   // Explosion variant
}

// DustSpawnOneRequestPayload contains parameters for single dust entity creation
type DustSpawnOneRequestPayload struct {
	X     int                  `toml:"x"`
	Y     int                  `toml:"y"`
	Char  rune                 `toml:"char"`
	Level component.GlyphLevel `toml:"level"`
}

// MetaStatusMessagePayload contains message to be displayed in status bar
type MetaStatusMessagePayload struct {
	Message string `toml:"message"`
}

// MetaSystemCommandPayload contains commands to the systems (currently only enable/disable functionality)
type MetaSystemCommandPayload struct {
	SystemName string `toml:"system_name"`
	Enabled    bool   `toml:"enabled"`
}

// VampireDrainRequestPayload
type VampireDrainRequestPayload struct {
	TargetEntity core.Entity `toml:"target_entity"`
	Delta        int         `toml:"delta"`
}

// WeaponAddRequestPayload
type WeaponAddRequestPayload struct {
	Weapon component.WeaponType `toml:"weapon"` // 0=rod, 1=launcher, 2=spray
}

// CombatAttackDirectRequestPayload
type CombatAttackDirectRequestPayload struct {
	AttackType   component.CombatAttackType `toml:"attack_type"`
	OwnerEntity  core.Entity                `toml:"owner_entity"`
	OriginEntity core.Entity                `toml:"origin_entity"`
	TargetEntity core.Entity                `toml:"target_entity"`
	HitEntity    core.Entity                `toml:"hit_entity"`
}

// CombatAttackAreaRequestPayload
type CombatAttackAreaRequestPayload struct {
	AttackType   component.CombatAttackType `toml:"attack_type"`
	OwnerEntity  core.Entity                `toml:"owner_entity"`
	OriginEntity core.Entity                `toml:"origin_entity"`
	TargetEntity core.Entity                `toml:"target_entity"`
	HitEntities  []core.Entity              `toml:"hit_entities"`
	// Optional explicit origin position for knockback direction (e.g., explosion center)
	// When both are 0, uses OriginEntity position
	OriginX int `toml:"origin_x"`
	OriginY int `toml:"origin_y"`
}

// FuseEffect defines visual effect type for fusion animations
type FuseEffect int

const (
	FuseEffectNone        FuseEffect = iota
	FuseEffectSpirit                 // Converging spirit trails
	FuseEffectMaterialize            // Reverse beam convergence
)

// TODO: future implementation
// FuseRequestPayload is generic payload for fusion types
type FuseRequestPayload struct {
	Sources []core.Entity `toml:"sources"`
	TargetX int           `toml:"target_x"`
	TargetY int           `toml:"target_y"`
	Effect  FuseEffect    `toml:"effect"`
	Type    int           `toml:"type"` // Maps to FuseType in fuse system
}

// FuseSwarmRequestPayload contains the two drains to fuse
type FuseSwarmRequestPayload struct {
	DrainA core.Entity `toml:"drain_a"`
	DrainB core.Entity `toml:"drain_b"`
	Effect FuseEffect  `toml:"effect"` // Defaults to FuseEffectSpirit (0)
}

// SwarmSpawnedPayload contains swarm spawn data
type SwarmSpawnedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
}

// SwarmDespawnedPayload contains despawn reason
type SwarmDespawnedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
}

// SwarmAbsorbedDrainPayload contains absorption data
type SwarmAbsorbedDrainPayload struct {
	SwarmEntity core.Entity `toml:"swarm_entity"`
	DrainEntity core.Entity `toml:"drain_entity"`
	HPAbsorbed  int         `toml:"hp_absorbed"`
}

// QuasarSpawnRequestPayload contains coordinates for creation
type QuasarSpawnRequestPayload struct {
	SpawnX int `toml:"spawn_x"`
	SpawnY int `toml:"spawn_y"`
}

// SwarmSpawnRequestPayload contains coordinates for creation
type SwarmSpawnRequestPayload struct {
	SpawnX int `toml:"spawn_x"`
	SpawnY int `toml:"spawn_y"`
}

// MarkerSpawnRequestPayload for marker creation
type MarkerSpawnRequestPayload struct {
	X         int                   `toml:"x"`
	Y         int                   `toml:"y"`
	Width     int                   `toml:"width"`
	Height    int                   `toml:"height"`
	Shape     component.MarkerShape `toml:"shape"`
	Color     terminal.RGB          `toml:"color"`
	Intensity int64                 `toml:"intensity"` // Q32.32
	Duration  time.Duration         `toml:"duration"`
	PulseRate int64                 `toml:"pulse_rate"` // Q32.32, 0 = none
	FadeMode  uint8                 `toml:"fade_mode"`  // 0=none, 1=out, 2=in
}

// MotionMarkerShowPayload contains direction for colored marker display
type MotionMarkerShowPayload struct {
	DirectionX int `toml:"direction_x"` // -1, 0, 1
	DirectionY int `toml:"direction_y"` // -1, 0, 1
}

// ModeChangeNotificationPayload contains the new mode
type ModeChangeNotificationPayload struct {
	Mode core.GameMode `toml:"mode"`
}

// MissileSpawnRequestPayload contains cluster missile spawn parameters
type MissileSpawnRequestPayload struct {
	OwnerEntity  core.Entity   `toml:"owner_entity"`  // Cursor
	OriginEntity core.Entity   `toml:"origin_entity"` // Launcher orb
	OriginX      int           `toml:"origin_x"`
	OriginY      int           `toml:"origin_y"`
	TargetX      int           `toml:"target_x"` // Far quadrant aim point
	TargetY      int           `toml:"target_y"`
	ChildCount   int           `toml:"child_count"`  // heat/10
	Targets      []core.Entity `toml:"targets"`      // Prioritized target entities
	HitEntities  []core.Entity `toml:"hit_entities"` // Corresponding hit points (member or same as target)
}

// MissileImpactPayload contains impact event data
type MissileImpactPayload struct {
	OwnerEntity  core.Entity `toml:"owner_entity"`
	TargetEntity core.Entity `toml:"target_entity"`
	HitEntity    core.Entity `toml:"hit_entity"`
	ImpactX      int         `toml:"impact_x"`
	ImpactY      int         `toml:"impact_y"`
}

// WallSpawnRequestPayload contains parameters for single wall cell creation
type WallSpawnRequestPayload struct {
	X         int
	Y         int
	BlockMask component.WallBlockMask
	Char      rune
	FgColor   terminal.RGB
	BgColor   terminal.RGB
	RenderFg  bool
	RenderBg  bool
}

// WallCompositeSpawnRequestPayload contains parameters for multi-cell wall structure
// Uses Header/Member pattern for lifecycle management
type WallCompositeSpawnRequestPayload struct {
	X         int // Anchor position
	Y         int
	BlockMask component.WallBlockMask // Applied to all cells
	Cells     []component.WallCellDef
}

// WallDespawnRequestPayload contains parameters for wall removal
type WallDespawnRequestPayload struct {
	X      int
	Y      int
	Width  int  // 0 = single cell
	Height int  // 0 = single cell
	All    bool // True = clear all walls
}

// WallMaskChangeRequestPayload modifies blocking behavior of existing walls
type WallMaskChangeRequestPayload struct {
	X         int
	Y         int
	Width     int
	Height    int
	BlockMask component.WallBlockMask
}

// WallSpawnedPayload notifies of wall creation completion
type WallSpawnedPayload struct {
	X            int
	Y            int
	Width        int
	Height       int
	Count        int
	HeaderEntity core.Entity // 0 for single walls
}

// FadeoutSpawnPayload contains parameters for single fadeout effect
type FadeoutSpawnPayload struct {
	X       int
	Y       int
	Char    rune // 0 = bg-only
	FgColor terminal.RGB
	BgColor terminal.RGB
}

// CompositeIntegrityBreachPayload notifies owner system of unexpected member loss
type CompositeIntegrityBreachPayload struct {
	HeaderEntity   core.Entity        `toml:"header_entity"`
	Behavior       component.Behavior `toml:"behavior"`
	LostCount      int                `toml:"lost_count"`
	RemainingCount int                `toml:"remaining_count"`
}

// CompositeDestroyRequestPayload requests centralized composite destruction
type CompositeDestroyRequestPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
	Effect       EventType   `toml:"effect"` // 0 = silent, EventFlashSpawnOneRequest, etc.
}
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
	OriginX   int                        `toml:"origin_x"`
	OriginY   int                        `toml:"origin_y"`
	ColorType component.CleanerColorType `toml:"color_type"`
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
	EnergyDeltaPassive                        // Passive drain, bypasses ember/boost, convergent clamp to zero
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
	ExplosionTypeEye                          // Self-destruct explosion with character noise
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
	Message          string        `toml:"message"`
	Duration         time.Duration `toml:"duration"`
	DurationOverride bool          `toml:"duration_override"`
}

// MetaSystemCommandPayload contains commands to the systems (currently only enable/disable functionality)
type MetaSystemCommandPayload struct {
	SystemName string `toml:"system_name"`
	Enabled    bool   `toml:"enabled"`
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

// SwarmDestroyedPayload contains despawn reason
type SwarmDestroyedPayload struct {
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
	X int `toml:"x"`
	Y int `toml:"y"`
}

// SwarmSpawnRequestPayload contains coordinates for creation
type SwarmSpawnRequestPayload struct {
	X int `toml:"x"`
	Y int `toml:"y"`
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

// WallSpawnRequestPayload contains parameters for single wall cell creation
type WallSpawnRequestPayload struct {
	X             int                     `toml:"x"`
	Y             int                     `toml:"y"`
	BlockMask     component.WallBlockMask `toml:"block_mask"`
	CollisionMode WallBatchCollisionMode  `toml:"collision_mode"`
	Char          rune                    `toml:"char"`
	FgColor       terminal.RGB            `toml:"fg_color"`
	BgColor       terminal.RGB            `toml:"bg_color"`
	RenderFg      bool                    `toml:"render_fg"`
	RenderBg      bool                    `toml:"render_bg"`
	BoxStyle      component.BoxDrawStyle  `toml:"box_style"` // Box-drawing style (0=none, 1=single, 2=double)
}

// WallBatchCollisionMode defines behavior when batch spawn encounters existing walls
type WallBatchCollisionMode uint8

const (
	// WallBatchSkipBlocked skips positions occupied by existing walls
	WallBatchSkipBlocked WallBatchCollisionMode = iota
	// WallBatchOverwrite destroys existing walls and spawns new ones at their positions
	WallBatchOverwrite
	// WallBatchFailIfBlocked aborts entire batch if any target position has a wall
	WallBatchFailIfBlocked
)

// WallBatchSpawnRequestPayload contains parameters for bulk wall creation
// Cells use offset coordinates relative to anchor (X, Y)
// BoxStyle at payload level applies to all cells (per-cell BoxStyle in WallCellDef ignored)
type WallBatchSpawnRequestPayload struct {
	X             int                     `toml:"x"`          // Anchor position
	Y             int                     `toml:"y"`          // Anchor position
	BlockMask     component.WallBlockMask `toml:"block_mask"` // Applied to all cells
	BoxStyle      component.BoxDrawStyle  `toml:"box_style"`  // Applied to all cells
	CollisionMode WallBatchCollisionMode  `toml:"collision_mode"`
	Composite     bool                    `toml:"composite"` // If true, create header/member structure
	Cells         []component.WallCellDef `toml:"cells"`
}

// WallCompositeSpawnRequestPayload contains parameters for multi-cell wall structure
type WallCompositeSpawnRequestPayload struct {
	X             int                     `toml:"x"` // Anchor position
	Y             int                     `toml:"y"`
	BlockMask     component.WallBlockMask `toml:"block_mask"` // Applied to all cells
	CollisionMode WallBatchCollisionMode  `toml:"collision_mode"`
	Cells         []component.WallCellDef `toml:"cells"`
	BoxStyle      component.BoxDrawStyle  `toml:"box_style"` // Applied to all cells
}

// WallPatternSpawnRequestPayload contains parameters for pattern-based wall creation
type WallPatternSpawnRequestPayload struct {
	Path          string                  `toml:"path"`       // Path to .vfimg file
	X             int                     `toml:"x"`          // Anchor X position
	Y             int                     `toml:"y"`          // Anchor Y position
	BlockMask     component.WallBlockMask `toml:"block_mask"` // Applied to all cells
	CollisionMode WallBatchCollisionMode  `toml:"collision_mode"`
}

// WallDespawnRequestPayload contains parameters for wall removal
type WallDespawnRequestPayload struct {
	X      int  `toml:"x"`
	Y      int  `toml:"y"`
	Width  int  `toml:"width"`  // 0 = single cell
	Height int  `toml:"height"` // 0 = single cell
	All    bool `toml:"all"`    // True = clear all walls
}

// WallMaskChangeRequestPayload modifies blocking behavior of existing walls
type WallMaskChangeRequestPayload struct {
	X         int                     `toml:"x"`
	Y         int                     `toml:"y"`
	Width     int                     `toml:"width"`
	Height    int                     `toml:"height"`
	BlockMask component.WallBlockMask `toml:"block_mask"`
}

// WallSpawnedPayload notifies of wall creation completion
type WallSpawnedPayload struct {
	X            int         `toml:"x"`
	Y            int         `toml:"y"`
	Width        int         `toml:"width"`
	Height       int         `toml:"height"`
	Count        int         `toml:"count"`
	HeaderEntity core.Entity `toml:"header_entity"` // 0 for single walls
}

// WallDespawnedPayload notifies of wall destruction
type WallDespawnedPayload struct {
	X      int `toml:"x"`
	Y      int `toml:"y"`
	Width  int `toml:"width"`
	Height int `toml:"height"`
	Count  int `toml:"count"`
}

// FadeoutSpawnPayload contains parameters for single fadeout effect
type FadeoutSpawnPayload struct {
	X       int  `toml:"x"`
	Y       int  `toml:"y"`
	Char    rune `toml:"char"` // 0 = bg-only
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

// EnemyKilledPayload carries entity type and death position for loot resolution
type EnemyKilledPayload struct {
	Entity  core.Entity           `toml:"entity"`
	Species component.SpeciesType `toml:"species"`
	X       int                   `toml:"x"`
	Y       int                   `toml:"y"`
}

// EnemyCreatedPayload signals enemy entity spawn for GA tracking
type EnemyCreatedPayload struct {
	Entity  core.Entity           `toml:"entity"`
	Species component.SpeciesType `toml:"species"`
}

// LootSpawnRequestPayload requests direct loot spawn (bypasses drop tables)
type LootSpawnRequestPayload struct {
	Type component.LootType `toml:"type"`
	X    int                `toml:"x"`
	Y    int                `toml:"y"`
}

// StormCircleDestroyedPayload contains individual circle death data
type StormCircleDestroyedPayload struct {
	CircleEntity core.Entity `toml:"circle_entity"`
	RootEntity   core.Entity `toml:"root_entity"`
	Index        int         `toml:"index"`
}

// StormDestroyedPayload contains storm death data
type StormDestroyedPayload struct {
	RootEntity core.Entity `toml:"root_entity"`
}

// LevelSetupPayload configures map dimensions and entity lifecycle
type LevelSetupPayload struct {
	Width         int  `toml:"width"`          // New map width in grid cells
	Height        int  `toml:"height"`         // New map height in grid cells
	ClearEntities bool `toml:"clear_entities"` // If true, destroy non-protected entities
	CropOnResize  bool `toml:"crop_on_resize"` // Explicit crop behavior (false = level mode)
}

// MazeRoomSpec defines an explicit room in maze
type MazeRoomSpec struct {
	CenterX int `toml:"center_x"` // 0 = random placement
	CenterY int `toml:"center_y"` // 0 = random placement
	Width   int `toml:"width"`    // 0 = use default
	Height  int `toml:"height"`   // 0 = use default
}

// MazeSpawnRequestPayload configures maze generation
type MazeSpawnRequestPayload struct {
	CellWidth     int                        `toml:"cell_width"`
	CellHeight    int                        `toml:"cell_height"`
	Braiding      float64                    `toml:"braiding"`
	BlockMask     component.WallBlockMask    `toml:"block_mask"`
	CollisionMode WallBatchCollisionMode     `toml:"collision_mode"`
	Visual        component.WallVisualConfig `toml:"visual"`
	// Room generation
	RoomCount         int            `toml:"room_count"`
	Rooms             []MazeRoomSpec `toml:"rooms"`
	DefaultRoomWidth  int            `toml:"default_room_width"`
	DefaultRoomHeight int            `toml:"default_room_height"`
}

// BulletSpawnRequestPayload requests creation of a linear projectile
type BulletSpawnRequestPayload struct {
	OriginX     int64                  `toml:"origin_x"` // Q32.32 spawn position
	OriginY     int64                  `toml:"origin_y"`
	VelX        int64                  `toml:"vel_x"`
	VelY        int64                  `toml:"vel_y"` // Q32.32 velocity
	Owner       core.Entity            `toml:"owner"`
	MaxLifetime time.Duration          `toml:"max_lifetime"`
	Damage      component.BulletDamage `toml:"damage"`
}

// StrobeRequestPayload configures screen flash effect
type StrobeRequestPayload struct {
	Color      terminal.RGB `toml:"color"`
	Intensity  float64      `toml:"intensity"`   // Base intensity 0.0-1.0
	DurationMs int64        `toml:"duration_ms"` // 0 = default value from parameters
}

// PylonSpawnRequestPayload contains parameters for pylon creation
type PylonSpawnRequestPayload struct {
	X       int `toml:"x"`
	Y       int `toml:"y"`
	RadiusX int `toml:"radius_x"`
	RadiusY int `toml:"radius_y"`
	MinHP   int `toml:"min_hp"` // HP at edge, when == MaxHP all members uniform
	MaxHP   int `toml:"max_hp"` // HP at center
}

// PylonSpawnedPayload contains pylon spawn data
type PylonSpawnedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
	MemberCount  int         `toml:"member_count"`
}

// PylonDestroyedPayload contains pylon death data
type PylonDestroyedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
	X            int         `toml:"x"`
	Y            int         `toml:"y"`
}

// SnakeSpawnRequestPayload contains coordinates for snake creation
type SnakeSpawnRequestPayload struct {
	X            int `toml:"x"`
	Y            int `toml:"y"`
	SegmentCount int `toml:"segment_count"` // Body segments to spawn (0 = default)
}

// SnakeSpawnedPayload emitted after successful spawn
type SnakeSpawnedPayload struct {
	RootEntity core.Entity `toml:"root_entity"`
	HeadEntity core.Entity `toml:"head_entity"`
	BodyEntity core.Entity `toml:"body_entity"`
}

// SnakeDestroyedPayload emitted on snake death
type SnakeDestroyedPayload struct {
	RootEntity core.Entity `toml:"root_entity"`
}

// TargetGroupUpdatePayload configures or updates a navigation target group
type TargetGroupUpdatePayload struct {
	GroupID uint8                `toml:"group_id"` // 0 = cursor (rarely set manually), 1+ = custom
	Type    component.TargetType `toml:"type"`
	Entity  core.Entity          `toml:"entity"` // For TargetEntity type
	PosX    int                  `toml:"pos_x"`  // For TargetPosition type
	PosY    int                  `toml:"pos_y"`  // For TargetPosition type
}

// TargetGroupRemovePayload removes a target group
type TargetGroupRemovePayload struct {
	GroupID uint8 `toml:"group_id"`
}

// EyeSpawnRequestPayload contains eye spawn parameters
type EyeSpawnRequestPayload struct {
	X             int               `toml:"x"`
	Y             int               `toml:"y"`
	Type          component.EyeType `toml:"type"`
	TargetGroupID uint8             `toml:"target_group_id"`
}

// EyeSpawnedPayload notifies eye composite creation
type EyeSpawnedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
}

// EyeDestroyedPayload notifies eye termination
type EyeDestroyedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
}

// TowerSpawnRequestPayload contains parameters for tower creation
type TowerSpawnRequestPayload struct {
	X             int                 `toml:"x"`
	Y             int                 `toml:"y"`
	Type          component.TowerType `toml:"type"`
	RadiusX       int                 `toml:"radius_x"`
	RadiusY       int                 `toml:"radius_y"`
	MinHP         int                 `toml:"min_hp"`
	MaxHP         int                 `toml:"max_hp"`
	TargetGroupID uint8               `toml:"target_group_id"` // Navigation target group
}

// TowerSpawnedPayload contains tower spawn data
type TowerSpawnedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
	MemberCount  int         `toml:"member_count"`
	X            int         `toml:"x"`
	Y            int         `toml:"y"`
}

// TowerDestroyedPayload contains tower death data
type TowerDestroyedPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
	X            int         `toml:"x"`
	Y            int         `toml:"y"`
}

// GatewaySpawnRequestPayload requests creation of a gateway entity
type GatewaySpawnRequestPayload struct {
	AnchorEntity      core.Entity `toml:"anchor_entity"`       // Parent entity (pylon header)
	Species           uint8       `toml:"species"`             // component.SpeciesType
	SubType           uint8       `toml:"sub_type"`            // Species variant (e.g. EyeType)
	GroupID           uint8       `toml:"group_id"`            // Target group for spawned entities
	BaseInterval      int64       `toml:"base_interval"`       // time.Duration as int64
	RateMultiplier    float64     `toml:"rate_multiplier"`     // 1.0 = no acceleration
	RateAccelInterval int64       `toml:"rate_accel_interval"` // time.Duration as int64, 0 = disabled
	MinInterval       int64       `toml:"min_interval"`        // time.Duration as int64
	OffsetX           int         `toml:"offset_x"`
	OffsetY           int         `toml:"offset_y"`
}

// GatewayDespawnRequestPayload requests removal of gateway anchored to entity
type GatewayDespawnRequestPayload struct {
	AnchorEntity core.Entity `toml:"anchor_entity"`
}

// GatewayDespawnedPayload emitted when a gateway is cleaned up
type GatewayDespawnedPayload struct {
	GatewayEntity core.Entity `toml:"gateway_entity"`
	AnchorEntity  core.Entity `toml:"anchor_entity"`
}

type DebugFlowGroupPayload struct {
	GroupID uint8
}
package event

import (
	"sync"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/pkg/audio"
)

// --- Engine ---

// --- Level ---

// LevelSetupPayload configures map dimensions and entity lifecycle
type LevelSetupPayload struct {
	Width         int  `toml:"width"`          // New map width in grid cells
	Height        int  `toml:"height"`         // New map height in grid cells
	ClearEntities bool `toml:"clear_entities"` // If true, destroy non-protected entities
	CropOnResize  bool `toml:"crop_on_resize"` // Explicit crop behavior (false = level mode)
}

// ScreenResizePayload carries new terminal dimensions. Terminal cells, not viewport
// cells: the viewport is derived from these by subtracting the fixed margins, and
// recording the derived value would hide a margin change behind an identical replay.
type ScreenResizePayload struct {
	Width  int `toml:"width"`  // Terminal columns
	Height int `toml:"height"` // Terminal rows
}

// --- Audio ---

// SoundRequestPayload contains the sound type to play
type SoundRequestPayload struct {
	ID audio.SoundID `toml:"sound_id"`
}

type SoundMuteToggleMode uint8

const (
	MuteCycle  SoundMuteToggleMode = iota // ignore Mask, advance the rotation
	MuteToggle                            // flip the channels named in Mask
	MuteSet                               // set the mask verbatim
)

// SoundMuteTogglePayload requests an audio mute-mask change
// A nil payload is equivalent to {Mode: MuteCycle}
type SoundMuteTogglePayload struct {
	Mode SoundMuteToggleMode `toml:"mode"`
	Mask uint8               `toml:"mask"` // parameter.AudioChan* bits
}

// AudioMuteChangedPayload announces the applied mask; AudioSystem is the sole emitter
type AudioMuteChangedPayload struct {
	Mask uint8 `toml:"mask"`
}

// --- Music ---

// MusicStartPayload initializes music state
type MusicStartPayload struct {
	BPM           int             `toml:"bpm"`
	Intensity     audio.Intensity `toml:"intensity"`
	BeatPattern   audio.PatternID `toml:"beat_pattern"`
	MelodyPattern audio.PatternID `toml:"melody_pattern"`
}

// BeatPatternRequestPayload requests beat pattern transition
type BeatPatternRequestPayload struct {
	Pattern        audio.PatternID `toml:"pattern"`
	TransitionTime time.Duration   `toml:"transition_time"` // 0 = default
	Quantize       bool            `toml:"quantize"`        // Wait for bar boundary
}

// MelodyNoteRequestPayload triggers immediate note
type MelodyNoteRequestPayload struct {
	Note       int                  `toml:"note"`       // MIDI note number
	Velocity   float64              `toml:"velocity"`   // 0.0-1.0
	Duration   time.Duration        `toml:"duration"`   // 0 = use instrument default
	Instrument audio.InstrumentType `toml:"instrument"` // 0 = default (piano)
}

// MelodyPatternRequestPayload requests melody pattern transition
type MelodyPatternRequestPayload struct {
	Pattern        audio.PatternID `toml:"pattern"`
	RootNote       int             `toml:"root_note"` // MIDI note for pattern root
	TransitionTime time.Duration   `toml:"transition_time"`
	Quantize       bool            `toml:"quantize"`
}

// MusicIntensityPayload adjusts overall music intensity
type MusicIntensityPayload struct {
	Intensity      audio.Intensity `toml:"intensity"`
	TransitionTime time.Duration   `toml:"transition_time"`
}

// MusicTempoPayload adjusts BPM
type MusicTempoPayload struct {
	BPM            int           `toml:"bpm"`
	TransitionTime time.Duration `toml:"transition_time"` // Ramp duration
}

// MusicSeedPayload re-keys the musical rng: generative melody, step
// probabilities, fill selection. Seed 0 draws from wall clock.
// Emit before EventMusicStart for a reproducible run
type MusicSeedPayload struct {
	Seed int64 `toml:"seed"`
}

// MusicSwingPayload sets shuffle; 0 = straight, audio.MaxSwing = maximum
type MusicSwingPayload struct {
	Amount float64 `toml:"amount"`
}

// --- Network ---

// NetworkConnectPayload signals peer connection
type NetworkConnectPayload struct {
	PeerID uint32 `toml:"peer_id"`
}

// ParticipantJoinedPayload names the roster slot a participant arriving mid-run
// takes. Like a departure it is a crossing rather than a local reaction: a joiner is
// admitted by one instance, and every other has to add the cursor at the same tick or
// their shared entity creation order diverges (D-11).
type ParticipantJoinedPayload struct {
	Participant uint32 `toml:"participant"`
	Slot        uint8  `toml:"slot"`
}

// ParticipantDepartedPayload names the roster slot a departed participant held. It
// is the crossing that makes a departure shared state: a link disconnect is observed
// only by a direct neighbour and at a moment of that neighbour's own choosing, so the
// removal has to travel as an artifact with an apply tick, like any other outcome
// every instance must reach together.
type ParticipantDepartedPayload struct {
	Participant uint32 `toml:"participant"`
	Slot        uint8  `toml:"slot"`
}

// NetworkDisconnectPayload signals peer disconnection
type NetworkDisconnectPayload struct {
	PeerID uint32 `toml:"peer_id"`
}

// --- Meta ---

// GameResetPayload requests a game restart; Purge additionally clears operator
// session state
type GameResetPayload struct {
	Purge bool `toml:"purge"`
}

// CursorDefeatStatePayload carries one owner's combined heat/energy state.
type CursorDefeatStatePayload struct {
	Entity   core.Entity `toml:"entity"`
	Defeated bool        `toml:"defeated"`
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

// GamePausePayload carries the new pause state
type GamePausePayload struct {
	Paused bool `toml:"paused"`
}

// GameSpeedPayload carries a time scale as an exact rational;
// a non-positive Num or Den selects real time
type GameSpeedPayload struct {
	Num int64 `toml:"num"`
	Den int64 `toml:"den"`
}

// GameStepPayload arms a debug step: Ticks > 0 advances that many game ticks
// while paused, otherwise Mode selects a run-until condition
type GameStepPayload struct {
	Mode   string `toml:"mode"`   // "", "fsm", "event"
	Region string `toml:"region"` // fsm: "" matches any region
	Event  string `toml:"event"`  // event: event name
	Ticks  int64  `toml:"ticks"`
	Num    int64  `toml:"num"` // run rate; 0 keeps the current rate
	Den    int64  `toml:"den"`
	Pause  bool   `toml:"pause"` // pause when the condition trips
	Off    bool   `toml:"off"`   // disarm any pending request
}

// --- FSM ---

// Region operations carried by FSMRegionPayload
const (
	RegionList      = "list"
	RegionSpawn     = "spawn"
	RegionPause     = "pause"
	RegionResume    = "resume"
	RegionTerminate = "terminate"
)

// FSMRegionPayload requests an FSM region operation; State applies to spawn only
type FSMRegionPayload struct {
	Op     string `toml:"op"`
	Region string `toml:"region"`
	State  string `toml:"state,omitempty"`
}

// --- Nugget ---

// NuggetCollectedPayload signals successful personal nugget collection.
type NuggetCollectedPayload struct {
	Entity core.Entity `toml:"entity"`
}

// NuggetDestroyedPayload signals external destruction of a personal nugget.
type NuggetDestroyedPayload struct {
	Entity core.Entity `toml:"entity"`
}

// NuggetJumpRequestPayload names the cursor jumping to the active nugget.
type NuggetJumpRequestPayload struct {
	Entity core.Entity `toml:"entity"`
}

// --- Cleaner ---

// DirectionalCleanerPayload contains origin for 4-way cleaner spawn
type DirectionalCleanerPayload struct {
	Entity    core.Entity                `toml:"entity"`
	OriginX   int                        `toml:"origin_x"`
	OriginY   int                        `toml:"origin_y"`
	ColorType component.CleanerColorType `toml:"color_type"`
}

// CleanerSweepingRequestPayload names the cursor whose polarity drives the sweep.
type CleanerSweepingRequestPayload struct {
	Entity core.Entity `toml:"entity"`
}

// --- Gold ---

// GoldSpawnedPayload provides information about the spawned gold sequence
type GoldSpawnedPayload struct {
	HeaderEntity core.Entity   `toml:"header_entity"`
	Length       int           `toml:"length"`
	Duration     time.Duration `toml:"duration"`
}

// GoldCompletionPayload identifies which gold sequence ended and who earned it.
// Entity is the cursor that typed the most members, 0 on timeout or destruction.
type GoldCompletionPayload struct {
	HeaderEntity core.Entity `toml:"header_entity"`
	Entity       core.Entity `toml:"entity"`
}

// GoldJumpRequestPayload names the cursor jumping to the active gold sequence.
type GoldJumpRequestPayload struct {
	Entity core.Entity `toml:"entity"`
}

// --- Splash ---

// SplashTimerRequestPayload anchors countdown timer to sequence position
type SplashTimerRequestPayload struct {
	AnchorEntity core.Entity   `toml:"anchor_entity"`
	Color        color.RGB     `toml:"color"`
	OriginX      int           `toml:"origin_x"`
	OriginY      int           `toml:"origin_y"`
	MarginLeft   int           `toml:"margin_left"`
	MarginRight  int           `toml:"margin_right"`
	MarginTop    int           `toml:"margin_top"`
	MarginBottom int           `toml:"margin_bottom"`
	Duration     time.Duration `toml:"duration"`
}

// SplashTimerCancelPayload cancels countdown timer of an anchor
type SplashTimerCancelPayload struct {
	AnchorEntity core.Entity `toml:"anchor_entity"`
}

// --- Energy ---

// EnergyAddPayload contains energy delta
type EnergyAddPayload struct {
	Entity     core.Entity               `toml:"entity"`
	Delta      int                       `toml:"delta"`      // Positive or negative, sign ignored if flags except percentage is set
	Percentage bool                      `toml:"percentage"` // True: percentage of current energy
	Type       component.EnergyDeltaType `toml:"type"`
}

// EnergySetPayload contains energy value
type EnergySetPayload struct {
	Entity core.Entity `toml:"entity"`
	Value  int         `toml:"value"`
}

// EnergyCrossedZeroPayload names the cursor whose energy changed sign
type EnergyCrossedZeroPayload struct {
	Entity core.Entity `toml:"entity"`
}

// EnergyGlyphConsumedPayload contains glyph data for centralized energy calculation
type EnergyGlyphConsumedPayload struct {
	Entity core.Entity          `toml:"entity"`
	Type   component.GlyphType  `toml:"type"`
	Level  component.GlyphLevel `toml:"level"`
}

// EnergyBlinkPayload triggers visual blink state
type EnergyBlinkPayload struct {
	Entity core.Entity `toml:"entity"`
	Type   int         `toml:"type"`  // 0=error, 1=blue, 2=green, 3=red, 4=gold
	Level  int         `toml:"level"` // 0=dark, 1=normal, 2=bright
}

// EnergyBlinkStopPayload names the cursor whose blink clears
type EnergyBlinkStopPayload struct {
	Entity core.Entity `toml:"entity"`
}

// --- Shield ----

// ShieldActivatePayload names the cursor whose shield comes up
type ShieldActivatePayload struct {
	Entity core.Entity `toml:"entity"`
}

// ShieldDeactivatePayload names the cursor whose shield drops
type ShieldDeactivatePayload struct {
	Entity core.Entity `toml:"entity"`
}

// ShieldDrainRequestPayload contains energy drain amount from external sources
type ShieldDrainRequestPayload struct {
	Entity core.Entity `toml:"entity"`
	Value  int         `toml:"value"`
}

// --- Weapon ---

// WeaponAddRequestPayload adds a weapon to cursor
type WeaponAddRequestPayload struct {
	Entity core.Entity          `toml:"entity"`
	Weapon component.WeaponType `toml:"weapon"` // 0=rod, 1=launcher, 2=spray
}

// WeaponFireRequestPayload adds a weapon to cursor
type WeaponFireRequestPayload struct {
	Entity core.Entity `toml:"entity"`
}

// FireSpecialRequestPayload names the cursor firing its special
type FireSpecialRequestPayload struct {
	Entity core.Entity `toml:"entity"`
}

// --- Heat ---

// HeatAddRequestPayload contains heat delta
type HeatAddRequestPayload struct {
	Entity core.Entity `toml:"entity"`
	Delta  int         `toml:"delta"`
}

// HeatSetRequestPayload contains absolute heat value
type HeatSetRequestPayload struct {
	Entity core.Entity `toml:"entity"`
	Value  int         `toml:"value"`
}

// HeatBurstPayload names the cursor that overheated
type HeatBurstPayload struct {
	Entity core.Entity `toml:"entity"`
}

// --- Boost ---

// BoostActivatePayload contains boost activation parameters
type BoostActivatePayload struct {
	Entity   core.Entity   `toml:"entity"`
	Duration time.Duration `toml:"duration"`
}

// BoostDeactivatePayload names the cursor whose boost ends
type BoostDeactivatePayload struct {
	Entity core.Entity `toml:"entity"`
}

// BoostExtendPayload contains boost extension parameters
type BoostExtendPayload struct {
	Entity   core.Entity   `toml:"entity"`
	Duration time.Duration `toml:"duration"`
}

// BoostRewardPayload names the cursor earning a boost
type BoostRewardPayload struct {
	Entity core.Entity `toml:"entity"`
}

// --- Typing ---

// CharacterTypedPayload captures keypress and cursor state when character is typed
type CharacterTypedPayload struct {
	Entity core.Entity `toml:"entity"`
	Char   rune        `toml:"char"`
	X      int         `toml:"x"`
	Y      int         `toml:"y"`
}

// CharacterTypedPayloadPool reduces GC pressure during high-frequency typing
var CharacterTypedPayloadPool = sync.Pool{
	New: func() any { return &CharacterTypedPayload{} },
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

// --- Ping ---

// PingGridRequestPayload carries configuration for the ping grid activation
type PingGridRequestPayload struct {
	Entity   core.Entity   `toml:"entity"`
	Duration time.Duration `toml:"duration"`
}

// --- Materialize ---

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

// MaterializeCompletedPayload carries details about a completed materialization
type MaterializeCompletedPayload struct {
	X    int                 `toml:"x"`
	Y    int                 `toml:"y"`
	Type component.SpawnType `toml:"type"`
}

// --- Flash ---

// FlashRequestPayload contains parameters for destruction flash effect
type FlashRequestPayload struct {
	X    int  `toml:"x"`
	Y    int  `toml:"y"`
	Char rune `toml:"char"`
}

// --- Explosion ---

// ExplosionType differentiates visual and behavioral explosion variants
type ExplosionType uint8

const (
	ExplosionTypeDust    ExplosionType = iota // Converts glyphs to dust, cyan palette
	ExplosionTypeMissile                      // Visual only, warm palette
	ExplosionTypeEye                          // Self-destruct explosion with character noise
	ExplosionTypePulse                        // Combat geometry only; PulseComponent owns the visual
)

// ExplosionRequestPayload describes one explosion center: geometry, combat family and
// visual variant. Attack has no safe zero value; producers must set it explicitly.
type ExplosionRequestPayload struct {
	Entity   core.Entity                `toml:"entity"` // Owner cursor, credited for damage
	X        int                        `toml:"x"`
	Y        int                        `toml:"y"`
	Radius   float64                    `toml:"radius"`   // 0 = ExplosionFieldRadius
	Duration time.Duration              `toml:"duration"` // 0 = ExplosionFieldDuration
	Attack   component.CombatAttackType `toml:"attack"`   // CombatAttackNone = visual only
	Type     ExplosionType              `toml:"type"`     // Palette selection
}

// ExplosionBatchRequestPayload describes one explosion made of several centers sharing
// geometry, combat family and visual variant. Pooled: the consumer releases it.
// Producers must truncate Centers at parameter.ExplosionRequestCenterCap so a
// map-wide detonation cannot flood the event queue or the center array.
type ExplosionBatchRequestPayload struct {
	Centers  []ExplosionCenterEntry
	Entity   core.Entity
	Radius   float64
	Duration time.Duration
	Attack   component.CombatAttackType
	Type     ExplosionType
}

// --- Dust ---

// DustSpawnOneRequestPayload contains parameters for single dust entity creation
type DustSpawnOneRequestPayload struct {
	X     int                  `toml:"x"`
	Y     int                  `toml:"y"`
	Char  rune                 `toml:"char"`
	Level component.GlyphLevel `toml:"level"`
}

// --- Blossom ---

// BlossomSpawnPayload contains parameters to spawn a single blossom entity
type BlossomSpawnPayload struct {
	X             int  `toml:"x"`
	Y             int  `toml:"y"`
	Char          rune `toml:"char"`
	SkipStartCell bool `toml:"skip_start_cell"` // True: particle skips interaction at spawn position
}

// --- Decay ---

// DecaySpawnPayload contains parameters to spawn a single decay entity
type DecaySpawnPayload struct {
	X             int  `toml:"x"`
	Y             int  `toml:"y"`
	Char          rune `toml:"char"`
	SkipStartCell bool `toml:"skip_start_cell"` // True: particle skips interaction at spawn position
}

// --- Death ---

// DeathRequestPayload contains a death request for one or more entities.
// EffectEvent: 0 = silent death, EventFlashSpawnOneRequest = flash, future: explosion, chain death
type DeathRequestPayload struct {
	Entities    []core.Entity `toml:"entities"`
	EffectEvent EventType     `toml:"effect_event"`
}

// --- Timer ---

// TimerStartPayload configuration for a new lifecycle timer
type TimerStartPayload struct {
	Entity   core.Entity   `toml:"entity"`
	Duration time.Duration `toml:"duration"`
}

// --- Composite ---

// CompositeMemberDestroyedPayload signals a composite member was typed
type CompositeMemberDestroyedPayload struct {
	HeaderEntity   core.Entity `toml:"header_entity"`
	MemberEntity   core.Entity `toml:"member_entity"`
	Entity         core.Entity `toml:"entity"` // Cursor that typed it; 0 for non-typed loss
	Char           rune        `toml:"char"`
	RemainingCount int         `toml:"remaining_count"` // CountEntities of remaining live members after this one
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

// --- Cursor ---

// CursorStatePayload is one cursor's owner-authored state (D-13), sent by the
// instance that simulates it and applied by every other. This is the only value
// transfer in the design; everything else re-derives.
//
// Shield and Combat are split to their cursor fields: both stores also carry
// quasar, loot and species state, which is re-derived and must not travel.
// ShieldActive, ShieldInvRxSq/RySq and EmberActive reproduce the remote cursor's
// presentation and owner-local interactions. No shared outcome reads this snapshot.
// CursorViewComponent.Orbs is absent: it names player-domain entities (D-4).
// Durations are nanoseconds so the TOML round trip is exact.
type CursorStatePayload struct {
	WeaponCharges  []int   `toml:"weapon_charges"`
	WeaponCooldown []int64 `toml:"weapon_cooldown"`

	Entity core.Entity `toml:"entity"` // shared, so both instances name the same one
	Seq    uint64      `toml:"seq"`    // owner's sync counter; a reordered packet is dropped

	Energy   int64 `toml:"energy"`
	Heat     int   `toml:"heat"`
	Overheat int   `toml:"overheat"`

	ShieldRadiusX float64 `toml:"shield_radius_x"`
	ShieldRadiusY float64 `toml:"shield_radius_y"`
	ShieldInvRxSq float64 `toml:"shield_inv_rx_sq"`
	ShieldInvRySq float64 `toml:"shield_inv_ry_sq"`

	BoostRemaining   int64 `toml:"boost_remaining"`
	BoostTotal       int64 `toml:"boost_total"`
	MainFireCooldown int64 `toml:"main_fire_cooldown"`

	HitPoints      int   `toml:"hit_points"`
	DamageImmunity int64 `toml:"damage_immunity"`

	ErrorFlash     int64 `toml:"error_flash"`
	BurstFlash     int64 `toml:"burst_flash"`
	BlinkRemaining int64 `toml:"blink_remaining"`
	BlinkType      int   `toml:"blink_type"`
	BlinkLevel     int   `toml:"blink_level"`

	PulseOriginX   int   `toml:"pulse_origin_x"`
	PulseOriginY   int   `toml:"pulse_origin_y"`
	PulseDuration  int64 `toml:"pulse_duration"`
	PulseRemaining int64 `toml:"pulse_remaining"`

	Slot         uint8 `toml:"slot"`
	EmberActive  bool  `toml:"ember_active"`
	ShieldActive bool  `toml:"shield_active"`
	BoostActive  bool  `toml:"boost_active"`
	BlinkActive  bool  `toml:"blink_active"`
	PulseActive  bool  `toml:"pulse_active"`
}

// CursorSpawnRequestPayload asks for a cursor entity
// Center overrides X/Y; Auto overrides Slot with the lowest free index
type CursorSpawnRequestPayload struct {
	X       int    `toml:"x"`
	Y       int    `toml:"y"`
	Slot    uint8  `toml:"slot"`
	Control uint8  `toml:"control"` // component.ControlKind
	PeerID  uint32 `toml:"peer_id"` // Remote owner when Control is ControlRemote
	Auto    bool   `toml:"auto"`
	Center  bool   `toml:"center"`
}

// CursorSpawnedPayload announces a created cursor
type CursorSpawnedPayload struct {
	Entity core.Entity `toml:"entity"`
	X      int         `toml:"x"`
	Y      int         `toml:"y"`
	Slot   uint8       `toml:"slot"`
}

// CursorDespawnRequestPayload selects cursors to destroy
// All wins over Entity, which wins over Slot
type CursorDespawnRequestPayload struct {
	Entity core.Entity `toml:"entity"`
	Slot   uint8       `toml:"slot"`
	All    bool        `toml:"all"`
}

// CursorDespawnedPayload announces a destroyed cursor
type CursorDespawnedPayload struct {
	Entity core.Entity `toml:"entity"`
	Slot   uint8       `toml:"slot"`
}

// CursorMoveRequestPayload asks for an absolute placement of an explicitly named cursor;
// the producer has already validated the destination.
type CursorMoveRequestPayload struct {
	Entity core.Entity `toml:"entity"`
	X      int         `toml:"x"`
	Y      int         `toml:"y"`
}

// CursorMovedPayload announces the position CursorSystem applied
type CursorMovedPayload struct {
	Entity core.Entity `toml:"entity"`
	X      int         `toml:"x"`
	Y      int         `toml:"y"`
}

// CursorSetLocalPayload names the roster slot input and the camera follow
type CursorSetLocalPayload struct {
	Slot uint8 `toml:"slot"`
}

// --- Fuse ---

// FuseEffect defines visual effect type for fusion animations
type FuseEffect int

const (
	FuseEffectNone        FuseEffect = iota
	FuseEffectSpirit                 // Converging spirit trails
	FuseEffectMaterialize            // Reverse beam convergence
)

// FuseSwarmRequestPayload contains the two drains to fuse
type FuseSwarmRequestPayload struct {
	DrainA core.Entity `toml:"drain_a"`
	DrainB core.Entity `toml:"drain_b"`
	Effect FuseEffect  `toml:"effect"` // Defaults to FuseEffectSpirit (0)
}

// --- Quasar ---

// QuasarSpawnRequestPayload contains coordinates for creation
type QuasarSpawnRequestPayload struct {
	X int `toml:"x"`
	Y int `toml:"y"`
}

// --- Swarm ---

// SwarmSpawnRequestPayload contains coordinates for creation
type SwarmSpawnRequestPayload struct {
	X int `toml:"x"`
	Y int `toml:"y"`
}

// --- Post-Process ---

// StrobeRequestPayload configures screen flash effect
type StrobeRequestPayload struct {
	Color      color.RGB `toml:"color"`
	Intensity  float64   `toml:"intensity"`   // Base intensity 0.0-1.0
	DurationMs int64     `toml:"duration_ms"` // 0 = default value from parameters
}

// --- Spirit ---

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

// --- Lightning ---

// LightningSpawnRequestPayload contains parameters to spawn a lightning entity
type LightningSpawnRequestPayload struct {
	Owner        core.Entity                  `toml:"owner"`
	OriginX      int                          `toml:"origin_x"`
	OriginY      int                          `toml:"origin_y"`
	TargetX      int                          `toml:"target_x"`
	TargetY      int                          `toml:"target_y"`
	OriginEntity core.Entity                  `toml:"origin_entity"` // 0 = use OriginX/Y as static
	TargetEntity core.Entity                  `toml:"target_entity"` // 0 = use TargetX/Y as static
	PathSeed     uint64                       // 0 = system generates
	Duration     time.Duration                `toml:"duration"`
	ColorType    component.LightningColorType `toml:"color_type"`
	Tracked      bool                         `toml:"tracked"` // If true, entity persists and target can be updated
}

// LightningUpdateRequestPayload updates target position for tracked lightning
type LightningUpdateRequestPayload struct {
	Owner   core.Entity `toml:"owner"`
	TargetX int         `toml:"target_x"`
	TargetY int         `toml:"target_y"`
}

// LightningDespawnRequestPayload specifies lightning removal criteria
type LightningDespawnRequestPayload struct {
	Owner        core.Entity `toml:"owner"`         // Required
	TargetEntity core.Entity `toml:"target_entity"` // 0 = removes all from owner
}

// --- Combat ---

// CombatAttackDirectRequestPayload contains direct attack information.
// HasOrigin/HasVelocity carry the emitter's geometry explicitly, so a player-domain
// emitter (orb, cleaner, bullet) describes itself without naming its entity.
type CombatAttackDirectRequestPayload struct {
	OwnerEntity  core.Entity                `toml:"owner_entity"`
	OriginEntity core.Entity                `toml:"origin_entity"`
	TargetEntity core.Entity                `toml:"target_entity"`
	HitEntity    core.Entity                `toml:"hit_entity"`
	OriginVelX   float64                    `toml:"origin_vel_x"`
	OriginVelY   float64                    `toml:"origin_vel_y"`
	OriginX      int                        `toml:"origin_x"`
	OriginY      int                        `toml:"origin_y"`
	AttackType   component.CombatAttackType `toml:"attack_type"`
	HasOrigin    bool                       `toml:"has_origin"`
	HasVelocity  bool                       `toml:"has_velocity"`
	ChainDepth   uint8                      `toml:"chain_depth"`
}

// IsDerived reports a chain follow-up, which the receiver produces from the root
// hit the wire carried; sending both would apply the chain twice (D-5).
func (p *CombatAttackDirectRequestPayload) IsDerived() bool { return p.ChainDepth > 0 }

// CombatAttackAreaRequestPayload contains area attack information.
// An empty HitEntities is the implicit single-hit form: the hit set is exactly
// {TargetEntity}. Single-cell targets use it to emit no per-event slice.
type CombatAttackAreaRequestPayload struct {
	HitEntities  []core.Entity              `toml:"hit_entities"`
	AttackType   component.CombatAttackType `toml:"attack_type"`
	OwnerEntity  core.Entity                `toml:"owner_entity"`
	OriginEntity core.Entity                `toml:"origin_entity"`
	TargetEntity core.Entity                `toml:"target_entity"`
	// HasOrigin marks origin position for knockback direction (e.g., explosion center)
	// Without it OriginEntity position is used
	HasOrigin  bool  `toml:"has_origin"`
	OriginX    int   `toml:"origin_x"`
	OriginY    int   `toml:"origin_y"`
	ChainDepth uint8 `toml:"chain_depth"`
}

// CombatHealRequestPayload adds uncapped hit points to a live target.
type CombatHealRequestPayload struct {
	TargetEntity core.Entity `toml:"target_entity"`
	Amount       int         `toml:"amount"`
}

// SpeciesKilledPayload carries species identity, death position, and player credit.
// KillerEntity is the cursor that dealt the fatal damage, or zero for lifecycle
// deaths and attacks not owned by a cursor.
type SpeciesKilledPayload struct {
	Entity       core.Entity           `toml:"entity"`
	KillerEntity core.Entity           `toml:"killer_entity"`
	X            int                   `toml:"x"` // -1 when no death position is available
	Y            int                   `toml:"y"` // -1 when no death position is available
	Species      component.SpeciesType `toml:"species"`
	SubType      uint8                 `toml:"sub_type"` // Species variant (e.g. EyeType)
}

// SpeciesCreatedPayload announces one fully initialized species instance.
// Genes and EvalID are populated only for GA-managed instances.
type SpeciesCreatedPayload struct {
	Entity      core.Entity           `toml:"entity"`
	Species     component.SpeciesType `toml:"species"`
	SubType     uint8                 `toml:"sub_type"`     // Species variant (e.g. EyeType)
	X           int                   `toml:"x"`            // -1 when the species has no single position
	Y           int                   `toml:"y"`            // -1 when the species has no single position
	MemberCount int                   `toml:"member_count"` // Composite members; zero for non-composites
	Genes       []float64             `toml:"genes"`
	EvalID      uint64                `toml:"eval_id"`
}

// --- Loot ---

// LootSpawnRequestPayload requests direct loot spawn (bypasses drop tables)
type LootSpawnRequestPayload struct {
	Type component.LootType `toml:"type"`
	X    int                `toml:"x"`
	Y    int                `toml:"y"`
}

// --- Missile ---

// MissileSpawnRequestPayload contains missile spawn parameters
type MissileSpawnRequestPayload struct {
	Targets     []core.Entity `toml:"targets"`      // Prioritized target entities
	HitEntities []core.Entity `toml:"hit_entities"` // Corresponding hit points (member or same as target)
	OwnerEntity core.Entity   `toml:"owner_entity"` // Cursor
	OriginX     int           `toml:"origin_x"`
	OriginY     int           `toml:"origin_y"`
	Count       int           `toml:"count"`
}

// --- Bullet ---

// BulletSpawnRequestPayload requests creation of a linear projectile
type BulletSpawnRequestPayload struct {
	OriginX     float64                `toml:"origin_x"`
	OriginY     float64                `toml:"origin_y"`
	VelX        float64                `toml:"vel_x"`
	VelY        float64                `toml:"vel_y"`
	Owner       core.Entity            `toml:"owner"`
	MaxLifetime time.Duration          `toml:"max_lifetime"`
	Damage      component.BulletDamage `toml:"damage"`
}

// --- Marker ---

// MarkerSpawnRequestPayload for marker creation
type MarkerSpawnRequestPayload struct {
	X         int                   `toml:"x"`
	Y         int                   `toml:"y"`
	Width     int                   `toml:"width"`
	Height    int                   `toml:"height"`
	Intensity float64               `toml:"intensity"` // 0.0-1.0
	Duration  time.Duration         `toml:"duration"`
	PulseRate float64               `toml:"pulse_rate"` // Hz, 0 = none
	Color     color.RGB             `toml:"color"`
	Shape     component.MarkerShape `toml:"shape"`
	FadeMode  uint8                 `toml:"fade_mode"` // 0=none, 1=out, 2=in
}

// --- Motion Marker ---

// MotionMarkerShowPayload contains direction for colored marker display
type MotionMarkerShowPayload struct {
	DirectionX int `toml:"direction_x"` // -1, 0, 1
	DirectionY int `toml:"direction_y"` // -1, 0, 1
}

// --- Mode ---

// ModeChangedPayload contains the new mode
type ModeChangedPayload struct {
	Mode core.GameMode `toml:"mode"`
}

// --- Wall ---

// WallSpawnRequestPayload contains parameters for single wall cell creation
type WallSpawnRequestPayload struct {
	X             int                     `toml:"x"`
	Y             int                     `toml:"y"`
	Char          rune                    `toml:"char"`
	FgColor       color.RGB               `toml:"fg_color"`
	BgColor       color.RGB               `toml:"bg_color"`
	CollisionMode WallBatchCollisionMode  `toml:"collision_mode"`
	BoxStyle      component.BoxDrawStyle  `toml:"box_style"` // Box-drawing style (0=none, 1=single, 2=double)
	RenderFg      bool                    `toml:"render_fg"`
	RenderBg      bool                    `toml:"render_bg"`
	BlockMask     component.WallBlockMask `toml:"block_mask"`
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
	Cells         []component.WallCellDef `toml:"cells"`
	X             int                     `toml:"x"`          // Anchor position
	Y             int                     `toml:"y"`          // Anchor position
	BlockMask     component.WallBlockMask `toml:"block_mask"` // Applied to all cells
	BoxStyle      component.BoxDrawStyle  `toml:"box_style"`  // Applied to all cells
	CollisionMode WallBatchCollisionMode  `toml:"collision_mode"`
	Composite     bool                    `toml:"composite"` // If true, create header/member structure
}

// WallCompositeSpawnRequestPayload contains parameters for multi-cell wall structure
type WallCompositeSpawnRequestPayload struct {
	Cells         []component.WallCellDef `toml:"cells"`
	X             int                     `toml:"x"` // Anchor position
	Y             int                     `toml:"y"`
	BlockMask     component.WallBlockMask `toml:"block_mask"` // Applied to all cells
	CollisionMode WallBatchCollisionMode  `toml:"collision_mode"`
	BoxStyle      component.BoxDrawStyle  `toml:"box_style"` // Applied to all cells
}

// WallPatternSpawnRequestPayload contains parameters for pattern-based wall creation
type WallPatternSpawnRequestPayload struct {
	Path          string                  `toml:"path"`       // Path to .vifimg file
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

// MazeRoomSpec defines an explicit room in maze
type MazeRoomSpec struct {
	CenterX int `toml:"center_x"` // 0 = random placement
	CenterY int `toml:"center_y"` // 0 = random placement
	Width   int `toml:"width"`    // 0 = use default
	Height  int `toml:"height"`   // 0 = use default
}

// MazeSpawnRequestPayload configures maze generation
type MazeSpawnRequestPayload struct {
	// Reference types (GC scan boundary)
	Rooms []MazeRoomSpec `toml:"rooms"`

	// 8-byte scalars
	CellWidth         int                     `toml:"cell_width"`
	CellHeight        int                     `toml:"cell_height"`
	RoomCount         int                     `toml:"room_count"`
	DefaultRoomWidth  int                     `toml:"default_room_width"`
	DefaultRoomHeight int                     `toml:"default_room_height"`
	Braiding          float64                 `toml:"braiding"`
	BlockMask         component.WallBlockMask `toml:"block_mask"`

	// Mixed/smaller structs and scalars
	Visual        component.WallVisualConfig `toml:"visual"`
	CollisionMode WallBatchCollisionMode     `toml:"collision_mode"`
}

// --- Fadeout ---

// FadeoutSpawnPayload contains parameters for single fadeout effect
type FadeoutSpawnPayload struct {
	X       int  `toml:"x"`
	Y       int  `toml:"y"`
	Char    rune `toml:"char"` // 0 = bg-only
	FgColor color.RGB
	BgColor color.RGB
}

// --- Pylon ---

// PylonSpawnRequestPayload contains parameters for pylon creation
type PylonSpawnRequestPayload struct {
	X       int `toml:"x"`
	Y       int `toml:"y"`
	RadiusX int `toml:"radius_x"`
	RadiusY int `toml:"radius_y"`
	MinHP   int `toml:"min_hp"` // HP at edge, when == MaxHP all members uniform
	MaxHP   int `toml:"max_hp"` // HP at center
}

// --- Snake ---

// SnakeSpawnRequestPayload contains coordinates for snake creation
type SnakeSpawnRequestPayload struct {
	X            int `toml:"x"`
	Y            int `toml:"y"`
	SegmentCount int `toml:"segment_count"` // Body segments to spawn (0 = default)
}

// --- Navigation ---

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

// RouteGraphRequestPayload requests route graph computation for a gateway-target pair
type RouteGraphRequestPayload struct {
	SourceX       int    `toml:"source_x"` // Gateway spawn position
	SourceY       int    `toml:"source_y"`
	RouteGraphID  uint32 `toml:"route_graph_id"` // Opaque ID, typically uint32(gatewayEntity); valid only while route-graph anchors are shared, the domain tag sits above bit 32
	TargetGroupID uint8  `toml:"target_group_id"`
}

// RouteGraphComputedPayload signals route graph computation completion
type RouteGraphComputedPayload struct {
	RouteCount   int    `toml:"route_count"`
	RouteGraphID uint32 `toml:"route_graph_id"`
}

// --- Eye ---

// EyeSpawnRequestPayload contains eye spawn parameters
type EyeSpawnRequestPayload struct {
	Genes  []float64 `toml:"genes"`
	EvalID uint64    `toml:"eval_id"`

	X             int               `toml:"x"`
	Y             int               `toml:"y"`
	RouteID       int               `toml:"route_id"`       // -1 = shared flow field
	RouteGraphID  uint32            `toml:"route_graph_id"` // 0 = no route graph
	Type          component.EyeType `toml:"type"`
	TargetGroupID uint8             `toml:"target_group_id"`
}

// --- Tower ---

// TowerSpawnRequestPayload contains parameters for tower creation
type TowerSpawnRequestPayload struct {
	Entity        core.Entity         `toml:"entity"`
	X             int                 `toml:"x"`
	Y             int                 `toml:"y"`
	RadiusX       int                 `toml:"radius_x"`
	RadiusY       int                 `toml:"radius_y"`
	MinHP         int                 `toml:"min_hp"`
	MaxHP         int                 `toml:"max_hp"`
	Type          component.TowerType `toml:"type"`
	TargetGroupID uint8               `toml:"target_group_id"` // Navigation target group
}

// --- Gateway ---

// GatewaySpawnRequestPayload requests creation of a gateway entity
type GatewaySpawnRequestPayload struct {
	AnchorEntity        core.Entity `toml:"anchor_entity"`          // Parent entity (pylon header)
	BaseIntervalMs      int         `toml:"base_interval_ms"`       // Spawn interval in milliseconds
	RateMultiplier      float64     `toml:"rate_multiplier"`        // <1.0 = accelerating, 1.0 = constant
	RateAccelIntervalMs int         `toml:"rate_accel_interval_ms"` // How often multiplier applied (ms), 0 = disabled
	MinIntervalMs       int         `toml:"min_interval_ms"`        // Floor interval in milliseconds
	OffsetX             int         `toml:"offset_x"`
	OffsetY             int         `toml:"offset_y"`
	Species             uint8       `toml:"species"`         // component.SpeciesType
	SubType             uint8       `toml:"sub_type"`        // Species variant (e.g. EyeType)
	GroupID             uint8       `toml:"group_id"`        // Target group for spawned entities
	UseRouteGraph       bool        `toml:"use_route_graph"` // If true, request route graph computation for this gateway
	PopulationID        uint32      `toml:"population_id"`
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

// --- Genetic ---

// ParameterBoundDef defines TOML-parsable bounds
type ParameterBoundDef struct {
	Min float64 `toml:"min"`
	Max float64 `toml:"max"`
}

// GeneticRegisterSpeciesPayload dynamically configures a species for GA tracking
type GeneticRegisterSpeciesPayload struct {
	Bounds             []ParameterBoundDef   `toml:"bounds"`
	PerturbationStdDev float64               `toml:"perturbation_std_dev"`
	GeneCount          int                   `toml:"gene_count"`
	ProbeBins          int                   `toml:"probe_bins"` // scout stratification bins for gene[0] (0 = uniform)
	Species            component.SpeciesType `toml:"species"`
	IsComposite        bool                  `toml:"is_composite"`
}

// GeneticAbandonEvalPayload is the information of species type evaluation that should be abandoned
type GeneticAbandonEvalPayload struct {
	EvalID  uint64                `toml:"eval_id"`
	Species component.SpeciesType `toml:"species"`
}

// --- Debug ---

// DebugFlowGroupPayload selects a navigation target group for debug overlays
type DebugFlowGroupPayload struct {
	GroupID uint8 `toml:"group_id"`
}

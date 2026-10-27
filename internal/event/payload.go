package event

import (
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/audio/model"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// --- Engine ---

// --- Level ---

// LevelSetupPayload configures map dimensions and entity lifecycle
type LevelSetupPayload struct {
	Width         int  `toml:"width"`          // New map width in grid cells
	Height        int  `toml:"height"`         // New map height in grid cells
	ClearEntities bool `toml:"clear_entities"` // If true, destroy non-protected entities
	PreserveLoot  bool `toml:"preserve_loot"`  // Encounter transitions keep uncollected rewards
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

// SoundRequestPayload names the sound to play: Go emitters pass the resolved ID,
// configuration passes the name, which AudioSystem resolves when ID is zero
type SoundRequestPayload struct {
	ID   model.SoundID `toml:"sound_id"`
	Name string        `toml:"sound"`
}

type SoundMuteToggleMode uint8

const (
	MuteCycle  SoundMuteToggleMode = iota // ignore Mask, advance the rotation
	MuteToggle                            // flip the channels named in Mask
	MuteSet                               // set the mask verbatim
)

var muteToggleModeNames = [...]string{MuteCycle: "cycle", MuteToggle: "toggle", MuteSet: "set"}

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
	Intensity     model.Intensity `toml:"intensity"`
	BeatPattern   model.PatternID `toml:"beat_pattern"`
	MelodyPattern model.PatternID `toml:"melody_pattern"`
}

// BeatPatternRequestPayload requests beat pattern transition
type BeatPatternRequestPayload struct {
	Pattern        model.PatternID `toml:"pattern"`
	TransitionTime time.Duration   `toml:"transition_time"` // 0 = default
	Quantize       bool            `toml:"quantize"`        // Wait for bar boundary
}

// MelodyNoteRequestPayload triggers immediate note
type MelodyNoteRequestPayload struct {
	Note       int                  `toml:"note"`       // MIDI note number
	Velocity   float64              `toml:"velocity"`   // 0.0-1.0
	Duration   time.Duration        `toml:"duration"`   // 0 = use instrument default
	Instrument model.InstrumentType `toml:"instrument"` // 0 = default (piano)
}

// MelodyPatternRequestPayload requests melody pattern transition
type MelodyPatternRequestPayload struct {
	Pattern        model.PatternID `toml:"pattern"`
	RootNote       int             `toml:"root_note"` // MIDI note for pattern root
	TransitionTime time.Duration   `toml:"transition_time"`
	Quantize       bool            `toml:"quantize"`
}

// MusicIntensityPayload adjusts overall music intensity
type MusicIntensityPayload struct {
	Intensity      model.Intensity `toml:"intensity"`
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
// takes. Like a departure it is a crossing rather than a local reaction: every
// instance has to add the cursor at the same tick or their shared entity creation
// order diverges (D-11). Claim is the digest the cursor carries for its participant's
// return; Record is what one coming back to its Wraith brings of the game it left.
type ParticipantJoinedPayload struct {
	Record      *CursorRecord `toml:"record"`
	Claim       string        `toml:"claim,omitempty"`
	Holder      uint32        `toml:"holder"`
	Participant uint32        `toml:"participant"`
	Slot        uint8         `toml:"slot"`
}

// ParticipantDepartedPayload names the roster slot a departed participant held. It
// is the crossing that makes a departure shared state: a link disconnect is observed
// only by a direct neighbour and at a moment of that neighbour's own choosing, so the
// removal has to travel as an artifact with an apply tick, like any other outcome
// every instance must reach together.
type ParticipantDepartedPayload struct {
	// Dismissed removes the cursor for good, as the host's drop, a holder's seat drop
	// and an expired or yielded Wraith do. Any other departure turns the cursor
	// Wraith, to wait for the participant's return.
	Dismissed   bool   `toml:"dismissed"`
	Participant uint32 `toml:"participant"`
	Slot        uint8  `toml:"slot"`
}

// PlayoutLeadPayload carries the ticks this instance stamps its own crossings with.
// Journaled, because a measurement is not something a reproduction can re-derive.
type PlayoutLeadPayload struct {
	Ticks uint64 `toml:"ticks"`
}

// SessionStatePayload is the session a world's simulation reads (engine.SessionState).
type SessionStatePayload struct {
	Live        bool   `toml:"live"`
	Participant uint32 `toml:"participant"`
	Authority   uint32 `toml:"authority"`
	SeatsOnly   bool   `toml:"seats_only"`
	Peers       uint32 `toml:"peers"`
}

// NetworkDisconnectPayload signals peer disconnection
type NetworkDisconnectPayload struct {
	PeerID uint32 `toml:"peer_id"`
}

// --- Meta ---

// GameResetPayload requests a game restart; Purge additionally clears operator
// session state, and Wait holds the new game until its player's first press
type GameResetPayload struct {
	Purge bool `toml:"purge"`
	Wait  bool `toml:"wait"`
}

// CursorDefeatStatePayload carries one owner's combined heat/energy state.
type CursorDefeatStatePayload struct {
	Entity       core.Entity `toml:"entity"`
	Defeated     bool        `toml:"defeated"`
	ProducedTick uint64      `toml:"produced_tick"`
}

// CursorCrushedPayload names the cursor scrolling rock pressed against the left edge
type CursorCrushedPayload struct {
	Entity core.Entity `toml:"entity"`
}

// CursorScopePayload scopes local effects to an owned cursor, or all local cursors
// when zero. Shared FSM emissions need this scope to preserve D-6.
type CursorScopePayload struct {
	Entity core.Entity `toml:"entity"`
}

// DrainDefeatedPayload names the shared cursor whose player domain produced one
// personal drain defeat. When a shared threshold is crossed, this is the causal
// election key that selects exactly one player-domain continuation.
type DrainDefeatedPayload struct {
	Entity core.Entity `toml:"entity"`
}

// MetaStatusMessagePayload is a message for the status bar: a notice, or an alert
// when a failure or a change to the session must be seen (parameter.StatusKind).
type MetaStatusMessagePayload struct {
	Message string `toml:"message"`
	Alert   bool   `toml:"alert"`
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

// FSMTransitionPayload is one transition the authority's FSM took, committed and
// applied at one tick on every instance (D-20). Owner and Index name it in the
// graph every instance shares; Vars are the fields its trigger captured.
type FSMTransitionPayload struct {
	Vars    []FSMVar  `toml:"vars,omitempty"`
	Region  string    `toml:"region"`
	Owner   int       `toml:"owner"`
	Index   int       `toml:"index"`
	Trigger EventType `toml:"trigger,omitempty"`
	// Then is the chain of unguarded Tick hops the authority folded into this
	// crossing, each taken after the one before it at the committed tick.
	Then []FSMHop `toml:"then,omitempty"`
}

// FSMHop is one transition of a folded chain: the state owning it and its index.
type FSMHop struct {
	Owner int `toml:"owner"`
	Index int `toml:"index"`
}

// FSMVar is one FSM variable a transition sets.
type FSMVar struct {
	Name  string `toml:"name"`
	Value int64  `toml:"value"`
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
	Delta      int                       `toml:"delta"`      // Magnitude; the delta class supplies direction
	Percentage bool                      `toml:"percentage"` // Delta is a percentage of current energy magnitude
	Type       component.EnergyDeltaType `toml:"type"`
}

// EnergySetPayload contains energy value. Weapons grants every weapon after the
// set, so a polarity crossing the set causes strips the old ones first.
type EnergySetPayload struct {
	Entity  core.Entity `toml:"entity"`
	Value   int         `toml:"value"`
	Weapons bool        `toml:"weapons"`
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
	Weapon component.WeaponType `toml:"weapon"`
}

// WeaponRemoveRequestPayload takes one kind from a cursor, the innate gun included
type WeaponRemoveRequestPayload struct {
	Entity core.Entity          `toml:"entity"`
	Weapon component.WeaponType `toml:"weapon"`
}

// WeaponFireRequestPayload names the cursor firing main and its ready loadout
type WeaponFireRequestPayload struct {
	Entity core.Entity `toml:"entity"`
}

// GunAimRequestPayload is the cardinals the gun fires along, component.TracerDirection
// bits (right 1, left 2, down 4, up 8); zero restores the gun's own four. A path
// replaces them: the gun's levels in order, up to parameter.GunPathMax, a gun of n
// charges firing level n; an empty one takes the gun off its path.
type GunAimRequestPayload struct {
	Directions component.TracerDirection `toml:"directions"`
	Path       []component.GunLevel      `toml:"path"`
}

// MountRequestPayload puts one weapon on a Shared host, replacing any it carried.
// Zero interval, range and width take the weapon's hosted defaults; zero muzzle fires
// from the host cell. A ray's Lane (1-8, vmath.Octants) fixes its direction; 0 aims.
type MountRequestPayload struct {
	Host       core.Entity          `toml:"host"`
	Weapon     component.WeaponType `toml:"weapon"`
	IntervalMs int                  `toml:"interval_ms"`
	Range      int                  `toml:"range"`
	Muzzle     float64              `toml:"muzzle"`
	Lane       int                  `toml:"lane"`
	Width      int                  `toml:"width"`
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

type HeatSpendRequestPayload struct {
	Entity core.Entity `toml:"entity"`
	Amount int         `toml:"amount"`
}

// HeatBurstPayload names the cursor whose ember lit, or burnt its heat out
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

// KeyCapturePayload names the keys to capture, empty to release them. Entity scopes
// it to the instance driving that cursor, every instance when zero. Releases asks a
// console keyboard for releases too, switching it raw while the capture holds.
type KeyCapturePayload struct {
	Keys     string      `toml:"keys"`
	Entity   core.Entity `toml:"entity"`
	Releases bool        `toml:"releases"`
}

// InputSource says what made an input. Every press is released: by its own release
// where the source reports one, at once where it reports presses alone.
type InputSource uint8

const (
	InputKey          InputSource = iota
	InputPointer                  // the left button
	InputPointerRight             // the right button
	InputWheelUp                  // a wheel notch, released as it presses
	InputWheelDown                // a wheel notch, released as it presses
	InputTouch                    // a finger, on a host that reports one
)

var inputSourceNames = [...]string{
	InputKey:          "key",
	InputPointer:      "pointer",
	InputPointerRight: "pointer_right",
	InputWheelUp:      "wheel_up",
	InputWheelDown:    "wheel_down",
	InputTouch:        "touch",
}

// InputPayload names the roster slot that pressed or released a captured key, the key,
// what made the input, and the tick it was made at: engine.ticks on the instance
// that made it, which a scenario judges timing by however late it applies. Lost
// marks a release no player made: the press was a tap, or play took the hold away.
type InputPayload struct {
	Slot   uint8       `toml:"slot"`
	Key    string      `toml:"key"`
	Source InputSource `toml:"source"`
	Tick   uint64      `toml:"tick"`
	Lost   bool        `toml:"lost"`
}

// BoundTick keeps a crossed input's tick to the ones its crossing could have left at:
// before the tick it applies at, and within the longest lead and latest commit of it
func (p *InputPayload) BoundTick(applyTick uint64) {
	latest := max(applyTick, 1) - 1
	p.Tick = min(max(p.Tick, latest-min(latest, parameter.NetworkApplyHorizonTicks)), latest)
}

// InputReportPayload says what a roster slot's keyboard reports while keys are
// captured: releases, or presses alone, each a tap released as it is pressed
type InputReportPayload struct {
	Slot     uint8 `toml:"slot"`
	Releases bool  `toml:"releases"`
}

// DeleteRangeType defines the scope of deletion
type DeleteRangeType int

const (
	DeleteRangeChar DeleteRangeType = iota
	DeleteRangeLine
)

var deleteRangeNames = [...]string{DeleteRangeChar: "char", DeleteRangeLine: "line"}

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
	X          int                   `toml:"x"`          // Top-left X
	Y          int                   `toml:"y"`          // Top-left Y
	AreaWidth  int                   `toml:"area_width"` // 0 or 1 = single cell
	AreaHeight int                   `toml:"area_height"`
	Type       component.SpawnType   `toml:"type"`
	Species    component.SpeciesType `toml:"species"` // the species its completion lands; SpeciesNone lands nothing
}

// MaterializeCompletedPayload carries details about a completed materialization
type MaterializeCompletedPayload struct {
	X       int                   `toml:"x"`
	Y       int                   `toml:"y"`
	Type    component.SpawnType   `toml:"type"`
	Species component.SpeciesType `toml:"species"`
}

// --- Flash ---

// FlashRequestPayload contains parameters for destruction flash effect
type FlashRequestPayload struct {
	X    int  `toml:"x"`
	Y    int  `toml:"y"`
	Char rune `toml:"char"`
}

// --- Explosion ---

// ExplosionType differentiates local presentation palettes.
type ExplosionType uint8

const (
	ExplosionTypeDust    ExplosionType = iota // Cyan palette
	ExplosionTypeMissile                      // Visual only, warm palette
	ExplosionTypeEye                          // Self-destruct explosion with character noise
)

var explosionTypeNames = [...]string{ExplosionTypeDust: "dust", ExplosionTypeMissile: "missile", ExplosionTypeEye: "eye"}

// ExplosionRequestPayload requires explicit attack data for the D-3 explosion.
// CrossingID seeds derived hits independently of each receiver's timing.
type ExplosionRequestPayload struct {
	CrossingID
	Entity core.Entity                `toml:"entity"` // Owner cursor, credited for damage
	X      int                        `toml:"x"`
	Y      int                        `toml:"y"`
	Radius float64                    `toml:"radius"` // 0 = ExplosionFieldRadius
	Attack component.CombatAttackType `toml:"attack"`
	// Charged says the weapon was at its charge cap, so a profile's Charged chain fires
	Charged bool `toml:"charged,omitempty"`
}

// ExplosionBatchRequestPayload describes one combat artifact made of several
// centers sharing geometry and combat family. Pooled: the consumer releases it.
// Producers must truncate Centers at parameter.ExplosionRequestCenterCap so a
// map-wide detonation cannot flood the event queue.
type ExplosionBatchRequestPayload struct {
	CrossingID
	Centers []ExplosionCenterEntry
	Entity  core.Entity
	Radius  float64
	Attack  component.CombatAttackType
}

// ExplosionVisualRequestPayload is player-domain presentation for one center.
type ExplosionVisualRequestPayload struct {
	X        int           `toml:"x"`
	Y        int           `toml:"y"`
	Radius   float64       `toml:"radius"`
	Duration time.Duration `toml:"duration"`
	Type     ExplosionType `toml:"type"`
}

// PulseVisualRequestPayload is player-domain presentation for one disruptor pulse.
type PulseVisualRequestPayload struct {
	X       int                     `toml:"x"`
	Y       int                     `toml:"y"`
	Palette component.WeaponPalette `toml:"palette"`
}

// ExplosionVisualBatchRequestPayload is player-domain presentation for a group
// of centers. It is not pooled: the local queue owns the producer's slice copy.
type ExplosionVisualBatchRequestPayload struct {
	Centers  []ExplosionCenterEntry
	Radius   float64
	Duration time.Duration
	Type     ExplosionType
}

// --- Particle ---

// ParticleSpawnPayload contains parameters to spawn one behavior-selected particle.
type ParticleSpawnPayload struct {
	Behavior      component.ParticleBehavior `toml:"behavior"`
	X             int                        `toml:"x"`
	Y             int                        `toml:"y"`
	Char          rune                       `toml:"char"`
	Level         component.GlyphLevel       `toml:"level"`           // Dust lifetime and colour; falling behaviors ignore it
	SkipStartCell bool                       `toml:"skip_start_cell"` // True: particle skips interaction at spawn position
}

// ParticleWavePayload selects the behavior of a particle wave across the map, or across
// one button's columns when Button names its place in the row
type ParticleWavePayload struct {
	Behavior component.ParticleBehavior `toml:"behavior"`
	Button   int                        `toml:"button"`
}

// ParticleVortexPayload selects the behavior glyphs turn into and the vortex holds, the
// button it circles, by its place in the row, and for how long before it lets go
type ParticleVortexPayload struct {
	Behavior   component.ParticleBehavior `toml:"behavior"`
	Button     int                        `toml:"button"`
	DurationMs int64                      `toml:"duration_ms"`
}

// --- Death ---

// DeathRequestPayload contains a death request for one or more entities.
// EffectEvent selects the emitted effect; Behavior discriminates the unified
// particle effect when EffectEvent is EventParticleSpawnOne.
type DeathRequestPayload struct {
	Entities    []core.Entity              `toml:"entities"`
	EffectEvent EventType                  `toml:"effect_event"`
	Behavior    component.ParticleBehavior `toml:"behavior"`
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

// CursorStatePayload carries owner-authored Shared values (D-13), each cell omitted
// while zero, so a sync is the size of what is set. Shield and combat values are
// cursor fields, never private player entity handles; durations are ns.
type CursorStatePayload struct {
	WeaponCharges  []int   `toml:"weapon_charges,omitempty"`
	WeaponCooldown []int64 `toml:"weapon_cooldown,omitempty"`

	Entity core.Entity `toml:"entity,omitempty"` // shared, so both instances name the same one
	Seq    uint64      `toml:"seq,omitempty"`    // owner's sync counter; a reordered packet is dropped

	Energy   int64 `toml:"energy,omitempty"`
	Heat     int   `toml:"heat,omitempty"`
	Overheat int   `toml:"overheat,omitempty"`

	ShieldRadiusX float64 `toml:"shield_radius_x,omitempty"`
	ShieldRadiusY float64 `toml:"shield_radius_y,omitempty"`
	ShieldInvRxSq float64 `toml:"shield_inv_rx_sq,omitempty"`
	ShieldInvRySq float64 `toml:"shield_inv_ry_sq,omitempty"`

	BoostRemaining   int64 `toml:"boost_remaining,omitempty"`
	BoostTotal       int64 `toml:"boost_total,omitempty"`
	MainFireCooldown int64 `toml:"main_fire_cooldown,omitempty"`

	HitPoints           int                                     `toml:"hit_points,omitempty"`
	DamageImmunity      int64                                   `toml:"damage_immunity,omitempty"`
	DamageImmunitySpent [component.CombatAttackTypeCount]uint32 `toml:"damage_immunity_spent,omitempty"` // by component.CombatAttackType

	ErrorFlash     int64 `toml:"error_flash,omitempty"`
	BurstFlash     int64 `toml:"burst_flash,omitempty"`
	BlinkRemaining int64 `toml:"blink_remaining,omitempty"`
	BlinkType      int   `toml:"blink_type,omitempty"`
	BlinkLevel     int   `toml:"blink_level,omitempty"`

	Slot         uint8 `toml:"slot,omitempty"`
	EmberActive  bool  `toml:"ember_active,omitempty"`
	ShieldActive bool  `toml:"shield_active,omitempty"`
	BoostActive  bool  `toml:"boost_active,omitempty"`
	BlinkActive  bool  `toml:"blink_active,omitempty"`
}

// Record is the part of an owner's sync its player brings back to its Wraith.
func (p *CursorStatePayload) Record() CursorRecord {
	return CursorRecord{Charges: slices.Clone(p.WeaponCharges), Energy: p.Energy, Heat: p.Heat, HitPoints: p.HitPoints}
}

// CursorSpawnRequestPayload asks for a cursor entity
// Center overrides X/Y; Auto overrides Slot with the lowest free index. A request
// for a slot its participant's own Wraith holds takes the Wraith back, at its cell
// and with Record over the template.
type CursorSpawnRequestPayload struct {
	Record  *CursorRecord `toml:"record"`
	Claim   string        `toml:"claim,omitempty"` // digest of the token its participant returns with
	Holder  uint32        `toml:"holder"`
	X       int           `toml:"x"`
	Y       int           `toml:"y"`
	Heat    int           `toml:"heat"`
	Energy  int           `toml:"energy"`
	Slot    uint8         `toml:"slot"`
	Control uint8         `toml:"control"` // component.ControlKind
	PeerID  uint32        `toml:"peer_id"` // Remote owner when Control is ControlRemote
	Auto    bool          `toml:"auto"`
	Center  bool          `toml:"center"`
}

// CursorRecord is what a player brings back to its Wraith of the game it left: the
// energy, heat, health and loadout its cursor last synced there.
type CursorRecord struct {
	Charges   []int `toml:"charges" json:"charges,omitempty"`
	Energy    int64 `toml:"energy" json:"energy"`
	Heat      int   `toml:"heat" json:"heat"`
	HitPoints int   `toml:"hit_points" json:"hit_points"`
}

// CursorArmRequestPayload sets heat, energy and the ember on the cursor this instance
// drives: the configured starting resources at a join, or a scenario's own values. It
// is local by design: these are D-13 owner-authored state after the shared cursor has
// been created.
type CursorArmRequestPayload struct {
	Heat   int  `toml:"heat"`
	Energy int  `toml:"energy"`
	Ember  bool `toml:"ember"`
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

// CursorDespawnedPayload announces a cursor leaving play. A Wraith keeps its entity,
// so Entity names a destroyed cursor or one that holds nothing but its slot.
type CursorDespawnedPayload struct {
	Entity core.Entity `toml:"entity"`
	Slot   uint8       `toml:"slot"`
}

// CursorWraithRequestPayload names the slot whose cursor turns Wraith: its
// participant left, and the cursor waits where it stood for it to come back.
type CursorWraithRequestPayload struct {
	Slot uint8 `toml:"slot"`
}

// CursorMoveRequestPayload asks for an absolute placement of an explicitly named cursor;
// the producer has already validated the destination. Pointer marks a cell the mouse
// named, which the camera follows with its pointer margins and APM admits at a lower rate.
type CursorMoveRequestPayload struct {
	Entity  core.Entity `toml:"entity"`
	X       int         `toml:"x"`
	Y       int         `toml:"y"`
	Pointer bool        `toml:"pointer,omitempty"`
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

// FuseQuasarRequestPayload selects which participant owns the player-domain
// continuation of one shared escalation decision.
type FuseQuasarRequestPayload struct {
	Entity core.Entity `toml:"entity"`
}

// FuseEffect is what plays a fusion and carries its spawn: the spirits its sources
// fly in, or a beam over the footprint
type FuseEffect int

const (
	FuseEffectNone        FuseEffect = iota // As FuseEffectSpirit
	FuseEffectSpirit                        // Spirits fly the sources in, and the species lands as they arrive
	FuseEffectMaterialize                   // A beam over the footprint, and the species lands as it ends
)

var fuseEffectNames = [...]string{FuseEffectNone: "none", FuseEffectSpirit: "spirit", FuseEffectMaterialize: "materialize"}

// FuseSwarmRequestPayload contains the two drains to fuse
type FuseSwarmRequestPayload struct {
	DrainA core.Entity `toml:"drain_a"`
	DrainB core.Entity `toml:"drain_b"`
	Effect FuseEffect  `toml:"effect"`
}

// FuseBehaviorRequestPayload names the program beamed in and the cell its
// header asks for
type FuseBehaviorRequestPayload struct {
	Program string `toml:"program"`
	X       int    `toml:"x"`
	Y       int    `toml:"y"`
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

// KrakenSpawnRequestPayload defaults (0, 0) to the map center.
type KrakenSpawnRequestPayload struct {
	X int `toml:"x"`
	Y int `toml:"y"`
}

// --- Behavior ---

// BehaviorSpawnRequestPayload names the program whose species spawns at the cell
type BehaviorSpawnRequestPayload struct {
	Program string `toml:"program"`
	X       int    `toml:"x"`
	Y       int    `toml:"y"`
}

// BehaviorCancelRequestPayload names the program cancelled; empty names every program
type BehaviorCancelRequestPayload struct {
	Program string `toml:"program"`
}

// --- Environment ---

// WindStartPayload configures a global gameplay wind. Direction is the direction
// of travel in radians in terminal-map coordinates: zero points right and pi/2
// points down. Force is expressed in relative-mass cells/second squared; each
// affected species receives acceleration Force / its profile mass.
type WindStartPayload struct {
	Force     float64       `toml:"force"`
	Direction float64       `toml:"direction"`
	Duration  time.Duration `toml:"duration"`
}

// DriftStartPayload sets loose glyphs drifting along Direction (radians, as for
// wind) at Speed cells/second, each level at its own parallax fraction and rows
// scaled for the cell aspect. DurationMs 0 drifts until cancelled. Scenery makes
// the text placed while it drifts scenery (ProtectScenery) for life.
type DriftStartPayload struct {
	Direction  float64 `toml:"direction"`
	Speed      float64 `toml:"speed"`
	DurationMs int64   `toml:"duration_ms"`
	Scenery    bool    `toml:"scenery"`
}

// --- Post-Process ---

// StrobeRequestPayload scopes a flash to one owned cursor, or all local cursors
// when zero, with its color and duration.
type StrobeRequestPayload struct {
	Color      color.RGB   `toml:"color"`
	Intensity  float64     `toml:"intensity"`   // Base intensity 0.0-1.0
	DurationMs int64       `toml:"duration_ms"` // 0 = default value from parameters
	Cursor     core.Entity `toml:"cursor"`      // 0 = session-wide
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
	// Species is the species the spirit's arrival lands, on the footprint whose
	// top-left is LandX, LandY, so a drain pair's swarm rides its spirit;
	// SpeciesNone lands nothing
	Species component.SpeciesType `toml:"species"`
	LandX   int                   `toml:"land_x"`
	LandY   int                   `toml:"land_y"`
}

// SpiritArrivedPayload is the species a spirit's arrival lands and the top-left of
// the footprint it lands on
type SpiritArrivedPayload struct {
	X       int                   `toml:"x"`
	Y       int                   `toml:"y"`
	Species component.SpeciesType `toml:"species"`
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

// CrossingID gives derived outcomes a stable seed across receiver timing: producer
// identity and wire sequence. Zero means no originating crossing.
type CrossingID struct {
	CrossingSource uint32 `toml:"crossing_source"`
	CrossingSeq    uint64 `toml:"crossing_seq"`
}

// StampCrossing is written once, by the producer, before the frame is encoded.
func (c *CrossingID) StampCrossing(source uint32, seq uint64) {
	c.CrossingSource, c.CrossingSeq = source, seq
}

// Seed is this artifact's own random source, salted so one artifact's several
// draws do not repeat. False for an event that never crossed: every instance
// produces those at the same tick in the same order, so the stream is the right
// source there.
func (c CrossingID) Seed(salt uint64) (uint64, bool) {
	if c.CrossingSeq == 0 {
		return 0, false
	}
	return vmath.MixChain(uint64(c.CrossingSource), c.CrossingSeq, salt), true
}

// CombatAttackDirectRequestPayload contains direct attack information.
// HasOrigin/HasVelocity carry the emitter's geometry explicitly, so a player-domain
// emitter (orb, tracer, bullet) describes itself without naming its entity.
type CombatAttackDirectRequestPayload struct {
	CrossingID
	OwnerEntity    core.Entity                `toml:"owner_entity"`
	OriginEntity   core.Entity                `toml:"origin_entity"`
	TargetEntity   core.Entity                `toml:"target_entity"`
	HitEntity      core.Entity                `toml:"hit_entity"`
	OriginVelX     float64                    `toml:"origin_vel_x"`
	OriginVelY     float64                    `toml:"origin_vel_y"`
	OriginX        int                        `toml:"origin_x"`
	OriginY        int                        `toml:"origin_y"`
	AttackType     component.CombatAttackType `toml:"attack_type"`
	HasOrigin      bool                       `toml:"has_origin"`
	HasVelocity    bool                       `toml:"has_velocity"`
	ChainDepth     uint8                      `toml:"chain_depth"`
	RootAttackType component.CombatAttackType `toml:"root_attack_type"` // Chains share their originating weapon's immunity.
}

// IsDerived reports a chain follow-up, which the receiver produces from the root
// hit the wire carried; sending both would apply the chain twice (D-5).
func (p *CombatAttackDirectRequestPayload) IsDerived() bool { return p.ChainDepth > 0 }

// CombatAttackAreaRequestPayload contains area attack information.
// An empty HitEntities is the implicit single-hit form: the hit set is exactly
// {TargetEntity}. Single-cell targets use it to emit no per-event slice.
type CombatAttackAreaRequestPayload struct {
	CrossingID
	HitEntities  []core.Entity              `toml:"hit_entities"`
	AttackType   component.CombatAttackType `toml:"attack_type"`
	OwnerEntity  core.Entity                `toml:"owner_entity"`
	OriginEntity core.Entity                `toml:"origin_entity"`
	TargetEntity core.Entity                `toml:"target_entity"`
	// HasOrigin marks origin position for knockback direction (e.g., explosion center)
	// Without it OriginEntity position is used
	HasOrigin      bool                       `toml:"has_origin"`
	OriginX        int                        `toml:"origin_x"`
	OriginY        int                        `toml:"origin_y"`
	ChainDepth     uint8                      `toml:"chain_depth"`
	RootAttackType component.CombatAttackType `toml:"root_attack_type"`
	Scale          uint8                      `toml:"scale"`             // Damage multiplier; zero is one
	Charged        bool                       `toml:"charged,omitempty"` // The weapon was at its cap; the profile's Charged chain fires
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
	SubType      component.SubType     `toml:"sub_type"`
}

// SpeciesCreatedPayload announces one fully initialized species instance.
// Genes and EvalID are populated only for GA-managed instances.
type SpeciesCreatedPayload struct {
	Entity      core.Entity           `toml:"entity"`
	Species     component.SpeciesType `toml:"species"`
	SubType     component.SubType     `toml:"sub_type"`
	X           int                   `toml:"x"`            // -1 when the species has no single position
	Y           int                   `toml:"y"`            // -1 when the species has no single position
	MemberCount int                   `toml:"member_count"` // Composite members; zero for non-composites
	Genes       []float64             `toml:"genes"`
	EvalID      uint64                `toml:"eval_id"`
}

// --- Loot ---

// LootSpawnRequestPayload names an event drop's kind and cell; no drop table is rolled
type LootSpawnRequestPayload struct {
	Type component.LootType `toml:"type"`
	X    int                `toml:"x"`
	Y    int                `toml:"y"`
}

// --- Missile ---

// MissileSpawnRequestPayload contains missile spawn parameters
type MissileSpawnRequestPayload struct {
	Targets     []core.Entity          `toml:"targets"`      // Prioritized target entities
	HitEntities []core.Entity          `toml:"hit_entities"` // Corresponding hit points (member or same as target)
	OwnerEntity core.Entity            `toml:"owner_entity"` // Cursor, or a mount's Shared host
	OriginX     int                    `toml:"origin_x"`
	OriginY     int                    `toml:"origin_y"`
	Count       int                    `toml:"count"`
	Hostile     bool                   `toml:"hostile"` // A mount's: targets cursors
	Damage      component.CursorDamage `toml:"damage"`  // A hostile blast's cost to each cursor in it
}

// --- Bullet ---

// BulletSpawnRequestPayload requests creation of a linear projectile
type BulletSpawnRequestPayload struct {
	OriginX     float64                    `toml:"origin_x"`
	OriginY     float64                    `toml:"origin_y"`
	VelX        float64                    `toml:"vel_x"`
	VelY        float64                    `toml:"vel_y"`
	Owner       core.Entity                `toml:"owner"`
	MaxLifetime time.Duration              `toml:"max_lifetime"`
	Hostile     bool                       `toml:"hostile"` // A mount's: strikes cursors for Damage
	Damage      component.CursorDamage     `toml:"damage"`
	Attack      component.CombatAttackType `toml:"attack"` // A cursor's: resolves against species
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
	FadeMode  component.MarkerFade  `toml:"fade_mode"`
}

// --- Button ---

// ButtonSpawnRequestPayload names a button's key, one printable character, its colour
// and the sound bank name it plays
type ButtonSpawnRequestPayload struct {
	Key   string    `toml:"key"`
	Color color.RGB `toml:"color"`
	Sound string    `toml:"sound"`
}

// ButtonLightRequestPayload names a button by its 1-based place in the row
type ButtonLightRequestPayload struct {
	Button     int   `toml:"button"`
	DurationMs int64 `toml:"duration_ms"`
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

var collisionModeNames = [...]string{
	WallBatchSkipBlocked:   "skip_blocked",
	WallBatchOverwrite:     "overwrite",
	WallBatchFailIfBlocked: "fail_if_blocked",
}

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
	Path          string                  `toml:"path"`       // Name below a config root's image/
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

// CaveStartPayload lays a cave across the map and scrolls it left at Speed columns a
// second, at most one a tick; its rock follows cave.Config at the map's height. A
// zero speed, gap or wavelength takes its parameter default, a zero seed draws one,
// a zero mask blocks everything, and a zero visual draws stone. Art names the stone
// art preset (pattern.ParseArtPreset) the rock draws in place of the visual; a
// scenario that varies it from cave to cave picks it (EmitEvent's pick).
type CaveStartPayload struct {
	Seed       uint64                     `toml:"seed"`
	Speed      float64                    `toml:"speed"`
	MinGap     int                        `toml:"min_gap"`
	MaxGap     int                        `toml:"max_gap"`
	MinWall    int                        `toml:"min_wall"`
	Wavelength int                        `toml:"wavelength"`
	Swing      int                        `toml:"swing"`
	Detail     int                        `toml:"detail"`
	Ramp       int                        `toml:"ramp"`
	Octaves    int                        `toml:"octaves"`
	BlockMask  component.WallBlockMask    `toml:"block_mask"`
	Visual     component.WallVisualConfig `toml:"visual"`
	Art        string                     `toml:"art"`
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
	AnchorEntity        core.Entity           `toml:"anchor_entity"`          // Parent entity (pylon header)
	BaseIntervalMs      int                   `toml:"base_interval_ms"`       // Spawn interval in milliseconds
	RateMultiplier      float64               `toml:"rate_multiplier"`        // <1.0 = accelerating, 1.0 = constant
	RateAccelIntervalMs int                   `toml:"rate_accel_interval_ms"` // How often multiplier applied (ms), 0 = disabled
	MinIntervalMs       int                   `toml:"min_interval_ms"`        // Floor interval in milliseconds
	OffsetX             int                   `toml:"offset_x"`
	OffsetY             int                   `toml:"offset_y"`
	Species             component.SpeciesType `toml:"species"`
	SubType             component.SubType     `toml:"sub_type"`
	GroupID             uint8                 `toml:"group_id"`        // Target group for spawned entities
	UseRouteGraph       bool                  `toml:"use_route_graph"` // If true, request route graph computation for this gateway
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

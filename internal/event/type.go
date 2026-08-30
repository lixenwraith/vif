package event

import "github.com/lixenwraith/vi-fighter/internal/core"

// GameEvent represents a single game event with metadata
type GameEvent struct {
	Payload any
	Type    EventType
	Seq     uint64      // Monotonic queue slot, stamped at push; orders events within a tick
	Origin  Origin      // Producer, for journaling and replay; never affects dispatch
	Domain  core.Domain // Producer domain, for journaling and replication; never affects dispatch
}

// EventType represents the type of game event
type EventType int

// EventType constants are the sole input to the event registry. The generator
// (cmd/gen-manifest) parses this block and emits event/registry_gen.go; that
// file is never hand-edited, and every constant declared here is registered.
//
// Doc comment format, one of:
//
//	// EventFoo (FooPayload) short description of what the event signals
//	// EventFoo short description of what the event signals
//
// The first form registers the event with a typed payload, making its fields
// addressable from FSM configs and from ":emit". The second registers nil.
// A constant with no doc comment at all is registered nil and warned about.
//
// Stem convention: an event named Event<Stem> pairs with a payload named
// <Stem>Payload. Keeping the stem identical is what lets the generator detect
// a forgotten annotation — it errors when <Stem>Payload exists but the doc
// comment declares no payload. A deliberately divergent name (EventGoldCompleted
// carrying GoldCompletionPayload) defeats that check and is registered nil in
// silence, so prefer the convention and treat divergence as a last resort.
//
// Payload types that are not TOML-authorable are annotated for documentation
// but register nil. The generator recognises them by the presence of '[' or
// '.' in the annotation:
//
//	// EventFlashSpawnBatchRequest (BatchPayload[FlashSpawnEntry]) ...   pooled
//	// EventDeathBatch (DeathRequestPayload) ...                         pooled
//
// Payload structs must carry `toml:"..."` tags on every field intended to be
// set from a config or from ":emit"; untagged fields resolve only by Go name
// and cannot be decoded.
//
// Ordering and numbering: values are contiguous in [0, EventTypeCount) and are
// never serialized, so the block may be freely reordered. EventNone is reserved
// at zero — it is the FSM tick sentinel (fsm.Transition.Event == 0) and the
// "no effect" marker in DeathRequestPayload.EffectEvent and
// CompositeDestroyRequestPayload.Effect. No real event may occupy it.

const (
	// EventNone is the zero value, reserved so that no real event
	// aliases the FSM tick sentinel (fsm.Transition.Event == 0) or the
	EventNone EventType = iota

	// --- Level ---

	// EventLevelSetup (LevelSetupPayload) [shared] signals map dimension change and optional entity clear
	EventLevelSetup
	// EventScreenResize (ScreenResizePayload) [local] signals terminal dimension change
	EventScreenResize

	// --- Audio ---

	// EventSoundRequest (SoundRequestPayload) [local] requests audio playback
	EventSoundRequest
	// EventSoundMuteToggle (SoundMuteTogglePayload) [local] requests an audio mute-mask change; nil payload cycles
	EventSoundMuteToggle
	// EventAudioMuteChanged (AudioMuteChangedPayload) [local] announces the applied audio mute mask
	EventAudioMuteChanged

	// --- Music ---

	// EventMusicStart (MusicStartPayload) [local] begins music playback
	EventMusicStart
	// EventMusicStop [local] halts music playback
	EventMusicStop
	// EventBeatPatternRequest (BeatPatternRequestPayload) [local] requests beat pattern change
	EventBeatPatternRequest
	// EventMelodyNoteRequest (MelodyNoteRequestPayload) [local] triggers single note
	EventMelodyNoteRequest
	// EventMelodyPatternRequest (MelodyPatternRequestPayload) [local] requests melody pattern change
	EventMelodyPatternRequest
	// EventMusicIntensityChange (MusicIntensityPayload) [local] adjusts music intensity
	EventMusicIntensityChange
	// EventMusicTempoChange (MusicTempoPayload) [local] adjusts BPM
	EventMusicTempoChange
	// EventMusicSeedRequest (MusicSeedPayload) [local] re-keys the sequencer rng
	EventMusicSeedRequest
	// EventMusicSwingRequest (MusicSwingPayload) [local] sets sequencer shuffle
	EventMusicSwingRequest

	// --- Network ---

	// EventNetworkConnect (NetworkConnectPayload) [local] signals a new peer connection
	EventNetworkConnect
	// EventNetworkDisconnect (NetworkDisconnectPayload) [local] signals peer disconnection
	EventNetworkDisconnect
	// EventCursorStateSync (CursorStatePayload) [local] carries one cursor's owner-authored state to the instances that do not simulate it
	EventCursorStateSync

	// --- Meta ---

	// EventGameResetRequest (GameResetPayload) [shared] signals a request to reset the game state
	EventGameResetRequest
	// EventMetaDebugRequest [local] signals a request to show debug overlay
	EventMetaDebugRequest
	// EventMetaHelpRequest [local] signals a request to show help overlay
	EventMetaHelpRequest
	// EventMetaAboutRequest [local] signals a request to show about overlay
	EventMetaAboutRequest
	// EventMetaStatusMessageRequest (MetaStatusMessagePayload) [local] signals a request to display a message in status bar
	EventMetaStatusMessageRequest
	// EventMetaSystemCommandRequest (MetaSystemCommandPayload) [shared] signals a request to execute a system command
	EventMetaSystemCommandRequest
	// EventGamePauseRequest (GamePausePayload) [local] asks MetaSystem to change pause state
	EventGamePauseRequest
	// EventGamePauseChanged (GamePausePayload) [local] announces applied pause state; systems react in their own domain
	EventGamePauseChanged
	// EventGameSpeedRequest (GameSpeedPayload) [local] asks MetaSystem to change the time scale
	EventGameSpeedRequest
	// EventGameSpeedChanged (GameSpeedPayload) [local] announces the applied time scale
	EventGameSpeedChanged
	// EventGameStepRequest (GameStepPayload) [local] asks MetaSystem to step ticks or arm a run-until breakpoint
	EventGameStepRequest

	// --- FSM ---

	// EventCycleDamageMultiplierIncrease [shared] signals cycle completion, doubles damage multiplier
	EventCycleDamageMultiplierIncrease
	// EventCycleDamageMultiplierReset [shared] signals cycle reset, resets damage multiplier to 1
	EventCycleDamageMultiplierReset
	// EventFSMRegionRequest (FSMRegionPayload) [shared] signals FSM to change the active region
	EventFSMRegionRequest

	// --- Nugget ---

	// EventNuggetCollected (NuggetCollectedPayload) [local] signals the personal nugget was collected
	EventNuggetCollected
	// EventNuggetDestroyed (NuggetDestroyedPayload) [local] signals the personal nugget was destroyed externally
	EventNuggetDestroyed
	// EventNuggetJumpRequest (NuggetJumpRequestPayload) [local] signals player intent to jump to their active nugget
	EventNuggetJumpRequest

	// --- Cleaner ---

	// EventCleanerDirectionalRequest (DirectionalCleanerPayload) [local] spawns 4-way cleaners from origin
	EventCleanerDirectionalRequest
	// EventCleanerSweepingRequest (CleanerSweepingRequestPayload) [local] spawns cleaners on rows with positive/negative energy glyphs
	EventCleanerSweepingRequest

	// --- Gold ---

	// EventGoldSpawnRequest [shared] signals a specific request to try spawning a gold sequence
	EventGoldSpawnRequest
	// EventGoldSpawnFailed [shared] signals that a requested spawn could not be completed (e.g. no space)
	EventGoldSpawnFailed
	// EventGoldSpawned (GoldSpawnedPayload) [shared] signals gold sequence creation
	EventGoldSpawned
	// EventGoldCompleted (GoldCompletionPayload) [shared] signals successful gold sequence completion
	EventGoldCompleted
	// EventGoldTimeout (GoldCompletionPayload) [shared] signals gold sequence expiration
	EventGoldTimeout
	// EventGoldDestroyed (GoldCompletionPayload) [shared] signals external gold destruction
	EventGoldDestroyed
	// EventGoldCancel [shared] signals mandatory cleanup of any active gold sequence
	EventGoldCancel
	// EventGoldJumpRequest (GoldJumpRequestPayload) [bus] signals player intent to jump to active gold sequence
	EventGoldJumpRequest

	// --- Splash ---

	// EventSplashTimerRequest (SplashTimerRequestPayload) [local] signals timer visual feedback
	EventSplashTimerRequest
	// EventSplashTimerCancel (SplashTimerCancelPayload) [local] signals ending timer visual feedback
	EventSplashTimerCancel

	// --- Energy ---

	// EventEnergyAddRequest (EnergyAddPayload) [local] signals energy delta on target entity
	EventEnergyAddRequest
	// EventEnergySetRequest (EnergySetPayload) [local] signals setting energy to specific value
	EventEnergySetRequest
	// EventEnergyCrossedZero (EnergyCrossedZeroPayload) [local] signals energy crossing zero
	EventEnergyCrossedZero
	// EventEnergyGlyphConsumed (EnergyGlyphConsumedPayload) [local] signals glyph destruction for energy calculation
	EventEnergyGlyphConsumed
	// EventEnergyBlinkStart (EnergyBlinkPayload) [local] signals visual blink trigger
	EventEnergyBlinkStart
	// EventEnergyBlinkStop (EnergyBlinkStopPayload) [local] signals blink clear
	EventEnergyBlinkStop

	// --- Shield ---

	// EventShieldActivate (ShieldActivatePayload) [local] signals shield should become active
	EventShieldActivate
	// EventShieldDeactivate (ShieldDeactivatePayload) [local] signals shield should become inactive
	EventShieldDeactivate
	// EventShieldDrainRequest (ShieldDrainRequestPayload) [local] signals energy drain from external source
	EventShieldDrainRequest

	// --- Weapon ---

	// EventWeaponAddRequest (WeaponAddRequestPayload) [local] signals activating buff for cursor
	EventWeaponAddRequest
	// EventWeaponFireRequest (WeaponFireRequestPayload) [local] signals weapon fire request
	EventWeaponFireRequest
	// EventFireSpecialRequest (FireSpecialRequestPayload) [local] signals player intent to fire special ability
	EventFireSpecialRequest

	// --- Heat ---

	// EventHeatAddRequest (HeatAddRequestPayload) [local] signals heat delta modification
	EventHeatAddRequest
	// EventHeatSetRequest (HeatSetRequestPayload) [local] signals absolute heat value
	EventHeatSetRequest
	// EventHeatBurst (HeatBurstPayload) [local] signals heat burst notification
	EventHeatBurst

	// --- Boost ---

	// EventBoostActivate (BoostActivatePayload) [local] signals boost activation request
	EventBoostActivate
	// EventBoostDeactivate (BoostDeactivatePayload) [local] signals boost deactivation
	EventBoostDeactivate
	// EventBoostExtend (BoostExtendPayload) [local] signals boost duration extension
	EventBoostExtend
	// EventBoostReward (BoostRewardPayload) [local] signals an earned boost; BoostSystem chooses activation or extension
	EventBoostReward

	// --- Typing ---

	// EventCharacterTyped (CharacterTypedPayload) [local] signals Insert mode keypress
	EventCharacterTyped
	// EventDeleteRequest (DeleteRequestPayload) [local] signals a deletion operation (x, d, etc.)
	EventDeleteRequest

	// --- Ping ---

	// EventPingGridRequest (PingGridRequestPayload) [local] signals a request to show the ping grid
	EventPingGridRequest

	// --- Materialize ---

	// EventMaterializeRequest (MaterializeRequestPayload) [stamped] signals a request to start a materialization visual effect
	EventMaterializeRequest
	// EventMaterializeComplete (MaterializeCompletedPayload) [stamped] signals materialization finished at location
	EventMaterializeComplete
	// EventMaterializeAreaRequest (MaterializeAreaRequestPayload) [stamped] requests area-based materialization (swarm, quasar)
	EventMaterializeAreaRequest

	// --- Flash ---

	// EventFlashSpawnOneRequest (FlashRequestPayload) [local] signals a request to spawn a destruction flash effect
	EventFlashSpawnOneRequest
	// EventFlashSpawnBatchRequest (BatchPayload[FlashSpawnEntry]) [local] signals batch spawn of destruction flash effects
	EventFlashSpawnBatchRequest

	// --- Explosion ---

	// EventExplosionRequest (ExplosionRequestPayload) [bus] triggers explosion effect at location
	EventExplosionRequest
	// EventExplosionBatchRequest (ExplosionBatchRequestPayload) [bus] triggers one explosion made of several centers
	EventExplosionBatchRequest

	// --- Dust ---

	// EventDustSpawnOneRequest (DustSpawnOneRequestPayload) [local] signals intent to spawn a single dust entity
	EventDustSpawnOneRequest
	// EventDustSpawnBatchRequest (BatchPayload[DustSpawnEntry]) [local] signals intent to spawn multiple dust entities
	EventDustSpawnBatchRequest
	// EventDustAllRequest [local] signals intent to convert all glyphs on the map to dust
	EventDustAllRequest

	// --- Blossom ---

	// EventBlossomSpawnOne (BlossomSpawnPayload) [local] signals intent to spawn a single blossom entity
	EventBlossomSpawnOne
	// EventBlossomSpawnBatch (BatchPayload[BlossomSpawnEntry]) [local] signals batch spawn of blossom entities
	EventBlossomSpawnBatch
	// EventBlossomWave [local] signals start of a full width rising blossom wave
	EventBlossomWave

	// --- Decay ---

	// EventDecaySpawnOne (DecaySpawnPayload) [local] signals intent to spawn a single decay entity
	EventDecaySpawnOne
	// EventDecaySpawnBatch (BatchPayload[DecaySpawnEntry]) [local] signals batch spawn of decay entities
	EventDecaySpawnBatch
	// EventDecayWave [local] signals start of a full width falling decay wave
	EventDecayWave

	// --- Death ---

	// EventDeathBatch (DeathRequestPayload) [stamped] signals intent to destroy one or more entities with an optional effect
	EventDeathBatch

	// --- Timer ---

	// EventTimerStart (TimerStartPayload) [stamped] signals creation of a lifecycle timer for an entity
	EventTimerStart

	// --- Composite ---

	// EventCompositeMemberDestroyed (CompositeMemberDestroyedPayload) [bus] signals a composite member was successfully typed
	EventCompositeMemberDestroyed
	// EventCompositeIntegrityBreach (CompositeIntegrityBreachPayload) [shared] signals unexpected member loss (OOB, species hit, etc.)
	EventCompositeIntegrityBreach
	// EventCompositeDestroyRequest (CompositeDestroyRequestPayload) [shared] signals owner system requests full composite destruction
	EventCompositeDestroyRequest

	// --- Cursor ---

	// EventCursorSpawnRequest (CursorSpawnRequestPayload) [shared] asks CursorSystem to create a cursor
	EventCursorSpawnRequest
	// EventCursorSpawned (CursorSpawnedPayload) [shared] announces a created cursor
	EventCursorSpawned
	// EventCursorSpawnFailed [shared] signals no roster slot or no free cell was available
	EventCursorSpawnFailed
	// EventCursorDespawnRequest (CursorDespawnRequestPayload) [shared] asks CursorSystem to destroy cursors
	EventCursorDespawnRequest
	// EventCursorDespawned (CursorDespawnedPayload) [shared] announces a destroyed cursor
	EventCursorDespawned
	// EventCursorMoveRequest (CursorMoveRequestPayload) [bus] asks CursorSystem to place a cursor
	EventCursorMoveRequest
	// EventCursorMoved (CursorMovedPayload) [shared] announces an applied cursor position
	EventCursorMoved
	// EventCursorDefeatState (CursorDefeatStatePayload) [bus] carries one owner's terminal lifecycle state
	EventCursorDefeatState
	// EventCursorSetLocalRequest (CursorSetLocalPayload) [local] rebinds which cursor input and camera follow
	EventCursorSetLocalRequest
	// EventCursorLocalChanged (CursorSetLocalPayload) [local] announces the bound slot
	EventCursorLocalChanged

	// --- Species ---

	// EventSpeciesCreated (SpeciesCreatedPayload) [stamped] announces a created species instance
	EventSpeciesCreated
	// EventSpeciesKilled (SpeciesKilledPayload) [stamped] announces a terminated species instance
	EventSpeciesKilled
	// EventDrainDefeated [bus] advances shared progression for one personal drain death
	EventDrainDefeated

	// --- Fuse ---

	// EventFuseQuasarRequest [local] signals drains should fuse into quasar
	EventFuseQuasarRequest
	// EventFuseSwarmRequest (FuseSwarmRequestPayload) [local] signals two enraged drains should fuse into swarm
	EventFuseSwarmRequest

	// --- Drain ---

	// EventDrainPause [local] signals DrainSystem to stop spawning
	EventDrainPause
	// EventDrainResume [local] signals DrainSystem to resume spawning
	EventDrainResume

	// --- Quasar ---

	// EventQuasarSpawnRequest (QuasarSpawnRequestPayload) [bus] signals QuasarSystem to create the entity at location
	EventQuasarSpawnRequest
	// EventQuasarCancelRequest [shared] signals manual termination of the quasar phase
	EventQuasarCancelRequest

	// --- Swarm ---

	// EventSwarmSpawnRequest (SwarmSpawnRequestPayload) [bus] signals SwarmSystem to create the entity at location
	EventSwarmSpawnRequest
	// EventSwarmCancelRequest [shared] signals destruction of all swarm composites
	EventSwarmCancelRequest

	// --- Storm ---

	// EventStormSpawnRequest [shared] triggers storm spawn
	EventStormSpawnRequest
	// EventStormCancelRequest [shared] signals destruction of all storm entities
	EventStormCancelRequest

	// --- Post-Process ---

	// EventGrayoutStart [local] signals persistent grayout activation
	EventGrayoutStart
	// EventGrayoutEnd [local] signals persistent grayout deactivation
	EventGrayoutEnd
	// EventStrobeRequest (StrobeRequestPayload) [local] triggers screen flash effect
	EventStrobeRequest

	// --- Spirit ---

	// EventSpiritSpawnRequest (SpiritSpawnRequestPayload) [stamped] signals intent to spawn a spirit entity
	EventSpiritSpawnRequest
	// EventSpiritDespawnRequest [stamped] signals force-clear of all spirit entities
	EventSpiritDespawnRequest

	// --- Lightning ---

	// EventLightningSpawnRequest (LightningSpawnRequestPayload) [local] signals intent to spawn a lightning visual effect
	EventLightningSpawnRequest
	// EventLightningUpdateRequest (LightningUpdateRequestPayload) [local] signals target position update for tracked lightning
	EventLightningUpdateRequest
	// EventLightningDespawnRequest (LightningDespawnRequestPayload) [local] signals force-removal of lightning entity(ies)
	EventLightningDespawnRequest

	// --- Combat ---

	// EventCombatAttackDirectRequest (CombatAttackDirectRequestPayload) [stamped] signals applying knockback
	EventCombatAttackDirectRequest
	// EventCombatAttackAreaRequest (CombatAttackAreaRequestPayload) [local] signals applying knockback
	EventCombatAttackAreaRequest
	// EventCombatAttackAreaCrossingRequest (CombatAttackAreaRequestPayload) [bus] carries an owner-resolved area hit on shared targets
	EventCombatAttackAreaCrossingRequest
	// EventCombatHealRequest (CombatHealRequestPayload) [bus] requests adding hit points to a live combat entity
	EventCombatHealRequest

	// --- Loot ---

	// EventLootSpawnRequest (LootSpawnRequestPayload) [local] requests direct loot spawn at position
	EventLootSpawnRequest

	// --- Missile ---

	// EventMissileSpawnRequest (MissileSpawnRequestPayload) [local] signals launcher buff firing a cluster missile
	EventMissileSpawnRequest

	// --- Bullet ---

	// EventBulletSpawnRequest (BulletSpawnRequestPayload) [local] signals creation of a linear projectile
	EventBulletSpawnRequest

	// --- Marker ---

	// EventMarkerSpawnRequest (MarkerSpawnRequestPayload) [shared] signals a request to spawn a visual marker
	EventMarkerSpawnRequest

	// --- Motion Marker ---

	// EventMotionMarkerShowColored (MotionMarkerShowPayload) [local] signals a request to show colored glyph motion markers in ping bound
	EventMotionMarkerShowColored
	// EventMotionMarkerClearColored [local] signals clearing colored motion markers (jump executed or cancelled)
	EventMotionMarkerClearColored

	// --- Mode ---

	// EventModeChanged (ModeChangedPayload) [local] signals change of the mode
	EventModeChanged

	// --- Wall ---

	// EventWallSpawnRequest (WallSpawnRequestPayload) [shared] requests creation of a single wall cell
	EventWallSpawnRequest
	// EventWallBatchSpawnRequest (WallBatchSpawnRequestPayload) [shared] creates multiple wall cells in a single batch operation (supports collision modes)
	EventWallBatchSpawnRequest
	// EventWallCompositeSpawnRequest (WallCompositeSpawnRequestPayload) [shared] requests creation of a multi-cell wall structure
	EventWallCompositeSpawnRequest
	// EventWallPatternSpawnRequest (WallPatternSpawnRequestPayload) [shared] requests creation of wall structure from .vifimg pattern file
	EventWallPatternSpawnRequest
	// EventMazeSpawnRequest (MazeSpawnRequestPayload) [shared] signals maze generation and wall spawning
	EventMazeSpawnRequest
	// EventWallDespawnRequest (WallDespawnRequestPayload) [shared] requests removal of walls in specified area or globally
	EventWallDespawnRequest
	// EventWallMaskChangeRequest (WallMaskChangeRequestPayload) [shared] modifies blocking behavior of existing walls
	EventWallMaskChangeRequest
	// EventWallPushCheckRequest [shared] triggers full entity displacement check for blocking walls
	EventWallPushCheckRequest
	// EventWallSpawned (WallSpawnedPayload) [shared] notifies completion of wall creation with bounds and entity count
	EventWallSpawned
	// EventWallDespawned (WallDespawnedPayload) [shared] notifies completion of wall destruction with bounds
	EventWallDespawned
	// EventWallDespawnAll [shared] signals silent destruction of all wall entities
	EventWallDespawnAll

	// --- Fadeout ---

	// EventFadeoutSpawnOne (FadeoutSpawnPayload) [local] signals intent to spawn a single fadeout effect
	EventFadeoutSpawnOne
	// EventFadeoutSpawnBatch (BatchPayload[FadeoutSpawnEntry]) [local] signals intent to spawn multiple fadeout effects
	EventFadeoutSpawnBatch

	// --- Pylon ---

	// EventPylonSpawnRequest (PylonSpawnRequestPayload) [shared] signals pylon creation at location
	EventPylonSpawnRequest
	// EventPylonSpawnFailed [shared] signals pylon spawn could not find valid position
	EventPylonSpawnFailed
	// EventPylonCancelRequest [shared] signals forced destruction of all pylons
	EventPylonCancelRequest

	// --- Snake ---

	// EventSnakeSpawnRequest (SnakeSpawnRequestPayload) [shared] signals SnakeSystem to create the entity at location
	EventSnakeSpawnRequest
	// EventSnakeCancelRequest [shared] signals manual termination of all snakes
	EventSnakeCancelRequest

	// --- Navigation ---

	// EventTargetGroupUpdate (TargetGroupUpdatePayload) [shared] configures or updates a navigation target group
	EventTargetGroupUpdate
	// EventTargetGroupRemove (TargetGroupRemovePayload) [shared] removes a target group, entities fall back to group 0
	EventTargetGroupRemove
	// EventNavigationRegraph [shared] signals a request to recalculate navigation graphs
	EventNavigationRegraph
	// EventRouteGraphRequest (RouteGraphRequestPayload) [shared] requests route graph computation for a gateway-target pair
	EventRouteGraphRequest
	// EventRouteGraphComputed (RouteGraphComputedPayload) [shared] signals route graph computation completion
	EventRouteGraphComputed

	// --- Eye ---

	// EventEyeSpawnRequest (EyeSpawnRequestPayload) [shared] signals EyeSystem to create entity at location
	EventEyeSpawnRequest
	// EventEyeCancelRequest [shared] signals destruction of all eye composites
	EventEyeCancelRequest

	// --- Tower ---

	// EventTowerSpawnRequest (TowerSpawnRequestPayload) [shared] signals tower creation at location
	EventTowerSpawnRequest
	// EventTowerSpawnFailed [shared] signals tower spawn could not find valid position
	EventTowerSpawnFailed
	// EventTowerCancelRequest [shared] signals forced destruction of all towers
	EventTowerCancelRequest

	// --- Gateway ---

	// EventGatewaySpawnRequest (GatewaySpawnRequestPayload) [shared] signals GatewaySystem to create a gateway entity anchored to a parent
	EventGatewaySpawnRequest
	// EventGatewayDespawnRequest (GatewayDespawnRequestPayload) [shared] signals GatewaySystem to remove gateway for a specific anchor
	EventGatewayDespawnRequest
	// EventGatewayDespawned (GatewayDespawnedPayload) [shared] signals that a gateway entity has been cleaned up
	EventGatewayDespawned

	// --- Genetic ---

	// EventGeneticRegisterSpecies (GeneticRegisterSpeciesPayload) [shared] dynamically registers a species for evolution
	EventGeneticRegisterSpecies
	// EventGeneticAbandonEval (GeneticAbandonEvalPayload) [shared] abandons evaluation of the species
	EventGeneticAbandonEval

	// --- Debug ---

	// EventDebugFlowToggle (DebugFlowGroupPayload) [local] toggles debug flow field visualization
	EventDebugFlowToggle
	// EventDebugGraphToggle (DebugFlowGroupPayload) [local] toggles debug graph visualization
	EventDebugGraphToggle
)

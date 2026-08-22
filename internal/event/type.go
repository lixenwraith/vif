package event

// GameEvent represents a single game event with metadata
type GameEvent struct {
	Payload any
	Type    EventType
	Seq     uint64 // Monotonic queue slot, stamped at push; orders events within a tick
	Origin  Origin // Producer, for journaling and replay; never affects dispatch
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

	// EventLevelSetup (LevelSetupPayload) signals map dimension change and optional entity clear
	EventLevelSetup
	// EventScreenResize (ScreenResizePayload) signals terminal dimension change
	EventScreenResize

	// --- Audio ---

	// EventSoundRequest (SoundRequestPayload) requests audio playback
	EventSoundRequest
	// EventSoundMuteToggle (SoundMuteTogglePayload) requests an audio mute-mask change; nil payload cycles
	EventSoundMuteToggle
	// EventAudioMuteChanged (AudioMuteChangedPayload) announces the applied audio mute mask
	EventAudioMuteChanged

	// --- Music ---

	// EventMusicStart (MusicStartPayload) begins music playback
	EventMusicStart
	// EventMusicStop halts music playback
	EventMusicStop
	// EventBeatPatternRequest (BeatPatternRequestPayload) requests beat pattern change
	EventBeatPatternRequest
	// EventMelodyNoteRequest (MelodyNoteRequestPayload) triggers single note
	EventMelodyNoteRequest
	// EventMelodyPatternRequest (MelodyPatternRequestPayload) requests melody pattern change
	EventMelodyPatternRequest
	// EventMusicIntensityChange (MusicIntensityPayload) adjusts music intensity
	EventMusicIntensityChange
	// EventMusicTempoChange (MusicTempoPayload) adjusts BPM
	EventMusicTempoChange
	// EventMusicSeedRequest (MusicSeedPayload) re-keys the sequencer rng
	EventMusicSeedRequest
	// EventMusicSwingRequest (MusicSwingPayload) sets sequencer shuffle
	EventMusicSwingRequest

	// --- Network ---

	// EventNetworkConnect (NetworkConnectPayload) signals a new peer connection
	EventNetworkConnect
	// EventNetworkDisconnect (NetworkDisconnectPayload) signals peer disconnection
	EventNetworkDisconnect
	// EventRemoteInput (RemoteInputPayload) signals input from a remote player
	EventRemoteInput
	// EventStateSync (StateSyncPayload) signals state snapshot received
	EventStateSync
	// EventNetworkEvent (NetworkEventPayload) signals a game event from remote peer
	EventNetworkEvent
	// EventNetworkError (NetworkErrorPayload) signals a network error
	EventNetworkError

	// --- Meta ---

	// EventGameResetRequest (GameResetPayload) signals a request to reset the game state
	EventGameResetRequest
	// EventMetaDebugRequest signals a request to show debug overlay
	EventMetaDebugRequest
	// EventMetaHelpRequest signals a request to show help overlay
	EventMetaHelpRequest
	// EventMetaAboutRequest signals a request to show about overlay
	EventMetaAboutRequest
	// EventMetaStatusMessageRequest (MetaStatusMessagePayload) signals a request to display a message in status bar
	EventMetaStatusMessageRequest
	// EventMetaSystemCommandRequest (MetaSystemCommandPayload) signals a request to execute a system command
	EventMetaSystemCommandRequest
	// EventGamePauseRequest (GamePausePayload) asks MetaSystem to change pause state
	EventGamePauseRequest
	// EventGamePauseChanged (GamePausePayload) announces applied pause state; systems react in their own domain
	EventGamePauseChanged
	// EventGameSpeedRequest (GameSpeedPayload) asks MetaSystem to change the time scale
	EventGameSpeedRequest
	// EventGameSpeedChanged (GameSpeedPayload) announces the applied time scale
	EventGameSpeedChanged
	// EventGameStepRequest (GameStepPayload) asks MetaSystem to step ticks or arm a run-until breakpoint
	EventGameStepRequest

	// --- FSM ---

	// EventCycleDamageMultiplierIncrease signals cycle completion, doubles damage multiplier
	EventCycleDamageMultiplierIncrease
	// EventCycleDamageMultiplierReset signals cycle reset, resets damage multiplier to 1
	EventCycleDamageMultiplierReset
	// EventFSMRegionRequest (FSMRegionPayload) signals FSM to change the active region
	EventFSMRegionRequest

	// --- Nugget ---

	// EventNuggetCollected (NuggetCollectedPayload) signals nugget was collected by player
	EventNuggetCollected
	// EventNuggetDestroyed (NuggetDestroyedPayload) signals nugget was destroyed externally
	EventNuggetDestroyed
	// EventNuggetJumpRequest (NuggetJumpRequestPayload) signals player intent to jump to active nugget
	EventNuggetJumpRequest

	// --- Cleaner ---

	// EventCleanerDirectionalRequest (DirectionalCleanerPayload) spawns 4-way cleaners from origin
	EventCleanerDirectionalRequest
	// EventCleanerSweepingRequest (CleanerSweepingRequestPayload) spawns cleaners on rows with positive/negative energy glyphs
	EventCleanerSweepingRequest

	// --- Gold ---

	// EventGoldSpawnRequest signals a specific request to try spawning a gold sequence
	EventGoldSpawnRequest
	// EventGoldSpawnFailed signals that a requested spawn could not be completed (e.g. no space)
	EventGoldSpawnFailed
	// EventGoldSpawned (GoldSpawnedPayload) signals gold sequence creation
	EventGoldSpawned
	// EventGoldCompleted (GoldCompletionPayload) signals successful gold sequence completion
	EventGoldCompleted
	// EventGoldTimeout (GoldCompletionPayload) signals gold sequence expiration
	EventGoldTimeout
	// EventGoldDestroyed (GoldCompletionPayload) signals external gold destruction
	EventGoldDestroyed
	// EventGoldCancel signals mandatory cleanup of any active gold sequence
	EventGoldCancel
	// EventGoldJumpRequest (GoldJumpRequestPayload) signals player intent to jump to active gold sequence
	EventGoldJumpRequest

	// --- Splash ---

	// EventSplashTimerRequest (SplashTimerRequestPayload) signals timer visual feedback
	EventSplashTimerRequest
	// EventSplashTimerCancel (SplashTimerCancelPayload) signals ending timer visual feedback
	EventSplashTimerCancel

	// --- Energy ---

	// EventEnergyAddRequest (EnergyAddPayload) signals energy delta on target entity
	EventEnergyAddRequest
	// EventEnergySetRequest (EnergySetPayload) signals setting energy to specific value
	EventEnergySetRequest
	// EventEnergyCrossedZero (EnergyCrossedZeroPayload) signals energy crossing zero
	EventEnergyCrossedZero
	// EventEnergyGlyphConsumed (EnergyGlyphConsumedPayload) signals glyph destruction for energy calculation
	EventEnergyGlyphConsumed
	// EventEnergyBlinkStart (EnergyBlinkPayload) signals visual blink trigger
	EventEnergyBlinkStart
	// EventEnergyBlinkStop (EnergyBlinkStopPayload) signals blink clear
	EventEnergyBlinkStop

	// --- Shield ---

	// EventShieldActivate (ShieldActivatePayload) signals shield should become active
	EventShieldActivate
	// EventShieldDeactivate (ShieldDeactivatePayload) signals shield should become inactive
	EventShieldDeactivate
	// EventShieldDrainRequest (ShieldDrainRequestPayload) signals energy drain from external source
	EventShieldDrainRequest

	// --- Weapon ---

	// EventWeaponAddRequest (WeaponAddRequestPayload) signals activating buff for cursor
	EventWeaponAddRequest
	// EventWeaponFireRequest (WeaponFireRequestPayload) signals weapon fire request
	EventWeaponFireRequest
	// EventFireSpecialRequest (FireSpecialRequestPayload) signals player intent to fire special ability
	EventFireSpecialRequest

	// --- Heat ---

	// EventHeatAddRequest (HeatAddRequestPayload) signals heat delta modification
	EventHeatAddRequest
	// EventHeatSetRequest (HeatSetRequestPayload) signals absolute heat value
	EventHeatSetRequest
	// EventHeatBurst (HeatBurstPayload) signals heat burst notification
	EventHeatBurst

	// --- Boost ---

	// EventBoostActivate (BoostActivatePayload) signals boost activation request
	EventBoostActivate
	// EventBoostDeactivate (BoostDeactivatePayload) signals boost deactivation
	EventBoostDeactivate
	// EventBoostExtend (BoostExtendPayload) signals boost duration extension
	EventBoostExtend
	// EventBoostReward (BoostRewardPayload) signals an earned boost; BoostSystem chooses activation or extension
	EventBoostReward

	// --- Typing ---

	// EventCharacterTyped (CharacterTypedPayload) signals Insert mode keypress
	EventCharacterTyped
	// EventDeleteRequest (DeleteRequestPayload) signals a deletion operation (x, d, etc.)
	EventDeleteRequest

	// --- Ping ---

	// EventPingGridRequest (PingGridRequestPayload) signals a request to show the ping grid
	EventPingGridRequest

	// --- Materialize ---

	// EventMaterializeRequest (MaterializeRequestPayload) signals a request to start a materialization visual effect
	EventMaterializeRequest
	// EventMaterializeComplete (MaterializeCompletedPayload) signals materialization finished at location
	EventMaterializeComplete
	// EventMaterializeAreaRequest (MaterializeAreaRequestPayload) requests area-based materialization (swarm, quasar)
	EventMaterializeAreaRequest

	// --- Flash ---

	// EventFlashSpawnOneRequest (FlashRequestPayload) signals a request to spawn a destruction flash effect
	EventFlashSpawnOneRequest
	// EventFlashSpawnBatchRequest (BatchPayload[FlashSpawnEntry]) signals batch spawn of destruction flash effects
	EventFlashSpawnBatchRequest

	// --- Explosion ---

	// EventExplosionRequest (ExplosionRequestPayload) triggers explosion effect at location
	EventExplosionRequest

	// --- Dust ---

	// EventDustSpawnOneRequest (DustSpawnOneRequestPayload) signals intent to spawn a single dust entity
	EventDustSpawnOneRequest
	// EventDustSpawnBatchRequest (BatchPayload[DustSpawnEntry]) signals intent to spawn multiple dust entities
	EventDustSpawnBatchRequest
	// EventDustAllRequest signals intent to convert all glyphs on the map to dust
	EventDustAllRequest

	// --- Blossom ---

	// EventBlossomSpawnOne (BlossomSpawnPayload) signals intent to spawn a single blossom entity
	EventBlossomSpawnOne
	// EventBlossomSpawnBatch (BatchPayload[BlossomSpawnEntry]) signals batch spawn of blossom entities
	EventBlossomSpawnBatch
	// EventBlossomWave signals start of a full width rising blossom wave
	EventBlossomWave

	// --- Decay ---

	// EventDecaySpawnOne (DecaySpawnPayload) signals intent to spawn a single decay entity
	EventDecaySpawnOne
	// EventDecaySpawnBatch (BatchPayload[DecaySpawnEntry]) signals batch spawn of decay entities
	EventDecaySpawnBatch
	// EventDecayWave signals start of a full width falling decay wave
	EventDecayWave

	// --- Death ---

	// EventDeathBatch (DeathRequestPayload) signals intent to destroy one or more entities with an optional effect
	EventDeathBatch

	// --- Timer ---

	// EventTimerStart (TimerStartPayload) signals creation of a lifecycle timer for an entity
	EventTimerStart

	// --- Composite ---

	// EventCompositeMemberDestroyed (CompositeMemberDestroyedPayload) signals a composite member was successfully typed
	EventCompositeMemberDestroyed
	// EventCompositeIntegrityBreach (CompositeIntegrityBreachPayload) signals unexpected member loss (OOB, species hit, etc.)
	EventCompositeIntegrityBreach
	// EventCompositeDestroyRequest (CompositeDestroyRequestPayload) signals owner system requests full composite destruction
	EventCompositeDestroyRequest

	// --- Cursor ---

	// EventCursorSpawnRequest (CursorSpawnRequestPayload) asks CursorSystem to create a cursor
	EventCursorSpawnRequest
	// EventCursorSpawned (CursorSpawnedPayload) announces a created cursor
	EventCursorSpawned
	// EventCursorSpawnFailed signals no roster slot or no free cell was available
	EventCursorSpawnFailed
	// EventCursorDespawnRequest (CursorDespawnRequestPayload) asks CursorSystem to destroy cursors
	EventCursorDespawnRequest
	// EventCursorDespawned (CursorDespawnedPayload) announces a destroyed cursor
	EventCursorDespawned
	// EventCursorMoveRequest (CursorMoveRequestPayload) asks CursorSystem to place a cursor
	EventCursorMoveRequest
	// EventCursorMoved (CursorMovedPayload) announces an applied cursor position
	EventCursorMoved
	// EventCursorSetLocalRequest (CursorSetLocalPayload) rebinds which cursor input and camera follow
	EventCursorSetLocalRequest
	// EventCursorLocalChanged (CursorSetLocalPayload) announces the bound slot
	EventCursorLocalChanged

	// --- Species ---

	// EventSpeciesCreated (SpeciesCreatedPayload) announces a created species instance
	EventSpeciesCreated
	// EventSpeciesKilled (SpeciesKilledPayload) announces a terminated species instance
	EventSpeciesKilled

	// --- Fuse ---

	// EventFuseQuasarRequest signals drains should fuse into quasar
	EventFuseQuasarRequest
	// EventFuseSwarmRequest (FuseSwarmRequestPayload) signals two enraged drains should fuse into swarm
	EventFuseSwarmRequest

	// --- Drain ---

	// EventDrainPause signals DrainSystem to stop spawning
	EventDrainPause
	// EventDrainResume signals DrainSystem to resume spawning
	EventDrainResume

	// --- Quasar ---

	// EventQuasarSpawnRequest (QuasarSpawnRequestPayload) signals QuasarSystem to create the entity at location
	EventQuasarSpawnRequest
	// EventQuasarCancelRequest signals manual termination of the quasar phase
	EventQuasarCancelRequest

	// --- Swarm ---

	// EventSwarmSpawnRequest (SwarmSpawnRequestPayload) signals SwarmSystem to create the entity at location
	EventSwarmSpawnRequest
	// EventSwarmAbsorbedDrain (SwarmAbsorbedDrainPayload) signals drain absorbed by swarm
	EventSwarmAbsorbedDrain
	// EventSwarmCancelRequest signals destruction of all swarm composites
	EventSwarmCancelRequest

	// --- Storm ---

	// EventStormSpawnRequest triggers storm spawn
	EventStormSpawnRequest
	// EventStormCancelRequest signals destruction of all storm entities
	EventStormCancelRequest

	// --- Post-Process ---

	// EventGrayoutStart signals persistent grayout activation
	EventGrayoutStart
	// EventGrayoutEnd signals persistent grayout deactivation
	EventGrayoutEnd
	// EventStrobeRequest (StrobeRequestPayload) triggers screen flash effect
	EventStrobeRequest

	// --- Spirit ---

	// EventSpiritSpawnRequest (SpiritSpawnRequestPayload) signals intent to spawn a spirit entity
	EventSpiritSpawnRequest
	// EventSpiritDespawnRequest signals force-clear of all spirit entities
	EventSpiritDespawnRequest

	// --- Lightning ---

	// EventLightningSpawnRequest (LightningSpawnRequestPayload) signals intent to spawn a lightning visual effect
	EventLightningSpawnRequest
	// EventLightningUpdateRequest (LightningUpdateRequestPayload) signals target position update for tracked lightning
	EventLightningUpdateRequest
	// EventLightningDespawnRequest (LightningDespawnRequestPayload) signals force-removal of lightning entity(ies)
	EventLightningDespawnRequest

	// --- Combat ---

	// EventCombatAttackDirectRequest (CombatAttackDirectRequestPayload) signals applying knockback
	EventCombatAttackDirectRequest
	// EventCombatAttackAreaRequest (CombatAttackAreaRequestPayload) signals applying knockback
	EventCombatAttackAreaRequest

	// --- Loot ---

	// EventLootSpawnRequest (LootSpawnRequestPayload) requests direct loot spawn at position
	EventLootSpawnRequest

	// --- Missile ---

	// EventMissileSpawnRequest (MissileSpawnRequestPayload) signals launcher buff firing a cluster missile
	EventMissileSpawnRequest

	// --- Bullet ---

	// EventBulletSpawnRequest (BulletSpawnRequestPayload) signals creation of a linear projectile
	EventBulletSpawnRequest

	// --- Marker ---

	// EventMarkerSpawnRequest (MarkerSpawnRequestPayload) signals a request to spawn a visual marker
	EventMarkerSpawnRequest

	// --- Motion Marker ---

	// EventMotionMarkerShowColored (MotionMarkerShowPayload) signals a request to show colored glyph motion markers in ping bound
	EventMotionMarkerShowColored
	// EventMotionMarkerClearColored signals clearing colored motion markers (jump executed or cancelled)
	EventMotionMarkerClearColored

	// --- Mode ---

	// EventModeChanged (ModeChangedPayload) signals change of the mode
	EventModeChanged

	// --- Wall ---

	// EventWallSpawnRequest (WallSpawnRequestPayload) requests creation of a single wall cell
	EventWallSpawnRequest
	// EventWallBatchSpawnRequest (WallBatchSpawnRequestPayload) creates multiple wall cells in a single batch operation (supports collision modes)
	EventWallBatchSpawnRequest
	// EventWallCompositeSpawnRequest (WallCompositeSpawnRequestPayload) requests creation of a multi-cell wall structure
	EventWallCompositeSpawnRequest
	// EventWallPatternSpawnRequest (WallPatternSpawnRequestPayload) requests creation of wall structure from .vifimg pattern file
	EventWallPatternSpawnRequest
	// EventMazeSpawnRequest (MazeSpawnRequestPayload) signals maze generation and wall spawning
	EventMazeSpawnRequest
	// EventWallDespawnRequest (WallDespawnRequestPayload) requests removal of walls in specified area or globally
	EventWallDespawnRequest
	// EventWallMaskChangeRequest (WallMaskChangeRequestPayload) modifies blocking behavior of existing walls
	EventWallMaskChangeRequest
	// EventWallPushCheckRequest triggers full entity displacement check for blocking walls
	EventWallPushCheckRequest
	// EventWallSpawned (WallSpawnedPayload) notifies completion of wall creation with bounds and entity count
	EventWallSpawned
	// EventWallDespawned (WallDespawnedPayload) notifies completion of wall destruction with bounds
	EventWallDespawned
	// EventWallDespawnAll signals silent destruction of all wall entities
	EventWallDespawnAll

	// --- Fadeout ---

	// EventFadeoutSpawnOne (FadeoutSpawnPayload) signals intent to spawn a single fadeout effect
	EventFadeoutSpawnOne
	// EventFadeoutSpawnBatch (BatchPayload[FadeoutSpawnEntry]) signals intent to spawn multiple fadeout effects
	EventFadeoutSpawnBatch

	// --- Pylon ---

	// EventPylonSpawnRequest (PylonSpawnRequestPayload) signals pylon creation at location
	EventPylonSpawnRequest
	// EventPylonSpawnFailed signals pylon spawn could not find valid position
	EventPylonSpawnFailed
	// EventPylonCancelRequest signals forced destruction of all pylons
	EventPylonCancelRequest

	// --- Snake ---

	// EventSnakeSpawnRequest (SnakeSpawnRequestPayload) signals SnakeSystem to create the entity at location
	EventSnakeSpawnRequest
	// EventSnakeCancelRequest signals manual termination of all snakes
	EventSnakeCancelRequest

	// --- Navigation ---

	// EventTargetGroupUpdate (TargetGroupUpdatePayload) configures or updates a navigation target group
	EventTargetGroupUpdate
	// EventTargetGroupRemove (TargetGroupRemovePayload) removes a target group, entities fall back to group 0
	EventTargetGroupRemove
	// EventNavigationRegraph signals a request to recalculate navigation graphs
	EventNavigationRegraph
	// EventRouteGraphRequest (RouteGraphRequestPayload) requests route graph computation for a gateway-target pair
	EventRouteGraphRequest
	// EventRouteGraphComputed (RouteGraphComputedPayload) signals route graph computation completion
	EventRouteGraphComputed

	// --- Eye ---

	// EventEyeSpawnRequest (EyeSpawnRequestPayload) signals EyeSystem to create entity at location
	EventEyeSpawnRequest
	// EventEyeCancelRequest signals destruction of all eye composites
	EventEyeCancelRequest

	// --- Tower ---

	// EventTowerSpawnRequest (TowerSpawnRequestPayload) signals tower creation at location
	EventTowerSpawnRequest
	// EventTowerSpawnFailed signals tower spawn could not find valid position
	EventTowerSpawnFailed
	// EventTowerCancelRequest signals forced destruction of all towers
	EventTowerCancelRequest

	// --- Gateway ---

	// EventGatewaySpawnRequest (GatewaySpawnRequestPayload) signals GatewaySystem to create a gateway entity anchored to a parent
	EventGatewaySpawnRequest
	// EventGatewayDespawnRequest (GatewayDespawnRequestPayload) signals GatewaySystem to remove gateway for a specific anchor
	EventGatewayDespawnRequest
	// EventGatewayDespawned (GatewayDespawnedPayload) signals that a gateway entity has been cleaned up
	EventGatewayDespawned

	// --- Genetic ---

	// EventGeneticRegisterSpecies (GeneticRegisterSpeciesPayload) dynamically registers a species for evolution
	EventGeneticRegisterSpecies
	// EventGeneticAbandonEval (GeneticAbandonEvalPayload) abandons evaluation of the species
	EventGeneticAbandonEval

	// --- Debug ---

	// EventDebugFlowToggle (DebugFlowGroupPayload) toggles debug flow field visualization
	EventDebugFlowToggle
	// EventDebugGraphToggle (DebugFlowGroupPayload) toggles debug graph visualization
	EventDebugGraphToggle
)

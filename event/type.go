package event

// GameEvent represents a single game event with metadata
type GameEvent struct {
	Type    EventType
	Payload any
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
//	// EventDeathOne (core.Entity) ...                                   bit-packed scalar
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

	// EventGameReset signals a request to reset the game state
	EventGameReset
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

	// --- FSM ---

	// EventCycleDamageMultiplierIncrease signals cycle completion, doubles damage multiplier
	EventCycleDamageMultiplierIncrease
	// EventCycleDamageMultiplierReset signals cycle reset, resets damage multiplier to 1
	EventCycleDamageMultiplierReset

	// --- Nugget ---

	// EventNuggetCollected (NuggetCollectedPayload) signals nugget was collected by player
	EventNuggetCollected
	// EventNuggetDestroyed (NuggetDestroyedPayload) signals nugget was destroyed externally
	EventNuggetDestroyed
	// EventNuggetJumpRequest signals player intent to jump to active nugget
	EventNuggetJumpRequest

	// --- Cleaner ---

	// EventCleanerDirectionalRequest (DirectionalCleanerPayload) spawns 4-way cleaners from origin
	EventCleanerDirectionalRequest
	// EventCleanerSweepingRequest spawns cleaners on rows with positive/negative energy glyphs
	EventCleanerSweepingRequest
	// EventCleanerSweepingFinished marks cleaner animation completion
	EventCleanerSweepingFinished

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
	// EventGoldJumpRequest signals player intent to jump to active gold sequence
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
	// EventEnergyCrossedZero signals energy crossing zero
	EventEnergyCrossedZero
	// EventEnergyGlyphConsumed (EnergyGlyphConsumedPayload) signals glyph destruction for energy calculation
	EventEnergyGlyphConsumed
	// EventEnergyBlinkStart (EnergyBlinkPayload) signals visual blink trigger
	EventEnergyBlinkStart
	// EventEnergyBlinkStop signals blink clear
	EventEnergyBlinkStop

	// --- Shield ---

	// EventShieldActivate signals shield should become active
	EventShieldActivate
	// EventShieldDeactivate signals shield should become inactive
	EventShieldDeactivate
	// EventShieldDrainRequest (ShieldDrainRequestPayload) signals energy drain from external source
	EventShieldDrainRequest

	// --- Weapon ---

	// EventWeaponAddRequest (WeaponAddRequestPayload) signals activating buff for cursor
	EventWeaponAddRequest
	// EventWeaponFireRequest signals weapon fire request
	EventWeaponFireRequest
	// EventFireSpecialRequest signals player intent to fire special ability
	EventFireSpecialRequest

	// --- Heat ---

	// EventHeatAddRequest (HeatAddRequestPayload) signals heat delta modification
	EventHeatAddRequest
	// EventHeatSetRequest (HeatSetRequestPayload) signals absolute heat value
	EventHeatSetRequest
	// EventHeatBurst signals heat burst notification
	EventHeatBurst

	// --- Boost ---

	// EventBoostActivate (BoostActivatePayload) signals boost activation request
	EventBoostActivate
	// EventBoostDeactivate signals boost deactivation
	EventBoostDeactivate
	// EventBoostExtend (BoostExtendPayload) signals boost duration extension
	EventBoostExtend

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

	// EventDeathOne (core.Entity) signals intent to destroy a single game entity (scalar/silent)
	EventDeathOne
	// EventDeathBatch (DeathRequestPayload) signals intent to destroy a batch of entities with an optional effect
	EventDeathBatch

	// --- Timer ---

	// EventTimerStart (TimerStartPayload) signals creation of a lifecycle timer for an entity
	EventTimerStart

	// --- Composite ---

	// EventCompositeMemberDestroyed (CompositeMemberDestroyedPayload) signals a composite member was successfully typed
	EventCompositeMemberDestroyed
	// EventCompositeIntegrityBreach (CompositeIntegrityBreachPayload) signals unexpected member loss (OOB, enemy hit, etc.)
	EventCompositeIntegrityBreach
	// EventCompositeDestroyRequest (CompositeDestroyRequestPayload) signals owner system requests full composite destruction
	EventCompositeDestroyRequest

	// --- Cursor ---

	// EventCursorMoved (CursorMovedPayload) signals cursor position change
	EventCursorMoved

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
	// EventQuasarSpawned (QuasarSpawnedPayload) signals quasar composite creation
	EventQuasarSpawned
	// EventQuasarDestroyed signals quasar termination
	EventQuasarDestroyed
	// EventQuasarCancelRequest signals manual termination of the quasar phase
	EventQuasarCancelRequest

	// --- Swarm ---

	// EventSwarmSpawnRequest (SwarmSpawnRequestPayload) signals SwarmSystem to create the entity at location
	EventSwarmSpawnRequest
	// EventSwarmSpawned (SwarmSpawnedPayload) signals swarm composite creation
	EventSwarmSpawned
	// EventSwarmDestroyed (SwarmDestroyedPayload) signals swarm termination
	EventSwarmDestroyed
	// EventSwarmAbsorbedDrain (SwarmAbsorbedDrainPayload) signals drain absorbed by swarm
	EventSwarmAbsorbedDrain
	// EventSwarmCancelRequest signals destruction of all swarm composites
	EventSwarmCancelRequest

	// --- Storm ---

	// EventStormSpawnRequest triggers storm spawn
	EventStormSpawnRequest
	// EventStormCancelRequest signals destruction of all storm entities
	EventStormCancelRequest
	// EventStormCircleDestroyed (StormCircleDestroyedPayload) signals individual circle destruction
	EventStormCircleDestroyed
	// EventStormDestroyed (StormDestroyedPayload) signals all storm circles destroyed
	EventStormDestroyed

	// --- Post-Process ---

	// EventGrayoutStart signals persistent grayout activation
	EventGrayoutStart
	// EventGrayoutEnd signals persistent grayout deactivation
	EventGrayoutEnd
	// EventStrobeRequest (StrobeRequestPayload) triggers screen flash effect
	EventStrobeRequest

	// --- Spirit ---

	// EventSpiritSpawn (SpiritSpawnRequestPayload) signals intent to spawn a spirit entity
	EventSpiritSpawn
	// EventSpiritDespawn signals force-clear of all spirit entities
	EventSpiritDespawn

	// --- Lightning ---

	// EventLightningSpawnRequest (LightningSpawnRequestPayload) signals intent to spawn a lightning visual effect
	EventLightningSpawnRequest
	// EventLightningUpdate (LightningUpdatePayload) signals target position update for tracked lightning
	EventLightningUpdate
	// EventLightningDespawnRequest (LightningDespawnPayload) signals force-removal of lightning entity(ies)
	EventLightningDespawnRequest

	// --- Combat ---

	// EventCombatAttackDirectRequest (CombatAttackDirectRequestPayload) signals applying knockback
	EventCombatAttackDirectRequest
	// EventCombatAttackAreaRequest (CombatAttackAreaRequestPayload) signals applying knockback
	EventCombatAttackAreaRequest
	// EventEnemyCreated (EnemyCreatedPayload) signals enemy entity creation via its system
	EventEnemyCreated
	// EventEnemyKilled (EnemyKilledPayload) signals an enemy entity was destroyed via combat
	EventEnemyKilled

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
	// EventWallPatternSpawnRequest (WallPatternSpawnRequestPayload) requests creation of wall structure from .vfimg pattern file
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
	// EventPylonSpawned (PylonSpawnedPayload) signals pylon composite creation
	EventPylonSpawned
	// EventPylonDestroyed (PylonDestroyedPayload) signals pylon termination (all members dead)
	EventPylonDestroyed
	// EventPylonCancelRequest signals forced destruction of all pylons
	EventPylonCancelRequest

	// --- Snake ---

	// EventSnakeSpawnRequest (SnakeSpawnRequestPayload) signals SnakeSystem to create the entity at location
	EventSnakeSpawnRequest
	// EventSnakeSpawned (SnakeSpawnedPayload) signals snake composite creation complete
	EventSnakeSpawned
	// EventSnakeDestroyed (SnakeDestroyedPayload) signals snake termination
	EventSnakeDestroyed
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
	// EventEyeSpawned (EyeSpawnedPayload) signals eye composite creation
	EventEyeSpawned
	// EventEyeDestroyed (EyeDestroyedPayload) signals eye termination
	EventEyeDestroyed
	// EventEyeCancelRequest signals destruction of all eye composites
	EventEyeCancelRequest

	// --- Tower ---

	// EventTowerSpawnRequest (TowerSpawnRequestPayload) signals tower creation at location
	EventTowerSpawnRequest
	// EventTowerSpawnFailed signals tower spawn could not find valid position
	EventTowerSpawnFailed
	// EventTowerSpawned (TowerSpawnedPayload) signals tower composite creation
	EventTowerSpawned
	// EventTowerDestroyed (TowerDestroyedPayload) signals tower termination (all members dead)
	EventTowerDestroyed
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

	// --- Debug ---

	// EventDebugFlowToggle (DebugFlowGroupPayload) toggles debug flow field visualization
	EventDebugFlowToggle
	// EventDebugGraphToggle (DebugFlowGroupPayload) toggles debug graph visualization
	EventDebugGraphToggle
)

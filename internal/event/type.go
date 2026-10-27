package event

import "github.com/lixenwraith/vif/internal/core"

// GameEvent represents a single game event with metadata
type GameEvent struct {
	Payload any
	Type    EventType
	Seq     uint64 // Monotonic queue slot, stamped at push; orders events within a tick

	// CrossingSeq is the source-local wire sequence of this instance's own
	// crossing, stamped when the barrier publishes it. The scheduler returns it to
	// the sink after dispatch so a capture claims exactly the local crossings its
	// world contains, and the queue journals it whatever produced it.
	CrossingSeq uint64

	Origin Origin      // Producer, for journaling and replay; never affects dispatch
	Domain core.Domain // Producer domain, for journaling and replication; never affects dispatch

	// Phase marks a record the producer may have to derive again because the
	// shared state behind it is this instance's prediction. Stamped by
	// World.pushEvent, read by the player-domain consumers a second derivation
	// would pay twice; never affects dispatch or replication.
	Phase EventPhase
}

// EventType represents the type of game event
type EventType int

// EventType comments generate the registry: EventFoo (FooPayload) [class] description.
// Keep matching event/payload stems; omit the annotation only for nil payloads.
// Generic or qualified payload types register nil; authorable fields need TOML tags.
// Values are contiguous and never serialized. EventNone stays zero for FSM ticks
// and the no-effect sentinel; regenerate instead of editing registry_gen.go.

const (
	// EventNone is the zero value, reserved so that no real event aliases the FSM
	// tick sentinel (fsm.Transition.Event == 0) or the no-effect sentinel a field
	// such as a death batch's EffectEvent leaves unset.
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
	// EventParticipantJoined (ParticipantJoinedPayload) [bus] adds a participant to the session roster
	EventParticipantJoined
	// EventParticipantDeparted (ParticipantDepartedPayload) [bus] removes a participant from the session roster
	EventParticipantDeparted
	// EventPlayoutLead (PlayoutLeadPayload) [local] carries the lead this instance stamps its own crossings with
	EventPlayoutLead
	// EventCursorStateSync (CursorStatePayload) [local] carries one cursor's owner-authored state to the instances that do not simulate it
	EventCursorStateSync
	// EventSessionState (SessionStatePayload) [local] journals the session a world's simulation reads; never dispatched
	EventSessionState

	// --- Meta ---

	// EventGameResetRequest (GameResetPayload) [bus] resets the session; shared producers re-derive it, operator producers cross it
	EventGameResetRequest
	// EventMetaTelemetryRequest [local] signals a request to show the telemetry overlay
	EventMetaTelemetryRequest
	// EventMetaDebugRequest [local] signals a request to show the profiler report
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
	// EventGameHoldRequest (GamePausePayload) [local] asks MetaSystem to start or end a hold, the pause only :pause ends
	EventGameHoldRequest
	// EventGameHold [bus] starts a hold; a live session's host crosses it, so every participant stops after one tick
	EventGameHold

	// --- FSM ---

	// EventCycleDamageMultiplierIncrease [shared] signals cycle completion, doubles damage multiplier
	EventCycleDamageMultiplierIncrease
	// EventCycleDamageMultiplierReset [shared] signals cycle reset, resets damage multiplier to 1
	EventCycleDamageMultiplierReset
	// EventFSMRegionRequest (FSMRegionPayload) [shared] signals FSM to change the active region
	EventFSMRegionRequest
	// EventFSMTransition (FSMTransitionPayload) [bus] is one transition the authority's FSM took, applied at one tick everywhere
	EventFSMTransition

	// --- Nugget ---

	// EventNuggetCollected (NuggetCollectedPayload) [local] signals the personal nugget was collected
	EventNuggetCollected
	// EventNuggetDestroyed (NuggetDestroyedPayload) [local] signals the personal nugget was destroyed externally
	EventNuggetDestroyed
	// EventNuggetJumpRequest (NuggetJumpRequestPayload) [local] signals player intent to jump to their active nugget
	EventNuggetJumpRequest

	// --- Gold ---

	// EventGoldSpawnRequest [shared] signals a specific request to try spawning a gold sequence
	EventGoldSpawnRequest
	// EventGoldSpawnFailed [shared] signals that a requested spawn could not be completed (e.g. no space)
	EventGoldSpawnFailed
	// EventGoldSpawned (GoldSpawnedPayload) [shared] answers a spawn request with a new sequence, or the one already running
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

	// EventWeaponAddRequest (WeaponAddRequestPayload) [local] grants or recharges one kind on a cursor
	EventWeaponAddRequest
	// EventWeaponRemoveRequest (WeaponRemoveRequestPayload) [local] takes one kind from a cursor's loadout
	EventWeaponRemoveRequest
	// EventWeaponFireRequest (WeaponFireRequestPayload) [local] signals weapon fire request
	EventWeaponFireRequest
	// EventGunAimRequest (GunAimRequestPayload) [local] sets the cardinals, or the upgrade path, of the gun of the cursor this instance drives
	EventGunAimRequest
	// EventFireSpecialRequest (FireSpecialRequestPayload) [local] signals player intent to fire special ability
	EventFireSpecialRequest
	// EventMountRequest (MountRequestPayload) [shared] puts a weapon on a Shared host
	EventMountRequest

	// --- Heat ---

	// EventHeatAddRequest (HeatAddRequestPayload) [local] signals heat delta modification
	EventHeatAddRequest
	// EventHeatSetRequest (HeatSetRequestPayload) [local] signals absolute heat value
	EventHeatSetRequest
	// EventHeatSpendRequest (HeatSpendRequestPayload) [local] spends overheat before current heat
	EventHeatSpendRequest
	// EventHeatBurst (HeatBurstPayload) [local] signals heat burst notification
	EventHeatBurst
	// EventEmberSpent (HeatBurstPayload) [bus] carries an owner's ember burning its heat out
	EventEmberSpent

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
	// EventKeyCaptureRequest (KeyCapturePayload) [local] replaces the keys a scenario takes from Normal, Visual and Insert parsing
	EventKeyCaptureRequest
	// EventInputPressed (InputPayload) [bus] signals a captured key pressed from a roster slot, typed or clicked on its button
	EventInputPressed
	// EventInputReleased (InputPayload) [bus] signals the release of a captured key's press, made at once for a tap
	EventInputReleased
	// EventInputPressEcho (InputPayload) [local] presents this instance's own press at once, a playout lead before EventInputPressed applies
	EventInputPressEcho
	// EventInputReleaseEcho (InputPayload) [local] presents this instance's own release at once, as EventInputPressEcho does a press
	EventInputReleaseEcho
	// EventInputReport (InputReportPayload) [bus] signals whether a roster slot's keys release or tap while keys are captured
	EventInputReport

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

	// EventExplosionRequest (ExplosionRequestPayload) [bus] resolves shared combat geometry at one center
	EventExplosionRequest
	// EventExplosionBatchRequest (ExplosionBatchRequestPayload) [bus] resolves shared combat geometry at several centers
	EventExplosionBatchRequest
	// EventExplosionVisualRequest (ExplosionVisualRequestPayload) [local] presents one explosion center for its producer
	EventExplosionVisualRequest
	// EventExplosionVisualBatchRequest (ExplosionVisualBatchRequestPayload) [local] presents several explosion centers for its producer
	EventExplosionVisualBatchRequest
	// EventPulseVisualRequest (PulseVisualRequestPayload) [local] presents one disruptor pulse ring for its producer
	EventPulseVisualRequest

	// --- Particle ---

	// EventDustAllRequest [local] signals intent to convert all glyphs on the map to dust
	EventDustAllRequest

	// EventParticleSpawnOne (ParticleSpawnPayload) [local] signals intent to spawn one behavior-selected particle
	EventParticleSpawnOne
	// EventParticleSpawnBatch (BatchPayload[ParticleSpawnEntry]) [local] signals batch spawn of behavior-selected particles
	EventParticleSpawnBatch
	// EventParticleWave (ParticleWavePayload) [local] signals a wave selected by behavior, across the map or one button
	EventParticleWave
	// EventParticleVortex (ParticleVortexPayload) [local] turns every glyph into particles of a behavior and draws all of that behavior around one button
	EventParticleVortex

	// --- Death ---

	// EventDeathBatch (DeathRequestPayload) [stamped] signals intent to destroy one or more entities with an optional effect
	EventDeathBatch

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
	// EventCursorArmRequest (CursorArmRequestPayload) [local] sets the heat, energy and ember of the cursor this instance drives
	EventCursorArmRequest
	// EventCursorDespawnRequest (CursorDespawnRequestPayload) [shared] asks CursorSystem to destroy cursors
	EventCursorDespawnRequest
	// EventCursorDespawned (CursorDespawnedPayload) [shared] announces a cursor leaving play: destroyed, or turned Wraith
	EventCursorDespawned
	// EventCursorWraithRequest (CursorWraithRequestPayload) [shared] asks CursorSystem to turn a departed participant's cursor Wraith
	EventCursorWraithRequest
	// EventCursorMoveRequest (CursorMoveRequestPayload) [bus] asks CursorSystem to place a cursor
	EventCursorMoveRequest
	// EventCursorMoved (CursorMovedPayload) [shared] announces an applied cursor position
	EventCursorMoved
	// EventCursorPredicted (CursorMoveRequestPayload) [local] journals the D-18 prediction a placement made; never dispatched
	EventCursorPredicted
	// EventCursorDefeatState (CursorDefeatStatePayload) [bus] carries one owner's terminal lifecycle state
	EventCursorDefeatState
	// EventCursorCrushed (CursorCrushedPayload) [shared] announces scrolling rock pressing a cursor against the map's left edge
	EventCursorCrushed
	// EventCursorSetLocalRequest (CursorSetLocalPayload) [local] rebinds which cursor input and camera follow
	EventCursorSetLocalRequest
	// EventCursorLocalChanged (CursorSetLocalPayload) [local] announces the bound slot
	EventCursorLocalChanged

	// --- Species ---

	// EventSpeciesCreated (SpeciesCreatedPayload) [stamped] announces a created species instance
	EventSpeciesCreated
	// EventSpeciesKilled (SpeciesKilledPayload) [stamped] announces a terminated species instance
	EventSpeciesKilled
	// EventSpeciesKillConfirmed (SpeciesKilledPayload) [local] re-raises a predicted death an authoritative world proved
	EventSpeciesKillConfirmed
	// EventDrainDefeated (DrainDefeatedPayload) [bus] advances progression and names the causal cursor domain
	EventDrainDefeated

	// --- Fuse ---

	// EventFuseQuasarRequest (FuseQuasarRequestPayload) [local] asks the elected cursor's drains to fuse into quasar
	EventFuseQuasarRequest
	// EventFuseSwarmRequest (FuseSwarmRequestPayload) [local] signals two enraged drains should fuse into swarm
	EventFuseSwarmRequest
	// EventFuseBehaviorRequest (FuseBehaviorRequestPayload) [shared] beams a program's species in, its spawn sent as the beam ends
	EventFuseBehaviorRequest

	// --- Drain ---

	// EventDrainPause (CursorScopePayload) [local] signals DrainSystem to stop spawning for one cursor, or for every cursor when it names none
	EventDrainPause
	// EventDrainResume (CursorScopePayload) [local] releases one cursor's spawning pause, or every pause when it names none
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

	// --- Kraken ---

	// EventKrakenSpawnRequest (KrakenSpawnRequestPayload) [shared] creates a Kraken near the requested cell
	EventKrakenSpawnRequest
	// EventKrakenSpawnFailed [shared] reports a body that could not be placed
	EventKrakenSpawnFailed
	// EventKrakenCancelRequest [shared] removes all Kraken composites
	EventKrakenCancelRequest

	// --- Behavior ---

	// EventBehaviorSpawnRequest (BehaviorSpawnRequestPayload) [bus] signals BehaviorSystem to create a program's species at location
	EventBehaviorSpawnRequest
	// EventBehaviorCancelRequest (BehaviorCancelRequestPayload) [shared] removes one program's composites, or every program's when it names none
	EventBehaviorCancelRequest

	// --- Environment ---

	// EventWindStart (WindStartPayload) [shared] starts or replaces the global wind
	EventWindStart
	// EventWindCancel [shared] cancels the global wind
	EventWindCancel
	// EventDriftStart (DriftStartPayload) [shared] starts or replaces the global glyph drift, at once or eased
	EventDriftStart
	// EventDriftCancel (DriftCancelPayload) [shared] cancels the global glyph drift, at once or eased to a rest
	EventDriftCancel

	// --- Post-Process ---

	// EventGrayoutStart (CursorScopePayload) [local] signals persistent grayout activation for one cursor, or for every cursor when it names none
	EventGrayoutStart
	// EventGrayoutEnd (CursorScopePayload) [local] signals persistent grayout deactivation, scoped the same way
	EventGrayoutEnd
	// EventStrobeRequest (StrobeRequestPayload) [local] triggers screen flash effect
	EventStrobeRequest

	// --- Spirit ---

	// EventSpiritSpawnRequest (SpiritSpawnRequestPayload) [stamped] signals intent to spawn a spirit entity
	EventSpiritSpawnRequest
	// EventSpiritDespawnRequest [stamped] signals force-clear of all spirit entities
	EventSpiritDespawnRequest
	// EventSpiritArrived (SpiritArrivedPayload) [stamped] signals a spirit naming a species reached its target, which lands the species
	EventSpiritArrived

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

	// EventLootSpawnRequest (LootSpawnRequestPayload) [local] is an event drop: one loot kind at a cell for each cursor this instance simulates
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

	// --- Button ---

	// EventButtonSpawnRequest (ButtonSpawnRequestPayload) [shared] adds a button on a key to the end of the row
	EventButtonSpawnRequest
	// EventButtonLightRequest (ButtonLightRequestPayload) [shared] lights a button and plays its sound
	EventButtonLightRequest

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
	// EventCaveStart (CaveStartPayload) [shared] fills the map with a cave and scrolls it left, in place of any running one
	EventCaveStart
	// EventCaveStop (CaveStopPayload) [shared] halts the cave's scroll at once or eased to a rest, leaving its walls where they stand
	EventCaveStop
	// EventCavePace (CavePacePayload) [shared] sets the running cave's scroll to a new pace, at once or eased, resuming a halted one
	EventCavePace
	// EventCaveOpen (CaveOpenPayload) [shared] widens the cave from just past the map's right edge to an opening it then holds
	EventCaveOpen
	// EventCaveOpened [shared] announces the cave's opening filling the map
	EventCaveOpened
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

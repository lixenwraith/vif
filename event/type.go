package event

// EventType represents the type of game event
type EventType int

const (
	// === Engine Event === // TODO: future implementation
	// Mass entity cleanup
	EventWorldClear EventType = iota

	// System activation control
	EventSystemToggle

	// === Audio Event ===

	// EventSoundRequest requests audio playback
	// Trigger: Systems requiring audio feedback
	// Consumer: AudioSystem | Payload: *SoundRequestPayload
	EventSoundRequest

	// === Network Event ===

	// EventNetworkConnect signals a new peer connection
	// Trigger: NetworkService on accepted/established connection
	// Consumer: Game systems | Payload: *NetworkConnectPayload
	EventNetworkConnect

	// EventNetworkDisconnect signals peer disconnection
	// Trigger: NetworkService on connection close
	// Consumer: Game systems | Payload: *NetworkDisconnectPayload
	EventNetworkDisconnect

	// EventRemoteInput signals input from a remote player
	// Trigger: NetworkService on MsgInput received
	// Consumer: InputSystem | Payload: *RemoteInputPayload
	EventRemoteInput

	// EventStateSync signals state snapshot received
	// Trigger: NetworkService on MsgStateSync received
	// Consumer: SyncSystem | Payload: *StateSyncPayload
	EventStateSync

	// EventNetworkEvent signals a game event from remote peer
	// Trigger: NetworkService on MsgEvent received
	// Consumer: Game systems | Payload: *NetworkEventPayload
	EventNetworkEvent

	// EventNetworkError signals a network error
	// Trigger: NetworkService on error
	// Consumer: UISystem | Payload: *NetworkErrorPayload
	EventNetworkError

	// === Game Event ===

	// EventNuggetCollected signals nugget was collected by player
	// Trigger: EnergySystem on successful nugget character match
	// Consumer: NuggetSystem | Payload: *NuggetCollectedPayload
	EventNuggetCollected

	// EventNuggetDestroyed signals nugget was destroyed externally
	// Trigger: DrainSystem collision, DecaySystem collision
	// Consumer: NuggetSystem | Payload: *NuggetDestroyedPayload
	EventNuggetDestroyed

	// EventNuggetJumpRequest signals player intent to jump to active nugget
	// Trigger: InputHandler (Tab key)
	// Consumer: NuggetSystem | Payload: nil
	EventNuggetJumpRequest

	// EventGoldEnable signals whether gold sequence spawning is allowed
	// Trigger: FSM entering/exiting Normal state
	// Consumer: GoldSystem | Payload: *GoldEnablePayload
	EventGoldEnable

	// EventGoldSpawnRequest signals a specific request to try spawning a gold sequence
	// Trigger: FSM GameState Action
	// Consumer: GoldSystem | Payload: nil
	EventGoldSpawnRequest

	// EventGoldSpawnFailed signals that a requested spawn could not be completed (e.g. no space)
	// Trigger: GoldSystem
	// Consumer: FSM (to return to Normal/Wait) | Payload: nil
	EventGoldSpawnFailed

	// EventGoldSpawned signals gold sequence creation
	// Trigger: GoldSystem spawns sequence in PhaseNormal
	// Consumer: SplashSystem (timer) | Payload: *GoldSpawnedPayload
	EventGoldSpawned

	// EventGoldComplete signals successful gold sequence completion
	// Trigger: Final gold character typed
	// Consumer: SplashSystem (destroy timer) | Payload: *GoldCompletionPayload
	EventGoldComplete

	// EventGoldTimeout signals gold sequence expiration
	// Trigger: GoldSystem timeout | Payload: *GoldCompletionPayload
	EventGoldTimeout

	// EventGoldDestroyed signals external gold destruction (e.g., Drain)
	// Payload: *GoldCompletionPayload
	EventGoldDestroyed

	// EventGoldCancel signals mandatory cleanup of any active gold sequence
	// Trigger: FSM exiting QuasarPhase or Reset
	// Consumer: GoldSystem | Payload: nil
	EventGoldCancel

	// EventGoldJumpRequest signals player intent to jump to active gold sequence
	// Trigger: InputHandler (Shift+Tab key)
	// Consumer: GoldSystem | Payload: nil
	EventGoldJumpRequest

	// EventCleanerDirectionalRequest spawns 4-way cleaners from origin
	// Trigger: Nugget collected at max heat, Enter in Normal or mode
	// Consumer: CleanerSystem | Payload: *DirectionalCleanerPayload
	EventCleanerDirectionalRequest

	// EventCleanerSweepingRequest spawns cleaners on rows with Red(positive energy) or Blue(negative energy) glyphs
	// Trigger: Gold sequence completed at max heat
	// Consumer: CleanerSystem | Payload: nil
	EventCleanerSweepingRequest

	// EventCleanerSweepingFinished marks cleaner animation completion
	// Trigger: GetAllEntities cleaner entities destroyed | Payload: nil
	EventCleanerSweepingFinished

	// EventCharacterTyped signals Insert mode keypress
	// Trigger: InputHandler on printable key
	// Consumer: EnergySystem | Payload: *CharacterTypedPayload
	// Latency: max 50ms (next tick)
	EventCharacterTyped

	// EventSplashRequest signals transient visual feedback
	// Trigger: Character typed, command executed, nugget collected
	// Consumer: SplashSystem | Payload: *SplashRequestPayload
	EventSplashRequest

	// EventSplashTimerRequest signals timer visual feedback
	// Trigger: GoldSystem, QuasarSystem, ...
	// Consumer: SplashSystem | Payload: *SplashTimerRequestPayload
	EventSplashTimerRequest

	// EventSplashTimerCancel signals ending timer visual feedback
	// Trigger: GoldSystem, QuasarSystem, ...
	// Consumer: SplashSystem | Payload: *SplashTimerCancelPayload
	EventSplashTimerCancel

	// EventEnergyAddAmount signals energy delta on target entity
	// Trigger: Character typed, shield drain, nugget jump
	// Consumer: EnergySystem | Payload: *EnergyAddAmountPayload
	EventEnergyAddAmount

	// EventEnergySetAmount signals setting energy to specific value
	// Trigger: Game reset, cheats
	// Consumer: EnergySystem | Payload: *EnergySetAmountPayload
	EventEnergySetAmount

	// EventEnergyAddPercent signals energy delta percentage on target entity
	// Trigger: Shield passive drain
	// Consumer: EnergySystem | Payload: *EnergyAddAmountPayload
	EventEnergyAddPercent

	// EventEnergyCrossedZero signals energy crossing zero
	// Trigger: EnergySystem
	// Consumer: BuffSystem | Payload: nil
	EventEnergyCrossedZero

	// EventEnergyGlyphConsumed signals glyph destruction for energy calculation
	// Trigger: TypingSystem (correct character), DustSystem (shield collision)
	// Consumer: EnergySystem | Payload: *GlyphConsumedPayload
	EventEnergyGlyphConsumed

	// EventEnergyBlinkStart signals visual blink trigger
	// Trigger: Character typed (success/error)
	// Consumer: EnergySystem | Payload: *EnergyBlinkPayload
	EventEnergyBlinkStart

	// EventEnergyBlinkStop signals blink clear
	// Trigger: EnergySystem timeout
	// Consumer: EnergySystem | Payload: nil
	EventEnergyBlinkStop

	// EventHeatAdd signals heat delta modification
	// Trigger: Character typed, drain hit, nugget collected
	// Consumer: HeatSystem | Payload: *HeatAddPayload
	EventHeatAdd

	// EventHeatSet signals absolute heat value
	// Trigger: Gold complete, debug command, boost command, error reset
	// Consumer: HeatSystem | Payload: *HeatSetPayload
	EventHeatSet

	// EventShieldActivate signals shield should become active
	// Trigger: EnergySystem when energy > 0 and shield inactive
	// Consumer: ShieldSystem | Payload: nil
	EventShieldActivate

	// EventShieldDeactivate signals shield should become inactive
	// Trigger: EnergySystem when energy <= 0 and shield active
	// Consumer: ShieldSystem | Payload: nil
	EventShieldDeactivate

	// EventShieldDrain signals energy drain from external source
	// Trigger: DrainSystem when drain inside shield zone
	// Consumer: ShieldSystem | Payload: *ShieldDrainPayload
	EventShieldDrain

	// EventDeleteRequest signals a deletion operation (x, d, etc.)
	// Trigger: InputHandler via modes package
	// Consumer: EnergySystem | Payload: *DeleteRequestPayload
	EventDeleteRequest

	// EventPingGridRequest signals a request to show the ping grid
	// Trigger: InputHandler (relative line numbers toggle, etc.)
	// Consumer: PingSystem | Payload: *PingGridRequestPayload
	EventPingGridRequest

	// EventGameReset signals a request to reset the game state
	// Trigger: Command :new
	// Consumer: MetaSystem | Payload: nil
	EventGameReset

	// EventMetaDebugRequest signals a request to show debug overlay
	// Trigger: Command :debug
	// Consumer: MetaSystem | Payload: nil
	EventMetaDebugRequest

	// EventMetaHelpRequest signals a request to show help overlay
	// Trigger: Command :help
	// Consumer: MetaSystem | Payload: nil
	EventMetaHelpRequest

	// EventMetaHelpRequest signals a request to show help overlay
	// Trigger: Systems
	// Consumer: MetaSystem | Payload: *MetaStatusMessagePayload
	EventMetaStatusMessageRequest

	// EventMetaSystemCommandRequest signals a request to show help overlay
	// Trigger: MetaSystems
	// Consumer: Systems | Payload: *MetaSystemCommandPayload
	EventMetaSystemCommandRequest

	// EventTimerStart signals creation of a lifecycle timer for an entity
	// Trigger: Systems creating transient entities (Splash, Flash)
	// Consumer: TimeKeeperSystem | Payload: *TimerStartPayload
	EventTimerStart

	// EventBoostActivate signals boost activation request
	// Trigger: Max heat reached, :boost command
	// Consumer: BoostSystem | Payload: *BoostActivatePayload
	EventBoostActivate

	// EventBoostDeactivate signals boost deactivation
	// Trigger: Red character typed, error state
	// Consumer: BoostSystem | Payload: nil
	EventBoostDeactivate

	// EventBoostExtend signals boost duration extension
	// Trigger: Correct character typed while boost active
	// Consumer: BoostSystem | Payload: *BoostExtendPayload
	EventBoostExtend

	// EventMaterializeRequest signals a request to start a materialization visual effect
	// Trigger: DrainSystem (or others) determining a spawn location
	// Consumer: MaterializeSystem | Payload: *MaterializeRequestPayload
	EventMaterializeRequest

	// EventMaterializeComplete signals materialization finished at location
	// Trigger: MaterializeSystem
	// Consumer: DrainSystem (or others) | Payload: *SpawnCompletePayload
	EventMaterializeComplete

	// EventFlashRequest signals a request to spawn a destruction flash effect
	// Trigger: Systems destroying entities with visual feedback (Drain, Cleaner, Decay)
	// Consumer: FlashSystem | Payload: *FlashRequestPayload
	EventFlashRequest

	// EventBlossomSpawnOne signals intent to spawn a single blossom entity
	// Trigger: DeathSystem on death with blossom effect (cleaner + positive energy)
	// Consumer: BlossomSystem | Payload: *BlossomSpawnPayload
	EventBlossomSpawnOne

	// EventBlossomWave signals start of a full width rising blossom wave
	// Trigger: FSM
	// Consumer: BlossomSystem | Payload: nil
	EventBlossomWave

	// EventDecaySpawnOne signals intent to spawn a single decay entity
	// Trigger: DeathSystem on death with decay effect (cleaner + negative energy)
	// Consumer: DecaySystem | Payload: *DecaySpawnPayload
	EventDecaySpawnOne

	// EventDecayWave signals start of a full width falling decay wave
	// Trigger: FSM
	// Consumer: DecaySystem | Payload: nil
	EventDecayWave

	// EventDeathOne signals intent to destroy a single game entity (scalar/silent)
	// Trigger: TypingSystem, NuggetSystem, etc.
	// Consumer: DeathSystem | Payload: core.Entity
	EventDeathOne

	// EventDeathBatch signals intent to destroy a batch of entities with an optional effect
	// Trigger: CleanerSystem, DecaySystem, etc.
	// Consumer: DeathSystem | Payload: *DeathRequestPayload
	EventDeathBatch

	// EventMemberTyped signals a composite member was successfully typed
	// Trigger: TypingSystem on valid member character match
	// Consumer: CompositeSystem routes to behavior-specific handler
	// Payload: *MemberTypedPayload
	EventMemberTyped

	// EventCursorMoved signals cursor position change
	// Trigger: InputHandler on cursor movement (h/j/k/l, arrow keys, etc.)
	// Consumer: SplashSystem (magnifier) | Payload: *CursorMovedPayload
	EventCursorMoved

	// EventFuseDrains signals drains should fuse into quasar
	// Trigger: FSM
	// Consumer: FuseSystem | Payload: nil
	EventFuseDrains

	// EventDrainPause signals DrainSystem to stop spawning
	// Trigger: FuseSystem before destroying drains
	// Consumer: DrainSystem | Payload: nil
	EventDrainPause

	// EventDrainResume signals DrainSystem to resume spawning
	// Trigger: QuasarSystem on quasar termination
	// Consumer: DrainSystem | Payload: nil
	EventDrainResume

	// EventQuasarSpawned signals quasar composite creation
	// Trigger: FuseSystem after creating quasar
	// Consumer: QuasarSystem | Payload: *QuasarSpawnedPayload
	EventQuasarSpawned

	// EventQuasarDestroyed signals quasar termination
	// Trigger: QuasarSystem on lifecycle end
	// Consumer: (future: audio/effects) | Payload: nil
	EventQuasarDestroyed

	// EventQuasarCancel signals manual termination of the quasar phase
	// Trigger: FSM (on GoldComplete during Quasar)
	// Consumer: QuasarSystem | Payload: nil
	EventQuasarCancel

	// EventGrayoutStart signals persistent grayout activation
	// Trigger: QuasarSystem on activation
	// Consumer: GameState | Payload: nil
	EventGrayoutStart

	// EventGrayoutEnd signals persistent grayout deactivation
	// Trigger: QuasarSystem on termination
	// Consumer: GameState | Payload: nil
	EventGrayoutEnd

	// EventSpiritSpawn signals intent to spawn a spirit entity
	// Trigger: FuseSystem (or other systems needing convergence VFX)
	// Consumer: SpiritSystem | Payload: *SpiritSpawnPayload
	EventSpiritSpawn

	// EventSpiritDespawn signals force-clear of all spirit entities
	// Trigger: FuseSystem timer expiry (safety mechanism)
	// Consumer: SpiritSystem | Payload: nil
	EventSpiritDespawn

	// EventLightningSpawn signals intent to spawn a lightning visual effect
	// Trigger: QuasarSystem (zap), FuseSystem (convergence)
	// Consumer: LightningSystem | Payload: *LightningSpawnPayload
	EventLightningSpawn

	// EventLightningUpdate signals target position update for tracked lightning
	// Trigger: QuasarSystem (cursor tracking while zapping)
	// Consumer: LightningSystem | Payload: *LightningUpdatePayload
	EventLightningUpdate

	// EventLightningDespawn signals force-removal of specific lightning entity
	// Trigger: QuasarSystem (zap ends)
	// Consumer: LightningSystem | Payload: core.Entity
	EventLightningDespawn

	// EventFireSpecialRequest signals player intent to fire special ability
	// Trigger: InputHandler (\ key)
	// Consumer: TBD | Payload: nil
	EventFireSpecialRequest

	// EventExplosionRequest triggers explosion effect at location
	// Trigger: EventFireSpecialRequest handler, or programmatic
	// Consumer: ExplosionSystem | Payload: *ExplosionRequestPayload
	EventExplosionRequest

	// EventDustSpawnOne signals intent to spawn a single dust entity
	// Trigger: ExplosionSystem, future effects
	// Consumer: DustSystem | Payload: *DustSpawnPayload
	EventDustSpawnOne

	// EventDustSpawnBatch signals intent to spawn multiple dust entities
	// Trigger: ExplosionFieldSystem on glyph transformation
	// Consumer: DustSystem | Payload: *DustSpawnBatchPayload
	EventDustSpawnBatch

	// EventDustAll signals intent to spawn a single dust entity
	// Trigger: FSM
	// Consumer: DustSystem | Payload: nil
	EventDustAll

	// EventSwarmSpawnRequest signals spawning a wave of 5 swarm composites
	// Trigger: FSM
	// Consumer: SwarmSystem | Payload: nil
	EventSwarmSpawnRequest

	// EventSwarmCancel signals destruction of all swarm composites
	// Trigger: FSM
	// Consumer: SwarmSystem | Payload: nil
	EventSwarmCancel
)

// GameEvent represents a single game event with metadata
type GameEvent struct {
	Type    EventType
	Payload any
}
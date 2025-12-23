package event

// EventType represents the type of game event
type EventType int

const (
	// === Audio Events ===

	// EventSoundRequest requests audio playback
	// Trigger: Systems requiring audio feedback
	// Consumer: AudioSystem | Payload: *SoundRequestPayload
	EventSoundRequest EventType = iota

	// === Network Events ===

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

	// === Game Events ===

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
	// Trigger: FSM State Action
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

	// EventDirectionalCleanerRequest spawns 4-way cleaners from origin
	// Trigger: Nugget collected at max heat, Enter in Normal mode with heat >= 10
	// Consumer: CleanerSystem | Payload: *DirectionalCleanerPayload
	EventDirectionalCleanerRequest

	// EventManualCleanerTrigger signals player request to use cleaner ability (consumes heat)
	// Trigger: Enter key in Insert/Normal mode
	// Consumer: HeatSystem | Payload: nil
	EventManualCleanerTrigger

	// EventCleanerRequest spawns cleaners on rows with Red characters
	// Trigger: Gold sequence completed at max heat
	// Consumer: CleanerSystem | Payload: nil
	EventCleanerRequest

	// EventCleanerFinished marks cleaner animation completion
	// Trigger: All cleaner entities destroyed | Payload: nil
	EventCleanerFinished

	// EventCharacterTyped signals Insert mode keypress
	// Trigger: InputHandler on printable key
	// Consumer: EnergySystem | Payload: *CharacterTypedPayload
	// Latency: max 50ms (next tick)
	EventCharacterTyped

	// EventSplashRequest signals transient visual feedback
	// Trigger: Character typed, command executed, nugget collected
	// Consumer: SplashSystem | Payload: *SplashRequestPayload
	EventSplashRequest

	// EventEnergyAdd signals energy delta on target entity
	// Trigger: Character typed, shield drain, nugget jump
	// Consumer: EnergySystem | Payload: *EnergyAddPayload
	EventEnergyAdd

	// EventEnergySet signals setting energy to specific value
	// Trigger: Game reset, cheats
	// Consumer: EnergySystem | Payload: *EnergySetPayload
	EventEnergySet

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
	// Consumer: CommandSystem | Payload: nil
	EventGameReset

	// EventSpawnChange signals a request to enable/disable spawning
	// Trigger: Command :spawn
	// Consumer: SpawnSystem | Payload: *SpawnChangePayload
	EventSpawnChange

	// EventDebugRequest signals a request to show debug overlay
	// Trigger: Command :debug
	// Consumer: CommandSystem | Payload: nil
	EventDebugRequest

	// EventHelpRequest signals a request to show help overlay
	// Trigger: Command :help
	// Consumer: CommandSystem | Payload: nil
	EventHelpRequest

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

	// EventDecayStart signals decay timer expired and animation should begin
	// Trigger: FSM entering DecayAnimation state
	// Consumer: DecaySystem | Payload: nil
	EventDecayStart

	// EventDecayCancel signals that the decay phase should be aborted immediately
	// Trigger: Game reset, Phase change interruption
	// Consumer: DecaySystem | Payload: nil
	EventDecayCancel

	// EventDecayComplete signals all decay entities destroyed
	// Trigger: DecaySystem when entity count reaches zero
	// Consumer: ClockScheduler | Payload: nil
	EventDecayComplete

	// EventDecaySpawnOne signals intent to spawn a single decay entity
	// Trigger: DeathSystem on death with decay effect (cleaner + negative energy)
	// Consumer: DecaySystem | Payload: *DecaySpawnPayload
	EventDecaySpawnOne

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
)

// GameEvent represents a single game event with metadata
type GameEvent struct {
	Type    EventType
	Payload any
	Frame   int64 // For deduplication
}
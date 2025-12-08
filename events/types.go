package events

import (
	"time"
)

// EventType represents the type of game event
type EventType int

const (
	// EventCleanerRequest spawns cleaners on rows with Red characters
	// Trigger: Gold sequence completed at max heat
	// Consumer: CleanerSystem | Payload: nil
	EventCleanerRequest EventType = iota

	// EventDirectionalCleanerRequest spawns 4-way cleaners from origin
	// Trigger: Nugget collected at max heat, Enter in Normal mode with heat >= 10
	// Consumer: CleanerSystem | Payload: *DirectionalCleanerPayload
	EventDirectionalCleanerRequest

	// EventCleanerFinished marks cleaner animation completion
	// Trigger: All cleaner entities destroyed | Payload: nil
	EventCleanerFinished

	// EventNuggetJumpRequest signals player intent to jump to active nugget
	// Trigger: InputHandler (Tab key)
	// Consumer: NuggetSystem | Payload: nil
	EventNuggetJumpRequest

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

	// EventManualCleanerTrigger signals player request to use cleaner ability (consumes heat)
	// Trigger: Enter key in Insert/Normal mode
	// Consumer: HeatSystem | Payload: nil
	EventManualCleanerTrigger

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
)

// GameEvent represents a single game event with metadata
type GameEvent struct {
	Type      EventType
	Payload   any
	Frame     int64 // For deduplication
	Timestamp time.Time
}
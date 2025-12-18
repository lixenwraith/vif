package events

import (
	"reflect"
	"strings"
)

var (
	nameToType    = make(map[string]EventType)
	typeToName    = make(map[EventType]string)
	typeToPayload = make(map[EventType]reflect.Type)
	registryInit  = false
)

// RegisterType maps a string name to an EventType and its payload struct type
// payloadInstance should be a pointer to the payload struct (e.g., &SpawnChangePayload{})
// Pass nil if the event has no payload
func RegisterType(name string, et EventType, payloadInstance any) {
	nameToType[name] = et
	typeToName[et] = name
	if payloadInstance != nil {
		t := reflect.TypeOf(payloadInstance)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		typeToPayload[et] = t
	}
}

// GetEventType returns the EventType for a given name
func GetEventType(name string) (EventType, bool) {
	// Special case for FSM "Tick"
	if strings.EqualFold(name, "Tick") {
		return 0, true
	}
	et, ok := nameToType[name]
	return et, ok
}

// GetEventName returns the string name for an EventType
func GetEventName(et EventType) string {
	if et == 0 {
		return "Tick"
	}
	return typeToName[et]
}

// NewPayloadStruct returns a new pointer to a zero-value payload struct for the event type
// Returns nil if no payload is registered
func NewPayloadStruct(et EventType) any {
	t, ok := typeToPayload[et]
	if !ok {
		return nil
	}
	return reflect.New(t).Interface()
}

// InitRegistry populates the registry with all game events
// Must be called once at startup
func InitRegistry() {
	if registryInit {
		return
	}
	registryInit = true

	// Register all events from types.go
	RegisterType("EventSoundRequest", EventSoundRequest, &SoundRequestPayload{})
	RegisterType("EventNuggetCollected", EventNuggetCollected, &NuggetCollectedPayload{})
	RegisterType("EventNuggetDestroyed", EventNuggetDestroyed, &NuggetDestroyedPayload{})
	RegisterType("EventNuggetJumpRequest", EventNuggetJumpRequest, nil)
	RegisterType("EventDirectionalCleanerRequest", EventDirectionalCleanerRequest, &DirectionalCleanerPayload{})
	RegisterType("EventManualCleanerTrigger", EventManualCleanerTrigger, nil)
	RegisterType("EventCleanerRequest", EventCleanerRequest, nil)
	RegisterType("EventCleanerFinished", EventCleanerFinished, nil)
	RegisterType("EventGoldEnable", EventGoldEnable, &GoldEnablePayload{})
	RegisterType("EventGoldSpawnRequest", EventGoldSpawnRequest, nil)
	RegisterType("EventGoldSpawnFailed", EventGoldSpawnFailed, nil)
	RegisterType("EventGoldSpawned", EventGoldSpawned, &GoldSpawnedPayload{})
	RegisterType("EventGoldComplete", EventGoldComplete, &GoldCompletionPayload{})
	RegisterType("EventGoldTimeout", EventGoldTimeout, &GoldCompletionPayload{})
	RegisterType("EventGoldDestroyed", EventGoldDestroyed, &GoldCompletionPayload{})
	RegisterType("EventCharacterTyped", EventCharacterTyped, &CharacterTypedPayload{})
	RegisterType("EventSplashRequest", EventSplashRequest, &SplashRequestPayload{})
	RegisterType("EventEnergyAdd", EventEnergyAdd, &EnergyAddPayload{})
	RegisterType("EventEnergySet", EventEnergySet, &EnergySetPayload{})
	RegisterType("EventEnergyBlinkStart", EventEnergyBlinkStart, &EnergyBlinkPayload{})
	RegisterType("EventEnergyBlinkStop", EventEnergyBlinkStop, nil)
	RegisterType("EventHeatAdd", EventHeatAdd, &HeatAddPayload{})
	RegisterType("EventHeatSet", EventHeatSet, &HeatSetPayload{})
	RegisterType("EventShieldActivate", EventShieldActivate, nil)
	RegisterType("EventShieldDeactivate", EventShieldDeactivate, nil)
	RegisterType("EventShieldDrain", EventShieldDrain, &ShieldDrainPayload{})
	RegisterType("EventDeleteRequest", EventDeleteRequest, &DeleteRequestPayload{})
	RegisterType("EventPingGridRequest", EventPingGridRequest, &PingGridRequestPayload{})
	RegisterType("EventGameReset", EventGameReset, nil)
	RegisterType("EventSpawnChange", EventSpawnChange, &SpawnChangePayload{})
	RegisterType("EventDebugRequest", EventDebugRequest, nil)
	RegisterType("EventHelpRequest", EventHelpRequest, nil)
	RegisterType("EventTimerStart", EventTimerStart, &TimerStartPayload{})
	RegisterType("EventBoostActivate", EventBoostActivate, &BoostActivatePayload{})
	RegisterType("EventBoostDeactivate", EventBoostDeactivate, nil)
	RegisterType("EventBoostExtend", EventBoostExtend, &BoostExtendPayload{})
	RegisterType("EventMaterializeRequest", EventMaterializeRequest, &MaterializeRequestPayload{})
	RegisterType("EventMaterializeComplete", EventMaterializeComplete, &SpawnCompletePayload{})
	RegisterType("EventFlashRequest", EventFlashRequest, &FlashRequestPayload{})
	RegisterType("EventDecayStart", EventDecayStart, nil)
	RegisterType("EventDecayCancel", EventDecayCancel, nil)
	RegisterType("EventDecayComplete", EventDecayComplete, nil)
	RegisterType("EventRequestDeath", EventRequestDeath, &DeathRequestPayload{})
}
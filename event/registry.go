package event

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

	// Engine events
	RegisterType("EventWorldClear", EventWorldClear, &WorldClearPayload{})
	RegisterType("EventSystemToggle", EventSystemToggle, &SystemTogglePayload{})
	// Audio events
	RegisterType("EventSoundRequest", EventSoundRequest, &SoundRequestPayload{})
	// Network events
	RegisterType("EventNetworkConnect", EventNetworkConnect, &NetworkConnectPayload{})
	RegisterType("EventNetworkDisconnect", EventNetworkDisconnect, &NetworkDisconnectPayload{})
	RegisterType("EventRemoteInput", EventRemoteInput, &RemoteInputPayload{})
	RegisterType("EventStateSync", EventStateSync, &StateSyncPayload{})
	RegisterType("EventNetworkEvent", EventNetworkEvent, &NetworkEventPayload{})
	RegisterType("EventNetworkError", EventNetworkError, &NetworkErrorPayload{})
	// Game events
	RegisterType("EventNuggetCollected", EventNuggetCollected, &NuggetCollectedPayload{})
	RegisterType("EventNuggetDestroyed", EventNuggetDestroyed, &NuggetDestroyedPayload{})
	RegisterType("EventNuggetJumpRequest", EventNuggetJumpRequest, nil)
	RegisterType("EventCleanerDirectionalRequest", EventCleanerDirectionalRequest, &DirectionalCleanerPayload{})
	RegisterType("EventFireSpecialRequest", EventFireSpecialRequest, nil)
	RegisterType("EventCleanerSweepingRequest", EventCleanerSweepingRequest, nil)
	RegisterType("EventCleanerSweepingFinished", EventCleanerSweepingFinished, nil)
	RegisterType("EventGoldEnable", EventGoldEnable, &GoldEnablePayload{})
	RegisterType("EventGoldSpawnRequest", EventGoldSpawnRequest, nil)
	RegisterType("EventGoldSpawnFailed", EventGoldSpawnFailed, nil)
	RegisterType("EventGoldSpawned", EventGoldSpawned, &GoldSpawnedPayload{})
	RegisterType("EventGoldComplete", EventGoldComplete, &GoldCompletionPayload{})
	RegisterType("EventGoldTimeout", EventGoldTimeout, &GoldCompletionPayload{})
	RegisterType("EventGoldDestroyed", EventGoldDestroyed, &GoldCompletionPayload{})
	RegisterType("EventGoldCancel", EventGoldCancel, nil)
	RegisterType("EventGoldJumpRequest", EventGoldJumpRequest, nil)
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
	RegisterType("EventExplosionRequest", EventExplosionRequest, &ExplosionRequestPayload{})
	RegisterType("EventDustSpawnOne", EventDustSpawnOne, &DustSpawnPayload{})
	RegisterType("EventBlossomSpawnOne", EventBlossomSpawnOne, &BlossomSpawnPayload{})
	RegisterType("EventBlossomWave", EventBlossomWave, nil)
	RegisterType("EventDecaySpawnOne", EventDecaySpawnOne, &DecaySpawnPayload{})
	RegisterType("EventDecayWave", EventDecayWave, nil)
	RegisterType("EventDeathOne", EventDeathOne, nil) // Scalar bit-packed payload (no struct), use api
	RegisterType("EventDeathBatch", EventDeathBatch, &DeathRequestPayload{})
	RegisterType("EventMemberTyped", EventMemberTyped, &MemberTypedPayload{})
	RegisterType("EventCursorMoved", EventCursorMoved, &CursorMovedPayload{})
	// Fuse/Quasar events
	RegisterType("EventFuseDrains", EventFuseDrains, nil)
	RegisterType("EventDrainPause", EventDrainPause, nil)
	RegisterType("EventDrainResume", EventDrainResume, nil)
	RegisterType("EventQuasarSpawned", EventQuasarSpawned, &QuasarSpawnedPayload{})
	RegisterType("EventQuasarDestroyed", EventQuasarDestroyed, nil)
	RegisterType("EventQuasarCancel", EventQuasarCancel, nil)
	RegisterType("EventGrayoutStart", EventGrayoutStart, nil)
	RegisterType("EventGrayoutEnd", EventGrayoutEnd, nil)
	RegisterType("EventSpiritSpawn", EventSpiritSpawn, &SpiritSpawnPayload{})
	RegisterType("EventSpiritDespawn", EventSpiritDespawn, nil)
	RegisterType("EventLightningSpawn", EventLightningSpawn, &LightningSpawnPayload{})
	RegisterType("EventLightningUpdate", EventLightningUpdate, &LightningUpdatePayload{})
	RegisterType("EventLightningDespawn", EventLightningDespawn, nil)
}
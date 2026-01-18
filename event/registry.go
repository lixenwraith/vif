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

	// Audio events
	RegisterType("EventSoundRequest", EventSoundRequest, &SoundRequestPayload{})

	// Network events
	RegisterType("EventNetworkConnect", EventNetworkConnect, &NetworkConnectPayload{})
	RegisterType("EventNetworkDisconnect", EventNetworkDisconnect, &NetworkDisconnectPayload{})
	RegisterType("EventRemoteInput", EventRemoteInput, &RemoteInputPayload{})
	RegisterType("EventStateSync", EventStateSync, &StateSyncPayload{})
	RegisterType("EventNetworkEvent", EventNetworkEvent, &NetworkEventPayload{})
	RegisterType("EventNetworkError", EventNetworkError, &NetworkErrorPayload{})

	// Meta events
	RegisterType("EventGameReset", EventGameReset, nil)
	RegisterType("EventMetaDebugRequest", EventMetaDebugRequest, nil)
	RegisterType("EventMetaHelpRequest", EventMetaHelpRequest, nil)
	RegisterType("EventMetaSystemCommandRequest", EventMetaSystemCommandRequest, &MetaSystemCommandPayload{})

	// --- Game events ---

	// Nugget
	RegisterType("EventNuggetCollected", EventNuggetCollected, &NuggetCollectedPayload{})
	RegisterType("EventNuggetDestroyed", EventNuggetDestroyed, &NuggetDestroyedPayload{})
	RegisterType("EventNuggetJumpRequest", EventNuggetJumpRequest, nil)

	// Cleaner
	RegisterType("EventCleanerDirectionalRequest", EventCleanerDirectionalRequest, &DirectionalCleanerPayload{})
	RegisterType("EventFireSpecialRequest", EventFireSpecialRequest, nil)
	RegisterType("EventCleanerSweepingRequest", EventCleanerSweepingRequest, nil)
	RegisterType("EventCleanerSweepingFinished", EventCleanerSweepingFinished, nil)

	// Gold
	RegisterType("EventGoldSpawnRequest", EventGoldSpawnRequest, nil)
	RegisterType("EventGoldSpawnFailed", EventGoldSpawnFailed, nil)
	RegisterType("EventGoldSpawned", EventGoldSpawned, &GoldSpawnedPayload{})
	RegisterType("EventGoldComplete", EventGoldComplete, &GoldCompletionPayload{})
	RegisterType("EventGoldTimeout", EventGoldTimeout, &GoldCompletionPayload{})
	RegisterType("EventGoldDestroyed", EventGoldDestroyed, &GoldCompletionPayload{})
	RegisterType("EventGoldCancel", EventGoldCancel, nil)
	RegisterType("EventGoldJumpRequest", EventGoldJumpRequest, nil)
	RegisterType("EventCharacterTyped", EventCharacterTyped, &CharacterTypedPayload{})

	// Splash
	RegisterType("EventSplashRequest", EventSplashRequest, &SplashRequestPayload{})
	RegisterType("EventSplashTimerRequest", EventSplashTimerRequest, &SplashTimerRequestPayload{})
	RegisterType("EventSplashTimerCancel", EventSplashTimerCancel, &SplashTimerCancelPayload{})

	// Energy
	RegisterType("EventEnergyAddRequest", EventEnergyAddRequest, &EnergyAddPayload{})
	RegisterType("EventEnergySetRequest", EventEnergySetRequest, &EnergySetPayload{})
	RegisterType("EventEnergyCrossedZeroNotification", EventEnergyCrossedZeroNotification, nil)
	RegisterType("EventEnergyGlyphConsumed", EventEnergyGlyphConsumed, &GlyphConsumedPayload{})
	RegisterType("EventEnergyBlinkStart", EventEnergyBlinkStart, &EnergyBlinkPayload{})
	RegisterType("EventEnergyBlinkStop", EventEnergyBlinkStop, nil)

	// Vampire
	RegisterType("EventVampireDrainRequest", EventVampireDrainRequest, &VampireDrainRequestPayload{})

	// Buff
	RegisterType("EventBuffAddRequest", EventBuffAddRequest, &BuffAddRequestPayload{})
	RegisterType("EventBuffFireRequest", EventBuffFireRequest, nil)

	// Heat
	RegisterType("EventHeatAddRequest", EventHeatAddRequest, &HeatAddRequestPayload{})
	RegisterType("EventHeatSetRequest", EventHeatSetRequest, &HeatSetRequestPayload{})
	RegisterType("EventHeatOverheatNotification", EventHeatOverheatNotification, nil)

	// Shield
	RegisterType("EventShieldActivate", EventShieldActivate, nil)
	RegisterType("EventShieldDeactivate", EventShieldDeactivate, nil)
	RegisterType("EventShieldDrain", EventShieldDrain, &ShieldDrainPayload{})

	// Boost
	RegisterType("EventTimerStart", EventTimerStart, &TimerStartPayload{})
	RegisterType("EventBoostActivate", EventBoostActivate, &BoostActivatePayload{})
	RegisterType("EventBoostDeactivate", EventBoostDeactivate, nil)
	RegisterType("EventBoostExtend", EventBoostExtend, &BoostExtendPayload{})

	// Typing
	RegisterType("EventDeleteRequest", EventDeleteRequest, &DeleteRequestPayload{})

	// Ping
	RegisterType("EventPingGridRequest", EventPingGridRequest, &PingGridRequestPayload{})

	// Materialize
	RegisterType("EventMaterializeRequest", EventMaterializeRequest, &MaterializeRequestPayload{})
	RegisterType("EventMaterializeComplete", EventMaterializeComplete, &SpawnCompletePayload{})

	// Effects
	RegisterType("EventFlashRequest", EventFlashRequest, &FlashRequestPayload{})
	RegisterType("EventExplosionRequest", EventExplosionRequest, &ExplosionRequestPayload{})

	// Dust
	RegisterType("EventDustSpawnOne", EventDustSpawnOne, &DustSpawnPayload{})
	RegisterType("EventDustSpawnBatch", EventDustSpawnBatch, &DustSpawnBatchPayload{})
	RegisterType("EventDustAll", EventDustAll, nil)

	// Blossom
	RegisterType("EventBlossomSpawnOne", EventBlossomSpawnOne, &BlossomSpawnPayload{})
	RegisterType("EventBlossomWave", EventBlossomWave, nil)

	// Decay
	RegisterType("EventDecaySpawnOne", EventDecaySpawnOne, &DecaySpawnPayload{})
	RegisterType("EventDecayWave", EventDecayWave, nil)

	// Death
	RegisterType("EventDeathOne", EventDeathOne, nil) // Scalar bit-packed payload (no struct), use api
	RegisterType("EventDeathBatch", EventDeathBatch, &DeathRequestPayload{})

	// Composite
	RegisterType("EventMemberTyped", EventMemberTyped, &MemberTypedPayload{})

	// Cursor
	RegisterType("EventCursorMoved", EventCursorMoved, &CursorMovedPayload{})

	// Fuse/Quasar events
	RegisterType("EventFuseDrains", EventFuseDrains, nil)

	// Drain
	RegisterType("EventDrainPause", EventDrainPause, nil)
	RegisterType("EventDrainResume", EventDrainResume, nil)

	// Quasar
	RegisterType("EventQuasarSpawned", EventQuasarSpawned, &QuasarSpawnedPayload{})
	RegisterType("EventQuasarDestroyed", EventQuasarDestroyed, nil)
	RegisterType("EventQuasarCancelRequest", EventQuasarCancelRequest, nil)

	// Environment
	RegisterType("EventGrayoutStart", EventGrayoutStart, nil)
	RegisterType("EventGrayoutEnd", EventGrayoutEnd, nil)

	// Spirit
	RegisterType("EventSpiritSpawn", EventSpiritSpawn, &SpiritSpawnRequestPayload{})
	RegisterType("EventSpiritDespawn", EventSpiritDespawn, nil)

	// Lightning
	RegisterType("EventLightningSpawn", EventLightningSpawn, &LightningSpawnRequestPayload{})
	RegisterType("EventLightningUpdate", EventLightningUpdate, &LightningUpdatePayload{})
	RegisterType("EventLightningDespawn", EventLightningDespawn, nil)

	// Enemy
	RegisterType("EventCombatFullKnockbackRequest", EventCombatFullKnockbackRequest, &CombatKnockbackRequestPayload{})
}
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

	// Music events
	RegisterType("EventMusicStart", EventMusicStart, &MusicStartPayload{})
	RegisterType("EventMusicStop", EventMusicStop, nil)
	RegisterType("EventMusicPause", EventMusicPause, nil)
	RegisterType("EventBeatPatternRequest", EventBeatPatternRequest, &BeatPatternRequestPayload{})
	RegisterType("EventMelodyNoteRequest", EventMelodyNoteRequest, &MelodyNoteRequestPayload{})
	RegisterType("EventMelodyPatternRequest", EventMelodyPatternRequest, &MelodyPatternRequestPayload{})
	RegisterType("EventMusicIntensityChange", EventMusicIntensityChange, &MusicIntensityPayload{})
	RegisterType("EventMusicTempoChange", EventMusicTempoChange, &MusicTempoPayload{})

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
	RegisterType("EventMetaAboutRequest", EventMetaAboutRequest, nil)
	RegisterType("EventMetaSystemCommandRequest", EventMetaSystemCommandRequest, &MetaSystemCommandPayload{})
	RegisterType("EventMetaStatusMessageRequest", EventMetaStatusMessageRequest, &MetaStatusMessagePayload{})

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
	RegisterType("EventSplashTimerRequest", EventSplashTimerRequest, &SplashTimerRequestPayload{})
	RegisterType("EventSplashTimerCancel", EventSplashTimerCancel, &SplashTimerCancelPayload{})

	// Energy
	RegisterType("EventEnergyAddRequest", EventEnergyAddRequest, &EnergyAddPayload{})
	RegisterType("EventEnergySetRequest", EventEnergySetRequest, &EnergySetPayload{})
	RegisterType("EventEnergyCrossedZeroNotification", EventEnergyCrossedZeroNotification, nil)
	RegisterType("EventEnergyGlyphConsumed", EventEnergyGlyphConsumed, &GlyphConsumedPayload{})
	RegisterType("EventEnergyBlinkStart", EventEnergyBlinkStart, &EnergyBlinkPayload{})
	RegisterType("EventEnergyBlinkStop", EventEnergyBlinkStop, nil)

	// Weapon
	RegisterType("EventWeaponAddRequest", EventWeaponAddRequest, &WeaponAddRequestPayload{})
	RegisterType("EventWeaponFireRequest", EventWeaponFireRequest, nil)
	RegisterType("EventWeaponFireRequest", EventWeaponFireRequest, nil)

	// Heat
	RegisterType("EventHeatAddRequest", EventHeatAddRequest, &HeatAddRequestPayload{})
	RegisterType("EventHeatSetRequest", EventHeatSetRequest, &HeatSetRequestPayload{})
	RegisterType("EventHeatBurstNotification", EventHeatBurstNotification, nil)

	// Shield
	RegisterType("EventShieldActivate", EventShieldActivate, nil)
	RegisterType("EventShieldDeactivate", EventShieldDeactivate, nil)
	RegisterType("EventShieldDrainRequest", EventShieldDrainRequest, &ShieldDrainRequestPayload{})

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
	RegisterType("EventMaterializeAreaRequest", EventMaterializeAreaRequest, &MaterializeAreaRequestPayload{})

	// Effects
	RegisterType("EventFlashSpawnOneRequest", EventFlashSpawnOneRequest, &FlashRequestPayload{})
	RegisterType("EventFlashBatchRequest", EventFlashSpawnBatchRequest, nil) // Generic BatchPayload, no TOML decode
	RegisterType("EventExplosionRequest", EventExplosionRequest, &ExplosionRequestPayload{})

	// Dust
	RegisterType("EventDustSpawnOneRequest", EventDustSpawnOneRequest, &DustSpawnOneRequestPayload{})
	RegisterType("EventDustSpawnBatchRequest", EventDustSpawnBatchRequest, nil) // Generic BatchPayload, no TOML decode
	RegisterType("EventDustAllRequest", EventDustAllRequest, nil)

	// Blossom
	RegisterType("EventBlossomSpawnOne", EventBlossomSpawnOne, &BlossomSpawnPayload{})
	RegisterType("EventBlossomSpawnBatch", EventBlossomSpawnBatch, nil) // Generic BatchPayload, no TOML decode
	RegisterType("EventBlossomWave", EventBlossomWave, nil)

	// Decay
	RegisterType("EventDecaySpawnOne", EventDecaySpawnOne, &DecaySpawnPayload{})
	RegisterType("EventDecaySpawnBatch", EventDecaySpawnBatch, nil) // Generic BatchPayload, no TOML decode
	RegisterType("EventDecayWave", EventDecayWave, nil)

	// Death
	RegisterType("EventDeathOne", EventDeathOne, nil) // Scalar bit-packed payload (no struct), use api
	RegisterType("EventDeathBatch", EventDeathBatch, &DeathRequestPayload{})

	// Composite
	RegisterType("EventMemberTyped", EventMemberTyped, &MemberTypedPayload{})

	// Cursor
	RegisterType("EventCursorMoved", EventCursorMoved, &CursorMovedPayload{})

	// Fuse events
	RegisterType("EventFuseQuasarRequest", EventFuseQuasarRequest, nil)
	RegisterType("EventFuseSwarmRequest", EventFuseSwarmRequest, &FuseSwarmRequestPayload{})

	// Drain
	RegisterType("EventDrainPause", EventDrainPause, nil)
	RegisterType("EventDrainResume", EventDrainResume, nil)

	// Quasar
	RegisterType("EventQuasarSpawnRequest", EventQuasarSpawnRequest, &QuasarSpawnRequestPayload{})
	RegisterType("EventQuasarSpawned", EventQuasarSpawned, &QuasarSpawnedPayload{})
	RegisterType("EventQuasarDestroyed", EventQuasarDestroyed, nil)
	RegisterType("EventQuasarCancelRequest", EventQuasarCancelRequest, nil)

	// Swarm
	RegisterType("EventSwarmSpawnRequest", EventSwarmSpawnRequest, &SwarmSpawnRequestPayload{})
	RegisterType("EventSwarmSpawned", EventSwarmSpawned, &SwarmSpawnedPayload{})
	RegisterType("EventSwarmDestroyed", EventSwarmDestroyed, &SwarmDestroyedPayload{})
	RegisterType("EventSwarmAbsorbedDrain", EventSwarmAbsorbedDrain, &SwarmAbsorbedDrainPayload{})
	RegisterType("EventSwarmCancelRequest", EventSwarmCancelRequest, nil)

	// Storm
	RegisterType("EventStormSpawnRequest", EventStormSpawnRequest, nil)
	RegisterType("EventStormCancelRequest", EventStormCancelRequest, nil)
	RegisterType("EventStormCircleDestroyed", EventStormCircleDestroyed, &StormCircleDestroyedPayload{})
	RegisterType("EventStormDestroyed", EventStormDestroyed, &StormDestroyedPayload{})

	// Post-Process
	RegisterType("EventGrayoutStart", EventGrayoutStart, nil)
	RegisterType("EventGrayoutEnd", EventGrayoutEnd, nil)
	RegisterType("EventStrobeRequest", EventStrobeRequest, &StrobeRequestPayload{})

	// Level
	RegisterType("EventLevelSetup", EventLevelSetup, &LevelSetupPayload{})
	RegisterType("EventMazeSpawnRequest", EventMazeSpawnRequest, &MazeSpawnRequestPayload{})

	// Spirit
	RegisterType("EventSpiritSpawn", EventSpiritSpawn, &SpiritSpawnRequestPayload{})
	RegisterType("EventSpiritDespawn", EventSpiritDespawn, nil)

	// Lightning
	RegisterType("EventLightningSpawnRequest", EventLightningSpawnRequest, &LightningSpawnRequestPayload{})
	RegisterType("EventLightningUpdate", EventLightningUpdate, &LightningUpdatePayload{})
	RegisterType("EventLightningDespawnRequest", EventLightningDespawnRequest, &LightningDespawnPayload{})

	// Combat
	RegisterType("EventCombatAttackDirectRequest", EventCombatAttackDirectRequest, &CombatAttackDirectRequestPayload{})
	RegisterType("EventCombatAttackAreaRequest", EventCombatAttackAreaRequest, &CombatAttackAreaRequestPayload{})
	RegisterType("EventEnemyCreated", EventEnemyCreated, &EnemyCreatedPayload{})
	RegisterType("EventEnemyKilled", EventEnemyKilled, &EnemyKilledPayload{})
	RegisterType("EventLootSpawnRequest", EventLootSpawnRequest, &LootSpawnRequestPayload{})

	// Missile
	RegisterType("EventMissileSpawnRequest", EventMissileSpawnRequest, &MissileSpawnRequestPayload{})

	// Bullet
	RegisterType("EventBulletSpawnRequest", EventBulletSpawnRequest, &BulletSpawnRequestPayload{})

	// Marker
	RegisterType("EventMarkerSpawnRequest", EventMarkerSpawnRequest, &MarkerSpawnRequestPayload{})

	// Motion Marker
	RegisterType("EventMotionMarkerShowColored", EventMotionMarkerShowColored, &MotionMarkerShowPayload{})
	RegisterType("EventMotionMarkerClearColored", EventMotionMarkerClearColored, nil)

	// Mode
	RegisterType("EventModeChangeNotification", EventModeChangeNotification, &ModeChangeNotificationPayload{})

	// Wall
	RegisterType("EventWallSpawnRequest", EventWallSpawnRequest, &WallSpawnRequestPayload{})
	RegisterType("EventWallCompositeSpawnRequest", EventWallCompositeSpawnRequest, &WallCompositeSpawnRequestPayload{})
	RegisterType("EventWallPatternSpawnRequest", EventWallPatternSpawnRequest, &WallPatternSpawnRequestPayload{})
	RegisterType("EventWallDespawnRequest", EventWallDespawnRequest, &WallDespawnRequestPayload{})
	RegisterType("EventWallMaskChangeRequest", EventWallMaskChangeRequest, &WallMaskChangeRequestPayload{})
	RegisterType("EventWallPushCheckRequest", EventWallPushCheckRequest, nil)
	RegisterType("EventWallSpawned", EventWallSpawned, &WallSpawnedPayload{})
	RegisterType("EventWallDespawnAll", EventWallDespawnAll, nil)

	// Fadeout
	RegisterType("EventFadeoutSpawnOne", EventFadeoutSpawnOne, &FadeoutSpawnPayload{})
	RegisterType("EventFadeoutSpawnBatch", EventFadeoutSpawnBatch, nil) // Generic BatchPayload, no TOML decode

	// Composite Integrity
	RegisterType("EventCompositeIntegrityBreach", EventCompositeIntegrityBreach, &CompositeIntegrityBreachPayload{})
	RegisterType("EventCompositeDestroyRequest", EventCompositeDestroyRequest, &CompositeDestroyRequestPayload{})

	// FSM
	RegisterType("EventCycleDamageMultiplierIncrease", EventCycleDamageMultiplierIncrease, nil)
	RegisterType("EventCycleDamageMultiplierReset", EventCycleDamageMultiplierReset, nil)

	// Pylon
	RegisterType("EventPylonSpawnRequest", EventPylonSpawnRequest, &PylonSpawnRequestPayload{})
	RegisterType("EventPylonSpawned", EventPylonSpawned, &PylonSpawnedPayload{})
	RegisterType("EventPylonDestroyed", EventPylonDestroyed, &PylonDestroyedPayload{})
	RegisterType("EventPylonCancelRequest", EventPylonCancelRequest, nil)

	// Debug
	RegisterType("EventDebugFlowToggle", EventDebugFlowToggle, nil)
}
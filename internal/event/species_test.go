package event

import "testing"

func TestSpeciesLifecycleEventsAreCanonical(t *testing.T) {
	EnsureRegistry()

	for name, want := range map[string]EventType{
		"EventSpeciesCreated": EventSpeciesCreated,
		"EventSpeciesKilled":  EventSpeciesKilled,
	} {
		got, ok := GetEventType(name)
		if !ok || got != want {
			t.Fatalf("GetEventType(%q) = (%v, %t), want (%v, true)", name, got, ok, want)
		}
	}

	if _, ok := NewPayloadStruct(EventSpeciesCreated).(*SpeciesCreatedPayload); !ok {
		t.Fatalf("EventSpeciesCreated payload = %T, want *SpeciesCreatedPayload", NewPayloadStruct(EventSpeciesCreated))
	}
	if _, ok := NewPayloadStruct(EventSpeciesKilled).(*SpeciesKilledPayload); !ok {
		t.Fatalf("EventSpeciesKilled payload = %T, want *SpeciesKilledPayload", NewPayloadStruct(EventSpeciesKilled))
	}
	if _, ok := NewPayloadStruct(EventCombatHealRequest).(*CombatHealRequestPayload); !ok {
		t.Fatalf("EventCombatHealRequest payload = %T, want *CombatHealRequestPayload", NewPayloadStruct(EventCombatHealRequest))
	}

	for _, legacy := range []string{
		"EventEnemyCreated",
		"EventEnemyKilled",
		"EventSwarmAbsorbedDrain",
		"EventQuasarSpawned",
		"EventQuasarDestroyed",
		"EventSwarmSpawned",
		"EventSwarmDestroyed",
		"EventStormCircleDestroyed",
		"EventStormDestroyed",
		"EventEyeSpawned",
		"EventEyeDestroyed",
		"EventPylonSpawned",
		"EventPylonDestroyed",
		"EventSnakeSpawned",
		"EventSnakeDestroyed",
		"EventTowerSpawned",
		"EventTowerDestroyed",
	} {
		if got, ok := GetEventType(legacy); ok {
			t.Fatalf("legacy event %q remains registered as %v", legacy, got)
		}
	}
}

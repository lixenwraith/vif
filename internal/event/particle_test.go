package event

import (
	"reflect"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
)

func TestParticleEventsUseBehaviorSelectedPayloads(t *testing.T) {
	EnsureRegistry()

	for _, tc := range []struct {
		event EventType
		want  any
	}{
		{EventParticleSpawnOne, &ParticleSpawnPayload{}},
		{EventParticleWave, &ParticleWavePayload{}},
	} {
		if got := NewPayloadStruct(tc.event); reflect.TypeOf(got) != reflect.TypeOf(tc.want) {
			t.Errorf("%s payload = %T, want %T", GetEventName(tc.event), got, tc.want)
		}
		if got := ClassOf(tc.event); got != ClassLocal {
			t.Errorf("%s class = %s, want local", GetEventName(tc.event), got)
		}
	}
	if got := NewPayloadStruct(EventParticleSpawnBatch); got != nil {
		t.Errorf("batch registry payload = %T, want nil pooled payload", got)
	}
	for _, retired := range []string{"EventDecaySpawnOne", "EventDecaySpawnBatch", "EventDecayWave", "EventBlossomSpawnOne", "EventBlossomSpawnBatch", "EventBlossomWave"} {
		if _, ok := GetEventType(retired); ok {
			t.Errorf("retired event %q remains registered", retired)
		}
	}
}

func TestEmitParticleDeathCarriesBehavior(t *testing.T) {
	q := NewEventQueue()
	entity := core.MakeEntity(core.DomainPlayer, 7)
	EmitParticleDeath(q, component.ParticleBlossom, entity)

	ev := popBenchmarkEvent(q)
	p, ok := ev.Payload.(*DeathRequestPayload)
	if !ok {
		t.Fatalf("payload type = %T, want *DeathRequestPayload", ev.Payload)
	}
	defer ReleaseDeathRequest(p)
	if ev.Type != EventDeathBatch || ev.Domain != core.DomainPlayer {
		t.Fatalf("event = type %v domain %s, want EventDeathBatch/player", ev.Type, ev.Domain)
	}
	if p.EffectEvent != EventParticleSpawnOne || p.Behavior != component.ParticleBlossom {
		t.Fatalf("particle death = effect %v behavior %v", p.EffectEvent, p.Behavior)
	}
	if len(p.Entities) != 1 || p.Entities[0] != entity {
		t.Fatalf("entities = %v, want [%v]", p.Entities, entity)
	}
}

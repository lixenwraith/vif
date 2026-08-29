package event_test

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
)

// TestWireEncodingBudget records the representative per-tick stream cost.
func TestWireEncodingBudget(t *testing.T) {
	event.EnsureRegistry()
	tests := []struct {
		name   string
		count  int
		typeID event.EventType
		build  func(int) any
		budget int
	}{
		{
			name: "four cursor moves", count: 4, typeID: event.EventCursorMoveRequest, budget: 900,
			build: func(i int) any {
				return &event.CursorMoveRequestPayload{Entity: core.Entity(100 + i), X: 40 + i, Y: 12}
			},
		},
		{
			name: "six resolved shield hits", count: 6, typeID: event.EventCombatAttackAreaCrossingRequest, budget: 2400,
			build: func(i int) any {
				return &event.CombatAttackAreaRequestPayload{
					HitEntities: []core.Entity{core.Entity(200 + i*3), core.Entity(201 + i*3), core.Entity(202 + i*3)},
					AttackType:  component.CombatAttackShield, OwnerEntity: 11, TargetEntity: core.Entity(200 + i*3),
					HasOrigin: true, OriginX: 42, OriginY: 13,
				}
			},
		},
	}

	empty, err := event.EncodeWireBatch(event.WireBatch{Source: 1, ProducedTick: 100})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("empty epoch: %d bytes", len(empty)+network.HeaderSize)
	state, encErr := event.NewWireFrame(event.GameEvent{
		Type: event.EventCursorStateSync, Domain: core.DomainShared,
		Payload: &event.CursorStatePayload{
			Entity: 11, Slot: 0, Seq: 17, Energy: 80, Heat: 35, HitPoints: 100,
			ShieldRadiusX: 7.5, ShieldRadiusY: 3.5, ShieldInvRxSq: 0.017, ShieldInvRySq: 0.082,
			WeaponCharges: []int{0, 2, 1, 0}, WeaponCooldown: []int64{0, 250000000, 0, 0},
		},
	})
	if encErr != "" {
		t.Fatal(encErr)
	}
	stateBody, err := event.EncodeFrames([]event.WireFrame{state})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("one owner-state sync: %d bytes", len(stateBody)+network.HeaderSize)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frames := make([]event.ScheduledWireFrame, 0, tc.count)
			for i := range tc.count {
				frame, encErr := event.NewWireFrame(event.GameEvent{
					Type: tc.typeID, Domain: core.DomainPlayer, Seq: uint64(i + 1), Payload: tc.build(i),
				})
				if encErr != "" {
					t.Fatal(encErr)
				}
				frames = append(frames, event.ScheduledWireFrame{Frame: frame, ApplyTick: 103})
			}
			body, err := event.EncodeWireBatch(event.WireBatch{Source: 1, ProducedTick: 100, Frames: frames})
			if err != nil {
				t.Fatal(err)
			}
			bytes := len(body) + network.HeaderSize
			t.Logf("%s: %d bytes", tc.name, bytes)
			if bytes > tc.budget {
				t.Fatalf("wire size %d exceeds %d-byte budget", bytes, tc.budget)
			}
		})
	}
}

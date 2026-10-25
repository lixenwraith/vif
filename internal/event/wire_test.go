package event_test

import (
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	link "github.com/lixenwraith/vif/pkg/network"
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
			name: "four cursor moves", count: 4, typeID: event.EventCursorMoveRequest, budget: 80,
			build: func(i int) any {
				return &event.CursorMoveRequestPayload{Entity: core.Entity(100 + i), X: 40 + i, Y: 12}
			},
		},
		{
			name: "six resolved shield hits", count: 6, typeID: event.EventCombatAttackAreaCrossingRequest, budget: 220,
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
	t.Logf("empty epoch: %d bytes", len(empty)+link.HeaderSize)
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
	stateBody, err := event.EncodeWireBatch(event.WireBatch{
		Source: 2, Frames: []event.ScheduledWireFrame{{Frame: state, ApplyTick: 101}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A sync carries the cells that are set: this one, with its energy, heat, hit
	// points, shield and two charged weapons, is a third of the 27-cell frame.
	if n := len(stateBody) + link.HeaderSize; n > 120 {
		t.Fatalf("one owner-state sync: %d bytes exceeds the 120-byte budget", n)
	}
	t.Logf("one owner-state sync: %d bytes", len(stateBody)+link.HeaderSize)
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
			bytes := len(body) + link.HeaderSize
			t.Logf("%s: %d bytes", tc.name, bytes)
			if bytes > tc.budget {
				t.Fatalf("wire size %d exceeds %d-byte budget", bytes, tc.budget)
			}
		})
	}
}

// TestAWireBatchRoundTripsItsEnvelope: every field of a batch and of each frame
// comes back as sent, the empty epoch included; a sequence that steps down, an apply
// tick below the produced tick and a negative age ride the deltas.
func TestAWireBatchRoundTripsItsEnvelope(t *testing.T) {
	event.EnsureRegistry()
	for _, want := range []event.WireBatch{
		{Source: 1, ProducedTick: 100},
		{
			Run: 3, ProducedTick: 1 << 40, Source: 17, Hops: 2, Committed: true, Age: -4, Leaf: true,
			Frames: []event.ScheduledWireFrame{
				{Frame: event.WireFrame{Event: event.EventCursorMoveRequest, Domain: core.DomainPlayer, Payload: []byte("x = 1\n"), Seq: 9}, ApplyTick: 1<<40 + 5},
				{Frame: event.WireFrame{Event: event.EventCombatAttackDirectRequest, Domain: core.DomainShared, Seq: 7}, ApplyTick: 1<<40 - 1},
				{Frame: event.WireFrame{Event: event.EventCursorStateSync, Domain: core.DomainShared, Payload: []byte("slot = 1\n")}},
			},
		},
	} {
		body, err := event.EncodeWireBatch(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := event.DecodeWireBatch(body)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("decoded %+v (%v), want %+v", got, err, want)
		}
	}
}

// TestAFrameOutsideTheRegistryIsNotEncoded: an event no registry index names, or a
// domain the enum lacks, is an error at the producer, not a frame a receiver drops.
func TestAFrameOutsideTheRegistryIsNotEncoded(t *testing.T) {
	for _, f := range []event.WireFrame{
		{Event: event.EventNone, Domain: core.DomainShared},
		{Event: event.EventTypeCount, Domain: core.DomainShared},
		{Event: event.EventCursorMoveRequest, Domain: core.DomainCount},
	} {
		if _, err := event.EncodeWireBatch(event.WireBatch{Source: 1, Frames: []event.ScheduledWireFrame{{Frame: f}}}); err == nil {
			t.Errorf("frame %+v encoded", f)
		}
	}
}

// TestAMalformedWireBodyIsRefused: a body the encoder could not have written is an
// error, never a read past its end or an allocation its bytes do not back.
func TestAMalformedWireBodyIsRefused(t *testing.T) {
	event.EnsureRegistry()
	header := func(frames uint64) []byte {
		b := []byte{0, 1, 0, 100, 0, 0} // flags, source, run, produced tick, hops, age
		return binary.AppendUvarint(b, frames)
	}
	frame := func(et uint64, flags byte, payload string) []byte {
		b := binary.AppendUvarint(nil, et)
		b = append(b, flags, 2, 8) // domain flags, sequence delta 1, apply tick delta 4
		b = binary.AppendUvarint(b, uint64(len(payload)))
		return append(b, payload...)
	}
	move := uint64(event.EventCursorMoveRequest)
	whole := append(header(1), frame(move, 1, "x = 1\n")...)
	if _, err := event.DecodeWireBatch(whole); err != nil {
		t.Fatalf("the well-formed body is refused: %v", err)
	}
	for _, c := range []struct {
		name string
		body []byte
	}{
		{"a header cut short", whole[:3]},
		{"a payload cut short", whole[:len(whole)-1]},
		{"a frame count beyond the bytes", append(header(1<<40), frame(move, 1, "")...)},
		{"an event outside the registry", append(header(1), frame(uint64(event.EventTypeCount), 1, "")...)},
		{"the tick sentinel as an event", append(header(1), frame(0, 1, "")...)},
		{"a domain outside the enum", append(header(1), frame(move, byte(core.DomainCount), "")...)},
		{"a reserved frame flag", append(header(1), frame(move, 0x80, "")...)},
	} {
		if _, err := event.DecodeWireBatch(c.body); err == nil {
			t.Errorf("%s was decoded", c.name)
		}
	}
}

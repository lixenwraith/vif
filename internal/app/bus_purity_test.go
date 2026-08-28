package app

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// busEvents lists the types that can carry a crossing. Whether one instance does is
// busCrossing's business: several of these are Stamped, not Bus.
// TODO(phase6): read the declared class from the registry and delete this table.
// EventCombatAttackAreaRequest is absent deliberately: D-5 makes it derived from
// EventExplosionBatchRequest, not transported.
var busEvents = map[event.EventType]bool{
	event.EventCombatAttackDirectRequest: true,
	event.EventExplosionRequest:          true,
	event.EventExplosionBatchRequest:     true,
	event.EventQuasarSpawnRequest:        true,
	event.EventSwarmSpawnRequest:         true,
	event.EventMaterializeAreaRequest:    true,
}

var entityType = reflect.TypeOf(core.Entity(0))

// busCrossing reports whether one instance is the crossing form of its type.
// D-3: an effect on a player target does not cross, so a combat request aimed at a
// player-domain entity is local traffic that may name player entities freely.
// TODO(phase6): the class is per-instance, so a static per-type registry entry
// cannot carry it; the journal filter needs this same predicate.
func busCrossing(ev event.GameEvent) bool {
	switch p := ev.Payload.(type) {
	case *event.CombatAttackDirectRequestPayload:
		// D-5: a chain follow-up is re-derived from the record that produced it
		return p.ChainDepth == 0 && p.TargetEntity.Domain() == core.DomainShared
	}
	return true
}

// TestBusPayloadsNameOnlySharedEntities asserts D-4 over a soak: every entity a
// crossing payload names is shared, so a replicated record means the same thing on
// every instance. The tap runs on the caller's goroutine — a driven App has no
// scheduler — so the accumulators need no synchronization.
func TestBusPayloadsNameOnlySharedEntities(t *testing.T) {
	const seed = 0x4B15
	steps := 1500
	if testing.Short() {
		steps = 300
	}

	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()

	named := 0
	seen := make(map[string]bool)
	var bad []string
	a.SetDispatchTap(func(ev event.GameEvent) {
		if !busEvents[ev.Type] || ev.Payload == nil || !busCrossing(ev) {
			return
		}
		entityScan(reflect.ValueOf(ev.Payload), event.GetEventName(ev.Type), &named, func(msg string) {
			if !seen[msg] {
				seen[msg] = true
				bad = append(bad, msg)
			}
		})
	})

	if _, err := RunScript(a, DefaultScript(seed, steps)); err != nil {
		t.Fatalf("soak: %v", err)
	}
	if named == 0 {
		t.Fatal("no crossing payload named an entity; the soak asserts nothing")
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("D-4 violations (%d entities named):\n  %s", named, strings.Join(bad, "\n  "))
	}
}

// entityScan walks a payload, counting the entities it names and reporting each one
// that is not shared
func entityScan(v reflect.Value, path string, named *int, report func(string)) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			entityScan(v.Elem(), path, named, report)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			entityScan(v.Index(i), fmt.Sprintf("%s[%d]", path, i), named, report)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range v.NumField() {
			entityScan(v.Field(i), path+"."+t.Field(i).Name, named, report)
		}
	default:
		if v.Type() != entityType {
			return
		}
		e := core.Entity(v.Uint())
		if e == 0 {
			return
		}
		*named++
		if e.Domain() != core.DomainShared {
			report(path + " names a " + e.Domain().String() + " entity")
		}
	}
}

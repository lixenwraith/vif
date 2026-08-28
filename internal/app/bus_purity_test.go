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
// crossingTargets' business: several of these are Stamped, not Bus.
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

// targetFields name the receiving side of a payload. The emitter side is asserted
// unconditionally: D-4 reduces a player emitter to HasOrigin/OriginX/Y on every
// instance, crossing or not.
var targetFields = map[string]bool{
	"TargetEntity": true, "HitEntity": true, "HitEntities": true,
}

var entityType = reflect.TypeOf(core.Entity(0))

// crossingTargets reports whether this instance's target fields must be shared.
// D-3: an effect on a player target does not cross, so it names player entities
// freely; D-5: a chain follow-up is re-derived from the record that produced it.
// TODO(phase6): the class is per-instance, so no static registry entry can carry it.
func crossingTargets(ev event.GameEvent) bool {
	switch p := ev.Payload.(type) {
	case *event.CombatAttackDirectRequestPayload:
		return p.ChainDepth == 0 && p.TargetEntity.Domain() == core.DomainShared
	}
	return true
}

// TestBusPayloadsNameOnlySharedEntities asserts D-4 over a soak. The tap runs on the
// caller's goroutine — a driven App has no scheduler — so no synchronization is needed.
func TestBusPayloadsNameOnlySharedEntities(t *testing.T) {
	const seed, steps = 0x4B15, 1500 // This seed produces no crossing inside the old 300-step short horizon.

	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()

	named, crossings := 0, 0
	seen := make(map[string]bool)
	var bad []string
	a.SetDispatchTap(func(ev event.GameEvent) {
		if !busEvents[ev.Type] || ev.Payload == nil {
			return
		}
		crossing := crossingTargets(ev)
		if crossing {
			crossings++
		}
		entityScan(reflect.ValueOf(ev.Payload), event.GetEventName(ev.Type), "",
			crossing, &named, func(msg string) {
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
		t.Fatal("no crossing-capable payload named an entity; the soak asserts nothing")
	}
	t.Logf("inspected %d entity references, %d of them on a crossing", named, crossings)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("D-4 violations:\n  %s", strings.Join(bad, "\n  "))
	}
}

// entityScan walks a payload, counting the entities it names and reporting each one
// that is not shared. Target fields are skipped when the instance is not a crossing.
func entityScan(v reflect.Value, path, field string, crossing bool, named *int, report func(string)) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			entityScan(v.Elem(), path, field, crossing, named, report)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			entityScan(v.Index(i), fmt.Sprintf("%s[%d]", path, i), field, crossing, named, report)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range v.NumField() {
			name := t.Field(i).Name
			if !crossing && targetFields[name] {
				continue
			}
			entityScan(v.Field(i), path+"."+name, name, crossing, named, report)
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

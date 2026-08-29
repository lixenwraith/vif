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

// targetFields name the receiving side of a payload. The emitter side is asserted
// unconditionally: D-4 reduces a player emitter to HasOrigin/OriginX/Y on every
// instance, crossing or not.
var targetFields = map[string]bool{
	"TargetEntity": true, "HitEntity": true, "HitEntities": true,
}

var entityType = reflect.TypeOf(core.Entity(0))

// TestBusPayloadsNameOnlySharedEntities asserts D-4 over a soak: a record that
// replicates names only shared entities. The transported set comes from the class
// table, so this is the Phase 6 exit criterion rather than a hand-list — a Stamped
// type resolves through the domain its producer stamped, which for a combat hit is
// the target's own domain. A record that does not replicate constrains nothing and
// is skipped whole; its player entities are this instance's business.
//
// The tap runs on the caller's goroutine — a driven App has no scheduler — so no
// synchronization is needed.
func TestBusPayloadsNameOnlySharedEntities(t *testing.T) {
	const seed, steps = 0x4B15, 1500 // This seed produces no crossing inside the old 300-step short horizon.

	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()

	named, crossings := 0, 0
	seen := make(map[string]bool)
	var bad []string
	a.SetDispatchTap(func(ev event.GameEvent) {
		if ev.Payload == nil || !event.Replicated(ev.Type, ev.Domain) {
			return
		}
		crossings++
		entityScan(reflect.ValueOf(ev.Payload), event.GetEventName(ev.Type), "",
			true, &named, func(msg string) {
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
		t.Fatal("no replicated payload named an entity; the soak asserts nothing")
	}
	t.Logf("inspected %d entity references across %d replicated records", named, crossings)
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

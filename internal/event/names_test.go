package event_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/pkg/audio/model"
)

// TestEveryEnumAPayloadHoldsTakesNames: a field of a defined integer type, at any
// depth of any payload, takes its type's names, as one decoding only by number is
// retargeted by a reordered enum; identifiers and quantities stay numbers.
func TestEveryEnumAPayloadHoldsTakesNames(t *testing.T) {
	numbers := []reflect.Type{reflect.TypeFor[core.Entity](), reflect.TypeFor[time.Duration](),
		reflect.TypeFor[model.SoundID](), reflect.TypeFor[model.PatternID]()}
	named := map[string]bool{}
	for _, l := range event.PayloadNames(component.Declared{}).List() {
		named[l.Type] = true
	}
	seen := map[reflect.Type]bool{}
	var walk func(where string, ft reflect.Type)
	walk = func(where string, ft reflect.Type) {
		if seen[ft] {
			return
		}
		seen[ft] = true
		switch ft.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			walk(where, ft.Elem())
		case reflect.Struct:
			for f := range ft.Fields() {
				if f.IsExported() && f.Tag.Get("toml") != "-" {
					walk(where+"."+f.Name, f.Type)
				}
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if ft.PkgPath() != "" && !named[ft.String()] && !slices.Contains(numbers, ft) {
				t.Errorf("%s: %s takes no names", where, ft)
			}
		}
	}
	event.RangeEvents(func(name string, _ event.EventType, payload any) {
		if payload != nil {
			walk(name, reflect.TypeOf(payload))
		}
	})
}

// TestASpeciesFieldTakesAScenariosPrograms: a scenario's programs name the data
// species from SpeciesDataFirst in sorted-name order, whatever order they come in,
// and the code's names, which every scenario shares, gain none of them.
func TestASpeciesFieldTakesAScenariosPrograms(t *testing.T) {
	names := event.PayloadNames(component.Declared{Programs: []string{"zeta", "alpha"}})
	for name, want := range map[string]component.SpeciesType{
		"eye": component.SpeciesEye, "alpha": component.SpeciesDataFirst, "zeta": component.SpeciesDataFirst + 1,
	} {
		if got, ok := names.Value[component.SpeciesType](name); !ok || got != want {
			t.Errorf("%s = %d, %t; want %d", name, got, ok, want)
		}
	}
	if _, ok := event.PayloadNames(component.Declared{}).Value[component.SpeciesType]("alpha"); ok {
		t.Error("the code's names took a scenario's program")
	}
}

// TestALootFieldTakesAScenariosKinds: a scenario's loot kinds name the kinds from
// LootCount in sorted-name order, whatever order they come in, beside the code's,
// which every scenario shares and which gain none of them.
func TestALootFieldTakesAScenariosKinds(t *testing.T) {
	names := event.PayloadNames(component.Declared{Loot: []string{"zeta", "alpha"}})
	for name, want := range map[string]component.LootType{
		"gun": component.LootGun, "alpha": component.LootCount, "zeta": component.LootCount + 1,
	} {
		if got, ok := names.Value[component.LootType](name); !ok || got != want {
			t.Errorf("%s = %d, %t; want %d", name, got, ok, want)
		}
	}
	if _, ok := event.PayloadNames(component.Declared{}).Value[component.LootType]("alpha"); ok {
		t.Error("the code's names took a scenario's kind")
	}
}

package event_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

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
	for _, l := range event.PayloadNames(nil).List() {
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

package snapshot

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
)

// filledWorld is a world with every store holding n rows of random leaves, the
// first store's rows at the given entities.
func filledWorld(t *testing.T, n int, seed uint64) engine.SharedWorldState {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, seed^0x9E3779B9))
	var s engine.SharedWorldState
	world := reflect.ValueOf(&s).Elem()
	for i := range world.NumField() {
		f := world.Field(i)
		if f.Kind() != reflect.Slice {
			continue
		}
		f.Set(reflect.MakeSlice(f.Type(), n, n))
		e := uint64(0)
		for k := range n {
			e += 1 + uint64(r.IntN(3)) // gaps, so the entity deltas are not all ones
			f.Index(k).Field(0).SetUint(e)
			fillLeaves(f.Index(k).Field(1), r)
		}
	}
	return s
}

// TestEveryStoreRoundTripsThroughItsColumns: each store's rows, every leaf set at
// random, lay out in columns and read back to the same JSON, nil slices as nil and
// empty ones as empty; a body cut short, run past, or claiming more rows than it
// has bytes is refused without the allocation it asks for.
func TestEveryStoreRoundTripsThroughItsColumns(t *testing.T) {
	s := filledWorld(t, 7, 11)
	for i, name := range engine.SharedWorldStoreNames {
		plain := engine.SharedWorldEncodeStoreColumns(nil, &s, i)
		var back engine.SharedWorldState
		if err := engine.SharedWorldDecodeStoreColumns(&back, i, plain); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := reflect.ValueOf(s).Field(i + 3).Interface()
		got := reflect.ValueOf(back).Field(i + 3).Interface()
		if string(mustJSON(got)) != string(mustJSON(want)) {
			t.Fatalf("%s: the rows read back differ:\n got %s\nwant %s", name, mustJSON(got), mustJSON(want))
		}
		for _, bad := range [][]byte{plain[:len(plain)-1], append(append([]byte(nil), plain...), 0)} {
			if err := engine.SharedWorldDecodeStoreColumns(&back, i, bad); err == nil {
				t.Fatalf("%s: a damaged body decoded", name)
			}
		}
	}
	// A row count past the bytes: ten rows claimed of a few bytes.
	if err := engine.SharedWorldDecodeStoreColumns(new(engine.SharedWorldState), 0, []byte{10, 1, 2}); err == nil ||
		!strings.Contains(err.Error(), "rows claimed") {
		t.Fatalf("a body claiming more rows than bytes decoded: %v", err)
	}
	if err := engine.SharedWorldDecodeStoreColumns(new(engine.SharedWorldState), engine.SharedWorldStoreCount, nil); err == nil {
		t.Fatal("a store past the last decoded")
	}
}

// TestASliceFieldKeepsItsNilness: a nil slice travels as nil and an empty one as
// empty, since the generated comparisons tell them apart.
func TestASliceFieldKeepsItsNilness(t *testing.T) {
	s := filledWorld(t, 3, 5)
	world := reflect.ValueOf(&s).Elem()
	emptied, nilled := 0, 0
	for i := range world.NumField() {
		f := world.Field(i)
		if f.Kind() != reflect.Slice || f.Len() == 0 {
			continue
		}
		v := f.Index(0).Field(1)
		for k := range v.NumField() {
			if sf := v.Field(k); sf.Kind() == reflect.Slice && sf.CanSet() {
				sf.Set(reflect.MakeSlice(sf.Type(), 0, 0))
				emptied++
			}
		}
		v = f.Index(1).Field(1)
		for k := range v.NumField() {
			if sf := v.Field(k); sf.Kind() == reflect.Slice && sf.CanSet() {
				sf.Set(reflect.Zero(sf.Type()))
				nilled++
			}
		}
	}
	if emptied == 0 || nilled == 0 {
		t.Skip("no component carries a slice")
	}
	for i := range engine.SharedWorldStoreNames {
		plain := engine.SharedWorldEncodeStoreColumns(nil, &s, i)
		var back engine.SharedWorldState
		if err := engine.SharedWorldDecodeStoreColumns(&back, i, plain); err != nil {
			t.Fatal(err)
		}
		want, got := reflect.ValueOf(s).Field(i+3), reflect.ValueOf(back).Field(i+3)
		for k := range want.Len() {
			wv, gv := want.Index(k).Field(1), got.Index(k).Field(1)
			for j := range wv.NumField() {
				if wf := wv.Field(j); wf.Kind() == reflect.Slice && wf.IsNil() != gv.Field(j).IsNil() {
					t.Fatalf("%s row %d field %s: nil %v read back as nil %v",
						engine.SharedWorldStoreNames[i], k, wv.Type().Field(j).Name, wf.IsNil(), gv.Field(j).IsNil())
				}
			}
		}
	}
}

// TestADeltaTravelsInColumnsAndIsToldFromAKeyframe: a delta's changed rows, removed
// entities and order, nil and not, travel in the delta part and read back equal;
// DecodeCorrection names the shape, and DecodeCapture refuses a delta.
func TestADeltaTravelsInColumnsAndIsToldFromAKeyframe(t *testing.T) {
	base := SharedCapture{World: filledWorld(t, 6, 21)}
	base.Header.Tick = 40
	next := base
	next.Header.Tick = 44
	next.World = filledWorld(t, 6, 21)
	next.World.Positions = append(next.World.Positions[:5:5], engine.StoreEntry[component.PositionComponent]{Entity: 99, Value: component.PositionComponent{X: 3, Y: 4}})
	next.World.Positions[0].Value.X++
	next.World.Glyph = next.World.Glyph[1:]
	d := DiffCapture(base, next)
	if len(d.World.Positions.Changed) != 2 || len(d.World.Positions.Removed) != 1 || d.World.Glyph.Order != nil || len(d.World.Glyph.Removed) != 1 {
		t.Fatalf("the diff is not the one the test wants: %+v %+v", d.World.Positions, d.World.Glyph)
	}
	d.World.Positions.Order = []core.Entity{} // explicit and empty is not nil

	body, err := EncodeCorrectionDelta(d)
	if err != nil {
		t.Fatal(err)
	}
	kind, _, back, err := DecodeCorrection(body)
	if err != nil || kind != CorrectionDelta {
		t.Fatalf("a delta decoded as kind %d: %v", kind, err)
	}
	if string(mustJSON(back)) != string(mustJSON(d)) {
		t.Fatalf("the delta read back differs:\n got %s\nwant %s", mustJSON(back), mustJSON(d))
	}
	if back.World.Positions.Order == nil || back.World.Glyph.Order != nil {
		t.Fatal("the order's nil-ness did not travel")
	}
	if _, err := DecodeCapture(body); err == nil {
		t.Fatal("a delta decoded as a capture")
	}

	key, err := new(Encoder).EncodeCapture(next)
	if err != nil {
		t.Fatal(err)
	}
	if kind, cap, _, err := DecodeCorrection(key); err != nil || kind != CorrectionKeyframe || cap.Header.Tick != 44 {
		t.Fatalf("a keyframe decoded as kind %d, tick %d: %v", kind, cap.Header.Tick, err)
	}
	if _, _, _, err := DecodeCorrection(body[:len(body)-2]); err == nil {
		t.Fatal("a cut delta decoded")
	}
}

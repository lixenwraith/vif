package snapshot

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestEncoderReuseMatchesAFreshEncoding: whatever moved in a capture, an encoder that
// reuses its encodings indexes, seals and renders it as a fresh one does, renders
// json.Marshal's bytes, and leaves the indexes it returned before as they were. Every
// exported leaf of every store moves once, a float to positive and negative zero.
func TestEncoderReuseMatchesAFreshEncoding(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var cap SharedCapture
	world := reflect.ValueOf(&cap.World).Elem()
	for i := range world.NumField() {
		f := world.Field(i)
		if f.Kind() != reflect.Slice {
			continue
		}
		n := 3
		if world.Type().Field(i).Name == "Wall" {
			n = parameter.SnapshotManifestPageRows + 1 // one removal crosses a page boundary
		}
		f.Set(reflect.MakeSlice(f.Type(), n, n))
		for k := range n {
			f.Index(k).Field(0).SetUint(uint64(k + 1)) // one entity across stores, as a cursor's cells are
			fillLeaves(f.Index(k).Field(1), r)
		}
	}

	var enc Encoder
	var held []*Manifest
	var expected []*Manifest
	check := func(what string) {
		t.Helper()
		got, err := enc.Build(cap, 1)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		want, err := BuildManifest(cap, 1)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if !reflect.DeepEqual(got.summary, want.summary) || !reflect.DeepEqual(got.sections, want.sections) {
			t.Fatalf("%s: the reused index differs from a fresh one", what)
		}
		sum, err := enc.Integrity(cap)
		if fresh, _ := Integrity(cap); err != nil || sum != fresh {
			t.Fatalf("%s: the reused seal differs from a fresh one", what)
		}
		body, err := enc.EncodeCapture(cap)
		if err != nil {
			t.Fatalf("%s: encode: %v", what, err)
		}
		back, err := DecodeCapture(body)
		if got, _ := json.Marshal(back); err != nil || string(got) != string(mustJSON(cap)) {
			t.Fatalf("%s: the capture decoded from its sections differs from the one encoded (%v)", what, err)
		}
		held, expected = append(held, got), append(expected, want)
	}
	check("first")
	check("unchanged")
	for i := range world.NumField() {
		f := world.Field(i)
		if f.Kind() != reflect.Slice {
			continue
		}
		name := world.Type().Field(i).Name
		moveLeaves(f.Index(0).Field(1), name, func(leaf string) { check(leaf) })
	}

	walls := cap.World.Wall
	cap.World.Wall = slices.Clone(walls[1:])
	check("wall removed")
	cap.World.Wall = append(cap.World.Wall, engine.StoreEntry[component.WallComponent]{Entity: 9999})
	check("wall added")
	slices.Reverse(cap.World.Wall)
	check("walls reordered")
	cap.World.Wall = nil
	check("walls gone")
	cap.World.Wall = []engine.StoreEntry[component.WallComponent]{}
	check("walls empty")
	for i := range held {
		if !reflect.DeepEqual(held[i].sections, expected[i].sections) {
			t.Fatalf("a later build changed retained index %d", i)
		}
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// fillLeaves sets every exported leaf of v from r.
func fillLeaves(v reflect.Value, r *rand.Rand) {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(r.IntN(2) == 1)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(r.Int64N(100))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(r.Uint64N(100))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(r.Float64())
	case reflect.String:
		v.SetString(strconv.Itoa(r.IntN(100)))
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(r.Int64N(1e9), 0).UTC()))
			return
		}
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				fillLeaves(v.Field(i), r)
			}
		}
	case reflect.Array:
		for i := range v.Len() {
			fillLeaves(v.Index(i), r)
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := range v.Len() {
			fillLeaves(v.Index(i), r)
		}
	}
}

// moveLeaves changes every exported leaf of v in place, calling moved after each.
func moveLeaves(v reflect.Value, path string, moved func(string)) {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(v.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(0)
		moved(path + " to zero")
		v.SetFloat(math.Copysign(0, -1))
	case reflect.String:
		v.SetString(v.String() + "x")
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(v.Interface().(time.Time).Add(time.Second)))
			break
		}
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				moveLeaves(v.Field(i), path+"."+v.Type().Field(i).Name, moved)
			}
		}
		return
	case reflect.Array:
		for i := range v.Len() {
			moveLeaves(v.Index(i), path+"["+strconv.Itoa(i)+"]", moved)
		}
		return
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		moved(path + " to empty")
		v.Set(reflect.Zero(v.Type()))
	default:
		return
	}
	moved(path)
}

// TestHashesAreDomainSeparated pins the construction the proofs rest on: the same
// bytes hashed as a page, as a section and as a root are three different values,
// and a page's content under another page's identity is a fourth.
func TestHashesAreDomainSeparated(t *testing.T) {
	rows := []ManifestRow{{Name: "a", Value: json.RawMessage(`1`)}}
	page := pageHash("w.glyph", 0, rows)
	otherPage := pageHash("w.glyph", 1, rows)
	otherSection := pageHash("w.wall", 0, rows)
	section := sectionHash("w.glyph", []uint64{page})
	root := manifestRoot(CaptureHeader{Schema: Schema}, 1,
		[]SectionSummary{{ID: "w.glyph", Hash: section, Pages: 1, Rows: 1}})

	seen := map[uint64]string{}
	for name, v := range map[string]uint64{
		"page": page, "page 1": otherPage, "other section's page": otherSection,
		"section": section, "root": root,
	} {
		if prev, dup := seen[v]; dup {
			t.Fatalf("%s and %s hash to the same value", prev, name)
		}
		seen[v] = name
	}

	// A section's hash covers its pages in order, so swapping two page hashes
	// changes it.
	if sectionHash("s", []uint64{1, 2}) == sectionHash("s", []uint64{2, 1}) {
		t.Fatal("a section hash does not commit to its page order")
	}
}

// TestPagesStayBounded pins the partition: a section's page count is a function of
// its row count, capped by the protocol rather than by the world.
func TestPagesStayBounded(t *testing.T) {
	for _, rows := range []int{0, 1, parameter.SnapshotManifestPageRows,
		parameter.SnapshotManifestPageRows*parameter.SnapshotManifestMaxPages*4 + 1} {
		if n := pageCount(rows); n < 1 || n > parameter.SnapshotManifestMaxPages {
			t.Fatalf("%d rows partition into %d pages", rows, n)
		}
	}
	if pageCount(parameter.SnapshotManifestPageRows*4) <= pageCount(parameter.SnapshotManifestPageRows) {
		t.Fatal("the partition does not grow with the row count")
	}
}

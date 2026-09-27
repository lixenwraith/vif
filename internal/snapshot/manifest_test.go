package snapshot

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
)

func TestWallReuseMatchesEveryFreshManifest(t *testing.T) {
	// Value equality must imply identical JSON; floats and aliases break that rule.
	var exactType func(reflect.Type)
	exactType = func(typ reflect.Type) {
		switch typ.Kind() {
		case reflect.Struct:
			for i := range typ.NumField() {
				exactType(typ.Field(i).Type)
			}
		case reflect.Array:
			exactType(typ.Elem())
		case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64:
		default:
			t.Fatalf("wall reuse needs a byte-exact comparison for %v", typ)
		}
	}
	exactType(reflect.TypeFor[component.WallComponent]())

	w := engine.NewWorld()
	var entities []core.Entity
	w.RunSafe(func() {
		for i := range parameter.SnapshotManifestPageRows + 1 {
			e := w.CreateEntity(core.DomainShared)
			entities = append(entities, e)
			w.Components.Wall.SetComponent(e, component.WallComponent{Rune: rune(i + 32)})
		}
	})
	var builder ManifestBuilder
	var cap SharedCapture
	var held []*Manifest
	var expected []*Manifest
	check := func() {
		t.Helper()
		got, err := builder.Build(cap, 1)
		if err != nil {
			t.Fatal(err)
		}
		want, err := BuildManifest(cap, 1)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("reused index differs from a fresh index: %v", err)
		}
		held, expected = append(held, got), append(expected, want)
		for i := range held {
			if !reflect.DeepEqual(held[i], expected[i]) {
				t.Fatalf("building a later index changed retained index %d", i)
			}
		}
	}
	capture := func() {
		w.RunSafe(func() { cap.World = w.CaptureSharedWorld() })
		check()
	}
	capture()
	check()
	cap.Header.Tick++
	cap.Header.Run++
	cap.Header.Term++
	check()
	cap.World.Wall[0].Value.RenderBg = true
	check() // Mutating the caller's capture cannot mutate the cached comparison.
	w.RunSafe(func() {
		wall, _ := w.Components.Wall.GetPtr(entities[0])
		wall.BlockMask = component.WallBlockAll
	})
	capture()
	w.RunSafe(func() {
		w.Components.Wall.Each(func(_ core.Entity, wall *component.WallComponent) bool {
			wall.RenderFg = true
			return true
		})
	})
	capture()
	w.RunSafe(func() { w.Components.Wall.RemoveEntity(entities[0]) })
	capture() // Cross a page-count boundary.
	w.RunSafe(func() { w.Components.Wall.SetComponent(entities[0], component.WallComponent{Rune: 'x'}) })
	capture()
	slices.Reverse(cap.World.Wall)
	check()
	cap.World.Wall = nil
	check()
	cap.World.Wall = []engine.StoreEntry[component.WallComponent]{}
	check()
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

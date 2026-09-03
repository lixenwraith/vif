package snapshot

import (
	"encoding/json"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

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

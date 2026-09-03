package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// The manifest suite works on captures rather than on sessions, deliberately.
//
// A session test proves the protocol converges; it cannot easily prove *what was
// not sent*, and "what was not sent" is the whole of Phase 6's claim. These drive
// the index, the descent and the repair directly, so a mismatch can be injected in
// one named cell and the shard set that answers it can be counted.

// manifestFixture is a capture of a warmed world, and the index over it.
func manifestFixture(t *testing.T) (SharedCapture, *captureManifest) {
	t.Helper()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)
	a.Tick(60)
	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	index, err := buildManifest(cap, 1)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return cap, index
}

// cloneCapture round-trips a capture through the wire so a test can mutate one
// copy without touching the other's slices.
func cloneCapture(t *testing.T, cap SharedCapture) SharedCapture {
	t.Helper()
	body, err := EncodeCapture(cap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeCapture(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// TestEqualRootsProduceOnlyHashTraffic is requirement 1: two instances holding the
// same state exchange the index and nothing else.
func TestEqualRootsProduceOnlyHashTraffic(t *testing.T) {
	cap, host := manifestFixture(t)
	guest, err := buildManifest(cloneCapture(t, cap), 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	if host.Root() != guest.Root() {
		t.Fatalf("two indexes over one capture produced roots %d and %d", host.Root(), guest.Root())
	}

	req, sections, pages := compareRequest(guest, host.Summary())
	if !req.Converged() {
		t.Fatalf("equal roots produced a request for %d sections", len(req.Sections))
	}
	if pages != 0 {
		t.Fatalf("equal roots hashed %d pages; the descent should have stopped at the root", pages)
	}
	if sections == 0 {
		t.Fatal("the comparison examined no sections at all")
	}

	set, repaired, err := buildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}
	if repaired != 0 || len(set.Shards) != 0 {
		t.Fatalf("a converged request produced %d shards", len(set.Shards))
	}

	// The compact half is what actually travels, and it has to be materially
	// smaller than the correction it replaces or the exchange is not worth a round
	// trip. The capture here is a quiet world; the storm figure is reported by
	// TestSelectiveCorrectionCostAtTheStormHighWater.
	manifestBody, err := EncodeManifest(host.Summary())
	if err != nil {
		t.Fatalf("manifest encode: %v", err)
	}
	captureBody, err := EncodeCapture(cap)
	if err != nil {
		t.Fatalf("capture encode: %v", err)
	}
	if len(manifestBody) >= len(captureBody) {
		t.Fatalf("the index is %d bytes against a %d-byte capture", len(manifestBody), len(captureBody))
	}
	t.Logf("quiet world: index %d bytes, capture %d bytes, %d sections",
		len(manifestBody), len(captureBody), len(host.Summary().Sections))
}

// TestOneMismatchRepairsOnlyItsPage is requirement 2: an injected disagreement in
// one component cell moves one page, and the repair restores the exact root.
func TestOneMismatchRepairsOnlyItsPage(t *testing.T) {
	cap, host := manifestFixture(t)
	mine := cloneCapture(t, cap)
	if len(mine.World.Glyph) == 0 {
		t.Fatal("the fixture world holds no glyph to perturb")
	}
	mine.World.Glyph[0].Value.Rune = 'Z' + 1

	guest, err := buildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	if guest.Root() == host.Root() {
		t.Fatal("a changed glyph did not move the root")
	}

	req, _, _ := compareRequest(guest, host.Summary())
	if len(req.Sections) != 1 || req.Sections[0].ID != storeSectionPrefix+"glyph" {
		t.Fatalf("the descent asked for %v, want only the glyph store", requestedSections(req))
	}

	set, repaired, err := buildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}
	if repaired != 1 || len(set.Shards) != 1 {
		t.Fatalf("one changed cell moved %d pages", repaired)
	}
	if err := validateShardSet(set, cap.Header.Tick, 1, set.Root, cap.Header); err != nil {
		t.Fatalf("validate: %v", err)
	}
	rep, err := applyShardSet(&mine, guest, set)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rep.Pages != 1 || rep.Sections != 1 {
		t.Fatalf("the repair touched %d pages in %d sections", rep.Pages, rep.Sections)
	}
	if guest.Root() != host.Root() {
		t.Fatal("the repaired capture does not reproduce the authority's root")
	}
}

// TestSeveralSectionsRepairWithoutAnUnrelatedOne is requirement 3.
func TestSeveralSectionsRepairWithoutAnUnrelatedOne(t *testing.T) {
	cap, host := manifestFixture(t)
	mine := cloneCapture(t, cap)
	if len(mine.World.Glyph) == 0 || len(mine.Status.Ints) == 0 {
		t.Fatal("the fixture world is too small to perturb two sections")
	}
	mine.World.Glyph[0].Value.Rune = 'Z' + 1
	mine.Status.Ints[0].Value += 7
	mine.World.NextEntity += 3

	guest, err := buildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	req, _, _ := compareRequest(guest, host.Summary())
	got := requestedSections(req)
	want := map[string]bool{storeSectionPrefix + "glyph": true, sectionStatus: true, sectionMeta: true}
	if len(got) != len(want) {
		t.Fatalf("the descent asked for %v, want exactly %v", got, keysOf(want))
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("the descent asked for unrelated section %q", id)
		}
	}

	set, _, err := buildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}
	for _, sh := range set.Shards {
		if !want[sh.Section] {
			t.Fatalf("the repair carried unrelated section %q", sh.Section)
		}
	}
	if err := validateShardSet(set, cap.Header.Tick, 1, set.Root, cap.Header); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if _, err := applyShardSet(&mine, guest, set); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if guest.Root() != host.Root() {
		t.Fatal("the repaired capture does not reproduce the authority's root")
	}
}

// TestTheIndexHoldsNoPlayerDomainOrOwnerAuthoredState is requirement 4's first
// half: the hashed surface carries no player-domain entity at all, and no
// owner-authored cell of a cursor another participant drives.
func TestTheIndexHoldsNoPlayerDomainOrOwnerAuthoredState(t *testing.T) {
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(40)

	// A second cursor, owned by someone else: its owner-authored cells are the set
	// a receiver keeps and no repair may carry.
	a.Context().PushEventOrigin(event.EventCursorSpawnRequest,
		&event.CursorSpawnRequestPayload{
			Slot: 1, X: 20, Y: 10,
			Control: uint8(component.ControlRemote), PeerID: 2,
		}, event.OriginDebug)
	a.Settle()
	remote := spawnRemoteCursor(t, a, 1, 2)

	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	index, err := buildManifest(cap, 1)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}

	for _, id := range []string{storeSectionPrefix + "energy", storeSectionPrefix + "heat",
		storeSectionPrefix + "weapon", storeSectionPrefix + "combat"} {
		sec, ok := index.section(id)
		if !ok {
			t.Fatalf("the index holds no section %q", id)
		}
		for _, row := range sec.rows {
			if row.Entity == remote {
				t.Fatalf("section %q hashes the owner-authored cell of cursor %d", id, uint64(remote))
			}
			if row.Entity.Domain() != core.DomainShared {
				t.Fatalf("section %q hashes player-domain entity %d", id, uint64(row.Entity))
			}
		}
	}
	// Player-domain entities never appear anywhere in the index, which is a
	// property of the capture rather than of the exclusion above — asserted here so
	// a capture that started carrying them would fail at the index as well.
	for _, id := range index.orderedSectionIDs() {
		sec, _ := index.section(id)
		for _, row := range sec.rows {
			if row.Entity != 0 && row.Entity.Domain() != core.DomainShared {
				t.Fatalf("section %q hashes entity %d, which is not shared", id, uint64(row.Entity))
			}
		}
	}
}

// TestReorderedRowsFailTheProof is requirement 5. The page hash commits to the
// canonical order, so a shard whose rows have been shuffled reproduces neither its
// declared hash nor the sender's page.
func TestReorderedRowsFailTheProof(t *testing.T) {
	cap, host := manifestFixture(t)
	mine := cloneCapture(t, cap)
	mine.World.Glyph[0].Value.Rune = 'Z' + 1
	guest, err := buildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	req, _, _ := compareRequest(guest, host.Summary())
	set, _, err := buildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}

	var target int = -1
	for i, sh := range set.Shards {
		if len(sh.Rows) >= 2 {
			target = i
			break
		}
	}
	if target < 0 {
		t.Skip("the injected mismatch produced no page with two rows to swap")
	}
	rows := set.Shards[target].Rows
	rows[0], rows[1] = rows[1], rows[0]

	if err := validateShardSet(set, cap.Header.Tick, 1, set.Root, cap.Header); err == nil {
		t.Fatal("a shard with reordered rows passed its integrity proof")
	}
	// And nothing was written: the receiver's capture is what it was.
	if guest.Root() == host.Root() {
		t.Fatal("the refused shard was applied anyway")
	}
}

// TestMalformedShardSetsAreRefusedAtomically is requirement 6. Each case is a
// separate refusal reason, and none of them may reach the splice.
func TestMalformedShardSetsAreRefusedAtomically(t *testing.T) {
	cap, host := manifestFixture(t)
	mine := cloneCapture(t, cap)
	mine.World.Glyph[0].Value.Rune = 'Z' + 1
	guest, err := buildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	req, _, _ := compareRequest(guest, host.Summary())
	good, _, err := buildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}
	if len(good.Shards) == 0 {
		t.Fatal("the fixture produced no shard to corrupt")
	}

	for _, tc := range []struct {
		name   string
		mutate func(s *CorrectionShardSet)
		want   string
	}{
		{"unknown version", func(s *CorrectionShardSet) { s.Version = ManifestVersion + 1 }, "version"},
		{"unknown schema", func(s *CorrectionShardSet) { s.Schema = SnapshotSchema + 1 }, "schema"},
		{"stale baseline", func(s *CorrectionShardSet) { s.Header.Tick-- }, "tick"},
		{"foreign session", func(s *CorrectionShardSet) { s.Header.Session++ }, "another run"},
		{"foreign crossing fence", func(s *CorrectionShardSet) { s.Header.AuthorityCrossingSeq++ }, "header"},
		{"another authority", func(s *CorrectionShardSet) { s.Authority = 9 }, "authority"},
		{"corrupt content", func(s *CorrectionShardSet) {
			s.Shards[0].Rows = append([]ManifestRow(nil), s.Shards[0].Rows...)
			s.Shards[0].Rows[0].Value = json.RawMessage(`{"corrupt":true}`)
		}, "page hash"},
		{"duplicate page", func(s *CorrectionShardSet) { s.Shards = append(s.Shards, s.Shards[0]) }, "repeats"},
		{"duplicate conflicting page", func(s *CorrectionShardSet) {
			conflict := s.Shards[0]
			conflict.Hash++
			s.Shards = append(s.Shards, conflict)
		}, "different content"},
		{"page outside its partition", func(s *CorrectionShardSet) {
			s.Shards[0].Page = s.Shards[0].Pages
		}, "page"},
		{"partition disagrees with its section", func(s *CorrectionShardSet) {
			s.Shards[0].Pages++
		}, "partitions into"},
		{"unsummarised section", func(s *CorrectionShardSet) { s.Shards[0].Section = "w.nowhere" }, "does not summarise"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := cloneShardSet(t, good)
			tc.mutate(&set)
			err := validateShardSet(set, cap.Header.Tick, 1, set.Root, cap.Header)
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s refused with %q, want a reason naming %q", tc.name, err, tc.want)
			}
		})
	}

	// Oversize is refused by the sender rather than by the validator: past the
	// bound a repair is a capture with per-page overhead, and the keyframe the
	// caller falls back to is both smaller and self-sufficient.
	body, err := EncodeShardSet(good)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(body) > parameter.SnapshotShardBytesMax {
		t.Fatalf("a one-page repair is %d bytes, past the %d-byte bound",
			len(body), parameter.SnapshotShardBytesMax)
	}
}

// TestOneSetIsOneBaseline is requirement 7's protocol half: a repair carries its
// own baseline and root, so two of them cannot be combined and a newer one
// replaces an older rather than merging with it.
func TestOneSetIsOneBaseline(t *testing.T) {
	cap, host := manifestFixture(t)
	mine := cloneCapture(t, cap)
	mine.World.Glyph[0].Value.Rune = 'Z' + 1
	guest, err := buildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	req, _, _ := compareRequest(guest, host.Summary())
	set, _, err := buildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}

	// A set answering another baseline is refused before anything is spliced, and
	// the receiver's capture is unchanged.
	before := guest.Root()
	if err := validateShardSet(set, cap.Header.Tick+1, 1, set.Root, cap.Header); err == nil {
		t.Fatal("a repair naming another baseline was accepted")
	}
	if guest.Root() != before {
		t.Fatal("a refused repair changed the receiver's index")
	}

	// And a set whose section summary does not produce the root it declares is
	// refused, which is what stops a repair assembled from two baselines.
	mixed := cloneShardSet(t, set)
	mixed.Sections[0].Hash++
	if err := validateShardSet(mixed, cap.Header.Tick, 1, mixed.Root, cap.Header); err == nil {
		t.Fatal("a repair whose summary does not produce its root was accepted")
	}
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
	root := manifestRoot(CaptureHeader{Schema: SnapshotSchema}, 1,
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

// requestedSections names the sections a descent asked about.
func requestedSections(req CorrectionRequest) []string {
	out := make([]string, 0, len(req.Sections))
	for _, s := range req.Sections {
		out = append(out, s.ID)
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// cloneShardSet copies a repair through the wire so a case can corrupt one field
// without disturbing the next case's copy.
func cloneShardSet(t *testing.T, set CorrectionShardSet) CorrectionShardSet {
	t.Helper()
	body, err := EncodeShardSet(set)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeShardSet(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// orderedSectionIDs lists the index's sections in the order the root absorbs them.
func (m *captureManifest) orderedSectionIDs() []string {
	out := make([]string, 0, len(m.summary.Sections))
	for _, s := range m.summary.Sections {
		out = append(out, s.ID)
	}
	return out
}

// spawnRemoteCursor gives the fixture a cursor another participant drives, which is
// what the D-13 exclusion is about.
func spawnRemoteCursor(t *testing.T, a *App, slot uint8, peer uint32) core.Entity {
	t.Helper()
	var e core.Entity
	a.World().RunSafe(func() {
		w := a.World()
		e = w.Resources.Player.Slot(slot)
		if e == 0 {
			return
		}
		if c, ok := w.Components.Cursor.GetPtr(e); ok {
			c.Control, c.PeerID = component.ControlRemote, peer
		}
		w.Components.Energy.SetComponent(e, component.EnergyComponent{})
		w.Components.Heat.SetComponent(e, component.HeatComponent{})
		w.Components.Weapon.SetComponent(e, component.WeaponComponent{})
		w.Components.Combat.SetComponent(e, component.CombatComponent{OwnerEntity: e})
	})
	if e == 0 {
		t.Fatalf("slot %d holds no cursor", slot)
	}
	return e
}

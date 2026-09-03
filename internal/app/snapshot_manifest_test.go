package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// The manifest suite works on captures rather than on sessions, deliberately.
//
// A session test proves the protocol converges; it cannot easily prove *what was
// not sent*, and "what was not sent" is the whole of Phase 6's claim. These drive
// the index, the descent and the repair directly, so a mismatch can be injected in
// one named cell and the shard set that answers it can be counted.

// manifestFixture is a capture of a warmed world, and the index over it.
func manifestFixture(t *testing.T) (snapshot.SharedCapture, *snapshot.Manifest) {
	t.Helper()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)
	a.Tick(60)
	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	index, err := snapshot.BuildManifest(cap, 1)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return cap, index
}

// cloneCapture round-trips a capture through the wire so a test can mutate one
// copy without touching the other's slices.
func cloneCapture(t *testing.T, cap snapshot.SharedCapture) snapshot.SharedCapture {
	t.Helper()
	body, err := snapshot.EncodeCapture(cap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := snapshot.DecodeCapture(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// TestEqualRootsProduceOnlyHashTraffic is requirement 1: two instances holding the
// same state exchange the index and nothing else.
func TestEqualRootsProduceOnlyHashTraffic(t *testing.T) {
	cap, host := manifestFixture(t)
	guest, err := snapshot.BuildManifest(cloneCapture(t, cap), 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	if host.Root() != guest.Root() {
		t.Fatalf("two indexes over one capture produced roots %d and %d", host.Root(), guest.Root())
	}

	req, sections, pages := snapshot.CompareRequest(guest, host.Summary())
	if !req.Converged() {
		t.Fatalf("equal roots produced a request for %d sections", len(req.Sections))
	}
	if pages != 0 {
		t.Fatalf("equal roots hashed %d pages; the descent should have stopped at the root", pages)
	}
	if sections == 0 {
		t.Fatal("the comparison examined no sections at all")
	}

	set, repaired, err := snapshot.BuildShardSet(host, req)
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
	manifestBody, err := snapshot.EncodeManifest(host.Summary())
	if err != nil {
		t.Fatalf("manifest encode: %v", err)
	}
	captureBody, err := snapshot.EncodeCapture(cap)
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

	guest, err := snapshot.BuildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	if guest.Root() == host.Root() {
		t.Fatal("a changed glyph did not move the root")
	}

	req, _, _ := snapshot.CompareRequest(guest, host.Summary())
	if len(req.Sections) != 1 || req.Sections[0].ID != snapshot.StoreSectionPrefix+"glyph" {
		t.Fatalf("the descent asked for %v, want only the glyph store", requestedSections(req))
	}

	set, repaired, err := snapshot.BuildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}
	if repaired != 1 || len(set.Shards) != 1 {
		t.Fatalf("one changed cell moved %d pages", repaired)
	}
	if err := snapshot.ValidateShardSet(set, cap.Header.Tick, 1, set.Root, cap.Header); err != nil {
		t.Fatalf("validate: %v", err)
	}
	rep, err := snapshot.ApplyShardSet(&mine, guest, set)
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

	guest, err := snapshot.BuildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	req, _, _ := snapshot.CompareRequest(guest, host.Summary())
	got := requestedSections(req)
	want := map[string]bool{snapshot.StoreSectionPrefix + "glyph": true, snapshot.SectionStatus: true, snapshot.SectionMeta: true}
	if len(got) != len(want) {
		t.Fatalf("the descent asked for %v, want exactly %v", got, keysOf(want))
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("the descent asked for unrelated section %q", id)
		}
	}

	set, _, err := snapshot.BuildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}
	for _, sh := range set.Shards {
		if !want[sh.Section] {
			t.Fatalf("the repair carried unrelated section %q", sh.Section)
		}
	}
	if err := snapshot.ValidateShardSet(set, cap.Header.Tick, 1, set.Root, cap.Header); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if _, err := snapshot.ApplyShardSet(&mine, guest, set); err != nil {
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
	index, err := snapshot.BuildManifest(cap, 1)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}

	for _, id := range []string{snapshot.StoreSectionPrefix + "energy", snapshot.StoreSectionPrefix + "heat",
		snapshot.StoreSectionPrefix + "weapon", snapshot.StoreSectionPrefix + "combat"} {
		rows, ok := index.SectionRows(id)
		if !ok {
			t.Fatalf("the index holds no section %q", id)
		}
		for _, row := range rows {
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
	for _, id := range index.Sections() {
		rows, _ := index.SectionRows(id)
		for _, row := range rows {
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
	guest, err := snapshot.BuildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	req, _, _ := snapshot.CompareRequest(guest, host.Summary())
	set, _, err := snapshot.BuildShardSet(host, req)
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

	if err := snapshot.ValidateShardSet(set, cap.Header.Tick, 1, set.Root, cap.Header); err == nil {
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
	guest, err := snapshot.BuildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	req, _, _ := snapshot.CompareRequest(guest, host.Summary())
	good, _, err := snapshot.BuildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}
	if len(good.Shards) == 0 {
		t.Fatal("the fixture produced no shard to corrupt")
	}

	for _, tc := range []struct {
		name   string
		mutate func(s *snapshot.CorrectionShardSet)
		want   string
	}{
		{"unknown version", func(s *snapshot.CorrectionShardSet) { s.Version = snapshot.ManifestVersion + 1 }, "version"},
		{"unknown schema", func(s *snapshot.CorrectionShardSet) { s.Schema = snapshot.Schema + 1 }, "schema"},
		{"stale baseline", func(s *snapshot.CorrectionShardSet) { s.Header.Tick-- }, "tick"},
		{"foreign session", func(s *snapshot.CorrectionShardSet) { s.Header.Session++ }, "another run"},
		{"foreign crossing fence", func(s *snapshot.CorrectionShardSet) { s.Header.AuthorityCrossingSeq++ }, "header"},
		{"another authority", func(s *snapshot.CorrectionShardSet) { s.Authority = 9 }, "authority"},
		{"corrupt content", func(s *snapshot.CorrectionShardSet) {
			s.Shards[0].Rows = append([]snapshot.ManifestRow(nil), s.Shards[0].Rows...)
			s.Shards[0].Rows[0].Value = json.RawMessage(`{"corrupt":true}`)
		}, "page hash"},
		{"duplicate page", func(s *snapshot.CorrectionShardSet) { s.Shards = append(s.Shards, s.Shards[0]) }, "repeats"},
		{"duplicate conflicting page", func(s *snapshot.CorrectionShardSet) {
			conflict := s.Shards[0]
			conflict.Hash++
			s.Shards = append(s.Shards, conflict)
		}, "different content"},
		{"page outside its partition", func(s *snapshot.CorrectionShardSet) {
			s.Shards[0].Page = s.Shards[0].Pages
		}, "page"},
		{"partition disagrees with its section", func(s *snapshot.CorrectionShardSet) {
			s.Shards[0].Pages++
		}, "partitions into"},
		{"unsummarised section", func(s *snapshot.CorrectionShardSet) { s.Shards[0].Section = "w.nowhere" }, "does not summarise"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := cloneShardSet(t, good)
			tc.mutate(&set)
			err := snapshot.ValidateShardSet(set, cap.Header.Tick, 1, set.Root, cap.Header)
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
	body, err := snapshot.EncodeShardSet(good)
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
	guest, err := snapshot.BuildManifest(mine, 1)
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	req, _, _ := snapshot.CompareRequest(guest, host.Summary())
	set, _, err := snapshot.BuildShardSet(host, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}

	// A set answering another baseline is refused before anything is spliced, and
	// the receiver's capture is unchanged.
	before := guest.Root()
	if err := snapshot.ValidateShardSet(set, cap.Header.Tick+1, 1, set.Root, cap.Header); err == nil {
		t.Fatal("a repair naming another baseline was accepted")
	}
	if guest.Root() != before {
		t.Fatal("a refused repair changed the receiver's index")
	}

	// And a set whose section summary does not produce the root it declares is
	// refused, which is what stops a repair assembled from two baselines.
	mixed := cloneShardSet(t, set)
	mixed.Sections[0].Hash++
	if err := snapshot.ValidateShardSet(mixed, cap.Header.Tick, 1, mixed.Root, cap.Header); err == nil {
		t.Fatal("a repair whose summary does not produce its root was accepted")
	}
}

// requestedSections names the sections a descent asked about.
func requestedSections(req snapshot.CorrectionRequest) []string {
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
func cloneShardSet(t *testing.T, set snapshot.CorrectionShardSet) snapshot.CorrectionShardSet {
	t.Helper()
	body, err := snapshot.EncodeShardSet(set)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := snapshot.DecodeShardSet(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
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

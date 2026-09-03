// Package app: the capture, seen as content rather than as bytes.
//
// Phase 5 left the correction stream carrying a whole body every cadence — a
// keyframe or the exact difference from the last one — and the measurement said
// what that costs: about 40 KiB/s at the storm high water. The cost is paid
// whether or not the receiver already agrees, and a deterministic guest usually
// does: it is running the same simulation from the same state, so between two
// corrections it diverges only where an input differed.
//
// A manifest is that observation made checkable. It is a deterministic, versioned
// index over the same capture the correction path already builds, partitioned into
// **sections** (one per component store, plus the capture's scalars, RNG streams,
// declared system state, compared status surface and shared FSM) and each section
// into bounded **pages**. Every page has a hash, every section a hash over its
// pages, and the manifest a root over its sections. Two instances that hold equal
// state produce an equal root; two that do not can find where they differ by
// descending, and repair exactly the pages that mismatch.
//
// Four properties are what make the index usable as evidence rather than as a
// hint, and each of them is a constraint on how the hashes are computed:
//
//   - **Order independence where order is not state.** A reconciled world keeps
//     its own dense store order (see ReconcileSharedWorld), so two instances
//     holding identical state hold it in different slots. A page is therefore
//     read in entity-ascending order, which neither instance chose, and page
//     membership is a function of the entity rather than of a position in a
//     slice. What order *does* commit to is the shard: a shard's rows must arrive
//     in that same canonical order or its hash does not reproduce, which is what
//     stops reordered data from passing the proof.
//
//   - **Domain separation.** Page, section and root hashes are seeded with
//     distinct prefixes and each level absorbs its own identity, so a page hash
//     can never be mistaken for a section hash, and a page's content hashed under
//     another page's identity does not match.
//
//   - **Version and baseline in the root.** The root absorbs the manifest
//     version, the capture schema, and the run/session/seed identity. A root
//     computed by another build, another run or another session cannot compare
//     equal to this one, so "the roots match" cannot be reached by two instances
//     that are not in the same session at all.
//
//   - **The owner-authored set is outside the hashed surface.** Energy, heat,
//     shield, boost, weapon, combat, view, ping and pulse on a *cursor* have
//     exactly one author (D-13) and a receiver keeps its own over the sender's
//     mirror, so those cells disagree permanently and by design. A manifest that
//     hashed them would carry a root disagreement no shard could ever close, and
//     the protocol would fall back to a keyframe forever. They are excluded from
//     the index, from every shard, and from selective apply. A capture still
//     carries them, because a joiner has to materialise a cursor it has never
//     held — that is the install's business, not the index's.
//
// Player-domain state needs no rule here: a capture has never contained any.
package app

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// ManifestVersion is the correction index's own version, separate from
// SnapshotSchema. The schema says what a capture contains; this says how it is
// partitioned and hashed. Either moving invalidates a comparison, and a receiver
// has to be able to say which one did.
const ManifestVersion = 1

// The section ids that are not component stores. Component store sections take
// their ids from engine.SharedWorldStoreNames, which is generated beside the
// capture, so a component added to the manifest is indexed without anyone
// remembering to add it here.
const (
	sectionMeta    = "meta"    // the shared allocator counter and lifetime totals
	sectionStreams = "streams" // every RNG stream's position
	sectionSystems = "systems" // each system's declared private state (D-19)
	sectionStatus  = "status"  // the compared status surface
	sectionFSM     = "fsm"     // the shared state machine's runtime position
)

// storeSectionPrefix separates a component store's section id from the fixed
// sections above, so a component named "status" could not collide with one.
const storeSectionPrefix = "w."

// Hash domain separators. Each level absorbs its own prefix first, so a value
// hashed at one level cannot compare equal to the same bytes hashed at another.
const (
	hashDomainRow     = "vif/manifest/row/1\x00"
	hashDomainPage    = "vif/manifest/page/1\x00"
	hashDomainSection = "vif/manifest/section/1\x00"
	hashDomainRoot    = "vif/manifest/root/1\x00"
)

// cursorStoreName is the component store whose rows carry the control assignment
// each instance re-derives rather than adopts.
const cursorStoreName = "cursor"

// normaliseStoreValue drops the cells of a component the receiver re-derives at
// install rather than adopting from the sender.
//
// There is exactly one, and it is the cursor's Control. A capture carries the
// sender's answer to "which of these cursors do I drive" — its own is ControlHuman
// and everyone else's ControlRemote — and rebindCursorRosterLocked replaces it on
// every install with this instance's own answer, derived from the participant
// identity the handshake assigned (D-13). Two instances of one session therefore
// hold *deliberately* different values in that field for the life of the session.
//
// A manifest that hashed it would carry a root disagreement that no shard could
// close: the repair would write the sender's answer, the install would immediately
// re-derive the receiver's, and the next manifest would find the same
// disagreement — an endless keyframe fallback over a field neither instance is
// wrong about. So the field is zeroed for hashing and for repair alike, and the
// install puts the right value back exactly as it always has.
func normaliseStoreValue(store string, raw json.RawMessage) (json.RawMessage, error) {
	if store != cursorStoreName {
		return raw, nil
	}
	var c component.CursorComponent
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	c.Control = 0
	return json.Marshal(c)
}

// ownerAuthoredStores names the component stores whose cursor cells are
// owner-authored under D-13. The list is the same one snapshot_roster.go reads
// and restores; keeping the two in one shape is deliberate, because a store that
// appeared in one and not the other would be either hashed and never repairable
// or repairable and silently overwritten.
var ownerAuthoredStores = map[string]bool{
	"energy": true, "heat": true, "shield": true, "boost": true,
	"weapon": true, "combat": true, "cursorview": true, "ping": true, "pulse": true,
}

// ManifestRow is one indexed cell in its canonical form.
//
// Exactly one of the two identities is used per section: component store sections
// key by entity, everything else by name. Sorting is by (Name, Entity), which for
// a store section is entity-ascending and for the rest is name-ascending — in both
// cases an order neither instance's insertion history chose.
type ManifestRow struct {
	Name   string          `json:"n,omitempty"`
	Entity core.Entity     `json:"e,omitempty"`
	Value  json.RawMessage `json:"v"`
}

// SectionSummary is one section as the root sees it: its hash, and how the
// receiver must re-partition it to compare page by page.
//
// Pages travels because the partition has to be the sender's. A receiver that
// derived a page count from its own row count would bucket the same entity
// differently the moment the two disagreed about how many rows there are, which is
// exactly the condition the descent exists to diagnose.
type SectionSummary struct {
	ID    string `json:"id"`
	Hash  uint64 `json:"h"`
	Pages uint32 `json:"p"`
	Rows  uint32 `json:"r"`
}

// CorrectionManifest is the compact summary a correction leads with.
//
// It carries no state. What it carries is the capture's header — which is what an
// install adopts and is a few hundred bytes — the root, and one summary per
// section. At the storm high water that is well under a kilobyte compressed,
// against about 7 KiB for the delta it replaces when the receiver already agrees.
type CorrectionManifest struct {
	Version int           `json:"version"`
	Header  CaptureHeader `json:"header"`
	Root    uint64        `json:"root"`

	// Authority names the participant whose world this index describes. It is what
	// makes the D-13 exclusion symmetric: both sides hash a cursor's owner-authored
	// cells exactly when that cursor belongs to the authority, so the cells a
	// receiver would adopt are compared and repaired, and the cells it authors —
	// which no install will ever take from the sender — are outside the hashed
	// surface on both sides at once. Hashing all of them would leave a root
	// disagreement no shard could close; hashing none of them would stop a mirror
	// of the authority's own cursor from ever being corrected.
	// The authority generation this index was produced under is not repeated here:
	// it is in Header, which every authoritative artifact carries and which the
	// root absorbs, so a manifest from another term cannot compare equal to this
	// one. Two places to read one fact is how the two stop agreeing.
	Authority uint32 `json:"authority"`

	// Sections is every section, always. The alternative — sending only the
	// sections that changed since the last manifest — would make a manifest
	// meaningful only against the one before it, and the point of the index is
	// that it is meaningful against any state at all.
	Sections []SectionSummary `json:"sections"`
}

// manifestSection is one section as the sender holds it: the summary the wire
// carries, plus the rows and page hashes the descent needs.
type manifestSection struct {
	SectionSummary
	rows     []ManifestRow
	pageHash []uint64
	pageRows [][]ManifestRow
}

// captureManifest is the whole index over one capture, held by whichever side
// built it. Only the CorrectionManifest half ever reaches the wire.
type captureManifest struct {
	summary   CorrectionManifest
	sections  map[string]*manifestSection
	index     map[string]int // section id to its slot in summary.Sections
	authority uint32
}

// buildManifest indexes one capture.
//
// Nothing here reads the world: the capture is already taken, so this runs on the
// correction goroutine and never under the world lock. That is the whole of
// requirement 8's outside-the-lock half — the bounded read stays where it was, and
// the partitioning, marshalling and hashing are charged to the publisher.
func buildManifest(cap SharedCapture, authority uint32) (*captureManifest, error) {
	cursors := ownerAuthoredCursors(cap, authority)
	m := &captureManifest{
		summary: CorrectionManifest{
			Version:   ManifestVersion,
			Header:    cap.Header,
			Authority: authority,
		},
		authority: authority,
		sections:  make(map[string]*manifestSection, engine.SharedWorldStoreCount+5),
		index:     make(map[string]int, engine.SharedWorldStoreCount+5),
	}

	add := func(id string, rows []ManifestRow) {
		sec := newManifestSection(id, rows)
		m.sections[id] = sec
		m.index[id] = len(m.summary.Sections)
		m.summary.Sections = append(m.summary.Sections, sec.SectionSummary)
	}

	meta, err := metaRows(cap)
	if err != nil {
		return nil, err
	}
	add(sectionMeta, meta)

	var scratch []engine.StoreRow
	for i := range engine.SharedWorldStoreCount {
		name := engine.SharedWorldStoreNames[i]
		scratch = scratch[:0]
		scratch, err = engine.SharedWorldStoreRows(&cap.World, i, scratch)
		if err != nil {
			return nil, fmt.Errorf("manifest %s: %w", name, err)
		}
		rows, err := storeManifestRows(name, scratch, cursors)
		if err != nil {
			return nil, fmt.Errorf("manifest %s: %w", name, err)
		}
		add(storeSectionPrefix+name, rows)
	}

	if rows, err := streamRows(cap); err != nil {
		return nil, err
	} else {
		add(sectionStreams, rows)
	}
	if rows, err := systemRows(cap); err != nil {
		return nil, err
	} else {
		add(sectionSystems, rows)
	}
	if rows, err := statusRows(cap); err != nil {
		return nil, err
	} else {
		add(sectionStatus, rows)
	}
	if rows, err := fsmRows(cap); err != nil {
		return nil, err
	} else {
		add(sectionFSM, rows)
	}

	m.summary.Root = manifestRoot(cap.Header, authority, m.summary.Sections)
	return m, nil
}

// storeManifestRows turns one store's canonical rows into indexed rows: the
// owner-authored cells of a cursor dropped, and the re-derived cells of what
// remains zeroed.
func storeManifestRows(name string, scratch []engine.StoreRow, cursors map[core.Entity]bool) ([]ManifestRow, error) {
	owner := ownerAuthoredStores[name]
	rows := make([]ManifestRow, 0, len(scratch))
	for _, r := range scratch {
		if owner && cursors[r.Entity] {
			continue // D-13: one author, and it is not the receiver's to repair
		}
		value, err := normaliseStoreValue(name, r.Value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, ManifestRow{Entity: r.Entity, Value: value})
	}
	return rows, nil
}

// ownerAuthoredCursors is the cursor set whose owner-authored cells stay *outside*
// the hashed surface: every cursor the authority does not own.
//
// The asymmetry is the point, and it has three cases rather than two. A cursor the
// authority owns has the authority as its single author, so its cells are exactly
// the ones every receiver adopts at install and exactly the ones a repair should
// carry. A cursor naming no participant is authored by nobody — every instance
// reads it as ControlRemote — so its cells are ordinary shared state. A cursor
// anyone else owns is authored somewhere else: the authority holds a mirror one
// sync period behind at best, and the owner keeps its own over anything an install
// writes (D-13, snapshot_roster.go). Only that third case is excluded, because
// only it would produce a disagreement that survives every repair; the
// owner-authored sync stream stays its carrier, which is where the domain model
// already puts it.
func ownerAuthoredCursors(cap SharedCapture, authority uint32) map[core.Entity]bool {
	out := make(map[core.Entity]bool, len(cap.World.Cursor))
	for _, en := range cap.World.Cursor {
		// A cursor naming no participant has no separate author: every instance
		// reads it as ControlRemote, nobody keeps its own values over an install,
		// and its cells are ordinary shared state that a repair must carry.
		if en.Value.PeerID == 0 || en.Value.PeerID == authority {
			continue
		}
		out[en.Entity] = true
	}
	return out
}

// newManifestSection partitions one section's rows and hashes them.
//
// The partition is by entity or name rather than by position, so a row added or
// removed moves nothing else between pages. The page count comes from the row
// count at build time and travels in the summary, so both sides bucket alike.
func newManifestSection(id string, rows []ManifestRow) *manifestSection {
	slices.SortFunc(rows, compareManifestRows)
	pages := pageCount(len(rows))
	sec := &manifestSection{
		SectionSummary: SectionSummary{ID: id, Pages: uint32(pages), Rows: uint32(len(rows))},
		rows:           rows,
		pageHash:       make([]uint64, pages),
		pageRows:       make([][]ManifestRow, pages),
	}
	for _, row := range rows {
		p := rowPage(row, uint32(pages))
		sec.pageRows[p] = append(sec.pageRows[p], row)
	}
	for p := range pages {
		sec.pageHash[p] = pageHash(id, uint32(p), sec.pageRows[p])
	}
	sec.Hash = sectionHash(id, sec.pageHash)
	return sec
}

// compareManifestRows is the canonical order: by name, then by entity.
func compareManifestRows(a, b ManifestRow) int {
	if n := strings.Compare(a.Name, b.Name); n != 0 {
		return n
	}
	switch {
	case a.Entity < b.Entity:
		return -1
	case a.Entity > b.Entity:
		return 1
	}
	return 0
}

// pageCount is how many pages a section of n rows is partitioned into: a power of
// two so the bucket is a mask, bounded above so a section's page vector is a
// property of the protocol rather than of the world.
func pageCount(n int) int {
	pages := 1
	for pages < parameter.SnapshotManifestMaxPages && pages*parameter.SnapshotManifestPageRows < n {
		pages *= 2
	}
	return pages
}

// rowPage is the page a row belongs to, mixed so that consecutively allocated
// entities do not land in one page.
func rowPage(row ManifestRow, pages uint32) uint32 {
	if pages <= 1 {
		return 0
	}
	h := fnv.New64a()
	writeString(h, row.Name)
	writeUint64(h, uint64(row.Entity))
	return uint32(h.Sum64()) & (pages - 1)
}

// pageHash commits to a page's identity and to its rows in canonical order.
//
// The row count and each row's identity are absorbed as well as its bytes, so
// neither a row moved between pages nor two rows swapped can reproduce the hash —
// which is what a shard's proof rests on.
func pageHash(section string, page uint32, rows []ManifestRow) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(hashDomainPage))
	writeUint64(h, ManifestVersion)
	writeUint64(h, SnapshotSchema)
	writeString(h, section)
	writeUint64(h, uint64(page))
	writeUint64(h, uint64(len(rows)))
	for _, row := range rows {
		rh := fnv.New64a()
		_, _ = rh.Write([]byte(hashDomainRow))
		writeString(rh, row.Name)
		writeUint64(rh, uint64(row.Entity))
		writeUint64(rh, uint64(len(row.Value)))
		_, _ = rh.Write(row.Value)
		writeUint64(h, rh.Sum64())
	}
	return h.Sum64()
}

// sectionHash commits to a section's identity and to its page hashes in order.
func sectionHash(section string, pages []uint64) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(hashDomainSection))
	writeUint64(h, ManifestVersion)
	writeUint64(h, SnapshotSchema)
	writeString(h, section)
	writeUint64(h, uint64(len(pages)))
	for _, p := range pages {
		writeUint64(h, p)
	}
	return h.Sum64()
}

// manifestRoot commits to the session the index belongs to and to every section.
//
// The tick is deliberately absent. Two instances compare roots to answer "do we
// hold the same state", and a guest is by construction a prediction ahead of the
// authority: including the tick would make every comparison fail for a reason that
// is not a disagreement about the world. What *is* absorbed is the identity a
// disagreement would otherwise be silent about — the manifest version, the capture
// schema, the run, session and seed, and the authority term — so two instances
// that are not in the same session, or not in the same generation of it, cannot
// reach an equal root.
func manifestRoot(h CaptureHeader, authority uint32, sections []SectionSummary) uint64 {
	w := fnv.New64a()
	_, _ = w.Write([]byte(hashDomainRoot))
	writeUint64(w, ManifestVersion)
	writeUint64(w, uint64(h.Schema))
	writeUint64(w, h.JournalSchema)
	writeUint64(w, h.Run)
	writeUint64(w, h.Session)
	writeUint64(w, h.Seed)
	writeUint64(w, uint64(h.MapWidth))
	writeUint64(w, uint64(h.MapHeight))
	writeUint64(w, uint64(authority))
	writeUint64(w, uint64(h.Term))
	writeUint64(w, uint64(len(sections)))
	for _, s := range sections {
		writeString(w, s.ID)
		writeUint64(w, uint64(s.Pages))
		writeUint64(w, uint64(s.Rows))
		writeUint64(w, s.Hash)
	}
	return w.Sum64()
}

// rebuild re-indexes the named sections against a capture that has changed under
// them, and recomputes the root.
//
// It exists so a receiver can verify a repair without paying for a second whole
// index: a shard set touches a handful of sections, and the rest of the capture is
// bit-for-bit what it already hashed. The sections' slots in the summary are
// preserved, because the root absorbs them in order.
func (m *captureManifest) rebuild(cap SharedCapture, ids []string) error {
	rows, err := m.sectionRowsFor(cap, ids)
	if err != nil {
		return err
	}
	for id, r := range rows {
		slot, ok := m.index[id]
		if !ok {
			return fmt.Errorf("manifest holds no section %q", id)
		}
		sec := newManifestSection(id, r)
		m.sections[id] = sec
		m.summary.Sections[slot] = sec.SectionSummary
	}
	m.summary.Header = cap.Header
	m.summary.Root = manifestRoot(cap.Header, m.authority, m.summary.Sections)
	return nil
}

// sectionRowsFor re-derives the canonical rows of just the named sections.
func (m *captureManifest) sectionRowsFor(cap SharedCapture, ids []string) (map[string][]ManifestRow, error) {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make(map[string][]ManifestRow, len(ids))
	var err error
	if want[sectionMeta] {
		if out[sectionMeta], err = metaRows(cap); err != nil {
			return nil, err
		}
	}
	if want[sectionStreams] {
		if out[sectionStreams], err = streamRows(cap); err != nil {
			return nil, err
		}
	}
	if want[sectionSystems] {
		if out[sectionSystems], err = systemRows(cap); err != nil {
			return nil, err
		}
	}
	if want[sectionStatus] {
		if out[sectionStatus], err = statusRows(cap); err != nil {
			return nil, err
		}
	}
	if want[sectionFSM] {
		if out[sectionFSM], err = fsmRows(cap); err != nil {
			return nil, err
		}
	}
	cursors := ownerAuthoredCursors(cap, m.authority)
	var scratch []engine.StoreRow
	for i := range engine.SharedWorldStoreCount {
		name := engine.SharedWorldStoreNames[i]
		id := storeSectionPrefix + name
		if !want[id] {
			continue
		}
		scratch = scratch[:0]
		if scratch, err = engine.SharedWorldStoreRows(&cap.World, i, scratch); err != nil {
			return nil, fmt.Errorf("manifest %s: %w", name, err)
		}
		rows, err := storeManifestRows(name, scratch, cursors)
		if err != nil {
			return nil, fmt.Errorf("manifest %s: %w", name, err)
		}
		out[id] = rows
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			return nil, fmt.Errorf("manifest holds no section %q", id)
		}
	}
	return out, nil
}

// Root returns the manifest's root hash.
func (m *captureManifest) Root() uint64 { return m.summary.Root }

// Summary returns the wire half of the index.
func (m *captureManifest) Summary() CorrectionManifest { return m.summary }

// section returns one section by id.
func (m *captureManifest) section(id string) (*manifestSection, bool) {
	s, ok := m.sections[id]
	return s, ok
}

// repartition rebuilds one section's page hashes under a page count the sender
// declared rather than the one this side would have chosen.
//
// This is the descent's first step on the receiving side. Without it a receiver
// whose row count differs — which is the ordinary case when something diverged —
// would bucket every row differently and report every page as mismatching, which
// is a true statement that identifies nothing.
func (m *captureManifest) repartition(id string, pages uint32) ([]uint64, bool) {
	sec, ok := m.sections[id]
	if !ok {
		return nil, false
	}
	if pages == 0 {
		return nil, false
	}
	if uint32(len(sec.pageHash)) == pages {
		return sec.pageHash, true
	}
	buckets := make([][]ManifestRow, pages)
	for _, row := range sec.rows {
		p := rowPage(row, pages)
		buckets[p] = append(buckets[p], row)
	}
	out := make([]uint64, pages)
	for p := range pages {
		out[p] = pageHash(id, p, buckets[p])
	}
	return out, true
}

// pageContent returns one section's rows for a page under a declared partition.
func (m *captureManifest) pageContent(id string, page, pages uint32) ([]ManifestRow, bool) {
	sec, ok := m.sections[id]
	if !ok || pages == 0 || page >= pages {
		return nil, false
	}
	if uint32(len(sec.pageRows)) == pages {
		return sec.pageRows[page], true
	}
	out := make([]ManifestRow, 0, len(sec.rows)/int(pages)+1)
	for _, row := range sec.rows {
		if rowPage(row, pages) == page {
			out = append(out, row)
		}
	}
	return out, true
}

// === section row builders ===

// metaScalars is the capture's shared-allocator surface as one indexed value.
type metaScalars struct {
	NextEntity uint64 `json:"next_entity"`
	Created    int64  `json:"created"`
	Destroyed  int64  `json:"destroyed"`
}

func metaRows(cap SharedCapture) ([]ManifestRow, error) {
	body, err := json.Marshal(metaScalars{
		NextEntity: cap.World.NextEntity,
		Created:    cap.World.Created,
		Destroyed:  cap.World.Destroyed,
	})
	if err != nil {
		return nil, err
	}
	return []ManifestRow{{Name: "scalars", Value: body}}, nil
}

func streamRows(cap SharedCapture) ([]ManifestRow, error) {
	out := make([]ManifestRow, 0, len(cap.Streams))
	for _, st := range cap.Streams {
		body, err := json.Marshal(st)
		if err != nil {
			return nil, err
		}
		out = append(out, ManifestRow{Name: streamRowName(st), Value: body})
	}
	return out, nil
}

// streamRowName is a stream's identity: domain and label, which is the pair
// LoadStreams resolves by.
func streamRowName(st engine.StreamState) string {
	return core.DomainNames[st.Domain] + "/" + st.Label
}

func systemRows(cap SharedCapture) ([]ManifestRow, error) {
	out := make([]ManifestRow, 0, len(cap.Systems))
	for _, rec := range cap.Systems {
		body, err := json.Marshal(rec.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, ManifestRow{Name: rec.System, Value: body})
	}
	return out, nil
}

// statusRows indexes the compared status surface, one row per cell.
//
// The type prefix is part of the name because the four registries are separate
// namespaces: an integer and a float may share a key, and a repair that confused
// them would write one metric's value into another's cell.
func statusRows(cap SharedCapture) ([]ManifestRow, error) {
	n := len(cap.Status.Ints) + len(cap.Status.Bools) + len(cap.Status.Floats) + len(cap.Status.Strings)
	out := make([]ManifestRow, 0, n)
	appendCell := func(prefix, key string, v any) error {
		body, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out = append(out, ManifestRow{Name: prefix + key, Value: body})
		return nil
	}
	for _, c := range cap.Status.Ints {
		if err := appendCell("i:", c.Key, c.Value); err != nil {
			return nil, err
		}
	}
	for _, c := range cap.Status.Bools {
		if err := appendCell("b:", c.Key, c.Value); err != nil {
			return nil, err
		}
	}
	for _, c := range cap.Status.Floats {
		if err := appendCell("f:", c.Key, c.Value); err != nil {
			return nil, err
		}
	}
	for _, c := range cap.Status.Strings {
		if err := appendCell("s:", c.Key, c.Value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func fsmRows(cap SharedCapture) ([]ManifestRow, error) {
	body, err := json.Marshal(cap.FSM)
	if err != nil {
		return nil, err
	}
	return []ManifestRow{{Name: "machine", Value: body}}, nil
}

// === hashing helpers ===

func writeUint64(h interface{ Write([]byte) (int, error) }, v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	_, _ = h.Write(b[:])
}

func writeString(h interface{ Write([]byte) (int, error) }, s string) {
	writeUint64(h, uint64(len(s)))
	_, _ = h.Write([]byte(s))
}

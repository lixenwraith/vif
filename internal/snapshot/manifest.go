package snapshot

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
)

// ManifestVersion is the correction index's own version, separate from
// Schema. The schema says what a capture contains; this says how it is
// partitioned and hashed. Either moving invalidates a comparison, and a receiver
// has to be able to say which one did.
const ManifestVersion = 4

// The section ids that are not component stores. Component store sections take
// their ids from engine.SharedWorldStoreNames, which is generated beside the
// capture, so a component added to the manifest is indexed without anyone
// remembering to add it here.
const (
	SectionMeta    = "meta"    // the shared allocator counter and lifetime totals
	SectionStreams = "streams" // every RNG stream's position
	SectionSystems = "systems" // each system's declared private state (D-19)
	SectionStatus  = "status"  // the compared status surface
	SectionFSM     = "fsm"     // the shared state machine's runtime position
)

// StoreSectionPrefix separates a component store's section id from the fixed
// sections above, so a component named "status" could not collide with one.
const StoreSectionPrefix = "w."

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

// normaliseStoreValue zeroes cursor Control for hashing and repair alike: every
// install re-derives it from local ownership, so hashing it would leave equal worlds
// disagreeing forever, falling back to keyframes over a field neither holds wrong.
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
	"weapon": true, "combat": true, "cursorview": true, "ping": true,
}

// ManifestRow keys component rows by entity and other rows by name.
// Canonical ordering is independent of dense store order.
type ManifestRow struct {
	Name   string          `json:"n,omitempty"`
	Entity core.Entity     `json:"e,omitempty"`
	Value  json.RawMessage `json:"v"`
}

// SectionSummary carries the sender's page count so a receiver with different
// row membership can reproduce the sender's partition.
type SectionSummary struct {
	ID    string `json:"id"`
	Hash  uint64 `json:"h"`
	Pages uint32 `json:"p"`
	Rows  uint32 `json:"r"`
}

// CorrectionManifest carries a header and hashes; receivers request rows only
// when the comparison differs.
type CorrectionManifest struct {
	Version int           `json:"version"`
	Header  CaptureHeader `json:"header"`
	Root    uint64        `json:"root"`

	// Authority identifies the publisher; Header carries its authority term.
	// Owner-authored cursor cells are excluded for all participants (D-13).
	Authority uint32 `json:"authority"`

	// Partition is the page count of each section with more than one page; a
	// section absent from it has one. A receiver whose root differs answers with its
	// page hashes under it, so the descent is one round trip, and a root that agrees
	// costs the partition and nothing more.
	Partition map[string]uint32 `json:"partition,omitempty"`

	// Next is the tick the sender means to index for this receiver next, outside the
	// root: the receiver reads its own world there as that tick closes, so an index
	// arriving after its tick is still compared against the tick it describes.
	Next uint64 `json:"next,omitempty"`
}

// section is one section as the sender holds it: the summary the wire carries,
// the rows it stands over and, per page, their indices in canonical order and the
// page hash. A store section stands over the row encoder's rows, which a retained
// index shares with the encoder and the indexes beside it rather than copying.
type section struct {
	SectionSummary
	rows     rowSource
	pages    [][]int32
	pageHash []uint64
}

// rowSource is where a section's rows stand: a store's encoded rows in capture
// order, or a carrier's named rows in canonical order.
type rowSource interface {
	row(i int32) ManifestRow
}

type storeRows []engine.StoreRow

func (r storeRows) row(i int32) ManifestRow {
	return ManifestRow{Entity: r[i].Entity, Value: r[i].Value}
}

type namedRows []ManifestRow

func (r namedRows) row(i int32) ManifestRow { return r[i] }

// rowsOf materialises the rows a page indexes.
func (s *section) rowsOf(page []int32) []ManifestRow {
	out := make([]ManifestRow, len(page))
	for k, i := range page {
		out[k] = s.rows.row(i)
	}
	return out
}

// indexed is every row the section indexes, page by page.
func (s *section) indexed() []ManifestRow {
	n := 0
	for _, page := range s.pages {
		n += len(page)
	}
	out := make([]ManifestRow, 0, n)
	for _, page := range s.pages {
		for _, i := range page {
			out = append(out, s.rows.row(i))
		}
	}
	return out
}

// Manifest is the whole index over one capture, held by whichever side
// built it. Only the CorrectionManifest half ever reaches the wire.
type Manifest struct {
	summary   CorrectionManifest
	list      []SectionSummary // every section, in the order the root absorbs them
	sections  map[string]*section
	index     map[string]int // section id to its slot in list
	authority uint32
	enc       *Encoder // what built it, which a repair's rebuild reuses
}

// BuildManifest indexes a detached capture outside the world lock, encoding afresh.
func BuildManifest(cap SharedCapture, authority uint32) (*Manifest, error) {
	return new(Encoder).Build(cap, authority)
}

// ownerAuthoredCursors is the cursor set whose owner-authored cells stay outside the
// hashed surface: every cursor a participant owns, the authority's included. Each
// instance holds another's as a mirror up to a sync period old, which a timer that
// moves every tick never matches, and the sync stream is their carrier (D-13). A
// cursor naming no participant has no separate author, so its cells are compared.
func ownerAuthoredCursors(cap SharedCapture) map[core.Entity]bool {
	out := make(map[core.Entity]bool, len(cap.World.Cursor))
	for _, en := range cap.World.Cursor {
		if en.Value.PeerID != 0 {
			out[en.Entity] = true
		}
	}
	return out
}

// liveOwnerAuthoredCursors is ownerAuthoredCursors read off the live store.
func liveOwnerAuthoredCursors(w *engine.World) map[core.Entity]bool {
	out := make(map[core.Entity]bool, parameter.MaxPlayers)
	for _, e := range w.Components.Cursor.Entities() {
		if c, ok := w.Components.Cursor.GetPtr(e); ok && e.Domain() == core.DomainShared && c.PeerID != 0 {
			out[e] = true
		}
	}
	return out
}

// newSection partitions one section's rows and hashes them.
//
// The partition is by entity or name rather than by position, so a row added or
// removed moves nothing else between pages. The page count comes from the row
// count at build time and travels in the summary, so both sides bucket alike.
func newSection(id string, rows []ManifestRow) *section {
	slices.SortFunc(rows, compareManifestRows)
	pages := pageCount(len(rows))
	sec := &section{
		SectionSummary: SectionSummary{ID: id, Pages: uint32(pages), Rows: uint32(len(rows))},
		rows:           namedRows(rows),
		pages:          make([][]int32, pages),
		pageHash:       make([]uint64, pages),
	}
	for i, row := range rows {
		p := rowPage(row, uint32(pages))
		sec.pages[p] = append(sec.pages[p], int32(i))
	}
	for p := range pages {
		sec.pageHash[p] = pageHash(id, uint32(p), sec.rowsOf(sec.pages[p]))
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
	return uint32(newHash().text(row.Name).u64(uint64(row.Entity)).sum()) & (pages - 1)
}

// pageHash commits to a page's identity and to its rows in canonical order.
//
// The row count and each row's identity are absorbed as well as its bytes, so
// neither a row moved between pages nor two rows swapped can reproduce the hash —
// which is what a shard's proof rests on.
func pageHash(section string, page uint32, rows []ManifestRow) uint64 {
	h := pageHasher(section, page, len(rows))
	for _, row := range rows {
		h = h.u64(rowHash(row.Name, row.Entity, row.Value))
	}
	return h.sum()
}

// pageHashOf is pageHash over a store's rows, hashed already: page indexes rows.
func pageHashOf(section string, p uint32, rows []engine.StoreRow, page []int32) uint64 {
	h := pageHasher(section, p, len(page))
	for _, i := range page {
		h = h.u64(rows[i].Hash)
	}
	return h.sum()
}

func pageHasher(section string, page uint32, rows int) hash {
	return newHash().text(hashDomainPage).u64(ManifestVersion).u64(Schema).
		text(section).u64(uint64(page)).u64(uint64(rows))
}

// rowHash commits to one row's identity and bytes.
func rowHash(name string, entity core.Entity, value []byte) uint64 {
	return newHash().text(hashDomainRow).text(name).u64(uint64(entity)).bytes(value).sum()
}

// sectionHash commits to a section's identity and to its page hashes in order.
func sectionHash(section string, pages []uint64) uint64 {
	h := newHash().text(hashDomainSection).u64(ManifestVersion).u64(Schema).
		text(section).u64(uint64(len(pages)))
	for _, p := range pages {
		h = h.u64(p)
	}
	return h.sum()
}

// The root includes identity and section hashes, but excludes tick-local
// metadata so equal worlds can agree across capture ticks.
func manifestRoot(h CaptureHeader, authority uint32, sections []SectionSummary) uint64 {
	w := newHash().text(hashDomainRoot).u64(ManifestVersion).u64(uint64(h.Schema)).
		u64(h.JournalSchema).u64(h.Run).u64(h.Session).u64(h.Seed).
		u64(uint64(h.MapWidth)).u64(uint64(h.MapHeight)).u64(uint64(authority)).
		u64(uint64(h.Term)).u64(uint64(len(sections)))
	for _, s := range sections {
		w = w.text(s.ID).u64(uint64(s.Pages)).u64(uint64(s.Rows)).u64(s.Hash)
	}
	return w.sum()
}

// Rebuild only repaired sections; unchanged sections retain their immutable
// rows and hashes. Preserve summary order because the root absorbs it.
func (m *Manifest) rebuild(cap SharedCapture, ids []string) error {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, ok := m.index[id]; !ok {
			return fmt.Errorf("manifest holds no section %q", id)
		}
		want[id] = true
	}
	secs, err := m.enc.sectionsFor(cap, want)
	if err != nil {
		return err
	}
	for _, sec := range secs {
		m.sections[sec.ID] = sec
		m.list[m.index[sec.ID]] = sec.SectionSummary
		m.partition(sec.SectionSummary)
	}
	m.summary.Header = cap.Header
	m.summary.Root = manifestRoot(cap.Header, m.authority, m.list)
	return nil
}

// partition records s's page count on the wire half where it is more than one.
func (m *Manifest) partition(s SectionSummary) {
	if s.Pages > 1 {
		if m.summary.Partition == nil {
			m.summary.Partition = make(map[string]uint32)
		}
		m.summary.Partition[s.ID] = s.Pages
	} else {
		delete(m.summary.Partition, s.ID)
	}
}

// Adopt rebinds the index to the header of a world it already describes: the root
// absorbs only session identity, which a proved-equal root already shares.
func (m *Manifest) Adopt(h CaptureHeader) {
	m.summary.Header = h
	m.summary.Root = manifestRoot(h, m.authority, m.list)
}

// Root returns the manifest's root hash.
func (m *Manifest) Root() uint64 { return m.summary.Root }

// Summary returns the wire half of the index.
func (m *Manifest) Summary() CorrectionManifest { return m.summary }

// section returns one section by id.
func (m *Manifest) section(id string) (*section, bool) {
	s, ok := m.sections[id]
	return s, ok
}

// Sections lists the index's sections in the order the root absorbs them.
func (m *Manifest) Sections() []string {
	out := make([]string, 0, len(m.list))
	for _, s := range m.list {
		out = append(out, s.ID)
	}
	return out
}

// Summaries is every section's summary in root order, which a shard set carries.
func (m *Manifest) Summaries() []SectionSummary { return m.list }

// SectionRows returns one section's canonical rows, for callers auditing what the
// index commits to, materialised from the rows it stands over.
func (m *Manifest) SectionRows(id string) ([]ManifestRow, bool) {
	s, ok := m.sections[id]
	if !ok {
		return nil, false
	}
	rows := s.indexed()
	slices.SortFunc(rows, compareManifestRows)
	return rows, true
}

// Use the sender's partition when row counts differ, so descent identifies
// different content instead of differences caused only by page counts.
func (m *Manifest) repartition(id string, pages uint32) ([]uint64, bool) {
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
	for _, row := range sec.indexed() {
		p := rowPage(row, pages)
		buckets[p] = append(buckets[p], row)
	}
	out := make([]uint64, pages)
	for p := range pages {
		slices.SortFunc(buckets[p], compareManifestRows)
		out[p] = pageHash(id, p, buckets[p])
	}
	return out, true
}

// pageContent returns one section's rows for a page under a declared partition.
func (m *Manifest) pageContent(id string, page, pages uint32) ([]ManifestRow, bool) {
	sec, ok := m.sections[id]
	if !ok || pages == 0 || page >= pages {
		return nil, false
	}
	if uint32(len(sec.pages)) == pages {
		return sec.rowsOf(sec.pages[page]), true
	}
	var out []ManifestRow
	for _, row := range sec.indexed() {
		if rowPage(row, pages) == page {
			out = append(out, row)
		}
	}
	slices.SortFunc(out, compareManifestRows)
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

package snapshot

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
)

// hashDomainCapture separates a capture's integrity from the manifest's hashes.
const hashDomainCapture = "vif/capture/integrity/1\x00"

// Encoder is one instance's encodings of the captures it seals, indexes and sends.
// A store cell keeps its JSON and row hash while its value is unchanged, and a store
// its indexed section while its rows are, so a capture costs what moved in it. Calls
// may run concurrently; what they return is read-only.
type Encoder struct {
	rows engine.RowEncoder

	mu       sync.Mutex
	sections []*storeSection
}

// storeSection is one store's last indexed section and what it was built from: the
// encoding, the owner-authored cells left out, and for each canonical row its
// capture position, page and hash.
type storeSection struct {
	sec      *section
	built    []engine.StoreRow
	excluded []core.Entity
	order    []int32   // capture position of each canonical row
	canon    []int32   // canonical row of each capture position, -1 when left out
	page     []uint32  // page of each canonical row
	members  [][]int32 // canonical rows of each page
	hashes   []uint64  // row hash of each canonical row
}

// storeRowHash is a store row's manifest hash; store rows carry no name.
func storeRowHash(e core.Entity, v []byte) uint64 { return rowHash("", e, v) }

// Integrity hashes a capture with its header's integrity field zeroed: the header and
// the whole parts by their JSON, each store by its rows' hashes in capture order.
func (e *Encoder) Integrity(cap SharedCapture) (uint64, error) {
	stores, err := e.rows.Encode(&cap.World, storeRowHash)
	if err != nil {
		return 0, fmt.Errorf("capture encode: %w", err)
	}
	return integrityOf(cap, stores)
}

// Seal sets a capture's integrity hash.
func (e *Encoder) Seal(cap *SharedCapture) error {
	sum, err := e.Integrity(*cap)
	if err != nil {
		return err
	}
	cap.Header.Integrity = sum
	return nil
}

// Verify reports whether a capture's body is the one its integrity hash describes.
func (e *Encoder) Verify(cap SharedCapture) bool {
	sum, err := e.Integrity(cap)
	return err == nil && sum == cap.Header.Integrity
}

func integrityOf(cap SharedCapture, stores [][]engine.StoreRow) (uint64, error) {
	cap.Header.Integrity = 0
	h := newHash().text(hashDomainCapture)
	for _, part := range []any{cap.Header, cap.Streams, cap.Systems, cap.Status, cap.FSM} {
		body, err := json.Marshal(part)
		if err != nil {
			return 0, fmt.Errorf("capture encode: %w", err)
		}
		h = h.bytes(body)
	}
	h = h.u64(cap.World.NextEntity).i64(cap.World.Created).i64(cap.World.Destroyed)
	for i, rows := range stores {
		h = h.text(engine.SharedWorldStoreNames[i]).u64(uint64(len(rows)))
		for _, r := range rows {
			h = h.u64(r.Hash)
		}
	}
	return h.sum(), nil
}

// EncodeCapture renders a capture in the bounded compressed wire envelope.
func (e *Encoder) EncodeCapture(cap SharedCapture) ([]byte, error) {
	plain, err := e.appendCapture(nil, cap)
	if err != nil {
		return nil, err
	}
	return encodePlain(plain, snapshotCodecFlate)
}

// EncodeCorrection renders a keyframe for transport.
func (e *Encoder) EncodeCorrection(cap SharedCapture) ([]byte, error) {
	plain, err := e.appendCapture([]byte(`{"full":`), cap)
	if err != nil {
		return nil, err
	}
	return encodePlain(append(plain, '}'), snapshotCodecFlate)
}

// appendCapture renders cap exactly as json.Marshal does, each store cell from its
// encoding: TestEncoderReuseMatchesAFreshEncoding holds the two together.
func (e *Encoder) appendCapture(dst []byte, cap SharedCapture) ([]byte, error) {
	stores, err := e.rows.Encode(&cap.World, storeRowHash)
	if err != nil {
		return nil, fmt.Errorf("capture encode: %w", err)
	}
	if dst, err = appendField(dst, `{"header":`, cap.Header); err != nil {
		return nil, err
	}
	w := cap.World
	dst = strconv.AppendUint(append(dst, `,"world":{"next_entity":`...), w.NextEntity, 10)
	dst = strconv.AppendInt(append(dst, `,"created":`...), w.Created, 10)
	dst = strconv.AppendInt(append(dst, `,"destroyed":`...), w.Destroyed, 10)
	for i, rows := range stores {
		if len(rows) == 0 {
			continue
		}
		dst = append(append(append(dst, `,"`...), engine.SharedWorldStoreNames[i]...), `":[`...)
		for j, r := range rows {
			if j > 0 {
				dst = append(dst, ',')
			}
			dst = strconv.AppendUint(append(dst, `{"e":`...), uint64(r.Entity), 10)
			dst = append(append(append(dst, `,"v":`...), r.Value...), '}')
		}
		dst = append(dst, ']')
	}
	dst = append(dst, '}')
	for _, f := range []struct {
		key string
		v   any
	}{{`,"streams":`, cap.Streams}, {`,"systems":`, cap.Systems}, {`,"status":`, cap.Status}, {`,"fsm":`, cap.FSM}} {
		if dst, err = appendField(dst, f.key, f.v); err != nil {
			return nil, err
		}
	}
	return append(dst, '}'), nil
}

func appendField(dst []byte, key string, v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("capture encode: %w", err)
	}
	return append(append(dst, key...), body...), nil
}

// Build indexes a detached capture outside the world lock.
func (e *Encoder) Build(cap SharedCapture, authority uint32) (*Manifest, error) {
	stores, err := e.rows.Encode(&cap.World, storeRowHash)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return e.build(cap, authority, stores, ownerAuthoredCursors(cap))
}

// BuildLive indexes the live world where it stands: cap carries every part of a
// capture but its store rows, which are read in place rather than copied, so an
// answer that proves the world costs the index and nothing else. Caller MUST hold
// the world lock.
func (e *Encoder) BuildLive(w *engine.World, cap SharedCapture, authority uint32) (*Manifest, error) {
	stores, err := e.rows.EncodeLive(w, storeRowHash)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return e.build(cap, authority, stores, liveOwnerAuthoredCursors(w))
}

func (e *Encoder) build(cap SharedCapture, authority uint32, stores [][]engine.StoreRow, cursors map[core.Entity]bool) (*Manifest, error) {
	m := &Manifest{
		summary: CorrectionManifest{
			Version:   ManifestVersion,
			Header:    cap.Header,
			Authority: authority,
		},
		authority: authority,
		enc:       e,
		sections:  make(map[string]*section, engine.SharedWorldStoreCount+5),
		index:     make(map[string]int, engine.SharedWorldStoreCount+5),
	}
	secs, err := e.sectionsOver(cap, nil, stores, cursors)
	if err != nil {
		return nil, err
	}
	for _, sec := range secs {
		m.sections[sec.ID] = sec
		m.index[sec.ID] = len(m.list)
		m.list = append(m.list, sec.SectionSummary)
		m.partition(sec.SectionSummary)
	}
	m.summary.Root = manifestRoot(cap.Header, authority, m.list)
	return m, nil
}

// sectionsFor builds the sections want names over a capture's own rows.
func (e *Encoder) sectionsFor(cap SharedCapture, want map[string]bool) ([]*section, error) {
	stores, err := e.rows.Encode(&cap.World, storeRowHash)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return e.sectionsOver(cap, want, stores, ownerAuthoredCursors(cap))
}

// sectionsOver builds the sections want names, every one when want is nil, in the
// order the root absorbs them, over rows already encoded.
func (e *Encoder) sectionsOver(cap SharedCapture, want map[string]bool, stores [][]engine.StoreRow, cursors map[core.Entity]bool) ([]*section, error) {
	wants := func(id string) bool { return want == nil || want[id] }
	out := make([]*section, 0, engine.SharedWorldStoreCount+5)
	if wants(SectionMeta) {
		rows, err := metaRows(cap)
		if err != nil {
			return nil, err
		}
		out = append(out, newSection(SectionMeta, rows))
	}
	e.mu.Lock()
	if e.sections == nil {
		e.sections = make([]*storeSection, len(stores))
	}
	for i, rows := range stores {
		if id := StoreSectionPrefix + engine.SharedWorldStoreNames[i]; wants(id) {
			sec, err := e.storeSectionLocked(i, rows, cursors)
			if err != nil {
				e.mu.Unlock()
				return nil, fmt.Errorf("manifest %s: %w", id, err)
			}
			out = append(out, sec)
		}
	}
	e.mu.Unlock()
	for _, part := range []struct {
		id   string
		rows func(SharedCapture) ([]ManifestRow, error)
	}{{SectionStreams, streamRows}, {SectionSystems, systemRows}, {SectionStatus, statusRows}, {SectionFSM, fsmRows}} {
		if !wants(part.id) {
			continue
		}
		rows, err := part.rows(cap)
		if err != nil {
			return nil, err
		}
		out = append(out, newSection(part.id, rows))
	}
	return out, nil
}

// storeSectionLocked is store i's section over rows: the last one when its rows and
// left-out cells are the same, that one with its changed pages rehashed when only
// values moved, and a fresh one otherwise. The cursor store's handful of rows is
// normalised and indexed afresh every time. Caller MUST hold e.mu.
func (e *Encoder) storeSectionLocked(i int, rows []engine.StoreRow, cursors map[core.Entity]bool) (*section, error) {
	name := engine.SharedWorldStoreNames[i]
	id := StoreSectionPrefix + name
	if name == cursorStoreName {
		out := make([]ManifestRow, 0, len(rows))
		for _, r := range rows {
			v, err := normaliseStoreValue(name, r.Value)
			if err != nil {
				return nil, err
			}
			out = append(out, ManifestRow{Entity: r.Entity, Value: v})
		}
		return newSection(id, out), nil
	}
	var excluded []core.Entity
	if ownerAuthoredStores[name] {
		for _, r := range rows {
			if cursors[r.Entity] {
				excluded = append(excluded, r.Entity) // D-13: one author, not the receiver's to repair
			}
		}
		slices.Sort(excluded)
	}
	prev := e.sections[i]
	if prev != nil && slices.Equal(prev.excluded, excluded) {
		if sameRows(prev.built, rows) {
			return prev.sec, nil
		}
		if next, ok := prev.update(id, rows); ok {
			e.sections[i] = next
			return next.sec, nil
		}
	}
	next := newStoreSection(id, rows, excluded)
	e.sections[i] = next
	return next.sec, nil
}

// sameRows reports whether b is a's slice, which an unchanged store's encoding is.
func sameRows(a, b []engine.StoreRow) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// newStoreSection indexes rows afresh, leaving out the excluded cells.
func newStoreSection(id string, rows []engine.StoreRow, excluded []core.Entity) *storeSection {
	s := &storeSection{built: rows, excluded: excluded, canon: make([]int32, len(rows))}
	s.order = make([]int32, 0, len(rows))
	for j, r := range rows {
		s.canon[j] = -1
		if _, out := slices.BinarySearch(excluded, r.Entity); !out {
			s.order = append(s.order, int32(j))
		}
	}
	slices.SortFunc(s.order, func(a, b int32) int {
		switch ea, eb := rows[a].Entity, rows[b].Entity; {
		case ea < eb:
			return -1
		case ea > eb:
			return 1
		}
		return 0
	})
	pages := pageCount(len(s.order))
	sec := &section{
		SectionSummary: SectionSummary{ID: id, Pages: uint32(pages), Rows: uint32(len(s.order))},
		rows:           make([]ManifestRow, len(s.order)),
		pageHash:       make([]uint64, pages),
		pageRows:       make([][]ManifestRow, pages),
	}
	s.page, s.hashes, s.members = make([]uint32, len(s.order)), make([]uint64, len(s.order)), make([][]int32, pages)
	for c, j := range s.order {
		r := rows[j]
		s.canon[j] = int32(c)
		sec.rows[c] = ManifestRow{Entity: r.Entity, Value: r.Value}
		p := rowPage(sec.rows[c], uint32(pages))
		s.page[c], s.hashes[c] = p, r.Hash
		s.members[p] = append(s.members[p], int32(c))
		sec.pageRows[p] = append(sec.pageRows[p], sec.rows[c])
	}
	for p := range pages {
		sec.pageHash[p] = pageHashOf(id, uint32(p), s.members[p], s.hashes)
	}
	sec.Hash = sectionHash(id, sec.pageHash)
	s.sec = sec
	return s
}

// update is s over rows holding the same entities in the same capture order, with
// only the pages whose rows changed rehashed. ok is false for any other sequence.
func (s *storeSection) update(id string, rows []engine.StoreRow) (*storeSection, bool) {
	if len(rows) != len(s.built) {
		return nil, false
	}
	var changed []int
	dirty := make(map[uint32]bool)
	for j := range rows {
		if rows[j].Entity != s.built[j].Entity {
			return nil, false
		}
		if c := s.canon[j]; c >= 0 && &rows[j].Value[0] != &s.built[j].Value[0] {
			changed = append(changed, j)
			dirty[s.page[c]] = true
		}
	}
	prev := s.sec
	sec := &section{
		SectionSummary: prev.SectionSummary,
		rows:           slices.Clone(prev.rows),
		pageHash:       slices.Clone(prev.pageHash),
		pageRows:       slices.Clone(prev.pageRows),
	}
	next := *s
	next.built, next.sec, next.hashes = rows, sec, slices.Clone(s.hashes)
	for _, j := range changed {
		c := s.canon[j]
		sec.rows[c].Value, next.hashes[c] = rows[j].Value, rows[j].Hash
	}
	for p := range dirty {
		members := s.members[p]
		page := make([]ManifestRow, len(members))
		for k, c := range members {
			page[k] = sec.rows[c]
		}
		sec.pageRows[p] = page
		sec.pageHash[p] = pageHashOf(id, p, members, next.hashes)
	}
	sec.Hash = sectionHash(id, sec.pageHash)
	return &next, true
}

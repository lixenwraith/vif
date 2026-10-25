package snapshot

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
)

// hashDomainCapture separates a capture's integrity from the manifest's hashes.
const hashDomainCapture = "vif/capture/integrity/1\x00"

// Encoder is one instance's encodings of the captures it seals, indexes and sends.
// A store cell keeps its JSON and row hash while its value is unchanged, and a
// store its indexed section and its deflated rows while its rows are, so a capture
// costs what moved in it. Calls may run concurrently; what they return is read-only.
type Encoder struct {
	rows engine.RowEncoder

	mu       sync.Mutex
	sections []*storeSection

	blobMu sync.Mutex
	blobs  []storeBlob
}

// A capture's parts on the wire: partRest is one JSON object holding the header
// under partHeader and the manifest's whole sections by section name; partStores
// holds the stores whose columns come under parameter.SnapshotSectionBlobBytes,
// each behind its index and length; the stores from that size up are parts of their
// own, named as their sections. A delta is one part, its JSON behind a length.
const (
	partRest   = "rest"
	partHeader = "header"
	partStores = "stores"
	partDelta  = "delta"
)

// storeBlob is one store's deflated columns and the rows they were laid out from.
type storeBlob struct {
	built []engine.StoreRow
	plain int
	blob  []byte
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

// IntegrityLive is Integrity over a world's rows read where they stand, with cap
// carrying everything beside them. Caller MUST hold w's updateMutex.
func (e *Encoder) IntegrityLive(w *engine.World, cap SharedCapture) (uint64, error) {
	stores, err := e.rows.EncodeLive(w, storeRowHash)
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

// EncodeCapture renders a capture as sections: the small parts deflated together,
// each large store in columns on its own and, while its rows are the slice the row
// encoder returned before, sent from the bytes made then; the small stores'
// columns travel together.
func (e *Encoder) EncodeCapture(cap SharedCapture) ([]byte, error) {
	stores, err := e.rows.Encode(&cap.World, storeRowHash)
	if err != nil {
		return nil, fmt.Errorf("capture encode: %w", err)
	}
	rest := []byte{'{'}
	for _, part := range []struct {
		name string
		v    any
	}{
		{partHeader, cap.Header},
		{SectionMeta, metaScalars{NextEntity: cap.World.NextEntity, Created: cap.World.Created, Destroyed: cap.World.Destroyed}},
		{SectionStreams, cap.Streams}, {SectionSystems, cap.Systems}, {SectionStatus, cap.Status}, {SectionFSM, cap.FSM},
	} {
		plain, err := json.Marshal(part.v)
		if err != nil {
			return nil, fmt.Errorf("capture encode: %w", err)
		}
		rest = appendMember(rest, part.name, plain)
	}
	rest = append(rest, '}')

	e.blobMu.Lock()
	defer e.blobMu.Unlock()
	if e.blobs == nil {
		e.blobs = make([]storeBlob, len(stores))
	}
	parts := make([]sectionPart, 1, len(stores)+2)
	var small []byte
	grouped := 0
	for i, rows := range stores {
		if len(rows) == 0 {
			continue
		}
		name, b := StoreSectionPrefix+engine.SharedWorldStoreNames[i], &e.blobs[i]
		if sameRows(b.built, rows) {
			parts = append(parts, sectionPart{name: name, plain: b.plain, blob: b.blob})
			continue
		}
		plain := engine.SharedWorldEncodeStoreColumns(nil, &cap.World, i)
		if len(plain) < parameter.SnapshotSectionBlobBytes {
			small = binary.AppendUvarint(small, uint64(i))
			small = binary.AppendUvarint(small, uint64(len(plain)))
			small = append(small, plain...)
			grouped++
			continue
		}
		blob, err := deflateBytes(plain)
		if err != nil {
			return nil, err
		}
		*b = storeBlob{built: rows, plain: len(plain), blob: blob}
		parts = append(parts, sectionPart{name: name, plain: len(plain), blob: blob})
	}
	blob, err := deflateBytes(rest)
	if err != nil {
		return nil, err
	}
	parts[0] = sectionPart{name: partRest, plain: len(rest), blob: blob}
	if grouped > 0 {
		plain := append(binary.AppendUvarint(nil, uint64(grouped)), small...)
		if blob, err = deflateBytes(plain); err != nil {
			return nil, err
		}
		parts = append(parts, sectionPart{name: partStores, plain: len(plain), blob: blob})
	}
	return encodeSections(parts)
}

// appendMember appends one member of the rest object, its JSON already rendered.
func appendMember(dst []byte, name string, plain []byte) []byte {
	if len(dst) > 1 {
		dst = append(dst, ',')
	}
	return append(append(strconv.AppendQuote(dst, name), ':'), plain...)
}

// DecodeCapture parses what EncodeCapture produced, part by part. It validates
// nothing about the world: the caller passes the result to VerifyCapture or
// InstallShared.
func DecodeCapture(b []byte) (SharedCapture, error) {
	parts, err := decodeSections(b)
	if err != nil {
		return SharedCapture{}, fmt.Errorf("capture decode: %w", err)
	}
	return captureFromParts(parts)
}

// captureFromParts fills a capture from its parts: the rest's members, the grouped
// small stores and each large store's columns, a store taken once.
func captureFromParts(parts []sectionPart) (SharedCapture, error) {
	var cap SharedCapture
	seen := make([]bool, engine.SharedWorldStoreCount)
	store := func(i int, data []byte) error {
		if i < 0 || i >= len(seen) {
			return fmt.Errorf("no store %d", i)
		}
		if seen[i] {
			return fmt.Errorf("store %q twice", engine.SharedWorldStoreNames[i])
		}
		seen[i] = true
		if err := engine.SharedWorldDecodeStoreColumns(&cap.World, i, data); err != nil {
			return fmt.Errorf("%s: %w", engine.SharedWorldStoreNames[i], err)
		}
		return nil
	}
	var err error
	for _, p := range parts {
		switch {
		case p.name == partRest:
			var members map[string]json.RawMessage
			err = json.Unmarshal(p.blob, &members)
			for name, data := range members { // each member fills its own field
				if err != nil {
					break
				}
				err = decodePart(&cap, name, data)
			}
		case p.name == partStores:
			err = decodeStoreGroup(p.blob, store)
		case strings.HasPrefix(p.name, StoreSectionPrefix):
			err = store(slices.Index(engine.SharedWorldStoreNames, p.name[len(StoreSectionPrefix):]), p.blob)
		default:
			err = fmt.Errorf("unknown part %q", p.name)
		}
		if err != nil {
			return SharedCapture{}, fmt.Errorf("capture decode: %w", err)
		}
	}
	return cap, nil
}

// decodeStoreGroup walks the grouped stores, each behind its index and length.
func decodeStoreGroup(data []byte, store func(int, []byte) error) error {
	next := func(what string, bound uint64) (int, error) {
		v, n := binary.Uvarint(data)
		if n <= 0 || v > bound {
			return 0, fmt.Errorf("the grouped stores' %s is cut short", what)
		}
		data = data[n:]
		return int(v), nil
	}
	count, err := next("count", uint64(len(data)))
	if err != nil {
		return err
	}
	for range count {
		i, err := next("index", uint64(engine.SharedWorldStoreCount))
		if err != nil {
			return err
		}
		size, err := next("length", uint64(len(data)))
		if err != nil {
			return err
		}
		if size > len(data) {
			return errors.New("a grouped store is cut short")
		}
		if err := store(i, data[:size]); err != nil {
			return err
		}
		data = data[size:]
	}
	if len(data) != 0 {
		return fmt.Errorf("%d bytes past the grouped stores", len(data))
	}
	return nil
}

// decodePart parses one named part into its place in cap.
func decodePart(cap *SharedCapture, name string, data []byte) error {
	var err error
	switch {
	case name == partHeader:
		err = json.Unmarshal(data, &cap.Header)
	case name == SectionMeta:
		var m metaScalars
		err = json.Unmarshal(data, &m)
		cap.World.NextEntity, cap.World.Created, cap.World.Destroyed = m.NextEntity, m.Created, m.Destroyed
	case name == SectionStreams:
		err = json.Unmarshal(data, &cap.Streams)
	case name == SectionSystems:
		err = json.Unmarshal(data, &cap.Systems)
	case name == SectionStatus:
		err = json.Unmarshal(data, &cap.Status)
	case name == SectionFSM:
		err = json.Unmarshal(data, &cap.FSM)
	default:
		return fmt.Errorf("unknown part %q", name)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
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

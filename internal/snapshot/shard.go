package snapshot

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
)

// SectionRequest is one section's page hashes as the requester computed them, under
// the partition the manifest declared. Sending the receiver's hashes makes the
// descent one round trip: the sender compares where the content is.
type SectionRequest struct {
	ID    string   `json:"id"`
	Pages uint32   `json:"p"`
	Hash  []uint64 `json:"h"`
}

// CorrectionRequest is a receiver's answer to one manifest. Every manifest is
// answered, the converged ones included: the ack keeps the peer in the selective
// protocol (SnapshotManifestSilenceCorrections), and Root, the receiver's own, lets
// the host record convergence from the message rather than from an absence.
type CorrectionRequest struct {
	Version int                   `json:"version"`
	Schema  int                   `json:"schema"`
	Tick    uint64                `json:"tick"`
	Run     uint64                `json:"run"`
	Session uint64                `json:"session"`
	Root    uint64                `json:"root"`
	Term    network.AuthorityTerm `json:"term,omitempty"`

	// Keyframe asks for a whole world instead of a repair. A receiver sets it when
	// it has nothing to compare against, when a repair failed its proof, or when
	// the convergence floor has elapsed without one arriving.
	Keyframe bool `json:"keyframe,omitempty"`

	// Index asks for the section summaries of a manifest that arrived as its root
	// alone: the roots differed, and pages can only be chosen section by section.
	Index bool `json:"index,omitempty"`

	// Sections is empty exactly when the roots matched, which is the healthy case
	// and the one this protocol exists to make cheap.
	Sections []SectionRequest `json:"sections,omitempty"`

	// Relayed names the participants this answer's sender forwarded the manifest
	// to and holds retention for. It is how the authority learns that a
	// participant it has no link to can nonetheless be answered — the one fact the
	// gate needs and the only one this instance can state on its behalf.
	Relayed []uint32 `json:"relayed,omitempty"`
}

// Converged reports whether this request asks for nothing: the hash-only case.
func (r CorrectionRequest) Converged() bool {
	return !r.Keyframe && !r.Index && len(r.Sections) == 0
}

// CorrectionShard is one repaired page. Rows is the page's whole content, bounded
// with the page, not a difference that would need a baseline of its own; an empty
// Rows says the authority holds nothing there, which an overwrite of only what
// arrived would miss.
type CorrectionShard struct {
	Section string        `json:"s"`
	Page    uint32        `json:"p"`
	Pages   uint32        `json:"n"`
	Hash    uint64        `json:"h"`
	Rows    []ManifestRow `json:"r,omitempty"`
}

// CorrectionShardSet is one atomic repair: one set, one baseline, all or nothing.
// It repeats the manifest's header, root and section summaries, so it is validated
// on its own and can never be read against, or merged with, another; a newer set
// replaces an older one, and every refusal ends at the keyframe fallback.
type CorrectionShardSet struct {
	Version int           `json:"version"`
	Schema  int           `json:"schema"`
	Header  CaptureHeader `json:"header"`
	Root    uint64        `json:"root"`

	// Authority is the participant whose world these pages describe, and Served the
	// peer that produced the answer; they differ when a relay answered. The proof is
	// the authority's either way, and Served prices the bytes on the edge they took.
	Authority uint32 `json:"authority"`
	Served    uint32 `json:"served,omitempty"`

	Sections []SectionSummary  `json:"sections"`
	Shards   []CorrectionShard `json:"shards"`
}

// CorrectionUnserved answers a request a retention holder cannot produce pages for.
// A silence would cost the receiver a cadence waiting, and a body from another
// baseline is what supersession makes unreachable, so the receiver is told and
// asks the authority instead, and failing that for a keyframe.
type CorrectionUnserved struct {
	Version int                   `json:"version"`
	Tick    uint64                `json:"tick"`
	Term    network.AuthorityTerm `json:"term,omitempty"`
	From    uint32                `json:"from"`
	Reason  string                `json:"reason"`
}

// EncodeUnserved renders one cannot-serve answer.
func EncodeUnserved(u CorrectionUnserved) ([]byte, error) { return EncodeJSON(u) }

// DecodeUnserved parses what EncodeUnserved produced.
func DecodeUnserved(b []byte) (CorrectionUnserved, error) {
	var u CorrectionUnserved
	if err := DecodeJSON(b, &u); err != nil {
		return CorrectionUnserved{}, fmt.Errorf("unserved answer decode: %w", err)
	}
	return u, nil
}

// EncodeManifest renders a manifest summary in the bounded, compressed envelope.
func EncodeManifest(m CorrectionManifest) ([]byte, error) { return EncodePlainJSON(m) }

// DecodeManifest parses what EncodeManifest produced.
func DecodeManifest(b []byte) (CorrectionManifest, error) {
	var m CorrectionManifest
	if err := DecodeJSON(b, &m); err != nil {
		return CorrectionManifest{}, fmt.Errorf("manifest decode: %w", err)
	}
	return m, nil
}

// EncodeCorrectionRequest renders one answer to a manifest.
func EncodeCorrectionRequest(r CorrectionRequest) ([]byte, error) { return EncodePlainJSON(r) }

// DecodeCorrectionRequest parses what EncodeCorrectionRequest produced.
func DecodeCorrectionRequest(b []byte) (CorrectionRequest, error) {
	var r CorrectionRequest
	if err := DecodeJSON(b, &r); err != nil {
		return CorrectionRequest{}, fmt.Errorf("correction request decode: %w", err)
	}
	return r, nil
}

// EncodeShardSet renders one repair.
func EncodeShardSet(s CorrectionShardSet) ([]byte, error) { return EncodeJSON(s) }

// DecodeShardSet parses what EncodeShardSet produced.
func DecodeShardSet(b []byte) (CorrectionShardSet, error) {
	var s CorrectionShardSet
	if err := DecodeJSON(b, &s); err != nil {
		return CorrectionShardSet{}, fmt.Errorf("shard set decode: %w", err)
	}
	return s, nil
}

// === receiver: the descent ===

// CompareRequest is what a receiver answers a manifest with. The descent stops at
// the first level that agrees: equal roots end it with an empty request, otherwise
// only the differing sections' page hashes are sent. A section the receiver does
// not know is reported wholly mismatching, since it has no page hash to offer.
func CompareRequest(mine *Manifest, want CorrectionManifest) (CorrectionRequest, int, int) {
	req := CorrectionRequest{
		Version: ManifestVersion,
		Schema:  Schema,
		Tick:    want.Header.Tick,
		Run:     want.Header.Run,
		Session: want.Header.Session,
		Root:    mine.Root(),
	}
	sections, pages := len(want.Sections), 0
	if mine.Root() == want.Root {
		return req, sections, pages
	}
	if want.Sections == nil {
		req.Index = true
		return req, sections, pages
	}
	for _, s := range want.Sections {
		theirs, ok := mine.section(s.ID)
		if ok && theirs.Hash == s.Hash && theirs.Pages == s.Pages {
			continue
		}
		hashes, ok := mine.repartition(s.ID, s.Pages)
		if !ok {
			hashes = make([]uint64, s.Pages)
		}
		pages += len(hashes)
		req.Sections = append(req.Sections, SectionRequest{ID: s.ID, Pages: s.Pages, Hash: hashes})
	}
	// The roots differed but every section agreed. That can only happen when the
	// two sides hold different *sets* of sections or different identities, and
	// neither is repairable by a page: the only sound answer is a whole world.
	if len(req.Sections) == 0 {
		req.Keyframe = true
	}
	return req, sections, pages
}

// === sender: building the repair ===

// BuildShardSet answers one request from the manifest the sender retained for the
// tick it names. A set past SnapshotShardBytesMax, checked on the encoded bytes a
// frame and an allocation are counted in, is not built: a keyframe is then both
// smaller and stronger.
func BuildShardSet(mine *Manifest, req CorrectionRequest) (CorrectionShardSet, int, error) {
	set := CorrectionShardSet{
		Version:   ManifestVersion,
		Schema:    Schema,
		Header:    mine.summary.Header,
		Root:      mine.Root(),
		Authority: mine.authority,
		Sections:  mine.summary.Sections,
	}
	pages := 0
	for _, sr := range req.Sections {
		if _, ok := mine.section(sr.ID); !ok {
			return CorrectionShardSet{}, 0, fmt.Errorf("request names section %q, which this capture has no page for", sr.ID)
		}
		if sr.Pages == 0 || sr.Pages > parameter.SnapshotManifestMaxPages {
			return CorrectionShardSet{}, 0, fmt.Errorf("request partitions %q into %d pages", sr.ID, sr.Pages)
		}
		ours, ok := mine.repartition(sr.ID, sr.Pages)
		if !ok {
			return CorrectionShardSet{}, 0, fmt.Errorf("cannot repartition %q into %d pages", sr.ID, sr.Pages)
		}
		for p := range uint32(len(ours)) {
			if int(p) < len(sr.Hash) && sr.Hash[p] == ours[p] {
				continue
			}
			rows, ok := mine.pageContent(sr.ID, p, sr.Pages)
			if !ok {
				return CorrectionShardSet{}, 0, fmt.Errorf("cannot read page %d of %q", p, sr.ID)
			}
			pages++
			set.Shards = append(set.Shards, CorrectionShard{
				Section: sr.ID,
				Page:    p,
				Pages:   sr.Pages,
				Hash:    ours[p],
				Rows:    rows,
			})
		}
	}
	return set, pages, nil
}

// === receiver: validation and apply ===

// ShardRepair is what one applied set moved, for telemetry and for the log line
// an operator reads when a repair looks wrong.
type ShardRepair struct {
	Pages    int
	Rows     int
	Entities int
	Sections int
}

// ValidateShardSet refuses a set before anything is spliced: an unknown version or
// schema, a baseline from another run, session or tick than the manifest answered,
// two shards for one page, a page outside its declared partition, or rows that do
// not reproduce their page hash. Nothing is touched until every check has passed.
func ValidateShardSet(set CorrectionShardSet, tick uint64, authority uint32, root uint64, an CaptureHeader) error {
	switch {
	case set.Version != ManifestVersion:
		return fmt.Errorf("shard set version %d, this build reads %d", set.Version, ManifestVersion)
	case set.Schema != Schema:
		return fmt.Errorf("shard set schema %d, this build reads %d", set.Schema, Schema)
	case set.Header.Tick != tick:
		return fmt.Errorf("shard set describes tick %d, the manifest asked about %d", set.Header.Tick, tick)
	case set.Header.Run != an.Run || set.Header.Session != an.Session || set.Header.Seed != an.Seed:
		return errors.New("shard set describes another run")
	case set.Header.Term != an.Term || set.Header.Authority != an.Authority ||
		!set.Header.Crossings.Equal(an.Crossings):
		// The root leaves out tick-local metadata, but the authority and crossing
		// fence decide which queued events a receiver drops, so they must match the
		// manifest. Integrity may differ: a relay's equal world has its own store order.
		return errors.New("shard set authority header differs from the manifest it answers")
	case set.Authority != authority:
		return errors.New("shard set names another authority than the manifest it answers")
	case set.Root != root:
		// What a relayed answer rests on: the declared root is the authority's, and
		// the repaired capture must reproduce it, so a substituted or corrupted page
		// fails here exactly as a corrupt wire does.
		return errors.New("shard set declares a root the manifest it answers does not")
	case len(set.Sections) == 0:
		return errors.New("shard set carries no section summary")
	}

	partition := make(map[string]uint32, len(set.Sections))
	for _, s := range set.Sections {
		if s.Pages == 0 || s.Pages > parameter.SnapshotManifestMaxPages {
			return fmt.Errorf("section %q declares %d pages", s.ID, s.Pages)
		}
		partition[s.ID] = s.Pages
	}
	if root := manifestRoot(set.Header, set.Authority, set.Sections); root != set.Root {
		return errors.New("shard set's section summary does not produce the root it declares")
	}

	seen := make(map[string]uint64, len(set.Shards))
	for _, sh := range set.Shards {
		pages, ok := partition[sh.Section]
		if !ok {
			return fmt.Errorf("shard names section %q, which the set does not summarise", sh.Section)
		}
		if sh.Pages != pages {
			return fmt.Errorf("shard for %q partitions into %d pages, its section declares %d",
				sh.Section, sh.Pages, pages)
		}
		if sh.Page >= sh.Pages {
			return fmt.Errorf("shard names page %d of %d in %q", sh.Page, sh.Pages, sh.Section)
		}
		key := fmt.Sprintf("%s/%d", sh.Section, sh.Page)
		if prev, dup := seen[key]; dup {
			if prev != sh.Hash {
				return fmt.Errorf("two shards for %s carry different content", key)
			}
			return fmt.Errorf("shard set repeats %s", key)
		}
		seen[key] = sh.Hash
		if got := pageHash(sh.Section, sh.Page, sh.Rows); got != sh.Hash {
			return fmt.Errorf("shard %s does not reproduce the page hash it declares", key)
		}
		for _, row := range sh.Rows {
			if rowPage(row, sh.Pages) != sh.Page {
				return fmt.Errorf("shard %s carries a row another page owns", key)
			}
		}
		if !slices.IsSortedFunc(sh.Rows, compareManifestRows) {
			return fmt.Errorf("shard %s carries its rows out of canonical order", key)
		}
	}
	return nil
}

// ApplyShardSet splices a validated set into the receiver's own capture and proves
// the result by the root, which is order-independent. mine is modified in place and
// is the receiver's to discard on failure. The header is the authority's, less its
// integrity: the reconstruction is a world equal to the sender's rather than a copy
// of its capture's bytes, so no capture hash describes it and none is computed.
func ApplyShardSet(mine *SharedCapture, index *Manifest, set CorrectionShardSet) (ShardRepair, error) {
	var rep ShardRepair
	touched := make(map[string]bool, len(set.Shards))
	cursors := ownerAuthoredCursors(*mine)

	for _, sh := range set.Shards {
		n, err := applyShard(mine, cursors, sh)
		if err != nil {
			return ShardRepair{}, err
		}
		rep.Pages++
		rep.Rows += len(sh.Rows)
		rep.Entities += n
		touched[sh.Section] = true
	}
	rep.Sections = len(touched)

	mine.Header = set.Header
	mine.Header.Integrity = 0

	ids := make([]string, 0, len(touched))
	for id := range touched {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if err := index.rebuild(*mine, ids); err != nil {
		return ShardRepair{}, err
	}
	if index.Root() != set.Root {
		return ShardRepair{}, errors.New("the repaired capture does not produce the root the shard set declares")
	}
	return rep, nil
}

// applyShard writes one page into the receiver's capture, reporting how many
// distinct entities the page's replacement touched.
func applyShard(mine *SharedCapture, cursors map[core.Entity]bool, sh CorrectionShard) (int, error) {
	if name, ok := strings.CutPrefix(sh.Section, StoreSectionPrefix); ok {
		idx := slices.Index(engine.SharedWorldStoreNames, name)
		if idx < 0 {
			return 0, fmt.Errorf("shard names store %q, which this build does not carry", name)
		}
		owner := ownerAuthoredStores[name]
		owns := func(e core.Entity) bool {
			if owner && cursors[e] {
				return false // D-13: the receiver authors this cell, not the sender
			}
			return rowPage(ManifestRow{Entity: e}, sh.Pages) == sh.Page
		}
		rows := make([]engine.StoreRow, 0, len(sh.Rows))
		entities := make(map[core.Entity]struct{}, len(sh.Rows))
		for _, row := range sh.Rows {
			rows = append(rows, engine.StoreRow{Entity: row.Entity, Value: row.Value})
			entities[row.Entity] = struct{}{}
		}
		if err := engine.SharedWorldApplyStoreRows(&mine.World, idx, owns, rows); err != nil {
			return 0, fmt.Errorf("shard %s: %w", sh.Section, err)
		}
		return len(entities), nil
	}

	switch sh.Section {
	case SectionMeta:
		return 0, applyMetaShard(mine, sh)
	case SectionStreams:
		return 0, applyStreamShard(mine, sh)
	case SectionSystems:
		return 0, applySystemShard(mine, sh)
	case SectionStatus:
		return 0, applyStatusShard(mine, sh)
	case SectionFSM:
		return 0, applyFSMShard(mine, sh)
	}
	return 0, fmt.Errorf("shard names section %q, which this build does not index", sh.Section)
}

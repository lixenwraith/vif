// Package app: repairing what differs instead of sending what is.
//
// The manifest says where two instances disagree. This file is what they do about
// it: a request naming the pages a receiver could not reproduce, a shard set
// carrying exactly those pages, and an apply that splices them into the receiver's
// own capture and then proves the result is the sender's.
//
// The proof is layered on purpose, because the two layers answer different
// questions.
//
//   - A **shard** carries the page hash its rows reproduce. Recomputing it on
//     arrival says the rows are the ones the sender hashed, in the order it hashed
//     them. That is an integrity statement about one page and nothing more: it
//     catches corruption, truncation, reordering and a page delivered under
//     another page's identity.
//
//   - The **root** is the end-to-end statement. After every shard in one set is
//     applied, the receiver re-indexes the sections it touched and recomputes the
//     root; it must equal the root the set declares. A repair that produced
//     something merely plausible fails here, and nothing is installed.
//
// Between the two sits the rule that makes a partial repair safe: **one set, one
// baseline, all or nothing.** A shard set carries its own header, root and section
// summaries, so it is validated without reference to any earlier message and can
// never be combined with one. A newer set supersedes an older incomplete one by
// replacing it; there is no path that merges two. Requirement 6's "one logical
// object assembled from different baselines" is not defended against at apply time
// — it is unreachable, because a set is the only unit that is ever applied and it
// has exactly one baseline.
//
// What a receiver does when it cannot repair is always the same: it asks for a
// keyframe. Missing retention on the sender, a proof failure, a mismatch too wide
// to be worth shards, an unknown version, a root that did not verify — every one
// of them ends at a whole compressed capture, which is self-sufficient and which
// the host publishes on its own schedule anyway. That is the bounded fallback, and
// it is why none of the refusals here need a repair path of their own.
package snapshot

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// SectionRequest is one section's page hashes as the requester computed them,
// under the partition the manifest declared.
//
// Sending the receiver's hashes rather than asking for the sender's is what makes
// the descent one round trip instead of two: the sender already holds its own, so
// the comparison happens where the content is and only the mismatches travel back.
type SectionRequest struct {
	ID    string   `json:"id"`
	Pages uint32   `json:"p"`
	Hash  []uint64 `json:"h"`
}

// CorrectionRequest is a receiver's answer to one manifest.
//
// Every manifest is answered, including the ones that need nothing: the ack is
// what tells the host this peer is in the selective protocol at all, and a peer
// that stops answering falls back to whole bodies (see
// SnapshotManifestSilenceCorrections). Root is the receiver's own, so a host can
// record convergence from the message rather than infer it from an absence.
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
	return !r.Keyframe && len(r.Sections) == 0
}

// CorrectionShard is one repaired page.
//
// Rows is the page's whole content rather than a difference within it, and that is
// deliberate: a page is already bounded, so its content is bounded, and a
// difference-within-a-difference would need a baseline of its own — which is the
// mixed-baseline failure this design is built to make unreachable. An empty Rows
// is meaningful and common: it says the authority holds nothing in this page, and
// a receiver that only overwrote what arrived would keep what the authority
// dropped.
type CorrectionShard struct {
	Section string        `json:"s"`
	Page    uint32        `json:"p"`
	Pages   uint32        `json:"n"`
	Hash    uint64        `json:"h"`
	Rows    []ManifestRow `json:"r,omitempty"`
}

// CorrectionShardSet is one atomic repair.
//
// It repeats the manifest's header, root and section summaries so that it can be
// validated on its own — a receiver that lost the manifest it answers still has
// everything the apply needs, and a set can never be read against the wrong one.
type CorrectionShardSet struct {
	Version int           `json:"version"`
	Schema  int           `json:"schema"`
	Header  CaptureHeader `json:"header"`
	Root    uint64        `json:"root"`

	// Authority is the participant whose world these pages describe, and Served
	// the peer that produced the answer. They differ exactly when a relay answered
	// for the authority, which is the only thing about a relayed repair a receiver
	// treats differently: the proof is the authority's either way, so nothing about
	// validation changes, and what Served buys is that the bytes are priced against
	// the edge that carried them.
	Authority uint32 `json:"authority"`
	Served    uint32 `json:"served,omitempty"`

	Sections []SectionSummary  `json:"sections"`
	Shards   []CorrectionShard `json:"shards"`
}

// CorrectionUnserved is the answer a retention holder gives to a request it
// cannot produce pages for: it dropped the manifest the request names, or its own
// world never agreed with the authority's at that tick.
//
// It is a message rather than a silence because silence costs the receiver a
// whole cadence waiting for a repair that is not coming, and it is not a body
// because a body from a different baseline is exactly what the supersession rules
// make unreachable. The receiver degrades: it asks the authority instead, and
// failing that, for a keyframe.
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
func EncodeManifest(m CorrectionManifest) ([]byte, error) { return EncodeJSON(m) }

// DecodeManifest parses what EncodeManifest produced.
func DecodeManifest(b []byte) (CorrectionManifest, error) {
	var m CorrectionManifest
	if err := DecodeJSON(b, &m); err != nil {
		return CorrectionManifest{}, fmt.Errorf("manifest decode: %w", err)
	}
	return m, nil
}

// EncodeCorrectionRequest renders one answer to a manifest.
func EncodeCorrectionRequest(r CorrectionRequest) ([]byte, error) { return EncodeJSON(r) }

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

// CompareRequest is what a receiver answers a manifest with.
//
// The descent is two levels and stops at the first that agrees. Roots equal ends
// it immediately with an empty request, which is the healthy case: one hash
// comparison and no page work at all. Otherwise the section hashes decide which
// sections to descend into, and only those sections' page hashes are computed and
// sent — so a manifest carrying fifty-eight sections costs a page vector for the
// two that actually differ. That is requirement 4's "avoid an all-page hash list
// on every healthy correction".
//
// Sections the receiver does not know are reported as fully mismatching, which is
// the only honest answer: it cannot produce a page hash for content it has no
// section for, and the sender will send the whole section's pages.
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

// BuildShardSet answers one request from the manifest and capture the sender
// retained for the tick the request names.
//
// A set that would exceed SnapshotShardBytesMax is not built: past that width a
// keyframe is both smaller and stronger, so the caller is told to send one
// instead. The bound is checked against the encoded body rather than estimated,
// because what it is protecting is a transport frame and an allocation, and both
// are counted in bytes that were actually produced.
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

// ValidateShardSet refuses a set before anything is spliced.
//
// Every refusal here is atomic by construction: the checks run over the decoded
// set and the receiver's capture is not touched until all of them have passed.
// What is refused, and why each one has to be:
//
//   - an unknown manifest version or capture schema, because the partition and the
//     hash construction are what the two sides are agreeing on;
//   - a stale or foreign baseline — a different run, session or tick from the
//     manifest being answered — because a page is only meaningful against the
//     state its root describes;
//   - two shards naming one page with different content, because a receiver that
//     took either would install one of two worlds and could not say which;
//   - a page outside the partition its section declares, or a partition that
//     disagrees with the set's own section summary, because both make the page
//     identity ambiguous;
//   - a page whose rows do not reproduce its declared hash, which is the proof.
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
		set.Header.AuthorityCrossingSeq != an.AuthorityCrossingSeq:
		// The root intentionally excludes tick-local transport metadata so a
		// predictor can compare its world with the authority's. The authority and
		// crossing fence must nevertheless match the manifest: they decide which
		// queued events a receiver drops. Integrity may differ on a relay because
		// an equal state can have another dense-store order and recomputed capture
		// hash while producing the same canonical manifest root.
		return errors.New("shard set authority header differs from the manifest it answers")
	case set.Authority != authority:
		return errors.New("shard set names another authority than the manifest it answers")
	case set.Root != root:
		// The binding a relayed answer rests on. A relay serves pages it did not
		// author, so what makes its answer sound is that the root it declares is
		// the *authority's* — the one the receiver was sent in the manifest — and
		// that the repaired capture then reproduces it. A relay that substitutes,
		// truncates or corrupts a page fails one of the two, exactly as a corrupt
		// wire does.
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
// the result.
//
// mine is modified in place and is the receiver's to discard on failure; nothing
// touches the live world here. The header is adopted whole — the tick, the run and
// the map bounds are the authority's and an install needs them — and the integrity
// field is recomputed rather than copied, because the reconstruction is a *world*
// equal to the sender's rather than a byte-for-byte copy of its capture: the two
// hold their stores in whatever order their own histories left. The root is what
// proves the equality, and the root is order-independent by construction.
func ApplyShardSet(mine *SharedCapture, index *Manifest, set CorrectionShardSet) (ShardRepair, error) {
	var rep ShardRepair
	touched := make(map[string]bool, len(set.Shards))
	cursors := ownerAuthoredCursors(*mine, set.Authority)

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
	integrity, err := Integrity(*mine)
	if err != nil {
		return ShardRepair{}, err
	}
	mine.Header.Integrity = integrity

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

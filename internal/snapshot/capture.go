// Package snapshot is the wire model for shared-world captures.
//
// It holds what a capture is (SharedCapture and its header), how it is encoded
// (a bounded compressed JSON envelope), how it is indexed for selective repair
// (Manifest, pages, section hashes), and which status keys belong to the
// cross-instance comparison surface. It reads no world and holds no lock:
// internal/app performs capture and install against the live world and passes
// the values through here.
package snapshot

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/network"
)

// Schema is the capture layout version, distinct from the journal schema. A
// header names both so a mismatch says which one moved.
const Schema = 4

// SharedCapture is the shared world at one tick (D-19): the shared component
// stores, the allocator's next ID, every RNG stream position, and the private
// state each system declares. Everything an install recomputes (flow fields,
// spatial index, passability grid) and everything player-domain is absent.
type SharedCapture struct {
	Header  CaptureHeader           `json:"header"`
	World   engine.SharedWorldState `json:"world"`
	Streams []engine.StreamState    `json:"streams"`
	Systems []SystemStateRecord     `json:"systems"`

	// Status is the compared surface's registry half: every key SharedKey admits.
	// Cumulative species counters live here. They affect no future outcome, so no
	// system declares them under D-19, but D-11 requires two instances to agree on
	// them: a joiner holding its own totals would read as divergent from its first
	// tick and never converge.
	Status StatusState `json:"status"`

	// FSM is the shared machine's runtime position. Which state each region stands
	// in and how long it has stood there decides when the next timed transition
	// fires, so an installed world that entered its states at other ticks reaches
	// its next escalation on a different tick.
	FSM fsm.MachineState `json:"fsm"`
}

// StatusState is the shared half of the status registry, by metric type. Keys are
// sorted within each list so a capture is byte-comparable.
type StatusState struct {
	Ints    []IntCell    `json:"ints,omitempty"`
	Bools   []BoolCell   `json:"bools,omitempty"`
	Floats  []FloatCell  `json:"floats,omitempty"`
	Strings []StringCell `json:"strings,omitempty"`
}

// IntCell is one integer metric.
type IntCell struct {
	Key   string `json:"k"`
	Value int64  `json:"v"`
}

// BoolCell is one boolean metric.
type BoolCell struct {
	Key   string `json:"k"`
	Value bool   `json:"v"`
}

// FloatCell is one float metric.
type FloatCell struct {
	Key   string  `json:"k"`
	Value float64 `json:"v"`
}

// StringCell is one string metric.
type StringCell struct {
	Key   string `json:"k"`
	Value string `json:"v"`
}

// CaptureHeader names the tick a capture describes and the build, configuration
// and corpus it assumes. Identity is checked before an install writes anything: a
// capture installed under a different one diverges for reasons no digest
// attributes.
type CaptureHeader struct {
	Schema        int           `json:"schema"`
	JournalSchema uint64        `json:"journal_schema"`
	Run           uint64        `json:"run"`
	Tick          uint64        `json:"tick"`
	TickInterval  time.Duration `json:"tick_interval"`
	Seed          uint64        `json:"seed"`
	Session       uint64        `json:"session"`
	ConfigID      string        `json:"config_id"`
	ContentID     string        `json:"content_id"`
	ContentPin    string        `json:"content_pin"`
	ContentFiles  uint64        `json:"content_files"`
	ContentBlocks uint64        `json:"content_blocks"`
	ContentLines  uint64        `json:"content_lines"`

	// MapWidth and MapHeight are the D-14 shared bounds: simulation state rather
	// than this instance's terminal, and a joiner adopts them.
	MapWidth  int `json:"map_width"`
	MapHeight int `json:"map_height"`

	// Term is the authority generation this capture was produced under and
	// Authority the participant that produced it. A receiver ignores an artifact
	// from a term older than the one it holds and refuses one from a term it has
	// never been handed. Zero on a solo run.
	Term      network.AuthorityTerm `json:"term,omitempty"`
	Authority uint32                `json:"authority,omitempty"`

	// AuthorityCrossingSeq is the source-local sequence through which the authority
	// had completed its ordinary local-first crossings when the world was read.
	// Their receive-side ApplyTick may still be in the future, so this fence rather
	// than the capture tick tells a receiver which queued copies the capture
	// already contains. Barrier-bound crossings keep using their agreed ApplyTick.
	AuthorityCrossingSeq uint64 `json:"authority_crossing_seq,omitempty"`

	// Integrity hashes the capture body with this field zeroed. "Did this arrive
	// intact" and "does this describe my build" are separate questions and an
	// install answers both.
	Integrity uint64 `json:"integrity"`
}

// SystemStateRecord is one system's declared private state (D-19), named by the
// system so a capture survives systems being added, removed or reordered between
// builds. Data is opaque bytes: SaveShared promises bytes and the wall system
// hands over the maze generator's binary form, which is not JSON.
type SystemStateRecord struct {
	System string `json:"system"`
	Data   []byte `json:"data"`
}

// Anchor is the identity half of a header, in the shape the journal anchor
// comparison takes. A header reaches that comparison both from a whole capture,
// verified with its body, and from a correction manifest, which carries the
// header alone.
func Anchor(h CaptureHeader) event.JournalAnchor {
	return event.JournalAnchor{
		Schema:        h.JournalSchema,
		Seed:          h.Seed,
		Session:       h.Session,
		ConfigID:      h.ConfigID,
		ContentID:     h.ContentID,
		ContentPin:    h.ContentPin,
		ContentFiles:  h.ContentFiles,
		ContentBlocks: h.ContentBlocks,
		ContentLines:  h.ContentLines,
		TickInterval:  int64(h.TickInterval),
	}
}

// Integrity hashes a capture body with the header's integrity field zeroed, so
// the value covers everything except itself.
func Integrity(cap SharedCapture) (uint64, error) {
	cap.Header.Integrity = 0
	body, err := json.Marshal(cap)
	if err != nil {
		return 0, fmt.Errorf("capture encode: %w", err)
	}
	h := fnv.New64a()
	_, _ = h.Write(body)
	return h.Sum64(), nil
}

// EncodeCapture renders a capture in the bounded compressed wire envelope.
func EncodeCapture(cap SharedCapture) ([]byte, error) { return EncodeJSON(cap) }

// DecodeCapture parses what EncodeCapture produced. It validates nothing: the
// caller passes the result to VerifyCapture or InstallShared.
func DecodeCapture(b []byte) (SharedCapture, error) {
	var cap SharedCapture
	if err := DecodeJSON(b, &cap); err != nil {
		return SharedCapture{}, fmt.Errorf("capture decode: %w", err)
	}
	return cap, nil
}

// Package journal reads recorded runs back from vif-jrn JSONL files.
//
// Leaf over internal/event: a reader must not depend on the runtime it feeds.
// Load accepts several files so a rotated set reassembles by jseq.
package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// Set is one parsed journal: its anchors in emission order, and its records in
// jseq order with duplicates from overlapping files removed
type Set struct {
	Anchors []event.JournalAnchor
	Records []event.JournalRecord
}

// line is the envelope every vlog record shares; non-journal lines carry no sub
type line struct {
	Sub    string          `json:"sub"`
	Fields json.RawMessage `json:"fields"`
}

type recordFields struct {
	Origin    string `json:"origin"`
	Domain    string `json:"domain"`
	Ev        string `json:"ev"`
	Payload   string `json:"payload"`
	EncodeErr string `json:"encode_err"`
	JSeq      uint64 `json:"jseq"`
	Seq       uint64 `json:"seq"`
	Run       uint64 `json:"jrun"`
	Tick      uint64 `json:"jtick"`
	Boundary  uint64 `json:"boundary"`
}

type anchorFields struct {
	Speed         string `json:"speed"`
	ConfigID      string `json:"config_id"`
	ContentID     string `json:"content_id"`
	ContentPin    string `json:"content_pin"`
	Schema        uint64 `json:"schema"`
	Seed          uint64 `json:"seed"`
	Session       uint64 `json:"session"`
	JSeq          uint64 `json:"jseq"`
	Run           uint64 `json:"jrun"`
	Tick          uint64 `json:"jtick"`
	StartRun      uint64 `json:"start_run"`
	StartTick     uint64 `json:"start_tick"`
	ContentFiles  uint64 `json:"content_files"`
	ContentBlocks uint64 `json:"content_blocks"`
	ContentLines  uint64 `json:"content_lines"`
	TickInterval  int64  `json:"tick_ns"`
	Slot          uint64 `json:"slot"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	MapWidth      int    `json:"map_w"`
	MapHeight     int    `json:"map_h"`
	CropOnResize  bool   `json:"crop_on_resize"`
}

// Replicated returns the records that must appear identically in every instance's
// journal of one seed: the transported set of D-10. Anchors are dropped — they
// describe the file, not the run's shared state.
func (s Set) Replicated() []event.JournalRecord {
	out := make([]event.JournalRecord, 0, len(s.Records))
	for _, r := range s.Records {
		if r.Replicated() {
			out = append(out, r)
		}
	}
	return out
}

// Load reads one or more journal files into a single set. The event registry must
// be initialised first: event names resolve through it.
func Load(paths ...string) (Set, error) {
	var s Set
	for _, p := range paths {
		if err := s.readFile(p); err != nil {
			return Set{}, err
		}
	}
	if len(s.Records) == 0 {
		return s, fmt.Errorf("journal: no records in %v", paths)
	}
	slices.SortStableFunc(s.Records, func(a, b event.JournalRecord) int {
		return int(a.JSeq) - int(b.JSeq)
	})
	s.Records = slices.CompactFunc(s.Records, func(a, b event.JournalRecord) bool {
		return a.JSeq == b.JSeq
	})
	return s, nil
}

// readFile appends one file's anchors and records, skipping lines that are not ours
func (s *Set) readFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20) // a payload can be large
	for n := 1; sc.Scan(); n++ {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // heartbeat or foreign line
		}
		switch l.Sub {
		case event.SubJournalRecord:
			r, err := decodeRecord(l.Fields)
			if err != nil {
				return fmt.Errorf("%s:%d: %w", path, n, err)
			}
			s.Records = append(s.Records, r)
		case event.SubJournalAnchor:
			a, err := decodeAnchor(l.Fields)
			if err != nil {
				return fmt.Errorf("%s:%d: %w", path, n, err)
			}
			s.Anchors = append(s.Anchors, a)
		}
	}
	return sc.Err()
}

func decodeRecord(raw json.RawMessage) (event.JournalRecord, error) {
	var f recordFields
	if err := json.Unmarshal(raw, &f); err != nil {
		return event.JournalRecord{}, err
	}
	et, ok := event.GetEventType(f.Ev)
	if !ok {
		return event.JournalRecord{}, fmt.Errorf("jseq %d: unknown event %q", f.JSeq, f.Ev)
	}
	origin, ok := event.ParseOrigin(f.Origin)
	if !ok {
		return event.JournalRecord{}, fmt.Errorf("jseq %d: unknown origin %q", f.JSeq, f.Origin)
	}
	domain, ok := core.ParseDomain(f.Domain)
	if !ok {
		return event.JournalRecord{}, fmt.Errorf("jseq %d: unknown domain %q", f.JSeq, f.Domain)
	}
	return event.JournalRecord{
		Payload: f.Payload, EncodeErr: f.EncodeErr,
		JSeq: f.JSeq, Seq: f.Seq,
		Run: f.Run, Tick: f.Tick, Boundary: f.Boundary,
		Type: et, Origin: origin, Domain: domain,
	}, nil
}

func decodeAnchor(raw json.RawMessage) (event.JournalAnchor, error) {
	var f anchorFields
	if err := json.Unmarshal(raw, &f); err != nil {
		return event.JournalAnchor{}, err
	}
	return event.JournalAnchor{
		Speed: f.Speed, ConfigID: f.ConfigID,
		ContentID: f.ContentID, ContentPin: f.ContentPin,
		Schema: f.Schema, Seed: f.Seed, Session: f.Session, JSeq: f.JSeq,
		Run: f.Run, Tick: f.Tick, StartRun: f.StartRun, StartTick: f.StartTick,
		ContentFiles: f.ContentFiles, ContentBlocks: f.ContentBlocks, ContentLines: f.ContentLines,
		TickInterval: f.TickInterval, Width: f.Width, Height: f.Height,
		MapWidth: f.MapWidth, MapHeight: f.MapHeight, CropOnResize: f.CropOnResize,
		Slot: f.Slot,
	}, nil
}

// CheckDense reports the first jseq gap; a set that starts mid-stream reports its
// first jseq rather than a gap at 1
func (s Set) CheckDense() error {
	if len(s.Records) == 0 {
		return nil
	}
	for i, want := 0, s.Records[0].JSeq; i < len(s.Records); i, want = i+1, want+1 {
		if s.Records[i].JSeq != want {
			return fmt.Errorf("journal gap: jseq %d follows %d", s.Records[i].JSeq, want-1)
		}
	}
	return nil
}

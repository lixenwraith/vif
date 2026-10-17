package event

import (
	"encoding/base64"
	"strconv"

	"github.com/lixenwraith/vif/internal/vlog"
)

// Journal record subs; the offline verifier filters on these
const (
	SubJournalRecord  = "journal"
	SubJournalAnchor  = "anchor"
	SubJournalCapture = "capture"
	SubJournalDigest  = "digest"
	SubJournalEnd     = "end"
)

// vlogSink writes journal output to the dedicated vlog journal session, each
// record's envelope stamped by the run's own log handle.
type vlogSink struct{ log *vlog.Log }

// VlogSink returns the sink backed by the vlog journal file, stamping through l.
func VlogSink(l *vlog.Log) JournalSink { return vlogSink{log: l} }

// Record writes one event record; every argument is an immutable value, as
// vlog formats asynchronously on its own goroutine. encode_err and crossing are
// written only when set: most records are neither broken nor an own crossing.
func (s vlogSink) Record(r JournalRecord) {
	kv := []any{
		"jseq", r.JSeq,
		"seq", r.Seq,
		"jrun", r.Run,
		"jtick", r.Tick,
		"boundary", r.Boundary,
		"origin", r.Origin.String(),
		"domain", r.Domain.String(),
		"ev", GetEventName(r.Type),
		"payload", r.Payload,
	}
	if r.EncodeErr != "" {
		kv = append(kv, "encode_err", r.EncodeErr)
	}
	if r.Crossing != 0 {
		kv = append(kv, "crossing", r.Crossing)
	}
	s.log.Journal(SubJournalRecord, kv...)
}

// Anchor writes one header record
func (s vlogSink) Anchor(a JournalAnchor) {
	s.log.Journal(SubJournalAnchor,
		"schema", a.Schema,
		"jseq", a.JSeq,
		"jrun", a.Run,
		"jtick", a.Tick,
		"start_run", a.StartRun,
		"start_tick", a.StartTick,
		"seed", a.Seed,
		"session", a.Session,
		"scenario_id", a.ScenarioID,
		"scenario_digest", a.ScenarioDigest,
		"content_id", a.ContentID,
		"content_pin", a.ContentPin,
		"content_files", a.ContentFiles,
		"content_blocks", a.ContentBlocks,
		"content_lines", a.ContentLines,
		"tick_ns", a.TickInterval,
		"width", a.Width,
		"height", a.Height,
		"map_w", a.MapWidth,
		"map_h", a.MapHeight,
		"crop_on_resize", a.CropOnResize,
		"cell_aspect", a.CellAspect,
		"session_shared", a.SessionShared,
		"slot", a.Slot,
		"speed", a.Speed)
}

// Capture writes one installed world, its body base64 in a JSON string
func (s vlogSink) Capture(c JournalCapture) {
	s.log.Journal(SubJournalCapture,
		"jseq", c.JSeq,
		"cseq", c.CSeq,
		"jrun", c.Run,
		"jtick", c.Tick,
		"boundary", c.Boundary,
		"participant", uint64(c.Participant),
		"authority", uint64(c.Authority),
		"body", base64.StdEncoding.EncodeToString(c.Body))
}

// Digest writes one world digest, its hashes in hex as a snapshot line carries them
func (s vlogSink) Digest(d JournalDigest) {
	s.log.Journal(SubJournalDigest, "jrun", d.Run, "jtick", d.Tick,
		"positions", strconv.FormatUint(d.Positions, 16),
		"kinetics", strconv.FormatUint(d.Kinetics, 16),
		"combat", strconv.FormatUint(d.Combat, 16),
		"entities", strconv.FormatUint(d.Entities, 16))
}

func (s vlogSink) Finish(st Stamp) {
	s.log.Journal(SubJournalEnd, "jrun", st.Run, "jtick", st.Tick, "boundary", st.Boundary)
}

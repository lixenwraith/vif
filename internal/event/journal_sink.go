package event

import (
	"encoding/base64"

	"github.com/lixenwraith/vif/internal/vlog"
)

// Journal record subs; the offline verifier filters on these
const (
	SubJournalRecord  = "journal"
	SubJournalAnchor  = "anchor"
	SubJournalCapture = "capture"
)

// vlogSink writes journal output to the dedicated vlog journal session
type vlogSink struct{}

// VlogSink returns the sink backed by the vlog journal file
func VlogSink() JournalSink { return vlogSink{} }

// Record writes one event record; every argument is an immutable value, as
// vlog formats asynchronously on its own goroutine. encode_err is written only
// when set: it is empty on all but a broken record.
func (vlogSink) Record(r JournalRecord) {
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
	vlog.Journal(SubJournalRecord, kv...)
}

// Anchor writes one header record
func (vlogSink) Anchor(a JournalAnchor) {
	vlog.Journal(SubJournalAnchor,
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
		"session_shared", a.SessionShared,
		"slot", a.Slot,
		"speed", a.Speed)
}

// Capture writes one installed world, its body base64 in a JSON string
func (vlogSink) Capture(c JournalCapture) {
	vlog.Journal(SubJournalCapture,
		"jseq", c.JSeq,
		"jrun", c.Run,
		"jtick", c.Tick,
		"boundary", c.Boundary,
		"participant", uint64(c.Participant),
		"authority", uint64(c.Authority),
		"body", base64.StdEncoding.EncodeToString(c.Body))
}

package event

import "github.com/lixenwraith/vi-fighter/internal/vlog"

// Journal record subs; the offline verifier filters on these
const (
	SubJournalRecord = "journal"
	SubJournalAnchor = "anchor"
)

// vlogSink writes journal output to the dedicated vlog journal session
type vlogSink struct{}

// VlogSink returns the sink backed by the vlog journal file
func VlogSink() JournalSink { return vlogSink{} }

// Record writes one event record; every argument is an immutable value, as
// vlog formats asynchronously on its own goroutine
func (vlogSink) Record(r JournalRecord) {
	vlog.Journal(SubJournalRecord,
		"jseq", r.JSeq,
		"seq", r.Seq,
		"origin", r.Origin.String(),
		"ev", GetEventName(r.Type),
		"payload", r.Payload,
		"encode_err", r.EncodeErr)
}

// Anchor writes one header record
func (vlogSink) Anchor(a JournalAnchor) {
	vlog.Journal(SubJournalAnchor,
		"schema", a.Schema,
		"jseq", a.JSeq,
		"seed", a.Seed,
		"session", a.Session,
		"config_id", a.ConfigID,
		"content_id", a.ContentID,
		"content_pin", a.ContentPin,
		"content_files", a.ContentFiles,
		"content_blocks", a.ContentBlocks,
		"content_lines", a.ContentLines,
		"tick_ns", a.TickInterval,
		"width", a.Width,
		"height", a.Height,
		"speed", a.Speed)
}

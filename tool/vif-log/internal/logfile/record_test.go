package logfile

import (
	"strings"
	"testing"
)

// parse runs one line through the index and the record parser the way the
// viewer does, so the two discriminator paths cannot drift apart.
func parse(t *testing.T, line string) (*Record, Meta, *Index) {
	t.Helper()
	x := indexFile(t, line)
	m := x.Metas()[0]
	var r Record
	r.Parse(m, []byte(line))
	return &r, m, x
}

func TestRecordNamesJournalRecordsByEvent(t *testing.T) {
	r, m, x := parse(t, jrn(7, 3, "player", "EventNuggetJumpRequest", ""))

	if got := r.Msg(); got != "EventNuggetJumpRequest" {
		t.Errorf("Msg() = %q, want the event name", got)
	}
	// The list column comes from the index, the search column from the parse:
	// they must agree, or /msg matches something other than what is drawn.
	if got := x.MsgName(m.Msg); got != r.Msg() {
		t.Errorf("index msg %q != parsed msg %q", got, r.Msg())
	}
	// ev is the discriminator, so it is spent and must not repeat in fields.
	if fields := r.FieldsText(); strings.Contains(fields, "ev=") {
		t.Errorf("fields repeat the discriminator: %q", fields)
	}
	if got := r.Domain(); got != DomPlayer {
		t.Errorf("Domain() = %v, want DomPlayer", got)
	}
}

func TestRecordNamesTheAnchorByItsSub(t *testing.T) {
	r, _, _ := parse(t, anchorLine)
	if got := r.Msg(); got != SubAnchor {
		t.Errorf("Msg() = %q, want %q", got, SubAnchor)
	}
	if got := string(r.ColumnBytes(ColMsg)); got != SubAnchor {
		t.Errorf("ColMsg = %q, want %q", got, SubAnchor)
	}
	// The anchor has no discriminator field, so nothing is dropped from fields.
	if fields := r.FieldsText(); !strings.Contains(fields, "schema=9") {
		t.Errorf("anchor fields = %q, want the schema", fields)
	}
}

func TestFieldsTextFlattensMultiLineValues(t *testing.T) {
	// A journal payload is TOML: it carries newlines, and one row of the list
	// is one line of the terminal.
	r, _, _ := parse(t, jrn(1, 1, "shared", "EventLevelSetup", `x = 1\ny = 2\n`))

	fields := r.FieldsText()
	if strings.ContainsAny(fields, "\n\r\t") {
		t.Errorf("fields column carries a control character: %q", fields)
	}
	if !strings.Contains(fields, "payload=x = 1 y = 2") {
		t.Errorf("payload not flattened in place: %q", fields)
	}
	// Search scans the same text the list draws.
	if strings.ContainsAny(string(r.ColumnBytes(ColAll)), "\n\r") {
		t.Error("ColAll carries a control character")
	}
	// The detail pane wraps on the real newlines, so Display keeps them.
	f, ok := r.Get("payload")
	if !ok {
		t.Fatal("payload field missing")
	}
	if got := r.Display(f); got != "x = 1\ny = 2\n" {
		t.Errorf("Display(payload) = %q, want the unflattened value", got)
	}
}

func TestFollowValueSkipsTheDiscriminator(t *testing.T) {
	// f/F group records that look alike; for the journal that is the origin,
	// since ev has already become the msg column.
	r, _, _ := parse(t, jrn(1, 1, "shared", "EventA", ""))
	if got := r.FollowValue(); got != "input" {
		t.Errorf("FollowValue() = %q, want the origin", got)
	}
}

func TestAnchorTickIntervalRendersAsADuration(t *testing.T) {
	r, _, _ := parse(t, anchorLine)
	f, ok := r.Get("tick_ns")
	if !ok {
		t.Fatal("tick_ns missing")
	}
	if got := r.Display(f); got != "50ms" {
		t.Errorf("Display(tick_ns) = %q, want 50ms", got)
	}
}

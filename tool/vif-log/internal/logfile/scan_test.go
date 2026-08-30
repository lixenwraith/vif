package logfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The fixtures below are verbatim vlog output: the four context keys the logger
// is built with, then the record's own fields object. A change to the game's
// sink shows up here as a failing test rather than as an empty column.
const (
	anchorLine = `{"time":"2026-08-30T01:27:39.654615818Z","level":"INFO","sub":"anchor",` +
		`"run":0,"tick":0,"frame":0,"fields":{"schema":9,"jseq":0,"jrun":0,"jtick":0,` +
		`"start_run":0,"start_tick":0,"seed":1788052523337853842,"session":3,` +
		`"config_id":"cfg-abc","content_id":"content-xyz","content_pin":"",` +
		`"content_files":5,"content_blocks":191,"content_lines":847,"tick_ns":50000000,` +
		`"width":147,"height":37,"map_w":144,"map_h":34,"crop_on_resize":false,` +
		`"slot":0,"speed":"1"}}`

	statLine = `{"time":"2026-08-29T21:15:33.450821414-04:00","level":"INFO","sub":"stat",` +
		`"run":0,"tick":200,"frame":625,"fields":{"msg":"adapt","graphs":0,"populations":0}}`

	procLine = `{"time":"2026-08-29T21:15:23.33805528-04:00","level":"PROC","run":0,"tick":0,` +
		`"frame":0,"fields":{"type":"proc","sequence":1,"processed_logs":0}}`
)

// jrn builds one journal record line the way internal/event's vlog sink does.
func jrn(jseq uint64, tick uint32, domain, ev, payload string) string {
	return `{"time":"2026-08-30T01:27:39.65473` + string(rune('0'+jseq%10)) +
		`Z","level":"INFO","sub":"journal","run":0,"tick":` + itoa(uint64(tick)) +
		`,"frame":0,"fields":{"jseq":` + itoa(jseq) + `,"seq":` + itoa(jseq*2) +
		`,"jrun":0,"jtick":` + itoa(uint64(tick)) + `,"boundary":0,"origin":"input",` +
		`"domain":"` + domain + `","ev":"` + ev + `","payload":"` + payload +
		`","encode_err":""}}`
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// indexFile writes the lines to a temp file and waits for the scan to finish.
func indexFile(t *testing.T, lines ...string) *Index {
	t.Helper()
	p := filepath.Join(t.TempDir(), "vif-jrn-test.jsonl")
	var buf []byte
	for _, l := range lines {
		buf = append(buf, l...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(p, buf, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	x, err := Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, done := x.Progress(); done && x.Len() == len(lines) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan did not finish: %d of %d rows", x.Len(), len(lines))
		}
		time.Sleep(time.Millisecond)
	}
	if err := x.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return x
}

func TestScanIndexesJournalDomainAndEvent(t *testing.T) {
	x := indexFile(t,
		anchorLine,
		jrn(1, 1, "shared", "EventCleanerDirectionalRequest", ""),
		jrn(2, 2, "player", "EventNuggetJumpRequest", ""),
		statLine,
	)
	m := x.Metas()

	if !x.HasDomains() {
		t.Fatal("HasDomains false for a journal")
	}
	want := []Domain{DomNone, DomShared, DomPlayer, DomNone}
	for i, w := range want {
		if m[i].Dom != w {
			t.Errorf("row %d domain = %v, want %v", i, m[i].Dom, w)
		}
	}

	// The journal carries no msg; ev is what names the record.
	if got := x.MsgName(m[1].Msg); got != "EventCleanerDirectionalRequest" {
		t.Errorf("journal msg = %q, want the event name", got)
	}
	// The anchor carries neither, so its sub stands in as its discriminator.
	if got := x.MsgName(m[0].Msg); got != SubAnchor {
		t.Errorf("anchor msg = %q, want %q", got, SubAnchor)
	}
	if m[0].Flags&FlagAnchor == 0 || !m[0].Landmark() {
		t.Error("anchor is not flagged as a landmark")
	}
	if m[1].Flags&FlagAnchor != 0 {
		t.Error("journal record wrongly flagged as an anchor")
	}
	// A stat snapshot head is the other landmark, and still is.
	if !m[3].Landmark() || m[3].Flags&FlagSnapHead == 0 {
		t.Error("stat record lost its snapshot head")
	}
}

func TestScanLeavesDiagnosticRecordsUntouched(t *testing.T) {
	x := indexFile(t, statLine, procLine)
	m := x.Metas()

	if x.HasDomains() {
		t.Error("HasDomains true for a log with no journal records")
	}
	if got := x.MsgName(m[0].Msg); got != "adapt" {
		t.Errorf("stat msg = %q, want adapt", got)
	}
	// The logger self-report has no msg; type is its discriminator, as before.
	if got := x.MsgName(m[1].Msg); got != "proc" {
		t.Errorf("proc msg = %q, want proc", got)
	}
	if m[1].Lvl != LevelProc {
		t.Errorf("proc level = %v, want LevelProc", m[1].Lvl)
	}
	if x.JournalGaps() != 0 {
		t.Errorf("JournalGaps = %d for a diagnostic log", x.JournalGaps())
	}
}

func TestScanCountsJournalSeqGaps(t *testing.T) {
	// jseq is dense by construction, so 1,2,5,6 is one break: three records the
	// writer dropped, reported as the single discontinuity they are.
	x := indexFile(t,
		anchorLine,
		jrn(1, 1, "shared", "EventA", ""),
		jrn(2, 1, "shared", "EventB", ""),
		jrn(5, 2, "player", "EventC", ""),
		jrn(6, 2, "player", "EventD", ""),
	)
	if got := x.JournalGaps(); got != 1 {
		t.Errorf("JournalGaps = %d, want 1", got)
	}
}

func TestScanIgnoresGapsAcrossTheAnchor(t *testing.T) {
	// The anchor's own jseq is the count at emission, not a record number; it
	// must not be read as a step in the record run.
	x := indexFile(t,
		jrn(1, 1, "shared", "EventA", ""),
		anchorLine, // jseq 0
		jrn(2, 1, "shared", "EventB", ""),
	)
	if got := x.JournalGaps(); got != 0 {
		t.Errorf("JournalGaps = %d, want 0", got)
	}
}

func TestScanKeepsMalformedLines(t *testing.T) {
	x := indexFile(t, jrn(1, 1, "shared", "EventA", ""), `{"time":`, statLine)
	if x.Malformed() != 1 {
		t.Errorf("Malformed = %d, want 1", x.Malformed())
	}
	if x.Metas()[1].Flags&FlagMalformed == 0 {
		t.Error("truncated line not flagged")
	}
}

func TestParseMetaDoesNotAllocate(t *testing.T) {
	// The index pass runs once per line of a multi-gigabyte file, so it holds
	// no line bytes and takes no heap. Journal records — with a domain, a jseq
	// and an event name — must be no more expensive than the rest.
	subN, msgN := newInterner(), newInterner()
	for _, tc := range []struct{ name, line string }{
		{"journal", jrn(1, 1, "player", "EventNuggetJumpRequest", `x = 1\ny = 2\n`)},
		{"anchor", anchorLine},
		{"stat", statLine},
		{"proc", procLine},
	} {
		line := []byte(tc.line)
		parseMeta(line, 0, 0, subN, msgN) // warm the interners
		got := testing.AllocsPerRun(100, func() {
			parseMeta(line, 0, 0, subN, msgN)
		})
		if got != 0 {
			t.Errorf("%s: parseMeta allocates %.0f times per line", tc.name, got)
		}
	}
}

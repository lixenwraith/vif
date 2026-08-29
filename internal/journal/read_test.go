package journal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// write lays one JSONL file down in a temp dir and returns its path
func write(t *testing.T, name string, lines ...string) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), name)
	var buf []byte
	for _, l := range lines {
		buf = append(buf, l...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(p, buf, 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// record builds one journal line; the field names are the sink's own
func record(jseq uint64, ev, domain, origin string) string {
	return `{"sub":"journal","fields":{"jseq":` + itoa(jseq) +
		`,"seq":` + itoa(jseq) + `,"jrun":0,"jtick":1,"boundary":0,"origin":"` + origin +
		`","domain":"` + domain + `","ev":"` + ev + `","payload":"","encode_err":""}}`
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

const anchorLine = `{"sub":"anchor","fields":{"schema":9,"jseq":0,"jrun":0,"jtick":0,` +
	`"start_run":0,"start_tick":0,"seed":42,"session":3,"config_id":"cfg","content_id":"c",` +
	`"content_pin":"","content_files":1,"content_blocks":2,"content_lines":3,` +
	`"tick_ns":16000000,"width":120,"height":40,"map_w":100,"map_h":30,` +
	`"crop_on_resize":true,"slot":0,"speed":"1x"}}`

// TestLoadRoundTripsRecordsAndAnchor covers the reader's whole contract: the
// envelope, the field names the sink emits, and the enum parses that a
// core.DomainNames or Origin rename would silently break.
func TestLoadRoundTripsRecordsAndAnchor(t *testing.T) {
	event.EnsureRegistry()

	p := write(t, "a.jsonl",
		anchorLine,
		`{"sub":"heartbeat","fields":{}}`, // a foreign line is skipped, not an error
		record(1, "EventLevelSetup", "shared", "command"),
		record(2, "EventCharacterTyped", "player", "input"),
	)

	s, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Records) != 2 {
		t.Fatalf("got %d records, want 2", len(s.Records))
	}
	if len(s.Anchors) != 1 {
		t.Fatalf("got %d anchors, want 1", len(s.Anchors))
	}

	if a := s.Anchors[0]; a.Schema != event.JournalSchema || a.Seed != 42 ||
		a.Session != 3 || a.Width != 120 || a.Height != 40 || a.Speed != "1x" ||
		a.MapWidth != 100 || a.MapHeight != 30 || !a.CropOnResize {
		t.Errorf("anchor round-trip lost fields: %+v", a)
	}

	if got := s.Records[0]; got.Type != event.EventLevelSetup ||
		got.Domain != core.DomainShared || got.Tick != 1 {
		t.Errorf("record 0 round-trip: %+v", got)
	}
	if got := s.Records[1]; got.Domain != core.DomainPlayer {
		t.Errorf("record 1 domain = %v, want player", got.Domain)
	}
}

// TestLoadReassemblesRotatedFilesByJSeq asserts the property Load exists for:
// a rotated set overlaps, and the overlap must collapse rather than duplicate.
func TestLoadReassemblesRotatedFilesByJSeq(t *testing.T) {
	event.EnsureRegistry()

	first := write(t, "1.jsonl", anchorLine,
		record(1, "EventLevelSetup", "shared", "command"),
		record(2, "EventCharacterTyped", "player", "input"))
	second := write(t, "2.jsonl", anchorLine,
		record(2, "EventCharacterTyped", "player", "input"), // overlap
		record(3, "EventDeleteRequest", "player", "input"))

	s, err := Load(second, first) // out of order on purpose
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Records) != 3 {
		t.Fatalf("got %d records, want 3 after dedupe", len(s.Records))
	}
	for i, r := range s.Records {
		if r.JSeq != uint64(i+1) {
			t.Fatalf("record %d has jseq %d; set is not in jseq order", i, r.JSeq)
		}
	}
}

// TestReplicatedSelectsTheTransportedSet checks the D-10 filter over a set that
// mixes all four classes, including a Stamped type resolving both ways.
func TestReplicatedSelectsTheTransportedSet(t *testing.T) {
	event.EnsureRegistry()

	p := write(t, "c.jsonl", anchorLine,
		record(1, "EventLevelSetup", "shared", "command"),               // shared: kept
		record(2, "EventCharacterTyped", "player", "input"),             // local: dropped
		record(3, "EventExplosionRequest", "player", "system"),          // bus: kept
		record(4, "EventCombatAttackDirectRequest", "shared", "system"), // stamped shared: kept
		record(5, "EventCombatAttackDirectRequest", "player", "system"), // stamped player: dropped
	)

	s, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var got []uint64
	for _, r := range s.Replicated() {
		got = append(got, r.JSeq)
	}
	want := []uint64{1, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("replicated jseqs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("replicated jseqs = %v, want %v", got, want)
		}
	}
}

// TestLoadRejectsUnknownNames fails loudly on a name the registry no longer
// carries, which is the failure a DomainNames or event rename actually causes.
func TestLoadRejectsUnknownNames(t *testing.T) {
	event.EnsureRegistry()

	for _, tc := range []struct{ name, line string }{
		{"event", record(1, "EventNoSuchThing", "shared", "command")},
		{"domain", record(1, "EventLevelSetup", "sideways", "command")},
		{"origin", record(1, "EventLevelSetup", "shared", "nowhere")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(write(t, "bad.jsonl", anchorLine, tc.line)); err == nil {
				t.Fatalf("unknown %s accepted", tc.name)
			}
		})
	}
}

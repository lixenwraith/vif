//go:build !wasm && !novlog

package vlog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigureKeepsJournalDirectoryIndependent(t *testing.T) {
	Configure(Config{Dir: "logs", JournalDir: "journals"})
	mu.Lock()
	got := cfg
	mu.Unlock()
	if got.Dir != "logs" || got.JournalDir != "journals" {
		t.Fatalf("configured directories = %q, %q", got.Dir, got.JournalDir)
	}

	Configure(Config{Dir: "shared"})
	mu.Lock()
	got = cfg
	mu.Unlock()
	if got.JournalDir != "shared" {
		t.Fatalf("default journal directory = %q, want shared", got.JournalDir)
	}
}

func TestSessionIDTagsApplicationRecords(t *testing.T) {
	dir := t.TempDir()
	Configure(Config{Dir: dir, Level: "info", SessionID: "abc123"})
	path, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(path); got != "abc123.jsonl" {
		t.Fatalf("log filename = %q, want abc123.jsonl", got)
	}
	Info("app", "msg", "session test", "answer", 42)
	Shutdown(time.Second)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var record struct {
			Sub    string         `json:"sub"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		if record.Sub != "app" || record.Fields["msg"] != "session test" {
			continue
		}
		if got := record.Fields["session_id"]; got != "abc123" {
			t.Fatalf("fields.session_id = %#v, want abc123", got)
		}
		return
	}
	t.Fatal("session test record not found")
}

func TestSessionIDOmittedWhenUnset(t *testing.T) {
	args := []any{"msg", "plain"}
	Configure(Config{})
	got := sessionArgs(args)
	if len(got) != len(args) {
		t.Fatalf("sessionArgs added %d values without a session ID", len(got)-len(args))
	}
}

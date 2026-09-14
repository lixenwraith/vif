//go:build !wasm && !novlog

package vlog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/log"
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

func TestSessionIDTagsEveryApplicationRecord(t *testing.T) {
	dir := t.TempDir()
	Configure(Config{Dir: dir, Level: "info", Scope: "all", SessionID: "abc123"})
	path, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Shutdown(time.Second) })
	if got := filepath.Base(path); got != "abc123.jsonl" {
		t.Fatalf("log filename = %q, want abc123.jsonl", got)
	}
	Info("app", "msg", "session test", "answer", 42)
	if _, err := EmitSet("stat", 7, 8, func(emit func(args ...any)) {
		emit("msg", "status test", "value", true)
	}); err != nil {
		t.Fatal(err)
	}
	Shutdown(time.Second)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	applicationRecords := 0
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
		if record.Sub == "" {
			continue
		}
		applicationRecords++
		if !bytes.Contains(line, []byte(`"fields":{"msg":`)) {
			t.Errorf("fields.msg is not the first payload key: %s", line)
		}
		if got := record.Fields["session_id"]; got != "abc123" {
			t.Errorf("fields.session_id = %#v, want abc123 in %s", got, line)
		}
	}
	if applicationRecords != 2 {
		t.Fatalf("application records = %d, want 2", applicationRecords)
	}
}

func TestSessionIDOmittedWhenUnset(t *testing.T) {
	dir := t.TempDir()
	Configure(Config{Dir: dir, Level: "info", Scope: "all"})
	path, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Shutdown(time.Second) })
	if _, err := time.Parse("vif-log-060102-150405.jsonl", filepath.Base(path)); err != nil {
		t.Fatalf("ordinary log filename = %q: %v", filepath.Base(path), err)
	}
	loggerConfig := sink.Load().GetConfig()
	if loggerConfig.MaxSizeKB != maxSizeMB*1000 ||
		loggerConfig.MaxTotalSizeKB != maxTotalSizeMB*1000 ||
		loggerConfig.MinDiskFreeKB != minDiskFreeMB*1000 ||
		loggerConfig.RetentionPeriodHrs != retentionHrs {
		t.Fatalf("ordinary logger policy changed: %+v", loggerConfig)
	}

	Info("app", "msg", "plain", "answer", 42)
	Shutdown(time.Second)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		var sub string
		if raw := record["sub"]; raw != nil {
			if err := json.Unmarshal(raw, &sub); err != nil {
				t.Fatal(err)
			}
		}
		if sub != "app" {
			continue
		}
		for _, key := range []string{"time", "level", "sub", "run", "tick", "fields"} {
			if _, ok := record[key]; !ok {
				t.Errorf("ordinary record omitted %q: %s", key, line)
			}
		}
		if len(record) != 6 {
			t.Errorf("ordinary record has %d top-level keys, want 6: %s", len(record), line)
		}
		var fields map[string]any
		if err := json.Unmarshal(record["fields"], &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["session_id"]; ok {
			t.Errorf("ordinary record has fields.session_id: %s", line)
		}
		return
	}
	t.Fatal("ordinary application record not found")
}

func TestCommissionedLoggersRotateOnlyTheirOwnFiles(t *testing.T) {
	dir := t.TempDir()
	Configure(Config{Dir: dir, Level: "info", SessionID: "alpha"})
	first, firstPath, err := buildLogger(dir, "alpha", "info", false)
	if err != nil {
		t.Fatal(err)
	}
	Configure(Config{Dir: dir, Level: "info", SessionID: "beta"})
	second, secondPath, err := buildLogger(dir, "beta", "info", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, logger := range []*log.Logger{first, second} {
		loggerConfig := logger.GetConfig()
		if loggerConfig.MaxSizeKB != commissionedMaxSizeMB*1000 ||
			loggerConfig.MaxTotalSizeKB != 0 || loggerConfig.MinDiskFreeKB != 0 ||
			loggerConfig.RetentionPeriodHrs != 0 {
			t.Fatalf("commissioned logger policy = %+v", loggerConfig)
		}
		loggerConfig.MaxSizeKB = 1
		loggerConfig.HeartbeatLevel = 0
		if err := logger.ApplyConfig(loggerConfig); err != nil {
			t.Fatal(err)
		}
		if err := logger.Start(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = second.Shutdown(time.Second)
		_ = first.Shutdown(time.Second)
	})

	first.Info("alpha baseline")
	second.Info("beta baseline")
	if err := first.Flush(time.Second); err != nil {
		t.Fatal(err)
	}
	if err := second.Flush(time.Second); err != nil {
		t.Fatal(err)
	}
	betaBefore, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}

	first.Info(strings.Repeat("a", 2000))
	if err := first.Flush(time.Second); err != nil {
		t.Fatal(err)
	}
	betaAfter, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(betaAfter, betaBefore) {
		t.Fatal("alpha rotation changed beta's active file")
	}
	alphaBefore, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}

	second.Info(strings.Repeat("b", 2000))
	if err := second.Flush(time.Second); err != nil {
		t.Fatal(err)
	}
	alphaAfter, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(alphaAfter, alphaBefore) {
		t.Fatal("beta rotation changed alpha's active file")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	archives := map[string]bool{"alpha": false, "beta": false}
	for _, entry := range entries {
		for id := range archives {
			if strings.HasPrefix(entry.Name(), id+"_") && strings.HasSuffix(entry.Name(), ".jsonl") {
				archives[id] = true
			}
		}
	}
	for id, found := range archives {
		if !found {
			t.Errorf("%s did not rotate its own file", id)
		}
	}
}

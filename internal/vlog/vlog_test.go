//go:build !wasm && !novlog

package vlog

import "testing"

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

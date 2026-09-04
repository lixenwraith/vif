//go:build !wasm && !novlog

package vlog

import (
	"testing"
)

// TestAConsoleSessionNeedsNoDirectory is what makes the stdout sink usable where
// it is needed. A supervised run's filesystem is not where anybody will look for
// its log, and under a read-only root it may not be writable at all — so asking
// for stdout must not also require a directory to have been resolved.
func TestAConsoleSessionNeedsNoDirectory(t *testing.T) {
	Configure(Config{Console: true})
	path, err := Start()
	if err != nil {
		t.Fatalf("a console session refused to start: %v", err)
	}
	t.Cleanup(func() { Shutdown(0) })
	if path != "stdout" {
		t.Fatalf("a console session reports its path as %q, want stdout", path)
	}
	if !Enabled() {
		t.Fatal("a started console session reports itself disabled")
	}
}

// TestAFileSessionStillNeedsOne holds the other half: the two sinks are exclusive,
// and a run that named neither a directory nor stdout has asked for nothing.
func TestAFileSessionStillNeedsOne(t *testing.T) {
	Configure(Config{})
	if _, err := Start(); err == nil {
		Shutdown(0)
		t.Fatal("a session with no directory and no console started anyway")
	}
}

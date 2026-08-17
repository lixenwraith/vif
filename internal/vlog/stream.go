//go:build !wasm && !novlog

package vlog

import (
	"fmt"
	"time"

	"github.com/lixenwraith/log"
)

// streamDrainTimeout bounds the synchronous drain when a stream closes
const streamDrainTimeout = 3 * time.Second

// Stream is a dedicated file with its own lifecycle, independent of the session
// logger's level and scope, for record sets that must not be silenced.
type Stream struct {
	l    *log.Logger
	path string
}

// OpenStream starts a stream file named "<prefix><timestamp>.jsonl" in the
// configured directory. Blocking: performs disk I/O.
func OpenStream(prefix string) (*Stream, error) {
	mu.Lock()
	dir, spawn := cfg.Dir, cfg.Spawn
	mu.Unlock()

	if dir == "" {
		return nil, fmt.Errorf("vlog: no directory configured")
	}

	l, p, err := buildLogger(dir, prefix+time.Now().Format(fileTimeFormat), "trace")
	if err != nil {
		return nil, err
	}
	l.SetErrorHandler(recordInternalError)
	if spawn != nil {
		l.SetSpawn(spawn)
	}
	if err := l.Start(); err != nil {
		_ = l.Shutdown(time.Second)
		return nil, err
	}
	return &Stream{l: l, path: p}, nil
}

// Emit writes one record stamped with the live run, tick and frame values
func (s *Stream) Emit(sub string, args ...any) {
	s.l.LogContext(context(sub), s.l.Flags()|log.FlagKV, LevelInfo, 0, args...)
}

// Path returns the stream's file
func (s *Stream) Path() string { return s.path }

// Close drains and closes the stream
func (s *Stream) Close() error { return s.l.Shutdown(streamDrainTimeout) }

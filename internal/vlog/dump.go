//go:build !wasm && !novlog

package vlog

import (
	"fmt"
	"time"

	"github.com/lixenwraith/log"
)

// dumpTimeout bounds the synchronous drain of a snapshot file
const dumpTimeout = 3 * time.Second

// Dump writes a standalone snapshot file using a second logger instance,
// independent of the session logger's state, level and scopes.
// Blocking: opens, fills, drains and closes before returning. Operator paths only.
func Dump(fill func(emit func(sub string, args ...any))) (string, error) {
	mu.Lock()
	dir, spawn := cfg.Dir, cfg.Spawn
	mu.Unlock()

	if dir == "" {
		return "", fmt.Errorf("vlog: no directory configured")
	}

	name := snapPrefix + time.Now().Format(fileTimeFormat)
	l, p, err := buildLogger(dir, name, "trace")
	if err != nil {
		return "", err
	}
	l.SetErrorHandler(recordInternalError)
	if spawn != nil {
		l.SetSpawn(spawn)
	}
	if err := l.Start(); err != nil {
		_ = l.Shutdown(time.Second)
		return "", err
	}

	// Correlation stamp matches the session so a snapshot joins on run/tick
	fill(func(sub string, args ...any) {
		l.LogContext(context(sub), l.Flags()|log.FlagKV, LevelInfo, 0, args...)
	})

	if err := l.Shutdown(dumpTimeout); err != nil {
		return p, fmt.Errorf("snapshot drain: %w", err)
	}
	return p, nil
}

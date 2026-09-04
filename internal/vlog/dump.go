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
	l, p, err := buildLogger(dir, name, "trace", false)
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

// recPrefix names a standalone flight-recorder file, written only when no
// session log is running
const recPrefix = "vif-rec-"

// recTimeFormat resolves to milliseconds: unlike the session and snapshot
// files, sidecars are trigger-paced and two can land in the same second
const recTimeFormat = "060102-150405.000"

// EmitSet writes a correlated set of records under one explicit stamp, so the
// whole set shares run/tick/frame even when the render goroutine advances the
// frame counter mid-emission. It targets the session log when one is running,
// otherwise a standalone file. Returns the standalone path, empty otherwise.
func EmitSet(sub string, run, tick, frame uint64, fill func(emit func(args ...any))) (string, error) {
	if l := sink.Load(); l != nil {
		if !l.Enabled(LevelInfo) || !scopeEnabled(sub) {
			return "", nil
		}
		emitSet(l, sub, run, tick, frame, fill)
		return "", nil
	}

	mu.Lock()
	dir, spawn := cfg.Dir, cfg.Spawn
	mu.Unlock()
	if dir == "" {
		return "", fmt.Errorf("vlog: no directory configured")
	}

	name := recPrefix + time.Now().Format(recTimeFormat)
	l, p, err := buildLogger(dir, name, "trace", false)
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

	emitSet(l, sub, run, tick, frame, fill)

	if err := l.Shutdown(dumpTimeout); err != nil {
		return p, fmt.Errorf("record drain: %w", err)
	}
	return p, nil
}

// emitSet feeds fill an emitter bound to one context stamp
func emitSet(l *log.Logger, sub string, run, tick, frame uint64, fill func(emit func(args ...any))) {
	ctx := log.Context{Tag: sub, Vals: [log.ContextSlots]uint64{run, tick, frame}}
	flags := l.Flags() | log.FlagKV
	fill(func(args ...any) { l.LogContext(ctx, flags, LevelInfo, 0, args...) })
}

//go:build !wasm && !novlog

// Package vlog is the process-wide logging facade. Leaf package: it imports
// only the standard library and lixenwraith/log, so any vi-fighter package may
// use it without creating a cycle.
//
// ARGUMENT LIFETIME: records are formatted asynchronously on the logger
// goroutine, up to BufferSize records after the call. Pass primitives and
// value copies only — never Store.GetPtr pointers, pooled event payloads, or
// reused scratch slices.
package vlog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/log"
)

// Level constants mirrored from log for call-site brevity
const (
	LevelDebug = log.LevelDebug
	LevelInfo  = log.LevelInfo
	LevelWarn  = log.LevelWarn
	LevelError = log.LevelError
)

// File naming: one JSON object per line, not a JSON document
const (
	filePrefix     = "vif-log-"
	snapPrefix     = "vif-snap-"
	fileTimeFormat = "060102-150405"
	fileExtension  = "jsonl"
)

// Rotation and buffering; tuned after live measurement
const (
	bufferSize      = 8192
	maxSizeMB       = 64
	maxTotalSizeMB  = 512
	minDiskFreeMB   = 100
	flushIntervalMs = 250
	retentionHrs    = 24.0
	heartbeatS      = 60

	crashFlushTimeout = 200 * time.Millisecond
)

const (
	defaultLevel = "debug"
	stopTimeout  = 2 * time.Second
)

// LevelTrace is below debug; reserved for per-emission taps
const LevelTrace = log.LevelTrace

// Config is the resolved logger setup; Dir must be non-empty
type Config struct {
	Spawn func(func()) // goroutine launcher owning panic recovery
	Dir   string
	Level string // debug, info, warn, error; empty means debug
	Scope string // scope spec; empty means all. Pre-validate with ParseScopes
}

var (
	sink  atomic.Pointer[log.Logger]
	path  atomic.Pointer[string]
	level atomic.Int64

	run   atomic.Uint64
	tick  atomic.Uint64
	frame atomic.Uint64

	// lastErr holds the most recent internal diagnostic; the terminal belongs
	// to the game, so it is reported on shutdown instead
	lastErr atomic.Pointer[string]

	mu      sync.Mutex // serializes Start/Stop/Shutdown
	cfg     Config
	closing atomic.Bool
)

// Configure stores the session setup without touching the filesystem.
// Call once at startup so a session can be started later by command.
// Scope is applied best-effort; callers surface errors via ParseScopes first.
func Configure(c Config) {
	mu.Lock()
	defer mu.Unlock()

	if c.Level == "" {
		c.Level = defaultLevel
	}
	cfg = c
	if lv, err := log.Level(c.Level); err == nil {
		level.Store(lv)
	}
	if c.Scope != "" {
		if s, err := ParseScopes(c.Scope, ScopeAll); err == nil {
			scopes.Store(uint32(s))
		}
	}
}

// Init configures and starts a session in one call
func Init(c Config) (string, error) {
	Configure(c)
	return Start()
}

// buildLogger constructs a configured, unstarted logger and its resolved path
func buildLogger(dir, name, levelName string) (*log.Logger, string, error) {
	l, err := log.NewBuilder().
		Directory(dir).
		Name(name).
		Extension(fileExtension).
		Format("json").
		Sanitization(log.PolicyRaw). // json transport escaping is unconditional
		LevelString(levelName).
		EnableFile(true).
		EnableConsole(false). // console writes corrupt the alternate screen
		InternalErrorsToStderr(false).
		BufferSize(bufferSize).
		MaxSizeMB(maxSizeMB).
		MaxTotalSizeMB(maxTotalSizeMB).
		MinDiskFreeMB(minDiskFreeMB).
		FlushIntervalMs(flushIntervalMs).
		EnablePeriodicSync(true).
		RetentionPeriodHrs(retentionHrs).
		HeartbeatLevel(1). // drop and rotation counters, one-way into the log
		HeartbeatIntervalS(heartbeatS).
		ContextKeys("sub", "run", "tick", "frame").
		Build()
	if err != nil {
		return nil, "", err
	}

	p := filepath.Join(dir, name+"."+fileExtension)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return l, p, nil
}

// Start opens a new log file and begins processing, returning its path.
// Each call produces a distinct file. Performs disk I/O: acceptable for a
// startup or an operator command, not for a hot path.
func Start() (string, error) {
	mu.Lock()
	defer mu.Unlock()

	if sink.Load() != nil {
		return currentPath(), fmt.Errorf("vlog: already running")
	}
	if closing.Load() {
		return "", fmt.Errorf("vlog: previous session still draining")
	}
	if cfg.Dir == "" {
		return "", fmt.Errorf("vlog: no directory configured")
	}

	name := filePrefix + time.Now().Format(fileTimeFormat)
	l, p, err := buildLogger(cfg.Dir, name, cfg.Level)
	if err != nil {
		return "", err
	}

	l.SetErrorHandler(recordInternalError)
	if cfg.Spawn != nil {
		l.SetSpawn(cfg.Spawn)
	}
	if err := l.Start(); err != nil {
		_ = l.Shutdown(time.Second)
		return "", err
	}

	// A level set while stopped is honoured by the new session
	l.SetLevel(level.Load())

	path.Store(&p)
	sink.Store(l)
	return p, nil
}

// Stop detaches the sink and drains it off the caller's goroutine, so a
// command handler holding the world lock never waits on disk
func Stop() {
	mu.Lock()
	defer mu.Unlock()

	l := sink.Swap(nil)
	if l == nil {
		return
	}
	path.Store(nil)
	closing.Store(true)

	drain := func() {
		if err := l.Shutdown(stopTimeout); err != nil {
			recordInternalError("shutdown: " + err.Error())
		}
		closing.Store(false)
	}
	if cfg.Spawn != nil {
		cfg.Spawn(drain)
		return
	}
	go drain()
}

// Enabled reports whether a session is running
func Enabled() bool { return sink.Load() != nil }

// Dir returns the configured log directory, empty when unconfigured
func Dir() string {
	mu.Lock()
	defer mu.Unlock()
	return cfg.Dir
}

// Path returns the active log file, empty when stopped
func Path() string { return currentPath() }

func currentPath() string {
	if p := path.Load(); p != nil {
		return *p
	}
	return ""
}

// LevelName returns the current threshold as a display name
func LevelName() string { return log.LevelToString(level.Load()) }

// SetLevel retargets the emit threshold; safe under the world lock
func SetLevel(lv int64) {
	level.Store(lv)
	if l := sink.Load(); l != nil {
		l.SetLevel(lv)
	}
}

// SetLevelName parses and applies a threshold by name
func SetLevelName(name string) error {
	lv, err := log.Level(name)
	if err != nil {
		return err
	}
	SetLevel(lv)
	return nil
}

// Shutdown drains and closes synchronously; for process exit only
func Shutdown(timeout time.Duration) {
	mu.Lock()
	l := sink.Swap(nil)
	path.Store(nil)
	mu.Unlock()

	if l != nil {
		if err := l.Shutdown(timeout); err != nil {
			fmt.Fprintf(os.Stderr, "log shutdown: %v\n", err)
		}
	}

	// Wait out an in-flight command-initiated drain so its tail reaches disk
	deadline := time.Now().Add(timeout)
	for closing.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if p := lastErr.Load(); p != nil {
		fmt.Fprintf(os.Stderr, "log: last internal error: %s\n", *p)
	}
}

// E reports whether a record at level would be written, ignoring scope.
// Prefer On at scoped call sites.
func E(level int64) bool {
	l := sink.Load()
	return l != nil && l.Enabled(level)
}

// On reports whether a record with this sub and level would be written.
// Guard hot call sites with it: the variadic slice is built before the call
// and escapes to the heap.
func On(sub string, level int64) bool {
	l := sink.Load()
	return l != nil && l.Enabled(level) && scopeEnabled(sub)
}

func Debug(sub string, args ...any) { emit(sub, LevelDebug, args) }
func Info(sub string, args ...any)  { emit(sub, LevelInfo, args) }
func Warn(sub string, args ...any)  { emit(sub, LevelWarn, args) }
func Error(sub string, args ...any) { emit(sub, LevelError, args) }

// emit stamps the record with the current correlation values and queues it
// Scopes filter noise, not failures: error and above always emit
func emit(sub string, level int64, args []any) {
	l := sink.Load()
	if l == nil || !l.Enabled(level) {
		return
	}
	if level < LevelError && !scopeEnabled(sub) {
		return
	}
	l.LogContext(context(sub), l.Flags()|log.FlagKV, level, 0, args...)
}

// Trace emits a record carrying a stack trace of depth frames
// Depth is raised by one to cover this wrapper, which appears as the innermost trace entry.
func Trace(sub string, level int64, depth int, args ...any) {
	l := sink.Load()
	if l == nil || !l.Enabled(level) {
		return
	}
	if level < LevelError && !scopeEnabled(sub) {
		return
	}
	l.LogContext(context(sub), l.Flags()|log.FlagKV, level, int64(depth)+1, args...)
}

func context(sub string) log.Context {
	return log.Context{
		Tag:  sub,
		Vals: [log.ContextSlots]uint64{run.Load(), tick.Load(), frame.Load()},
	}
}

// SetRun advances the session counter; owned by the FSM reset path
func SetRun(n uint64) { run.Store(n) }

// SetTick publishes the game tick stamped on subsequent records
func SetTick(n uint64) { tick.Store(n) }

// SetFrame publishes the render frame stamped on subsequent records
func SetFrame(n uint64) { frame.Store(n) }

// CrashHook records a panic and flushes before the host restores the terminal.
// Registered with core.SetCrashHook.
func CrashHook(r any, stack []byte) {
	l := sink.Load()
	if l == nil {
		return
	}
	l.LogContext(context("crash"), l.Flags()|log.FlagKV, LevelError, 0,
		"msg", "panic",
		"panic", fmt.Sprint(r),
		"stack", string(stack))
	_ = l.Flush(crashFlushTimeout)
}

// recordInternalError holds logger diagnostics until shutdown
func recordInternalError(msg string) { lastErr.Store(&msg) }

// NextRun advances the session counter stamped on subsequent records
func NextRun() uint64 { return run.Add(1) }

// Detail emits at the trace level without a stack trace, for per-item taps
// gated by scope rather than by call site. Trace is for call chains.
func Detail(sub string, args ...any) { emit(sub, LevelTrace, args) }

//go:build wasm || novlog

// Package vlog is the process-wide logging facade. This build carries no
// logger: every entry point is a no-op and lixenwraith/log is not linked.
package vlog

import (
	"errors"
	"time"
)

// Level constants mirrored from log for call-site brevity
const (
	LevelTrace int64 = -8
	LevelDebug int64 = -4
	LevelInfo  int64 = 0
	LevelWarn  int64 = 4
	LevelError int64 = 8
)

// Scope mirrors the real build's record categories
type Scope uint32

const (
	ScopeApp Scope = 1 << iota
	ScopeFSM
	ScopeEvent
	ScopeInput
	ScopeStat
	ScopeLock
	ScopeTap
)

const (
	ScopeNone Scope = 0
	ScopeAll        = ScopeApp | ScopeFSM | ScopeEvent | ScopeInput | ScopeStat | ScopeLock | ScopeTap
)

// ErrDisabled reports that this build carries no logger
var ErrDisabled = errors.New("vlog: built without logging support")

// Config mirrors the real build's setup struct
type Config struct {
	Spawn func(func())
	Dir   string
	Level string
	Scope string
}

func Init(Config) (string, error)      { return "", ErrDisabled }
func Configure(Config)                 {}
func Start() (string, error)           { return "", ErrDisabled }
func Stop()                            {}
func Enabled() bool                    { return false }
func Path() string                     { return "" }
func Dir() string                      { return "" }
func LevelName() string                { return "OFF" }
func SetLevelName(string) error        { return ErrDisabled }
func E(int64) bool                     { return false }
func On(string, int64) bool            { return false }
func Debug(string, ...any)             {}
func Info(string, ...any)              {}
func Warn(string, ...any)              {}
func Error(string, ...any)             {}
func SetRun(uint64)                    {}
func SetTick(uint64)                   {}
func SetFrame(uint64)                  {}
func SetLevel(int64)                   {}
func CrashHook(any, []byte)            {}
func Shutdown(time.Duration)           {}
func Trace(string, int64, int, ...any) {}
func NextRun() uint64                  { return 0 }

func ScopeOf(string) Scope                            { return ScopeTap }
func Scopes() Scope                                   { return ScopeNone }
func SetScopes(Scope)                                 {}
func ScopeString(Scope) string                        { return "none" }
func Dump(func(func(string, ...any))) (string, error) { return "", ErrDisabled }

// ParseScopes validates a spec so CLI parsing behaves identically
func ParseScopes(spec string, cur Scope) (Scope, error) { return cur, nil }

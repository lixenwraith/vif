package vlog

import (
	"sync/atomic"
	"time"
)

// Log is one runtime's handle on the process log: the run and tick its records
// carry, the instance tag naming it beside others in one process, and whether it
// writes. A replay copy is muted until it is presented; a seat is tagged.
type Log struct {
	run      atomic.Uint64
	tick     atomic.Uint64
	muted    atomic.Bool
	instance string
}

// NewLog creates a handle at run 0 tick 0; an empty instance adds no tag.
func NewLog(instance string) *Log { return &Log{instance: instance} }

var defaultLog = NewLog("")

// Default is the process's own handle, which the runtime a process runs adopts.
func Default() *Log { return defaultLog }

// SetRun publishes the reset generation stamped on subsequent records.
func (l *Log) SetRun(n uint64) { l.run.Store(n) }

// SetTick publishes the game tick stamped on subsequent records.
func (l *Log) SetTick(n uint64) { l.tick.Store(n) }

// Stamp returns the live run and tick.
func (l *Log) Stamp() (uint64, uint64) { return l.run.Load(), l.tick.Load() }

// Mute withholds this handle's records below error while on.
func (l *Log) Mute(on bool) { l.muted.Store(on) }

// audible reports whether a record at level escapes the mute.
func (l *Log) audible(level int64) bool { return level >= LevelError || !l.muted.Load() }

// fileTimeFormat is the time part every output file of a run is named with.
const fileTimeFormat = "060102-150405"

// FileStamp is now in fileTimeFormat, for an output named beside the logs.
func FileStamp() string { return time.Now().Format(fileTimeFormat) }

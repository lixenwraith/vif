package logfile

import (
	"time"
)

// Level is record severity. LevelTrace..LevelError are ordered for threshold
// filtering; LevelProc and LevelBad sit outside that order.
type Level uint8

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelProc
	LevelBad
	LevelCount
)

// OrderedCount is the number of threshold-comparable levels.
const OrderedCount = int(LevelError) + 1

var levelName = [LevelCount]string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "PROC", "BAD"}
var levelInitial = [LevelCount]byte{'T', 'D', 'I', 'W', 'E', 'P', 'B'}

func (l Level) String() string {
	if l < LevelCount {
		return levelName[l]
	}
	return "?"
}

// Initial returns the single character used to toggle the level.
func (l Level) Initial() byte {
	if l < LevelCount {
		return levelInitial[l]
	}
	return '?'
}

// ParseLevel maps a level token to a Level; unknown tokens are LevelBad.
func ParseLevel(b []byte) Level {
	switch string(b) {
	case "TRACE":
		return LevelTrace
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN":
		return LevelWarn
	case "ERROR":
		return LevelError
	case "PROC":
		return LevelProc
	}
	return LevelBad
}

// LevelByInitial resolves a toggle character to a Level.
func LevelByInitial(c byte) (Level, bool) {
	for i := Level(0); i < LevelCount; i++ {
		if levelInitial[i] == c {
			return i, true
		}
	}
	return LevelBad, false
}

// SubStat marks stat snapshot records.
const SubStat = "stat"

// StampText renders a record's wall-clock stamp, or a placeholder when the
// line carried no usable time and inherited one for ordering.
func StampText(m Meta) string {
	if m.Flags&FlagNoTime != 0 {
		return "--:--:--.---"
	}
	return FormatTS(m.TS)
}

// KnownSubs is the closed subsystem vocabulary; ad-hoc taps are also accepted.
var KnownSubs = []string{
	"app", "service", "fsm", "event", "dispatch", "push",
	"input", "stat", "rec", "lock", "race", "crash",
}

// DurUnit is the time unit implied by a metric key's suffix.
type DurUnit uint8

const (
	DurNone DurUnit = iota
	DurNs
	DurUs
	DurMs
)

// durKeys maps a key name to the unit it implies. Order matters: the first
// match wins, so unit suffixes are tested after the bare duration names.
var durKeys = []struct {
	name string
	unit DurUnit
}{
	{"timer", DurNs}, {"duration", DurNs}, {"elapsed", DurNs}, {"remaining", DurNs},
	{"ns", DurNs}, {"us", DurUs}, {"ms", DurMs},
}

// DurationUnit reports the duration unit implied by a field key.
func DurationUnit(key string) DurUnit {
	for _, d := range durKeys {
		if hasKeySegment(key, d.name) {
			return d.unit
		}
	}
	return DurNone
}

// hasKeySegment matches name as the whole key or as its trailing '.'/'_'
// segment: "elapsed", "max_duration" and "fsm.elapsed" match, "populations"
// does not match "ns".
func hasKeySegment(key, name string) bool {
	if len(key) < len(name) || key[len(key)-len(name):] != name {
		return false
	}
	if len(key) == len(name) {
		return true
	}
	c := key[len(key)-len(name)-1]
	return c == '.' || c == '_'
}

// FormatDuration renders v, expressed in unit u, as a human duration.
func FormatDuration(v int64, u DurUnit) string {
	switch u {
	case DurNs:
		return time.Duration(v).String()
	case DurUs:
		return (time.Duration(v) * time.Microsecond).String()
	case DurMs:
		return (time.Duration(v) * time.Millisecond).String()
	}
	return ""
}

// FormatTS renders unix nanoseconds as a local wall-clock stamp.
func FormatTS(ns int64) string {
	if ns == 0 {
		return "--:--:--.---"
	}
	return time.Unix(0, ns).Format("15:04:05.000")
}

// Dash renders empty vocabulary values: records without sub, or without a
// fields.msg/type discriminator.
func Dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

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

// Sub tags the viewer reasons about. Everything else is an ordinary tag.
const (
	// SubStat marks stat snapshot records.
	SubStat = "stat"
	// SubJournal marks replay-journal event records, written to vif-jrn files.
	SubJournal = "journal"
	// SubAnchor marks the journal's self-describing header records.
	SubAnchor = "anchor"
)

// Domain is a journal record's replication scope. DomNone covers every record
// that carries no domain at all, which is the whole diagnostic log.
type Domain uint8

const (
	DomNone Domain = iota
	DomShared
	DomPlayer
	DomCount
)

// domainName mirrors core.DomainNames; the journal writes these verbatim.
var domainName = [DomCount]string{"", "shared", "player"}
var domainInitial = [DomCount]byte{' ', 's', 'p'}

func (d Domain) String() string {
	if d < DomCount {
		return domainName[d]
	}
	return "?"
}

// Initial returns the one-character gutter and strip label.
func (d Domain) Initial() byte {
	if d < DomCount {
		return domainInitial[d]
	}
	return '?'
}

// ParseDomain maps a fields.domain token to a Domain; unknown tokens are DomNone.
func ParseDomain(b []byte) Domain {
	switch string(b) {
	case "shared":
		return DomShared
	case "player":
		return DomPlayer
	}
	return DomNone
}

// DomainByName resolves a domain name for filter specs; "both" and "" mean
// unconstrained.
func DomainByName(s string) (Domain, bool) {
	switch s {
	case "", "both", "all", "any":
		return DomNone, true
	case "shared":
		return DomShared, true
	case "player":
		return DomPlayer, true
	}
	return DomNone, false
}

// discriminatorKeys are the fields.* keys that name a record, in precedence
// order. Diagnostic records use msg, the logger self-report uses type, and
// journal records use ev — the event name is what distinguishes them.
var discriminatorKeys = [...]string{"msg", "type", "ev"}

// SyntheticMsg names records whose fields carry no discriminator at all. The
// journal anchor is the only one: it is a header, so its sub is its identity.
func SyntheticMsg(sub string) string {
	if sub == SubAnchor {
		return SubAnchor
	}
	return ""
}

// syntheticMsgTok is the index pass's form of SyntheticMsg: it returns a slice
// of sub rather than a fresh string, so a record without a discriminator costs
// the scan no allocation.
func syntheticMsgTok(sub []byte) []byte {
	if string(sub) == SubAnchor {
		return sub
	}
	return nil
}

// StampText renders a record's wall-clock stamp, or a placeholder when the
// line carried no usable time and inherited one for ordering.
func StampText(m Meta) string {
	if m.Flags&FlagNoTime != 0 {
		return "--:--:--.---"
	}
	return FormatTS(m.TS)
}

// KnownSubs is the subsystem vocabulary the game emits, in scope order, with
// the two journal subs last. Ad-hoc taps are also accepted; this drives the
// filter hint only.
var KnownSubs = []string{
	"app", "service", "crash", "race", "fsm", "event", "dispatch", "push",
	"input", "stat", "rec", "lock", "domain", "system",
	SubJournal, SubAnchor,
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

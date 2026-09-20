package status

import (
	"strconv"
	"strings"
	"time"
)

// Integer metric encoding is carried by the key suffix; no metadata is
// stored and the log emits raw values. Display consumers — overlay, status
// bar, log viewer — resolve through here so the convention has one owner.
var intUnits = []struct {
	suffix string
	scale  time.Duration
}{
	{".timer", time.Nanosecond},
	{".duration", time.Nanosecond}, // also matches .max_duration
	{".elapsed", time.Nanosecond},
	{".remaining", time.Nanosecond},
	{"_ns", time.Nanosecond},
	{"_us", time.Microsecond},
	{"_ms", time.Millisecond},
}

// IntUnit returns the duration scale a key encodes, 0 for a plain count
func IntUnit(key string) time.Duration {
	for _, u := range intUnits {
		if strings.HasSuffix(key, u.suffix) {
			return u.scale
		}
	}
	return 0
}

// FormatInt renders an int metric for display
func FormatInt(key string, v int64) string {
	if scale := IntUnit(key); scale != 0 {
		return (time.Duration(v) * scale).String()
	}
	return strconv.FormatInt(v, 10)
}

// FormatCount renders a count for a display that has no room for its digits:
// under ten thousand it is itself, above it one thousandfold suffix per step.
func FormatCount(v int64) string {
	if v > -10000 && v < 10000 {
		return strconv.FormatInt(v, 10)
	}
	f, suffix := float64(v), "K"
	for _, s := range []string{"K", "M", "B", "T", "Q"} {
		f, suffix = f/1000, s
		if f > -1000 && f < 1000 {
			break
		}
	}
	prec := 0
	if f > -100 && f < 100 {
		prec = 1
	}
	return strconv.FormatFloat(f, 'f', prec, 64) + suffix
}

// FormatLatency renders a round trip in five columns at most: a sub-millisecond
// link in tenths, a working one in whole milliseconds, a failing one in seconds.
func FormatLatency(us int64) string {
	switch {
	case us <= 0:
		return "--"
	case us < 1000:
		return strconv.FormatFloat(float64(us)/1000, 'f', 1, 64) + "ms"
	case us < 1_000_000:
		return strconv.FormatInt((us+500)/1000, 10) + "ms"
	default:
		return strconv.FormatFloat(float64(us)/1e6, 'f', 1, 64) + "s"
	}
}

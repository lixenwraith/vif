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

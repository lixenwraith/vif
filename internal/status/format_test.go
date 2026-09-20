package status

import "testing"

// TestFormatCountKeepsDisplayWidthBounded is the whole point of the suffixes: a
// metric a status bar shares a row with cannot spend a column per digit.
func TestFormatCountKeepsDisplayWidthBounded(t *testing.T) {
	tests := []struct {
		v    int64
		want string
	}{
		{0, "0"},
		{-1234, "-1234"},
		{9999, "9999"},
		{10_000, "10.0K"},
		{123_456, "123K"},
		{-1_500_000, "-1.5M"},
		{7_300_000_000, "7.3B"},
		{4_000_000_000_000, "4.0T"},
		{-9_223_372_036_854_775_808, "-9223Q"},
	}
	for _, tt := range tests {
		if got := FormatCount(tt.v); got != tt.want {
			t.Errorf("FormatCount(%d) = %q, want %q", tt.v, got, tt.want)
		}
	}
}

// TestFormatLatencyChangesUnitRatherThanWidth pins the reading a player takes
// from the session badge: a link is milliseconds until it is seconds.
func TestFormatLatencyChangesUnitRatherThanWidth(t *testing.T) {
	tests := []struct {
		us   int64
		want string
	}{
		{0, "--"},
		{300, "0.3ms"},
		{42_000, "42ms"},
		{999_499, "999ms"},
		{1_500_000, "1.5s"},
	}
	for _, tt := range tests {
		if got := FormatLatency(tt.us); got != tt.want {
			t.Errorf("FormatLatency(%d) = %q, want %q", tt.us, got, tt.want)
		}
	}
}

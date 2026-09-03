// Package snapshot: the cross-instance comparison surface.
//
// Three filters over the status registry, and one line format they render
// through. SharedKey is what two participants of one session must agree on;
// SimDeniedKey is what a replay must not compare, because it describes how a run
// was watched and driven rather than what it simulated.
package snapshot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// deniedSharedPrefix lists status key prefixes that are owner-authored under
// D-13: written by one instance from its own input and never re-derived. A
// cross-instance comparison drops them; a same-instance replay still compares them.
var deniedSharedPrefix = []string{
	"player.", // per-slot cursor state, including the weapon inventory cards
	// This instance's view of the map: mirrors of fields ctx|view already drops.
	"context.screen_", "context.camera_", "context.mode",
	// Instance-local traffic and index: only Shared ∪ Bus replicates (D-10), and a
	// resize re-runs ResizeGrid on the instance that received it. The one
	// comparable member is re-admitted by allowedSharedKey.
	"event.", "spatial.",
	// Transport counters: the exact complement of the peer's, not a shared quantity.
	"network.",
	// Aggregate entity counters sum both domains, so a second participant's
	// player-domain effects move them. ctx|world carries the shared half.
	"entity.",
	// Per-system counters of a player- or dual-profile system. The rule is the
	// profile in manifest.Systems, not the name.
	"drain.", "dust.", "decay.", "blossom.", "bullet.", "missile.",
	"lightning.", "flash.", "fadeout.", "splash.", "spirit.", "loot.",
	"weapon.", "energy.", "heat.", "typing.", "ping.", "boost.",
	"glyph.", "nugget.", "fuse.", "shield.", "cleaner.", "camera.", "transient.",
	"motion_marker.", "materialize.", "soft_collision.", "audio.", "music.",
	"death.", "timer.",
	// Combat resolves targets in both domains from one set of counters, so the
	// whole group is a mixed aggregate; the shared combat digest carries its
	// shared half.
	"combat.",
	// Kill tallies mix shared species with the player-domain drain.
	"kills.",
	// Capture cost and correction magnitude: the measurement cadence is chosen
	// from, and under weakened D-11 not something two instances agree on.
	"snapshot.",
}

// deniedSharedKey drops single per-instance keys from groups that are otherwise
// comparable.
var deniedSharedKey = map[string]bool{
	"engine.apm":       true, // actions this participant took, not the session's
	"engine.music_apm": true,
	// A missed deadline is this process's pacing, not simulation state. Elapsed
	// game time is absent: engine.SimTime derives it from the tick, so comparing
	// it is what pins the simulation clock deterministic.
	"engine.tick_slips": true,
	// Whole-store counts sum both domains; the shared position digest covers the
	// shared half.
	"nav.entities": true,
	// Corpus consumption is a player-domain draw. The fingerprint beside these
	// (files, blocks, lines, source) describes the corpus rather than a position
	// in it and stays comparable.
	"content.served":   true,
	"content.rejected": true,
	"content.file":     true,
}

// allowedSharedKey re-admits a key its group prefix denies. spatial.indexed_shared
// counts the shared half of the partition, which D-11 requires two instances to
// agree on.
var allowedSharedKey = map[string]bool{
	"spatial.indexed_shared": true,
}

// deniedSharedField lists context record fields that are local, in records that
// otherwise carry shared state.
var deniedSharedField = map[string]bool{
	"created_local":   true,
	"destroyed_local": true,
}

// SharedKey reports whether a status key belongs in a cross-instance comparison.
func SharedKey(key string) bool {
	if SimDeniedKey(key) || deniedSharedKey[key] {
		return false
	}
	// Scratch high-water marks: allocation telemetry sized by this instance's own
	// player-domain population. newBufferTelemetry names all of them.
	if strings.Contains(key, ".buf_") && strings.HasSuffix(key, "_hwm") {
		return false
	}
	// Shared sweep systems count rejected player victims separately so their
	// shared rejection counter stays comparable.
	if strings.HasSuffix(key, ".protected_player_rejects") {
		return false
	}
	if allowedSharedKey[key] {
		return true
	}
	for _, p := range deniedSharedPrefix {
		if strings.HasPrefix(key, p) {
			return false
		}
	}
	return true
}

// deniedSimKey lists status keys a replay must not compare.
//
// Pause, rate and step are recorded and injected like any other event, but a
// replay does not honour them: a manual clock stores a rate without applying it
// and no replay runs schedulerLoop. fps and frame belong to the render loop,
// event.backoffs counts update-mutex contention in the live event loop, and rec
// and stat are telemetry about telemetry. Keys rather than prefixes: the engine
// group mixes these with simulation counters.
var deniedSimKey = map[string]bool{
	"engine.paused":     true,
	"engine.speed":      true,
	"engine.speed_pct":  true,
	"engine.step":       true,
	"engine.breakpoint": true,
	"engine.fps":        true,
	"context.frame":     true,
	"event.backoffs":    true,
	"rec.depth":         true,
	"rec.flushes":       true,
	"rec.records":       true,
	"rec.skipped":       true,
	"stat.late":         true,
	"stat.groups":       true,
	"stat.metrics":      true,
}

// SimDeniedKeys lists the explicitly named replay-excluded keys, sorted. The
// snapshot group SimDeniedKey also excludes is a prefix and has no fixed list.
func SimDeniedKeys() []string {
	out := make([]string, 0, len(deniedSimKey))
	for k := range deniedSimKey {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SimDeniedKey is deniedSimKey plus the whole snapshot group: a replay installs
// no capture and applies no correction, so that group describes the session the
// run was part of rather than the simulation being reproduced.
func SimDeniedKey(key string) bool {
	return deniedSimKey[key] || strings.HasPrefix(key, "snapshot.")
}

// IsRecord reports whether an emitted record carries the given msg name.
func IsRecord(args []any, name string) bool {
	if len(args) < 2 {
		return false
	}
	k, _ := args[0].(string)
	v, _ := args[1].(string)
	return k == "msg" && v == name
}

// FilterFields drops the local fields of an otherwise shared record.
func FilterFields(args []any) []any {
	out := args[:0:0]
	for i := 0; i+1 < len(args); i += 2 {
		if key, _ := args[i].(string); deniedSharedField[key] {
			continue
		}
		out = append(out, args[i], args[i+1])
	}
	return out
}

// Line renders one emitted record as "source|sub|key=value|...". The source tag
// separates the two emitters, which share group names.
func Line(source, sub string, args []any) string {
	var b strings.Builder
	b.WriteString(source)
	b.WriteByte('|')
	b.WriteString(sub)
	for i := 0; i+1 < len(args); i += 2 {
		key, _ := args[i].(string)
		b.WriteByte('|')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(FormatValue(args[i+1]))
	}
	return b.String()
}

// FormatValue renders a metric for comparison. Floats use the shortest
// round-tripping form, exact for same-process equality.
func FormatValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

// FirstDiff returns the index and both values of the first differing line; ok is
// false when the two are identical.
func FirstDiff(x, y []string) (idx int, lineX, lineY string, ok bool) {
	n := min(len(x), len(y))
	for i := range n {
		if x[i] != y[i] {
			return i, x[i], y[i], true
		}
	}
	switch {
	case len(x) > n:
		return n, x[n], "", true
	case len(y) > n:
		return n, "", y[n], true
	}
	return 0, "", "", false
}

// Diff returns up to max rendered differences, for a failure message. FirstDiff
// answers whether two snapshots differ; this answers where.
func Diff(x, y []string, max int) []string {
	out := make([]string, 0, max)
	n := min(len(x), len(y))
	for i := 0; i < n && len(out) < max; i++ {
		if x[i] != y[i] {
			out = append(out, fmt.Sprintf("  [%d] want %s\n       got  %s", i, x[i], y[i]))
		}
	}
	for i := n; i < len(x) && len(out) < max; i++ {
		out = append(out, fmt.Sprintf("  [%d] want %s\n       got  <absent>", i, x[i]))
	}
	for i := n; i < len(y) && len(out) < max; i++ {
		out = append(out, fmt.Sprintf("  [%d] want <absent>\n       got  %s", i, y[i]))
	}
	return out
}

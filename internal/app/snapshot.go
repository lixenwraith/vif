// Package app: snapshot surface.
//
// Two views over one reading. Snapshot is everything, for ":d save" and for the
// perturbation test — journaling must change nothing at all. SnapshotSimulation
// drops the operator surface: the session record from SnapshotContext plus the
// registry keys in denySim. That surface describes how a run is being watched and
// driven, is written by direct context and TimeControl writes rather than by
// events, and is read by no system — so it neither replays nor matters.
package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/engine"
)

// denyShared lists status key prefixes that are owner-authored under D-13: written
// by one instance from its own input and never re-derived. A cross-instance
// comparison drops them; a same-instance replay still compares them.
var denySharedPrefix = []string{
	"player.", // per-slot cursor state, including the weapon inventory cards
	// This instance's view of the map: mirrors of fields ctx|view already drops
	"context.screen_", "context.camera_", "context.mode",
	// Instance-local traffic and index: only Shared ∪ Bus replicates (D-10), and a
	// resize re-runs ResizeGrid on the instance that received it. The one comparable
	// member is exempted by allowSharedKey rather than by narrowing the prefix.
	"event.", "spatial.",
	// Transport traffic: what this instance sent and received, which is the exact
	// complement of its peer's counters rather than a shared quantity
	"network.",
	// Aggregate entity counters sum both domains, so a second participant's own
	// player-domain effects move them. ctx|world carries the shared half, and the
	// shared position digest covers placement.
	"entity.",
	// Per-system counters of a player- or dual-profile system. Two instances of one
	// terminal ran identical player-domain simulations, so most of these matched by
	// accident; a real second participant drives its own cursor, so none of them do.
	// The rule is the profile in manifest.Systems, not the name.
	"drain.", "dust.", "decay.", "blossom.", "bullet.", "missile.",
	"lightning.", "flash.", "fadeout.", "splash.", "spirit.", "loot.",
	"weapon.", "energy.", "heat.", "typing.", "ping.", "boost.",
	"glyph.", "fuse.", "shield.", "cleaner.", "camera.", "transient.",
	"motion_marker.", "materialize.", "soft_collision.", "audio.", "music.",
	"death.", "timer.",
	// Combat resolves targets in both domains from one set of counters, so the whole
	// group is a mixed aggregate. The shared combat digest carries the shared half;
	// splitting these per domain would restore the group to the comparison.
	"combat.",
	// Kill tallies mix shared species with the player-domain drain, so the total
	// and the drain column move with this participant's own population
	"kills.",
}

// denySharedKey drops a single per-instance key from a group that is otherwise
// comparable. Keys, not a prefix: the engine group mixes these with the tick
// counters two participants must agree on.
var denySharedKey = map[string]bool{
	"engine.apm":       true, // actions this participant took, not the session's
	"engine.music_apm": true,
	// Whole-store counts, which sum both domains: a participant's own player-domain
	// population moves them. The shared position digest covers the shared half.
	"nav.entities": true,
	// Corpus consumption is a player-domain draw; the fingerprint beside it
	// (files, blocks, lines, source) is shared and stays comparable.
	"content.served":   true,
	"content.rejected": true,
}

// allowSharedKey re-admits a key its group prefix denies. spatial.indexed_shared
// counts the shared half of the partition, which D-11 requires two instances to
// agree on; the rest of the spatial group is this instance's index.
var allowSharedKey = map[string]bool{
	"spatial.indexed_shared": true,
}

// denySharedField lists context record fields that are local, in records that
// otherwise carry shared state
var denySharedField = map[string]bool{
	"created_local":   true,
	"destroyed_local": true,
}

// sharedKey reports whether a status key belongs in a cross-instance comparison
func sharedKey(key string) bool {
	if denySim[key] || denySharedKey[key] {
		return false
	}
	// Scratch high-water marks: allocation telemetry sized by this instance's own
	// player-domain population. Named by newBufferTelemetry, which every system
	// publishing one goes through, so the suffix is the whole rule.
	if strings.Contains(key, ".buf_") && strings.HasSuffix(key, "_hwm") {
		return false
	}
	if allowSharedKey[key] {
		return true
	}
	for _, p := range denySharedPrefix {
		if strings.HasPrefix(key, p) {
			return false
		}
	}
	return true
}

// denySim lists status keys a replay must not compare.
//
// Pause, rate and step are recorded and injected like any other event — the driver
// filters nothing — but the replay does not honour them: a manual clock stores a
// rate without applying it, stepTick opens the pause gate, and a step allowance is
// drained only by schedulerLoop, which no replay runs. Comparing them would assert
// observer telemetry, and engine.step would diverge outright.
// fps and frame belong to the render loop; event.backoffs counts update-mutex
// contention in the live event loop; rec and stat are telemetry about telemetry,
// paced by trigger timing and registration order.
// Keys, not prefixes: the engine group mixes these with simulation counters
// (ticks, apm, music_apm, tick_slips, time.game_elapsed_ms).
var denySim = map[string]bool{
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

// Snapshot returns the sorted context and registry state as comparable lines.
// Two runs of one seed must produce identical slices.
func (a *App) Snapshot() []string { return a.snapshot(false) }

// SnapshotSimulation returns the snapshot with the operator surface removed, for
// comparing a replay against the run it was recorded from
func (a *App) SnapshotSimulation() []string { return a.snapshot(true) }

// SnapshotShared returns the shared half of the snapshot: the state D-11 requires
// two instances of one session to agree on, with owner-authored state removed.
func (a *App) SnapshotShared() []string { return a.snapshotShared() }

// snapshotShared reads both emitters in one critical section, narrowed to the
// cross-instance surface: shared-domain digest, shared records, shared keys.
func (a *App) snapshotShared() []string {
	lines := make([]string, 0, 64)

	a.world.RunSafe(func() {
		wd := a.worldDigestScopedLocked(engine.ScopeShared)
		lines = append(lines, "ctx|digest"+
			"|positions="+wd.Positions.String()+
			"|kinetics="+wd.Kinetics.String()+
			"|combat="+wd.Combat.String())

		a.ctx.SnapshotContext(func(sub string, args ...any) {
			if isRecord(args, "session") || isRecord(args, "view") {
				return
			}
			lines = append(lines, snapshotLine("ctx", sub, filterFields(args)))
		})
		a.world.Resources.Status.SnapshotFiltered(sharedKey, func(sub string, args ...any) {
			lines = append(lines, snapshotLine("reg", sub, args))
		})
	})

	slices.Sort(lines)
	return lines
}

// filterFields drops the local fields of an otherwise shared record
func filterFields(args []any) []any {
	out := args[:0:0]
	for i := 0; i+1 < len(args); i += 2 {
		if key, _ := args[i].(string); denySharedField[key] {
			continue
		}
		out = append(out, args[i], args[i+1])
	}
	return out
}

// snapshot reads both emitters in one critical section: SnapshotContext reads world
// state, and the registry reading belongs to the same instant
func (a *App) snapshot(simOnly bool) []string {
	lines := make([]string, 0, 64)
	keep := func(key string) bool { return !simOnly || !denySim[key] }

	a.world.RunSafe(func() {
		wd := a.worldDigestLocked()
		lines = append(lines, "ctx|digest"+
			"|positions="+wd.Positions.String()+
			"|kinetics="+wd.Kinetics.String()+
			"|combat="+wd.Combat.String()+
			"|entities="+wd.Entities.String())

		a.ctx.SnapshotContext(func(sub string, args ...any) {
			if simOnly && isRecord(args, "session") {
				return
			}
			lines = append(lines, snapshotLine("ctx", sub, args))
		})
		a.world.Resources.Status.SnapshotFiltered(keep, func(sub string, args ...any) {
			lines = append(lines, snapshotLine("reg", sub, args))
		})
	})

	slices.Sort(lines)
	return lines
}

// isRecord reports whether an emitted record carries the given msg name
func isRecord(args []any, name string) bool {
	if len(args) < 2 {
		return false
	}
	k, _ := args[0].(string)
	v, _ := args[1].(string)
	return k == "msg" && v == name
}

// snapshotLine renders one emitted record as "source|sub|key=value|...".
// The source tag separates the two emitters, which share group names.
func snapshotLine(source, sub string, args []any) string {
	var b strings.Builder
	b.WriteString(source)
	b.WriteByte('|')
	b.WriteString(sub)
	for i := 0; i+1 < len(args); i += 2 {
		key, _ := args[i].(string)
		b.WriteByte('|')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(formatSnapshotValue(args[i+1]))
	}
	return b.String()
}

// formatSnapshotValue renders a metric for comparison. Floats use the shortest
// round-tripping form, which is exact for same-process equality.
func formatSnapshotValue(v any) string {
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

// FirstDiff returns the index and both values of the first differing snapshot
// line; ok is false when the snapshots are identical.
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

// Diff returns up to max rendered differences between two snapshots, for a failure
// message. FirstDiff answers whether they differ; this answers where.
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

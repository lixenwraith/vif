// Snapshot surface: three views over one reading of the status registry and the
// game context. Snapshot is everything, for ":d save" and the perturbation test.
// Simulation drops the operator surface, which describes how a run is watched and
// driven and is read by no system. Shared narrows to what D-11 requires two
// instances of one session to agree on. The filters and the line format live in
// internal/snapshot.

package app

import (
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// Snapshot returns the sorted context and registry state as comparable lines.
// Two runs of one seed produce identical slices.
func (a *App) Snapshot() []string { return a.snapshotLines(false) }

// SnapshotSimulation returns the snapshot with the operator surface removed, for
// comparing a replay against the run it was recorded from.
func (a *App) SnapshotSimulation() []string { return a.snapshotLines(true) }

// SnapshotShared returns the cross-instance surface: shared-domain digest, shared
// records, shared keys, with owner-authored state removed.
func (a *App) SnapshotShared() []string {
	var lines []string
	a.world.RunSafe(func() {
		_, digestLine, context, status := a.sharedSurfaceLocked()
		lines = make([]string, 0, 1+len(context)+len(status))
		lines = append(lines, digestLine)
		lines = append(lines, context...)
		lines = append(lines, status...)
	})
	slices.Sort(lines)
	return lines
}

// sharedSurfaceLocked assembles the comparable surface once, for the two readers
// that must not disagree about it: SnapshotShared, which is what a test compares,
// and sharedDigestLocked, which is what the session's drift gauge hashes. Two
// assemblies of one surface would let a filter drift between them, and the gauge
// would then report agreement about a surface nobody compares.
//
// Caller MUST hold the world lock.
func (a *App) sharedSurfaceLocked() (wd worldDigest, digestLine string, context, status []string) {
	wd = a.worldDigestScopedLocked(engine.ScopeShared)
	context = make([]string, 0, 8)
	a.ctx.SnapshotContext(func(sub string, args ...any) {
		if snapshot.IsRecord(args, "session") || snapshot.IsRecord(args, "view") {
			return
		}
		context = append(context, snapshot.Line("ctx", sub, snapshot.FilterFields(args)))
	})
	status = make([]string, 0, 56)
	a.world.Resources.Status.SnapshotFiltered(snapshot.SharedKey, func(sub string, args ...any) {
		status = append(status, snapshot.Line("reg", sub, args))
	})
	return wd, wd.line(false), context, status
}

// snapshotLines reads both emitters in one critical section: SnapshotContext reads
// world state, and the registry reading belongs to the same instant.
func (a *App) snapshotLines(simOnly bool) []string {
	lines := make([]string, 0, 64)
	keep := func(key string) bool { return !simOnly || !snapshot.SimDeniedKey(key) }

	a.world.RunSafe(func() {
		lines = append(lines, a.worldDigestLocked().line(true))

		a.ctx.SnapshotContext(func(sub string, args ...any) {
			if simOnly && snapshot.IsRecord(args, "session") {
				return
			}
			lines = append(lines, snapshot.Line("ctx", sub, args))
		})
		a.world.Resources.Status.SnapshotFiltered(keep, func(sub string, args ...any) {
			lines = append(lines, snapshot.Line("reg", sub, args))
		})
	})

	slices.Sort(lines)
	return lines
}

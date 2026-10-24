package app

import (
	"github.com/lixenwraith/vif/internal/snapshot"
)

// Snapshot returns the sorted context and registry state as comparable lines.
// Two runs of one seed produce identical slices.
func (a *App) Snapshot() []string { return a.snapshotLines(false) }

// SnapshotSimulation drops the operator surface, for comparing a replay against
// the run it was recorded from.
func (a *App) SnapshotSimulation() []string { return a.snapshotLines(true) }

// SnapshotShared returns the cross-instance surface: shared-domain digest, shared
// records, shared keys, with owner-authored state removed.
func (a *App) SnapshotShared() (lines []string) {
	a.world.RunSafe(func() { lines = snapshot.SharedLines(a.ctx, a.world) })
	return lines
}

// snapshotLines reads both emitters in one critical section: SnapshotContext reads
// world state, and the registry reading belongs to the same instant.
func (a *App) snapshotLines(simOnly bool) (lines []string) {
	a.world.RunSafe(func() { lines = snapshot.Lines(a.ctx, a.world, simOnly) })
	return lines
}

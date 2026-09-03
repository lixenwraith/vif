package app

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// FNV-1a 64, inlined so a per-tick digest allocates nothing
const (
	fnvOffset uint64 = 14695981039346656037
	fnvPrime  uint64 = 1099511628211
)

// digest is a running FNV-1a hash over simulation state
type digest uint64

func newDigest() digest { return digest(fnvOffset) }

func (d digest) u64(v uint64) digest {
	for i := range 8 {
		d ^= digest(byte(v >> (8 * i)))
		d *= digest(fnvPrime)
	}
	return d
}

func (d digest) i64(v int64) digest   { return d.u64(uint64(v)) }
func (d digest) f64(v float64) digest { return d.u64(math.Float64bits(v)) }

func (d digest) b(v bool) digest {
	if v {
		return d.u64(1)
	}
	return d.u64(0)
}

func (d digest) text(v string) digest {
	for i := range len(v) {
		d ^= digest(v[i])
		d *= digest(fnvPrime)
	}
	return d.u64(uint64(len(v)))
}

func (d digest) String() string { return strconv.FormatUint(uint64(d), 16) }

// worldDigest hashes the state the status registry does not carry: entity
// placement, sub-cell motion, and combat timers. Split per store so a diff
// names which one moved first.
type worldDigest struct {
	Positions digest
	Kinetics  digest
	Combat    digest
	Entities  digest
}

// worldDigestLocked computes the digest over both domains; caller MUST hold the world lock
func (a *App) worldDigestLocked() worldDigest {
	return a.worldDigestScopedLocked(engine.ScopeBoth)
}

// worldDigestScopedLocked hashes one domain scope, so two instances compare shared
// state without their player-domain effects. Caller MUST hold the world lock.
func (a *App) worldDigestScopedLocked(scope engine.DomainScope) worldDigest {
	var wd worldDigest

	wd.Positions = newDigest()
	for _, e := range digestEntities(a.world.Positions.Entities(), scope) {
		pos, ok := a.world.Positions.GetPosition(e)
		if !ok {
			continue
		}
		wd.Positions = wd.Positions.u64(uint64(e)).i64(int64(pos.X)).i64(int64(pos.Y))
	}

	// Float divergence surfaces here many ticks before it moves a grid cell
	wd.Kinetics = newDigest()
	for _, e := range digestEntities(a.world.Components.Kinetic.Entities(), scope) {
		k, ok := a.world.Components.Kinetic.GetPtr(e)
		if !ok {
			continue
		}
		wd.Kinetics = wd.Kinetics.u64(uint64(e)).
			f64(k.PreciseX).f64(k.PreciseY).f64(k.VelX).f64(k.VelY)
	}

	// A cursor's combat is owner-authored and transported (D-13), so it is compared
	// only within one instance; every other combatant is re-derived and must match.
	shared := scope != engine.ScopeBoth
	wd.Combat = newDigest()
	for _, e := range digestEntities(a.world.Components.Combat.Entities(), scope) {
		if shared && a.world.Components.Cursor.HasEntity(e) {
			continue
		}
		c, ok := a.world.Components.Combat.GetPtr(e)
		if !ok {
			continue
		}
		wd.Combat = wd.Combat.u64(uint64(e)).
			i64(int64(c.HitPoints)).b(c.IsEnraged).
			i64(int64(c.StunnedRemaining)).
			i64(int64(c.RemainingKineticImmunity)).
			i64(int64(c.RemainingDamageImmunity))
	}

	wd.Entities = newDigest().
		i64(a.world.CreatedCount()).
		i64(a.world.DestroyedCount()).
		i64(int64(a.world.Positions.CountEntities()))

	return wd
}

// sharedDigestLocked hashes exactly SnapshotShared's comparable surface into one
// transport-sized value. Caller MUST hold the world lock; unlike snapshotShared it
// must not acquire it again.
func (a *App) sharedDigestLocked(detail bool) engine.SharedStateDigest {
	wd := a.worldDigestScopedLocked(engine.ScopeShared)
	digestLine := "ctx|digest" +
		"|positions=" + wd.Positions.String() +
		"|kinetics=" + wd.Kinetics.String() +
		"|combat=" + wd.Combat.String()
	contextLines := make([]string, 0, 8)
	a.ctx.SnapshotContext(func(sub string, args ...any) {
		if snapshot.IsRecord(args, "session") || snapshot.IsRecord(args, "view") {
			return
		}
		contextLines = append(contextLines, snapshot.Line("ctx", sub, snapshot.FilterFields(args)))
	})
	statusLines := make([]string, 0, 56)
	a.world.Resources.Status.SnapshotFiltered(snapshot.SharedKey, func(sub string, args ...any) {
		statusLines = append(statusLines, snapshot.Line("reg", sub, args))
	})
	// One hash per record, so a mismatch names the record rather than the category
	// it belongs to. Built only on request: it is a diagnostic, not a probe.
	var groups map[string]uint64
	if detail {
		groups = make(map[string]uint64, len(contextLines)+len(statusLines))
		for _, line := range contextLines {
			groups[recordName(line)] = uint64(newDigest().text(line))
		}
		for _, line := range statusLines {
			groups[recordName(line)] = uint64(newDigest().text(line))
		}
		groups["world"] = uint64(newDigest().text(digestLine))
	}
	slices.Sort(contextLines)
	slices.Sort(statusLines)
	contextDigest := newDigest()
	for _, line := range contextLines {
		contextDigest = contextDigest.text(line)
	}
	statusDigest := newDigest()
	for _, line := range statusLines {
		statusDigest = statusDigest.text(line)
	}
	lines := append(contextLines, statusLines...)
	slices.Sort(lines)
	surface := newDigest()
	for _, line := range lines {
		surface = surface.text(line)
	}
	lines = append(lines, digestLine)
	slices.Sort(lines)
	d := newDigest()
	for _, line := range lines {
		d = d.text(line)
	}
	return engine.SharedStateDigest{
		Hash:      uint64(d),
		Positions: uint64(wd.Positions),
		Kinetics:  uint64(wd.Kinetics),
		Combat:    uint64(wd.Combat),
		Context:   uint64(contextDigest),
		Status:    uint64(statusDigest),
		Surface:   uint64(surface),
		Groups:    groups,
	}
}

// recordName is the identity of one snapshot line: its emitter and record name,
// without the field values that follow. Two instances build the same set of names
// from the same world, so a differing hash under one name is the record to read.
func recordName(line string) string {
	name := line
	if i := strings.Index(name, "|msg="); i >= 0 {
		head, tail := name[:i], name[i+len("|msg="):]
		if j := strings.IndexByte(tail, '|'); j >= 0 {
			tail = tail[:j]
		}
		return head + "|" + tail
	}
	if i := strings.IndexByte(name, '|'); i >= 0 {
		if j := strings.IndexByte(name[i+1:], '|'); j >= 0 {
			return name[:i+1+j]
		}
	}
	return name
}

// digestEntities canonically projects a mixed dense store for cross-instance comparison.
func digestEntities(entities []core.Entity, scope engine.DomainScope) []core.Entity {
	if scope == engine.ScopeBoth {
		return entities
	}
	out := make([]core.Entity, 0, len(entities))
	for _, e := range entities {
		if scope.Selects(e) {
			out = append(out, e)
		}
	}
	slices.Sort(out)
	return out
}

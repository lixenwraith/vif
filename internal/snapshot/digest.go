package snapshot

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
)

// FNV-1a 64, inlined so a per-tick digest allocates nothing
const (
	fnvOffset uint64 = 14695981039346656037
	fnvPrime  uint64 = 1099511628211
)

// hash is a running FNV-1a hash over simulation state
type hash uint64

func newHash() hash { return hash(fnvOffset) }

func (h hash) u64(v uint64) hash {
	for i := range 8 {
		h ^= hash(byte(v >> (8 * i)))
		h *= hash(fnvPrime)
	}
	return h
}

func (h hash) i64(v int64) hash   { return h.u64(uint64(v)) }
func (h hash) f64(v float64) hash { return h.u64(math.Float64bits(v)) }

func (h hash) b(v bool) hash {
	if v {
		return h.u64(1)
	}
	return h.u64(0)
}

func (h hash) text(v string) hash {
	for i := range len(v) {
		h ^= hash(v[i])
		h *= hash(fnvPrime)
	}
	return h.u64(uint64(len(v)))
}

func (h hash) String() string { return strconv.FormatUint(uint64(h), 16) }

// WorldDigest hashes the state the status registry does not carry: entity
// placement, sub-cell motion, and combat timers. Split per store so a diff
// names which one moved first.
type WorldDigest struct {
	Positions hash
	Kinetics  hash
	Combat    hash
	Entities  hash
}

// Line renders the digest as the record the comparable surface leads with. The
// shared surface omits entities: creation and destruction counts include this
// instance's own player domain.
func (wd WorldDigest) Line(entities bool) string {
	out := "ctx|digest" +
		"|positions=" + wd.Positions.String() +
		"|kinetics=" + wd.Kinetics.String() +
		"|combat=" + wd.Combat.String()
	if entities {
		out += "|entities=" + wd.Entities.String()
	}
	return out
}

// DigestWorld hashes one domain scope, so two instances compare shared state
// without their player-domain effects. Caller MUST hold the world lock.
func DigestWorld(w *engine.World, scope engine.DomainScope) WorldDigest {
	var wd WorldDigest

	wd.Positions = newHash()
	for _, e := range digestEntities(w.Positions.Entities(), scope) {
		pos, ok := w.Positions.GetPosition(e)
		if !ok {
			continue
		}
		wd.Positions = wd.Positions.u64(uint64(e)).i64(int64(pos.X)).i64(int64(pos.Y))
	}

	// Float divergence surfaces here many ticks before it moves a grid cell
	wd.Kinetics = newHash()
	for _, e := range digestEntities(w.Components.Kinetic.Entities(), scope) {
		k, ok := w.Components.Kinetic.GetPtr(e)
		if !ok {
			continue
		}
		wd.Kinetics = wd.Kinetics.u64(uint64(e)).
			f64(k.PreciseX).f64(k.PreciseY).f64(k.VelX).f64(k.VelY)
	}

	// A cursor's combat is owner-authored and transported (D-13), so it is compared
	// only within one instance; every other combatant is re-derived and must match.
	shared := scope != engine.ScopeBoth
	wd.Combat = newHash()
	for _, e := range digestEntities(w.Components.Combat.Entities(), scope) {
		if shared && w.Components.Cursor.HasEntity(e) {
			continue
		}
		c, ok := w.Components.Combat.GetPtr(e)
		if !ok {
			continue
		}
		wd.Combat = wd.Combat.u64(uint64(e)).
			i64(int64(c.HitPoints)).b(c.IsEnraged).
			i64(int64(c.StunnedRemaining)).
			i64(int64(c.RemainingKineticImmunity)).
			i64(int64(c.RemainingDamageImmunity)).
			u64(uint64(c.DamageImmunitySpent))
	}

	wd.Entities = newHash().
		i64(w.CreatedCount()).
		i64(w.DestroyedCount()).
		i64(int64(w.Positions.CountEntities()))

	return wd
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

// RecordName is the identity of one snapshot line: its emitter and record name,
// without the field values. Two instances build the same names from the same
// world, so a differing hash under one name is the record to read.
func RecordName(line string) string {
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

// SharedSurface assembles the cross-instance comparable surface once, for the two
// readers that must not disagree about it: the lines a test compares and the
// digest the session's drift gauge hashes. Caller MUST hold the world lock.
func SharedSurface(ctx *engine.GameContext, w *engine.World) (wd WorldDigest, digestLine string, context, status []string) {
	wd = DigestWorld(w, engine.ScopeShared)
	context = make([]string, 0, 8)
	ctx.SnapshotContext(func(sub string, args ...any) {
		if IsRecord(args, "session") || IsRecord(args, "view") {
			return
		}
		context = append(context, Line("ctx", sub, FilterFields(args)))
	})
	status = make([]string, 0, 56)
	w.Resources.Status.SnapshotFiltered(SharedKey, func(sub string, args ...any) {
		status = append(status, Line("reg", sub, args))
	})
	return wd, wd.Line(false), context, status
}

// SharedLines is SharedSurface as the sorted line set a comparison reads.
// Caller MUST hold the world lock.
func SharedLines(ctx *engine.GameContext, w *engine.World) []string {
	_, digestLine, context, status := SharedSurface(ctx, w)
	lines := make([]string, 0, 1+len(context)+len(status))
	lines = append(lines, digestLine)
	lines = append(lines, context...)
	lines = append(lines, status...)
	slices.Sort(lines)
	return lines
}

// SharedDigest hashes exactly SharedLines' surface into one transport-sized
// value. detail additionally hashes each record on its own, so a mismatch names
// the record rather than the category. Caller MUST hold the world lock.
func SharedDigest(ctx *engine.GameContext, w *engine.World, detail bool) engine.SharedStateDigest {
	wd, digestLine, context, status := SharedSurface(ctx, w)
	var groups map[string]uint64
	if detail {
		groups = make(map[string]uint64, len(context)+len(status))
		for _, line := range context {
			groups[RecordName(line)] = uint64(newHash().text(line))
		}
		for _, line := range status {
			groups[RecordName(line)] = uint64(newHash().text(line))
		}
		groups["world"] = uint64(newHash().text(digestLine))
	}
	slices.Sort(context)
	slices.Sort(status)

	fold := func(lines []string) hash {
		h := newHash()
		for _, line := range lines {
			h = h.text(line)
		}
		return h
	}
	lines := append(slices.Clone(context), status...)
	slices.Sort(lines)
	surface := fold(lines)
	lines = append(lines, digestLine)
	slices.Sort(lines)
	return engine.SharedStateDigest{
		Hash:      uint64(fold(lines)),
		Positions: uint64(wd.Positions),
		Kinetics:  uint64(wd.Kinetics),
		Combat:    uint64(wd.Combat),
		Context:   uint64(fold(context)),
		Status:    uint64(fold(status)),
		Surface:   uint64(surface),
		Groups:    groups,
	}
}

// Lines is the whole-instance surface: the both-domain digest, every context
// record and every registry cell. simOnly drops the operator surface, which
// describes how a run is watched and driven. Caller MUST hold the world lock.
func Lines(ctx *engine.GameContext, w *engine.World, simOnly bool) []string {
	lines := make([]string, 0, 64)
	lines = append(lines, DigestWorld(w, engine.ScopeBoth).Line(true))

	ctx.SnapshotContext(func(sub string, args ...any) {
		if simOnly && IsRecord(args, "session") {
			return
		}
		lines = append(lines, Line("ctx", sub, args))
	})
	w.Resources.Status.SnapshotFiltered(func(key string) bool {
		return !simOnly || !SimDeniedKey(key)
	}, func(sub string, args ...any) {
		lines = append(lines, Line("reg", sub, args))
	})

	slices.Sort(lines)
	return lines
}

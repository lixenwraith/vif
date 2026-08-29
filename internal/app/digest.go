package app

import (
	"math"
	"slices"
	"strconv"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
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

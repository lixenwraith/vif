package app

import (
	"math"
	"strconv"

	"github.com/lixenwraith/vi-fighter/internal/component"
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
	for _, e := range a.world.Positions.Entities() {
		if !scope.Selects(e) {
			continue
		}
		pos, ok := a.world.Positions.GetPosition(e)
		if !ok {
			continue
		}
		wd.Positions = wd.Positions.u64(uint64(e)).i64(int64(pos.X)).i64(int64(pos.Y))
	}

	// Float divergence surfaces here many ticks before it moves a grid cell
	wd.Kinetics = newDigest()
	a.world.Components.Kinetic.Each(func(e core.Entity, k *component.KineticComponent) bool {
		if !scope.Selects(e) {
			return true
		}
		wd.Kinetics = wd.Kinetics.u64(uint64(e)).
			f64(k.PreciseX).f64(k.PreciseY).f64(k.VelX).f64(k.VelY)
		return true
	})

	wd.Combat = newDigest()
	a.world.Components.Combat.Each(func(e core.Entity, c *component.CombatComponent) bool {
		if !scope.Selects(e) {
			return true
		}
		wd.Combat = wd.Combat.u64(uint64(e)).
			i64(int64(c.HitPoints)).b(c.IsEnraged).
			i64(int64(c.StunnedRemaining)).
			i64(int64(c.RemainingKineticImmunity)).
			i64(int64(c.RemainingDamageImmunity))
		return true
	})

	wd.Entities = newDigest().
		i64(a.world.CreatedCount()).
		i64(a.world.DestroyedCount()).
		i64(int64(a.world.Positions.CountEntities()))

	return wd
}

package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// bufferTelemetry tracks the largest live length reached by reusable buffers.
// Names are stable field names so capacity changes can be compared across runs.
type bufferTelemetry struct {
	values []*atomic.Int64
}

func newBufferTelemetry(reg *status.Registry, domain string, names ...string) bufferTelemetry {
	b := bufferTelemetry{values: make([]*atomic.Int64, len(names))}
	for i, name := range names {
		b.values[i] = reg.Ints.Get(domain + ".buf_" + name + "_hwm")
	}
	return b
}

func (b *bufferTelemetry) Reset() {
	for _, value := range b.values {
		value.Store(0)
	}
}

func (b *bufferTelemetry) Observe(index, length int) {
	if uint(index) >= uint(len(b.values)) {
		return
	}
	storeMax(b.values[index], int64(length))
}

// lifecycleTelemetry gives every species the same five session counters.
type lifecycleTelemetry struct {
	spawned         *atomic.Int64
	despawned       *atomic.Int64
	killedPlayer    *atomic.Int64
	killedLifecycle *atomic.Int64
	spawnFailures   *atomic.Int64
}

// rejectionTelemetry distinguishes unresolved cursor requests from disabled drops.
type rejectionTelemetry struct {
	cursor   *atomic.Int64
	disabled *atomic.Int64
}

func newRejectionTelemetry(reg *status.Registry, domain string) rejectionTelemetry {
	return rejectionTelemetry{
		cursor:   reg.Ints.Get(domain + ".cursor_rejects"),
		disabled: reg.Ints.Get(domain + ".disabled_rejects"),
	}
}

func (r *rejectionTelemetry) Reset() {
	r.cursor.Store(0)
	r.disabled.Store(0)
}

func newLifecycleTelemetry(reg *status.Registry, domain string) lifecycleTelemetry {
	return lifecycleTelemetry{
		spawned:         reg.Ints.Get(domain + ".spawned"),
		despawned:       reg.Ints.Get(domain + ".despawned"),
		killedPlayer:    reg.Ints.Get(domain + ".killed_by_player"),
		killedLifecycle: reg.Ints.Get(domain + ".killed_by_lifecycle"),
		spawnFailures:   reg.Ints.Get(domain + ".spawn_failures"),
	}
}

func (s *lifecycleTelemetry) Reset() {
	s.spawned.Store(0)
	s.despawned.Store(0)
	s.killedPlayer.Store(0)
	s.killedLifecycle.Store(0)
	s.spawnFailures.Store(0)
}

func (s *lifecycleTelemetry) RecordKill(world *engine.World, killer core.Entity) {
	if world.ResolveCursor(killer) != 0 {
		s.killedPlayer.Add(1)
		return
	}
	s.killedLifecycle.Add(1)
}

// bounceTelemetry records sub-step work and resolved contacts for composite movers.
type bounceTelemetry struct {
	wall        *atomic.Int64
	boundary    *atomic.Int64
	physicsStep *atomic.Int64
}

func newBounceTelemetry(reg *status.Registry, domain string) bounceTelemetry {
	return bounceTelemetry{
		wall:        reg.Ints.Get(domain + ".wall_collisions"),
		boundary:    reg.Ints.Get(domain + ".boundary_reflections"),
		physicsStep: reg.Ints.Get(domain + ".physics_steps"),
	}
}

func (b *bounceTelemetry) Reset() {
	b.wall.Store(0)
	b.boundary.Store(0)
	b.physicsStep.Store(0)
}

func (b *bounceTelemetry) Record(stats physics.BounceStats) {
	b.wall.Add(int64(stats.WallCollisions))
	b.boundary.Add(int64(stats.BoundaryReflections))
	b.physicsStep.Add(int64(stats.Steps))
}

func storeMax(dst *atomic.Int64, value int64) {
	for old := dst.Load(); value > old; old = dst.Load() {
		if dst.CompareAndSwap(old, value) {
			return
		}
	}
}

package engine

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

// TestEntityDomainRoundTrip verifies the [domain:8][id:56] packing
func TestEntityDomainRoundTrip(t *testing.T) {
	ids := []uint64{0, 1, 2, 1 << 30, core.EntityIDMax}
	for d := core.Domain(0); d < core.DomainCount; d++ {
		for _, id := range ids {
			e := core.MakeEntity(d, id)
			if e.Domain() != d || e.ID() != id || e.Valid() != (id != 0) {
				t.Fatalf("domain %s id %d: got domain %s id %d valid %v",
					d, id, e.Domain(), e.ID(), e.Valid())
			}
		}
	}
}

// TestCreateEntityDomainsDisjoint verifies the per-domain counters never collide
func TestCreateEntityDomainsDisjoint(t *testing.T) {
	w := NewWorld()
	seen := make(map[core.Entity]bool, 20000)
	for range 10000 {
		for _, d := range []core.Domain{core.DomainShared, core.DomainPlayer} {
			e := w.CreateEntity(d)
			if !e.Valid() || e.Domain() != d {
				t.Fatalf("domain %s: created %d, tagged %s", d, uint64(e), e.Domain())
			}
			if seen[e] {
				t.Fatalf("duplicate entity %d", uint64(e))
			}
			seen[e] = true
		}
	}
}

// TestClearResetsBothCounters verifies a cleared world reissues from one
func TestClearResetsBothCounters(t *testing.T) {
	w := NewWorld()
	w.Resources.Player = &PlayerResource{} // Clear() unbinds the roster

	var first [core.DomainCount]core.Entity
	for d := range first {
		first[d] = w.CreateEntity(core.Domain(d))
	}
	for range 100 {
		w.CreateEntity(core.DomainShared)
		w.CreateEntity(core.DomainPlayer)
	}

	w.Clear()
	for d := range first {
		if got := w.CreateEntity(core.Domain(d)); got != first[d] {
			t.Fatalf("domain %d: after Clear got %d, want %d", d, uint64(got), uint64(first[d]))
		}
	}
}

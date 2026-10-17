package engine

import (
	"math"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// A capture installs entities under IDs a peer chose. Whatever they are, the index
// finds, moves and drops each one, holding no more than its byte bound.
func TestStoreIndexHoldsPeerIDsWithinBound(t *testing.T) {
	s := NewWorld().Components.Member
	var es []core.Entity
	for i := range 3000 {
		es = append(es, core.MakeEntity(core.DomainShared, uint64(i+1)<<parameter.StoreIndexPageBits))
	}
	es = append(es, 0, core.MakeEntity(core.DomainPlayer, core.EntityIDMax), core.Entity(math.MaxUint64), core.Entity(2)<<56|5)
	for i, e := range es {
		s.SetComponent(e, component.MemberComponent{HeaderEntity: core.Entity(i)})
	}
	if bound := parameter.StoreIndexFloorBytes + s.index.n*parameter.StoreIndexEntityBytes; s.index.held > bound || len(s.index.far) == 0 {
		t.Fatalf("index holds %d B of %d B bound with %d far, want within and some far", s.index.held, bound, len(s.index.far))
	}
	for i := 0; i < len(es); i += 2 {
		s.RemoveEntity(es[i])
	}
	for i, e := range es {
		got, ok := s.GetComponent(e)
		if want := i%2 == 1; ok != want || ok && got.HeaderEntity != core.Entity(i) {
			t.Fatalf("entity %x: got %v %v, want held %v as %d", uint64(e), got.HeaderEntity, ok, want, i)
		}
	}
	s.RemoveBatch(es)
	if s.CountEntities() != 0 || s.index.n != 0 || s.index.far != nil {
		t.Fatalf("after removing all: %d entities, %d indexed, far %v", s.CountEntities(), s.index.n, s.index.far)
	}
}

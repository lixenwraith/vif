package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
)

// TestACaptureDoesNotShareStorageWithTheLiveWorld: a capture is retained past the
// instant it reads, and a shared component that owns a slice is written in place
// through Store.GetPtr, so one sharing the live array changes under whoever kept
// it. Every such component detaches; a snake body that did not served repairs
// whose pages no longer matched the hashes published for them.
func TestACaptureDoesNotShareStorageWithTheLiveWorld(t *testing.T) {
	st := reflect.TypeOf(SharedWorldState{})
	for i := range st.NumField() {
		f := st.Field(i)
		if f.Type.Kind() != reflect.Slice {
			continue
		}
		v, ok := f.Type.Elem().FieldByName("Value")
		if !ok || !holdsReference(v.Type) {
			continue
		}
		if _, ok := v.Type.MethodByName("DetachSnapshot"); !ok {
			t.Errorf("%s owns a reference and has no DetachSnapshot", v.Type)
		}
	}

	w := NewWorld()
	head := w.CreateEntity(core.DomainShared)
	w.Components.Header.SetComponent(head, component.HeaderComponent{
		Behavior: component.BehaviorGold,
		MemberEntries: []component.MemberEntry{
			{Entity: 11, OffsetX: 0}, {Entity: 12, OffsetX: 1}, {Entity: 13, OffsetX: 2},
		},
	})
	gene := w.CreateEntity(core.DomainShared)
	w.Components.Genotype.SetComponent(gene, component.GenotypeComponent{
		Genes: []float64{0.25, 0.5, 0.75},
	})

	before := w.CaptureSharedWorld()

	// What CompositeSystem does when a member is typed away: tombstone in place,
	// then compact the same backing array.
	h, ok := w.Components.Header.GetPtr(head)
	if !ok {
		t.Fatal("the header left the store")
	}
	h.MemberEntries[1].Entity = 0
	h.MemberEntries = append(h.MemberEntries[:1], h.MemberEntries[2:]...)
	g, ok := w.Components.Genotype.GetPtr(gene)
	if !ok {
		t.Fatal("the genotype left the store")
	}
	g.Genes[0] = 9.5

	if got := before.Header[0].Value.MemberEntries; len(got) != 3 ||
		got[0].Entity != 11 || got[1].Entity != 12 || got[2].Entity != 13 {
		t.Fatalf("the retained capture's member table followed the live world: %v", got)
	}
	if got := before.Genotype[0].Value.Genes[0]; got != 0.25 {
		t.Fatalf("the retained capture's gene vector followed the live world: %v", got)
	}

	// The write-back boundary is the same claim in the other direction: a capture
	// installed into a world must not hand the world its own arrays, or the next
	// tick rewrites the correction baseline the receiver is still comparing against.
	other := NewWorld()
	other.InstallSharedWorld(before)
	oh, ok := other.Components.Header.GetPtr(before.Header[0].Entity)
	if !ok {
		t.Fatal("the install did not place the header")
	}
	oh.MemberEntries[0].Entity = 99
	og, _ := other.Components.Genotype.GetPtr(before.Genotype[0].Entity)
	og.Genes[0] = -1

	if before.Header[0].Value.MemberEntries[0].Entity != 11 {
		t.Fatal("installing the capture let the installed world write back into it")
	}
	if before.Genotype[0].Value.Genes[0] != 0.25 {
		t.Fatal("installing the capture let the installed world write back into its genes")
	}
}

// holdsReference reports whether a value of t can share storage with a copy of it
func holdsReference(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Chan, reflect.Func:
		return true
	case reflect.Array:
		return holdsReference(t.Elem())
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return false
		}
		for i := range t.NumField() {
			if holdsReference(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

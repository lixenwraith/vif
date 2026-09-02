package engine

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
)

// TestACaptureDoesNotShareStorageWithTheLiveWorld is the correction stream's
// baseline invariant seen at its source.
//
// A capture is a reading of one instant, and a correction retains it for several:
// the authority diffs the next capture against it, and a receiver reconstructs
// against the copy it was sent. Two shared components own a slice — a composite
// header's member table and a genotype's gene vector — and both are written in
// place through Store.GetPtr, so a capture that kept the live backing array
// changed under whoever retained it. What that produced was a delta computed
// against a baseline neither side held: the diff saw the mutated array on both
// sides, called the store unchanged, and the receiver reconstructed a body its
// header did not describe.
func TestACaptureDoesNotShareStorageWithTheLiveWorld(t *testing.T) {
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

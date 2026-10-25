package network

import "testing"

// TestNormalizeProducesOneCanonicalForm is the property the manifest path rests on:
// two instances holding equal worlds must encode equal bytes, and an unsorted or
// zero-padded vector would make two identical worlds look different.
func TestNormalizeProducesOneCanonicalForm(t *testing.T) {
	t.Parallel()
	got := CrossingFences{
		{Source: 3, Seq: 9},
		{Source: 1, Seq: 4},
		{Source: 2, Seq: 0}, // produced nothing this world holds
		{Source: 0, Seq: 7}, // not a participant
		{Source: 1, Seq: 2}, // an older reading of the same source
	}.Normalize()

	want := CrossingFences{{Source: 1, Seq: 4}, {Source: 3, Seq: 9}}
	if !got.Equal(want) {
		t.Fatalf("normalized to %v, want %v", got, want)
	}
	// The same content in another order is the same vector.
	other := CrossingFences{{Source: 3, Seq: 9}, {Source: 1, Seq: 4}}.Normalize()
	if !got.Equal(other) {
		t.Fatalf("two orders of one vector normalized differently: %v and %v", got, other)
	}
	if empty := CrossingFences(nil).Normalize(); empty != nil {
		t.Fatalf("an empty vector normalized to %v", empty)
	}
	zeroes := CrossingFences{{Source: 5, Seq: 0}}
	if got := zeroes.Normalize(); got != nil {
		t.Fatalf("a vector of nothing but zeroes normalized to %v", got)
	}
}

// TestSeqTreatsAnUnnamedSourceAsHoldingNothing pins the conservative direction. A
// capture that says nothing about a source claims nothing about it, so its frames
// are kept — a duplicate is repaired by the next correction, a discarded action is
// not.
func TestSeqTreatsAnUnnamedSourceAsHoldingNothing(t *testing.T) {
	t.Parallel()
	f := CrossingFences{{Source: 1, Seq: 4}, {Source: 3, Seq: 9}}
	if got := f.Seq(1); got != 4 {
		t.Fatalf("Seq(1) = %d, want 4", got)
	}
	if got := f.Seq(2); got != 0 {
		t.Fatalf("Seq(2) = %d, want 0 for a source the vector does not name", got)
	}
	var none CrossingFences
	if got := none.Seq(1); got != 0 {
		t.Fatalf("Seq on an empty vector = %d, want 0", got)
	}
}

// TestEqualDistinguishesADifferentBoundary keeps a selective repair honest: a shard
// set assembled under another fence answers a different world, and ValidateShardSet
// refuses it on this comparison.
func TestEqualDistinguishesADifferentBoundary(t *testing.T) {
	t.Parallel()
	base := CrossingFences{{Source: 1, Seq: 4}, {Source: 3, Seq: 9}}
	for name, other := range map[string]CrossingFences{
		"a moved sequence": {{Source: 1, Seq: 5}, {Source: 3, Seq: 9}},
		"a missing source": {{Source: 1, Seq: 4}},
		"an extra source":  {{Source: 1, Seq: 4}, {Source: 2, Seq: 1}, {Source: 3, Seq: 9}},
		"a renamed source": {{Source: 1, Seq: 4}, {Source: 4, Seq: 9}},
		"nothing at all":   nil,
	} {
		if base.Equal(other) {
			t.Fatalf("%s compared equal to the original", name)
		}
	}
	if !base.Equal(CrossingFences{{Source: 1, Seq: 4}, {Source: 3, Seq: 9}}) {
		t.Fatal("an identical vector compared unequal")
	}
}

package engine

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func draws(r *vmath.FastRand, n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = r.Next()
	}
	return out
}

// TestRandResourceSessionZeroIsRoot pins the delegate contract
func TestRandResourceSessionZeroIsRoot(t *testing.T) {
	const root = 0x5EED
	rr := NewRandResource(root)
	if rr.Root() != root || rr.Session() != 0 {
		t.Fatalf("root %x session %d", rr.Root(), rr.Session())
	}
	got := draws(rr.Stream("glyph"), 8)
	want := draws(vmath.NewSeededRand(root, "glyph"), 8)
	if !slices.Equal(got, want) {
		t.Fatal("session 0 stream is not the raw root stream")
	}
}

// TestRandResourceStreamsIndependent asserts label separation and repeatability
func TestRandResourceStreamsIndependent(t *testing.T) {
	rr := NewRandResource(1)
	a := draws(rr.Stream("glyph"), 8)
	b := draws(rr.Stream("storm"), 8)
	if slices.Equal(a, b) {
		t.Fatal("distinct labels share a stream")
	}
	if !slices.Equal(a, draws(rr.Stream("glyph"), 8)) {
		t.Fatal("same label did not reproduce")
	}
}

// TestRandResourceSessionsDiverge covers the :new contract
func TestRandResourceSessionsDiverge(t *testing.T) {
	rr := NewRandResource(99)
	s0 := draws(rr.Stream("glyph"), 8)
	if rr.NextSession() != 1 {
		t.Fatal("session did not advance")
	}
	s1 := draws(rr.Stream("glyph"), 8)
	if slices.Equal(s0, s1) {
		t.Fatal("session advance did not change the stream")
	}
	if rr.Root() != 99 {
		t.Fatal("root mutated")
	}
}

// TestRandResourceReproducible asserts one root replays every session
func TestRandResourceReproducible(t *testing.T) {
	a, b := NewRandResource(0xC0FFEE), NewRandResource(0xC0FFEE)
	for range 3 {
		a.NextSession()
		b.NextSession()
	}
	if !slices.Equal(draws(a.Stream("swarm"), 16), draws(b.Stream("swarm"), 16)) {
		t.Fatal("same root and session count diverged")
	}
}

package engine

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func draws(r *vmath.FastRand, n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = r.Next()
	}
	return out
}

// TestRandResourceSessionZeroIsRoot pins the delegate contract: session 0 folds
// only the domain into the root
func TestRandResourceSessionZeroIsRoot(t *testing.T) {
	const root = 0x5EED
	rr := NewRandResource(root)
	if rr.Root() != root || rr.Session() != 0 {
		t.Fatalf("root %x session %d", rr.Root(), rr.Session())
	}
	got := draws(rr.Stream(core.DomainShared, "glyph"), 8)
	want := draws(vmath.NewSeededRand(vmath.DeriveSeed(root, "shared"), "glyph"), 8)
	if !slices.Equal(got, want) {
		t.Fatal("session 0 shared stream is not the domain-folded root stream")
	}
}

// TestRandResourceStreamsIndependent asserts label separation and repeatability
func TestRandResourceStreamsIndependent(t *testing.T) {
	rr := NewRandResource(1)
	a := draws(rr.Stream(core.DomainShared, "glyph"), 8)
	b := draws(rr.Stream(core.DomainShared, "storm"), 8)
	if slices.Equal(a, b) {
		t.Fatal("distinct labels share a stream")
	}
	if !slices.Equal(a, draws(rr.Stream(core.DomainShared, "glyph"), 8)) {
		t.Fatal("same label did not reproduce")
	}
}

// TestRandResourceDomainsIndependent asserts one label draws differently per
// domain, so a player draw cannot advance a shared stream
func TestRandResourceDomainsIndependent(t *testing.T) {
	rr := NewRandResource(7)
	shared := draws(rr.Stream(core.DomainShared, "combat"), 8)
	player := draws(rr.Stream(core.DomainPlayer, "combat"), 8)
	if slices.Equal(shared, player) {
		t.Fatal("domains share a stream for one label")
	}
	if rr.DomainRoot(core.DomainShared) == rr.DomainRoot(core.DomainPlayer) {
		t.Fatal("domain roots collide")
	}
}

// TestRandResourceSessionsDiverge covers the :new contract
func TestRandResourceSessionsDiverge(t *testing.T) {
	rr := NewRandResource(99)
	s0 := draws(rr.Stream(core.DomainShared, "glyph"), 8)
	if rr.NextSession() != 1 {
		t.Fatal("session did not advance")
	}
	s1 := draws(rr.Stream(core.DomainShared, "glyph"), 8)
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
	if !slices.Equal(
		draws(a.Stream(core.DomainShared, "swarm"), 16),
		draws(b.Stream(core.DomainShared, "swarm"), 16),
	) {
		t.Fatal("same root and session count diverged")
	}
}

package vmath

import (
	"fmt"
	"testing"
)

// TestNewSeededRandMatchesDerive pins the composition
func TestNewSeededRandMatchesDerive(t *testing.T) {
	const root, label = 0xABCDEF, "quasar"
	a, b := NewSeededRand(root, label), NewFastRand(DeriveSeed(root, label))
	for i := range 32 {
		if x, y := a.Next(), b.Next(); x != y {
			t.Fatalf("draw %d: %#x vs %#x", i, x, y)
		}
	}
}

// TestDeriveSeedSensitivity asserts neighbouring inputs do not collide
func TestDeriveSeedSensitivity(t *testing.T) {
	const label = "glyph"
	for root := range uint64(1024) {
		if DeriveSeed(root, label) == DeriveSeed(root+1, label) {
			t.Fatalf("root %d collides with %d", root, root+1)
		}
		if DeriveSeed(root, label) == DeriveSeed(root, label+"x") {
			t.Fatalf("label mutation collides at root %d", root)
		}
	}
}

// TestDeriveSeedNeverZero keeps xorshift out of its absorbing state
func TestDeriveSeedNeverZero(t *testing.T) {
	for i := range 4096 {
		if DeriveSeed(uint64(i), fmt.Sprintf("s%d", i)) == 0 {
			t.Fatalf("zero seed at %d", i)
		}
	}
}

// TestStreamIndependence asserts labelled streams do not overlap in practice
func TestStreamIndependence(t *testing.T) {
	const labels, depth = 32, 1024
	seen := make(map[uint64]string, labels*depth)
	for i := range labels {
		label := fmt.Sprintf("system%02d", i)
		r := NewSeededRand(0x1234, label)
		for k := range depth {
			v := r.Next()
			if prev, dup := seen[v]; dup {
				t.Fatalf("%s draw %d collides with %s", label, k, prev)
			}
			seen[v] = label
		}
	}
}

// TestFastRandIntnRange keeps Lemire bounded
func TestFastRandIntnRange(t *testing.T) {
	r := NewFastRand(7)
	for _, n := range []int{1, 2, 3, 7, 100, 1 << 20} {
		for range 1000 {
			if v := r.Intn(n); v < 0 || v >= n {
				t.Fatalf("Intn(%d) = %d", n, v)
			}
		}
	}
	if v := r.Intn(0); v != 0 {
		t.Fatalf("Intn(0) = %d", v)
	}
	if v := r.Intn(-5); v != 0 {
		t.Fatalf("Intn(-5) = %d", v)
	}
}

// TestSetStateResumesTheSequence is the property a snapshot needs from a stream:
// not that a seed reproduces a sequence from its start, but that a recorded
// position reproduces it from where the run had reached.
func TestSetStateResumesTheSequence(t *testing.T) {
	origin := NewFastRand(0xC0FFEE)
	for range 97 {
		origin.Next()
	}
	mark := origin.State()

	want := make([]uint64, 16)
	for i := range want {
		want[i] = origin.Next()
	}

	// A different generator, at a different position, resumed from the mark.
	resumed := NewFastRand(0xDEADBEEF)
	for range 13 {
		resumed.Next()
	}
	resumed.SetState(mark)
	for i, w := range want {
		if got := resumed.Next(); got != w {
			t.Fatalf("draw %d after resume: got %x want %x", i, got, w)
		}
	}
}

// TestSetStateRejectsZero keeps a zero-valued field from producing a dead stream:
// xorshift64 has a fixed point at zero and would return it forever.
func TestSetStateRejectsZero(t *testing.T) {
	r := NewFastRand(1)
	r.SetState(0)
	if r.State() == 0 {
		t.Fatal("SetState(0) left the generator at its fixed point")
	}
	if r.Next() == 0 || r.Next() == 0 {
		t.Fatal("generator is stuck at zero")
	}
}

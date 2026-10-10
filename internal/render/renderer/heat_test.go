package renderer

import "testing"

// TestHeatNotchesCloseEachTenth: at every width a level of 10k lights exactly
// through the k-th notch, so a lit notch with a dark cell after it reads 10k,
// and a full level lights the whole bar.
func TestHeatNotchesCloseEachTenth(t *testing.T) {
	t.Parallel()
	for width := 10; width <= 2000; width++ {
		k := 0
		for x := range width {
			if !heatNotch(x, width) {
				continue
			}
			k++
			if lit := heatBarLit(10*k, width); lit-1 != x {
				t.Fatalf("width %d: notch %d at column %d, but level %d lights %d cells", width, k, x, 10*k, lit)
			}
		}
		if k != 9 {
			t.Fatalf("width %d: %d notches, want 9", width, k)
		}
		if lit := heatBarLit(100, width); lit != width {
			t.Fatalf("width %d: full level lights %d cells", width, lit)
		}
	}
}

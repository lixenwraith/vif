package renderer

import "testing"

// TestHeadingOctantReachesEveryHeading: each of the eight headings and rest is reachable,
// so a head moving straight up or down draws a vertical glyph, not a diagonal wedge.
func TestHeadingOctantReachesEveryHeading(t *testing.T) {
	t.Parallel()
	tests := []struct {
		velX, velY float64
		want       int
	}{
		{4, 0, 0}, {-4, 1, 1}, {0, 3, 2}, {1, -3, 3},
		{3, 2, 4}, {3, -2, 5}, {-3, 2, 6}, {-3, -2, 7},
		{0, 0, 8},
	}
	for _, tt := range tests {
		if got := headingOctant(tt.velX, tt.velY); got != tt.want {
			t.Errorf("headingOctant(%v, %v) = %d, want %d", tt.velX, tt.velY, got, tt.want)
		}
	}
}

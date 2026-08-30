package system

import "testing"

func TestDigestDifferenceNamesFirstCategory(t *testing.T) {
	base := stateDigest{
		Hash: 1, Positions: 2, Kinetics: 3, Combat: 4,
		Context: 5, Status: 6, Surface: 7,
	}
	tests := []struct {
		name string
		edit func(*stateDigest)
		want string
	}{
		{"positions", func(d *stateDigest) { d.Positions++ }, "positions"},
		{"kinetics", func(d *stateDigest) { d.Kinetics++ }, "kinetics"},
		{"combat", func(d *stateDigest) { d.Combat++ }, "combat"},
		{"context", func(d *stateDigest) { d.Context++ }, "context"},
		{"status", func(d *stateDigest) { d.Status++ }, "status"},
		{"surface", func(d *stateDigest) { d.Surface++ }, "snapshot"},
		{"combined", func(d *stateDigest) { d.Hash++ }, "combined"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			remote := base
			tc.edit(&remote)
			if got := digestDifference(base, remote); got != tc.want {
				t.Fatalf("difference = %q, want %q", got, tc.want)
			}
		})
	}
}

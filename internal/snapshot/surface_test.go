package snapshot

import "testing"

// TestTheComparedSurfaceDropsWhatOneInstanceAloneMoves: a mixed-domain aggregate
// differs between instances whose shared state agrees, so it does not belong in a
// cross-instance comparison; a shared metric and a per-tick work count, which
// differs only when the state it derives from does, do.
func TestTheComparedSurfaceDropsWhatOneInstanceAloneMoves(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]bool{
		"explosion.triggered":  false,
		"combat.hits":          false,
		"nav.recomputes":       true,
		"storm.circle_count":   true,
		"time.game_elapsed_ms": true,
	} {
		if got := SharedKey(key); got != want {
			t.Errorf("SharedKey(%q) = %v, want %v", key, got, want)
		}
	}
}

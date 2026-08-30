package app

import (
	"errors"
	"strings"
	"testing"
)

// TestJoinAdmitsAMatchingParticipant covers the join handshake: a second instance of one
// seed reproduces the session's identity and adopts the host's D-14 map latch,
// arriving at bounds its own terminal would not have produced.
func TestJoinAdmitsAMatchingParticipant(t *testing.T) {
	const seed = 0x5EEDBEEF

	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	host.SetupLevel(100, 30, true, false)

	guest := mustHeadless(t, seed, 180, 56)
	defer guest.Close()

	an := host.JoinAnchor()
	if an.Anchor.MapWidth != 100 || an.Anchor.MapHeight != 30 || an.Anchor.CropOnResize {
		t.Fatalf("host latch = %dx%d crop %t, want 100x30 crop false",
			an.Anchor.MapWidth, an.Anchor.MapHeight, an.Anchor.CropOnResize)
	}
	if err := guest.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}

	var gotW, gotH int
	var gotCrop bool
	guest.World().RunSafe(func() {
		cfg := guest.World().Resources.Config
		gotW, gotH, gotCrop = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
	})
	if gotW != 100 || gotH != 30 || gotCrop {
		t.Fatalf("guest map = %dx%d crop %t, want the host's 100x30 crop false", gotW, gotH, gotCrop)
	}

	// The guest keeps its own terminal; only the shared bounds are adopted
	if guest.Context().Width != 180 || guest.Context().Height != 56 {
		t.Fatalf("guest terminal = %dx%d, want its own 180x56",
			guest.Context().Width, guest.Context().Height)
	}
}

// TestJoinRejectsADifferentSession asserts the handshake is not decorative: a
// participant whose streams derive from another seed is refused before it ticks.
func TestJoinRejectsADifferentSession(t *testing.T) {
	host := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer host.Close()
	guest := mustHeadless(t, 0xC0FFEE, 120, 40)
	defer guest.Close()

	err := guest.Join(host.JoinAnchor())
	if err == nil || !strings.Contains(err.Error(), "seed") {
		t.Fatalf("join with a foreign seed = %v, want a seed mismatch", err)
	}
}

// TestJoinRejectsAMidRunHost covers the position check: nothing transports world
// state, so a joiner can only reproduce a session from its start.
func TestJoinRejectsAMidRunHost(t *testing.T) {
	const seed = 0x5EEDBEEF

	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	host.Tick(5)

	guest := mustHeadless(t, seed, 120, 40)
	defer guest.Close()

	if err := guest.Join(host.JoinAnchor()); !errors.Is(err, ErrJoinMidRun) {
		t.Fatalf("join with a mid-run host = %v, want ErrJoinMidRun", err)
	}
}

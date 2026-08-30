package app

import (
	"os"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/network"
)

// corpusDir is the multi-file corpus the parity criterion needs. The embedded one
// is a single file, so its cursor never rolls over and the divergence below cannot
// occur — which is exactly why every criterion built on it missed this.
const corpusDir = "../../data"

// TestParticipantsShareTheCorpusFingerprintNotItsCursor is the criterion for a
// leak the harness could not see: content glyphs are player-domain, so two
// participants who type differently consume blocks at different rates, and the
// corpus cursor is a position in a shared file list rather than shared state.
//
// The fingerprint — how many files, blocks and lines the corpus holds, and where it
// came from — is shared and stays compared. The file the cursor has reached is not,
// and comparing it desynchronised a live session the moment the two participants
// rolled onto different files: a permanent DESYNC with a world that agreed
// completely.
func TestParticipantsShareTheCorpusFingerprintNotItsCursor(t *testing.T) {
	const seed = 0xC0FFEE
	if _, err := os.Stat(corpusDir); err != nil {
		t.Skipf("multi-file corpus %s not present", corpusDir)
	}

	base := Config{Mode: ModeHeadless, Seed: seed, ForceDefault: true}
	base.ContentPath = corpusDir
	base.ForceDefault = false

	host := base
	host.Width, host.Height = 120, 40
	a, err := NewHeadless(host)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	defer a.Close()
	an := a.JoinAnchor()

	guest := base
	guest.Width, guest.Height = 84, 26
	guest.MapWidth, guest.MapHeight = an.Anchor.MapWidth, an.Anchor.MapHeight
	guest.CropOnResize, guest.LockMap = an.Anchor.CropOnResize, an.Anchor.SessionShared
	b, err := NewHeadless(guest)
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	defer b.Close()
	if err := b.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}
	a.adoptMapLatch(an.Anchor)

	pa, pb := network.NewLoopbackPair(1, 2)
	a.AttachTransport(pa)
	b.AttachTransport(pb)
	for _, x := range []*App{a, b} {
		tickUntilCursor(t, x)
		x.Tick(1)
	}
	mirrorCursors(t, a, b)
	assertSharedParity(t, a, b, -1)

	// One participant consumes more of the corpus than the other, which is what
	// typing does. Drawing directly makes the asymmetry immediate and exact instead
	// of waiting several hundred steps for two scripts to drift apart.
	corpusFile := func(x *App) string {
		var s string
		x.World().RunSafe(func() { s = x.World().Resources.Status.Strings.Get("content.file").Load() })
		return s
	}
	start := corpusFile(a)
	if start != corpusFile(b) {
		t.Fatalf("participants began on different corpus files: %q and %q", start, corpusFile(b))
	}
	var drawn int
	for range 400 {
		if corpusFile(a) != start {
			break
		}
		a.World().RunSafe(func() {
			if res := a.World().Resources.Content; res != nil && res.Provider != nil {
				res.Provider.NextBlock()
			}
		})
		drawn++
	}
	if corpusFile(a) == start {
		t.Fatalf("the corpus cursor never left %q after %d blocks; it may hold one file", start, drawn)
	}

	for i := range 8 {
		a.Tick(1)
		b.Tick(1)
		assertSharedParity(t, a, b, i)
	}
	if corpusFile(a) == corpusFile(b) {
		t.Fatalf("both participants report corpus file %q; the criterion proves nothing", corpusFile(a))
	}
}

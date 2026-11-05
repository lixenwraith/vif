// Test fixtures every group in this package builds on.

package app

import (
	"os"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	link "github.com/lixenwraith/vif/pkg/network"
)

// TestMain hides the machine's config roots. A joiner's config resolves the default
// scenario through them, so an installed one would make "the host's embedded
// scenario" mean something else on a developer's machine than on CI.
func TestMain(m *testing.M) {
	empty, err := os.MkdirTemp("", "vif-app-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", empty)
	_ = os.Setenv(paths.SystemRootsEnv, empty)
	code := m.Run()
	_ = os.RemoveAll(empty)
	os.Exit(code)
}

// mustHeadless builds a driven App on the embedded assets, failing the test on error
func mustHeadless(t *testing.T, seed uint64, w, h int) *App {
	t.Helper()
	a, err := NewHeadless(Config{Seed: seed, Width: w, Height: h, Resources: resource.Options{Embedded: true}})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	return a
}

// portOf is the socket a run's session runs on, as its world holds it: a host's
// door, whether opened at start or with :host, or a joiner's link to its session
// and the door it takes joins at once it hosts.
func portOf(t *testing.T, a *App) *link.SocketPort {
	t.Helper()
	var port engine.NetworkPort
	a.World().RunSafe(func() {
		if r := a.world.Resources.Network; r != nil {
			port = r.Port
		}
	})
	p, ok := port.(*link.SocketPort)
	if !ok {
		t.Fatalf("the run's session runs on %T, not a socket", port)
	}
	return p
}

// dialling reports whether a run's :join is still being dialled, as its commands
// tell it: :host, which nothing opens while one is, says so first.
func dialling(a *App) (busy bool) {
	a.World().RunSafe(func() {
		err := a.ctx.SessionCtl.HostError()
		busy = err != nil && strings.Contains(err.Error(), "being dialled")
	})
	return busy
}

// mustJoiner builds a participant the way ConfigForJoin does for a real join: it
// adopts the session's D-14 bounds before its FSM boots and latches the world as
// shared, with its own terminal. Bounds from that terminal would spawn cursor slot
// zero on a different cell than the host (D-11); no latch would leave the playout
// barrier disengaged, which is invisible until an FSM deadline falls inside the lead.
func mustJoiner(t *testing.T, seed uint64, w, h int, an event.JoinAnchor) *App {
	t.Helper()
	a, err := NewHeadless(Config{
		Seed: seed, Width: w, Height: h, Resources: resource.Options{Embedded: true},
		MapWidth: an.Anchor.MapWidth, MapHeight: an.Anchor.MapHeight,
		CropOnResize: an.Anchor.CropOnResize, LockMap: an.Anchor.SessionShared,
	})
	if err != nil {
		t.Fatalf("headless joiner: %v", err)
	}
	return a
}

// tickUntilCursor advances until the FSM has spawned the first cursor
func tickUntilCursor(t *testing.T, a *App) {
	t.Helper()
	for range 40 {
		a.Tick(1)
		var n int
		a.World().RunSafe(func() { n = a.World().Resources.Player.Count() })
		if n > 0 {
			return
		}
	}
	t.Fatal("no cursor after 40 ticks")
}

// spawnCursor adds one auto-slot cursor at the map centre and settles the request
func spawnCursor(t *testing.T, a *App) {
	t.Helper()
	var before, after int
	a.World().RunSafe(func() { before = a.World().Resources.Player.Count() })
	a.Context().PushEventOrigin(event.EventCursorSpawnRequest,
		&event.CursorSpawnRequestPayload{Auto: true, Center: true}, event.OriginDebug)
	a.Settle()
	a.World().RunSafe(func() { after = a.World().Resources.Player.Count() })
	if after != before+1 {
		t.Fatalf("roster has %d cursors, want %d", after, before+1)
	}
}

func statOf(a *App, key string) (v int64) {
	a.World().RunSafe(func() { v = a.World().Resources.Status.Ints.Get(key).Load() })
	return v
}

// tickAll advances every participant by one tick, which is the paired boundary the
// barrier's fixed playout lead is measured against.
func tickAll(apps []*App) {
	for _, a := range apps {
		a.Tick(1)
	}
}

// statBoolOf reads a boolean counter as an integer, so the numeric helpers above
// can assert both kinds the same way.
func statBoolOf(a *App, key string) (v bool) {
	a.World().RunSafe(func() { v = a.World().Resources.Status.Bools.Get(key).Load() })
	return v
}

// cursorPosition reads one roster entity under the world lock.
func cursorPosition(a *App, e core.Entity) (pos component.PositionComponent) {
	a.World().RunSafe(func() { pos, _ = a.World().Positions.GetPosition(e) })
	return pos
}

// mustCaptureShared reads the shared world, failing the test on error.
func mustCaptureShared(t *testing.T, a *App) snapshot.SharedCapture {
	t.Helper()
	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	return cap
}

// mustRoundTrip is that capture after the encode and decode a receiver performs,
// which is what an install actually holds.
func mustRoundTrip(t *testing.T, a *App) snapshot.SharedCapture {
	t.Helper()
	cap, err := snapshot.DecodeCapture(mustEncode(t, mustCaptureShared(t, a)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return cap
}

// stagingOf is the run a staged install resolved into, valid until it commits or
// is discarded.
func stagingOf(s *corrector.StagedInstall) *App { return s.StagingWorld().(stagingRun).App }

func mustEncode(t *testing.T, cap snapshot.SharedCapture) []byte {
	t.Helper()
	body, err := snapshot.EncodeCapture(cap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return body
}

func mustEncodeCorrection(t *testing.T, cap snapshot.SharedCapture) []byte {
	t.Helper()
	body, err := snapshot.EncodeCapture(cap)
	if err != nil {
		t.Fatalf("correction encode: %v", err)
	}
	return body
}

// installCorrection hands one capture to an instance the way the wire does — as a
// correction body its apply loop resolves — so a criterion drives the real path
// rather than the install under it.
func installCorrection(t *testing.T, a *App, cap snapshot.SharedCapture) {
	t.Helper()
	before := statOf(a, "snapshot.corrections_applied")
	a.corrections.Receive(mustEncodeCorrection(t, cap))
	a.ApplyPendingCorrections()
	if statOf(a, "snapshot.corrections_applied") == before {
		t.Fatalf("the correction at tick %d was not applied", cap.Header.Tick)
	}
}

func mustEncodeRequest(t *testing.T, req snapshot.CorrectionRequest) []byte {
	t.Helper()
	body, err := snapshot.EncodeCorrectionRequest(req)
	if err != nil {
		t.Fatalf("request encode: %v", err)
	}
	return body
}

// Criteria for the manifest exchange: what a receiver asks for, what it refuses,
// and where every refusal ends.

package converge

import (
	"testing"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
)

// exchange is a host and a guest on one link, the guest holding the host's world
// because the first publication is always the keyframe every later delta names.
func exchange(t *testing.T) (host, guest *run) {
	t.Helper()
	runs := session(t, 2, [][2]int{{1, 2}})
	host, guest = runs[0], runs[1]
	if err := host.c.PublishDue(); err != nil {
		t.Fatalf("keyframe: %v", err)
	}
	deliver([]*run{guest}, 1)
	if guest.world.installs() == 0 {
		t.Fatal("the guest never installed the keyframe the exchange is named against")
	}
	return host, guest
}

// diverge moves the guest's prediction away from the host's world, so the next
// index comparison has something to descend into.
func diverge(guest *run, spread int) {
	cap := capture(guest.world.Position().Tick, spread)
	guest.world.setWorld(cap)
}

// outstandingRepair drives the exchange far enough for the guest to be awaiting a
// repair, then builds the repair the host would have sent — optionally corrupted —
// without letting the guest apply it. The interception is white-box on purpose: a
// repair is queued and applied in one drain, so building the same message from the
// same two indexes is the only way to ask what the receiver does with a bad one.
func outstandingRepair(t *testing.T, host, guest *run, corrupt func(*snapshot.CorrectionShardSet)) ([]byte, uint64) {
	t.Helper()
	// A publication the guest has to descend into: not a keyframe, so it leads with
	// the index. The guest is level with the host, because an index is answered at
	// the tick it describes; one round delivers it and lets the guest answer, and the
	// request that comes back is intercepted here rather than served.
	host.world.advance(1)
	guest.world.advance(1)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// The index leads with its root; the guest's differs, so it asks for the
	// sections and answers those.
	deliver([]*run{guest}, 1)
	deliver([]*run{host}, 1)
	deliver([]*run{guest}, 1)

	guest.c.selectiveMu.Lock()
	awaiting := guest.c.selective.awaiting
	guest.c.selectiveMu.Unlock()
	if len(awaiting) == 0 {
		t.Fatal("the guest is not awaiting a repair; the injected divergence produced none")
	}
	a := awaiting[len(awaiting)-1]
	req, _, _ := snapshot.CompareRequest(a.index, a.manifest)
	if req.Converged() {
		t.Fatal("the guest reported convergence; the injected divergence produced no request")
	}

	host.c.publishMu.Lock()
	held, ok := host.c.retainedAtLocked(a.tick)
	host.c.publishMu.Unlock()
	if !ok {
		t.Fatalf("the host retained no capture for tick %d", a.tick)
	}
	set, pages, err := snapshot.BuildShardSet(held.index, req)
	if err != nil || pages == 0 {
		t.Fatalf("build repair: %v (%d pages)", err, pages)
	}
	if corrupt != nil {
		corrupt(&set)
	}
	body, err := snapshot.EncodeShardSet(set)
	if err != nil {
		t.Fatalf("encode repair: %v", err)
	}
	return body, a.tick
}

// TestAFailedProofReachesTheKeyframeFallback: a repair that does not verify is
// refused without touching the world, and the guest asks for a whole world
// instead — the one answer that cannot fail the same way.
func TestAFailedProofReachesTheKeyframeFallback(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	diverge(guest, 9)

	corrupt, _ := outstandingRepair(t, host, guest, func(set *snapshot.CorrectionShardSet) {
		set.Shards[0].Hash++
	})

	installs := guest.world.installs()
	guest.c.applyRepair(corrupt)
	if guest.stat("snapshot.proof_failures") == 0 {
		t.Fatal("a corrupted repair passed its proof")
	}
	if guest.stat("snapshot.keyframe_fallbacks") == 0 {
		t.Fatal("a failed proof did not reach the keyframe fallback")
	}
	if got := guest.world.installs(); got != installs {
		t.Fatalf("a refused repair was installed anyway (%d installs, was %d)", got, installs)
	}
	if !guest.c.Selective().Keyframe {
		t.Fatal("the guest is not waiting for the whole world it asked for")
	}
}

// TestSupersededRepairsAreRefusedRatherThanCombined: a repair answering a baseline
// the receiver has moved past is refused, so two of them can never be spliced into
// one world.
func TestSupersededRepairsAreRefusedRatherThanCombined(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	diverge(guest, 9)

	stale, staleTick := outstandingRepair(t, host, guest, nil)

	// The exchange completes normally: the host serves the request the guest sent,
	// the guest applies it, and the baseline the intercepted copy names is behind
	// the receiver. A newer round then puts a later one outstanding.
	deliver([]*run{host}, 1)
	deliver([]*run{guest}, 1)
	diverge(guest, 13)
	_, freshTick := outstandingRepair(t, host, guest, nil)
	if freshTick <= staleTick {
		t.Fatalf("the second round named tick %d, not later than %d", freshTick, staleTick)
	}

	installs := guest.world.installs()
	guest.c.applyRepair(stale)
	if guest.stat("snapshot.shards_refused") == 0 {
		t.Fatal("a superseded repair was accepted")
	}
	if got := guest.world.installs(); got != installs {
		t.Fatalf("a superseded repair was installed anyway (%d installs, was %d)", got, installs)
	}
	if guest.stat("snapshot.baseline_refusals") == 0 {
		t.Fatal("a superseded repair was refused for some reason other than its baseline")
	}
	if got := guest.stat("snapshot.proof_failures"); got != 0 {
		t.Fatalf("a superseded repair was spliced and then failed its root %d times", got)
	}
}

// TestAWidenedPeerIsServedForItsWholeWindow: a peer dropped out of the exchange for
// a repair wider than the world it aimed at is owed the whole body for every round
// it is out, the final one included. The skip and the fallback are one decision —
// deriving the second from the counter the first spent left the last round sending
// neither an index nor a body, which is a stall the protocol has no answer for.
func TestAWidenedPeerIsServedForItsWholeWindow(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)

	// What serveOne reaches when a repair is not worth sending.
	host.c.widen(uint32(guest.world.local))

	keyframes := host.stat("snapshot.keyframes")
	for round := range parameter.SnapshotManifestSilenceCorrections {
		sent := host.stat("snapshot.correction_bytes_sent")
		host.world.advance(1)
		if err := host.c.Publish(); err != nil {
			t.Fatalf("round %d publish: %v", round, err)
		}
		if host.stat("snapshot.correction_bytes_sent") == sent {
			t.Fatalf("round %d of the widen window left the peer with neither an index nor a body", round)
		}
	}
	if got := host.stat("snapshot.keyframes"); got != keyframes {
		t.Fatalf("the window crossed a keyframe (%d, was %d), which every peer is sent anyway; "+
			"this run did not exercise the fallback it is about", got, keyframes)
	}

	// The window is over: the peer is back in the exchange.
	manifests := host.stat("snapshot.manifests_sent")
	host.world.advance(1)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("publish after the window: %v", err)
	}
	if host.stat("snapshot.manifests_sent") <= manifests {
		t.Fatal("the peer never returned to the selective exchange")
	}
}

// TestAPeerThatProvedTheWorldIsNotSentAKeyframe: a hash-only answer meets the
// convergence floor as a keyframe would, so the keyframe period passing sends none.
func TestAPeerThatProvedTheWorldIsNotSentAKeyframe(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	host.c.publishMu.Lock()
	keyTick := host.c.lastKeyTick
	host.c.publishMu.Unlock()

	for range parameter.SnapshotFloorKeyframeTicks + parameter.SnapshotCorrectionTicks {
		host.world.advance(1)
		guest.world.advance(1)
		if err := host.c.Publish(); err != nil {
			t.Fatalf("publish: %v", err)
		}
		deliver([]*run{guest, host}, 1)
	}
	host.c.publishMu.Lock()
	sent := host.c.lastKeyTick
	host.c.publishMu.Unlock()
	if sent != keyTick {
		t.Fatalf("the host sent a keyframe at %d to a peer that answered hash-only", sent)
	}
	if guest.stat("snapshot.corrections_hash_only") == 0 {
		t.Fatal("the guest never proved it held the host's world")
	}
}

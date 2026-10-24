// Criteria for the manifest exchange: what a receiver asks for, what it refuses,
// and where every refusal ends.

package converge

import (
	"bytes"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestBaselineBytesFollowTheirBaseline: the fallback and join bodies a baseline is
// served in encode that baseline, never one it replaced.
func TestBaselineBytesFollowTheirBaseline(t *testing.T) {
	host, guest := exchange(t)
	check := func() {
		t.Helper()
		for range 2 {
			host.c.sendKeyframeTo(host.world.port, 2, host.world.Position().Tick)
			var assembly network.SnapshotAssembly
			in := make([]network.Inbound, 64)
			n := guest.world.port.(*network.MeshPort).Drain(in)
			done := false
			for _, frame := range in[:n] {
				if frame.Msg == nil || frame.Msg.Type != network.MsgStateCorrection {
					continue
				}
				var err error
				done, err = assembly.Add(frame.Msg.Payload)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, got := assembly.Result()
			want, err := snapshot.EncodeCorrection(host.c.baseline)
			if err != nil || !done || !bytes.Equal(got, want) {
				t.Fatalf("fallback differs from its current baseline: complete=%t err=%v", done, err)
			}
		}
		join, tick, err := host.c.KeyframeAt(host.c.baseline.Header.Tick, time.Now())
		want, wantErr := snapshot.EncodeCapture(host.c.baseline)
		if err != nil || wantErr != nil || tick != host.c.baseline.Header.Tick || !bytes.Equal(join, want) {
			t.Fatalf("join body differs from its current baseline: err=%v", err)
		}
	}
	check()
	host.world.advance(4)
	check() // A fallback refreshes a baseline that is too old.
	host.world.advance(parameter.SnapshotFloorKeyframeTicks)
	if err := host.c.Publish(); err != nil {
		t.Fatal(err)
	}
	deliver([]*run{guest}, 1)
	check() // A scheduled keyframe also replaces the reusable body.

	cap := capture(1, 7)
	cap.Header.Run++
	host.world.setWorld(seal(cap))
	host.world.stamp.Run = cap.Header.Run
	check() // The restarted run can reuse a tick, but never its old bytes.

	cap.Header.Term++
	host.c.installed, host.c.haveBase = seal(cap), true
	host.c.BecomeAuthority(network.HandoffRecord{Term: cap.Header.Term})
	check() // Succession replaces the baseline even at the same tick.
}

// TestAWholeWorldLeavesAsItsLinkDrains: a body owed a peer goes out a chunk at a
// time while no backlog stands, so what waits ahead of the next epoch is a chunk
// rather than the world, and the rest follows as the link delivers it.
func TestAWholeWorldLeavesAsItsLinkDrains(t *testing.T) {
	t.Parallel()
	host, guest, runs := slowLink(t)
	host.c.sendKeyframeTo(host.world.port, 2, host.world.Position().Tick)
	host.c.publishMu.Lock()
	left, total := len(host.c.peers[2].flight.frames), len(host.c.keyCorrection)
	host.c.publishMu.Unlock()
	if total < 8*parameter.SnapshotCorrectionChunkBytes || left == 0 {
		t.Fatalf("a %d-byte world left %d chunks behind on a slow link; want it paced", total, left)
	}
	for range 400 {
		if guest.world.installs() > 0 {
			return
		}
		if err := host.c.PublishDue(); err != nil {
			t.Fatal(err)
		}
		deliver(runs, 1)
	}
	t.Fatal("the rest of the world never followed its first chunks")
}

// TestWhatABusyLinkIsOwedFollowsWithoutAPublication: what a peer is owed while its
// link still delivers an earlier transfer waits behind it rather than being dropped,
// and leaves as the link drains with nothing but the exchange running. A dropped
// answer would wait on a publication a driven session never makes.
func TestWhatABusyLinkIsOwedFollowsWithoutAPublication(t *testing.T) {
	t.Parallel()
	host, guest, runs := slowLink(t)
	host.c.sendKeyframeTo(host.world.port, 2, host.world.Position().Tick)

	host.world.advance(5)
	guest.world.advance(5) // the clock a world at the new tick installs against
	want := host.world.Position().Tick
	host.c.sendKeyframeTo(host.world.port, 2, want)

	for range 400 {
		if guest.world.installedTick() == want {
			return
		}
		deliver(runs, 1)
	}
	t.Fatalf("the guest holds tick %d; the world owed behind the first never followed it", guest.world.installedTick())
}

// slowLink is a host and a guest on one link that drains far slower than the world
// between them, which compresses poorly, so a keyframe spans many chunks.
func slowLink(t *testing.T) (host, guest *run, runs []*run) {
	t.Helper()
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	mesh.Shape(1, 2, network.LinkShape{BytesPerTick: 2000})
	runs = []*run{newRun(t, 1, mesh.Node(1), roster(2)), newRun(t, 2, mesh.Node(2), roster(2))}
	for _, r := range runs {
		r.open(1, nil, false)
	}
	cap := capture(1, 0)
	rng := vmath.NewFastRand(3)
	for i := range 6000 {
		cap.World.Positions = append(cap.World.Positions, engine.StoreEntry[component.PositionComponent]{
			Entity: core.Entity(captureRows + i + 1),
			Value:  component.PositionComponent{X: rng.Intn(1 << 15), Y: rng.Intn(1 << 15)},
		})
	}
	cap.World.NextEntity = uint64(captureRows + 6000 + 1)
	runs[0].world.setWorld(seal(cap))
	for range 8 {
		deliver(runs, 1) // probes give the link the origin a backlog is read against
	}
	return runs[0], runs[1], runs
}

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
	// the root. The guest is level with the host, because a manifest is answered at
	// the tick it describes; one round delivers it and lets the guest answer with its
	// page hashes, and the request that goes back is intercepted here, not served.
	host.world.advance(1)
	guest.world.advance(1)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
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

// discard drops whatever r's port holds without handing it to the protocol.
func discard(r *run) {
	port := r.world.port.(*network.MeshPort)
	buf := make([]network.Inbound, 64)
	for port.Drain(buf) == len(buf) {
	}
}

// TestAManifestNoNewerThanTheProvedWorldIsRefused: an index answering a tick this
// instance has since proved past is refused rather than compared off its tick,
// which would buy a repair refused on arrival.
func TestAManifestNoNewerThanTheProvedWorldIsRefused(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	host.world.advance(1)
	guest.world.advance(1)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	deliver([]*run{guest}, 1)
	proved := guest.stat("snapshot.corrections_hash_only")
	if proved == 0 {
		t.Fatal("the guest did not prove the published tick")
	}
	host.c.publishMu.Lock()
	held, ok := host.c.retainedAtLocked(guest.c.provedTick())
	host.c.publishMu.Unlock()
	if !ok {
		t.Fatal("the host retained no index for the proved tick")
	}
	body, err := snapshot.EncodeManifest(held.index.Summary())
	if err != nil {
		t.Fatal(err)
	}
	refusals := guest.stat("snapshot.baseline_refusals")
	guest.c.ReceiveSelective(uint8(network.MsgStateManifest), 1, body)
	guest.c.Apply()
	if guest.stat("snapshot.baseline_refusals") != refusals+1 || guest.stat("snapshot.corrections_hash_only") != proved {
		t.Fatal("an index for a tick already proved was answered instead of refused")
	}
}

// TestAManifestInHandIsAnsweredInPlace: an index that arrived before its tick is
// answered off the live rows as that tick closes, with no capture, when its root
// agrees; one whose root differs is captured at that close, so the repair has rows.
func TestAManifestInHandIsAnsweredInPlace(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	host.world.advance(1)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	deliver([]*run{guest}, 1)
	guest.world.advance(1)
	guest.c.TickClosed(2)
	if guest.world.indexed != 1 || guest.world.captured != 0 {
		t.Fatalf("an agreeing tick closed with %d indexes and %d captures, want one index alone", guest.world.indexed, guest.world.captured)
	}
	guest.c.Apply()
	if guest.stat("snapshot.corrections_hash_only") != 1 || guest.stat("snapshot.manifests_in_place") != 1 {
		t.Fatalf("hash-only %d, in place %d; want the index in hand answered in place", guest.stat("snapshot.corrections_hash_only"), guest.stat("snapshot.manifests_in_place"))
	}

	host.world.advance(1)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	deliver([]*run{guest}, 1)
	guest.world.setWorld(capture(3, 1))
	guest.c.TickClosed(3)
	if guest.world.indexed != 2 || guest.world.captured != 1 {
		t.Fatalf("a differing tick closed with %d indexes and %d captures, want the capture its repair needs", guest.world.indexed, guest.world.captured)
	}
	guest.c.Apply()
	if guest.stat("snapshot.corrections_hash_only") != 1 || guest.stat("snapshot.manifests_in_place") != 1 {
		t.Fatal("a differing root was answered as proved")
	}
}

// TestARequestThePeerHasOutgrownIsNotServed: a page request for a tick the peer has
// since proved it holds is not built or sent, since the repair would only be
// refused; the proof is the newest hash-only answer.
func TestARequestThePeerHasOutgrownIsNotServed(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	diverge(guest, 9)
	_, tick := outstandingRepair(t, host, guest, nil)
	guest.c.selectiveMu.Lock()
	a := guest.c.selective.awaiting[len(guest.c.selective.awaiting)-1]
	guest.c.selectiveMu.Unlock()
	req, _, _ := snapshot.CompareRequest(a.index, a.manifest)
	req.Term = a.manifest.Header.Term
	body, err := snapshot.EncodeCorrectionRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	discard(host) // the page request in flight; it is re-sent below, after the proof

	host.world.advance(1)
	guest.world.setWorld(host.world.world)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	deliver([]*run{guest}, 1)
	deliver([]*run{host}, 1)
	if guest.c.provedTick() <= tick {
		t.Fatalf("the guest proved %d, not past %d", guest.c.provedTick(), tick)
	}
	sent, refused := host.stat("snapshot.shards_sent"), host.stat("snapshot.shards_refused")
	host.c.ReceiveSelective(uint8(network.MsgStateRequest), 2, body)
	host.c.Apply()
	if host.stat("snapshot.shards_sent") != sent || host.stat("snapshot.shards_refused") != refused+1 {
		t.Fatal("a page request the peer had outgrown was served")
	}
}

// TestADivergedAnswerDoesNotForfeitAPeersProof: a hash-only answer stands as the
// peer's proof for half the floor, so one answer that asks for pages does not make
// the next publication a whole world the repair under way would also deliver.
func TestADivergedAnswerDoesNotForfeitAPeersProof(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	host.c.publishMu.Lock()
	keyTick := host.c.lastKeyTick
	host.c.publishMu.Unlock()
	for i := range parameter.SnapshotFloorKeyframeTicks {
		host.world.advance(1)
		guest.world.advance(1)
		if i == parameter.SnapshotFloorKeyframeTicks*3/4 {
			diverge(guest, 9)
		}
		if err := host.c.Publish(); err != nil {
			t.Fatalf("publish: %v", err)
		}
		deliver([]*run{guest}, 1)
		deliver([]*run{host}, 1)
	}
	host.c.publishMu.Lock()
	sent := host.c.lastKeyTick
	host.c.publishMu.Unlock()
	if sent != keyTick {
		t.Fatalf("the host sent a keyframe at %d for one diverged answer", sent)
	}
	if guest.stat("snapshot.shards_applied") == 0 {
		t.Fatal("the divergence was never repaired selectively")
	}
}

// TestARootMismatchIsRepairedInOneRoundTrip: a manifest carries its partition, so a
// receiver whose root differs answers at once with its page hashes and the repair
// arrives one round trip after the root, with no section index between.
func TestARootMismatchIsRepairedInOneRoundTrip(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	diverge(guest, 9)
	host.world.advance(1)
	guest.world.advance(1)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	deliver([]*run{guest}, 1) // the root differs: the guest answers with its page hashes
	deliver([]*run{host}, 1)  // the repair leaves
	deliver([]*run{guest}, 1) // and lands
	if guest.stat("snapshot.pages_repaired") == 0 {
		t.Fatalf("no repair landed one round trip after the root (%d shards sent)", host.stat("snapshot.shards_sent"))
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

// TestAStaleRequestIsSupersededUnlessItAsksForAWorld: a request naming a tick the
// authority has let go of was overtaken by the indexes sent since, so it costs the
// peer no whole world; one that asks for a whole world gets it, whatever it names.
func TestAStaleRequestIsSupersededUnlessItAsksForAWorld(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	diverge(guest, 9)
	stale := func(world bool) {
		t.Helper()
		guest.c.selectiveMu.Lock()
		guest.c.selective.wantKeyframe = world
		guest.c.selectiveMu.Unlock()
		host.world.advance(1)
		guest.world.advance(1)
		if err := host.c.Publish(); err != nil {
			t.Fatalf("publish: %v", err)
		}
		deliver([]*run{guest}, 1)
		for range parameter.SnapshotManifestRetention {
			host.world.advance(1)
			if err := host.c.Publish(); err != nil {
				t.Fatalf("publish: %v", err)
			}
		}
		deliver([]*run{host}, 1)
		deliver([]*run{guest}, 1)
	}

	stale(false)
	if guest.c.Selective().Keyframe {
		t.Fatal("a superseded request left the guest waiting for a whole world")
	}
	installs := guest.world.installs()
	stale(true)
	if guest.world.installs() == installs {
		t.Fatal("a guest that asked for a whole world was refused one for naming a tick let go")
	}
}

// TestAPeerThatProvedTheWorldIsNotSentAKeyframe: a hash-only answer meets the
// convergence floor as a keyframe would, so the keyframe period passing sends none.
// A participant behind a relay proves it in the relay's own answer.
func TestAPeerThatProvedTheWorldIsNotSentAKeyframe(t *testing.T) {
	t.Parallel()
	for _, relayed := range []bool{false, true} {
		n, links := 2, [][2]int{{1, 2}}
		if relayed {
			n, links = 3, [][2]int{{1, 2}, {2, 3}}
		}
		runs := session(t, n, links)
		host := runs[0]
		if err := host.c.PublishDue(); err != nil {
			t.Fatalf("keyframe: %v", err)
		}
		deliver(runs[1:], 1)
		host.c.publishMu.Lock()
		keyTick := host.c.lastKeyTick
		host.c.publishMu.Unlock()

		for range parameter.SnapshotFloorKeyframeTicks + parameter.SnapshotCorrectionTicks {
			for _, r := range runs {
				r.world.advance(1)
			}
			if err := host.c.Publish(); err != nil {
				t.Fatalf("publish: %v", err)
			}
			deliver(runs, 1)
		}
		host.c.publishMu.Lock()
		sent := host.c.lastKeyTick
		host.c.publishMu.Unlock()
		if sent != keyTick {
			t.Fatalf("relayed=%t: the host's last keyframe is at %d, want %d", relayed, sent, keyTick)
		}
		for _, r := range runs[1:] {
			if r.stat("snapshot.corrections_hash_only") == 0 {
				t.Fatalf("relayed=%t: participant %d never proved it held the host's world", relayed, r.world.local)
			}
		}
	}
}

// TestARepairTravelsInChunksAndIsBoundedByTheKeyframe: a shard set leaves as a body
// does, in chunks the receiver applies only once whole, and a repair's width is
// judged against the keyframe it stands in for rather than against a frame.
func TestARepairTravelsInChunksAndIsBoundedByTheKeyframe(t *testing.T) {
	t.Parallel()
	host, guest := exchange(t)
	diverge(guest, 9)
	body, tick := outstandingRepair(t, host, guest, nil)

	chunks, err := network.EncodeSnapshotChunksOf(1, tick, body, 64)
	if err != nil || len(chunks) < 3 {
		t.Fatalf("chunking the repair: %v (%d chunks)", err, len(chunks))
	}
	from := uint32(host.world.local)
	for _, chunk := range chunks[:len(chunks)-1] {
		guest.c.ReceiveSelective(uint8(network.MsgStateShard), from, chunk)
	}
	guest.c.applySelective()
	if guest.stat("snapshot.pages_repaired") != 0 {
		t.Fatal("a repair was applied before its last chunk arrived")
	}
	guest.c.ReceiveSelective(uint8(network.MsgStateShard), from, chunks[len(chunks)-1])
	guest.c.applySelective()
	if guest.stat("snapshot.pages_repaired") == 0 {
		t.Fatal("the assembled repair was not applied")
	}

	host.c.publishMu.Lock()
	host.c.sizes.Keyframe = 3 * network.MaxPayloadSize
	host.c.publishMu.Unlock()
	if !host.c.repairIsWorthSending(2 * network.MaxPayloadSize) {
		t.Fatal("a repair wider than a frame but narrower than the keyframe was refused")
	}
	if host.c.repairIsWorthSending(4 * network.MaxPayloadSize) {
		t.Fatal("a repair wider than the keyframe was sent")
	}
}

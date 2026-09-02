package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// The selective suite drives whole sessions. What the manifest suite proves about
// the index, these prove about the exchange: which messages actually left, how many
// bytes they were, and that every refusal ends at the keyframe the host was going
// to send anyway.

// selectivePair is a two-participant session with the roster split, ready to
// exchange corrections.
func selectivePair(t *testing.T, seed uint64) (host, guest *App, advance func()) {
	t.Helper()
	host, guest = pair(t, seed, 0)
	mirrorCursors(t, host, guest)
	advance = func() { host.Tick(1); guest.Tick(1) }
	return host, guest, advance
}

// deliverSameTick publishes a correction and settles the whole exchange without
// advancing either participant.
//
// It is the only shape that can assert what was *not* sent. Every tick moves the
// clock-derived half of the compared surface — a region's time in state, the gold
// deadline — so a comparison across one is a comparison of two different instants
// and will always find something. Holding the clock still is what makes "the guest
// already agreed, so nothing travelled" a statement about the protocol rather than
// about the tick it straddled.
func deliverSameTick(t *testing.T, host *App, guests []*App) []string {
	t.Helper()
	return deliverCorrectionNow(t, host, guests, func() {})
}

// divergeGuest perturbs one shared cell on a guest and nowhere else, which is the
// disagreement a repair exists to close.
//
// A crossing would not do: the producer applies it immediately and the authority
// applies it a playout lead later, so the two converge on their own. This is a
// difference the session has no artifact for, which is what a lost frame or a
// mispredicted step actually leaves behind.
func divergeGuest(t *testing.T, guest *App) {
	t.Helper()
	moved := false
	guest.World().RunSafe(func() {
		w := guest.World()
		// The mirror of a cursor this instance does not drive: shared placement,
		// not owner-authored, and present from the first tick of any session.
		e := w.Resources.Player.Slot(0)
		if e == 0 {
			return
		}
		pos, ok := w.Positions.GetPosition(e)
		if !ok {
			return
		}
		pos.X += 3
		w.Positions.SetPosition(e, pos)
		moved = true
	})
	if !moved {
		t.Skip("the fixture session holds no mirrored cursor to perturb")
	}
}

// TestAConvergedGuestReceivesTheIndexAndNoState is requirement 1 as a session: a
// guest whose prediction was right gets hashes and nothing else.
func TestAConvergedGuestReceivesTheIndexAndNoState(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)

	// The first correction is a keyframe — a guest holds no authority yet — and it
	// leaves the two holding one world at one tick. The claim is about the ones
	// after that.
	deliverCorrection(t, host, []*App{guest}, advance)
	shardBytes := statOf(host, "snapshot.shard_bytes_sent")
	bodyBytes := statOf(host, "snapshot.correction_bytes_sent")
	manifestBytes := statOf(host, "snapshot.manifest_bytes_sent")
	hashOnly := statOf(guest, "snapshot.corrections_hash_only")

	for range 4 {
		// One tick each, from one world: the two simulate the same thing, so the
		// index is the whole of what the correction has to carry.
		advance()
		want := deliverSameTick(t, host, []*App{guest})
		assertCorrected(t, want, guest, "guest")
	}

	if got := statOf(guest, "snapshot.corrections_hash_only") - hashOnly; got == 0 {
		t.Fatal("a converged guest recorded no hash-only correction")
	}
	if got := statOf(host, "snapshot.shard_bytes_sent") - shardBytes; got != 0 {
		t.Fatalf("a converged guest was sent %d bytes of state", got)
	}
	if got := statOf(host, "snapshot.correction_bytes_sent") - bodyBytes; got != 0 {
		t.Fatalf("a converged guest was sent %d bytes of correction body", got)
	}
	index := statOf(host, "snapshot.manifest_bytes_sent") - manifestBytes
	if index == 0 {
		t.Fatal("no index was published at all")
	}
	t.Logf("four converged corrections: %d index bytes, no state", index)
}

// TestASelectiveRepairRestoresTheAuthority is the ordinary case: the two
// participants drive their own cursors, so they disagree, and the repair closes it.
func TestASelectiveRepairRestoresTheAuthority(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	repaired := false
	for range 5 {
		inject(t, host, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
		for range 3 {
			advance()
		}
		deliverCorrection(t, host, []*App{guest}, advance)
		advance()
		divergeGuest(t, guest)
		before := statOf(guest, "snapshot.pages_repaired")
		want := deliverSameTick(t, host, []*App{guest})
		assertCorrected(t, want, guest, "guest")
		if statOf(guest, "snapshot.pages_repaired") > before {
			repaired = true
		}
	}
	if !repaired {
		t.Fatal("two participants driving apart never needed a page repaired")
	}
	if got := statOf(guest, "snapshot.proof_failures"); got != 0 {
		t.Fatalf("%d repairs failed their proof on a healthy link", got)
	}
	if got := statOf(guest, "snapshot.keyframe_fallbacks"); got != 0 {
		t.Fatalf("a healthy link fell back to a keyframe %d times", got)
	}
	if got := statOf(guest, "snapshot.shards_refused"); got != 0 {
		t.Fatalf("%d repairs were refused on a healthy link", got)
	}
}

// TestOwnerAuthoredDisagreementNeverDegradesToKeyframes is requirement 4's second
// half and the failure the plan names by hand: a guest keeps its own energy, heat
// and loadout over the host's mirror for the cursor it drives, so those cells
// disagree for the life of the session. A hashed surface that carried them would
// produce a root mismatch no repair could close, and the protocol would fall back
// to a whole world every correction, forever.
func TestOwnerAuthoredDisagreementNeverDegradesToKeyframes(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	var guestCursor core.Entity
	guest.World().RunSafe(func() { guestCursor = guest.World().Resources.Player.Slot(1) })
	if guestCursor == 0 {
		t.Fatal("the guest drives no cursor")
	}

	// Force a standing disagreement: the guest's own cursor holds values the host's
	// mirror does not, which is exactly what a sync period behind looks like.
	setOwnerAuthored := func(a *App, e core.Entity, energy int64, heat int) {
		a.World().RunSafe(func() {
			w := a.World()
			if c, ok := w.Components.Energy.GetPtr(e); ok {
				c.Current = energy
			}
			if c, ok := w.Components.Heat.GetPtr(e); ok {
				c.Current = heat
			}
		})
	}

	fallbacks := statOf(guest, "snapshot.keyframe_fallbacks")
	for round := range 5 {
		setOwnerAuthored(guest, guestCursor, int64(40+round), 10+round)
		setOwnerAuthored(host, guestCursor, 90, 90)
		want := deliverCorrection(t, host, []*App{guest}, advance)
		assertCorrected(t, want, guest, "guest")

		var energy int64
		guest.World().RunSafe(func() {
			if c, ok := guest.World().Components.Energy.GetComponent(guestCursor); ok {
				energy = c.Current
			}
		})
		if energy == 90 {
			t.Fatalf("round %d: the correction adopted the host's mirror of a cursor the guest authors", round)
		}
	}
	if got := statOf(guest, "snapshot.keyframe_fallbacks") - fallbacks; got != 0 {
		t.Fatalf("a standing owner-authored disagreement drove %d keyframe fallbacks", got)
	}
	if got := statOf(guest, "snapshot.proof_failures"); got != 0 {
		t.Fatalf("a standing owner-authored disagreement drove %d proof failures", got)
	}
}

// TestASilentPeerIsSentWholeBodies is the fallback that keeps a peer which cannot
// answer the index from starving: after SnapshotManifestSilenceCorrections
// unanswered manifests the host publishes the Phase 5 body again.
func TestASilentPeerIsSentWholeBodies(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	// The guest stops answering: its ticks still run, but nothing drains or replies
	// to the index. Publishing is driven, so each round is one manifest.
	for range parameter.SnapshotManifestSilenceCorrections + 1 {
		if err := host.PublishCorrection(); err != nil {
			t.Fatalf("publish: %v", err)
		}
		host.Tick(1)
	}
	report := host.SelectiveReport()
	peer, ok := report.PeerState[2]
	if !ok {
		t.Fatalf("the host holds no standing for participant 2: %+v", report.PeerState)
	}
	if peer.Silence < parameter.SnapshotManifestSilenceCorrections {
		t.Fatalf("a peer that answered nothing recorded %d unanswered manifests", peer.Silence)
	}

	// The next publish carries a whole body again, and the guest — still ticking —
	// takes it through the ordinary correction path.
	before := statOf(host, "snapshot.correction_bytes_sent")
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := statOf(host, "snapshot.correction_bytes_sent"); got == before {
		t.Fatal("a silent peer was left with an index it could not answer")
	}
	applied := statOf(guest, "snapshot.corrections_applied")
	for range parameter.NetworkRelayHopLimit {
		advance()
		guest.ApplyPendingCorrections()
		if statOf(guest, "snapshot.corrections_applied") > applied {
			return
		}
	}
	t.Fatal("the fallback body never reached the guest")
}

// TestLinkPacingPricesTheSelectiveWire is requirement 8: the controller's cost
// model is fed the bytes the new protocol actually sends, not the whole delta it
// replaced.
func TestLinkPacingPricesTheSelectiveWire(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	for range 5 {
		deliverCorrection(t, host, []*App{guest}, advance)
	}

	report := host.CadenceReport()
	measured := statOf(host, "snapshot.selective_bytes")
	if measured == 0 {
		t.Fatal("the selective wire size was never measured")
	}
	if report.DeltaBytes != measured {
		t.Fatalf("the controller prices a non-keyframe correction at %d bytes, the wire measured %d",
			report.DeltaBytes, measured)
	}
	if report.KeyframeBytes <= report.DeltaBytes {
		t.Fatalf("a keyframe (%d bytes) is priced no higher than a selective correction (%d)",
			report.KeyframeBytes, report.DeltaBytes)
	}
	// Admission reads the same pair, so a link is judged against what the session
	// will actually put on it.
	sizes := host.cadenceSizes()
	if sizes.Delta != measured || sizes.Keyframe != report.KeyframeBytes {
		t.Fatalf("admission prices %+v, the report says keyframe %d delta %d",
			sizes, report.KeyframeBytes, report.DeltaBytes)
	}
	t.Logf("priced from the wire: keyframe %d bytes, selective correction %d bytes",
		sizes.Keyframe, sizes.Delta)
}

// TestAFailedProofReachesTheKeyframeFallback is requirement 7's session half: a
// repair that does not verify is refused without touching the world, the guest asks
// for a whole world instead, and the world it gets converges it.
func TestAFailedProofReachesTheKeyframeFallback(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)
	advance()
	divergeGuest(t, guest)

	corrupt, awaiting := outstandingRepair(t, host, guest, func(set *CorrectionShardSet) {
		set.Shards[0].Hash++
	})
	_ = awaiting

	before := statOf(guest, "snapshot.proof_failures")
	guest.corrections.applyRepair(corrupt)
	if statOf(guest, "snapshot.proof_failures") <= before {
		t.Fatal("a corrupted repair passed its proof")
	}
	if got := statOf(guest, "snapshot.keyframe_fallbacks"); got == 0 {
		t.Fatal("a failed proof did not reach the keyframe fallback")
	}
	if got := statOf(guest, "snapshot.corrections_applied"); got != 1 {
		t.Fatalf("a refused repair was installed anyway (%d corrections applied)", got)
	}

	// The fallback resolves it: the next correction converges the guest whole, and
	// it does so through the keyframe path rather than by repairing.
	want := deliverSameTick(t, host, []*App{guest})
	assertCorrected(t, want, guest, "guest")
}

// TestSupersededRepairsAreRefusedRatherThanCombined is the other half of
// requirement 7: a repair that answers a baseline the receiver has moved past is
// refused, so two of them can never be spliced into one world.
func TestSupersededRepairsAreRefusedRatherThanCombined(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)
	advance()
	divergeGuest(t, guest)

	stale, staleTick := outstandingRepair(t, host, guest, nil)

	// A newer manifest supersedes what the guest was awaiting, and the guest is now
	// waiting on a repair for a later baseline. The held one answers a state this
	// instance has moved past.
	advance()
	divergeGuest(t, guest)
	_, freshTick := outstandingRepair(t, host, guest, nil)
	if freshTick <= staleTick {
		t.Fatalf("the second round named tick %d, not later than %d", freshTick, staleTick)
	}

	before := statOf(guest, "snapshot.shards_refused")
	baselines := statOf(guest, "snapshot.baseline_refusals")
	guest.corrections.applyRepair(stale)
	if statOf(guest, "snapshot.shards_refused") <= before {
		t.Fatal("a superseded repair was accepted")
	}
	if statOf(guest, "snapshot.baseline_refusals") <= baselines {
		t.Fatal("a superseded repair was refused for some reason other than its baseline")
	}
	if got := statOf(guest, "snapshot.proof_failures"); got != 0 {
		t.Fatalf("a superseded repair was spliced and then failed its root %d times", got)
	}

	// And the session recovers on its own: the fresh baseline is still outstanding,
	// so the next round converges the guest.
	advance()
	want := deliverSameTick(t, host, []*App{guest})
	assertCorrected(t, want, guest, "guest")
}

// outstandingRepair drives the exchange far enough for the guest to be awaiting a
// repair, then builds the repair the host would have sent — optionally corrupted —
// without letting the guest apply it.
//
// The interception is white-box on purpose. A repair reaches the receiver's queue
// and is applied in the same drain, so there is no moment in the live path where a
// test could reach in and change one; building the same message from the same two
// indexes is the only way to ask what the receiver does with a bad one.
func outstandingRepair(t *testing.T, host, guest *App, corrupt func(*CorrectionShardSet)) ([]byte, uint64) {
	t.Helper()
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	host.ApplyPendingCorrections()
	guest.ApplyPendingCorrections() // answers the index, now awaiting a repair

	guest.corrections.selectiveMu.Lock()
	awaiting := guest.corrections.selective.awaiting
	guest.corrections.selectiveMu.Unlock()
	if awaiting == nil {
		t.Fatal("the guest is not awaiting a repair; the injected divergence produced none")
	}
	req, _, _ := compareRequest(awaiting.index, awaiting.manifest)
	if req.Converged() {
		t.Fatal("the guest reported convergence; the injected divergence produced no request")
	}

	host.corrections.publishMu.Lock()
	held, ok := host.corrections.retainedAtLocked(awaiting.tick)
	host.corrections.publishMu.Unlock()
	if !ok {
		t.Fatalf("the host retained no capture for tick %d", awaiting.tick)
	}
	set, pages, err := buildShardSet(held.index, req)
	if err != nil {
		t.Fatalf("build repair: %v", err)
	}
	if pages == 0 {
		t.Fatal("the request asked for no page")
	}
	if corrupt != nil {
		corrupt(&set)
	}
	body, err := EncodeShardSet(set)
	if err != nil {
		t.Fatalf("encode repair: %v", err)
	}
	return body, awaiting.tick
}

// TestSelectiveCorrectionKeepsThePlayerDomainUntouched is requirement 4 seen from
// the world rather than from the index: a selective apply may not create, destroy
// or move a player-domain entity.
func TestSelectiveCorrectionKeepsThePlayerDomainUntouched(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	playerEntities := func(a *App) map[core.Entity]component.PositionComponent {
		out := map[core.Entity]component.PositionComponent{}
		a.World().RunSafe(func() {
			for _, e := range a.World().Positions.Entities() {
				if e.Domain() == core.DomainPlayer {
					pos, _ := a.World().Positions.GetPosition(e)
					out[e] = pos
				}
			}
		})
		return out
	}

	for range 4 {
		inject(t, host, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
		for range 3 {
			advance()
		}
		deliverCorrection(t, host, []*App{guest}, advance)
		advance()
		divergeGuest(t, guest)
		before := playerEntities(guest)
		want := deliverSameTick(t, host, []*App{guest})
		assertCorrected(t, want, guest, "guest")
		after := playerEntities(guest)
		if len(before) != len(after) {
			t.Fatalf("a correction changed the player-domain population from %d to %d",
				len(before), len(after))
		}
		for e, pos := range before {
			got, ok := after[e]
			if !ok {
				t.Fatalf("a correction destroyed player-domain entity %d", uint64(e))
			}
			if got != pos {
				t.Fatalf("a correction moved player-domain entity %d from %v to %v",
					uint64(e), pos, got)
			}
		}
	}
}

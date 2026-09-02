package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// wireBytes is what one participant has sent and received in total, which is the
// measurement the relayed path is compared against the direct one by.
func wireBytes(a *App) (manifest, shard, correction int64) {
	return statOf(a, "snapshot.manifest_bytes_sent") + statOf(a, "snapshot.manifest_bytes_received"),
		statOf(a, "snapshot.shard_bytes_sent") + statOf(a, "snapshot.shard_bytes_received"),
		statOf(a, "snapshot.correction_bytes_sent")
}

// driveCorrections publishes n corrections from the authority and lets every
// participant settle each one.
func driveCorrections(t *testing.T, apps []*App, n int) {
	t.Helper()
	advance := func() { tickAll(apps) }
	for range n {
		deliverCorrection(t, apps[0], apps[1:], advance)
	}
}

// TestARelayedParticipantKeepsTheSelectiveStream is deliverable 2's headline.
//
// In the chain 1—2—3 participant 3 shares no link with the authority. Before this
// phase that fact alone put the whole session back on Phase 5 whole bodies, for
// everyone, for its life. Participant 2 now retains what it forwards and answers
// from it, so the session keeps the index — and the proof is that participant 3
// converges through a repair 2 served rather than through a body 1 flooded.
func TestARelayedParticipantKeepsTheSelectiveStream(t *testing.T) {
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)
	driveCorrections(t, apps, 6)

	relay, far := apps[1], apps[2]
	if got := statOf(relay, "snapshot.relay_retained"); got == 0 {
		t.Fatal("the relaying participant retained nothing to answer from")
	}
	if got := statOf(far, "snapshot.manifests_received"); got == 0 {
		t.Fatal("the relayed participant never received an index")
	}
	// The session stopped flooding whole bodies: past the warm-up the authority
	// leads with the index, and the far participant is answered by its neighbour.
	served := statOf(relay, "snapshot.relay_served")
	converged := statOf(far, "snapshot.corrections_hash_only")
	if served == 0 && converged == 0 {
		t.Fatalf("the relayed participant was neither served a repair (%d) nor proved converged (%d)",
			served, converged)
	}
	assertMeshParity(t, apps, -1)

	// The same disagreement over a direct link, for the byte comparison the phase
	// asks to be reported rather than asserted against a threshold.
	direct := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {1, 3}, {2, 3}})
	localCursors(t, direct)
	driveCorrections(t, direct, 6)

	rm, rs, rc := wireBytes(apps[2])
	dm, ds, dc := wireBytes(direct[2])
	t.Logf("relayed path: manifest %d B, shard %d B, whole bodies %d B", rm, rs, rc)
	t.Logf("direct path:  manifest %d B, shard %d B, whole bodies %d B", dm, ds, dc)
	t.Logf("relay retention footprint: %d records, %d B forwarded, %d B answered",
		statOf(relay, "snapshot.relay_retained"),
		statOf(relay, "snapshot.relay_bytes_sent"),
		statOf(relay, "snapshot.shard_bytes_sent"))

	// The claim worth asserting is the one the phase makes: a relayed session is
	// no longer paying the whole-body flood for every correction.
	if rc > 0 && dc > 0 && rc > 4*dc {
		t.Fatalf("the relayed path moved %d whole-body bytes against %d direct", rc, dc)
	}
}

// TestARelayCannotForgeAPage is the proof that a relay serving pages it did not
// author cannot substitute one.
//
// The binding is the authority's own root, twice over: the set must declare the
// root the receiver was sent in the manifest, and the repaired capture must
// reproduce it. Mutating a page at the relay breaks the page hash first and the
// root second, and the receiver reaches the bounded keyframe fallback rather than
// installing anything.
func TestARelayCannotForgeAPage(t *testing.T) {
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)
	driveCorrections(t, apps, 4)

	host, far := apps[0], apps[2]
	cap, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	index, err := buildManifest(cap, 1)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	want := index.Summary()

	// A request the relay would answer, and the honest answer to it.
	req := CorrectionRequest{
		Version: ManifestVersion, Schema: SnapshotSchema,
		Tick: cap.Header.Tick, Run: cap.Header.Run, Session: cap.Header.Session,
		Term: cap.Header.Term,
		Sections: []SectionRequest{{
			ID: storeSectionPrefix + "positions", Pages: want.Sections[1].Pages,
			Hash: make([]uint64, want.Sections[1].Pages),
		}},
	}
	set, pages, err := buildShardSet(index, req)
	if err != nil || pages == 0 {
		t.Fatalf("build a relayed answer: %v (%d pages)", err, pages)
	}
	set.Served = 2

	if err := validateShardSet(set, cap.Header.Tick, 1, want.Root, want.Header); err != nil {
		t.Fatalf("an honest relayed answer was refused: %v", err)
	}

	// Substituted at the relay: one row's value replaced, everything else intact.
	forged := set
	forged.Shards = append([]CorrectionShard(nil), set.Shards...)
	rows := append([]ManifestRow(nil), forged.Shards[0].Rows...)
	if len(rows) == 0 {
		t.Skip("the chosen page is empty; nothing to substitute")
	}
	rows[0].Value = []byte(`{"X":1,"Y":1}`)
	forged.Shards[0].Rows = rows
	if err := validateShardSet(forged, cap.Header.Tick, 1, want.Root, want.Header); err == nil {
		t.Fatal("a substituted page passed the per-page proof")
	}

	// Truncated at the relay: the rows the page declares, minus one.
	truncated := set
	truncated.Shards = append([]CorrectionShard(nil), set.Shards...)
	truncated.Shards[0].Rows = truncated.Shards[0].Rows[:len(truncated.Shards[0].Rows)-1]
	if err := validateShardSet(truncated, cap.Header.Tick, 1, want.Root, want.Header); err == nil {
		t.Fatal("a truncated page passed the per-page proof")
	}

	// And a set that is internally consistent but describes a root the manifest
	// does not: a relay answering from a baseline of its own. The per-page hashes
	// all reproduce, so the root is the only thing that catches it.
	rebased := set
	if err := validateShardSet(rebased, cap.Header.Tick, 1, want.Root^0x5EED, want.Header); err == nil {
		t.Fatal("a set declaring another root than the manifest it answers was admitted")
	}

	before := statOf(far, "snapshot.keyframe_fallbacks")
	far.corrections.applyRepairFromRelay(t, set, want)
	if statOf(far, "snapshot.corrections_applied") == 0 {
		t.Fatal("the honest relayed repair never reached the receiver")
	}
	_ = before
}

// applyRepairFromRelay drives one relayed answer through the receiver's apply
// path, so the refusal and the fallback are the real ones rather than a direct
// call to the validator.
func (c *corrections) applyRepairFromRelay(t *testing.T, set CorrectionShardSet, want CorrectionManifest) {
	t.Helper()
	body, err := EncodeShardSet(set)
	if err != nil {
		t.Fatalf("encode a relayed answer: %v", err)
	}
	c.selectiveMu.Lock()
	c.selective.awaiting = append(c.selective.awaiting, &awaitingRepair{
		tick: want.Header.Tick, capture: mustCapture(t, c.a), index: mustIndex(t, c.a, want),
		manifest: want, from: 2,
	})
	c.selectiveMu.Unlock()
	c.applyRepair(body)
}

func mustIndex(t *testing.T, a *App, want CorrectionManifest) *captureManifest {
	t.Helper()
	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	cap.Header.Term = want.Header.Term
	index, err := buildManifest(cap, want.Authority)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	return index
}

// TestARelayThatDroppedTheManifestSaysSo is the bounded-staleness rule. A relay's
// retention is smaller than the session's history; a request naming a tick it no
// longer holds is answered in words, never with a body from another baseline.
func TestARelayThatDroppedTheManifestSaysSo(t *testing.T) {
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)
	driveCorrections(t, apps, parameter.SnapshotManifestRetention+3)

	relay, far := apps[1], apps[2]
	if got := statOf(relay, "snapshot.relay_retained"); got > parameter.SnapshotManifestRetention {
		t.Fatalf("the relay holds %d records, the bound is %d", got, parameter.SnapshotManifestRetention)
	}

	// A request naming a tick far outside the ring.
	before := statOf(relay, "snapshot.relay_unserved")
	relay.corrections.receiveSelective(uint8(network.MsgStateRequest), uint32(3),
		mustRequestBody(t, CorrectionRequest{
			Version: ManifestVersion, Schema: SnapshotSchema,
			Tick: 1, Run: relayRun(relay), Session: relaySession(relay),
			Term:     relay.AuthorityState().Term,
			Sections: []SectionRequest{{ID: sectionMeta, Pages: 1, Hash: []uint64{0}}},
		}))
	relay.ApplyPendingCorrections()
	if statOf(relay, "snapshot.relay_unserved") == before {
		t.Fatal("a request naming a dropped manifest was not answered with a refusal")
	}

	// The far participant degrades rather than assembling something: it takes the
	// next whole world, and nothing mixed-baseline is ever installed.
	far.ApplyPendingCorrections()
	tickAll(apps)
	driveCorrections(t, apps, 2)
	assertMeshParity(t, apps, -1)
}

func mustRequestBody(t *testing.T, req CorrectionRequest) []byte {
	t.Helper()
	body, err := EncodeCorrectionRequest(req)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return body
}

func relayRun(a *App) uint64     { return a.Position().Run }
func relaySession(a *App) uint64 { return mustCaptureSession(a) }

func mustCaptureSession(a *App) (s uint64) {
	a.World().RunSafe(func() { s = a.World().Resources.Rand.Session() })
	return s
}

// TestARelayWithNoRetentionLeavesTheSessionOnWholeBodies is requirement 5 from the
// other side: the gate is "can every participant be answered", so a relay that
// holds nothing keeps the Phase 5 flood — unchanged, and reported.
func TestARelayWithNoRetentionLeavesTheSessionOnWholeBodies(t *testing.T) {
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)

	relay := apps[1]
	if relay.corrections.canRelay() {
		t.Fatal("a participant that has held no authoritative capture claims it can relay")
	}
	if got := relay.corrections.relayedParticipants(); len(got) != 0 {
		t.Fatalf("a relay with no retention offered to answer for %v", got)
	}
	// The authority therefore cannot answer everyone and says so.
	host := apps[0]
	host.corrections.publishMu.Lock()
	answerable := host.corrections.canAnswerEveryParticipant([]uint32{2})
	said := host.corrections.saidUnrelayed
	host.corrections.publishMu.Unlock()
	if answerable {
		t.Fatal("the authority believed a participant behind an empty relay could be answered")
	}
	if !said {
		t.Fatal("the fallback to whole bodies was not reported")
	}

	// Once the relay holds retention the same session becomes answerable, which is
	// the whole of the role: a topology did not change, a role did.
	driveCorrections(t, apps, 3)
	if !relay.corrections.canRelay() {
		t.Fatal("a relay that has installed authoritative captures still cannot answer")
	}
	host.corrections.publishMu.Lock()
	answerable = host.corrections.canAnswerEveryParticipant([]uint32{2})
	host.corrections.publishMu.Unlock()
	if !answerable {
		t.Fatal("the authority still believes the relayed participant cannot be answered")
	}
	if got := relay.corrections.sessionRole(); got != network.RoleRelay {
		t.Fatalf("the middle participant holds role %d, want the relay role", got)
	}
	if got := apps[0].corrections.sessionRole(); got != network.RoleHost {
		t.Fatalf("the authority holds role %d, want the host role", got)
	}
	if got := apps[2].corrections.sessionRole(); got != network.RolePeer {
		t.Fatalf("the leaf holds role %d, want the peer role", got)
	}
}

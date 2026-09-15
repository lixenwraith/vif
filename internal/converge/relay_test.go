// Criteria for the relay role: what a participant may answer for, and what the
// authority does about a participant nobody can answer for.

package converge

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// TestARelayWithNoRetentionLeavesTheSessionOnWholeBodies is the gate the role
// rests on: "can every participant be answered". A relay that holds nothing keeps
// the session on whole bodies — unchanged, and reported — and the same topology
// becomes answerable the moment it holds an authoritative record. A topology did
// not change, a role did.
func TestARelayWithNoRetentionLeavesTheSessionOnWholeBodies(t *testing.T) {
	t.Parallel()
	runs := session(t, 3, [][2]int{{1, 2}, {2, 3}})
	host, relay, leaf := runs[0], runs[1], runs[2]

	if relay.c.canRelay() {
		t.Fatal("a participant that has held no authoritative capture claims it can relay")
	}
	if got := relay.c.relayedParticipants(); len(got) != 0 {
		t.Fatalf("a relay with no retention offered to answer for %v", got)
	}
	// The authority therefore cannot answer everyone and says so.
	if host.c.canAnswer([]uint32{2}) {
		t.Fatal("the authority believed a participant behind an empty relay could be answered")
	}
	if !host.c.Selective().WholeBodies {
		t.Fatal("the fallback to whole bodies was not reported")
	}

	// Once the relay holds retention the same session becomes answerable. It takes
	// one whole world for the relay to hold a record, and one index exchange for it
	// to say upstream who it holds one for.
	if err := host.c.PublishDue(); err != nil {
		t.Fatalf("keyframe: %v", err)
	}
	deliver(runs, 2)
	host.world.advance(1)
	if err := host.c.Publish(); err != nil {
		t.Fatalf("index: %v", err)
	}
	deliver(runs, 3)
	if !relay.c.canRelay() {
		t.Fatal("a relay that has installed an authoritative capture still cannot answer")
	}
	if !host.c.canAnswer([]uint32{2}) {
		t.Fatal("the authority still believes the relayed participant cannot be answered")
	}
	if got := relay.c.role(); got != network.RoleRelay {
		t.Fatalf("the middle participant holds role %d, want the relay role", got)
	}
	if got := host.c.role(); got != network.RoleHost {
		t.Fatalf("the authority holds role %d, want the host role", got)
	}
	if got := leaf.c.role(); got != network.RolePeer {
		t.Fatalf("the leaf holds role %d, want the peer role", got)
	}
}

// TestARelayedAnswerProvesTheAuthoritysRoot is what the relay role rests on: an
// adopted record may be served only because its root is provably the authority's,
// so a substituted or truncated page fails the same check that catches a corrupt
// wire, and a set declaring another root fails it too. The honest answer installs.
func TestARelayedAnswerProvesTheAuthoritysRoot(t *testing.T) {
	t.Parallel()
	runs := session(t, 3, [][2]int{{1, 2}, {2, 3}})
	host, far := runs[0], runs[2]

	authoritative, err := host.world.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	index, err := snapshot.BuildManifest(authoritative, 1)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	want := index.Summary()

	// A request the relay would answer, and the honest answer to it.
	req := snapshot.CorrectionRequest{
		Version: snapshot.ManifestVersion, Schema: snapshot.Schema,
		Tick: want.Header.Tick, Run: want.Header.Run, Session: want.Header.Session,
		Term: want.Header.Term,
		Sections: []snapshot.SectionRequest{{
			ID: snapshot.StoreSectionPrefix + "positions", Pages: want.Sections[1].Pages,
			Hash: make([]uint64, want.Sections[1].Pages),
		}},
	}
	set, pages, err := snapshot.BuildShardSet(index, req)
	if err != nil || pages == 0 {
		t.Fatalf("build a relayed answer: %v (%d pages)", err, pages)
	}
	set.Served = 2
	if err := snapshot.ValidateShardSet(set, want.Header.Tick, 1, want.Root, want.Header); err != nil {
		t.Fatalf("an honest relayed answer was refused: %v", err)
	}

	// Substituted at the relay: one row's value replaced, everything else intact.
	forged := set
	forged.Shards = append([]snapshot.CorrectionShard(nil), set.Shards...)
	rows := append([]snapshot.ManifestRow(nil), forged.Shards[0].Rows...)
	rows[0].Value = []byte(`{"X":1,"Y":1}`)
	forged.Shards[0].Rows = rows
	if err := snapshot.ValidateShardSet(forged, want.Header.Tick, 1, want.Root, want.Header); err == nil {
		t.Fatal("a substituted page passed the per-page proof")
	}

	// Truncated at the relay: the rows the page declares, minus one.
	truncated := set
	truncated.Shards = append([]snapshot.CorrectionShard(nil), set.Shards...)
	truncated.Shards[0].Rows = truncated.Shards[0].Rows[:len(truncated.Shards[0].Rows)-1]
	if err := snapshot.ValidateShardSet(truncated, want.Header.Tick, 1, want.Root, want.Header); err == nil {
		t.Fatal("a truncated page passed the per-page proof")
	}

	// And a set that is internally consistent but describes a root the manifest
	// does not: a relay answering from a baseline of its own. The per-page hashes
	// all reproduce, so the root is the only thing that catches it.
	if err := snapshot.ValidateShardSet(set, want.Header.Tick, 1, want.Root^0x5EED, want.Header); err == nil {
		t.Fatal("a set declaring another root than the manifest it answers was admitted")
	}

	// The honest one reaches the receiver through the apply path rather than the
	// validator, so the refusal and the fallback are the real ones.
	far.world.setWorld(capture(want.Header.Tick, 9))
	if err := far.c.expect(want, 2); err != nil {
		t.Fatalf("await a relayed answer: %v", err)
	}
	body, err := snapshot.EncodeShardSet(set)
	if err != nil {
		t.Fatalf("encode a relayed answer: %v", err)
	}
	far.c.applyRepair(body)
	if far.world.installs() == 0 {
		t.Fatal("the honest relayed repair never reached the receiver")
	}
}

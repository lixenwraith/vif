package app

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// TestTheHostRefusesAPeerThatSkippedItsOwnCheck is the whole point of moving the
// verdict to the authority. A joiner comparing the anchor it was offered is enough
// exactly while every peer runs code that does it; the host owns the session, so it
// has to be able to refuse one that did not.
func TestTheHostRefusesAPeerThatSkippedItsOwnCheck(t *testing.T) {
	// Not parallel: a real socket against wall-clock deadlines.
	host := mustHeadless(t, 0x1DEA, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	addr := host.midRunPort.Addr().String()

	// A guest that reports a different simulation, having done its own checks and
	// passed them: the anchor it was sent is the anchor it holds.
	pending, offered, err := network.DialSession(addr, network.DebugConfig(network.RolePeer, ""))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer pending.Close()

	report := network.JoinerReport{Width: 120, Height: 40, Identity: identityFromAnchor(offered.Anchor)}
	report.Identity.Simulation = "0000000000000000"

	// Complete reports the acceptance; the host's refusal reaches its error channel.
	_ = pending.Complete(nil, report)

	select {
	case err := <-host.midRunPort.Errors():
		if !network.IsIdentityRefusal(err) {
			t.Fatalf("the host refused for %v, want an identity refusal", err)
		}
		if !strings.Contains(err.Error(), "simulation") {
			t.Fatalf("refusal %q does not name the simulation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host never refused a peer running a different simulation")
	}

	if host.guestCount() != 0 || host.midRunPort.PeerCount() != 0 {
		t.Fatalf("the refused peer is still in the session: roster %d peers %d",
			host.guestCount(), host.midRunPort.PeerCount())
	}
}

// TestTheOfferCarriesWhatTheJoinIsCheckedAgainst pins the two halves against each
// other: what a coordinator offers is what it later measures a reply against, so a
// reset between the two cannot turn a valid join into a mismatch or the reverse.
func TestTheOfferCarriesWhatTheJoinIsCheckedAgainst(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x0FFE, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	offer, err := a.assignParticipant()
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if offer.Identity.Protocol != network.ProtocolVersion {
		t.Fatalf("the offer names protocol %d", offer.Identity.Protocol)
	}
	if offer.Identity.Simulation != manifest.FingerprintString() {
		t.Fatalf("the offer names simulation %q, this build is %q",
			offer.Identity.Simulation, manifest.FingerprintString())
	}
	if offer.Identity.CaptureSchema != snapshot.Schema {
		t.Fatalf("the offer names capture schema %d, this build reads %d",
			offer.Identity.CaptureSchema, snapshot.Schema)
	}
	// The offer's identity and this instance's own describe the same session,
	// because one is derived from the anchor the other was read from.
	if err := offer.Identity.Verify(a.sessionIdentity()); err != nil {
		t.Fatalf("a host would refuse itself: %v", err)
	}
}

// TestADifferentCorpusIsRefusedAtTheJoin is the mismatch a person actually hits:
// two machines with different content installed. It is the case the anchor check
// already caught on the joiner, asserted here through the authority's check so it
// stays caught when the joiner is the one that is wrong.
func TestADifferentCorpusIsRefusedAtTheJoin(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x0C0C, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	offer, err := a.assignParticipant()
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	remote := a.sessionIdentity()
	remote.ContentBlocks += 3

	err = offer.Identity.Verify(remote)
	if err == nil {
		t.Fatal("a peer with a different corpus was accepted")
	}
	if !strings.Contains(err.Error(), "content_blocks") {
		t.Fatalf("refusal %q does not name the corpus", err)
	}
	if !network.IsIdentityRefusal(err) {
		t.Fatalf("refusal %q is not recognisable across the wire", err)
	}
}

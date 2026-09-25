package engine

import (
	"testing"

	"github.com/lixenwraith/vif/internal/core"
)

// TestSaveStreamsReportsOneDomainsIssuedStreams answers the "~24 per-system RNG
// streams" hidden-state survey line (D-19) and which of them a capture may carry
// (D-8). A Player stream is drawn by mechanics only its own participant simulates,
// so a shared capture carrying one makes every correction overwrite each receiver's
// position with the sender's.
func TestSaveStreamsReportsOneDomainsIssuedStreams(t *testing.T) {
	rr := NewRandResource(0x5EED)
	shared := rr.Stream(core.DomainShared, "swarm")
	player := rr.Stream(core.DomainPlayer, "swarm")
	rr.Stream(core.DomainShared, "storm")

	for range 11 {
		shared.Next()
	}
	for range 3 {
		player.Next()
	}

	saved := rr.SaveStreams(core.DomainShared)
	if len(saved) != 2 {
		t.Fatalf("saved %d shared streams, want 2: %v", len(saved), saved)
	}
	// Sorted by label, so two instances that issued the same streams serialize
	// identically.
	for i := 1; i < len(saved); i++ {
		if saved[i-1].Label >= saved[i].Label {
			t.Fatalf("streams are not in a canonical order: %v", saved)
		}
	}

	found := map[string]uint64{}
	for _, st := range saved {
		if st.Domain != core.DomainShared {
			t.Fatalf("a %s stream reached a shared save: %v", core.DomainNames[st.Domain], st)
		}
		found[st.Label] = st.State
	}
	if got := found["swarm"]; got != shared.State() {
		t.Fatalf("shared swarm saved %x, generator is at %x", got, shared.State())
	}
	if _, ok := found["storm"]; !ok {
		t.Fatalf("an issued stream is missing from the inventory: %v", saved)
	}

	// The same label in the other domain is a different stream and reports there.
	if other := rr.SaveStreams(core.DomainPlayer); len(other) != 1 ||
		other[0].State != player.State() {
		t.Fatalf("player streams saved as %v, want the one issued", other)
	}
}

// TestLoadStreamsResumesTheGeneratorsSystemsHold is the property that makes the
// inventory useful: restoring must move the very generator a system drew in Init
// and has held ever since, not a copy the system will never read.
func TestLoadStreamsResumesTheGeneratorsSystemsHold(t *testing.T) {
	origin := NewRandResource(0x5EED)
	held := origin.Stream(core.DomainShared, "quasar")
	for range 40 {
		held.Next()
	}
	saved := origin.SaveStreams(core.DomainShared)
	want := make([]uint64, 8)
	for i := range want {
		want[i] = held.Next()
	}

	// A second resource, its stream at a different position, is handed the capture.
	receiver := NewRandResource(0x5EED)
	receiverHeld := receiver.Stream(core.DomainShared, "quasar")
	for range 7 {
		receiverHeld.Next()
	}
	if unknown := receiver.LoadStreams(core.DomainShared, saved); len(unknown) != 0 {
		t.Fatalf("receiver did not recognise streams it issues: %v", unknown)
	}
	for i, w := range want {
		if got := receiverHeld.Next(); got != w {
			t.Fatalf("draw %d after install: got %x want %x; the pointer the "+
				"system holds was not the one restored", i, got, w)
		}
	}
}

// TestLoadStreamsNamesUnknownStreams keeps a name the receiving build does not
// issue from being dropped. Two sides disagreeing about which streams exist is a
// divergence, and a stream that silently restarts from its seed is one nothing
// else would catch.
func TestLoadStreamsNamesUnknownStreams(t *testing.T) {
	rr := NewRandResource(1)
	rr.Stream(core.DomainShared, "swarm")

	rr.Stream(core.DomainPlayer, "nugget")

	unknown := rr.LoadStreams(core.DomainShared, []StreamState{
		{Domain: core.DomainShared, Label: "swarm", State: 99},
		{Domain: core.DomainShared, Label: "a_system_this_build_does_not_have", State: 7},
		// Issued here, but not this caller's to resume: a shared capture carrying
		// a Player stream is the boundary defect, not a missing stream.
		{Domain: core.DomainPlayer, Label: "nugget", State: 7},
	})
	if len(unknown) != 2 || unknown[0] != "shared:a_system_this_build_does_not_have" ||
		unknown[1] != "player:nugget" {
		t.Fatalf("unknown streams reported as %v", unknown)
	}
}

// TestStreamIssuesAFreshGeneratorPerDraw pins the behaviour a reset depends on: a
// system re-running Init must get its stream from the start of the new session's
// sequence, never resumed at the finished game's position.
func TestStreamIssuesAFreshGeneratorPerDraw(t *testing.T) {
	rr := NewRandResource(3)
	first := rr.Stream(core.DomainShared, "gold")
	for range 20 {
		first.Next()
	}
	second := rr.Stream(core.DomainShared, "gold")
	if second.State() == first.State() {
		t.Fatal("a re-drawn stream resumed the previous generator's position")
	}

	// And the registry now names the live one, because that is the pointer the
	// re-initialized system kept.
	saved := rr.SaveStreams(core.DomainShared)
	if len(saved) != 1 || saved[0].State != second.State() {
		t.Fatalf("inventory names %v, not the generator the system now holds (%x)",
			saved, second.State())
	}
}

package engine

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

// TestSaveStreamsReportsEveryIssuedStream is D-19's answer to the "~24 per-system
// RNG streams" line in the enhancement plan's hidden-state survey: they are
// enumerable because they are issued through one factory, not because anything
// keeps a list by hand.
func TestSaveStreamsReportsEveryIssuedStream(t *testing.T) {
	rr := NewRandResource(0x5EED)
	shared := rr.Stream(core.DomainShared, "swarm")
	player := rr.Stream(core.DomainPlayer, "swarm")
	other := rr.Stream(core.DomainShared, "storm")

	for range 11 {
		shared.Next()
	}
	for range 3 {
		player.Next()
	}

	saved := rr.SaveStreams()
	if len(saved) != 3 {
		t.Fatalf("saved %d streams, want 3: %v", len(saved), saved)
	}
	// Sorted by domain then label, so two instances that issued the same streams
	// serialize identically.
	for i := 1; i < len(saved); i++ {
		if saved[i-1].Domain > saved[i].Domain ||
			(saved[i-1].Domain == saved[i].Domain && saved[i-1].Label >= saved[i].Label) {
			t.Fatalf("streams are not in a canonical order: %v", saved)
		}
	}

	found := map[string]uint64{}
	for _, st := range saved {
		found[core.DomainNames[st.Domain]+":"+st.Label] = st.State
	}
	if got := found["shared:swarm"]; got != shared.State() {
		t.Fatalf("shared:swarm saved %x, generator is at %x", got, shared.State())
	}
	if got := found["player:swarm"]; got != player.State() {
		t.Fatalf("player:swarm saved %x, generator is at %x", got, player.State())
	}
	if _, ok := found["shared:storm"]; !ok {
		t.Fatalf("an issued stream is missing from the inventory: %v", saved)
	}
	_ = other
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
	saved := origin.SaveStreams()
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
	if unknown := receiver.LoadStreams(saved); len(unknown) != 0 {
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

	unknown := rr.LoadStreams([]StreamState{
		{Domain: core.DomainShared, Label: "swarm", State: 99},
		{Domain: core.DomainShared, Label: "a_system_this_build_does_not_have", State: 7},
	})
	if len(unknown) != 1 || unknown[0] != "shared:a_system_this_build_does_not_have" {
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
	saved := rr.SaveStreams()
	if len(saved) != 1 || saved[0].State != second.State() {
		t.Fatalf("inventory names %v, not the generator the system now holds (%x)",
			saved, second.State())
	}
}

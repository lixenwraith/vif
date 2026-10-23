package app

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
)

// memoryClaims is a tab's storage, which outlives the keeper reading it.
type memoryClaims struct{ doc []byte }

func (m *memoryClaims) Load() ([]byte, error) { return m.doc, nil }
func (m *memoryClaims) Store(doc []byte) error {
	m.doc = slices.Clone(doc)
	return nil
}

// offerOf is an offer of the place a roster entry waiting under claim c holds.
func offerOf(c *claim) network.SessionOffer {
	return network.SessionOffer{Assigned: 2, Roster: []network.RosterEntry{{ID: 2, Slot: 1, Claim: network.ClaimDigest(c.Token)}}}
}

// TestADialNamesTheClaimsNoRunHoldsOfItsKind: a seat names its bots' claims and a
// player its own, and none a run is in its session under; an offer of a place one
// waits under hands that claim to the run.
func TestADialNamesTheClaimsNoRunHoldsOfItsKind(t *testing.T) {
	k := newClaimKeeper(nil)
	person, bot := k.issue(false), k.issue(true)
	if got := append(k.tokens(false), k.tokens(true)...); len(got) != 0 {
		t.Fatalf("claims their runs hold were named: %q", got)
	}
	k.release(person)
	k.release(bot)
	if got := k.tokens(false); !slices.Equal(got, []string{person.Token}) {
		t.Fatalf("a player names %q, want its own claim alone", got)
	}
	if got := k.tokens(true); !slices.Equal(got, []string{bot.Token}) {
		t.Fatalf("a seat names %q, want its bot's claim alone", got)
	}
	if k.take(offerOf(person), true) != nil {
		t.Fatal("a seat took a player's claim")
	}
	if k.take(offerOf(person), false) != person || len(k.tokens(false)) != 0 {
		t.Fatal("the offer of a player's place did not hand its claim to the run")
	}
	if k.take(network.SessionOffer{Assigned: 3, Roster: []network.RosterEntry{{ID: 3}}}, true) != nil {
		t.Fatal("an offer of a new place handed over a claim")
	}
}

// TestAPlayerBringsBackWhatItLeftWith: a claim keeps its run's syncs while the run
// is in its session under it, and what the last one held once the run leaves.
func TestAPlayerBringsBackWhatItLeftWith(t *testing.T) {
	k := newClaimKeeper(nil)
	c := k.issue(false)
	left := event.CursorRecord{Energy: 500, Heat: 3, HitPoints: 61}
	k.keep(c, event.CursorRecord{Energy: 100})
	k.keep(c, left)
	k.release(c)
	k.keep(c, event.CursorRecord{Energy: 9999})
	if c.Record == nil || c.Record.Energy != left.Energy || c.Record.HitPoints != left.HitPoints {
		t.Fatalf("the claim brings back %+v, want what the run left with, %+v", c.Record, left)
	}
}

// TestAPlaceCarriedOnKeepsTheRecordItLeftWith: the claim a place goes on under in its
// run's next session starts from the record it left with, and what it plays there
// is kept on that claim alone, so the one it left under brings back the record as it
// stood. Only a claim a run gave up is carried.
func TestAPlaceCarriedOnKeepsTheRecordItLeftWith(t *testing.T) {
	k := newClaimKeeper(nil)
	left := k.issue(true)
	k.keep(left, event.CursorRecord{Energy: 500, Charges: []int{2, 1}})
	if k.carry(network.ClaimDigest(left.Token)) != nil {
		t.Fatal("a claim its run still holds was carried")
	}
	k.release(left)
	carried := k.carry(network.ClaimDigest(left.Token))
	if carried == nil || carried.Token == left.Token || !carried.Bot || carried.Record == nil || carried.Record.Energy != 500 {
		t.Fatalf("the place goes on under %+v; want a bot's claim of its own over the record it left with", carried)
	}
	carried.Record.Charges[0] = 8
	if k.take(offerOf(carried), true) != carried {
		t.Fatal("the place's offer did not hand the run the claim it goes on under")
	}
	k.keep(carried, event.CursorRecord{Energy: 7})
	if left.Record.Energy != 500 || !slices.Equal(left.Record.Charges, []int{2, 1}) {
		t.Fatalf("the claim the place left under brings back %+v, want the record it left with", *left.Record)
	}
	if k.carry(network.ClaimDigest(network.NewClaimToken())) != nil {
		t.Fatal("a claim this process never held was carried")
	}
}

// TestATabKeepsItsClaimsAcrossAReload: a keeper reads back what the tab's storage
// kept, leaving out a document or a token of another form, and keeps at most
// ClaimsKept, the oldest no run holds giving way.
func TestATabKeepsItsClaimsAcrossAReload(t *testing.T) {
	tab := &memoryClaims{}
	k := newClaimKeeper(tab)
	bot := k.issue(true)
	k.keep(bot, event.CursorRecord{Energy: 77})
	k.release(bot)
	reloaded := newClaimKeeper(tab)
	if got := reloaded.tokens(true); !slices.Equal(got, []string{bot.Token}) || reloaded.claims[0].Record.Energy != 77 {
		t.Fatalf("after a reload the tab names %q with %+v, want its bot's claim and record", got, reloaded.claims[0].Record)
	}

	for _, doc := range []string{`not a document`, `[{"token":"0123"}]`, `[null]`} {
		if got := newClaimKeeper(&memoryClaims{doc: []byte(doc)}).claims; len(got) != 0 {
			t.Fatalf("a kept %s was read as claims %+v", doc, got)
		}
	}

	held := k.issue(false)
	for range parameter.ClaimsKept {
		k.release(k.issue(false))
	}
	if len(k.claims) != parameter.ClaimsKept || k.claims[0] != held || slices.Contains(k.claims, bot) {
		t.Fatalf("the keeper holds %d claims, first held %t, oldest kept %t; want %d, the held one kept, the oldest given way",
			len(k.claims), k.claims[0] == held, slices.Contains(k.claims, bot), parameter.ClaimsKept)
	}
}

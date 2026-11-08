package membership

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
)

// memoryPlaces is a tab's storage, which outlives the keeper reading it.
type memoryPlaces struct{ doc []byte }

func (m *memoryPlaces) Load() ([]byte, error) { return m.doc, nil }
func (m *memoryPlaces) Store(doc []byte) error {
	m.doc = slices.Clone(doc)
	return nil
}

// offerOf is an offer of the place a roster entry holds under p's key.
func offerOf(p *place) network.SessionOffer {
	return network.SessionOffer{Assigned: 2, Roster: []network.RosterEntry{{ID: 2, Slot: 1, Key: p.id()}}}
}

// ids is the roster form of each key but the first, the fresh one a dial proves.
func ids(keys []network.Key) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys[1:] {
		out = append(out, k.ID())
	}
	return out
}

// TestADialProvesTheKeysNoRunHoldsOfItsKind: a dial proves a fresh key first, then
// a seat its bots' places and a player its own, none a run is in its session under;
// an offer of a place one of them holds hands that place to the run.
func TestADialProvesTheKeysNoRunHoldsOfItsKind(t *testing.T) {
	k := NewPlaceKeeper(nil)
	person, bot := k.issue(false, network.NewKey()), k.issue(true, network.Key{})
	if !bot.Key.Valid() {
		t.Fatal("a place issued with no key holds none")
	}
	if got := append(ids(k.keys(false)), ids(k.keys(true))...); len(got) != 0 {
		t.Fatalf("places their runs hold were proved: %q", got)
	}
	k.release(person)
	k.release(bot)
	if got := k.keys(false); len(got) != 2 || !got[0].Valid() || got[0].ID() == person.id() || !slices.Equal(ids(got), []string{person.id()}) {
		t.Fatalf("a player proves %v, want a fresh key and its own place alone", ids(got))
	}
	if got := ids(k.keys(true)); !slices.Equal(got, []string{bot.id()}) {
		t.Fatalf("a seat proves %q, want its bot's place alone", got)
	}
	if k.take(offerOf(person), true) != nil {
		t.Fatal("a seat took a player's place")
	}
	if k.take(offerOf(person), false) != person || len(ids(k.keys(false))) != 0 {
		t.Fatal("the offer of a player's place did not hand it to the run")
	}
	if k.take(network.SessionOffer{Assigned: 3, Roster: []network.RosterEntry{{ID: 3}}}, true) != nil {
		t.Fatal("an offer of a new place handed over a kept one")
	}
}

// TestAPlayerBringsBackWhatItLeftWith: a place keeps its run's syncs while the run
// is in its session under it, and what the last one held once the run leaves.
func TestAPlayerBringsBackWhatItLeftWith(t *testing.T) {
	k := NewPlaceKeeper(nil)
	p := k.issue(false, network.NewKey())
	left := event.CursorRecord{Energy: 500, Heat: 3, HitPoints: 61}
	k.keep(p, event.CursorRecord{Energy: 100})
	k.keep(p, left)
	k.release(p)
	k.keep(p, event.CursorRecord{Energy: 9999})
	if p.Record == nil || p.Record.Energy != left.Energy || p.Record.HitPoints != left.HitPoints {
		t.Fatalf("the place brings back %+v, want what the run left with, %+v", p.Record, left)
	}
}

// TestAPlaceCarriedOnKeepsTheRecordItLeftWith: the key a place goes on under in its
// run's next session starts from the record it left with, and what it plays there
// is kept on that place alone, so the one it left brings back the record as it
// stood. Only a place a run gave up is carried.
func TestAPlaceCarriedOnKeepsTheRecordItLeftWith(t *testing.T) {
	k := NewPlaceKeeper(nil)
	left := k.issue(true, network.NewKey())
	k.keep(left, event.CursorRecord{Energy: 500, Charges: []int{2, 1}})
	if k.carry(left.id()) != nil {
		t.Fatal("a place its run still holds was carried")
	}
	k.release(left)
	carried := k.carry(left.id())
	if carried == nil || carried.id() == left.id() || !carried.Bot || carried.Record == nil || carried.Record.Energy != 500 {
		t.Fatalf("the place goes on under %+v; want a bot's key of its own over the record it left with", carried)
	}
	carried.Record.Charges[0] = 8
	if k.take(offerOf(carried), true) != carried {
		t.Fatal("the place's offer did not hand the run the place it goes on under")
	}
	k.keep(carried, event.CursorRecord{Energy: 7})
	if left.Record.Energy != 500 || !slices.Equal(left.Record.Charges, []int{2, 1}) {
		t.Fatalf("the place left brings back %+v, want the record it left with", *left.Record)
	}
	if k.carry(network.NewKey().ID()) != nil {
		t.Fatal("a place this process never held was carried")
	}
}

// TestAStoreKeepsItsPlacesAcrossARestart: a keeper reads back what the store kept,
// the keys signing as they did, leaving out a document or a key of another form,
// and keeps at most PlacesKept, the oldest no run holds giving way.
func TestAStoreKeepsItsPlacesAcrossARestart(t *testing.T) {
	store := &memoryPlaces{}
	k := NewPlaceKeeper(store)
	bot := k.issue(true, network.NewKey())
	k.keep(bot, event.CursorRecord{Energy: 77})
	k.release(bot)
	reloaded := NewPlaceKeeper(store)
	if got := ids(reloaded.keys(true)); !slices.Equal(got, []string{bot.id()}) || reloaded.places[0].Record.Energy != 77 {
		t.Fatalf("after a restart the store proves %q with %+v, want its bot's place and record", got, reloaded.places[0].Record)
	}
	nonce := network.NewNonce()
	proof := reloaded.places[0].Key.Prove(nonce, nil)
	if !network.VerifyProof(network.DecodeKey(bot.id()), proof.Sig, nonce, nil) {
		t.Fatal("a key read back does not sign as the one kept")
	}

	for _, doc := range []string{`not a document`, `[{"key":"AAAA"}]`, `[null]`} {
		if got := NewPlaceKeeper(&memoryPlaces{doc: []byte(doc)}).places; len(got) != 0 {
			t.Fatalf("a kept %s was read as places %+v", doc, got)
		}
	}

	held := k.issue(false, network.NewKey())
	for range parameter.PlacesKept {
		k.release(k.issue(false, network.Key{}))
	}
	if len(k.places) != parameter.PlacesKept || k.places[0] != held || slices.Contains(k.places, bot) {
		t.Fatalf("the keeper holds %d places, first held %t, oldest kept %t; want %d, the held one kept, the oldest given way",
			len(k.places), k.places[0] == held, slices.Contains(k.places, bot), parameter.PlacesKept)
	}
}

// TestAForkLetsItsPlaceGo: what a fork plays is not what its player brings back, so
// the place it was in its session under is let go as it stood, whether its loop
// takes it into a session of its own or it plays on where it stood; the loop is
// asked to take it on before the place goes.
func TestAForkLetsItsPlaceGo(t *testing.T) {
	t.Parallel()
	for _, mode := range []config.Mode{config.ModePlay, config.ModeHeadless} {
		m := wraithWorld()()
		m.run, m.places = stubRun{mode: mode}, NewPlaceKeeper(nil)
		p := m.places.issue(false, network.NewKey())
		m.place.Store(p)
		m.forked(nil, 2)
		if proved := slices.Contains(ids(m.places.keys(false)), p.id()); m.place.Load() != nil || !proved {
			t.Errorf("a %s fork holds its place %t and proves it on its next dial %t; want it let go, proved",
				mode, m.place.Load() != nil, proved)
		}
		if loop := mode == config.ModePlay; m.leaving.Load() != loop {
			t.Errorf("a %s fork asks its loop to take it on %t; want %t", mode, m.leaving.Load(), loop)
		}
	}
}

package membership

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
)

// PlaceKeeper is what one process comes back to its sessions with: the key each of
// its runs and seats holds its place under, and what its cursor held when it left.
// Every run of the process and every seat it holds shares one; its store keeps the
// keys past the process where the host has one.
type PlaceKeeper struct {
	mu     sync.Mutex
	places []*place // oldest first
	store  config.PlaceStore
	stored time.Time // [wall] the last write a sync made
}

// place is one place a run can come back to, under the key it holds it with. Bot
// marks a seat's, which only a seat proves; held is a run in its session under it,
// which no other dial proves.
type place struct {
	Key    network.Key         `json:"key"`
	Bot    bool                `json:"bot,omitempty"`
	Record *event.CursorRecord `json:"record,omitempty"`
	// Pass is the admission the place's last seat gave it, shown at a door that
	// names its session's first authority key (network.ChoosePass).
	Pass *network.Signed `json:"pass,omitempty"`
	held bool
}

// id is the place's key as the roster carries it.
func (p *place) id() string { return p.Key.ID() }

// NewPlaceKeeper reads the places the store kept, if any; a document or a place this
// build cannot read is left out, and replaced at the next change.
func NewPlaceKeeper(store config.PlaceStore) *PlaceKeeper {
	k := &PlaceKeeper{store: store}
	if store == nil {
		return k
	}
	var kept []*place
	if doc, err := store.Load(); err != nil || len(doc) == 0 || json.Unmarshal(doc, &kept) != nil {
		return k
	}
	kept = slices.DeleteFunc(kept, func(p *place) bool { return p == nil || !p.Key.Valid() })
	k.places = kept[max(0, len(kept)-parameter.PlacesKept):]
	return k
}

// keys is what a dial of the kind proves: a fresh key for the place it may be given,
// then each kept place of the kind no run holds, so a place one of them waits under
// is the one its seat is offered.
func (k *PlaceKeeper) keys(bot bool) []network.Key {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := []network.Key{network.NewKey()}
	for _, p := range k.places {
		if p.Bot == bot && !p.held {
			out = append(out, p.Key)
		}
	}
	return out
}

// take holds the place an offer gives back: the one its key the roster entry names.
// Nil is an offer of a new place.
func (k *PlaceKeeper) take(o network.SessionOffer, bot bool) *place {
	entry, _ := o.Entry(o.Assigned)
	if entry.Key == "" {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, p := range k.places {
		if p.Bot == bot && !p.held && p.id() == entry.Key {
			p.held = true
			if s := passFor(o, p.Key); s != nil {
				p.Pass = s
				k.storeLocked()
			}
			return p
		}
	}
	return nil
}

// issue holds a new place under key, the one the dial proved for it, or a key made
// here when it proved none this keeper holds; pass is the offer's for that key
// (passFor), none for a key made here.
func (k *PlaceKeeper) issue(bot bool, key network.Key, pass *network.Signed) *place {
	if !key.Valid() {
		key, pass = network.NewKey(), nil
	}
	p := &place{Key: key, Bot: bot, Pass: pass, held: true}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.addLocked(p)
	return p
}

// passes is what a dial of the kind shows beside its keys: the pass of each kept
// place of the kind no run holds, for the door to pick from (network.ChoosePass).
func (k *PlaceKeeper) passes(bot bool) []*network.Signed {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []*network.Signed
	for _, p := range k.places {
		if p.Bot == bot && !p.held && p.Pass != nil {
			out = append(out, p.Pass)
		}
	}
	return out
}

// passFor is the pass an offer gives the place key holds: the offer's, when it
// names that key and holds under the authority key the offer carries for its term.
func passFor(o network.SessionOffer, key network.Key) *network.Signed {
	if o.Pass == nil || !key.Valid() {
		return nil
	}
	termKey := func(t network.AuthorityTerm) []byte { return network.TermKey(o.TermKeys, t) }
	body, err := network.VerifyPass(o.Pass, termKey, time.Now()) // [wall] the pass's window
	if err != nil || !slices.Equal(body.Key, key.Public()) {
		return nil
	}
	return o.Pass
}

// carry is the place a bot's seat goes on under in the session its run opens next: a
// new key over the record the place left with, so the key it left under keeps that
// record for a return to the session it left. Nil when no place a run gave up has
// that key.
func (k *PlaceKeeper) carry(key string) *place {
	k.mu.Lock()
	defer k.mu.Unlock()
	i := slices.IndexFunc(k.places, func(p *place) bool { return !p.held && p.id() == key })
	if i < 0 {
		return nil
	}
	p := &place{Key: network.NewKey(), Bot: k.places[i].Bot}
	if r := k.places[i].Record; r != nil {
		record := *r
		record.Charges = slices.Clone(r.Charges)
		p.Record = &record
	}
	k.addLocked(p)
	return p
}

// addLocked keeps one more place, and the oldest no run holds give way to it.
// Caller holds mu.
func (k *PlaceKeeper) addLocked(p *place) {
	k.places = append(k.places, p)
	for i := 0; len(k.places) > parameter.PlacesKept && i < len(k.places); i++ {
		if !k.places[i].held {
			k.places = slices.Delete(k.places, i, i+1)
			i--
		}
	}
	k.storeLocked()
}

// keep records the cursor a held place's run plays as its last sync sent it, which
// is what the player brings back should it leave.
func (k *PlaceKeeper) keep(p *place, r event.CursorRecord) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !p.held {
		return
	}
	p.Record = &r
	if now := time.Now(); now.Sub(k.stored) >= parameter.PlaceStoreInterval { // [wall] see PlaceStoreInterval
		k.stored = now
		k.storeLocked()
	}
}

// release gives a place up once its run is out of the session, its record as it was
// then: what the run plays afterwards, alone or elsewhere, is not what it brings back.
func (k *PlaceKeeper) release(p *place) {
	k.mu.Lock()
	defer k.mu.Unlock()
	p.held = false
	k.storeLocked()
}

// hold takes a place back for the run that let it go to redial its host, its record
// as it was: false once a dial took it or newer places pushed it out.
func (k *PlaceKeeper) hold(p *place) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if p.held || !slices.Contains(k.places, p) {
		return false
	}
	p.held = true
	return true
}

// forget drops a place that is gone: its participant was dismissed.
func (k *PlaceKeeper) forget(p *place) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.places = slices.DeleteFunc(k.places, func(o *place) bool { return o == p })
	k.storeLocked()
}

// storeLocked hands the store the places as they stand. Caller holds mu.
func (k *PlaceKeeper) storeLocked() {
	if k.store == nil {
		return
	}
	if doc, err := json.Marshal(k.places); err == nil {
		_ = k.store.Store(doc)
	}
}

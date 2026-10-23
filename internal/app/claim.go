package app

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
)

// ClaimStore keeps a process's claims past the process: a page's, across a reload,
// in the tab's own storage. Without one they last as long as the process does.
type ClaimStore interface {
	Load() ([]byte, error)
	Store([]byte) error
}

// claimKeeper is what one process comes back to its sessions with: the token each
// of its runs and seats joined under, and what its cursor held when it left. Every
// run of the process and every seat it holds shares one.
type claimKeeper struct {
	mu     sync.Mutex
	claims []*claim // oldest first
	store  ClaimStore
	stored time.Time // [wall] the last write a sync made
}

// claim is one place a run can come back to. Bot marks a seat's, which only a seat
// names; held is a run in its session under it, which no other dial names.
type claim struct {
	Token  string              `json:"token"`
	Bot    bool                `json:"bot,omitempty"`
	Record *event.CursorRecord `json:"record,omitempty"`
	held   bool
}

// newClaimKeeper reads the claims store kept, if any; a document or a claim this
// build cannot read is left out, and replaced at the next change.
func newClaimKeeper(store ClaimStore) *claimKeeper {
	k := &claimKeeper{store: store}
	if store == nil {
		return k
	}
	var kept []*claim
	if doc, err := store.Load(); err != nil || len(doc) == 0 || json.Unmarshal(doc, &kept) != nil {
		return k
	}
	kept = slices.DeleteFunc(kept, func(c *claim) bool { return c == nil || !network.ValidClaim(c.Token) })
	k.claims = kept[max(0, len(kept)-parameter.ClaimsKept):]
	return k
}

// tokens is what a dial of the kind names: each claim of it no run holds.
func (k *claimKeeper) tokens(bot bool) []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for _, c := range k.claims {
		if c.Bot == bot && !c.held {
			out = append(out, c.Token)
		}
	}
	return out
}

// take holds the claim an offer gives a place back under: the place it assigns
// waits under that claim's digest. Nil is an offer of a new place.
func (k *claimKeeper) take(o network.SessionOffer, bot bool) *claim {
	entry, _ := o.Entry(o.Assigned)
	if entry.Claim == "" {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, c := range k.claims {
		if c.Bot == bot && !c.held && network.ClaimDigest(c.Token) == entry.Claim {
			c.held = true
			return c
		}
	}
	return nil
}

// issue holds a new claim, for a new place.
func (k *claimKeeper) issue(bot bool) *claim {
	c := &claim{Token: network.NewClaimToken(), Bot: bot, held: true}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.addLocked(c)
	return c
}

// carry is the claim a place goes on under in the session its run opens next: a new
// token over the record the place left with, so the claim it left under keeps that
// record for a return to the session it left. Nil when no claim a run gave up has
// that digest.
func (k *claimKeeper) carry(digest string) *claim {
	k.mu.Lock()
	defer k.mu.Unlock()
	i := slices.IndexFunc(k.claims, func(c *claim) bool { return !c.held && network.ClaimDigest(c.Token) == digest })
	if i < 0 {
		return nil
	}
	c := &claim{Token: network.NewClaimToken(), Bot: k.claims[i].Bot}
	if r := k.claims[i].Record; r != nil {
		record := *r
		record.Charges = slices.Clone(r.Charges)
		c.Record = &record
	}
	k.addLocked(c)
	return c
}

// addLocked keeps one more claim, and the oldest no run holds give way to it.
// Caller holds mu.
func (k *claimKeeper) addLocked(c *claim) {
	k.claims = append(k.claims, c)
	for i := 0; len(k.claims) > parameter.ClaimsKept && i < len(k.claims); i++ {
		if !k.claims[i].held {
			k.claims = slices.Delete(k.claims, i, i+1)
			i--
		}
	}
	k.storeLocked()
}

// keep records the cursor a held claim's run plays as its last sync sent it, which
// is what the player brings back should it leave.
func (k *claimKeeper) keep(c *claim, r event.CursorRecord) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !c.held {
		return
	}
	c.Record = &r
	if now := time.Now(); now.Sub(k.stored) >= parameter.ClaimStoreInterval { // [wall] see ClaimStoreInterval
		k.stored = now
		k.storeLocked()
	}
}

// release gives a claim up once its run is out of the session, its record as it was
// then: what the run plays afterwards, alone or elsewhere, is not what it brings back.
func (k *claimKeeper) release(c *claim) {
	k.mu.Lock()
	defer k.mu.Unlock()
	c.held = false
	k.storeLocked()
}

// hold takes a claim back for the run that let it go to redial its host, its record
// as it was: false once a dial took it or newer claims pushed it out.
func (k *claimKeeper) hold(c *claim) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if c.held || !slices.Contains(k.claims, c) {
		return false
	}
	c.held = true
	return true
}

// forget drops a claim whose place is gone: its participant was dismissed.
func (k *claimKeeper) forget(c *claim) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.claims = slices.DeleteFunc(k.claims, func(o *claim) bool { return o == c })
	k.storeLocked()
}

// storeLocked hands the store the claims as they stand. Caller holds mu.
func (k *claimKeeper) storeLocked() {
	if k.store == nil {
		return
	}
	if doc, err := json.Marshal(k.claims); err == nil {
		_ = k.store.Store(doc)
	}
}

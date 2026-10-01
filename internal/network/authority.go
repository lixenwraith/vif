package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/event"
)

// AuthorityTerm is the authority generation. Term zero is "no session"; a session
// opens at FirstTerm and every successful handoff adds one.
type AuthorityTerm uint64

// FirstTerm is the term the instance that opens a session authors under.
const FirstTerm AuthorityTerm = 1

// AuthorityReport floods loss news beyond the lost authority's direct neighbours.
// Deduplication uses (From, Term); the roster, not reports, elects the successor.
type AuthorityReport struct {
	Term AuthorityTerm `json:"term"`
	From PeerID        `json:"from"`
	Lost PeerID        `json:"lost"`
}

// HandoffRecord is the evidence a receiver needs before it will adopt a term it
// has never seen. It carries the membership the successor is taking over, so
// adopting it is one decision rather than a term change followed by a roster
// negotiation.
type HandoffRecord struct {
	Term        AuthorityTerm `json:"term"`
	Authority   PeerID        `json:"authority"`
	Predecessor PeerID        `json:"predecessor"`

	// The membership, moved whole. Roster and slot assignments are the closed
	// roster (D-11) and must be byte-identical across the handoff; the anchor is
	// what a joiner admitted by the successor adopts.
	Roster []RosterEntry    `json:"roster"`
	Anchor event.JoinAnchor `json:"anchor"`

	// Chain is the candidate list the successor is taking over with, moved whole
	// for the same reason the roster is.
	Chain SuccessionChain `json:"chain,omitempty"`

	// EvidenceTick is the newest retained authoritative tick the successor holds.
	// It is reported rather than enforced: a receiver reads how far back the world
	// it is about to adopt was last proved authoritative, which is the number that
	// says whether the handoff cost it anything.
	EvidenceTick uint64 `json:"evidence_tick"`
}

// HandoffRefusalTag marks a join refused because the session was electing a new
// authority. It travels inside the refusal text because that is what the join
// handshake carries back, and it is a tag rather than a sentence so a joiner can
// recognise it without matching prose.
const HandoffRefusalTag = "authority-handoff"

// IsHandoffRefusal reports whether a join failed because a succession was running,
// which is the one refusal a joiner should retry rather than report.
func IsHandoffRefusal(err error) bool {
	return err != nil && strings.Contains(err.Error(), HandoffRefusalTag)
}

// Validate refuses a handoff record the succession rule could not have produced.
// chain is the receiver's own, for the same reason the roster is: the check a
// receiver makes for itself is the half of the split-brain rule it can make.
func (h HandoffRecord) Validate(roster []RosterEntry, chain SuccessionChain) error {
	if h.Term < FirstTerm {
		return errors.New("handoff carries no authority term")
	}
	if h.Authority == 0 || h.Predecessor == 0 || h.Authority == h.Predecessor {
		return errors.New("handoff names invalid predecessor or authority")
	}
	// The lost process's bots may have crossed their departures before its link
	// closed. Only that subtree can differ across the handoff boundary.
	withoutLost := func(in []RosterEntry) []RosterEntry {
		return slices.DeleteFunc(slices.Clone(in), func(p RosterEntry) bool {
			return p.ID == h.Predecessor || p.Holder == h.Predecessor
		})
	}
	if !SameRoster(withoutLost(h.Roster), withoutLost(roster)) {
		return errors.New("handoff carries a different surviving roster")
	}
	// The whole of the split-brain check, and the receiver makes it for itself
	// rather than counting evidence the record brought with it: the successor a
	// term may name is a function of the roster above, which this instance already
	// holds and has just been shown to agree with.
	want, ok := DesignatedSuccessor(roster, h.Predecessor, chain)
	if !ok {
		return errors.New("handoff names a successor for a roster with no survivor")
	}
	if h.Authority != want {
		return fmt.Errorf("handoff names participant %d as the successor to %d; this roster designates %d",
			h.Authority, h.Predecessor, want)
	}
	return nil
}

// SameRoster reports whether two rosters carry the same identities in the same
// slots. It is order-independent: what has to survive a handoff is the assignment,
// not the order the coordinator happened to store it in.
func SameRoster(a, b []RosterEntry) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	byID := func(p, q RosterEntry) int { return int(p.ID) - int(q.ID) }
	slices.SortFunc(x, byID)
	slices.SortFunc(y, byID)
	return slices.Equal(x, y)
}

// DesignatedSuccessor chooses the first surviving independent chain member, falling
// back to the lowest independent roster identity. Local reach is never an input.
func DesignatedSuccessor(roster []RosterEntry, lost PeerID, chain SuccessionChain) (PeerID, bool) {
	alive := func(id PeerID) bool {
		return id != 0 && id != lost &&
			slices.ContainsFunc(roster, func(p RosterEntry) bool { return p.ID == id && p.Holder == 0 })
	}
	for _, e := range chain {
		if alive(e.ID) {
			return e.ID, true
		}
	}
	best, found := PeerID(0), false
	for _, p := range roster {
		if alive(p.ID) && (!found || p.ID < best) {
			best, found = p.ID, true
		}
	}
	return best, found
}

// EncodeAuthorityReport and its sibling are the wire forms. They are separate
// message kinds rather than shapes of one because a receiver acts on each at a
// different moment: a report is news, and a handoff is a membership change.
func EncodeAuthorityReport(r AuthorityReport) ([]byte, error) { return json.Marshal(r) }

// DecodeAuthorityReport parses one survivor's succession input.
func DecodeAuthorityReport(b []byte) (AuthorityReport, error) {
	var r AuthorityReport
	err := json.Unmarshal(b, &r)
	return r, err
}

// EncodeHandoff renders the record a successor publishes before it authors.
func EncodeHandoff(h HandoffRecord) ([]byte, error) { return json.Marshal(h) }

// DecodeHandoff parses one handoff record.
func DecodeHandoff(b []byte) (HandoffRecord, error) {
	var h HandoffRecord
	err := json.Unmarshal(b, &h)
	return h, err
}

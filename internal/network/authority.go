package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	link "github.com/lixenwraith/vif/pkg/network"

	"github.com/lixenwraith/vif/internal/event"
)

// AuthorityTerm is the authority generation. Term zero is "no session"; a session
// opens at FirstTerm and every successful handoff adds one.
type AuthorityTerm uint64

// FirstTerm is the term the instance that opens a session authors under.
const FirstTerm AuthorityTerm = 1

// AuthorityReport floods loss news beyond the lost authority's direct neighbours. A
// notice names the term the lost authority held; a survivor that adopted a record
// reports that record's term, and Via the neighbour it reaches its host through,
// zero on a link of its own. Deduplication uses (From, Term); the chain and the
// roster, not reports, name the candidates.
type AuthorityReport struct {
	Term AuthorityTerm `json:"term"`
	From link.PeerID   `json:"from"`
	Lost link.PeerID   `json:"lost"`
	Via  link.PeerID   `json:"via,omitempty"`
}

// HandoffRecord is the evidence a receiver needs before it will adopt a term it
// has never seen. It carries the membership the successor is taking over, so
// adopting it is one decision rather than a term change followed by a roster
// negotiation.
type HandoffRecord struct {
	Term        AuthorityTerm `json:"term"`
	Authority   link.PeerID   `json:"authority"`
	Predecessor link.PeerID   `json:"predecessor"`

	// The membership, moved whole. Roster and slot assignments are the closed
	// roster (D-11); apart from the lost predecessor and the bots it holds they
	// must agree across the handoff. The anchor is what a joiner admitted by the
	// successor adopts.
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

// Validate refuses a handoff record the succession rule could not have produced from
// held, the term the receiver holds. chain is the receiver's own, for the same reason
// the roster is: the check a receiver makes for itself is the half of the split-brain
// rule it can make.
func (h HandoffRecord) Validate(roster []RosterEntry, chain SuccessionChain, held AuthorityTerm) error {
	if h.Term < FirstTerm {
		return errors.New("handoff carries no authority term")
	}
	if h.Authority == 0 || h.Predecessor == 0 || h.Authority == h.Predecessor {
		return errors.New("handoff names invalid predecessor or authority")
	}
	// Only players may author, so only they must agree: the lost process may have
	// crossed its departure before its link closed, and a seat crosses at its holder's
	// word, which a receiver a playout behind its relay may hold unapplied still.
	players := func(in []RosterEntry) []RosterEntry {
		return slices.DeleteFunc(slices.Clone(in), func(p RosterEntry) bool {
			return p.ID == h.Predecessor || p.Holder != 0
		})
	}
	if !SameRoster(players(h.Roster), players(roster)) {
		return errors.New("handoff carries different surviving players")
	}
	// The whole of the split-brain check, and the receiver makes it for itself
	// rather than counting evidence the record brought with it: the candidate a
	// term may name is a function of the roster above, which this instance has just
	// been shown to agree with, and of its own chain.
	candidates := SuccessionCandidates(roster, h.Predecessor, chain)
	if h.Term <= held || uint64(h.Term-held) > uint64(len(candidates)) {
		return fmt.Errorf("handoff enters term %d from term %d, which none of %d candidates takes",
			h.Term, held, len(candidates))
	}
	if want := candidates[h.Term-held-1]; h.Authority != want {
		return fmt.Errorf("handoff names participant %d for term %d; this roster's candidate for it is %d",
			h.Authority, h.Term, want)
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

// SuccessionCandidates is the list a lost authority's survivors walk: every
// surviving independent chain member, in chain order. The k-th takes the term k past
// the lost one's, so two hosts' records never tie. One away is no survivor, and a
// participant with no port is no candidate, since nobody could dial it. Local reach
// is never an input.
func SuccessionCandidates(roster []RosterEntry, lost link.PeerID, chain SuccessionChain) []link.PeerID {
	var out []link.PeerID
	for _, e := range chain {
		if e.ID != 0 && e.ID != lost &&
			slices.ContainsFunc(roster, func(p RosterEntry) bool { return p.ID == e.ID && p.Holder == 0 && !p.Away }) {
			out = append(out, e.ID)
		}
	}
	return out
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

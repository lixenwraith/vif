// Package network: who is allowed to author, and for how long.
//
// One instance in a session holds the authoritative Shared world. Until Phase 7
// that was whichever instance started the session, permanently: losing it ended
// the session's shared identity and left every survivor predicting alone. This
// file is the seam that separates *authorship* from *the instance that started
// the session*.
//
// The unit is the **authority term**: a monotonically increasing generation, one
// per session, incremented exactly once per successful handoff. `epoch` was not
// available — in this codebase an epoch is a closed barrier production epoch, one
// tick's worth of artifacts — and the word borrowed instead is Raft's, because
// half the invariant is Raft's: at most one authority per term, and a term never
// goes backwards on any instance.
//
// The other half is not Raft's, and the difference is the topology. Raft elects by
// quorum because its members can all reach each other; a session's members often
// cannot. The shipped CLI dials one address, so a session is a star, and when the
// star's centre goes every survivor is left alone — no survivor can reach any
// other, so no survivor can ever collect a vote, and a quorum rule elects nobody
// in the one shape every real session has. That is why the rule here is not a
// quorum:
//
//   - **The successor is the roster's, not the survivors'.** It is the lowest
//     surviving identity in the closed roster that the session confirmed it could
//     reach — the first guest the coordinator admitted and dialled back, because
//     identities are handed out lowest-free-first in arrival order. Every instance
//     computes it from the roster it already holds (D-11 makes that roster
//     identical everywhere) and the confirmed set the term froze (reach.go), with
//     no messages and no agreement step, so at most one instance can ever conclude
//     that it is the successor. That is what makes split brain impossible rather
//     than unlikely, and it is why succession needs neither a vote nor a
//     randomized timer. A *local* dial result is never an input: two survivors
//     filtering by their own reach would compute two successors.
//
//   - **A term is never adopted, only granted.** A receiver ignores an artifact
//     from a term older than the one it holds and refuses one from a term it has
//     never seen. The only way forward is a handoff record naming an authority the
//     receiver's own roster agrees is the designated one, which is what makes an
//     unheralded higher term a split brain to report rather than a fast successor
//     to follow.
//
//   - **The successor must hold retention.** A candidate with no retained
//     authoritative record has no baseline to publish deltas against and would fan
//     a keyframe out to every survivor at once. Retention *ordering* is
//     deliberately not an eligibility test: with one designated candidate there is
//     no alternative to prefer, and a successor a cadence behind a peer moves that
//     peer back by a cadence — which is what a correction is. What it may not be
//     is empty.
//
// What the rule gives up, and knowingly: a designated successor that went with the
// authority elects nobody, even where other survivors could have agreed on one, so
// the session forks. Deciding that needs agreement on whether the designated one is
// dead, which is the quorum the topology cannot supply. The confirmed set narrows
// this rather than closing it — a candidate the session never reached is skipped
// before it can be elected, so the case left is one that was reachable and then
// went with the authority.
//
// Nothing here authenticates. A participant that can claim another's identity can
// make itself the successor, which is a strictly larger exposure than Phase 6's and
// is stated as such in the plan rather than partly mitigated.
package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/event"
)

// AuthorityTerm is the authority generation. Term zero is "no session"; a session
// opens at FirstTerm and every successful handoff adds one.
type AuthorityTerm uint64

// FirstTerm is the term the instance that opens a session authors under.
const FirstTerm AuthorityTerm = 1

// AuthorityReport is the loss notice a survivor floods when the authority goes.
//
// It carries no election input, because the election has none: the successor is a
// function of the roster every instance already holds. What it carries is the
// *news*, and that is load-bearing on its own — only a direct neighbour of the
// authority sees the link drop, so a participant two links away would otherwise
// wait for a departure crossing whose only producer is the participant that is
// gone. Flooded and deduplicated by (From, Term) like any other artifact.
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
	// roster (D-11) and must be byte-identical across the handoff; the anchor and
	// the barrier delay are what a joiner admitted by the successor adopts.
	Roster            []SessionParticipant `json:"roster"`
	Anchor            event.JoinAnchor     `json:"anchor"`
	BarrierDelayTicks uint64               `json:"barrier_delay_ticks"`

	// The reachability tables, moved whole for the same reason the roster is: a
	// participant that adopts this record is adopting the successor's whole view of
	// who can be reached and where. Reachable is the term's frozen confirmed set
	// and is a succession input; Addresses is the live map and is not. See reach.go.
	Addresses PeerAddresses `json:"addresses,omitempty"`
	Reachable []PeerID      `json:"reachable,omitempty"`

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

// Validate refuses a handoff record that could not have been produced by the
// succession rule, before any of it is adopted.
//
// reachable is the receiver's *own* frozen confirmed set, not the record's, for
// exactly the reason the roster is its own: the check a receiver makes for itself
// is the half of the split-brain rule it can make. Both are identical across the
// term by construction — the roster because D-11 makes the world's cursors
// identical, the confirmed set because it is written by the artifact that opened
// the term and never changed inside it.
func (h HandoffRecord) Validate(roster []SessionParticipant, reachable []PeerID) error {
	if h.Term < FirstTerm {
		return errors.New("handoff carries no authority term")
	}
	if h.Authority == 0 {
		return errors.New("handoff names no authority")
	}
	if len(h.Roster) != len(roster) {
		return fmt.Errorf("handoff carries a roster of %d, this session closed on %d",
			len(h.Roster), len(roster))
	}
	if !SameRoster(h.Roster, roster) {
		return errors.New("handoff carries a different roster than the session closed on")
	}
	// The whole of the split-brain check, and the receiver makes it for itself
	// rather than counting evidence the record brought with it: the successor a
	// term may name is a function of the roster above, which this instance already
	// holds and has just been shown to agree with.
	want, ok := DesignatedSuccessor(roster, h.Predecessor, reachable)
	if !ok {
		return errors.New("handoff names a successor for a roster with no survivor")
	}
	if h.Authority != want {
		return fmt.Errorf("handoff names participant %d as the successor to %d; this roster designates %d",
			h.Authority, h.Predecessor, want)
	}
	if h.BarrierDelayTicks == 0 {
		return errors.New("handoff carries no barrier delay")
	}
	return nil
}

// SameRoster reports whether two rosters carry the same identities in the same
// slots. It is order-independent: what has to survive a handoff is the assignment,
// not the order the coordinator happened to store it in.
func SameRoster(a, b []SessionParticipant) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	byID := func(p, q SessionParticipant) int { return int(p.ID) - int(q.ID) }
	slices.SortFunc(x, byID)
	slices.SortFunc(y, byID)
	return slices.Equal(x, y)
}

// DesignatedSuccessor is the succession rule: the lowest surviving identity in the
// closed roster that the session has confirmed it can reach.
//
// It takes no reports, no links and no votes, and that is the point. Every
// instance computes it from a roster D-11 makes identical everywhere and a
// confirmed set the term froze, so every instance names the same successor without
// exchanging anything — which is what makes "at most one authority per term" hold
// in a star, where survivors cannot exchange anything at all. A quorum rule cannot:
// the star's survivors are mutually unreachable the moment its centre goes, so none
// of them can ever collect a vote and the session forks every time.
//
// The two inputs answer different halves. Lowest identity is the first guest the
// coordinator admitted — identities are handed out lowest-free-first in arrival
// order — so a session that loses its host continues under the participant that has
// been in it longest. The confirmed set is what makes that participant one the
// others can actually reach: a guest behind NAT, behind a firewall, or that chose
// not to advertise plays normally and is skipped here, where before the map existed
// it would take the term and author alone.
//
// A *local* dial result is never an input, and this is the one rule that matters
// most: if each instance filtered candidates by what it could reach, two survivors
// would compute two successors, which is the single outcome this design rules out.
// A survivor that cannot reach the elected successor falls back to the timeout and
// forks — strictly better than the previous behaviour, where nobody reached anybody.
//
// An empty confirmed set is "nothing was ever confirmed" rather than "nobody is
// eligible", so the rule falls back to the roster alone, which is what a session of
// leaves and a session from a build that predates this both get.
//
// The second return is false for a roster with nobody left, which is a session
// with nothing to continue.
func DesignatedSuccessor(roster []SessionParticipant, lost PeerID, reachable []PeerID) (PeerID, bool) {
	if id, ok := lowestSurvivor(roster, lost, reachable); ok {
		return id, true
	}
	return lowestSurvivor(roster, lost, nil)
}

// lowestSurvivor is the rule over one candidate filter; an empty filter admits
// every survivor.
func lowestSurvivor(roster []SessionParticipant, lost PeerID, admit []PeerID) (PeerID, bool) {
	best, found := PeerID(0), false
	for _, p := range roster {
		if p.ID == 0 || p.ID == lost {
			continue
		}
		if len(admit) != 0 && !slices.Contains(admit, p.ID) {
			continue
		}
		if !found || p.ID < best {
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

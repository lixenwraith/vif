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
// the invariant is Raft's: at most one authority per term, and a term never goes
// backwards on any instance.
//
// Three rules carry the whole of it, and each is enforced here rather than at a
// call site:
//
//   - **A term is never adopted, only granted.** A receiver ignores an artifact
//     from a term older than the one it holds and refuses one from a term it has
//     never seen. The only way forward is a handoff record naming the votes it was
//     elected on, which is what makes an unheralded higher term a split brain to
//     report rather than a fast successor to follow.
//
//   - **One vote per participant per term.** A survivor votes once for the lowest
//     eligible candidate in its own view and never revises it, so two candidates
//     cannot both collect a strict majority of a closed roster. That is what makes
//     split brain impossible rather than unlikely, and it is why succession needs
//     no randomized timer to break ties.
//
//   - **Eligibility is a function of the roster and the survivor set.** A
//     candidate must be directly linked to a strict majority of the roster and
//     must hold retention as new as the newest any survivor reports. The first
//     keeps a minority partition from electing; the second keeps a participant
//     that has been silently behind from becoming the thing everyone else adopts.
//
// Nothing here authenticates. A participant that can lie about its links or its
// retention can make itself the successor, which is a strictly larger exposure
// than Phase 6's and is stated as such in the plan rather than partly mitigated.
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

// AuthorityReport is one survivor's input to a succession: what it can reach and
// how current its retention is.
//
// It is information rather than a decision, so it is revisable and idempotent —
// flooded and deduplicated by (From, Term) like any other artifact. The decision
// is the vote below, which is neither.
type AuthorityReport struct {
	Term  AuthorityTerm `json:"term"`
	From  PeerID        `json:"from"`
	Lost  PeerID        `json:"lost"`
	Links []PeerID      `json:"links,omitempty"`

	// RetainedTick is the newest authoritative tick this participant holds an
	// index over, and Retained how many such records it has. Together they are
	// requirement (b): a successor may not be an instance that has been silently
	// behind, and the retained ring is the evidence because a fresh capture proves
	// only what the candidate believes.
	RetainedTick uint64 `json:"retained_tick"`
	Retained     int    `json:"retained"`
}

// AuthorityVote is one participant's single, immutable choice for one term.
type AuthorityVote struct {
	Term      AuthorityTerm `json:"term"`
	Voter     PeerID        `json:"voter"`
	Candidate PeerID        `json:"candidate"`
}

// HandoffRecord is the evidence a receiver needs before it will adopt a term it
// has never seen. It carries the membership the successor is taking over as well
// as the votes it was elected on, so adopting it is one decision rather than a
// term change followed by a roster negotiation.
type HandoffRecord struct {
	Term        AuthorityTerm `json:"term"`
	Authority   PeerID        `json:"authority"`
	Predecessor PeerID        `json:"predecessor"`

	// Voters is the strict majority that elected this authority. A receiver counts
	// it against the roster it already holds; a record that does not carry one is
	// refused, which is the half of the split-brain rule a receiver can check for
	// itself.
	Voters []PeerID `json:"voters"`

	// The membership, moved whole. Roster and slot assignments are the closed
	// roster (D-11) and must be byte-identical across the handoff; the anchor and
	// the barrier delay are what a joiner admitted by the successor adopts.
	Roster            []SessionParticipant `json:"roster"`
	Anchor            event.JoinAnchor     `json:"anchor"`
	BarrierDelayTicks uint64               `json:"barrier_delay_ticks"`

	// EvidenceTick is the newest retained authoritative tick the successor holds.
	// It is the claim requirement 3 makes checkable: the successor's own Shared
	// world is at least as new as the last artifact published under the term it is
	// replacing.
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

// Majority is the smallest strict majority of n participants.
func Majority(n int) int { return n/2 + 1 }

// Validate refuses a handoff record that could not have been produced by the
// succession rule, before any of it is adopted.
func (h HandoffRecord) Validate(roster []SessionParticipant) error {
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
	if !slices.ContainsFunc(h.Roster, func(p SessionParticipant) bool { return p.ID == h.Authority }) {
		return errors.New("handoff names an authority the roster does not carry")
	}
	seen := make(map[PeerID]bool, len(h.Voters))
	votes := 0
	for _, v := range h.Voters {
		if seen[v] {
			continue
		}
		if !slices.ContainsFunc(h.Roster, func(p SessionParticipant) bool { return p.ID == v }) {
			return fmt.Errorf("handoff carries a vote from participant %d, which is not in the roster", v)
		}
		seen[v] = true
		votes++
	}
	if votes < Majority(len(roster)) {
		return fmt.Errorf("handoff carries %d votes, a strict majority of %d is %d",
			votes, len(roster), Majority(len(roster)))
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

// ElectSuccessor is the succession rule, as a function of the closed roster and
// the reports the caller has collected. It decides nothing about timing: the
// caller is what says when its view is complete enough to vote from.
//
// A candidate must be a surviving participant that (a) is directly linked to a
// strict majority of the roster, counting itself, and (b) holds retention as new
// as the newest any reporting survivor does. Among those the lowest participant
// ID wins, which is what makes the answer the roster's rather than whoever
// noticed the loss first.
//
// The second return is false when nothing is eligible, which is a session that
// falls back to local continuation rather than one that waits.
func ElectSuccessor(roster []SessionParticipant, lost PeerID,
	reports map[PeerID]AuthorityReport) (PeerID, bool) {
	if len(roster) == 0 || len(reports) == 0 {
		return 0, false
	}
	inRoster := func(id PeerID) bool {
		return slices.ContainsFunc(roster, func(p SessionParticipant) bool { return p.ID == id })
	}
	need := Majority(len(roster))

	newest := uint64(0)
	for id, r := range reports {
		if id == lost || !inRoster(id) {
			continue
		}
		newest = max(newest, r.RetainedTick)
	}

	best, found := PeerID(0), false
	for id, r := range reports {
		if id == lost || !inRoster(id) || r.Retained == 0 {
			continue
		}
		if r.RetainedTick < newest {
			continue // silently behind: it would publish a world the session has passed
		}
		reach := 1
		seen := map[PeerID]bool{id: true}
		for _, l := range r.Links {
			if l == lost || l == id || seen[l] || !inRoster(l) {
				continue
			}
			seen[l] = true
			reach++
		}
		if reach < need {
			continue
		}
		if !found || id < best {
			best, found = id, true
		}
	}
	return best, found
}

// EncodeAuthorityReport and its siblings are the wire forms. They are separate
// message kinds rather than shapes of one because a receiver acts on each at a
// different moment: a report is information, a vote is a commitment, and a
// handoff is a membership change.
func EncodeAuthorityReport(r AuthorityReport) ([]byte, error) { return json.Marshal(r) }

// DecodeAuthorityReport parses one survivor's succession input.
func DecodeAuthorityReport(b []byte) (AuthorityReport, error) {
	var r AuthorityReport
	err := json.Unmarshal(b, &r)
	return r, err
}

// EncodeAuthorityVote renders one immutable vote.
func EncodeAuthorityVote(v AuthorityVote) ([]byte, error) { return json.Marshal(v) }

// DecodeAuthorityVote parses one vote.
func DecodeAuthorityVote(b []byte) (AuthorityVote, error) {
	var v AuthorityVote
	err := json.Unmarshal(b, &v)
	return v, err
}

// EncodeHandoff renders the record a successor publishes before it authors.
func EncodeHandoff(h HandoffRecord) ([]byte, error) { return json.Marshal(h) }

// DecodeHandoff parses one handoff record.
func DecodeHandoff(b []byte) (HandoffRecord, error) {
	var h HandoffRecord
	err := json.Unmarshal(b, &h)
	return h, err
}

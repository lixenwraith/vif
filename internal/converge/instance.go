package converge

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// Instance is the run this protocol authors or follows. It is the whole of what the
// package needs from the world: every method takes the world lock itself, so none
// may be called by a caller already holding it, and nothing here is called from
// inside a tick.
type Instance interface {
	// Position is the run and tick every schedule and every containment rule is
	// measured against; Driven marks a run whose caller paces its own cadence.
	Position() event.Stamp
	Driven() bool

	// LocalParticipant is this instance's session identity, zero outside a session,
	// and RosterSize how many cursors the closed roster holds.
	LocalParticipant() uint32
	RosterSize() int

	// WorldRoster is the roster as the cursors hold it: the same list on every
	// instance, because arrivals and departures are barrier-bound crossings. Empty
	// before the cursors exist, which is what makes the offer the fallback.
	WorldRoster() []network.RosterEntry

	// Transport is the attached endpoint, nil outside a session; DrainOffTick
	// translates what it holds without advancing a tick.
	Transport() engine.NetworkPort
	DrainOffTick()

	// InstallCapture projects a capture to this instance's own tick — the clock
	// never moves backwards — writes the projection, and reports how far the live
	// world had drifted from it. AdoptAuthority takes a header whose world this
	// instance provably holds already. VerifyCaptureIdentity answers whether a
	// header describes this session without the body a full verification hashes.
	CaptureShared() (snapshot.SharedCapture, error)
	InstallCapture(snapshot.SharedCapture) (engine.WorldDifference, error)
	AdoptAuthority(snapshot.CaptureHeader)
	VerifyCaptureIdentity(snapshot.CaptureHeader) error

	// PlayoutLead is the lead the barrier defers by now, the lead one roster and
	// this instance's links ask for, and the participants past the ceiling. The
	// first is read rather than remembered: a successor inherits a lead it never
	// derived, and re-announcing its own copy would move the session back to it.
	// SetPlayoutLead publishes one; DropParticipant closes a link and says so.
	PlayoutLead(roster []network.RosterEntry) (current, target uint64, overrun []network.PeerID)
	SetPlayoutLead(ticks uint64)
	DropParticipant(id uint32) bool

	// AuthorityChanged moves the membership a handoff carries into the places the
	// run reads it from; DropAbandonedCursors removes the participants an instance
	// left with no link will never hear from again.
	AuthorityChanged(rec network.HandoffRecord, mine bool)
	DropAbandonedCursors(roster []network.RosterEntry, local network.PeerID)

	// SetStatusMessage is the operator surface a refusal or a recovery is said on.
	SetStatusMessage(msg string, duration time.Duration, override bool)
}

// New builds the three halves of the protocol and wires them to each other. One
// constructor rather than three and a setter: they hold references both ways — a
// correction is refused by the term gate, a succession seeds itself from retention
// — so a half-wired graph is not a state this package can be in.
func New(inst Instance, tel snapshot.Telemetry, reg *status.Registry) (*Corrections, *Authority, *Reach) {
	c := newCorrections(inst, tel)
	u := newAuthority(inst, tel, reg)
	r := newReach(inst, reg)
	c.authority = u
	u.corrections, u.reach = c, r
	r.authority = u
	return c, u, r
}

package converge

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// Instance is the run this protocol authors or follows. Every method takes the world
// lock itself except CaptureSharedLocked, which only TickClosed calls from inside a
// tick.
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
	// CaptureSharedLocked is the read without its seal, for TickClosed, which runs
	// under the world lock; SealCapture pins and hashes it outside the lock.
	CaptureSharedLocked() (snapshot.SharedCapture, error)
	SealCapture(*snapshot.SharedCapture) error
	InstallCapture(snapshot.SharedCapture) (engine.WorldDifference, error)
	AdoptAuthority(snapshot.CaptureHeader)
	VerifyCaptureIdentity(snapshot.CaptureHeader) error

	// DropParticipant closes one participant's link; the departure that follows is
	// the ordinary one. CommitLate is how many of a participant's crossings reached
	// this instance late while it authored.
	DropParticipant(id uint32) bool
	CommitLate(id uint32) uint64

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

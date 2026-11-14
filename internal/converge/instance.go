package converge

import (
	"time"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/status"
	link "github.com/lixenwraith/vif/pkg/network"
)

// Instance is the run this protocol authors or follows. Every method takes the world
// lock itself except CaptureSharedLocked, which only TickClosed calls from inside a
// tick.
type Instance interface {
	// Position is the run and tick every schedule and every containment rule is
	// measured against; Driven marks a run whose caller paces its own cadence.
	Position() event.Stamp
	Driven() bool
	// Held reports a hold standing on this run, which reaches no tick until it ends;
	// it takes no lock. Rate is the rate its clock runs at and TickInterval the game
	// time a tick advances, both the host's in a session.
	Held() bool
	Rate() engine.TimeScale
	TickInterval() time.Duration

	// LocalParticipant is this instance's session identity, zero outside a session,
	// and LocalKey the participant key it holds its place under, which signs its peer
	// links and succession messages; an empty key before it is in one. Seated marks
	// a bot's run, which shares its holder's fate: the run's own word, since its
	// world holds no entry for it until its arrival crosses there.
	LocalParticipant() uint32
	LocalKey() network.Key
	Seated() bool
	// TakesOver reports whether this run's player lets a session move to it, asked
	// as its turn on the list comes: one who turned that off since the join passes it.
	TakesOver() bool
	// Superseding reports a run still waiting for its own arrival in the place it
	// took back, which the authority it came back through scheduled and no other holds.
	Superseding() bool
	// KeepOut is the keys and accounts the session keeps out, as its doors hold
	// them, which the record this instance authors carries to its successors.
	KeepOut() []string

	// WorldRoster is the roster as the cursors hold it: the same list on every
	// instance, because arrivals and departures are barrier-bound crossings. Empty
	// before the cursors exist, which is what makes the offer the fallback.
	WorldRoster() []network.RosterEntry

	// Transport is the attached endpoint, nil outside a session; DrainOffTick
	// translates what it holds without advancing a tick.
	Transport() engine.NetworkPort
	// Downstream lists the linked participants behind this instance: the ones it
	// forwards the authority's artifacts to and answers for.
	Downstream() []uint32
	DrainOffTick()

	// Reaches reports a path to the authority: a link to it, or a relay that carries
	// this instance. Carries reports a link whose participant this instance carries:
	// one that joined through it, or any once its player turned relaying on. Carry
	// marks one behind it, which the authority's artifacts then flow down to.
	Reaches() bool
	Carries(id uint32) bool
	Carry(id uint32)

	// InstallCapture projects a capture proved by its integrity hash or the
	// authority's root to this instance's tick, writes it, and reports the drift.
	// AdoptAuthority takes a header whose world this instance provably holds;
	// VerifyCaptureIdentity answers whether a header describes this session; Encoder
	// is the capture encodings every seal, index and body here shares.
	CaptureShared() (snapshot.SharedCapture, error)
	// CaptureSharedLocked is the read without its seal, for TickClosed, which runs
	// under the world lock; a comparison needs no integrity hash, only the index.
	// IndexSharedLocked is the index alone, built there off the live rows against
	// the authority and term a manifest names, with the capture's every part but
	// those rows: what a root that agrees needs, and a root that differs does not.
	CaptureSharedLocked() (snapshot.SharedCapture, error)
	IndexSharedLocked(authority uint32, term network.AuthorityTerm) (*snapshot.Manifest, snapshot.SharedCapture, error)
	InstallCapture(snapshot.SharedCapture) (engine.WorldDifference, error)
	AdoptAuthority(snapshot.CaptureHeader)
	VerifyCaptureIdentity(snapshot.CaptureHeader) error
	Encoder() *snapshot.Encoder

	// DropParticipant closes one participant's link; the departure that follows is
	// the ordinary one. CommitLate is how many of a participant's crossings reached
	// this instance late while it authored, and Heard its news of one then: each
	// epoch of its admitted and each roster change of its crossed.
	DropParticipant(id uint32) bool
	CommitLate(id uint32) uint64
	Heard(id uint32) uint64
	// ShedParticipant dismisses one participant, with its bots, for the load it
	// costs, which every participant and the dropped one are told.
	ShedParticipant(id uint32) bool

	// Ingress is how many frame bytes this instance has drained from a link.
	Ingress(id uint32) uint64

	// AuthorityChanged moves the membership a handoff carries into the places the
	// run reads it from, and via is the link it follows the new host through, zero
	// for its own; CrossDepartures keeps as Wraiths the places a new host's group
	// lost and those of persons it no longer hears; Forked says no handoff came,
	// so the run goes on alone from the last authoritative state and roster.
	AuthorityChanged(rec network.HandoffRecord, mine bool, via link.PeerID)
	CrossDepartures(away []network.RosterEntry)
	// CrossReturns gives back the places of participants the floor sent away whose
	// membership reports reached the host late, each as a key's return is crossed.
	CrossReturns(back []network.RosterEntry)
	Forked(roster []network.RosterEntry, local link.PeerID)

	// Redial starts this run's redial of the authority it lost, at the door it came
	// in by with the key its place waits under, and reports whether one started.
	// It dials while Authority.Redialing, replaces the run once RedialAdmitted, and
	// reports a door that turned it away through RedialRefused.
	Redial(lost link.PeerID) bool

	// Freeze stops the run while its authority moves; Thaw is the successor's end of
	// that window, which leaves standing a hold the lost host set.
	Freeze()
	Thaw()

	// SetStatusMessage is the operator surface a refusal or a recovery is said on.
	SetStatusMessage(msg string, kind parameter.StatusKind)
}

// New builds the three halves of the protocol and wires them to each other. One
// constructor rather than three and a setter: they hold references both ways — a
// correction is refused by the term gate, a succession seeds itself from retention
// — so a half-wired graph is not a state this package can be in.
func New(inst Instance, tel snapshot.Telemetry, reg *status.Registry) (*Corrections, *Authority, *Reach) {
	c := newCorrections(inst, tel)
	u := newAuthority(inst, tel, reg)
	r := newReach(inst, reg)
	c.log, u.log, r.log = reg.Log(), reg.Log(), reg.Log()
	c.authority = u
	u.corrections, u.reach = c, r
	r.authority = u
	return c, u, r
}

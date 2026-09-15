package app

import (
	"errors"
	"slices"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/converge"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// instance is the App as the authority protocol sees it: the world a correction is
// read from and written to, and nothing else. Every method takes the world lock
// itself, which is what keeps converge.Instance's contract — "never called while
// the caller holds it" — a property of this file rather than of every call site.
type instance struct{ a *App }

func (i instance) Position() event.Stamp { return i.a.Position() }
func (i instance) Driven() bool          { return i.a.cfg.Mode.Driven() }

func (i instance) LocalParticipant() uint32 { return i.a.localParticipant() }

func (i instance) RosterSize() (n int) {
	i.a.world.RunSafe(func() { n = i.a.world.Resources.Player.Count() })
	return n
}

// WorldRoster reads the roster off the cursors rather than off the lobby the offer
// described: arrivals and departures are barrier-bound crossings, so this is the
// same list on every instance.
func (i instance) WorldRoster() []network.SessionParticipant {
	var out []network.SessionParticipant
	i.a.world.RunSafe(func() {
		w := i.a.world
		for slot := range parameter.MaxPlayers {
			e := w.Resources.Player.Slot(uint8(slot))
			if e == 0 {
				continue
			}
			c, ok := w.Components.Cursor.GetComponent(e)
			if !ok || c.PeerID == 0 {
				continue
			}
			out = append(out, network.SessionParticipant{
				ID: network.PeerID(c.PeerID), Slot: uint8(slot),
			})
		}
	})
	slices.SortFunc(out, func(a, b network.SessionParticipant) int { return int(a.ID) - int(b.ID) })
	return out
}

func (i instance) Transport() engine.NetworkPort { return i.a.sessionTransport() }

// DrainOffTick translates whatever the endpoint holds without advancing a tick, so
// a manifest, a request or a repair is acted on the moment it lands.
func (i instance) DrainOffTick() {
	i.a.world.RunSafe(func() {
		for _, sys := range i.a.world.Systems() {
			if d, ok := sys.(interface{ DrainOffTick() }); ok {
				d.DrainOffTick()
				return
			}
		}
	})
}

func (i instance) CaptureShared() (snapshot.SharedCapture, error) { return i.a.CaptureShared() }

// InstallCapture resolves a capture against the staging world and commits it
// between two ticks, reporting how far this instance had drifted.
func (i instance) InstallCapture(cap snapshot.SharedCapture) (engine.WorldDifference, error) {
	staged, err := i.a.StageShared(cap)
	if err != nil {
		return engine.WorldDifference{}, err
	}
	if err := staged.Commit(); err != nil {
		return engine.WorldDifference{}, err
	}
	return staged.Difference(), nil
}

func (i instance) VerifyCaptureIdentity(h snapshot.CaptureHeader) error {
	return i.a.verifyCaptureIdentity(h)
}

func (i instance) ReplayLocalSuffix(h snapshot.CaptureHeader) { i.a.replayLocalSuffix(h) }

func (i instance) AuthorityChanged(rec network.HandoffRecord, mine bool) {
	i.a.applyAuthorityChange(rec, mine)
}

func (i instance) DropAbandonedCursors(roster []network.SessionParticipant, local network.PeerID) {
	i.a.dropAbandonedCursors(roster, local)
}

func (i instance) SetStatusMessage(msg string, duration time.Duration, override bool) {
	i.a.ctx.SetStatusMessage(msg, duration, override)
}

// === the run's side ===

// receiveCorrection queues one reassembled correction. It is the seam every
// transport binds to, so a correction reaches the same queue however this run came
// to be in a session. Caller holds the world lock: take the bytes and nothing else.
func (a *App) receiveCorrection(_ uint64, body []byte) {
	if a.corrections != nil {
		a.corrections.Receive(body)
	}
}

// receiveSelective queues one manifest, request or repair. Same rule as
// receiveCorrection: the caller holds the world lock, so this takes the bytes and
// decides nothing.
func (a *App) receiveSelective(kind uint8, from uint32, body []byte) {
	if a.corrections != nil {
		a.corrections.ReceiveSelective(kind, from, body)
	}
}

// receiveAuthorityFrame queues one succession frame, under the same rule: the
// decode and the decision happen between two ticks, in the correction loop's drain.
func (a *App) receiveAuthorityFrame(kind uint8, from uint32, body []byte) {
	if a.corrections != nil {
		a.corrections.ReceiveAuthorityFrame(kind, from, body)
	}
}

// reportPeerLost hands a departure to the succession. Caller holds the world lock.
func (a *App) reportPeerLost(id uint32) {
	if a.corrections != nil {
		a.corrections.QueuePeerLost(id)
	}
}

// ApplyPendingCorrections installs whatever authority has arrived, between two
// ticks. A caller-driven run reaches it from Tick; an interactive one has a
// goroutine of its own, because nothing on its side calls Tick.
func (a *App) ApplyPendingCorrections() {
	if a.corrections == nil {
		return
	}
	a.corrections.Apply()
}

// PublishCorrection takes one authoritative capture and broadcasts it, for a driven
// run that paces its own cadence. An interactive host has the pump instead, and
// calling this retires it: a run with two things deciding when a correction leaves
// hands its guests a world newer than the one its driver was describing.
func (a *App) PublishCorrection() error {
	if a.corrections == nil {
		return errors.New("this run is not in a session")
	}
	return a.corrections.Publish()
}

// adoptCorrectionBaseline records the capture a join installed, so the deltas that
// follow it have the keyframe they name. It is the same object from both ends: the
// host sent a keyframe and this is the instance that installed it.
func (a *App) adoptCorrectionBaseline(cap snapshot.SharedCapture) {
	if a.corrections != nil {
		a.corrections.SetBaseline(cap)
	}
}

// CadenceReport is the session's operating point, for `:session` and the criteria.
func (a *App) CadenceReport() converge.CadenceReport {
	if a.corrections == nil {
		return converge.CadenceReport{}
	}
	return a.corrections.Cadence()
}

// SelectiveReport describes the selective exchange.
func (a *App) SelectiveReport() converge.SelectiveReport {
	if a.corrections == nil {
		return converge.SelectiveReport{PeerState: map[uint32]converge.PeerSelective{}}
	}
	return a.corrections.Selective()
}

// AuthorityState describes this instance's place in the session's authority.
func (a *App) AuthorityState() converge.AuthorityReport {
	if a.authority == nil {
		return converge.AuthorityReport{}
	}
	return a.authority.State()
}

// cadenceSizes is what a correction currently costs on this world.
func (a *App) cadenceSizes() linkpace.Sizes {
	if a.corrections == nil {
		return linkpace.Sizes{}
	}
	return a.corrections.Sizes()
}

// correctionMagnitude reports the last correction's size, for a caller that wants
// the number without reading the registry.
func (a *App) correctionMagnitude() engine.WorldDifference {
	m := a.telemetry
	return engine.WorldDifference{
		Entries:   int(m.CorrectionEntries.Load()),
		Entities:  int(m.CorrectionEntities.Load()),
		CellShift: int(m.CorrectionCells.Load()),
	}
}

// === authority ===

// authorityStamp is the term and participant a capture read now is authoritative
// under. Zero on a solo run, which is what makes a capture saved from one carry no
// authority claim at all.
func (a *App) authorityStamp() (network.AuthorityTerm, uint32) {
	if a.authority == nil {
		return 0, 0
	}
	return a.authority.Term(), uint32(a.authority.Holder())
}

// authorityTerm is the generation this instance is part of.
func (a *App) authorityTerm() network.AuthorityTerm {
	if a.authority == nil {
		return 0
	}
	return a.authority.Term()
}

// authoring reports whether this instance is the one publishing the world.
func (a *App) authoring() bool {
	return a.authority != nil && a.authority.IsAuthority()
}

// authorityID is the participant currently authoring, which every admission
// artifact names. It falls back to the session's first identity so a run that has
// not opened a session yet still offers a valid one.
func (a *App) authorityID() network.PeerID {
	if a.authority == nil {
		return hostParticipantID
	}
	if id := a.authority.Holder(); id != 0 {
		return id
	}
	return hostParticipantID
}

// admitArtifactTerm is the wire gate as the correction path calls it.
func (a *App) admitArtifactTerm(term network.AuthorityTerm, from uint32) bool {
	if a.authority == nil {
		return true
	}
	return a.authority.Admit(term, from)
}

// openAuthority records the term and membership this run enters a session under.
func (a *App) openAuthority(o network.SessionOffer, local network.PeerID) {
	if a.authority == nil {
		return
	}
	a.authority.Open(o, local)
	a.publishAuthorityResource()
}

// openAuthorityLocked is openAuthority for a caller that already holds the world
// lock, which the operator `:host` path does.
// Caller MUST hold updateMutex.
func (a *App) openAuthorityLocked(o network.SessionOffer, local network.PeerID) {
	if a.authority == nil {
		return
	}
	a.authority.Open(o, local)
	a.publishAuthorityResourceLocked()
}

// applyAuthorityChange moves the membership a handoff carries into the places the
// session reads it from, and switches this instance's role. Nothing is re-derived:
// roster, slots, anchor and barrier delay are adopted exactly as carried, which is
// what makes them byte-identical on every survivor.
func (a *App) applyAuthorityChange(rec network.HandoffRecord, mine bool) {
	a.sessionMu.Lock()
	a.sessionRoster = slices.Clone(rec.Roster)
	a.sessionOffer.Host = rec.Authority
	a.sessionOffer.Term = rec.Term
	a.sessionOffer.Anchor = rec.Anchor
	a.sessionOffer.BarrierDelayTicks = rec.BarrierDelayTicks
	a.sessionOffer.Participants = slices.Clone(rec.Roster)
	a.sessionMu.Unlock()

	a.publishAuthorityResource()
	if a.corrections == nil {
		return
	}
	if mine {
		a.corrections.BecomeAuthority(rec)
		a.crossPredecessorDeparture(rec)
		return
	}
	a.corrections.FollowAuthority(rec)
}

// publishAuthorityResource hands the transport the two cells the barrier reads:
// which participant may produce a roster crossing, and under which generation.
func (a *App) publishAuthorityResource() {
	a.world.RunSafe(a.publishAuthorityResourceLocked)
}

// publishAuthorityResourceLocked is the same write for a caller that holds the
// world lock. Caller MUST hold updateMutex.
func (a *App) publishAuthorityResourceLocked() {
	term, holder := a.authorityStamp()
	if r := a.world.Resources.Network; r != nil {
		r.Authority.Store(holder)
		r.Term.Store(uint64(term))
	}
}

// sessionChain is what an offer carries, read through the authority because a
// guest holds one too.
func (a *App) sessionChain() network.SuccessionChain {
	if a.authority == nil {
		return nil
	}
	return a.authority.Chain()
}

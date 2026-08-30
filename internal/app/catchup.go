// Package app: mid-run join.
//
// Nothing transports world state, so a participant arriving after tick zero cannot
// be handed the session — it has to reproduce it. That is what the journal already
// does for replay: a run is a pure function of its anchor and its non-system record
// stream, so replaying that stream to the host's position reconstructs the host's
// world rather than approximating it. A host therefore retains its records for the
// life of the session and a late joiner replays them at full speed.
//
// Two things make the handoff exact. The record stream is authoritative up to the
// tick it ends on, so a barrier artifact already covered by it must be discarded
// rather than applied twice. And the arrival itself is a crossing, so every instance
// — the joiner included — spawns the new cursor at one agreed tick, the same way a
// departure removes one.
//
// The cost this pays is memory: the log is complete from tick zero because replay is,
// and it grows with session length. A world snapshot is what would bound it, and is
// the same thing that would let a participant resume rather than reproduce.
package app

import (
	"errors"
	"fmt"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/system"
)

// SessionLog returns the records a late joiner must replay to reach this instance's
// position, and the position they reach. Empty on an instance that is not hosting.
func (a *App) SessionLog() ([]event.JournalRecord, event.Stamp) {
	if a.sessionLog == nil {
		return nil, a.Position()
	}
	// Read under the world lock so the tick cannot advance between the records and
	// the position they reach: a log that runs past the position it is paired with
	// would tick a replaying joiner beyond the session it is joining.
	var records []event.JournalRecord
	var at event.Stamp
	a.world.RunSafe(func() {
		records = a.sessionLog.Records()
		at = a.Position()
	})
	return records, at
}

// CatchUp reproduces a session already in progress. The anchor supplies the identity
// every participant must share (D-11) and the D-14 map latch; the records supply
// everything the run has done since. On return this instance stands at the anchor's
// position and may take part in the barrier like any other participant.
//
// Call after construction from the same anchor and before the first live tick.
func (a *App) CatchUp(j event.JoinAnchor, records []event.JournalRecord) error {
	an := j.Anchor
	if err := firstAnchorMismatch("join", a.anchorIdentity(an)); err != nil {
		return err
	}
	if an.MapWidth <= 0 || an.MapHeight <= 0 {
		return fmt.Errorf("join: anchor carries no map latch (%dx%d)", an.MapWidth, an.MapHeight)
	}
	if st := a.Position(); st.Run != 0 || st.Tick != 0 {
		return fmt.Errorf("catch up: this instance is at run %d tick %d, not its start", st.Run, st.Tick)
	}
	// The latch is not adopted up front: the record stream carries the level setup
	// that produced it, and applying that twice re-runs an event the host ran once.
	// It is adopted afterwards only if the replay did not arrive at it, which is the
	// case for a session whose bounds were never journaled.
	driver, err := NewReplayDriver(a, records)
	if err != nil {
		return fmt.Errorf("catch up: %w", err)
	}
	if err := driver.RunAll(); err != nil {
		return fmt.Errorf("catch up: %w", err)
	}
	// The stream ends at its last record, which need not be the host's newest tick:
	// a quiet stretch journals nothing. Ticking closes that gap.
	for a.Position().Tick < an.Tick {
		a.Tick(1)
	}
	if st := a.Position(); st.Run != an.Run || st.Tick != an.Tick {
		return fmt.Errorf("catch up: reached run %d tick %d, host is at run %d tick %d",
			st.Run, st.Tick, an.Run, an.Tick)
	}
	// The latch is adopted only if the replay did not arrive at it, which is the case
	// for a session whose bounds were never journaled. Adopting it as well would run
	// a level setup the session ran once, and the record stream already carried.
	if !a.mapLatched(an) {
		a.adoptMapLatch(an)
	}

	// Artifacts the log already carries must not land a second time. A live epoch can
	// arrive while this instance is still replaying, and everything it covers up to
	// the anchor tick is in the records just applied.
	a.discardCaughtUpArtifacts(an.Tick)
	return nil
}

// discardCaughtUpArtifacts drops barrier artifacts the replayed log already applied.
func (a *App) discardCaughtUpArtifacts(through uint64) {
	a.world.RunSafe(func() {
		for _, sys := range a.world.Systems() {
			if net, ok := sys.(*system.NetworkSystem); ok {
				net.DiscardArtifactsThrough(through)
			}
		}
	})
}

// AdmitParticipant crosses one participant's arrival, so every instance spawns its
// cursor at the same tick. Only the coordinator produces it, for the same reason it
// is the only producer of a departure: one producer is what gives the roster change a
// single apply tick.
func (a *App) AdmitParticipant(p network.SessionParticipant) error {
	if p.ID == 0 || int(p.ID) > parameter.MaxPlayers || int(p.Slot) >= parameter.MaxPlayers {
		return fmt.Errorf("admit: participant %d slot %d is outside the roster", p.ID, p.Slot)
	}
	var occupied core.Entity
	a.world.RunSafe(func() { occupied = a.world.Resources.Player.Slot(p.Slot) })
	if occupied != 0 {
		return fmt.Errorf("admit: slot %d already holds cursor %d", p.Slot, occupied)
	}
	a.ctx.PushEventFull(event.EventParticipantJoined,
		&event.ParticipantJoinedPayload{Participant: uint32(p.ID), Slot: p.Slot},
		event.OriginSession, core.DomainPlayer)
	return nil
}

// ErrCatchUpUnavailable reports a mid-run join against an instance keeping no log.
var ErrCatchUpUnavailable = errors.New("join: this session retains no replayable log")

// SessionLogChunks is the coordinator's log accessor: the retained records, split
// into frames the transport can carry. Paired with the offer's anchor, which names
// the position they reach.
func (a *App) SessionLogChunks() ([][]byte, error) {
	if a.sessionLog == nil {
		return nil, ErrCatchUpUnavailable
	}
	records, _ := a.SessionLog()
	return event.EncodeSessionLog(records, network.MaxPayloadSize-network.HeaderSize)
}

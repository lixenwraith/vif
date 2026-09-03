// Package app: join handshake.
//
// A second participant reproduces a session rather than receiving it: same seed,
// same session counter, same config and corpus, so shared entity identity and
// creation order are identical from the first tick (D-11). Only the D-14 map latch
// is adopted rather than re-derived — the joiner's terminal must not decide the
// shared bounds. The carrier is JournalAnchor, which already describes exactly this
// set for replay; the join adds the latch and drops the recording terminal.
package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// ErrJoinMidRun is returned when the two participants are not at the same position.
// Nothing carries world state yet, so a joiner can only reproduce a session from
// its start; a later join needs a world snapshot the current transport does not carry.
var ErrJoinMidRun = errors.New("join: participants are not at the same position")

// JoinAnchor describes this session to a participant that wants to share it: the
// identity VerifyAnchor already checks, plus the live D-14 map latch.
// The terminal fields describe this instance and the joiner ignores them.
func (a *App) JoinAnchor() event.JoinAnchor {
	var out event.JoinAnchor
	a.world.RunSafe(func() { out = a.joinAnchorLocked() })
	return out
}

// joinAnchorLocked is JoinAnchor for a caller that already holds the world lock —
// the operator `:host` path does, because mode/ runs inside it.
// Caller MUST hold updateMutex.
func (a *App) joinAnchorLocked() event.JoinAnchor {
	an := a.buildAnchor()
	an.Schema = event.JournalSchema
	cfg := a.world.Resources.Config
	an.MapWidth, an.MapHeight, an.CropOnResize = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
	st := a.world.Resources.Event.Queue.Stamp()
	an.Run, an.Tick = st.Run, st.Tick
	return event.JoinAnchor{Anchor: an}
}

// Join admits this App into the session an anchor describes: it verifies that this
// instance reproduces the identity, refuses a position it cannot reconstruct, and
// adopts the map latch. Call after NewHeadless, before the first Tick.
//
// Reproducing the position is what a capture removes the need for; JoinAt is the
// same admission for a participant that receives the world instead of re-deriving it.
func (a *App) Join(j event.JoinAnchor) error { return a.join(j, false) }

// JoinAt admits this App into a session at whatever tick the host has reached. The
// caller installs the capture; this only checks that the two instances are the same
// simulation and adopts the shared bounds.
func (a *App) JoinAt(j event.JoinAnchor) error { return a.join(j, true) }

func (a *App) join(j event.JoinAnchor, midRun bool) error {
	an := j.Anchor
	if err := firstAnchorMismatch("join", a.anchorIdentity(an)); err != nil {
		return err
	}
	if an.MapWidth <= 0 || an.MapHeight <= 0 {
		return fmt.Errorf("join: anchor carries no map latch (%dx%d)", an.MapWidth, an.MapHeight)
	}
	if st := a.Position(); !midRun && (st.Run != an.Run || st.Tick != an.Tick) {
		return fmt.Errorf("%w: host at run %d tick %d, this instance at run %d tick %d",
			ErrJoinMidRun, an.Run, an.Tick, st.Run, st.Tick)
	}
	a.adoptMapLatch(an)
	return nil
}

// JoinSession verifies a coordinator offer and adopts its map and roster. It is the
// tick-zero form, kept for a harness that builds both worlds itself; the session
// path takes JoinSessionAt.
func (a *App) JoinSession(o network.SessionOffer) error {
	if err := a.validateSessionOffer(o, o.Assigned); err != nil {
		return err
	}
	if err := a.Join(o.Anchor); err != nil {
		return err
	}
	a.openAuthority(o, o.Assigned)
	return a.configureSessionRoster(o, o.Assigned)
}

// JoinSessionAt admits this instance into a running session by installing the
// host's world rather than reproducing it.
//
// The order is the whole of the join. The map latch first, because the bounds
// decide what the level setup reflows and a capture's placements are relative to
// them. Then the FSM boot's queued spawn is settled — not because the entities it
// creates are wanted, but because it is what declares the cursor template a late
// arrival is armed from, and because leaving it queued would spawn a second cursor
// into slot zero after the install. Then the capture is staged into a second world
// and swapped in, which is where this instance stops being its own session and
// becomes part of the host's. The roster is configured last, on the installed
// world: every cursor the offer names is already there, so what is left is which of
// them this participant drives (D-13).
func (a *App) JoinSessionAt(o network.SessionOffer, cap snapshot.SharedCapture) error {
	if err := a.validateSessionOffer(o, o.Assigned); err != nil {
		return err
	}
	if err := a.JoinAt(o.Anchor); err != nil {
		return err
	}
	a.openAuthority(o, o.Assigned)
	a.scheduler.Settle()

	staged, err := a.StageShared(cap)
	if err != nil {
		return fmt.Errorf("join capture: %w", err)
	}
	if err := staged.Commit(); err != nil {
		return err
	}
	stage, commit := staged.Timings()
	vlog.Info("app", "msg", "join installed the session world",
		"tick", cap.Header.Tick, "run", cap.Header.Run,
		"stage_ms", stage.Milliseconds(), "commit_ms", commit.Milliseconds())

	// The world a join installs is the host's current keyframe, which is what the
	// deltas that follow it are computed against. Adopting it here is what lets a
	// participant start applying corrections at the next cadence rather than at the
	// next keyframe.
	a.adoptCorrectionBaseline(cap)

	return a.bindSessionControl(o, o.Assigned)
}

// HostSession applies the same map and roster sequence after a guest accepts.
func (a *App) HostSession(o network.SessionOffer) error {
	if err := a.validateSessionOffer(o, o.Host); err != nil {
		return err
	}
	a.adoptMapLatch(o.Anchor.Anchor)
	a.openAuthority(o, o.Host)
	return a.configureSessionRoster(o, o.Host)
}

func (a *App) validateSessionOffer(o network.SessionOffer, local network.PeerID) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if len(o.Participants) > parameter.MaxPlayers {
		return fmt.Errorf("join roster has %d participants, maximum is %d", len(o.Participants), parameter.MaxPlayers)
	}
	for _, p := range o.Participants {
		if int(p.Slot) >= parameter.MaxPlayers || int(p.ID) > parameter.MaxPlayers {
			return fmt.Errorf("join roster assignment id %d slot %d exceeds maximum %d", p.ID, p.Slot, parameter.MaxPlayers)
		}
	}
	if _, ok := o.Participant(local); !ok {
		return fmt.Errorf("join roster omits local participant %d", local)
	}
	return nil
}

// configureSessionRoster creates slots in coordinator order, then applies local control.
func (a *App) configureSessionRoster(o network.SessionOffer, local network.PeerID) error {
	participants := slices.Clone(o.Participants)
	slices.SortFunc(participants, func(x, y network.SessionParticipant) int { return int(x.Slot) - int(y.Slot) })

	// The boot script's cursor spawn may still be queued: the FSM enters its boot
	// state inside New and nothing has ticked yet. Settling it is what publishes the
	// heat and energy template every rostered cursor is then created and armed from.
	a.scheduler.Settle()

	var initialHeat, initialEnergy int
	a.world.RunSafe(func() { initialHeat, initialEnergy = a.world.Resources.Player.InitialResources() })

	for _, p := range participants {
		var exists bool
		a.world.RunSafe(func() { exists = a.world.Resources.Player.Slot(p.Slot) != 0 })
		if exists {
			continue
		}
		a.ctx.PushEventOrigin(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{
			Slot: p.Slot, Center: true, Control: uint8(component.ControlRemote), PeerID: uint32(p.ID),
			Heat: initialHeat, Energy: initialEnergy,
		}, event.OriginDebug)
		a.scheduler.Settle()
	}

	var count int
	a.world.RunSafe(func() { count = a.world.Resources.Player.Count() })
	if count != len(participants) {
		return fmt.Errorf("join roster created %d cursors, want %d", count, len(participants))
	}
	if err := a.bindSessionControl(o, local); err != nil {
		return err
	}
	a.ctx.PushLocal(event.EventCursorArmRequest,
		&event.CursorArmRequestPayload{Heat: initialHeat, Energy: initialEnergy})
	a.scheduler.Settle()
	a.world.RunSafe(a.ctx.PublishMapLock)
	return nil
}

// bindSessionControl applies the D-13 control assignment for this instance over a
// roster that already exists: which participant owns each cursor, and which of them
// this one drives. It creates nothing.
//
// It is the whole of a mid-run join's roster work. The cursors the offer names came
// from the capture, and the one this participant is about to take does not exist on
// any instance yet — it arrives as the EventParticipantJoined crossing, at one
// agreed tick, which is the only way a shared entity may be created after tick zero
// (D-11). A slot the offer names and the world does not hold is therefore normal
// here rather than an error.
func (a *App) bindSessionControl(o network.SessionOffer, local network.PeerID) error {
	a.world.RunSafe(func() {
		roster := a.world.Resources.Player
		for _, p := range o.Participants {
			e := roster.Slot(p.Slot)
			if c, ok := a.world.Components.Cursor.GetPtr(e); ok {
				c.PeerID = uint32(p.ID)
				c.Control = component.ControlRemote
				if p.ID == local {
					c.Control = component.ControlHuman
				}
			}
		}
	})
	localAssignment, ok := o.Participant(local)
	if !ok {
		return fmt.Errorf("join roster omits local participant %d", local)
	}
	var owned bool
	a.world.RunSafe(func() { owned = a.world.Resources.Player.Slot(localAssignment.Slot) != 0 })
	if owned && localAssignment.Slot != a.localSlot() {
		a.ctx.PushEventOrigin(event.EventCursorSetLocalRequest,
			&event.CursorSetLocalPayload{Slot: localAssignment.Slot}, event.OriginDebug)
		a.scheduler.Settle()
	}
	a.world.RunSafe(a.ctx.PublishMapLock)
	return nil
}

// localSlot returns the roster slot this instance's input follows.
func (a *App) localSlot() uint8 {
	var slot uint8
	a.world.RunSafe(func() { slot = a.world.Resources.Player.LocalSlot() })
	return slot
}

// adoptMapLatch applies the host's bounds through the D-14 authority — the level
// setup path — rather than by writing Config, so the grid, the camera and every
// cursor reflow exactly as they would for a map script. Entities are kept: the
// joiner's world is the same seed's world, not a fresh one.
//
// A joining run now installs the latch before the FSM boots (Config.MapWidth), so
// this is the confirmation rather than the adoption; it still runs the level setup
// unconditionally, because that event is part of the session's record stream and a
// participant reproducing the session by replay has to see the same one.
func (a *App) adoptMapLatch(an event.JournalAnchor) {
	a.SetupLevel(an.MapWidth, an.MapHeight, false, an.CropOnResize)
}

// AttachTransport binds a transport to this App, for a harness or an embedder that
// builds its own endpoint instead of taking the one NetworkService contributes.
// NetworkSystem reads the port per tick, so this needs no re-registration.
func (a *App) AttachTransport(port engine.NetworkPort) {
	a.world.RunSafe(func() { a.attachTransportLocked(port) })
}

// attachTransportLocked is AttachTransport for a caller that already holds the
// world lock — the operator command path does, because mode/ runs inside it.
// Caller MUST hold updateMutex.
func (a *App) attachTransportLocked(port engine.NetworkPort) {
	r := engine.NewNetworkResource(port)
	r.OnDeparture = a.releaseParticipant32
	r.SharedDigest = a.sharedDigestLocked
	// The correction queue takes bytes and nothing else: this runs inside a tick,
	// and decoding or installing a correction here would do both under the lock the
	// install itself needs.
	r.OnCorrection = a.receiveCorrection
	r.OnSelective = a.receiveSelective
	r.OnAuthority = a.receiveAuthorityFrame
	r.OnPeerLost = a.reportPeerLost
	term, holder := a.authorityStamp()
	if holder != 0 {
		r.Authority.Store(holder)
		r.Term.Store(uint64(term))
	}
	a.world.Resources.Network = r
	a.world.MarkSessionShared()
	a.ctx.PublishMapLock()
}

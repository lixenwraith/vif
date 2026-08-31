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
)

// ErrJoinMidRun is returned when the two participants are not at the same position.
// Nothing carries world state yet, so a joiner can only reproduce a session from
// its start; a later join needs a world snapshot the current transport does not carry.
var ErrJoinMidRun = errors.New("join: participants are not at the same position")

// JoinAnchor describes this session to a participant that wants to share it: the
// identity VerifyAnchor already checks, plus the live D-14 map latch.
// The terminal fields describe this instance and the joiner ignores them.
func (a *App) JoinAnchor() event.JoinAnchor {
	an := a.buildAnchor()
	an.Schema = event.JournalSchema
	a.world.RunSafe(func() {
		cfg := a.world.Resources.Config
		an.MapWidth, an.MapHeight, an.CropOnResize = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
	})
	st := a.Position()
	an.Run, an.Tick = st.Run, st.Tick
	return event.JoinAnchor{Anchor: an}
}

// Join admits this App into the session an anchor describes: it verifies that this
// instance reproduces the identity, refuses a position it cannot reconstruct, and
// adopts the map latch. Call after NewHeadless, before the first Tick.
func (a *App) Join(j event.JoinAnchor) error {
	an := j.Anchor
	if err := firstAnchorMismatch("join", a.anchorIdentity(an)); err != nil {
		return err
	}
	if an.MapWidth <= 0 || an.MapHeight <= 0 {
		return fmt.Errorf("join: anchor carries no map latch (%dx%d)", an.MapWidth, an.MapHeight)
	}
	if st := a.Position(); st.Run != an.Run || st.Tick != an.Tick {
		return fmt.Errorf("%w: host at run %d tick %d, this instance at run %d tick %d",
			ErrJoinMidRun, an.Run, an.Tick, st.Run, st.Tick)
	}
	a.adoptMapLatch(an)
	return nil
}

// JoinSession verifies a coordinator offer and adopts its map and roster.
func (a *App) JoinSession(o network.SessionOffer) error {
	if err := a.validateSessionOffer(o, o.Assigned); err != nil {
		return err
	}
	if err := a.Join(o.Anchor); err != nil {
		return err
	}
	return a.configureSessionRoster(o, o.Assigned)
}

// HostSession applies the same map and roster sequence after a guest accepts.
func (a *App) HostSession(o network.SessionOffer) error {
	if err := a.validateSessionOffer(o, o.Host); err != nil {
		return err
	}
	a.adoptMapLatch(o.Anchor.Anchor)
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

	localAssignment, _ := o.Participant(local)
	var count int
	a.world.RunSafe(func() {
		roster := a.world.Resources.Player
		count = roster.Count()
		for _, p := range participants {
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
	if count != len(participants) {
		return fmt.Errorf("join roster created %d cursors, want %d", count, len(participants))
	}
	if localAssignment.Slot != 0 {
		a.ctx.PushEventOrigin(event.EventCursorSetLocalRequest,
			&event.CursorSetLocalPayload{Slot: localAssignment.Slot}, event.OriginDebug)
		a.scheduler.Settle()
	}
	a.ctx.PushLocal(event.EventCursorArmRequest,
		&event.CursorArmRequestPayload{Heat: initialHeat, Energy: initialEnergy})
	a.scheduler.Settle()
	a.world.RunSafe(a.ctx.PublishMapLock)
	return nil
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
	a.world.RunSafe(func() {
		r := engine.NewNetworkResource(port)
		r.OnDeparture = a.releaseParticipant32
		r.SharedDigest = a.sharedDigestLocked
		a.world.Resources.Network = r
		a.world.MarkSessionShared()
		a.ctx.PublishMapLock()
	})
}

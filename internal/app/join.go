package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	link "github.com/lixenwraith/vif/pkg/network"
)

// ErrJoinMidRun is returned by Join, the admission that re-derives the world, when
// the two participants are not at the same position; JoinAt admits at any tick for
// a caller that installs the world it is sent.
var ErrJoinMidRun = errors.New("join: participants are not at the same position")

// JoinAnchor describes this session to a participant that wants to share it: the
// identity VerifyAnchor already checks, plus the live D-14 map latch.
// The terminal fields describe this instance and the joiner ignores them.
func (m *membership) JoinAnchor() event.JoinAnchor {
	var out event.JoinAnchor
	m.world.RunSafe(func() { out = m.joinAnchorLocked() })
	return out
}

// joinAnchorLocked is JoinAnchor for a caller that already holds the world lock —
// the operator `:host` path does, because mode/ runs inside it.
// Caller MUST hold updateMutex.
func (m *membership) joinAnchorLocked() event.JoinAnchor {
	an := m.run.buildAnchor()
	an.Schema = event.JournalSchema
	cfg := m.world.Resources.Config
	an.MapWidth, an.MapHeight, an.CropOnResize = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
	an.CellAspect = float64(cfg.CellAspect)
	st := m.world.Resources.Event.Queue.Stamp()
	an.Run, an.Tick = st.Run, st.Tick
	return event.JoinAnchor{Anchor: an}
}

// Join admits this App into the session an anchor describes: it verifies the
// identity, refuses a position it cannot reconstruct, and adopts the map latch. Call
// after NewHeadless, before the first Tick. JoinAt is the same admission for a
// participant that receives the world instead of re-deriving it.
func (m *membership) Join(j event.JoinAnchor) error { return m.join(j, false) }

// JoinAt admits this App into a session at whatever tick the host has reached. The
// caller installs the capture; this only checks that the two instances are the same
// simulation and adopts the shared bounds.
func (m *membership) JoinAt(j event.JoinAnchor) error { return m.join(j, true) }

func (m *membership) join(j event.JoinAnchor, midRun bool) error {
	an := j.Anchor
	if err := firstAnchorMismatch("join", m.run.sessionAnchorFields(an)); err != nil {
		return err
	}
	if an.MapWidth <= 0 || an.MapHeight <= 0 {
		return fmt.Errorf("join: anchor carries no map latch (%dx%d)", an.MapWidth, an.MapHeight)
	}
	if st := m.run.Position(); !midRun && (st.Run != an.Run || st.Tick != an.Tick) {
		return fmt.Errorf("%w: host at run %d tick %d, this instance at run %d tick %d",
			ErrJoinMidRun, an.Run, an.Tick, st.Run, st.Tick)
	}
	m.adoptMapLatch(an)
	return nil
}

// JoinSession verifies a coordinator offer and adopts its map and roster. It is the
// tick-zero form, kept for a harness that builds both worlds itself; the session
// path takes JoinSessionAt.
func (m *membership) JoinSession(o network.SessionOffer) error {
	if err := m.validateSessionOffer(o, o.Assigned); err != nil {
		return err
	}
	if err := m.Join(o.Anchor); err != nil {
		return err
	}
	m.openAuthority(o, o.Assigned)
	return m.configureSessionRoster(o, o.Assigned)
}

// JoinSessionAt admits this instance into a running session by installing the host's
// world rather than reproducing it, in the order that is the whole of the join: the
// map latch, which a capture's placements are relative to; the FSM boot's queued spawn,
// which declares the cursor template; the staged install; then the roster on the
// installed world. bytes is the capture's wire cost, zero for one this run did not receive.
func (m *membership) JoinSessionAt(o network.SessionOffer, cap snapshot.SharedCapture, bytes int) error {
	if err := m.validateSessionOffer(o, o.Assigned); err != nil {
		return err
	}
	if err := m.JoinAt(o.Anchor); err != nil {
		return err
	}
	m.openAuthority(o, o.Assigned)
	m.scheduler.Settle()

	staged, err := m.glue.StageShared(cap)
	if err != nil {
		return fmt.Errorf("join capture: %w", err)
	}
	if err := staged.Commit(); err != nil {
		return err
	}
	stage, commit := staged.Timings()
	m.log.Info("session", "msg", "join installed the session world",
		"tick", cap.Header.Tick, "run", cap.Header.Run,
		"stage_us", stage.Microseconds(), "commit_us", commit.Microseconds())

	// The world a join installs is the host's current keyframe, which is what the
	// deltas that follow it are computed against. Adopting it here is what lets a
	// participant start applying corrections at the next cadence rather than at the
	// next keyframe.
	m.adoptCorrectionBaseline(cap, bytes)

	return m.bindSessionControl(o, o.Assigned)
}

// HostSession applies the same map and roster sequence after a guest accepts.
func (m *membership) HostSession(o network.SessionOffer) error {
	if err := m.validateSessionOffer(o, o.Host); err != nil {
		return err
	}
	m.adoptMapLatch(o.Anchor.Anchor)
	m.openAuthority(o, o.Host)
	m.authority.PublishChain()
	return m.configureSessionRoster(o, o.Host)
}

func (m *membership) validateSessionOffer(o network.SessionOffer, local link.PeerID) error {
	if err := o.Validate(); err != nil {
		return err
	}
	// The ceiling is on cursors rather than on participants: a dedicated host holds
	// a roster entry and an identity, and no slot on the map.
	if n := cursorParticipants(o.Roster); n > parameter.MaxPlayers {
		return fmt.Errorf("join roster has %d cursors, maximum is %d", n, parameter.MaxPlayers)
	}
	for _, p := range o.Roster {
		if p.Slot == parameter.NoPlayerSlot {
			continue
		}
		if int(p.Slot) >= parameter.MaxPlayers || int(p.ID) > parameter.MaxPlayers+1 {
			return fmt.Errorf("join roster assignment id %d slot %d exceeds maximum %d", p.ID, p.Slot, parameter.MaxPlayers)
		}
	}
	if _, ok := o.Entry(local); !ok {
		return fmt.Errorf("join roster omits local participant %d", local)
	}
	return nil
}

// cursorParticipants counts the roster entries that own a cursor.
func cursorParticipants(participants []network.RosterEntry) int {
	n := 0
	for _, p := range participants {
		if p.Slot != parameter.NoPlayerSlot {
			n++
		}
	}
	return n
}

// configureSessionRoster creates slots in coordinator order, then applies local control.
func (m *membership) configureSessionRoster(o network.SessionOffer, local link.PeerID) error {
	participants := make([]network.RosterEntry, 0, len(o.Roster))
	for _, p := range o.Roster {
		if p.Slot != parameter.NoPlayerSlot {
			participants = append(participants, p)
		}
	}
	slices.SortFunc(participants, func(x, y network.RosterEntry) int { return int(x.Slot) - int(y.Slot) })

	// The boot script's cursor spawn may still be queued: the FSM enters its boot
	// state inside New and nothing has ticked yet. Settling it is what publishes the
	// heat and energy template every rostered cursor is then created and armed from.
	m.scheduler.Settle()

	var initialHeat, initialEnergy int
	m.world.RunSafe(func() { initialHeat, initialEnergy = m.world.Resources.Player.InitialResources() })

	for _, p := range participants {
		var exists bool
		m.world.RunSafe(func() { exists = m.world.Resources.Player.Slot(p.Slot) != 0 })
		if exists {
			continue
		}
		m.ctx.PushEventOrigin(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{
			Slot: p.Slot, Center: true, Control: uint8(component.ControlRemote), PeerID: uint32(p.ID), Holder: uint32(p.Holder),
			Heat: initialHeat, Energy: initialEnergy,
		}, event.OriginDebug)
		m.scheduler.Settle()
	}

	var count int
	m.world.RunSafe(func() { count = m.world.Resources.Player.Count() })
	if count != len(participants) {
		return fmt.Errorf("join roster created %d cursors, want %d", count, len(participants))
	}
	if err := m.bindSessionControl(o, local); err != nil {
		return err
	}
	// Arming is a local-cursor operation; a dedicated host has none to arm.
	if assignment, ok := o.Entry(local); ok && assignment.Slot != parameter.NoPlayerSlot {
		m.ctx.PushLocal(event.EventCursorArmRequest,
			&event.CursorArmRequestPayload{Heat: initialHeat, Energy: initialEnergy})
		m.scheduler.Settle()
	}
	m.world.RunSafe(m.ctx.PublishMapLock)
	return nil
}

// bindSessionControl applies the D-13 control assignment over a roster that already
// exists: who owns each cursor, and which this instance drives. It creates nothing —
// the cursor this participant takes arrives as the EventParticipantJoined crossing at
// one agreed tick, the only way a shared entity may be created after tick zero
// (D-11), so a slot the offer names and the world does not hold is normal here.
func (m *membership) bindSessionControl(o network.SessionOffer, local link.PeerID) error {
	m.world.RunSafe(func() { m.bindCursorOwnersLocked(o.Roster, local) })
	localAssignment, ok := o.Entry(local)
	if !ok {
		return fmt.Errorf("join roster omits local participant %d", local)
	}
	// A participant drives the cursor in its place and none while the place holds
	// none or its Wraith, which its arrival brings or takes back as it crosses: the
	// boot slot is another's. A cursorless one takes the sentinel for good, so every
	// "is this my cursor" test answers no.
	slot := localAssignment.Slot
	m.world.RunSafe(func() {
		if e := m.world.Resources.Player.Slot(slot); e == 0 || m.world.Components.Wraith.HasEntity(e) {
			slot = parameter.NoPlayerSlot
		}
	})
	if slot != m.localSlot() {
		m.ctx.PushEventOrigin(event.EventCursorSetLocalRequest, &event.CursorSetLocalPayload{Slot: slot}, event.OriginDebug)
		m.scheduler.Settle()
	}
	m.world.RunSafe(m.ctx.PublishMapLock)
	return nil
}

// bindCursorOwnersLocked writes each slot's owning participant onto its cursor, and
// which of them this instance drives: a slot no roster attributes is a participant
// a succession cannot see leave. A Wraith keeps the owner it left with, driven by
// nobody, under the claim the roster names, which a session a run opens names
// afresh for its bots. Caller MUST hold updateMutex.
func (m *membership) bindCursorOwnersLocked(participants []network.RosterEntry, local link.PeerID) {
	roster := m.world.Resources.Player
	for _, p := range participants {
		if p.Slot == parameter.NoPlayerSlot {
			continue
		}
		e := roster.Slot(p.Slot)
		c, ok := m.world.Components.Cursor.GetPtr(e)
		switch {
		case !ok:
		case m.world.Components.Wraith.HasEntity(e):
			if c.PeerID == uint32(p.ID) {
				c.Claim = p.Claim
			}
		default:
			c.PeerID, c.Holder, c.Claim = uint32(p.ID), uint32(p.Holder), p.Claim
			c.Control = component.ControlRemote
			if p.ID == local {
				c.Control = component.ControlLocal
			}
		}
	}
}

// localSlot returns the roster slot this instance's input follows.
func (m *membership) localSlot() uint8 {
	var slot uint8
	m.world.RunSafe(func() { slot = m.world.Resources.Player.LocalSlot() })
	return slot
}

// localParticipant is this instance's session identity, zero outside a session.
func (m *membership) localParticipant() uint32 {
	var id uint32
	m.world.RunSafe(func() { id = m.world.LocalParticipant() })
	return id
}

// adoptMapLatch applies the host's bounds through the D-14 authority — the level
// setup path — rather than by writing Config, so grid, camera and cursors reflow as
// they would for a map script and entities are kept. Config.MapWidth has already
// installed the latch, so this is the confirmation; it runs unconditionally because
// the event is part of the session's record stream a replay has to see.
func (m *membership) adoptMapLatch(an event.JournalAnchor) {
	m.ctx.PushEventOrigin(event.EventLevelSetup, &event.LevelSetupPayload{
		Width: an.MapWidth, Height: an.MapHeight, CropOnResize: an.CropOnResize,
	}, event.OriginDebug)
	m.scheduler.Settle()
}

// AttachTransport binds a transport to this App, for a harness or an embedder that
// builds its own endpoint instead of taking the one NetworkService contributes.
// NetworkSystem reads the port per tick, so this needs no re-registration.
func (m *membership) AttachTransport(port engine.NetworkPort) {
	m.world.RunSafe(func() { m.attachTransportLocked(port) })
}

// attachTransportLocked is AttachTransport for a caller that already holds the
// world lock — the operator command path does, because mode/ runs inside it.
// Caller MUST hold updateMutex.
func (m *membership) attachTransportLocked(port engine.NetworkPort) {
	r := engine.NewNetworkResource(port)
	m.bindSessionHooks(r)
	term, holder := m.authorityStamp()
	if holder != 0 {
		r.Authority.Store(holder)
		r.Term.Store(uint64(term))
	}
	m.world.Resources.Network = r
	m.world.MarkSessionShared()
	m.world.LatchSession()
	m.ctx.PublishMapLock()
}

// bindSessionHooks answers a network resource from this run's session layer, the
// service's endpoint and an attached one alike.
func (m *membership) bindSessionHooks(r *engine.NetworkResource) {
	r.Relay = uint32(m.joinedThrough)
	r.OnDeparture = m.participantDeparted
	// Replays and staging worlds apply roster changes without ending their viewer.
	if _, live := r.Port.(engine.PeerDroppingPort); live {
		r.OnDismissed = func(shed bool) {
			m.shed.Store(shed)
			m.dismissed.Store(true)
			m.haltSeats(false)
			// The place is gone, and the claim with it.
			if c := m.claim.Swap(nil); c != nil {
				m.claims.forget(c)
			}
		}
	}
	// The correction queue takes bytes and nothing else: this runs inside a tick,
	// and decoding or installing a correction here would do both under the lock the
	// install itself needs.
	r.OnCorrection = m.receiveCorrection
	r.OnSelective = m.receiveSelective
	r.OnTickClosed = m.tickClosed
	r.OnAuthority = m.receiveAuthorityFrame
	r.OnPeerLost = m.reportPeerLost
	r.OnSessionRestart = m.receiveSessionRestart
	r.OnRelayJoin = m.onRelayJoin
	r.SeatsOnly = m.seatsOnly
	r.OnAuthorityAhead = m.authorityAhead
	r.DismissWraith = func() uint32 { return m.dismissWraith.Swap(0) }
	r.WraithKept = m.wraithKept
	// Read as each artifact arrives, since the context is built after the world.
	r.Relaying = func() bool { return m.ctx.Relaying.Load() }
	r.OnOwnState = m.keepRecord
}

// authorityAhead marks a hold the host has run past: a held host sends no epoch, so
// one past this instance's tick is the resume. The hold ends after the off-tick
// drain, since ending it settles, which a system mid-drain must not. Caller MUST
// hold updateMutex.
func (m *membership) authorityAhead() {
	if m.ctx.TimeCtl.Held() {
		m.hostResumed = true
	}
}

// releaseHold ends a hold no command here ends through the request a command makes,
// settled at once: the hold ends in the pass that dispatches the request, as on a
// replay of this run, which has no link or succession to end it, so input settled
// beside it counts alike on both. Caller MUST hold updateMutex, between two ticks.
func (m *membership) releaseHold(said string) {
	if !m.ctx.TimeCtl.Held() {
		return
	}
	start := m.ctx.TimeCtl.Holds(engine.HoldStart)
	m.ctx.PushLocalOrigin(event.EventGameHoldRequest, &event.GamePausePayload{Paused: false}, event.OriginSession)
	m.scheduler.SettleLocked()
	if !start { // the game moving is a start's answer
		m.ctx.SetStatusMessage(said, parameter.StatusNotice)
	}
}

// wraithKept reports whether a participant's Wraith is on its way back: a dial took
// its place back, or its arrival crossed and has yet to apply.
func (m *membership) wraithKept(participant uint32) bool {
	id := link.PeerID(participant)
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	if _, back := m.returning[id]; back {
		return true
	}
	i := slices.IndexFunc(m.sessionRoster, func(p network.RosterEntry) bool { return p.ID == id })
	return i >= 0 && !m.sessionRoster[i].Away
}

// keepRecord keeps what this run's own cursor holds as its sync sends it, which is
// what its player brings back should the run leave the session.
func (m *membership) keepRecord(p *event.CursorStatePayload) {
	if c := m.claim.Load(); c != nil {
		m.claims.keep(c, p.Record())
	}
}

// leaveClaim gives up the claim this run is in its session under, which keeps the
// record its last sync left.
func (m *membership) leaveClaim() {
	if c := m.claim.Swap(nil); c != nil {
		m.claims.release(c)
	}
}

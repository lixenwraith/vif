package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// sessionOfferFor builds the closed roster a coordinator would hand out, for a test
// that drives HostSession and JoinSessionAt directly instead of over a socket.
func sessionOfferFor(an event.JoinAnchor, n int) network.SessionOffer {
	o := network.SessionOffer{
		Anchor:            an,
		Host:              hostParticipantID,
		Assigned:          2,
		Term:              network.FirstTerm,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}
	for i := range n {
		o.Participants = append(o.Participants,
			network.SessionParticipant{ID: network.PeerID(i + 1), Slot: uint8(i)})
	}
	return o
}

// TestSnapshotJoinCarriesTheGoldDeadline closes the gold-deadline defect a
// reproducing joiner had: it settled the FSM boot queue at a different point than its
// host, so the two entered MainSpawnGold a tick apart and carried origins 50 ms apart
// for every sequence. A joiner installs the host's world now and the gold carrier
// writes both instants relative to the capture's tick, so gold.timer is compared.
func TestSnapshotJoinCarriesTheGoldDeadline(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x14AD, 160, 48)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x14AD, 84, 26, an)
	defer guest.Close()

	offer := sessionOfferFor(an, 2)
	if err := host.HostSession(offer); err != nil {
		t.Fatalf("host session: %v", err)
	}

	// Far enough in that a gold sequence is live and has been counting down for a
	// while: a timer read at spawn would match by accident.
	host.Tick(60)
	if !goldActive(host) {
		t.Fatal("no gold sequence is live; the deadline comparison proves nothing")
	}
	before := goldTimer(host)

	if err := guest.JoinSessionAt(offer, mustRoundTrip(t, host)); err != nil {
		t.Fatalf("join session: %v", err)
	}
	if got := goldTimer(guest); got != before {
		t.Fatalf("gold deadline after the join = %d, host holds %d", got, before)
	}
	assertSharedParity(t, host, guest, 0)

	// The origins have to stay together, not merely coincide at the install: a
	// deadline rebased onto the wrong tick agrees once and then drifts by exactly
	// one tick's worth for the life of the sequence.
	for step := range 8 {
		host.Tick(5)
		guest.Tick(5)
		if !goldActive(host) {
			break
		}
		if got, want := goldTimer(guest), goldTimer(host); got != want {
			t.Fatalf("gold deadline %d ticks after the join = %d, host holds %d",
				(step+1)*5, got, want)
		}
	}
}

// TestSnapshotJoinTakesTheHostsWorldNotItsOwn is the join criterion at its
// simplest: a participant that arrives mid-run holds the host's world, not the one
// its own seed would have produced by then.
func TestSnapshotJoinTakesTheHostsWorldNotItsOwn(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x2B0B, 120, 40)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x2B0B, 120, 40, an)
	defer guest.Close()

	offer := sessionOfferFor(an, 2)
	if err := host.HostSession(offer); err != nil {
		t.Fatalf("host session: %v", err)
	}
	host.Tick(240)

	// The guest is driven to a different tick first, so an install that did nothing
	// could not pass.
	guest.Tick(20)
	if !sharedSurfacesDiffer(host, guest) {
		t.Fatal("the two runs already agree; the install would prove nothing")
	}

	cap := mustRoundTrip(t, host)
	if err := guest.JoinSessionAt(offer, cap); err != nil {
		t.Fatalf("join session: %v", err)
	}
	if idx, lx, ly, differs := snapshot.FirstDiff(host.SnapshotShared(), guest.SnapshotShared()); differs {
		t.Fatalf("joined world differs at line %d\n  host:  %s\n  guest: %s\n%s",
			idx, lx, ly, strings.Join(snapshot.Diff(host.SnapshotShared(), guest.SnapshotShared(), 8), "\n"))
	}
	if got := guest.Position(); got.Tick != cap.Header.Tick || got.Run != cap.Header.Run {
		t.Fatalf("guest record position = run %d tick %d, capture named run %d tick %d",
			got.Run, got.Tick, cap.Header.Run, cap.Header.Tick)
	}
}

// TestSnapshotJoinLeavesEachParticipantDrivingItsOwnCursor is the D-13 half of an
// install: the whole cursor component travels, so a capture carries the sender's
// answer to which cursor it drives. A receiver that adopted it would simulate the
// host's cursor and not its own — two participants writing one cell, and none writing
// the other.
func TestSnapshotJoinLeavesEachParticipantDrivingItsOwnCursor(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x2B0C, 120, 40)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x2B0C, 120, 40, an)
	defer guest.Close()

	offer := sessionOfferFor(an, 2)
	if err := host.HostSession(offer); err != nil {
		t.Fatalf("host session: %v", err)
	}
	host.Tick(30)
	if err := guest.JoinSessionAt(offer, mustRoundTrip(t, host)); err != nil {
		t.Fatalf("join session: %v", err)
	}

	// The roster is identical on both, entity for entity: it is re-derived from the
	// installed cursor store rather than adopted or rebuilt.
	for slot := range 2 {
		var hostE, guestE core.Entity
		host.World().RunSafe(func() { hostE = host.World().Resources.Player.Slot(uint8(slot)) })
		guest.World().RunSafe(func() { guestE = guest.World().Resources.Player.Slot(uint8(slot)) })
		if hostE == 0 || hostE != guestE {
			t.Fatalf("slot %d = entity %d on the host, %d on the guest", slot, hostE, guestE)
		}
	}

	// Each participant simulates its own and only its own.
	assertControl(t, host, 0, component.ControlHuman)
	assertControl(t, host, 1, component.ControlRemote)
	assertControl(t, guest, 0, component.ControlRemote)
	assertControl(t, guest, 1, component.ControlHuman)

	if got := guest.localSlot(); got != 1 {
		t.Fatalf("guest follows slot %d, want its own slot 1", got)
	}
	if got := host.localSlot(); got != 0 {
		t.Fatalf("host follows slot %d, want its own slot 0", got)
	}
}

func assertControl(t *testing.T, a *App, slot uint8, want component.ControlKind) {
	t.Helper()
	var got component.ControlKind
	var found bool
	a.World().RunSafe(func() {
		e := a.World().Resources.Player.Slot(slot)
		if c, ok := a.World().Components.Cursor.GetComponent(e); ok {
			got, found = c.Control, true
		}
	})
	if !found {
		t.Fatalf("slot %d holds no cursor", slot)
	}
	if got != want {
		t.Fatalf("slot %d control = %v, want %v", slot, got, want)
	}
}

func goldActive(a *App) bool {
	return a.World().Resources.Status.Bools.Get("gold.active").Load()
}

func goldTimer(a *App) int64 {
	return a.World().Resources.Status.Ints.Get("gold.timer").Load()
}

// TestAJoinerAdoptsTheHostsRngSession is what a restarted host needs from a join. The
// seed says which family of streams a run draws from and the session counter which
// game in that family, which a reset advances. A joiner that counted from one would
// build a different world and be refused on the identity check — which made a host
// that had ever reset unjoinable, and a dedicated host restarts routinely.
func TestAJoinerAdoptsTheHostsRngSession(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()

	// The run a fresh host reports, and the run it reports after a restart.
	first := host.JoinAnchor().Anchor.Session
	host.Context().PushEventOrigin(event.EventGameResetRequest,
		&event.GameResetPayload{}, event.OriginDebug)
	host.Settle()
	host.Tick(4)
	an := host.JoinAnchor()
	if an.Anchor.Session <= first {
		t.Fatalf("a restart left the session counter at %d, want it past %d",
			an.Anchor.Session, first)
	}

	cfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: 120, Height: 40}, network.SessionOffer{
		Anchor: an, Host: 1, Assigned: 2, Term: network.FirstTerm,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
		Participants: []network.SessionParticipant{
			{ID: 1, Slot: 0}, {ID: 2, Slot: 1},
		},
	})
	if err != nil {
		t.Fatalf("join config: %v", err)
	}
	if cfg.Session != an.Anchor.Session {
		t.Fatalf("the join config carries session %d, the host is on %d", cfg.Session, an.Anchor.Session)
	}
	guest, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("join app: %v", err)
	}
	defer guest.Close()
	if err := guest.JoinAt(an); err != nil {
		t.Fatalf("a joiner that adopted the host's session was refused: %v", err)
	}
}

func TestReproducingJoinAdmissionAndRefusals(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF

	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	host.SetupLevel(100, 30, true, false)

	an := host.JoinAnchor()
	if an.Anchor.MapWidth != 100 || an.Anchor.MapHeight != 30 || an.Anchor.CropOnResize {
		t.Fatalf("host latch = %dx%d crop %t, want 100x30 crop false",
			an.Anchor.MapWidth, an.Anchor.MapHeight, an.Anchor.CropOnResize)
	}

	guest := mustHeadless(t, seed, 180, 56)
	defer guest.Close()
	if err := guest.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}
	var gotW, gotH int
	var gotCrop bool
	guest.World().RunSafe(func() {
		cfg := guest.World().Resources.Config
		gotW, gotH, gotCrop = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
	})
	if gotW != 100 || gotH != 30 || gotCrop {
		t.Fatalf("guest map = %dx%d crop %t, want the host's 100x30 crop false", gotW, gotH, gotCrop)
	}
	// Only the shared bounds are adopted; the guest keeps its own terminal.
	if guest.Context().Width != 180 || guest.Context().Height != 56 {
		t.Fatalf("guest terminal = %dx%d, want its own 180x56",
			guest.Context().Width, guest.Context().Height)
	}

	foreign := mustHeadless(t, 0xC0FFEE, 120, 40)
	defer foreign.Close()
	if err := foreign.Join(an); err == nil || !strings.Contains(err.Error(), "seed") {
		t.Fatalf("join with a foreign seed = %v, want a seed mismatch", err)
	}

	host.Tick(5)
	late := mustHeadless(t, seed, 120, 40)
	defer late.Close()
	if err := late.Join(host.JoinAnchor()); !errors.Is(err, ErrJoinMidRun) {
		t.Fatalf("join with a mid-run host = %v, want ErrJoinMidRun", err)
	}
}

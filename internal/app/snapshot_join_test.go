package app

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// sessionOfferFor builds the closed roster a coordinator would hand out, for a test
// that drives HostSession and JoinSessionAt directly instead of over a socket.
func sessionOfferFor(an event.JoinAnchor, n int) network.SessionOffer {
	o := network.SessionOffer{
		Anchor:            an,
		Host:              hostParticipantID,
		Assigned:          2,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}
	for i := range n {
		o.Participants = append(o.Participants,
			network.SessionParticipant{ID: network.PeerID(i + 1), Slot: uint8(i)})
	}
	return o
}

// mustCapture reads a capture through the encoder, so a test exercises the same
// bytes a join would and not a value that never left the process.
func mustCapture(t *testing.T, a *App) SharedCapture {
	t.Helper()
	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	body, err := EncodeCapture(cap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeCapture(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

// TestSnapshotJoinCarriesTheGoldDeadline closes Phase 2's one recorded defect.
//
// The gold sequence's remaining time is measured from the tick the sequence
// spawned on. A joiner that reproduced the session from tick zero settled the FSM
// boot queue at a different point than its host — the join's level setup settled it,
// the host's construction did not — so the two entered MainSpawnGold a tick apart
// and carried origins 50 ms apart for the life of every sequence. gold.timer was
// excluded from the compared surface for that reason alone, which meant the one
// thing the exclusion hid was also the only thing that would have caught it.
//
// A joiner no longer reproduces anything. It installs the host's world, and the
// gold carrier writes both instants relative to the capture's tick, so the origin
// is the host's on every instance. The key is compared now, and this is the case
// that would have failed before: a host well into a sequence, a joiner arriving in
// the middle of it, and the two asked for the same remaining time.
func TestSnapshotJoinCarriesTheGoldDeadline(t *testing.T) {
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

	if err := guest.JoinSessionAt(offer, mustCapture(t, host)); err != nil {
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

	cap := mustCapture(t, host)
	if err := guest.JoinSessionAt(offer, cap); err != nil {
		t.Fatalf("join session: %v", err)
	}
	if idx, lx, ly, differs := FirstDiff(host.SnapshotShared(), guest.SnapshotShared()); differs {
		t.Fatalf("joined world differs at line %d\n  host:  %s\n  guest: %s\n%s",
			idx, lx, ly, strings.Join(Diff(host.SnapshotShared(), guest.SnapshotShared(), 8), "\n"))
	}
	if got := guest.Position(); got.Tick != cap.Header.Tick || got.Run != cap.Header.Run {
		t.Fatalf("guest record position = run %d tick %d, capture named run %d tick %d",
			got.Run, got.Tick, cap.Header.Run, cap.Header.Tick)
	}
}

// TestSnapshotJoinLeavesEachParticipantDrivingItsOwnCursor is the D-13 half of an
// install. Every cursor is a shared entity and the whole component travels, so a
// capture also carries the sender's answer to which of them it drives — its own is
// ControlHuman and everyone else's is ControlRemote. A receiver that adopted that
// would start simulating the host's cursor and stop simulating its own, which is
// two participants writing one cell and one participant writing none.
func TestSnapshotJoinLeavesEachParticipantDrivingItsOwnCursor(t *testing.T) {
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
	if err := guest.JoinSessionAt(offer, mustCapture(t, host)); err != nil {
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

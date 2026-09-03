package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/resource"
)

// cursorlessOffer is the roster a dedicated host closes on: itself with no slot,
// and one cursor-owning guest per slot from zero.
func cursorlessOffer(an event.JoinAnchor, guests int) network.SessionOffer {
	o := network.SessionOffer{
		Anchor: an, Host: hostParticipantID, Assigned: 2,
		Term:              network.FirstTerm,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
		Participants: []network.SessionParticipant{
			{ID: hostParticipantID, Slot: parameter.NoPlayerSlot},
		},
	}
	for i := range guests {
		o.Participants = append(o.Participants,
			network.SessionParticipant{ID: network.PeerID(i + 2), Slot: uint8(i)})
	}
	return o
}

// TestADedicatedHostDrivesNoCursor is what "zero players" has to mean: the
// coordinator keeps its participant identity and its authority, the roster is
// exactly the guests, and nothing on the map is this instance's to simulate.
//
// The boot cursor is not suppressed. It is created as it always is and the roster
// hands it to the first guest, which is what keeps shared creation order identical
// to an ordinary host's.
func TestADedicatedHostDrivesNoCursor(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x5E4E, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)

	offer := cursorlessOffer(host.JoinAnchor(), 2)
	if err := host.HostSession(offer); err != nil {
		t.Fatalf("host a cursorless session: %v", err)
	}

	if got := host.localSlot(); got != parameter.NoPlayerSlot {
		t.Fatalf("the coordinator took slot %d, want none", got)
	}
	if host.localPlayers() != 0 {
		t.Fatal("the coordinator reports a local player")
	}
	var (
		count  int
		local  core.Entity
		owned  int
		slots  []uint8
		peerOf = map[uint8]uint32{}
	)
	host.World().RunSafe(func() {
		w := host.World()
		count = w.Resources.Player.Count()
		local = w.Resources.Player.Entity
		w.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
			slots = append(slots, c.Slot)
			peerOf[c.Slot] = c.PeerID
			if c.Control != component.ControlRemote {
				owned++
			}
			return true
		})
	})
	if count != 2 {
		t.Fatalf("the session holds %d cursors, want one per guest", count)
	}
	if local != 0 {
		t.Fatalf("the coordinator is bound to cursor %d", uint64(local))
	}
	if owned != 0 {
		t.Fatalf("the coordinator simulates %d cursors", owned)
	}
	// The boot cursor is slot zero and now belongs to the first guest.
	if peerOf[0] != 2 || peerOf[1] != 3 {
		t.Fatalf("cursor ownership = %v, want slot 0 to participant 2 and slot 1 to 3", peerOf)
	}
	if len(slots) != 2 {
		t.Fatalf("cursor slots = %v", slots)
	}
}

// TestAGuestOfADedicatedHostDrivesItsOwn is the other side of the same roster: a
// participant that does hold a slot still binds it, and the cursorless entry in
// the offer changes nothing about that.
func TestAGuestOfADedicatedHostDrivesItsOwn(t *testing.T) {
	t.Parallel()
	guest := mustHeadless(t, 0x5E4E, 120, 40)
	defer guest.Close()
	tickUntilCursor(t, guest)

	offer := cursorlessOffer(guest.JoinAnchor(), 2)
	offer.Assigned = 3
	if err := guest.JoinSession(offer); err != nil {
		t.Fatalf("join a cursorless host's session: %v", err)
	}
	if got := guest.localSlot(); got != 1 {
		t.Fatalf("the guest drives slot %d, want its own slot 1", got)
	}
	if guest.localPlayers() != 1 {
		t.Fatal("the guest reports no local player")
	}
	var owned int
	guest.World().RunSafe(func() {
		guest.World().Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
			if c.Control != component.ControlRemote {
				owned++
			}
			return true
		})
	})
	if owned != 1 {
		t.Fatalf("the guest simulates %d cursors, want exactly its own", owned)
	}
}

// TestACursorlessRosterIsOnlyTheCoordinators pins the protocol rule: one
// participant may hold no slot, and only the coordinator may be it. Anything else
// takes a vote and a roster entry while contributing nothing the roster describes.
func TestACursorlessRosterIsOnlyTheCoordinators(t *testing.T) {
	t.Parallel()
	an := event.JoinAnchor{}
	base := cursorlessOffer(an, 2)
	if err := base.Validate(); err != nil {
		t.Fatalf("a cursorless coordinator was refused: %v", err)
	}

	twoCursorless := cursorlessOffer(an, 2)
	twoCursorless.Participants[1].Slot = parameter.NoPlayerSlot
	if err := twoCursorless.Validate(); err == nil {
		t.Fatal("two cursorless participants were accepted")
	}

	wrongOne := cursorlessOffer(an, 2)
	wrongOne.Participants[0].Slot = 2
	wrongOne.Participants[1].Slot = parameter.NoPlayerSlot
	if err := wrongOne.Validate(); err == nil {
		t.Fatal("a cursorless participant that is not the host was accepted")
	}
}

// TestADedicatedHostCorrectsItsGuests drives the whole arrangement: a coordinator
// that owns no cursor still authors the shared world, publishes corrections and
// converges the participants that do.
func TestADedicatedHostCorrectsItsGuests(t *testing.T) {
	t.Parallel()
	const seed = 0x5E4E
	apps := make([]*App, 3)
	for i := range apps {
		apps[i] = mustHeadless(t, seed, 120, 40)
	}
	t.Cleanup(func() {
		for _, a := range apps {
			a.Close()
		}
	})

	offer := cursorlessOffer(apps[0].JoinAnchor(), 2)
	if err := apps[0].HostSession(offer); err != nil {
		t.Fatalf("host: %v", err)
	}
	for i := 1; i < len(apps); i++ {
		local := offer
		local.Assigned = network.PeerID(i + 1)
		if err := apps[i].JoinSession(local); err != nil {
			t.Fatalf("participant %d join: %v", i+1, err)
		}
	}

	mesh := network.NewMesh()
	mesh.Link(1, 2)
	mesh.Link(1, 3)
	for i, a := range apps {
		a.AttachTransport(mesh.Node(network.PeerID(i + 1)))
	}
	for _, a := range apps {
		a.activateNetworkSession()
	}

	// Each guest drives its own cursor, so the correction has something to close.
	for i, a := range apps[1:] {
		var e core.Entity
		a.World().RunSafe(func() { e = a.World().Resources.Player.Entity })
		if e == 0 {
			t.Fatalf("participant %d holds no cursor", i+2)
		}
		a.Context().PushCrossing(event.EventCursorMoveRequest,
			&event.CursorMoveRequestPayload{Entity: e, X: 10 + i*20, Y: 8 + i*4})
		a.Settle()
	}

	advance := func() {
		for _, a := range apps {
			a.Tick(1)
		}
	}
	want := deliverCorrection(t, apps[0], apps[1:], advance)
	for i, a := range apps[1:] {
		assertCorrected(t, want, a, "guest of a dedicated host")
		if a.localPlayers() != 1 {
			t.Fatalf("participant %d lost its cursor across a correction", i+2)
		}
	}
	if apps[0].localPlayers() != 0 {
		t.Fatal("the coordinator acquired a cursor across a correction")
	}
	if sent := statOf(apps[0], "snapshot.corrections_sent"); sent == 0 {
		t.Fatal("the coordinator published nothing")
	}
}

// TestADedicatedHostAdmitsALateDial covers the seam that lets a server outlive its
// guests: the mid-run gate is installed from construction and answers nothing
// until the startup lobby has closed, and it finds the endpoint NetworkService
// contributed rather than one the run opened for itself with :host.
func TestADedicatedHostAdmitsALateDial(t *testing.T) {
	// Not parallel: this binds a real socket.
	a, err := New(Config{
		Mode: ModeServer, HostAddress: "127.0.0.1:0", Participants: 1,
		Width: 120, Height: 40, Resources: resource.Options{Embedded: true}, Seed: 0x5E4E,
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	defer a.Close()

	if a.cfg.networkConfig.OnAdmit == nil {
		t.Fatal("a dedicated host installs no mid-run gate")
	}
	if a.cfg.networkConfig.MaxPeers != parameter.MaxPlayers {
		t.Fatalf("a dedicated host accepts %d peers, want the full roster",
			a.cfg.networkConfig.MaxPeers)
	}
	// Before the lobby closes the gate is startHostSessionOn's, and a second one
	// would race it. An unarmed dial is therefore a no-op rather than a refusal.
	a.admitLateJoiner(2)
	if a.lateJoins.Load() {
		t.Fatal("the mid-run gate armed itself")
	}

	if err := a.hub.StartAll(); err != nil {
		t.Fatalf("start services: %v", err)
	}
	port, err := a.socketPort()
	if err != nil || port == nil {
		t.Fatalf("a dedicated host has no endpoint to admit onto: %v", err)
	}
	if bound := port.Addr(); bound == nil {
		t.Fatal("the endpoint is not bound")
	}
}

// TestServerModeIsWiredWithoutATerminal pins the shape of a dedicated host: it
// runs the real clock and the scheduler goroutine like interactive play, and
// builds none of the I/O a person would use.
func TestServerModeIsWiredWithoutATerminal(t *testing.T) {
	t.Parallel()
	if ModeServer.Presents() || ModeServer.Audio() || ModeServer.OwnsInput() || ModeServer.OwnsGeometry() {
		t.Fatal("a dedicated host builds I/O nobody is there to use")
	}
	if ModeServer.Driven() {
		t.Fatal("a dedicated host waits for a caller to tick it")
	}
	if !ModeServer.Serves() {
		t.Fatal("ModeServer does not report itself as a dedicated host")
	}

	base := func() Config {
		return Config{
			Mode: ModeServer, HostAddress: "127.0.0.1:0", Participants: 1,
			Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("a one-guest server was refused: %v", err)
	}
	// One guest is a session; on an ordinary host the same value would not be.
	ordinary := base()
	ordinary.Mode, ordinary.Participants = ModePlay, 1
	if err := ordinary.Validate(); err == nil {
		t.Fatal("an interactive host accepted a one-participant lobby")
	}
	for _, tc := range []struct {
		name  string
		apply func(*Config)
	}{
		{"no bind address", func(c *Config) { c.HostAddress = "" }},
		{"more guests than slots", func(c *Config) { c.Participants = parameter.MaxPlayers + 1 }},
		{"a simulation rate", func(c *Config) { c.TimeScaleSpec = "2" }},
		{"a colour mode", func(c *Config) { c.ColorModeSet = true }},
	} {
		cfg := base()
		tc.apply(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("a dedicated host accepted %s", tc.name)
		}
	}
}

package app

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	link "github.com/lixenwraith/vif/pkg/network"
)

// routerStandIn is a front door for doors alone, as vif-router places them: a run's
// publication becomes a control connection, a dial on a door's name is spliced to
// the connection the run opens back for it.
func routerStandIn(t *testing.T) (string, *network.TunnelHub) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().String()
	hub := network.NewTunnelHub(func(id string) string { return "vif://" + addr + "/" + id }, 4, 0)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				first, err := link.DecodeWithin(conn, network.MaxTunnelFrame)
				if err != nil {
					_ = conn.Close()
					return
				}
				switch first.Type {
				case network.MsgSessionPublish:
					var req network.PublishRequest
					_ = json.Unmarshal(first.Payload, &req)
					if d, err := hub.Publish(conn, "", req); err == nil {
						hub.Serve(d)
					}
				case network.MsgTunnelAccept:
					var ticket network.TunnelTicket
					_ = json.Unmarshal(first.Payload, &ticket)
					if !hub.Accept(conn, ticket.Ticket) {
						_ = conn.Close()
					}
				case network.MsgSessionRoute:
					upstream, session, err := hub.Open(string(first.Payload), parameter.NetworkTunnelOpenTimeout)
					if err != nil {
						network.RefuseJoin(conn, err, time.Second)
						_ = conn.Close()
						return
					}
					_ = link.NewMessage(network.MsgSessionRoute, []byte(session)).Encode(upstream)
					var done sync.WaitGroup
					done.Add(2)
					pipe := func(dst, src net.Conn) {
						defer done.Done()
						_, _ = io.Copy(dst, src)
						if half, ok := dst.(interface{ CloseWrite() error }); ok {
							_ = half.CloseWrite()
						}
					}
					go pipe(upstream, conn)
					go pipe(conn, upstream)
					done.Wait()
					_ = conn.Close()
					_ = upstream.Close()
				default:
					_ = conn.Close()
				}
			}()
		}
	}()
	return addr, hub
}

// TestARelayPublishesItsDoorThroughTheRouter: a guest with -relay vif://<router>
// publishes its door through the router rather than binding one, the chain names the
// link the router gave it, and a person who dials that link joins through the guest,
// the router placing the dial on the connection the guest opened back.
func TestARelayPublishesItsDoorThroughTheRouter(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{Membership: Membership{HostAddress: addr, Bots: []string{"roam"}}}, "patrol", nil)
	waitForCursors(t, host, 2)
	router, hub := routerStandIn(t)
	relay, _ := driveBot(t, Config{Membership: Membership{JoinAddress: addr,
		RelayAddress: "vif://" + router}}, "patrol", nil)
	waitForCursors(t, host, 3)
	relayID := link.PeerID(relay.membership.LocalParticipant())
	var door string
	waitUntil(t, func() bool {
		d, ok := host.authority.Chain().Door(relayID)
		door = d
		return ok
	}, "the host's chain naming the relay's door")
	e, err := link.ParseEndpoint(door)
	if err != nil || e.Addr != router || !hub.Has(e.Name) {
		t.Fatalf("the relay's door is %q (%v); want a link at the router naming a door it carries", door, err)
	}
	person, _ := driveBot(t, Config{Membership: Membership{JoinAddress: e.Addr, SessionName: e.Name}}, "roam", nil)
	for _, a := range []*App{host, relay, person} {
		waitForCursors(t, a, 4)
	}
	if through := relayOf(person); through != relayID || person.membership.SessionTransport().PeerCount() != 1 {
		t.Fatalf("the person joined through participant %d holding %d links; want through the relay, on its one link",
			through, person.membership.SessionTransport().PeerCount())
	}
	waitForMotion(t, host, cursorOf(t, host, person.membership.LocalParticipant()), "the person's cursor on the host")
}

const doorPhrase, otherPhrase = "door-phrase-one", "other-phrase-two"

// ex runs one command as the command line does, in play as Run's loop serves it,
// and returns what the status line then reads.
func ex(a *App, line string) string {
	a.World().RunSafe(func() {
		was := a.cfg.Mode
		a.cfg.Mode = ModePlay
		mode.ExecuteCommand(a.Context(), line)
		a.cfg.Mode = was
	})
	return a.Context().GetStatusMessage()
}

// previewWith dials addr as a newcomer showing phrase, and says how the door answered.
func previewWith(addr, phrase string) error {
	cfg := network.DebugConfig(link.RolePeer, "")
	cfg.Keys, cfg.Passphrase = []network.Key{network.NewKey()}, phrase
	pending, _, err := network.PreviewSession(addr, cfg)
	if err == nil {
		_ = pending.Close()
	}
	return err
}

// gatedHost is a run its player opened behind doorPhrase with :host, ticking.
func gatedHost(t *testing.T) *App {
	t.Helper()
	a := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	tickUntilCursor(t, a)
	stop := tickInBackground(a)
	t.Cleanup(func() { stop(); a.Close() })
	if got := ex(a, "host 127.0.0.1:0 passphrase "+doorPhrase); a.HostAddr() == "" {
		t.Fatalf(":host opened no door: %q", got)
	}
	return a
}

// TestADoorTakesThePassphraseSetForIt: a door asks each dial for the phrase its
// run holds as the dial arrives. One opened after the run's bots, whose port was
// made and whose offers were written under the launch's phrase, takes the one
// :host set; one opened after leaving a gated session takes the run's own, not
// the session's it left.
func TestADoorTakesThePassphraseSetForIt(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	wrong := &network.Refusal{Code: network.RefusalCredential}
	t.Run("after the run's bots", func(t *testing.T) {
		a, err := NewHeadless(Config{Seed: fixtureSeed, Width: BotWidth, Height: BotHeight,
			Membership: Membership{Passphrase: doorPhrase}, Resources: resource.Options{Embedded: true}})
		if err != nil {
			t.Fatal(err)
		}
		tickUntilCursor(t, a)
		stop := tickInBackground(a)
		t.Cleanup(func() { stop(); a.Close() })
		a.World().RunSafe(func() { err = a.ctx.SessionCtl.AddBot("roam") })
		if err != nil {
			t.Fatal(err)
		}
		waitForSeats(t, a, 1)
		ex(a, "host 127.0.0.1:0 passphrase "+otherPhrase)
		if err := previewWith(a.HostAddr(), otherPhrase); err != nil {
			t.Fatalf("the door refused the passphrase :host set: %v", err)
		}
		if err := previewWith(a.HostAddr(), doorPhrase); !errors.Is(err, wrong) {
			t.Fatalf("the phrase the run launched with: %v; want it refused", err)
		}
	})
	t.Run("after leaving a gated session", func(t *testing.T) {
		host := gatedHost(t)
		guest, _ := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, BotWidth, BotHeight,
			func(c *Config) { c.Membership.Passphrase = doorPhrase })
		guest.cfg.Membership.JoinAddress = host.HostAddr() // what -join sets; a headless run refuses it
		stop := tickInBackground(guest)
		t.Cleanup(stop)
		waitForCursors(t, host, 2) // both runs tick behind the test, which only reads them
		waitForCursors(t, guest, 2)
		leaveAs(t, guest)
		ex(guest, "host 127.0.0.1:0 passphrase "+otherPhrase)
		if err := previewWith(guest.HostAddr(), otherPhrase); err != nil {
			t.Fatalf("the door refused the passphrase :host set: %v", err)
		}
		if err := previewWith(guest.HostAddr(), doorPhrase); !errors.Is(err, wrong) {
			t.Fatalf("the phrase of the session it left: %v; want it refused", err)
		}
	})
}

// TestARefusedCommandSetsNoPassphrase: a :join or :host a hosting run refuses, for
// the reason its menu row gives, leaves the phrase :session shows and its door
// takes as they were.
func TestARefusedCommandSetsNoPassphrase(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	a := gatedHost(t)
	for _, c := range []struct{ line, want string }{
		{"join 127.0.0.1:1 " + otherPhrase, "Join: this run is already in a session"},
		{"host 127.0.0.1:0 passphrase " + otherPhrase, "Host: this run is already hosting"},
	} {
		if got := ex(a, c.line); got != c.want {
			t.Fatalf(":%s reads %q; want %q", c.line, got, c.want)
		}
	}
	if line := ex(a, "session"); !strings.Contains(line, "(passphrase "+doorPhrase+")") {
		t.Fatalf(":session reads %q; want the passphrase the door opened with", line)
	}
	if err := previewWith(a.HostAddr(), doorPhrase); err != nil {
		t.Fatalf("the door after the refused commands: %v; want its passphrase admitted", err)
	}
}

// TestARestartKeepsThePassphraseItsPlayerTyped: the phrase :host or :join set is
// the next run's too, so a door a scenario change reopens takes it and a run that
// joined with it shows it again when its session moves.
func TestARestartKeepsThePassphraseItsPlayerTyped(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	root := t.TempDir()
	writeScenario(t, root, "other", "A scenario this root holds.")
	line := runConfig{Config: Config{Seed: fixtureSeed, Width: BotWidth, Height: BotHeight, Resources: resource.Options{Dir: root}}}
	host, err := NewHeadless(line.Config)
	if err != nil {
		t.Fatal(err)
	}
	tickUntilCursor(t, host)
	stop := tickInBackground(host)
	t.Cleanup(func() { stop(); host.Close() })
	ex(host, "host 127.0.0.1:0 passphrase "+doorPhrase)

	guest, err := NewHeadless(line.Config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(guest.Close)
	tickUntilCursor(t, guest)
	guest.World().RunSafe(func() { guest.cfg.Mode = ModePlay }) // :join serves Run's loop
	ex(guest, "join "+host.HostAddr()+" "+doorPhrase)
	waitUntil(t, func() bool { return guest.restart.Load() != nil }, "the join admitted; status "+guest.Context().GetStatusMessage())
	joined := guest.restart.Load()
	defer joined.Dialled.Abandon(errors.New("the test is over"))
	if next, _ := line.restarted(joined); next.Membership.Passphrase != doorPhrase {
		t.Fatalf("the run that joined shows %q at its next dial; want the phrase its player typed", next.Membership.Passphrase)
	}

	host.World().RunSafe(func() { host.cfg.Mode = ModePlay }) // a scenario change serves Run's loop
	ex(host, "n other")
	changed := host.restart.Load()
	if changed == nil || changed.Host == "" {
		t.Fatalf("the scenario change asked for %+v; want a run that reopens the door", changed)
	}
	if next, _ := line.restarted(changed); next.Membership.Passphrase != doorPhrase {
		t.Fatalf("the reopened door takes %q; want the phrase :host set", next.Membership.Passphrase)
	}
}

// TestADoorReopenedWithoutItsPhraseKeepsItsGate: a scenario change carries the
// verifier its door took, so a run that holds no phrase for the session, one that
// took it over, reopens a door that takes the session's passphrase and no bare key.
func TestADoorReopenedWithoutItsPhraseKeepsItsGate(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	root := t.TempDir()
	writeScenario(t, root, "other", "A scenario this root holds.")
	line := runConfig{Config: Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 80, Height: 24, Resources: resource.Options{Dir: root}}}
	a, err := NewHeadless(line.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	tickUntilCursor(t, a)
	ex(a, "host 127.0.0.1:0 passphrase "+doorPhrase)
	a.World().RunSafe(func() { a.cfg.Mode = ModePlay }) // a scenario change serves Run's loop
	ex(a, "n other")
	req := a.restart.Load()
	if req == nil || req.Door == nil {
		t.Fatalf("the scenario change asked for %+v; want the door's verifier carried", req)
	}
	next, _ := line.restarted(req)
	next.Membership.Passphrase = "" // as a run whose player never typed the phrase is rebuilt
	b, err := newDriven(next)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	tickUntilCursor(t, b)
	if err := b.BeginHosting(next.wiring.resumeHost); err != nil {
		t.Fatal(err)
	}
	if err := previewWith(b.HostAddr(), doorPhrase); err != nil {
		t.Fatalf("the reopened door refused the session's passphrase: %v", err)
	}
	if err := previewWith(b.HostAddr(), ""); !errors.Is(err, &network.Refusal{Code: network.RefusalSignIn}) {
		t.Fatalf("a dial with no passphrase at the reopened door: %v; want it asked for one", err)
	}
}

// TestBotsAreSeatedInAGatedSession: a run's bots take their seats over a stream
// of its own, which is no door and asks them no passphrase: the host's, and a
// guest's through the guest.
func TestBotsAreSeatedInAGatedSession(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{Membership: Membership{HostAddress: addr, Bots: []string{"roam"},
		Passphrase: doorPhrase}}, "patrol", nil)
	waitForCursors(t, host, 2)
	guest, _ := driveBot(t, Config{Membership: Membership{JoinAddress: addr, Bots: []string{"roam"},
		Passphrase: doorPhrase}}, "patrol", nil)
	waitForCursors(t, host, 4)
	waitForCursors(t, guest, 4)
}

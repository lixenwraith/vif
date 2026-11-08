package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app/internal/membership"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	link "github.com/lixenwraith/vif/pkg/network"
)

// pressPresentation is a screen with a keyboard: what a test sends on events is what
// its player pressed.
type pressPresentation struct {
	framePresentation
	events chan terminal.Event
}

func (p *pressPresentation) Events() <-chan terminal.Event { return p.events }

// press is the key a player reaches for to join.
var press = terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'j'}

// waitingJoiner builds the run its player's join opens, as Loop finds it: prepared on
// its session, its world built and checked, waiting for their press, with bots to
// seat once it is in.
func waitingJoiner(t *testing.T, addr string, bots ...string) (*App, *pressPresentation) {
	t.Helper()
	cfg := joinerConfig(addr, true, bots...)
	guest, err := newSessionApp(cfg)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	t.Cleanup(guest.Close)
	if guest.membership.HoldsPlace() || guest.membership.LocalParticipant() != 0 {
		t.Fatalf("a join waiting for its press is seated as participant %d", guest.membership.LocalParticipant())
	}
	return guest, cfg.host.Presentation.(*pressPresentation)
}

// joinerConfig is the run a player's join of addr opens on a screen with a keyboard,
// waiting for their press or not.
func joinerConfig(addr string, press bool, bots ...string) runConfig {
	screen := &pressPresentation{framePresentation: framePresentation{w: 120, h: 40}, events: make(chan terminal.Event, 1)}
	return runConfig{Config: Config{Mode: ModePlay, Membership: Membership{JoinAddress: addr, Bots: bots},
		Resources: resource.Options{Embedded: true}}, host: Host{Presentation: screen},
		wiring: wiring{membership: membership.Wiring{JoinPress: press}}}
}

// hostingRun is a run with a player that opened its session to cap participants,
// ticking until the test is done with it.
func hostingRun(t *testing.T, seed uint64, cap int) *App {
	t.Helper()
	host := mustHeadless(t, seed, 120, 40)
	t.Cleanup(host.Close)
	host.cfg.Membership.Participants = cap
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	t.Cleanup(tickInBackground(host))
	return host
}

// TestAJoinWaitingForItsPressHoldsNoPlace: a join its player started holds nothing in
// its session until the press, so a dial that seats at once takes the session's last
// place first, and the press is refused as a full session's dial would be. A place
// assigned at the dial would have been held for a player who might never come.
func TestAJoinWaitingForItsPressHoldsNoPlace(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x3031
	host := hostingRun(t, seed, 2)
	waiting, screen := waitingJoiner(t, host.HostAddr())
	if n := host.membership.GuestCount(); n != 0 {
		t.Fatalf("the host holds %d guest(s) for a join that has not pressed", n)
	}

	mustSocketJoiner(t, host.HostAddr(), seed, 120, 40)
	screen.events <- press
	next, err := waiting.awaitJoinPress(nil)
	if next != nil || !errors.Is(err, &network.Refusal{Code: network.RefusalFull}) {
		t.Fatalf("the press into a session filled meanwhile was answered %v, %v; want a full session's refusal", next, err)
	}
	if waiting.membership.HoldsPlace() {
		t.Fatal("the refused press seated its run")
	}
}

// TestALatePressDialsAgainIntoTheSameGame: the host keeps a prepared join's handshake
// for its ready period and no longer, holding nothing for it meanwhile. A press after
// that dials again and seats the world this run already built, which the gate then
// admits as any joiner's.
func TestALatePressDialsAgainIntoTheSameGame(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	host := hostingRun(t, 0x3032, 0)
	guest, screen := waitingJoiner(t, host.HostAddr())
	world := guest.World()

	time.Sleep(parameter.NetworkJoinPressWindow + parameter.SessionRejoinInterval) // [wall] the player away past the ready period
	if n := host.membership.GuestCount(); n != 0 {
		t.Fatalf("the host holds %d guest(s) for a join whose ready period ran out", n)
	}
	screen.events <- press
	if next, err := guest.awaitJoinPress(nil); next != nil || err != nil {
		t.Fatalf("a late press was answered %v, %v; want a seat", next, err)
	}
	if !guest.membership.HoldsPlace() || guest.World() != world {
		t.Fatal("a late press did not seat the run it came to, on the world it built")
	}
	id := guest.membership.LocalParticipant()
	if id == 0 {
		t.Fatal("the seated run is no participant")
	}
	if err := guest.membership.StartJoinSession(nil); err != nil {
		t.Fatalf("join gate: %v", err)
	}
	// The host links the participant its offer assigned, so a run that took another
	// identity than its offer's is not the one linked.
	if linked := portOf(t, host).Connected(id); !linked || host.membership.GuestCount() != 1 {
		t.Fatalf("after its gate the host holds %d guest(s), linked to participant %d %v", host.membership.GuestCount(), id, linked)
	}
}

// TestAPressIntoAGameItsSessionReplacedJoinsTheNewOne: a press that finds its session
// playing another game than the one this run built gives back the place its seat was
// assigned, and the run that follows joins that game seated at once, the press taken,
// with the bots this run was to seat.
func TestAPressIntoAGameItsSessionReplacedJoinsTheNewOne(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	host := hostingRun(t, 0x3034, 0)
	guest, screen := waitingJoiner(t, host.HostAddr(), "roam")
	// The session's next game, crossing at the playout lead while this run waits.
	was := host.JoinAnchor().Anchor.Session
	host.World().RunSafe(func() { mode.ExecuteCommand(host.Context(), "n") })
	waitUntil(t, func() bool { return host.JoinAnchor().Anchor.Session != was }, "the host's next game")

	screen.events <- press
	next, err := guest.awaitJoinPress(nil)
	if err != nil || next == nil || !next.Rejoin || next.Press || !slices.Equal(next.Bots, []string{"roam"}) {
		t.Fatalf("a press into a replaced game was answered %+v, %v; want a rejoin, pressed, with its bot", next, err)
	}
	if guest.membership.HoldsPlace() {
		t.Fatal("a press into a replaced game seated its run")
	}
	deadline := time.Now().Add(parameter.NetworkJoinReadyTimeout) // [wall] the host's handshake ends off this goroutine
	for host.membership.GuestCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(parameter.SessionRejoinInterval) // [wall]
	}
	if n := host.membership.GuestCount(); n != 0 {
		t.Fatalf("the host holds %d guest(s) for a seat its joiner turned down", n)
	}
}

// TestARetriedOpenDialsAfresh: a :join whose seat is refused for a moment once its
// world is built opens again on a dial of its own, as the dial the run before handed
// over went with the attempt refused on it.
func TestARetriedOpenDialsAfresh(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	// A full session a player left: the seat finds the Wraith giving up its place,
	// and a retry a moment later finds that place free.
	host := awayGuest(t).host
	cfg := joinerConfig(host.HostAddr(), false)
	cfg.Normalize()
	d, err := membership.DialJoin(cfg.Membership, cfg.wiring.membership, cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	cfg.wiring.dialled = d

	a, err := openRun(cfg, false)
	if err != nil {
		t.Fatalf("a :join refused for a moment at its seat did not open: %v", err)
	}
	t.Cleanup(a.Close)
	if !a.membership.HoldsPlace() || a.membership.LocalParticipant() == 0 {
		t.Fatalf("the reopened run is seated %v as participant %d", a.membership.HoldsPlace(), a.membership.LocalParticipant())
	}
}

// TestAJoinerLandsInAGameWaitingForItsHostsStart: a joiner arriving while its host's
// new game waits for the host's press waits for it too, as that rather than as a
// pause, and starts at the host's press with nothing said.
func TestAJoinerLandsInAGameWaitingForItsHostsStart(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x3033
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	host.Context().WaitStart.Store(true)
	mode.ExecuteCommand(host.Context(), "n")
	host.Settle()
	for range parameter.NetworkBarrierMaxDelayTicks + 4 {
		host.Tick(1) // a session's new game crosses at the playout lead
	}
	if !host.Context().TimeCtl.Holds(engine.HoldStart) {
		t.Fatal("the host's new game does not wait for its press")
	}
	at := host.Position()

	stopTicking := tickInBackground(host)
	guest, _ := mustSocketJoiner(t, host.HostAddr(), seed, 120, 40)
	stopTicking()
	if p := guest.Position(); !guest.Context().TimeCtl.Holds(engine.HoldStart) || p.Run != at.Run || p.Tick != at.Tick {
		t.Fatalf("the joiner waits %v at %+v, want waiting at the host's %+v",
			guest.Context().TimeCtl.Holds(engine.HoldStart), p, at)
	}
	if got := guest.Context().GetStatusMessage(); got == "Paused by the host" {
		t.Fatalf("the joiner of a game waiting to start was told %q", got)
	}
	assertSharedParity(t, host, guest, 0)

	if !startPress(host) {
		t.Fatal("the host's press did not start its game")
	}
	waitForRosterPair(t, host, guest)
	if guest.Context().TimeCtl.Held() || guest.Position().Tick <= at.Tick {
		t.Fatalf("after the host's press the joiner holds %v at tick %d", guest.Context().TimeCtl.Held(), guest.Position().Tick)
	}
	if got := guest.Context().GetStatusMessage(); got == "Resumed by the host" || got == "Paused by the host" {
		t.Fatalf("the joiner was told %q of a start", got)
	}
	settleCorrections(t, host, guest)
	alignTicks(t, host, guest)
	assertSharedParity(t, host, guest, 1)
}

// TestAPreviewRefusesOnlyWhatItsSeatWould: a preview allocates nothing and settles
// nothing a seat would. A full session refuses it at once, with no place to give; a
// full one a player left previews, for a seat to take that place back or have it
// given up, and neither is begun until then.
func TestAPreviewRefusesOnlyWhatItsSeatWould(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	preview := func(addr string, keys ...network.Key) (network.SessionOffer, error) {
		cfg := network.DebugConfig(link.RolePeer, "")
		cfg.Keys = keys
		pending, o, err := network.PreviewSession(addr, cfg)
		if err == nil {
			_ = pending.Close()
		}
		return o, err
	}
	full := mustHosting(t, Membership{Participants: 2})
	seated, _ := dialSession(t, full.HostAddr())
	defer seated.Close()
	if _, err := preview(full.HostAddr()); !errors.Is(err, &network.Refusal{Code: network.RefusalFull}) {
		t.Fatalf("a full session previewed a newcomer with %v, want a full session's refusal", err)
	}

	a := awayGuest(t)
	a.stop() // a yield begun is the next tick's to act on
	before := a.host.membership.GuestCount()
	for _, keys := range [][]network.Key{nil, {a.key}} {
		o, err := preview(a.host.HostAddr(), keys...)
		if err != nil || o.ValidatePreview() != nil {
			t.Fatalf("a full session a player left previewed %d kept keys with %+v, %v", len(keys), o, err)
		}
	}
	var dismiss uint32
	var kept bool
	a.host.World().RunSafe(func() {
		r := a.host.world.Resources.Network
		dismiss, kept = r.DismissWraith(), r.WraithKept(uint32(a.id))
	})
	if after := a.host.membership.GuestCount(); after != before || dismiss != 0 || kept {
		t.Fatalf("a preview began a seat's work: %d guests of %d, participant %d dismissed, the place held for a return %t",
			after, before, dismiss, kept)
	}
	if !wraithIn(a.host, a.slot) {
		t.Fatal("a preview moved the Wraith that keeps the player's place")
	}
}

// TestOnlyATypingOrMovingKeyOrAClickSeatsAJoin: the press that seats a join is one
// the page starts a game on; a key reached for out of habit, a chord, a release, a
// wheel or a click with the mouse off seats nobody.
func TestOnlyATypingOrMovingKeyOrAClickSeatsAJoin(t *testing.T) {
	t.Parallel()
	key := func(k terminal.Key, mods terminal.Modifier, action terminal.KeyAction) terminal.Event {
		return terminal.Event{Type: terminal.EventKey, Key: k, Rune: 'j', Modifiers: mods, Action: action}
	}
	click := func(b terminal.MouseButton) terminal.Event {
		return terminal.Event{Type: terminal.EventMouse, MouseBtn: b, MouseAction: terminal.MouseActionPress}
	}
	for _, c := range []struct {
		name     string
		ev       terminal.Event
		mouseOff bool
		want     bool
	}{
		{"a letter", key(terminal.KeyRune, 0, terminal.KeyPress), false, true},
		{"enter", key(terminal.KeyEnter, 0, terminal.KeyPress), false, true},
		{"space", key(terminal.KeySpace, 0, terminal.KeyPress), false, true},
		{"backspace", key(terminal.KeyBackspace, 0, terminal.KeyPress), false, true},
		{"an arrow", key(terminal.KeyLeft, 0, terminal.KeyPress), false, true},
		{"insert", key(terminal.KeyInsert, 0, terminal.KeyPress), false, true},
		{"a shifted letter", key(terminal.KeyRune, terminal.ModShift, terminal.KeyPress), false, true},
		{"escape", key(terminal.KeyEscape, 0, terminal.KeyPress), false, false},
		{"tab", key(terminal.KeyTab, 0, terminal.KeyPress), false, false},
		{"a function key", key(terminal.KeyF1, 0, terminal.KeyPress), false, false},
		{"a control key", key(terminal.KeyCtrlA, 0, terminal.KeyPress), false, false},
		{"a ctrl chord", key(terminal.KeyRune, terminal.ModCtrl, terminal.KeyPress), false, false},
		{"an alt chord", key(terminal.KeyRune, terminal.ModAlt, terminal.KeyPress), false, false},
		{"a repeat", key(terminal.KeyRune, 0, terminal.KeyRepeat), false, false},
		{"a release", key(terminal.KeyRune, 0, terminal.KeyRelease), false, false},
		{"a left click", click(terminal.MouseBtnLeft), false, true},
		{"a right click", click(terminal.MouseBtnRight), false, true},
		{"a click with the mouse off", click(terminal.MouseBtnLeft), true, false},
		{"a wheel", click(terminal.MouseBtnWheelUp), false, false},
		{"a resize", terminal.Event{Type: terminal.EventResize, Width: 80, Height: 24}, false, false},
	} {
		if got := joinPress(c.ev, c.mouseOff); got != c.want {
			t.Errorf("%s seats a join: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestOnlyAJoinItsPlayerStartedWaitsForTheirPress: the dial a player starts, a :join
// here, waits for their press; a run following its session to a new game, or one
// whose player already pressed, does not.
func TestOnlyAJoinItsPlayerStartedWaitsForTheirPress(t *testing.T) {
	t.Parallel()
	base := runConfig{Config: Config{Mode: ModePlay, Resources: resource.Options{Embedded: true}}}
	joined, solo := base.restarted(&restartRequest{Join: "127.0.0.1:7777", Press: true})
	if !joined.wiring.membership.JoinPress || joined.Membership.JoinAddress == "" || solo == nil {
		t.Fatalf("a :join asks for its player's press %v, joining %q",
			joined.wiring.membership.JoinPress, joined.Membership.JoinAddress)
	}
	provided := &resource.Scenario{Name: "kept"}
	rejoined, _ := joined.restarted(&restartRequest{Rejoin: true, provided: provided})
	if rejoined.wiring.membership.JoinPress || rejoined.Resources.Provided != provided {
		t.Fatalf("a rejoin asks for a press %v and joins with scenario %v",
			rejoined.wiring.membership.JoinPress, rejoined.Resources.Provided)
	}
	if again, _ := rejoined.restarted(&restartRequest{Rejoin: true}); again.Resources.Provided != nil {
		t.Fatal("a scenario one join held outlived the run it was handed to")
	}
}

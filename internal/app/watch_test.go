package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app/internal/membership"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// mustWatching is a driven run watching the session at addr, as -spectate starts one.
// Its host need not tick meanwhile: a viewer's world is read between two ticks.
func mustWatching(t *testing.T, addr string) *App {
	t.Helper()
	v, err := scriptedRun(Config{Mode: ModeHeadless, Width: 120, Height: 40,
		Membership: Membership{JoinAddress: addr, Watch: true}}, socketWait)
	if err != nil {
		t.Fatalf("watch %s: %v", addr, err)
	}
	t.Cleanup(v.Close)
	return v
}

// hostedSession is a host and players-1 guests in its session over real sockets,
// the host first, each ticked by the caller.
func hostedSession(t *testing.T, players int) []*App {
	t.Helper()
	apps := []*App{mustHosting(t, Membership{})}
	for len(apps) < players {
		var stops []func()
		for _, a := range apps {
			stops = append(stops, tickInBackground(a))
		}
		guest, _ := mustSocketJoiner(t, apps[0].HostAddr(), fixtureSeed, 120, 40)
		for _, stop := range stops {
			stop()
		}
		apps = append(apps, guest)
	}
	for deadline := time.Now().Add(socketWait); ; play(1, apps...) { // [wall] a link bound
		seated := true
		for _, a := range apps {
			a.World().RunSafe(func() { seated = seated && a.World().Resources.Player.Count() == players })
		}
		if seated {
			return apps
		}
		if time.Now().After(deadline) {
			t.Fatalf("the session never seated its %d players on each of them", players)
		}
	}
}

// watchedSession is a host, a guest playing in its session and a run watching it.
func watchedSession(t *testing.T) (host, guest, viewer *App) {
	t.Helper()
	apps := hostedSession(t, 2)
	return apps[0], apps[1], mustWatching(t, apps[0].HostAddr())
}

// brawl runs players through ticks of a fight, each moving one tick in three and
// firing one in six as the divergence harness's do, the host publishing on its pump,
// with viewers in step. The players are :god's, so no defeat starts a new game.
func brawl(t *testing.T, rng *vmath.FastRand, ticks int, players []*App, viewers ...*App) {
	t.Helper()
	motions := [...]input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	for range ticks {
		for _, a := range players {
			if rng.Intn(3) == 0 {
				inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
			}
			if rng.Intn(6) == 0 {
				inject(t, a, &input.Intent{Type: input.IntentFireMain, Count: 1})
			}
		}
		play(1, append(slices.Clone(players), viewers...)...)
		_ = players[0].corrections.PublishDue()
	}
}

// play runs a session's players n ticks in step, leaving their link a moment each.
func play(n int, players ...*App) {
	for range n {
		tickAll(players)
		time.Sleep(joinTestTickInterval / 5) // [wall] a link bound, not a game one
	}
}

// followTo ticks viewer until it stands on the tick host stands at. The host must
// stand still meanwhile, which is what sends its feed the ticks a record waits for.
func followTo(t *testing.T, host, viewer *App) {
	t.Helper()
	want := host.Position()
	for deadline := time.Now().Add(socketWait); ; time.Sleep(time.Millisecond) { // [wall] a link bound
		at := viewer.Position()
		if at.Run == want.Run && at.Tick == want.Tick {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the viewer stands at run %d tick %d, its host at run %d tick %d", at.Run, at.Tick, want.Run, want.Tick)
		}
		viewer.Tick(1)
	}
}

// tickUntil ticks a until done reports true, failing with what when it never does.
func tickUntil(t *testing.T, a *App, what string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(socketWait); !done(); time.Sleep(time.Millisecond) { // [wall] a link bound
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		a.Tick(1)
	}
}

// TestAViewerHoldsItsHostsWorldTickForTick: a viewer runs the session's simulation
// from a world its host wrote and the feed of what the host commits, so it stands on
// the host's shared world at every tick both ran. One that begins while a crossing
// is committed and not yet applied applies it with the players, and the world a
// feed period carries finds nothing in a viewer's own to correct, mid-fight.
func TestAViewerHoldsItsHostsWorldTickForTick(t *testing.T) {
	t.Parallel()
	players := hostedSession(t, 2)
	host, guest := players[0], players[1]
	var source uint32
	guest.World().RunSafe(func() { source = guest.World().LocalParticipant() })
	pending := func() (n int) {
		host.World().RunSafe(func() {
			for _, sys := range host.World().Systems() {
				if p, ok := sys.(interface {
					PendingAfter(uint64) []event.WireBatch
				}); ok {
					for _, b := range p.PendingAfter(host.Position().Tick) {
						if b.Source == source {
							n++
						}
					}
				}
			}
		})
		return n
	}
	// A move that reaches the host on the tick it applies at is never held between
	// two ticks, so the guest moves until one is
	moved := localCursorEntity(guest)
	var from component.PositionComponent
	for deadline, step := time.Now().Add(socketWait), 0; ; step++ { // [wall] a link bound
		from = cursorPosition(host, moved)
		inject(t, guest, intentMotion([...]input.MotionOp{input.MotionRight, input.MotionLeft}[step%2], 1))
		for pending() == 0 && cursorPosition(host, moved) == from {
			if time.Now().After(deadline) {
				t.Fatal("the host never held a move of its guest's committed and not yet applied")
			}
			play(1, guest, host)
		}
		if cursorPosition(host, moved) == from {
			break
		}
	}
	first := mustWatching(t, host.HostAddr())
	play(parameter.NetworkBarrierMaxDelayTicks+2, guest, host)
	followTo(t, host, first)
	if at := cursorPosition(first, moved); at == from || at != cursorPosition(host, moved) {
		t.Fatalf("the viewer holds the guest's cursor at %+v, its host at %+v, from %+v", at, cursorPosition(host, moved), from)
	}
	assertSharedParity(t, host, first, 0)

	for _, a := range players {
		injectExCommand(t, a, "god")
	}
	rng := vmath.NewFastRand(7)
	tel := first.glue.Telemetry()
	applied := tel.Applied.Load()
	for fought := 0; tel.Applied.Load() == applied; fought += parameter.FeedRecordTicks {
		if fought > 2*parameter.FeedSyncTicks {
			t.Fatalf("a viewer at tick %d was sent no world to check its own against", first.Position().Tick)
		}
		brawl(t, rng, parameter.FeedRecordTicks, players, first)
	}
	if n := tel.CorrectionEntries.Load(); n != 0 {
		t.Fatalf("a feed period's world moved %d entries of a viewer's own, want none", n)
	}

	stopHost, stopGuest := tickInBackground(host), tickInBackground(guest)
	defer stopHost()
	defer stopGuest()
	second := mustWatching(t, host.HostAddr()) // while the session runs
	stopGuest()
	stopHost()
	brawl(t, rng, 3*parameter.FeedRecordTicks, players, first)
	for i, v := range []*App{first, second} {
		followTo(t, host, v)
		assertSharedParity(t, host, v, i+1)
	}
	if viewers, _ := host.membership.Audience(); viewers != 2 {
		t.Fatalf("the host counts %d viewers, want 2", viewers)
	}
}

// TestAFeedOverAFightReadsItsCost is the feed's reading, run with VIF_FEED=1: a
// session of VIF_FEED_PLAYERS (2) fights for VIF_FEED_TICKS (2400) with a viewer in
// step. It prints the feed's bytes, counted once however many viewers it is copied
// to, beside what the host sent each guest, and each starting point: its size, its
// capture's stall on the host, its encoding, and what it moved of the viewer's world.
func TestAFeedOverAFightReadsItsCost(t *testing.T) {
	if os.Getenv("VIF_FEED") == "" {
		t.Skip("set VIF_FEED=1 to run the feed's reading")
	}
	count := func(key string, fallback int) int {
		if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
			return n
		}
		return fallback
	}
	vlog.Configure(vlog.Config{Dir: t.TempDir(), Level: "debug"})
	path, err := vlog.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vlog.Stop)

	players := hostedSession(t, count("VIF_FEED_PLAYERS", 2))
	host := players[0]
	for _, a := range players {
		injectExCommand(t, a, "god")
	}
	viewer := mustWatching(t, host.HostAddr())
	tel := viewer.glue.Telemetry()
	rng := vmath.NewFastRand(7)
	began, applied, ticks := host.Position().Tick, tel.Applied.Load(), count("VIF_FEED_TICKS", 4*parameter.FeedSyncTicks)
	sent := statOf(host, "network.wire_bytes_out")
	for fought := 0; fought < ticks; fought += parameter.FeedRecordTicks {
		brawl(t, rng, parameter.FeedRecordTicks, players, viewer)
		if n := tel.Applied.Load(); n != applied {
			applied = n
			t.Logf("the world at tick %d moved %d entries, %d entities of the viewer's", tel.CorrectionTick.Load(),
				tel.CorrectionEntries.Load(), tel.CorrectionEntities.Load())
		}
	}
	followTo(t, host, viewer)
	assertSharedParity(t, host, viewer, 0)
	_, totals := host.membership.Audience()
	seconds := (time.Duration(host.Position().Tick-began) * host.World().TickInterval()).Seconds()
	t.Logf("%d players over %.0f s of game time: stream %d bytes (%.0f B/s; %d before compression), starting points %d bytes (%.0f B/s)",
		len(players), seconds, totals.Stream, float64(totals.Stream)/seconds, totals.Plain, totals.Heads, float64(totals.Heads)/seconds)
	t.Logf("the host sent each guest %.0f B/s meanwhile",
		float64(statOf(host, "network.wire_bytes_out")-sent)/seconds/float64(len(players)-1))
	vlog.Stop()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for sc := bufio.NewScanner(f); sc.Scan(); {
		var rec struct {
			Fields struct {
				Msg         string
				Tick, Bytes uint64
				Capture     int64 `json:"capture_us"`
				Encode      int64 `json:"encode_us"`
			}
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Fields.Msg == "feed starting point" {
			t.Logf("starting point at tick %d: %d bytes, captured in %d us on the tick, encoded in %d us off it",
				rec.Fields.Tick, rec.Fields.Bytes, rec.Fields.Capture, rec.Fields.Encode)
		}
	}
}

// TestAViewerIsNobodyInItsSession: a viewer takes no slot and no link of its host's,
// and what it types changes nothing, in its own world or in the session's.
func TestAViewerIsNobodyInItsSession(t *testing.T) {
	t.Parallel()
	host, guest, viewer := watchedSession(t)
	var slot uint8
	var players, participant = 0, uint32(0)
	viewer.World().RunSafe(func() {
		w := viewer.World()
		slot, players, participant = w.Resources.Player.LocalSlot(), w.Resources.Player.Count(), w.LocalParticipant()
	})
	if slot != parameter.NoPlayerSlot || participant != 0 || players != 2 {
		t.Fatalf("the viewer is participant %d on slot %d among %d players; want nobody, watching 2", participant, slot, players)
	}
	if peers := portOf(t, host).PeerCount(); peers != 1 {
		t.Fatalf("the host has %d links, want its guest's alone", peers)
	}
	if got := host.World().Resources.Player.Count(); got != 2 {
		t.Fatalf("the host seats %d players, want 2", got)
	}

	inject(t, viewer, intentMotion(input.MotionRight, 3), &input.Intent{Type: input.IntentFireMain, Count: 1})
	mode.ExecuteCommand(viewer.Context(), "pause")
	viewer.Settle()
	if got := viewer.Context().GetStatusMessage(); got != "Unavailable while spectating: :pause" {
		t.Fatalf("a viewer's :pause was told %q", got)
	}
	play(parameter.NetworkBarrierMaxDelayTicks+2, guest, host)
	if host.Context().TimeCtl.Held() {
		t.Fatal("a viewer held its host's session")
	}
	followTo(t, host, viewer)
	assertSharedParity(t, host, viewer, 0)
}

// TestAViewerFollowsANewGame: the host's :n reaches a viewer as the reset that
// opens the next run, which it waits in as the players do until the host's press;
// a viewer that begins during the wait waits with them. The new game's world is
// sent whole, and finds nothing in the world a viewer reset into to correct.
func TestAViewerFollowsANewGame(t *testing.T) {
	t.Parallel()
	host, guest, first := watchedSession(t)
	tel := first.glue.Telemetry()
	applied := tel.Applied.Load()
	host.Context().WaitStart.Store(true)
	run := host.Position().Run
	mode.ExecuteCommand(host.Context(), "n")
	host.Settle()
	play(parameter.NetworkBarrierMaxDelayTicks+4, guest, host)
	if at := host.Position(); at.Run != run+1 || !host.Context().TimeCtl.Holds(engine.HoldStart) {
		t.Fatalf("the host stands at %+v, want waiting in run %d", at, run+1)
	}
	second := mustWatching(t, host.HostAddr())
	viewers := []*App{first, second}
	for i, v := range viewers {
		followTo(t, host, v)
		if !v.Context().TimeCtl.Holds(engine.HoldStart) {
			t.Fatalf("viewer %d runs a game its host has not started", i+1)
		}
		assertSharedParity(t, host, v, i)
	}
	tickUntil(t, first, "a viewer was never sent the new game's world", func() bool { return tel.Applied.Load() > applied })
	if n := tel.CorrectionEntries.Load(); n != 0 {
		t.Fatalf("the new game's world moved %d entries of a viewer's own, want none", n)
	}
	if !startPress(host) {
		t.Fatal("the host's press did not start its game")
	}
	play(2*parameter.FeedRecordTicks, guest, host)
	for i, v := range viewers {
		followTo(t, host, v)
		if got := v.Context().GetStatusMessage(); got == "Resumed by the host" {
			t.Fatalf("viewer %d was told %q of a start", i+1, got)
		}
		assertSharedParity(t, host, v, len(viewers)+i)
	}
}

// TestAHoldReachesAViewer: the host's :pause stops a viewer on the tick it stops the
// players, a viewer that begins meanwhile begins held, and both run on at its end.
func TestAHoldReachesAViewer(t *testing.T) {
	t.Parallel()
	host, guest, first := watchedSession(t)
	mode.ExecuteCommand(host.Context(), "pause")
	host.Settle()
	play(parameter.NetworkBarrierMaxDelayTicks+2, guest, host)
	if !host.Context().TimeCtl.Held() {
		t.Fatal("the host's :pause held nothing")
	}
	viewers := []*App{first, mustWatching(t, host.HostAddr())}
	for i, v := range viewers {
		followTo(t, host, v)
		v.Tick(4)
		if at, want := v.Position().Tick, host.Position().Tick; !v.Context().TimeCtl.Held() || at != want {
			t.Fatalf("viewer %d: held %v at tick %d, want held at its host's %d", i+1, v.Context().TimeCtl.Held(), at, want)
		}
		if got := v.Context().GetStatusMessage(); got != "Paused by the host" {
			t.Fatalf("viewer %d was told %q", i+1, got)
		}
	}
	mode.ExecuteCommand(host.Context(), "pause off")
	host.Settle()
	play(2*parameter.FeedRecordTicks, guest, host)
	for i, v := range viewers {
		followTo(t, host, v)
		if got := v.Context().GetStatusMessage(); v.Context().TimeCtl.Held() || got != "Resumed by the host" {
			t.Fatalf("viewer %d: held %v, told %q after the hold's end", i+1, v.Context().TimeCtl.Held(), got)
		}
		assertSharedParity(t, host, v, i)
	}
}

// TestAViewersClockKeepsStepWithItsFeed: a person's viewer runs on its own clock,
// paced to keep a few of its feed's ticks in hand. Beside a host at the game's rate
// it stays within the window it paces over, it stops where a host that stops stands,
// and the session's end starts it a game of its own.
func TestAViewersClockKeepsStepWithItsFeed(t *testing.T) {
	// Not parallel: this measures a real socket against the wall clock.
	host := mustHosting(t, Membership{})
	stop, halted := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(halted)
		tick := time.NewTicker(host.World().TickInterval()) // [wall] the game's own rate
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				host.Tick(1)
			}
		}
	}()
	halt := sync.OnceFunc(func() { close(stop); <-halted })
	t.Cleanup(halt)

	screen := &pressPresentation{framePresentation: framePresentation{w: 120, h: 40}, events: make(chan terminal.Event, 1)}
	viewer, err := newSessionApp(runConfig{Config: Config{Mode: ModePlay, Resources: resource.Options{Embedded: true},
		Membership: Membership{JoinAddress: host.HostAddr(), Watch: true}}, host: Host{Presentation: screen}})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	t.Cleanup(viewer.Close)
	type ended struct {
		next *restartRequest
		err  error
	}
	done := make(chan ended, 1)
	go func() {
		next, err := viewer.Loop()
		done <- ended{next, err}
	}()

	const settle, watched = 20, 40 // ticks: the catch-up's wobble, then the pace measured
	behind := func() int64 { return int64(host.Position().Tick) - int64(viewer.Position().Tick) }
	interval := host.World().TickInterval()
	time.Sleep(settle * interval) // [wall]
	from := viewer.Position().Tick
	for range watched {
		if lag := behind(); lag < 0 || lag > parameter.FeedPaceWindow {
			t.Fatalf("the viewer runs %d ticks behind its host, want within %d", lag, parameter.FeedPaceWindow)
		}
		time.Sleep(interval) // [wall]
	}
	if ran := viewer.Position().Tick - from; ran < watched-parameter.FeedPaceWindow {
		t.Fatalf("the viewer ran %d ticks while its host ran %d", ran, watched)
	}

	halt()
	for deadline := time.Now().Add(socketWait); behind() != 0; time.Sleep(interval) { // [wall] a link bound
		if time.Now().After(deadline) {
			t.Fatalf("the viewer stands %d ticks behind a host that stopped", behind())
		}
	}
	host.Close()
	select {
	case end := <-done:
		if end.err != nil || end.next == nil || end.next.Rejoin ||
			end.next.Notice != "The host closed the session; started a local game" {
			t.Fatalf("the session's end left the viewer's loop with %+v, %v", end.next, end.err)
		}
	case <-time.After(socketWait): // [wall] a link bound
		t.Fatal("the viewer's loop outlived its session")
	}
}

// TestAViewerLeavesWithItsFeed: a session its host closed ends a driven viewer and
// starts a person's a game of its own, saying why; one its host restarts is watched
// again where it reopens.
func TestAViewerLeavesWithItsFeed(t *testing.T) {
	t.Parallel()
	person := func(host *App) *App {
		v := mustWatching(t, host.HostAddr())
		v.World().RunSafe(func() { v.cfg.Mode = ModePlay }) // a replaced run is its loop's to serve
		return v
	}
	replaced := func(v *App) *restartRequest {
		tickUntil(t, v, "the viewer's run was never replaced", func() bool { return v.restart.Load() != nil })
		return v.restart.Load()
	}

	host := mustHosting(t, Membership{})
	driven, played := mustWatching(t, host.HostAddr()), person(host)
	host.Close()
	tickUntil(t, driven, "a driven viewer outlived its session", driven.membership.Dismissed)
	if req := replaced(played); req.Rejoin || req.Notice != "The host closed the session; started a local game" {
		t.Fatalf("a closed session replaced its viewer's run with %+v", req.RestartAsk)
	}

	host = mustHosting(t, Membership{})
	played = person(host)
	host.restart.Store(&restartRequest{RestartAsk: membership.RestartAsk{Host: host.HostAddr()}})
	host.Close()
	if req := replaced(played); !req.Rejoin || req.Notice != "" {
		t.Fatalf("a restarting session replaced its viewer's run with %+v", req.RestartAsk)
	}
	next, solo := played.cfg.restarted(played.restart.Load())
	if !next.Membership.Watch || next.Membership.JoinAddress != played.cfg.Membership.JoinAddress ||
		solo == nil || solo.Membership.Watch || solo.Membership.JoinAddress != "" {
		t.Fatalf("the viewer's next run is %+v, falling back to %+v; want watching again, or alone", next.Membership, solo)
	}
}

// TestAnAudienceHasACeiling: a host feeds as many viewers as it says and refuses the
// next until one leaves; one that takes none says so to a dial no retry helps.
func TestAnAudienceHasACeiling(t *testing.T) {
	t.Parallel()
	watch := func(host *App) error {
		cfg := network.DebugConfig(link.RolePeer, "")
		cfg.Keys = []network.Key{network.NewKey()}
		pending, _, err := network.WatchSession(host.HostAddr(), cfg)
		if err == nil {
			_ = pending.Close()
		}
		return err
	}
	host := mustHosting(t, Membership{Spectators: 1})
	viewer := mustWatching(t, host.HostAddr())
	if err := watch(host); !errors.Is(err, &network.Refusal{Code: network.RefusalAudience}) || network.RetryOf(err) != network.RetryLater {
		t.Fatalf("a second viewer of an audience of one was told %v", err)
	}
	if line := ex(host, "session"); !strings.Contains(line, ", 1 spectator(s)") {
		t.Fatalf(":session on a host one spectator watches reads %q", line)
	}
	viewer.Close()
	for deadline := time.Now().Add(socketWait); ; time.Sleep(time.Millisecond) { // [wall] a link bound
		if n, _ := host.membership.Audience(); n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the host still counts a viewer that left")
		}
	}
	if err := watch(host); err != nil {
		t.Fatalf("a viewer was refused the place another left: %v", err)
	}

	closed := mustHosting(t, Membership{Spectators: -1})
	if err := watch(closed); err == nil || !strings.Contains(err.Error(), "takes no spectators") || network.RetryOf(err) != network.RetryNever {
		t.Fatalf("a session that takes no spectators told one %v", err)
	}
}

// TestASpectatorLooksFromAPlayerWhoPlays: a spectator's view is one player's: the
// slot it began at, the one a press on a player, its keys or :spectate <slot> pick,
// and the next one playing once that player leaves. None of it plays anything.
func TestASpectatorLooksFromAPlayerWhoPlays(t *testing.T) {
	t.Parallel()
	apps := hostedSession(t, 2)
	host, guest := apps[0], apps[1]
	// Half the map's width, so the view holds one of two players at its ends
	viewer, err := scriptedRun(Config{Mode: ModeHeadless, Width: 60, Height: 20,
		Membership: Membership{JoinAddress: host.HostAddr(), Watch: true, Follow: 1}}, socketWait)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	t.Cleanup(viewer.Close)
	cell := func(slot uint8) (at component.PositionComponent, shown bool) {
		w := viewer.World()
		at, _ = w.Positions.GetPosition(w.Resources.Player.Slot(slot))
		_, _, shown = w.Resources.Config.MapToViewport(at.X, at.Y)
		return at, shown
	}
	looksFrom := func(slot uint8, alone bool, after string) {
		t.Helper()
		viewer.World().RunSafe(func() {
			_, own := cell(slot)
			_, other := cell(1 - slot)
			if got := viewer.World().Resources.Player.ViewSlot(); got != slot || !own || alone && other {
				t.Fatalf("%s the spectator looks from slot %X, slot %X in view %t and the other %t; want %X alone in view",
					after, got, slot, own, other, slot)
			}
		})
	}
	looksFrom(1, false, "started at slot 1,")

	inject(t, host, intentMotion(input.MotionLeft, 3))
	play(parameter.NetworkBarrierMaxDelayTicks+2, host, guest)
	followTo(t, host, viewer)
	var at component.PositionComponent
	viewer.World().RunSafe(func() { at, _ = cell(0) })
	inject(t, viewer, &input.Intent{Type: input.IntentMouseLeftDown, X: at.X, Y: at.Y, MapCell: true, Count: 1})
	looksFrom(0, false, "after a press on slot 0's player")

	for range 30 { // the players part to the map's two ends
		inject(t, host, intentMotion(input.MotionLeft, 3))
		inject(t, guest, intentMotion(input.MotionRight, 3))
		play(1, host, guest)
	}
	play(parameter.NetworkBarrierMaxDelayTicks+2, host, guest)
	followTo(t, host, viewer)
	looksFrom(0, true, "with the players apart")
	inject(t, viewer, intentMotion(input.MotionRight, 1))
	looksFrom(1, true, "after the key for the next player")
	for _, tc := range []struct{ line, told string }{
		{"spectate 0", "Spectating slot 0"}, {"spectate 5", "Nobody plays on slot 5"}, {"spectate 1", "Spectating slot 1"},
	} {
		if got := ex(viewer, tc.line); got != tc.told {
			t.Fatalf(":%s was told %q, want %q", tc.line, got, tc.told)
		}
	}
	if line := ex(viewer, "session"); !strings.Contains(line, "2 player(s), following slot 1;") {
		t.Fatalf(":session on a spectator reads %q", line)
	}
	looksFrom(1, true, "after :spectate 1")

	guest.Close()
	waitForHostWraith(t, host)
	play(1, host) // the view moves on the tick after its player's cursor turned Wraith
	followTo(t, host, viewer)
	looksFrom(0, true, "after slot 1's player left")
	assertSharedParity(t, host, viewer, 0)
}

// TestSpectatingReplacesARunOnceItsDialIsTaken: :spectate dials while the run plays
// on and replaces it only once the session took the dial, with a viewer looking from
// the slot named that brings no bots and keeps no journal; a refusal leaves the game.
func TestSpectatingReplacesARunOnceItsDialIsTaken(t *testing.T) {
	t.Parallel()
	line := runConfig{Config: Config{Seed: fixtureSeed, Width: 120, Height: 40,
		Resources: resource.Options{Embedded: true}, Membership: Membership{Bots: []string{"default"}}}}
	solo, err := NewHeadless(line.Config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(solo.Close)
	tickUntilCursor(t, solo)
	solo.World().RunSafe(func() { solo.cfg.Mode = ModePlay }) // :spectate serves Run's loop

	closed := mustHosting(t, Membership{Spectators: -1})
	ex(solo, "spectate "+closed.HostAddr())
	waitUntil(t, func() bool { return strings.HasPrefix(solo.Context().GetStatusMessage(), "Spectate failed: ") },
		"the refused dial reported; status "+solo.Context().GetStatusMessage())
	if got := solo.Context().GetStatusMessage(); !strings.Contains(got, "this session takes no spectators") || solo.restart.Load() != nil {
		t.Fatalf("a refused spectator was told %q and its run replaced with %+v", got, solo.restart.Load())
	}

	host := mustHosting(t, Membership{})
	ex(solo, "spectate "+host.HostAddr()+" 1")
	waitUntil(t, func() bool { return solo.restart.Load() != nil }, "the spectator's dial taken; status "+solo.Context().GetStatusMessage())
	req := solo.restart.Load()
	defer req.Dialled.Abandon(errors.New("the test is over"))
	line.Journal = true // as a run that keeps one is replaced
	next, _ := line.restarted(req)
	if m := next.Membership; !m.Watch || m.Follow != 1 || m.JoinAddress != host.HostAddr() || len(m.Bots) != 0 || next.Journal {
		t.Fatalf("the run that spectates is %+v, journal %t; want a viewer at slot 1 with no bots or journal", m, next.Journal)
	}
}

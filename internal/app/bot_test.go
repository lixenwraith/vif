package app

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
)

// playBot runs one embedded graph solo for n ticks on a fresh headless instance,
// journaling into capture when one is given, and returns the instance, still open.
// The graph is read from the binary rather than resolved, so no installed copy
// stands in for it.
func playBot(t *testing.T, name string, seed uint64, n int, capture *journal.Capture) (*App, bot.Stats) {
	t.Helper()
	data, err := fs.ReadFile(asset.DefaultBots, name+".toml")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := bot.ParseGraph(name, data)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Seed: seed, Width: 120, Height: 40, Resources: resource.Options{Embedded: true}}
	if capture != nil {
		cfg.Journal, cfg.JournalSink = true, capture
	}
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	d, err := bot.NewDriver(a, a.ctx, graph, a.Seed(), a.localParticipant())
	if err != nil {
		t.Fatal(err)
	}
	for range n {
		if more, err := d.Step(); !more || err != nil {
			t.Fatalf("%s: more %v, err %v", name, more, err)
		}
	}
	return a, d.Stats()
}

// TestAMapCellPointerReachesOnlyWhatTheViewportShows: a bot's pointer names map
// cells, and like a mouse it moves the cursor onto a cell the viewport shows and
// onto no other.
func TestAMapCellPointerReachesOnlyWhatTheViewportShows(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 100, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	if !a.SetupLevel(400, 120, true, false) {
		t.Fatal("a solo run refused a map larger than its viewport")
	}
	tickUntilCursor(t, a)

	var shownX, shownY, hiddenX, hiddenY int
	a.World().RunSafe(func() {
		cfg := a.World().Resources.Config
		pos, _ := a.World().LocalCursor()
		shownX, shownY = pos.X+1, pos.Y
		hiddenX, hiddenY = cfg.CameraX+cfg.ViewportWidth+5, pos.Y
		if _, _, ok := cfg.MapToViewport(shownX, shownY); !ok {
			t.Fatalf("the cell beside the cursor, %d,%d, is off screen", shownX, shownY)
		}
		if _, _, ok := cfg.MapToViewport(hiddenX, hiddenY); ok || hiddenX >= cfg.MapWidth {
			t.Fatalf("%d,%d is not a map cell off screen", hiddenX, hiddenY)
		}
	})
	point := func(x, y int) (atX, atY int) {
		a.handleIntent(&input.Intent{Type: input.IntentMouseMove, X: x, Y: y, MapCell: true})
		a.World().RunSafe(func() {
			pos, _ := a.World().LocalCursor()
			atX, atY = pos.X, pos.Y
		})
		return atX, atY
	}

	if x, y := point(hiddenX, hiddenY); x == hiddenX && y == hiddenY {
		t.Fatalf("the pointer reached %d,%d, which the viewport does not show", x, y)
	}
	if x, y := point(shownX, shownY); x != shownX || y != shownY {
		t.Fatalf("the pointer named %d,%d and the cursor is on %d,%d", shownX, shownY, x, y)
	}
}

// TestASoloBotRunIsAPureFunctionOfItsSeed: a graph draws from its own stream and reads
// a deterministic world, so one seed journals one run, for every shipped graph.
func TestASoloBotRunIsAPureFunctionOfItsSeed(t *testing.T) {
	t.Parallel()
	for _, name := range shippedGraphs(t) {
		var runs [2][]event.JournalRecord
		for i := range runs {
			capture := journal.NewCapture()
			a, _ := playBot(t, name, fixtureSeed, 600, capture)
			a.Close()
			runs[i] = capture.Records()
		}
		played := slices.ContainsFunc(runs[0], func(r event.JournalRecord) bool { return r.Origin == event.OriginInput })
		if !played {
			t.Fatalf("%s journaled no input, so its equality proves nothing", name)
		}
		if !slices.Equal(runs[0], runs[1]) {
			t.Fatalf("%s: one seed journaled %d and %d records that differ", name, len(runs[0]), len(runs[1]))
		}
	}
}

// shippedGraphs names every embedded bot graph.
func shippedGraphs(t *testing.T) []string {
	t.Helper()
	files, err := fs.Glob(asset.DefaultBots, "*.toml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no shipped graphs: %v", err)
	}
	for i, file := range files {
		files[i] = strings.TrimSuffix(file, ".toml")
	}
	return files
}

// TestEveryShippedGraphPlays: each embedded graph loads, acts, and never queues more
// than its rate releases.
func TestEveryShippedGraphPlays(t *testing.T) {
	t.Parallel()
	for _, name := range shippedGraphs(t) {
		a, st := playBot(t, name, fixtureSeed, 1200, nil)
		a.Close()
		if st.Injected == 0 || st.Dropped != 0 {
			t.Errorf("%s: injected %d, dropped %d", name, st.Injected, st.Dropped)
		}
	}
}

// driveBot plays graph on the run cfg describes, as vif -bot does, on its own
// goroutine and paced, and returns the run once the driver is on it, with its stop.
func driveBot(t *testing.T, cfg Config, graph string, hold func() bool) (*App, func()) {
	t.Helper()
	cfg.Seed, cfg.Width, cfg.Height = fixtureSeed, BotWidth, BotHeight
	cfg.Resources = resource.Options{Embedded: true}
	g, err := loadBotGraph(cfg.Resources, graph)
	if err != nil {
		t.Fatal(err)
	}
	stop, built, done := make(chan os.Signal), make(chan *App, 1), make(chan error, 1)
	go func() {
		_, err := drive(cfg, "bot", graph, stop, hold, func(a *App) (pacedSource, error) {
			d, err := bot.NewDriver(a, a.ctx, g, a.Seed(), a.localParticipant())
			built <- a
			return botSource{d}, err
		})
		done <- err
	}()
	var a *App
	select {
	case a = <-built:
	case err := <-done:
		t.Fatalf("%s did not start: %v", graph, err)
	case <-time.After(socketWait):
		t.Fatalf("%s did not start within %s", graph, socketWait)
	}
	var once sync.Once
	halt := func() {
		once.Do(func() {
			close(stop)
			if err := <-done; err != nil {
				t.Errorf("%s: %v", graph, err)
			}
		})
	}
	t.Cleanup(halt)
	return a, halt
}

// waitForCursors waits for a running instance's roster to hold n cursors.
func waitForCursors(t *testing.T, a *App, n int) {
	t.Helper()
	var held int
	for deadline := time.Now().Add(socketWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		a.World().RunSafe(func() { held = a.World().Resources.Player.Count() })
		if held == n {
			return
		}
	}
	a.world.RunSafe(func() {
		for slot := range parameter.MaxPlayers {
			e := a.world.Resources.Player.Slot(uint8(slot))
			if e != 0 {
				c, ok := a.world.Components.Cursor.GetComponent(e)
				t.Logf("slot=%d entity=%v cursor=%+v exists=%v", slot, e, c, ok)
			}
		}
	})
	t.Fatalf("the roster holds %d cursors, want %d; %+v; %s; bots %s; status %q", held, n, a.Position(), a.SessionSummary(),
		a.seatsSummary()+fmt.Sprint((instance{a}).WorldRoster()), a.ctx.GetStatusMessage())
}

// TestAHoldersBotsComeAndGoAsParticipants: a run with bots and no session hosts one
// on loopback, its bots join it as participants, one dropped departs, and the rest
// leave with the run.
func TestAHoldersBotsComeAndGoAsParticipants(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	holder, stop := driveBot(t, Config{Bots: []string{"roam", "patrol"}}, "patrol", nil)
	if addr := holder.HostAddr(); !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("a run with bots hosts on %q, not on loopback", addr)
	}
	waitForCursors(t, holder, 3)
	// A seat learns its slot from its own gate, which can finish after the holder
	// has applied its arrival.
	for err := holder.dropSeat(1); err != nil; err = holder.dropSeat(1) {
		if len(holder.seatGraphs()) < 2 {
			t.Fatalf("a seat left before it was dropped: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitForCursors(t, holder, 2)
	stop()
	if left := holder.seatGraphs(); len(left) != 0 {
		t.Fatalf("seats %v outlived their holder", left)
	}
}

func TestBotRemovalDoesNotFollowReusedRosterIdentities(t *testing.T) {
	a := &App{}
	old := &seat{number: 1, graph: &bot.Graph{Name: "default"}, stop: make(chan os.Signal)}
	old.id.Store(2)
	old.slot.Store(1)
	a.seats = []*seat{old}
	selected := a.botSeats()[0]
	replacement := &seat{number: 2, graph: old.graph, stop: make(chan os.Signal)}
	replacement.id.Store(2)
	replacement.slot.Store(1)
	a.seats = []*seat{replacement}
	if err := a.removeSeat(selected.ID); err == nil || a.botSeats()[0].Stopping {
		t.Fatal("stale selection removed a replacement with the same participant and slot")
	}
	if err := a.removeSeat(replacement.number); err != nil || !a.botSeats()[0].Stopping {
		t.Fatal("current bot could not be removed")
	}
	pending := &seat{number: 3, graph: old.graph, stop: make(chan os.Signal)}
	a.seats = []*seat{pending}
	if !a.botSeats()[0].Joining {
		t.Fatal("pending bot appears admitted")
	}
	if err := a.removeSeat(pending.number); err != nil || !a.botSeats()[0].Stopping {
		t.Fatal("pending bot could not be cancelled")
	}
}

// TestAnAuthorityPausesOnlyWhileItsOtherParticipantsAreItsBots: its own bots stand
// still with its clock, and anybody else would run on without it, so an arrival
// ends the pause and a session with a stranger in it refuses one.
func TestAnAuthorityPausesOnlyWhileItsOtherParticipantsAreItsBots(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	holder, _ := driveBot(t, Config{Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, holder, 2)
	pauses := func() bool {
		holder.Context().SetPaused(true)
		// A driven holder steps through its pause, so ticks bound the wait.
		for from := holder.Position().Tick; holder.Position().Tick < from+20; {
			if holder.Context().TimeCtl.IsPaused() {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !pauses() {
		t.Fatal("an authority whose only peer is its own bot refused to pause")
	}
	mustSocketJoiner(t, holder.HostAddr(), fixtureSeed, 120, 40)
	if holder.Context().TimeCtl.IsPaused() {
		t.Fatal("an arrival left the session paused, which its gate cannot serve")
	}
	if pauses() {
		t.Fatal("an authority paused a session holding a participant it does not hold")
	}
}

// TestAGuestsBotsLeaveWithIt: a guest's bots dial the session it joined and go when
// it does, while the host's own bots stay.
func TestAGuestsBotsLeaveWithIt(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host, _ := driveBot(t, Config{Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 2)
	_, leave := driveBot(t, Config{JoinAddress: host.HostAddr(), Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 4)
	leave()
	waitForCursors(t, host, 2)
}

// TestALobbyClosesOnlyOnGuestsOnItsLink: a dial is rostered before its handshake
// ends, so a lobby its own bots fill at once has entries still in flight when its
// first guest closes it, and its start gate has to wait for them.
func TestALobbyClosesOnlyOnGuestsOnItsLink(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	bots := slices.Repeat([]string{"roam"}, 6)
	host, _ := driveBot(t, Config{HostAddress: freeAddress(t), Bots: bots}, "patrol", nil)
	waitForCursors(t, host, len(bots)+1)
}

// TestASeatStandsStillWhileItsHoldersClockIsStopped: held, a driven run takes no
// tick, and released it resumes at pace rather than paying the hold back.
func TestASeatStandsStillWhileItsHoldersClockIsStopped(t *testing.T) {
	var held atomic.Bool
	held.Store(true)
	a, _ := driveBot(t, Config{TimeScaleSpec: "1"}, "patrol", held.Load)
	time.Sleep(20 * parameter.GameUpdateInterval)
	if at := a.Position().Tick; at != 0 {
		t.Fatalf("a held run reached tick %d", at)
	}
	held.Store(false)
	time.Sleep(5 * parameter.GameUpdateInterval)
	if at := a.Position().Tick; at == 0 || at > 12 {
		t.Fatalf("released for five tick intervals after twenty held, the run reached tick %d", at)
	}
}

// TestAHoldersBotsDialItFromThisMachine: a bot reaches its holder's listener over
// the loopback of the family it bound, and a loopback dial spends no join budget,
// so a host seats more bots than one address may join in a window.
func TestAHoldersBotsDialItFromThisMachine(t *testing.T) {
	t.Parallel()
	for bound, want := range map[string]string{
		"127.0.0.1:7777":     "127.0.0.1:7777",
		"0.0.0.0:7777":       "127.0.0.1:7777",
		"[::]:7777":          "[::1]:7777",
		"ws://[::]:7777":     "ws://[::1]:7777",
		"192.0.2.10:7777":    "192.0.2.10:7777",
		"ws://127.0.0.1:777": "ws://127.0.0.1:777",
	} {
		if got := loopbackOf(bound); got != want {
			t.Errorf("a listener bound on %s is dialled at %s, want %s", bound, got, want)
		}
	}
	a := &App{admissions: network.NewAdmissionLimiter()}
	for i := range parameter.MaxPlayers {
		if err := a.admitDial(&net.TCPAddr{IP: net.IPv6loopback, Port: 40000 + i}); err != nil {
			t.Fatalf("loopback dial %d was refused: %v", i+1, err)
		}
	}
	var err error
	for range parameter.NetworkAdmitBurst + 1 {
		err = a.admitDial(&net.TCPAddr{IP: net.ParseIP("192.0.2.7"), Port: 1})
	}
	if err == nil {
		t.Fatal("a remote address was not budgeted")
	}
}

// TestDefaultTypesWhatItReaches: the bot reads the glyph under its cursor before it types.
// A miss is a glyph the tick's own fire destroyed after the read, which is rare.
func TestDefaultTypesWhatItReaches(t *testing.T) {
	t.Parallel()
	a, _ := playBot(t, "default", fixtureSeed, 1200, nil)
	defer a.Close()
	reg := a.world.Resources.Status
	typed, missed := reg.Ints.Get("typing.correct").Load(), reg.Ints.Get("typing.errors").Load()
	if typed < 100 || missed*20 > typed {
		t.Fatalf("default typed %d and missed %d in a minute of play", typed, missed)
	}
}

func TestAnInheritedSessionSeatsBotsWithoutReplacingItsExistingLinks(t *testing.T) {
	host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	tickUntilCursor(t, host)
	if err := host.BeginHosting(seatLoopback); err != nil {
		t.Fatal(err)
	}
	stopTicks := tickInBackground(host)
	stop := func() { stopTicks(); host.Close() }
	t.Cleanup(stop)
	holder, _ := driveBot(t, Config{JoinAddress: host.HostAddr(), Bots: []string{DefaultBotGraph}}, DefaultBotGraph, nil)
	waitForCursors(t, host, 3)
	waitForCursors(t, holder, 3)
	port, err := holder.socketPort()
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(socketWait); port.PeerCount() != 2 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	if port.PeerCount() != 2 {
		t.Fatal("the holder has no warm link to its bot")
	}
	holder.seatsMu.Lock()
	kept := holder.seats[0]
	holder.seatsMu.Unlock()
	stop()
	for deadline := time.Now().Add(socketWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if holder.authority.Holder() == holder.authority.Local() && !holder.authority.Migrating() {
			break
		}
	}
	if holder.authority.Holder() != holder.authority.Local() {
		t.Fatal("the guest did not inherit the session")
	}
	waitForCursors(t, holder, 2)
	// Through the session control `:bot add` calls rather than typed: the holder's own
	// bot types as well, and a mode it switched to would swallow the command.
	holder.world.RunSafe(func() { err = holder.ctx.SessionCtl.AddBot("2:patrol") })
	if err != nil {
		t.Fatalf("adding bots: %v", err)
	}
	waitForCursors(t, holder, 4)
	select {
	case <-kept.done:
		t.Fatal("the holder's existing bot was replaced during migration")
	default:
	}
	if holder.seatAddress == "" {
		t.Fatal("the successor did not open local admission")
	}
}

func TestBotReplayEndsAtTheRecordedSimulationBoundary(t *testing.T) {
	capture := journal.NewCapture()
	source, _ := playBot(t, "default", fixtureSeed, 1100, capture)
	// Quiet trailing ticks must replay too, without running the bot again.
	source.Tick(7)
	want := source.Position()
	source.Close()
	cfg, err := ConfigFromAnchor(capture.Anchors()[0])
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	d, err := newReplayDriver(a, capture.Records(), capture.Captures())
	if err != nil {
		t.Fatal(err)
	}
	d.FinishAt(capture.End())
	if err := d.RunAll(); err != nil {
		t.Fatal(err)
	}
	if got := a.Position(); got.Run != want.Run || got.Tick != want.Tick {
		t.Fatalf("replay stopped at %v, recorded %v", got, want)
	}
	for range 10 {
		if more, err := d.Step(); more || err != nil {
			t.Fatalf("finished replay continued: %v %v", more, err)
		}
	}
	if got := a.Position(); got.Run != want.Run || got.Tick != want.Tick {
		t.Fatalf("replay advanced to %v", got)
	}
}

func TestHostDropsOneBotOrItsWholeHolderGroup(t *testing.T) {
	host, _ := driveBot(t, Config{Bots: []string{DefaultBotGraph}}, DefaultBotGraph, nil)
	waitForCursors(t, host, 2)
	guest, _ := driveBot(t, Config{JoinAddress: host.HostAddr(), Bots: []string{DefaultBotGraph, DefaultBotGraph}}, DefaultBotGraph, nil)
	waitForCursors(t, host, 5)
	waitForCursors(t, guest, 5)
	holder := guest.localParticipant()
	var guestSlot, botSlot int
	host.world.RunSafe(func() {
		for _, p := range (sessionControl{host}).Participants() {
			c, _ := host.world.Components.Cursor.GetComponent(p.Entity)
			if c.PeerID == holder {
				guestSlot = int(p.Slot)
			}
			if c.Holder == holder {
				botSlot = int(p.Slot)
			}
		}
	})
	if botSlot == 0 || guestSlot == 0 {
		t.Fatal("guest bot ownership absent from the host roster")
	}
	guest.world.RunSafe(func() {
		if err := (sessionControl{guest}).DropPlayer(0); err == nil {
			t.Error("guest dropped the host")
		}
	})
	host.world.RunSafe(func() {
		if err := (sessionControl{host}).DropBot(botSlot); err != nil {
			t.Fatal(err)
		}
	})
	waitForCursors(t, host, 4)
	waitForCursors(t, guest, 4)
	if guest.dismissed.Load() {
		t.Fatal("dropping one bot dismissed its holder")
	}
	host.world.RunSafe(func() {
		if err := (sessionControl{host}).DropPlayer(guestSlot); err != nil {
			t.Fatal(err)
		}
	})
	waitForCursors(t, host, 2)
	for deadline := time.Now().Add(socketWait); !guest.dismissed.Load() && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	if !guest.dismissed.Load() {
		t.Fatal("dropped guest did not receive dismissal")
	}
}

func TestBotInputPreservesOperatorFeedback(t *testing.T) {
	a, _ := playBot(t, DefaultBotGraph, fixtureSeed, 10, nil)
	defer a.Close()
	g, err := loadBotGraph(a.cfg.Resources, DefaultBotGraph)
	if err != nil {
		t.Fatal(err)
	}
	d, err := bot.NewDriver(a, a.ctx, g, a.Seed(), 0)
	if err != nil {
		t.Fatal(err)
	}
	a.ctx.SetLastCommand(":bot")
	a.ctx.SetStatusMessage("Bots joined", parameter.StatusMessageDefaultTimeout, true)
	for range 30 {
		if more, err := d.Step(); !more || err != nil {
			t.Fatal(err)
		}
	}
	if a.ctx.GetStatusMessage() != "Bots joined" || a.ctx.GetLastCommand() != ":bot" {
		t.Fatalf("bot replaced operator feedback: %q / %q", a.ctx.GetStatusMessage(), a.ctx.GetLastCommand())
	}
}

func TestHostAndBotsLeavingPreservesTheSurvivingSession(t *testing.T) {
	host, leave := driveBot(t, Config{Bots: []string{DefaultBotGraph}}, DefaultBotGraph, nil)
	waitForCursors(t, host, 2)
	successor, _ := driveBot(t, Config{JoinAddress: host.HostAddr(), Bots: []string{DefaultBotGraph}}, DefaultBotGraph, nil)
	other, _ := driveBot(t, Config{JoinAddress: host.HostAddr()}, DefaultBotGraph, nil)
	for _, a := range []*App{host, successor, other} {
		waitForCursors(t, a, 5)
	}
	port, err := other.socketPort()
	if err != nil {
		t.Fatal(err)
	}
	id := successor.localParticipant()
	for deadline := time.Now().Add(socketWait); !port.Connected(id) && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	if !port.Connected(id) {
		t.Fatal("survivors have no succession link")
	}
	leave()
	for _, a := range []*App{successor, other} {
		waitForCursors(t, a, 3)
		for deadline := time.Now().Add(socketWait); (uint32(a.authority.Holder()) != id || a.authority.Migrating()) && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		}
		if uint32(a.authority.Holder()) != id || forkCell(a).Load() {
			t.Fatal("survivors split after the host group left")
		}
	}
}

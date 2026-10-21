package app

import (
	"errors"
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
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
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

// TestAHoldersBotsComeAndGoAsParticipants: a run with bots and no session opens one
// with no listener, its bots join it in-process as participants, one dropped departs
// as the drop it was asked for rather than a link loss, and the rest leave with the run.
func TestAHoldersBotsComeAndGoAsParticipants(t *testing.T) {
	// Not parallel: paced runs against wall-clock deadlines.
	holder, stop := driveBot(t, Config{Bots: []string{"roam", "patrol"}}, "patrol", nil)
	if addr := holder.HostAddr(); addr != "" {
		t.Fatalf("a run with bots listens on %s", addr)
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
	if msg := holder.Context().GetStatusMessage(); strings.Contains(msg, "disconnected") {
		t.Fatalf("a dropped seat was reported as a link loss: %q", msg)
	}
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
	addr := freeAddress(t)
	holder, _ := driveBot(t, Config{HostAddress: addr, Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, holder, 2)
	// A bot is rostered before its seat learns its participant, and until a tick
	// latches that, its link is a stranger's to the pause gate. A host's bots join
	// its lobby, so the roster holds them before the holder has ticked at all.
	for deadline := time.Now().Add(socketWait); !holder.world.Session().SeatsOnly && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
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
	mustSocketJoiner(t, addr, fixtureSeed, 120, 40)
	if holder.Context().TimeCtl.IsPaused() {
		t.Fatal("an arrival left the session paused, which its gate cannot serve")
	}
	if pauses() {
		t.Fatal("an authority paused a session holding a participant it does not hold")
	}
}

// TestAGuestsBotsLeaveWithIt: a guest's bots join through it and go when it does,
// while the host's own bots stay.
func TestAGuestsBotsLeaveWithIt(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{HostAddress: addr, Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 2)
	_, leave := driveBot(t, Config{JoinAddress: addr, Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 4)
	leave()
	waitForCursors(t, host, 2)
}

// TestARunAndItsBotsJournalMidSession: a run that seated a bot after it started
// opens a journal where it stands, and so does the bot seat it holds, by slot, which
// goes on driving its own cursor; each closes and opens again in one command's time,
// and every journal replays from the world it opened on.
func TestARunAndItsBotsJournalMidSession(t *testing.T) {
	// Not parallel: paced runs against wall-clock deadlines, and the journal
	// directory is the process's.
	dir, level := t.TempDir(), vlog.LevelName()
	vlog.Configure(vlog.Config{JournalDir: dir, Level: level})
	t.Cleanup(func() { vlog.Configure(vlog.Config{Level: level}) })
	until := func(what string, done func() bool) {
		t.Helper()
		for deadline := time.Now().Add(socketWait); !done(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	holder, stop := driveBot(t, Config{}, "patrol", nil)
	ctl := holder.Context().SessionCtl
	var err error
	holder.World().RunSafe(func() { err = ctl.AddBot("roam") })
	if err != nil {
		t.Fatalf("add bot: %v", err)
	}
	waitForCursors(t, holder, 2)
	var s *seat
	until("the bot to be seated", func() bool {
		holder.seatsMu.Lock()
		defer holder.seatsMu.Unlock()
		if len(holder.seats) == 1 && holder.seats[0].app.Load() != nil {
			s = holder.seats[0]
		}
		return s != nil
	})
	slot := int(s.slot.Load())
	drives := func(a *App) (e core.Entity) {
		a.World().RunSafe(func() { e = a.World().Resources.Player.Entity })
		return e
	}
	// The seat binds its cursor a tick after it is seated
	bound := func(b *App) (ok bool) {
		b.World().RunSafe(func() {
			p := b.World().Resources.Player
			ok = p.Entity != 0 && p.Entity == p.Slot(uint8(slot))
		})
		return ok
	}
	var paths []string
	for round := range 2 {
		b := s.app.Load()
		until("the bot to drive its own cursor", func() bool { return bound(b) })
		holderCursor, seatCursor := drives(holder), drives(b)
		// The second opening is asked with the first journals' stop, before either loop serves it
		holder.World().RunSafe(func() {
			err = nil
			if round > 0 {
				err = errors.Join(ctl.StopJournal(-1), ctl.StopJournal(slot))
			}
			err = errors.Join(err, ctl.StartJournal(-1), ctl.StartJournal(slot))
		})
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		until("both journals to open", func() bool {
			h, r := holder.recorder.Load(), b.recorder.Load()
			return h != nil && r != nil && !slices.Contains(paths, h.Path()) && !slices.Contains(paths, r.Path())
		})
		if drives(holder) != holderCursor || drives(b) != seatCursor || int(b.localSlot()) != slot {
			t.Fatalf("the holder drives %d and its seat %d on slot %d after the openings, %d and %d on slot %d before",
				drives(holder), drives(b), b.localSlot(), holderCursor, seatCursor, slot)
		}
		paths = append(paths, holder.recorder.Load().Path(), b.recorder.Load().Path())
		from := b.Position().Tick
		until("the bot to play on", func() bool { return b.Position().Tick > from+3*parameter.DigestIntervalTicks })
	}
	holder.World().RunSafe(func() { err = errors.Join(ctl.StopJournal(-1), ctl.StopJournal(slot)) })
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	b := s.app.Load()
	until("both journals to close", func() bool { return holder.recorder.Load() == nil && b.recorder.Load() == nil })
	stop()
	for _, p := range paths {
		if st, err := VerifyJournal(Config{}, p); err != nil || st.Digests == 0 {
			t.Fatalf("%s reproduced %d digests: %v", p, st.Digests, err)
		}
	}
}

// TestASeatsOpeningLeavesItsRunAsItWas: a bot seat's journal opening, on a slot
// other than its holder's, leaves the seat's run as it stood, so the bot plays on.
func TestASeatsOpeningLeavesItsRunAsItWas(t *testing.T) {
	// Not parallel: paced runs against wall-clock deadlines, and the journal
	// directory is the process's.
	dir, level := t.TempDir(), vlog.LevelName()
	vlog.Configure(vlog.Config{JournalDir: dir, Level: level})
	t.Cleanup(func() { vlog.Configure(vlog.Config{Level: level}) })
	holder, stop := driveBot(t, Config{}, "patrol", nil)
	defer stop()
	ctl := holder.Context().SessionCtl
	var err error
	holder.World().RunSafe(func() { err = ctl.AddBot("roam") })
	if err != nil {
		t.Fatalf("add bot: %v", err)
	}
	var b *App
	for deadline := time.Now().Add(socketWait); b == nil || b.Position().Tick < 3*parameter.DigestIntervalTicks; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the bot to play")
		}
		holder.seatsMu.Lock()
		if len(holder.seats) == 1 {
			b = holder.seats[0].app.Load()
		}
		holder.seatsMu.Unlock()
	}
	// Under the seat's own lock throughout, so its loop takes no step in between
	var before, after []string
	b.World().RunSafe(func() {
		var twin *App
		an := b.buildAnchor()
		if twin, err = b.journalTwin(an); err != nil {
			return
		}
		defer twin.Close()
		b.scheduler.SettleLocked()
		before = snapshot.Lines(b.ctx, b.world, true)
		if _, err = b.openJournalLocked(an, twin); err == nil {
			after = snapshot.Lines(b.ctx, b.world, true)
		}
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if i, _, _, ok := snapshot.FirstDiff(before, after); ok {
		t.Fatalf("the seat's run differs across its opening at line %d:\n%s", i, strings.Join(snapshot.Diff(before, after, 12), "\n"))
	}
}

// TestASeatsJournalClosesWithItsBot: a bot dropped while its seat journals takes the
// journal with it, closed and replayable, and its holder says so.
func TestASeatsJournalClosesWithItsBot(t *testing.T) {
	// Not parallel: paced runs against wall-clock deadlines, and the journal
	// directory is the process's.
	dir, level := t.TempDir(), vlog.LevelName()
	vlog.Configure(vlog.Config{JournalDir: dir, Level: level})
	t.Cleanup(func() { vlog.Configure(vlog.Config{Level: level}) })
	until := func(what string, done func() bool) {
		t.Helper()
		for deadline := time.Now().Add(socketWait); !done(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	holder, stop := driveBot(t, Config{}, "patrol", nil)
	defer stop()
	ctl := holder.Context().SessionCtl
	var err error
	holder.World().RunSafe(func() { err = ctl.AddBot("roam") })
	if err != nil {
		t.Fatalf("add bot: %v", err)
	}
	var s *seat
	until("the bot to be seated", func() bool {
		holder.seatsMu.Lock()
		defer holder.seatsMu.Unlock()
		if len(holder.seats) == 1 && holder.seats[0].app.Load() != nil {
			s = holder.seats[0]
		}
		return s != nil
	})
	slot, b := int(s.slot.Load()), s.app.Load()
	holder.World().RunSafe(func() { err = ctl.StartJournal(slot) })
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	until("the journal to open", func() bool { return b.recorder.Load() != nil })
	path := b.recorder.Load().Path()
	holder.World().RunSafe(func() { err = ctl.DropBot(slot) })
	if err != nil {
		t.Fatalf("drop: %v", err)
	}
	until("the bot to leave", func() bool { return len(holder.botSeats()) == 0 })
	if msg := holder.Context().GetStatusMessage(); !strings.Contains(msg, "journal closed: "+path) {
		t.Fatalf("the holder shows %q once its journaling bot left", msg)
	}
	if st, err := VerifyJournal(Config{}, path); err != nil {
		t.Fatalf("%s reproduced %d digests: %v", path, st.Digests, err)
	}
}

// TestAHeadlessBotEndsWhenItsSessionDoes: a guest bot whose pinned host leaves is
// left with no session, and stops rather than playing its fork for nobody.
func TestAHeadlessBotEndsWhenItsSessionDoes(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, leave := driveBot(t, Config{HostAddress: addr, FixedAuthority: true, Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 2)
	cfg := Config{JoinAddress: addr, Seed: fixtureSeed, Width: BotWidth, Height: BotHeight,
		Resources: resource.Options{Embedded: true}}
	g, err := loadBotGraph(cfg.Resources, "patrol")
	if err != nil {
		t.Fatal(err)
	}
	stop, ended := make(chan os.Signal), make(chan error, 1)
	t.Cleanup(func() { close(stop) })
	go func() {
		_, err := drive(cfg, "bot", g.Name, stop, nil, func(a *App) (pacedSource, error) {
			d, err := bot.NewDriver(a, a.ctx, g, a.Seed(), a.localParticipant())
			return botSource{d}, err
		})
		ended <- err
	}()
	waitForCursors(t, host, 3)
	leave()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(socketWait):
		t.Fatal("a headless bot played on after its session ended")
	}
}

// TestAGuestsBotsJoinThroughIt: a guest's bot is admitted on the guest's link, not
// the host's, the host still allocates it and crosses its arrival, and what it plays
// reaches the host through the guest.
func TestAGuestsBotsJoinThroughIt(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines. The session is
	// named: the guest's relay acceptor checks the name, so its seat must send it.
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{HostAddress: addr, Bots: []string{"roam"}, SessionName: "named"}, "patrol", nil)
	waitForCursors(t, host, 2)
	guest, _ := driveBot(t, Config{JoinAddress: addr, SessionName: "named", Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 4)
	waitForCursors(t, guest, 4)
	if h, g := host.sessionTransport().PeerCount(), guest.sessionTransport().PeerCount(); h != 2 || g != 2 {
		t.Fatalf("the host holds %d links and the guest %d, want 2 each", h, g)
	}
	local := network.PeerID(guest.localParticipant())
	roster := (instance{host}).WorldRoster()
	i := slices.IndexFunc(roster, func(e network.RosterEntry) bool { return e.Holder == local })
	if i < 0 {
		t.Fatalf("the host holds no bot of participant %d: %+v", local, roster)
	}
	var e core.Entity
	host.world.RunSafe(func() { e = host.world.Resources.Player.Slot(roster[i].Slot) })
	first := cursorPosition(host, e)
	for deadline := time.Now().Add(socketWait); cursorPosition(host, e) == first; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the guest's bot never moved on the host")
		}
	}
}

// TestAStillGuestSeatsBotsAfterItsHostLeaves: a guest's bots join through it
// whoever holds authority, so the departed host's address is never dialled.
func TestAStillGuestSeatsBotsAfterItsHostLeaves(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, leave := driveBot(t, Config{HostAddress: addr, Bots: []string{DefaultBotGraph}}, DefaultBotGraph, nil)
	waitForCursors(t, host, 2)
	successor, _ := driveBot(t, Config{JoinAddress: addr}, DefaultBotGraph, nil)
	guest, _ := driveBot(t, Config{JoinAddress: addr}, DefaultBotGraph, nil)
	for _, a := range []*App{host, successor, guest} {
		waitForCursors(t, a, 4)
	}
	port, err := guest.socketPort()
	if err != nil {
		t.Fatal(err)
	}
	id := successor.localParticipant()
	for deadline := time.Now().Add(socketWait); !port.Connected(id) && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	leave()
	for _, a := range []*App{successor, guest} {
		waitForCursors(t, a, 2)
		for deadline := time.Now().Add(socketWait); (uint32(a.authority.Holder()) != id || a.authority.Migrating()) && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		}
		if uint32(a.authority.Holder()) != id {
			t.Fatal("the session did not pass to the first guest")
		}
	}
	guest.world.RunSafe(func() { err = guest.ctx.SessionCtl.AddBot(DefaultBotGraph) })
	if err != nil {
		t.Fatalf("adding a bot: %v", err)
	}
	for _, a := range []*App{successor, guest} {
		waitForCursors(t, a, 3)
	}
	// The bot is rostered before its handshake ends, so its link to the guest can
	// land after its cursor does
	links := func() (int, int) {
		return successor.sessionTransport().PeerCount(), guest.sessionTransport().PeerCount()
	}
	for deadline := time.Now().Add(socketWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if s, g := links(); s == 1 && g == 2 {
			break
		}
	}
	if s, g := links(); s != 1 || g != 2 {
		t.Fatalf("the authority holds %d links and the guest %d, want 1 and 2", s, g)
	}
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
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
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
	end := capture.End()
	d, err := newReplayDriver(a, journal.Set{Records: capture.Records(), Captures: capture.Captures(), End: &end}.Stream())
	if err != nil {
		t.Fatal(err)
	}
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
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{HostAddress: addr, Bots: []string{DefaultBotGraph}}, DefaultBotGraph, nil)
	waitForCursors(t, host, 2)
	guest, _ := driveBot(t, Config{JoinAddress: addr, Bots: []string{DefaultBotGraph, DefaultBotGraph}}, DefaultBotGraph, nil)
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
	a.ctx.SetStatusMessage("Bots joined", parameter.StatusNotice)
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
	addr := freeAddress(t)
	host, leave := driveBot(t, Config{HostAddress: addr, Bots: []string{DefaultBotGraph}}, DefaultBotGraph, nil)
	waitForCursors(t, host, 2)
	successor, _ := driveBot(t, Config{JoinAddress: addr, Bots: []string{DefaultBotGraph}}, DefaultBotGraph, nil)
	other, _ := driveBot(t, Config{JoinAddress: addr}, DefaultBotGraph, nil)
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

// waitListening waits for a run to bind an address it was told to.
func waitListening(t *testing.T, addr string) {
	t.Helper()
	for deadline := time.Now().Add(socketWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			return
		}
	}
	t.Fatalf("nothing listens at %s", addr)
}

// cursorOf is the cursor a participant drives, as another instance holds it.
func cursorOf(t *testing.T, a *App, participant uint32) core.Entity {
	t.Helper()
	roster := (instance{a}).WorldRoster()
	i := slices.IndexFunc(roster, func(e network.RosterEntry) bool { return uint32(e.ID) == participant })
	if i < 0 {
		t.Fatalf("participant %d is not in the roster %+v", participant, roster)
	}
	var e core.Entity
	a.world.RunSafe(func() { e = a.world.Resources.Player.Slot(roster[i].Slot) })
	return e
}

// waitForMotion waits for a cursor to move on an instance that does not drive it.
func waitForMotion(t *testing.T, a *App, e core.Entity, what string) {
	t.Helper()
	first := cursorPosition(a, e)
	for deadline := time.Now().Add(socketWait); cursorPosition(a, e) == first; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never moved", what)
		}
	}
}

// TestAPersonJoinsThroughARelayingGuest: a participant that cannot reach the host
// joins at a guest's -relay address as an independent player, and plays through
// it: its artifacts reach the authority on the guest's link and the authority's
// reach it back.
func TestAPersonJoinsThroughARelayingGuest(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{HostAddress: addr, Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 2)
	relayAddr := freeAddress(t)
	relay, _ := driveBot(t, Config{JoinAddress: addr, RelayAddress: relayAddr}, "patrol", nil)
	waitForCursors(t, host, 3)
	waitListening(t, relayAddr)
	person, _ := driveBot(t, Config{JoinAddress: relayAddr}, "roam", nil)
	for _, a := range []*App{host, relay, person} {
		waitForCursors(t, a, 4)
	}
	id := person.localParticipant()
	roster := (instance{host}).WorldRoster()
	if i := slices.IndexFunc(roster, func(e network.RosterEntry) bool { return uint32(e.ID) == id }); i < 0 || roster[i].Holder != 0 {
		t.Fatalf("the host holds the relayed person as %+v, want an independent participant", roster)
	}
	if h, r, p := host.sessionTransport().PeerCount(), relay.sessionTransport().PeerCount(),
		person.sessionTransport().PeerCount(); h != 2 || r != 2 || p != 1 {
		t.Fatalf("the host holds %d links, the relay %d and the person %d, want 2, 2 and 1", h, r, p)
	}
	waitForMotion(t, host, cursorOf(t, host, id), "the relayed person's cursor on the host")
	waitForMotion(t, person, cursorOf(t, person, host.localParticipant()), "the host's cursor on the relayed person")
}

// TestAPersonFindsAnotherPathWhenItsRelayLeaves: left with no link, a relayed
// participant walks the chain to another guest, which carries its artifacts from
// then on; the session goes on with its relay gone.
func TestAPersonFindsAnotherPathWhenItsRelayLeaves(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{HostAddress: addr, Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 2)
	relayAddr := freeAddress(t)
	relay, leave := driveBot(t, Config{JoinAddress: addr, RelayAddress: relayAddr}, "patrol", nil)
	waitForCursors(t, host, 3)
	other, _ := driveBot(t, Config{JoinAddress: addr}, "patrol", nil)
	waitForCursors(t, host, 4)
	waitListening(t, relayAddr)
	person, _ := driveBot(t, Config{JoinAddress: relayAddr}, "roam", nil)
	for _, a := range []*App{host, relay, other, person} {
		waitForCursors(t, a, 5)
	}
	id := person.localParticipant()
	if p := person.sessionTransport().PeerCount(); p != 1 {
		t.Fatalf("the person holds %d links before its relay leaves, want its relay alone", p)
	}

	leave()
	for _, a := range []*App{host, other, person} {
		waitForCursors(t, a, 4)
	}
	waitForMotion(t, host, cursorOf(t, host, id), "the relayed person's cursor on the host after its relay left")
	if o, p := other.sessionTransport().PeerCount(), person.sessionTransport().PeerCount(); o != 2 || p != 1 {
		t.Fatalf("the other guest holds %d links and the person %d, want the person behind the other guest", o, p)
	}
}

// TestABotsPressHoldsUntilItLetsGo: a bot's instance takes its keys as releasing,
// so a press of a captured key holds for the ticks its graph asked and a tap lets go
// at once, each let go rather than lost, and its slot crosses as releasing.
func TestABotsPressHoldsUntilItLetsGo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeOffMapScenario(t, root)
	a, err := NewHeadless(Config{Seed: 0xF103, Width: 80, Height: 24, Resources: resource.Options{Dir: root, Scenario: "keys"}})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()
	tickUntilCursor(t, a)
	g, err := bot.ParseGraph("holds", []byte(`
[bot]
actions_per_second = 1000

[regions.play]
initial = "Play"

[states.Play]
on_enter = [
    { action = "Intent", payload = { name = "press", char = "1", hold = 4 } },
    { action = "Intent", payload = { name = "press", char = "2" } },
]
`))
	if err != nil {
		t.Fatal(err)
	}
	d, err := bot.NewDriver(a, a.ctx, g, a.Seed(), a.localParticipant())
	if err != nil {
		t.Fatal(err)
	}
	// The graph reads no events, so this tap may stand in for the driver's own
	var got []string
	start := a.world.Resources.Game.State.GetGameTicks()
	a.SetDispatchTap(func(ev event.GameEvent) {
		switch p := ev.Payload.(type) {
		case *event.InputPayload:
			if ev.Type == event.EventInputPressed || ev.Type == event.EventInputReleased {
				got = append(got, fmt.Sprintf("%s %s +%d lost=%v", event.GetEventName(ev.Type), p.Key, p.Tick-start, p.Lost))
			}
		case *event.InputReportPayload:
			got = append(got, fmt.Sprintf("report slot %d releases=%v", p.Slot, p.Releases))
		}
	})
	for range 8 {
		if more, err := d.Step(); !more || err != nil {
			t.Fatalf("more %v, err %v", more, err)
		}
	}
	want := []string{"EventInputPressed 1 +0 lost=false", "EventInputPressed 2 +0 lost=false",
		"EventInputReleased 2 +0 lost=false", "report slot 0 releases=true", "EventInputReleased 1 +4 lost=false"}
	if !slices.Equal(got, want) {
		t.Fatalf("crossed %q, want %q", got, want)
	}
}

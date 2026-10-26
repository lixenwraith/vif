package app

import (
	"cmp"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	link "github.com/lixenwraith/vif/pkg/network"
)

func TestAllocatorRequestForwardsOptionsAndPreservesRunOnRefusal(t *testing.T) {
	type request struct {
		Players  int
		Scenario string
	}
	received, release := make(chan request, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/vif/api/sessions" {
			t.Errorf("unexpected allocator request %s %s", r.Method, r.URL.Path)
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		received <- req
		<-release
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"test quota reached"}}`))
	}))
	defer site.Close()
	defer unblock()
	a := mustHeadless(t, fixtureSeed, 80, 24)
	defer a.Close()
	a.cfg.Mode = ModePlay
	tickUntilCursor(t, a)
	before := a.Position()
	a.World().RunSafe(func() {
		ctl := a.ctx.SessionCtl
		for _, n := range []int{-1, parameter.MaxPlayers + 1} {
			if err := ctl.RequestSession(site.URL, n, "tower"); err == nil {
				t.Errorf("accepted players=%d", n)
			}
		}
		if err := ctl.RequestSession("127.0.0.1:4242", 2, "tower"); err == nil {
			t.Error("accepted a non-site allocator target")
		}
		if err := ctl.RequestSession(site.URL, 3, "tower"); err != nil {
			t.Fatal(err)
		}
		if ctl.HostError() == nil || ctl.JoinError() == nil {
			t.Error("pending join is available for another session action")
		}
		if err := ctl.AddBot("default"); err == nil {
			t.Error("seated bots during a pending join")
		}
		if err := ctl.BeginHosting("127.0.0.1:0", ""); err == nil {
			t.Error("hosted during a pending join")
		}
		if err := ctl.RequestSession(site.URL, 3, "tower"); err == nil {
			t.Error("started two allocator requests")
		}
	})
	select {
	case req := <-received:
		if req.Players != 3 || req.Scenario != "tower" {
			t.Fatalf("allocator received %+v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("allocator was not called")
	}
	unblock()
	for deadline := time.Now().Add(2 * time.Second); a.membership.dialling.Load() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if a.membership.dialling.Load() || a.restart.Load() != nil || a.Position() != before || !strings.Contains(a.ctx.GetStatusMessage(), "test quota reached") {
		t.Fatalf("refused request changed the run or hid the error: %s", a.ctx.GetStatusMessage())
	}
}

// joinTestTickInterval paces the host through a join in this harness. See the
// comment at its use.
const joinTestTickInterval = 10 * time.Millisecond

func TestMidRunHostUsesConfiguredCapOrMaximum(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	for _, tt := range []struct {
		name         string
		participants int
		want         int
	}{
		{name: "explicit", participants: 3, want: 3},
		{name: "unspecified", want: parameter.MaxPlayers},
	} {
		t.Run(tt.name, func(t *testing.T) {
			host := mustHeadless(t, 0x3016, 120, 40)
			defer host.Close()
			host.cfg.Participants = tt.participants
			if err := host.BeginHosting("127.0.0.1:0"); err != nil {
				t.Fatalf("begin hosting: %v", err)
			}
			if got := host.membership.sessionCapacity() + 1; got != tt.want {
				t.Fatalf("lobby size = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestAMidRunHostAttributesItsOwnCursor is what a succession is decided from: the
// roster is re-derived from the participant stamped on each cursor, and it travels
// in every capture. A session opened without a lobby stamps the coordinator's own
// cursor too; unstamped, no survivor's handoff record would name the participant
// that had gone, and its cursor would stay on the map.
func TestAMidRunHostAttributesItsOwnCursor(t *testing.T) {
	host := mustHeadless(t, 0x3017, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	var owner uint32
	host.World().RunSafe(func() {
		c, _ := host.World().Components.Cursor.GetComponent(host.World().Resources.Player.Slot(0))
		owner = c.PeerID
	})
	if owner != engine.CoordinatorParticipant {
		t.Fatalf("the coordinator's own cursor names participant %d, want %d",
			owner, engine.CoordinatorParticipant)
	}
}

// TestSoloRunBecomesAHostAndAdmitsAParticipantMidRun drives the whole mid-run join
// over a real socket at a tick that is not zero: a solo run opens a socket without
// restarting, a participant dials hundreds of ticks in and installs the world it
// receives, the crossings produced during the transfer reach it rather than falling
// into the gap, and the two then hold the same shared world.
func TestSoloRunBecomesAHostAndAdmitsAParticipantMidRun(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x3017
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(240)

	if got := host.SessionSummary(); !strings.Contains(got, "Solo") {
		t.Fatalf("before hosting the run reports %q", got)
	}
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	addr := host.HostAddr()
	if addr == "" {
		t.Fatal("hosting run reports no address")
	}
	var latched bool
	host.World().RunSafe(func() { latched = host.World().SessionShared() })
	if !latched {
		t.Fatal("hosting run is not latched as a session")
	}

	// The host has to keep running through the join: the capture is read under the
	// world lock from the accept goroutine, and the gap the joiner then closes is
	// exactly the ticks the host completed while its world was in transit. A host
	// frozen for the transfer would prove the easy half of this.
	stopTicking := tickInBackground(host)
	defer stopTicking()

	guest, _ := mustSocketJoiner(t, addr, seed, 120, 40)
	stopTicking()

	// The joiner arrived at a tick the host had reached, not at tick zero, and it
	// closed the gap between the world it was sent and where the session had got to
	// rather than staying behind it.
	guestTick := guest.Position().Tick
	if guestTick < 240 {
		t.Fatalf("guest entered at tick %d; the host was past 240 before it dialled", guestTick)
	}
	reg := guest.World().Resources.Status
	installed := reg.Ints.Get("snapshot.install_tick").Load()
	caught := reg.Ints.Get("snapshot.catch_up_ticks").Load()
	if installed <= 0 {
		t.Fatal("the guest installed no capture")
	}
	if caught == 0 || int64(guestTick) < installed {
		t.Fatalf("guest installed at tick %d and stands at %d after %d catch-up ticks; "+
			"the session moved during the transfer and the join has to close that",
			installed, guestTick, caught)
	}
	t.Logf("installed at tick %d, entered at %d (%d catch-up ticks), stage %dus commit %dus",
		installed, guestTick, caught,
		reg.Ints.Get("snapshot.stage_us").Load(), reg.Ints.Get("snapshot.commit_us").Load())
	// Behind by more than the lead is refused by the join itself, against the newest
	// epoch it saw: the host's own tick also counts the ticks it closed on its own
	// goroutine after that, which is wall time, not the join. Ahead is bounded here,
	// since the guest's crossings then carry the playout lead and no further.
	if ahead := int64(guestTick) - int64(host.Position().Tick); ahead > int64(parameter.NetworkJoinLagTicks) {
		t.Fatalf("guest stands %d ticks ahead of the host after catching up; the lead is %d",
			ahead, parameter.NetworkJoinLagTicks)
	}

	// The cadence is stopped for the rest of this test and everything already sent is
	// drained. A correction is a clock as much as a world — installing one pins this
	// guest's tick to the host's — so one landing between two assertions would move
	// the thing they compare. The correction criteria elsewhere own that subject.
	settleCorrections(t, host, guest)

	// Bring the two onto one tick, then hold them there. The residual offset is this
	// harness's wall pacing, not the join's: the guest caught up to the newest epoch
	// it had seen, and the host closed a few more while the goroutine was stopping.
	alignTicks(t, host, guest)
	assertSharedParity(t, host, guest, 0)

	// The arrival is a crossing, so the guest's cursor exists on both instances as
	// the same shared entity. It is not in the capture: the capture was read before
	// the participant had one.
	waitForRosterPair(t, host, guest)
	alignTicks(t, host, guest)
	assertControl(t, host, 1, component.ControlRemote)
	assertControl(t, guest, 1, component.ControlLocal)
	if got := guest.membership.localSlot(); got != 1 {
		t.Fatalf("guest follows slot %d, want its own slot 1", got)
	}

	// Nothing was applied twice: the artifacts the host produced between admitting
	// the guest and reading the world for it are in the capture, and the barrier
	// recognised them rather than replaying them onto a world that already had them.
	assertSharedParity(t, host, guest, 1)
	if got := host.SessionSummary(); !strings.Contains(got, "host") {
		t.Fatalf("hosting run reports %q", got)
	}
}

// TestAJoinerLandsInAHeldSessionHeld: a held session's gate can wait for no lead, so
// a joiner is given the world at the held tick and holds there with the host, and
// the host's resume starts both and crosses its arrival.
func TestAJoinerLandsInAHeldSessionHeld(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x3021
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(60)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	mode.ExecuteCommand(host.Context(), "pause")
	host.Settle()
	for range parameter.NetworkBarrierMaxDelayTicks + 2 {
		// A move made as the hold lands applies after it, so the world at the held
		// tick lacks it: the joiner hears it as what was still scheduled.
		inject(t, host, intentMotion(input.MotionRight, 1))
		host.Tick(1)
	}
	at := host.Position().Tick
	if !host.Context().TimeCtl.Held() {
		t.Fatal("the host did not hold")
	}

	// A held host drains its link only between ticks it does not take, as its
	// driver does while it waits.
	stopTicking := tickInBackground(host)
	guest, _ := mustSocketJoiner(t, host.HostAddr(), seed, 120, 40)
	stopTicking()
	if !guest.Context().TimeCtl.Held() || guest.Position().Tick != at || host.Position().Tick != at {
		t.Fatalf("the guest holds %v at tick %d and the host stands at %d, want both held at %d",
			guest.Context().TimeCtl.Held(), guest.Position().Tick, host.Position().Tick, at)
	}
	if got := guest.Context().GetStatusMessage(); got != "Paused by the host" {
		t.Fatalf("the joiner was told %q", got)
	}
	assertSharedParity(t, host, guest, 0)

	mode.ExecuteCommand(host.Context(), "pause off")
	host.Settle()
	waitForRosterPair(t, host, guest)
	if guest.Context().TimeCtl.Held() || guest.Position().Tick <= at {
		t.Fatalf("after the host resumed the guest holds %v at tick %d", guest.Context().TimeCtl.Held(), guest.Position().Tick)
	}
	settleCorrections(t, host, guest)
	alignTicks(t, host, guest)
	assertSharedParity(t, host, guest, 1)
}

// TestAJoinThroughARelayLandsInAHeldSessionHeld: a relay's gate meets a hold as the
// host's does. The hold goes out as the held tick's round, the relay proves the held
// world from it and hands it to the joiner, which lands held where the session
// stands, and runs on with everyone when the host resumes.
func TestAJoinThroughARelayLandsInAHeldSessionHeld(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{HostAddress: addr, Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 2)
	relayAddr := freeAddress(t)
	relay, _ := driveBot(t, Config{JoinAddress: addr, RelayAddress: relayAddr}, "patrol", nil)
	waitForCursors(t, host, 3)
	waitListening(t, relayAddr)
	// A driven host takes no step while held, so its end is settled here, as a
	// :pause off at its console would be.
	hold := func(held bool) {
		host.world.RunSafe(func() {
			host.ctx.PushLocal(event.EventGameHoldRequest, &event.GamePausePayload{Paused: held})
		})
		host.Settle()
	}
	hold(true)
	waitUntil(t, func() bool { return host.Context().TimeCtl.Held() && relay.Context().TimeCtl.Held() }, "the session held")
	at := host.Position().Tick
	person, _ := driveBot(t, Config{JoinAddress: relayAddr}, "roam", nil)
	waitUntil(t, func() bool { return person.Context().TimeCtl.Held() }, "the joiner held")
	if tick := person.Position().Tick; tick != at || host.Position().Tick != at {
		t.Fatalf("the joiner holds at tick %d and the host stands at %d, want both at %d", tick, host.Position().Tick, at)
	}
	if got := person.Context().GetStatusMessage(); got != "Paused by the host" {
		t.Fatalf("the joiner was told %q", got)
	}
	hold(false)
	for _, a := range []*App{host, relay, person} {
		waitForCursors(t, a, 4)
	}
	if tick := person.Position().Tick; tick <= at || person.Context().TimeCtl.Held() {
		t.Fatalf("after the host resumed the joiner holds %v at tick %d, want running past %d",
			person.Context().TimeCtl.Held(), tick, at)
	}
}

// TestAReconnectIsTheSameJoin asserts there is no fourth join mechanism: a dropped
// participant leaves a Wraith in its slot, and what comes back is an ordinary dial,
// which the full session admits once that Wraith gives up its place. The join runs
// twice against one lobby host — once through the start gate, once through the
// mid-run one — with a disconnect between, asserting the second arrival lands on a
// world the host has moved past.
func TestAReconnectIsTheSameJoin(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x3019
	addr := freeAddress(t)

	// newScriptApp is a scripted -host: a lobby, the start gate, and the mid-run
	// gate armed once the roster has closed.
	cfg := Config{
		Mode: ModeHeadless, Seed: seed, Width: 120, Height: 40,
		Resources:   resource.Options{Embedded: true},
		HostAddress: addr, Participants: 2,
	}
	cfg.scriptedSession = true
	hosted, hostFailed := make(chan *App, 1), make(chan error, 1)
	go func() {
		if a, err := newScriptApp(cfg, nil); err != nil {
			hostFailed <- err
		} else {
			hosted <- a
		}
	}()

	// The first join is the lobby's: the host is frozen at tick zero until this
	// guest confirms, so nothing has to drive it here.
	first, firstPort := mustSocketJoiner(t, addr, seed, 120, 40)
	var host *App
	select {
	case host = <-hosted:
	case err := <-hostFailed:
		t.Fatalf("host startup: %v", err)
	case <-time.After(socketWait):
		t.Fatal("the host never finished its start gate")
	}
	t.Cleanup(host.Close)

	// The gate the reconnect below arrives at, asserted where a run arms it. A lobby
	// that has closed has to do both things: stop refusing dials, and start serving
	// them. Doing only the first is what left a re-dial admitted onto no gate at all.
	if host.membership.lobbyClosing.Load() || !host.membership.lateJoins.Load() {
		t.Fatalf("after its start gate the host refuses dials = %t and serves mid-run joins = %t",
			host.membership.lobbyClosing.Load(), host.membership.lateJoins.Load())
	}
	firstTick := uint64(first.World().Resources.Status.Ints.Get("snapshot.install_tick").Load())

	_ = firstPort.Close()
	first.Close()
	waitForHostWraith(t, host)

	// The host keeps playing between the two joins, so the second capture describes
	// a world the first participant never saw.
	pumpHost(t, host, 60)
	secondTick := joinAndLeave(t, host, addr, seed)

	if secondTick <= firstTick {
		t.Fatalf("the reconnect installed tick %d, the first join installed %d; "+
			"the host has to have moved on between them or this proves nothing",
			secondTick, firstTick)
	}
}

// freeAddress reserves a loopback port and hands back the address nothing is now
// listening on. A run started from its flags takes a literal -host address, so a
// test that drives that path cannot ask the kernel for one after the fact.
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release the reserved port: %v", err)
	}
	return addr
}

// joinDeadline stands in for the person at the keyboard. The start gate has no
// deadline of its own — it is the host waiting for the rest of its lobby — so what
// bounds it is that it can be left, and this is the harness leaving it. A gate that
// could not be would hang a run rather than fail it, which is what it did.
func joinDeadline(d time.Duration) <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	time.AfterFunc(d, func() { ch <- os.Interrupt }) // [wall] a link bound, not a game one
	return ch
}

// dialSession dials until the coordinator answers with an offer. Both things it
// retries past are the protocol's own: a host binding on its own goroutine is not
// listening yet, and a refusal — a lobby mid-close, a Wraith giving up its place —
// is the answer the design tells a dialer to retry on. A refusal spends the
// address's admission budget, so it is retried at a rejoin's pace.
func dialSession(t *testing.T, addr string) (*network.PendingJoin, network.SessionOffer) {
	t.Helper()
	deadline := time.Now().Add(socketWait) // [wall] a link bound, not a game one
	var last error
	for time.Now().Before(deadline) {
		pending, offered, err := network.DialSession(addr, network.DebugConfig(link.RolePeer, ""))
		if err == nil {
			return pending, offered
		}
		last = err
		pause := time.Millisecond
		if errors.As(err, new(*network.Refusal)) {
			pause = parameter.SessionRejoinInterval
		}
		time.Sleep(pause)
	}
	t.Fatalf("dial session %s: %v", addr, last)
	return nil, network.SessionOffer{}
}

// tickInBackground keeps one instance running while the caller does something that
// needs a live host, and returns the stop. The pacing is faster than the game
// interval on purpose: at 50 ms a loopback transfer finishes inside one tick and
// there is no gap for a joiner to close. Stop is idempotent; callers also defer it,
// so a Fatal cannot leave the ticker running into the next test.
func tickInBackground(a *App) (stop func()) {
	var once sync.Once
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stopped:
				return
			default:
			}
			a.Tick(1)
			time.Sleep(joinTestTickInterval) // [wall] a link bound, not a game one
		}
	}()
	return func() {
		once.Do(func() { close(stopped) })
		<-done
	}
}

// joinAndLeave runs one full join against a live host, returns the tick the guest
// installed at, and then drops the link.
func joinAndLeave(t *testing.T, host *App, addr string, seed uint64) uint64 {
	t.Helper()
	stopTicking := tickInBackground(host)
	defer stopTicking()

	guest, port := mustSocketJoiner(t, addr, seed, 120, 40)
	stopTicking()

	installed := uint64(guest.World().Resources.Status.Ints.Get("snapshot.install_tick").Load())
	waitForRosterPair(t, host, guest)

	// The departure is a crossing like any other, so the host applies it a playout
	// lead after it observes the lost link, not where the link was lost.
	_ = port.Close()
	guest.Close()
	waitForHostWraith(t, host)
	return installed
}

// waitForHostWraith ticks the host until its guest's cursor waits as a Wraith beside
// its own, which is the departure crossing applying a playout lead after the link
// was seen to go.
func waitForHostWraith(t *testing.T, host *App) {
	t.Helper()
	var roster, wraiths int
	count := func() {
		host.World().RunSafe(func() {
			roster, wraiths = host.World().Resources.Player.Count(), host.World().Components.Wraith.CountEntities()
		})
	}
	for range 80 {
		if count(); roster == 2 && wraiths == 1 {
			return
		}
		host.Tick(1)
		time.Sleep(joinTestTickInterval / 2)
	}
	t.Fatalf("host roster holds %d cursors and %d Wraiths after the guest left, want 2 and 1", roster, wraiths)
}

// pumpHost advances a hosting instance at something like its wall pacing, so the
// transport's own goroutines see the ticks pass.
func pumpHost(t *testing.T, host *App, ticks int) {
	t.Helper()
	for range ticks {
		host.Tick(1)
		time.Sleep(joinTestTickInterval / 2)
	}
}

// TestHostCommandRunsUnderTheWorldLock is the regression for a deadlock. The router
// path runs inside App.handleIntent's critical section, so a SessionController method
// that took the world lock wedges the instance at the moment the operator presses
// enter. Calling BeginHosting or seating a bot directly cannot see that; only the
// real input path can.
func TestHostCommandRunsUnderTheWorldLock(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	a := mustHeadless(t, 0x301A, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	injectExCommand(t, a, "host 127.0.0.1:0")
	a.Tick(1)
	if a.HostAddr() == "" {
		t.Fatalf("the command opened no socket; status bar says %q", a.Context().GetStatusMessage())
	}
	injectExCommand(t, a, "session")
	a.Tick(1)
	if got := a.Context().GetStatusMessage(); !strings.Contains(got, "host") {
		t.Fatalf(":session reports %q", got)
	}

	injectExCommand(t, a, "bot add")
	stopTicking := tickInBackground(a)
	defer stopTicking()
	for deadline := time.Now().Add(socketWait); !strings.Contains(a.membership.seatsSummary(), "slot 1 default"); {
		if time.Now().After(deadline) {
			t.Fatalf("the bot was not seated: %q", a.membership.seatsSummary())
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopTicking()
	injectExCommand(t, a, "bot")
	if got := a.Context().GetStatusMessage(); !strings.Contains(got, "slot 1 default") {
		t.Fatalf(":bot reports %q", got)
	}
}

// injectExCommand types one ex command through the intent pipeline, which is the
// only path that runs it where the runtime actually runs it.
func injectExCommand(t *testing.T, a *App, command string) {
	t.Helper()
	a.Inject(&input.Intent{Type: input.IntentModeSwitch, ModeTarget: input.ModeTargetCommand, Count: 1})
	for _, r := range command {
		a.Inject(&input.Intent{Type: input.IntentTextChar, Char: r, Count: 1})
	}
	a.Inject(&input.Intent{Type: input.IntentTextConfirm, Count: 1})
}

// TestScenarioChangeNeedsARestartLoop pins the guard the command carries: a run the
// caller drives has no Run loop to service a restart, so the change is refused
// rather than latched as a request nothing will ever read.
func TestScenarioChangeNeedsARestartLoop(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x3020, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	injectExCommand(t, a, "n td")
	a.Tick(1)
	if got := a.Context().GetStatusMessage(); !strings.Contains(got, "restart loop") {
		t.Fatalf("status bar says %q; want a refusal naming the restart loop", got)
	}
	if req := a.restart.Load(); req != nil {
		t.Fatalf("a refused change latched %+v", req)
	}
}

// TestARefusedJoinLeavesTheRunPlaying pins that :join dials while the run plays on:
// a host that refuses it leaves the game as it was rather than ending it.
func TestARefusedJoinLeavesTheRunPlaying(t *testing.T) {
	t.Parallel()
	host, err := NewHeadless(Config{Seed: 0x3021, Width: 120, Height: 40, SessionName: "this",
		Resources: resource.Options{Embedded: true}})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	defer host.Close()
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("host: %v", err)
	}
	a := mustHeadless(t, 0x3022, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.cfg.Mode = ModePlay // joinLocked serves Run's loop; the rule is its dial

	a.World().RunSafe(func() { err = a.membership.joinLocked(host.HostAddr() + "/that") })
	if err != nil {
		t.Fatalf("join refused before dialling: %v", err)
	}
	deadline := time.Now().Add(socketWait) // [wall] a link bound
	for a.membership.dialling.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the dial never finished")
		}
		time.Sleep(time.Millisecond)
	}
	if req := a.restart.Load(); req != nil {
		t.Fatalf("a refused join replaced the run: %+v", req)
	}
	if got := a.Context().GetStatusMessage(); !strings.Contains(got, "Join failed") {
		t.Fatalf("status bar says %q; want the refusal", got)
	}
}

// TestOnlyTheHostChangesALiveSessionsScenario pins who may do it. A scenario
// change rebuilds every participant, so a guest that could ask for one could empty
// somebody else's session.
func TestOnlyTheHostChangesALiveSessionsScenario(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A2, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	guest := apps[1]
	var err error
	guest.World().RunSafe(func() { _, err = guest.membership.changeScenarioLocked("blank") })
	if err == nil || !strings.Contains(err.Error(), "only the host") {
		t.Fatalf("a guest's change = %v; want a refusal naming the host", err)
	}
	if req := guest.restart.Load(); req != nil {
		t.Fatalf("a refused change latched %+v", req)
	}
}

// TestAReceivedScenarioAnswersOnlyItsOwnName: a run that plays bytes a coordinator
// sent resets on their name and resolves any other from its roots, so a successor
// holding one can still change the session's scenario.
func TestAReceivedScenarioAnswersOnlyItsOwnName(t *testing.T) {
	t.Parallel()
	received, err := resource.LoadScenario(resource.Options{
		Scenario: writeScenario(t, t.TempDir(), "duel", "A received duel.")})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeScenario(t, root, "other", "A scenario this root holds.")
	a, err := NewHeadless(Config{Seed: fixtureSeed, Width: 80, Height: 24,
		Resources: resource.Options{Dir: root, Provided: &received}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.cfg.Mode = ModePlay

	var changed bool
	a.World().RunSafe(func() { changed, err = a.membership.changeScenarioLocked("duel") })
	if err != nil || changed {
		t.Fatalf("its own name: changed %v, %v; want a reset", changed, err)
	}
	a.World().RunSafe(func() { changed, err = a.membership.changeScenarioLocked("other") })
	if err != nil || !changed || a.restart.Load().Scenario != "other" {
		t.Fatalf("another name: changed %v, %v; want a restart on it", changed, err)
	}
}

// TestTheFirstRestartAskedForStands: a run already being replaced refuses a scenario
// change rather than overwriting the restart it latched, a dialled join's above all.
func TestTheFirstRestartAskedForStands(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeScenario(t, root, "other", "A scenario this root holds.")
	a, err := NewHeadless(Config{Seed: fixtureSeed, Width: 80, Height: 24, Resources: resource.Options{Dir: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.cfg.Mode = ModePlay
	first := &restartRequest{Join: "127.0.0.1:7777"}
	if !a.replaceWith(first) {
		t.Fatal("the first restart was refused")
	}
	var changed bool
	a.World().RunSafe(func() { changed, err = a.membership.changeScenarioLocked("other") })
	if !errors.Is(err, errRunReplaced) || changed || a.restart.Load() != first {
		t.Fatalf("a later change: changed %v, %v; want it refused and the first restart kept", changed, err)
	}
}

// TestABotToldToLeaveIsNotSeatedAgain: a restart asked for while a removed bot is
// still leaving does not bring it back in the next run.
func TestABotToldToLeaveIsNotSeatedAgain(t *testing.T) {
	// Not parallel: paced runs against wall-clock deadlines.
	a := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	tickUntilCursor(t, a)
	stop := tickInBackground(a)
	t.Cleanup(func() { stop(); a.Close() })
	var err error
	a.World().RunSafe(func() { err = (sessionControl{a}).AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForSeats(t, a, 1)
	req := &restartRequest{}
	a.World().RunSafe(func() {
		if err = a.membership.dropSeat(int(a.membership.botSeats()[0].Slot)); err == nil {
			a.replaceWith(req)
		}
	})
	if err != nil || len(req.Bots) != 0 {
		t.Fatalf("dropped %v, then the restart seats %v; want none", err, req.Bots)
	}
}

// TestARunWithOnlyItsBotsChangesToAScenarioThatHoldsThem: a session its own bots are
// all of has no address to send them back to and needs none, since the next run
// seats them again; a scenario whose max_players is below its players is refused.
func TestARunWithOnlyItsBotsChangesToAScenarioThatHoldsThem(t *testing.T) {
	// Not parallel: paced runs against wall-clock deadlines.
	root := t.TempDir()
	writeScenario(t, root, "other", "A scenario this root holds.")
	solo := filepath.Join(writeScenario(t, root, "solo", "One player."), "scenario.toml")
	body, err := os.ReadFile(solo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(solo, append([]byte("max_players = 1\n"), body...), 0o600); err != nil {
		t.Fatal(err)
	}
	a := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	a.cfg.Mode, a.cfg.Resources.Dir = ModePlay, root
	tickUntilCursor(t, a)
	stop := tickInBackground(a)
	t.Cleanup(func() { stop(); a.Close() })
	a.World().RunSafe(func() { err = (sessionControl{a}).AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForCursors(t, a, 2)
	waitForSeats(t, a, 1)
	var changed bool
	a.World().RunSafe(func() { changed, err = a.membership.changeScenarioLocked("solo") })
	if err == nil || !strings.Contains(err.Error(), "at most 1") || changed || a.restart.Load() != nil {
		t.Fatalf("a one-player scenario over a run and its bot: changed %v, %v; want it refused", changed, err)
	}
	a.World().RunSafe(func() { changed, err = a.membership.changeScenarioLocked("other") })
	if req := a.restart.Load(); err != nil || !changed || req.Host != "" || !slices.Equal(req.Bots, []string{"roam"}) {
		t.Fatalf("changed %v, %v, restart %+v; want one with no address that seats roam again", changed, err, req)
	}
}

// TestARunWithOnlyItsBotsOpensTheirSessionToOthers: :host on a run whose session is
// its own bots' opens that session's door under the policy it names, rather than
// refusing it as a second session, and a person who dials in joins beside the bot.
func TestARunWithOnlyItsBotsOpensTheirSessionToOthers(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	a := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	tickUntilCursor(t, a)
	stop := tickInBackground(a)
	t.Cleanup(func() { stop(); a.Close() })
	var err error
	a.World().RunSafe(func() { err = (sessionControl{a}).AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForSeats(t, a, 1)
	a.World().RunSafe(func() { err = a.membership.beginHostingLocked("127.0.0.1:0", AuthorityHost) })
	if err != nil {
		t.Fatalf(":host beside its own bot: %v", err)
	}
	a.World().RunSafe(func() { err = a.membership.beginHostingLocked("127.0.0.1:0", "") })
	if err == nil || !strings.Contains(err.Error(), "already hosting") {
		t.Fatalf("a second :host = %v; want it refused as hosting", err)
	}
	pending, offer := dialSession(t, a.HostAddr())
	_ = pending.Close()
	if !offer.FixedAuthority {
		t.Fatal("the door opened under the bots' policy, not the one :host named")
	}
	mustSocketJoiner(t, a.HostAddr(), fixtureSeed, BotWidth, BotHeight)
	waitForCursors(t, a, 3)
}

// TestARunWithOnlyItsBotsJoinsAnotherSessionWithThem: :join on a run whose session is
// its own bots' dials rather than refusing, and the run it becomes is the one the
// restart loop builds from its live seats, not the configuration it dialled with, so
// the bot comes along through its holder.
func TestARunWithOnlyItsBotsJoinsAnotherSessionWithThem(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHeadless(t, 0x3023, BotWidth, BotHeight)
	tickUntilCursor(t, host)
	stopHost := tickInBackground(host)
	t.Cleanup(func() { stopHost(); host.Close() })
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("host: %v", err)
	}
	a := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	a.cfg.Mode = ModePlay // joinLocked serves Run's loop
	tickUntilCursor(t, a)
	stop := tickInBackground(a)
	replaced := sync.OnceFunc(func() { stop(); a.Close() })
	t.Cleanup(replaced)
	var err error
	a.World().RunSafe(func() { err = (sessionControl{a}).AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForSeats(t, a, 1)
	a.World().RunSafe(func() { err = a.membership.joinLocked(host.HostAddr()) })
	if err != nil {
		t.Fatalf(":join beside its own bot: %v", err)
	}
	for deadline := time.Now().Add(socketWait); a.membership.dialling.Load(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the dial never finished")
		}
	}
	req := a.restart.Load()
	if req == nil || req.dialled == nil || !slices.Equal(req.Bots, []string{"roam"}) {
		t.Fatalf("restart %+v; want a dialled join that seats roam again (status %q)", req, a.Context().GetStatusMessage())
	}
	// What Run does next: the replaced run goes, and the request builds the next
	// one, driven here rather than presented.
	replaced()
	next, _ := Config{}.restarted(req)
	driveBot(t, next, "patrol", nil)
	waitForCursors(t, host, 3)
}

// waitForSeats waits for a run's n bot seats to be admitted.
func waitForSeats(t *testing.T, a *App, n int) {
	t.Helper()
	seated := func() bool {
		b := a.membership.botSeats()
		return len(b) == n && !slices.ContainsFunc(b, func(s engine.BotSeat) bool { return s.Joining })
	}
	for deadline := time.Now().Add(socketWait); !seated(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("seats %+v; want %d admitted", a.membership.botSeats(), n)
		}
	}
}

// TestASuccessorLeadsTheSessionItInherited pins what a term means for every rule
// reserved for the authority: IsSessionCoordinator follows the authority the held
// term names, so a participant that inherited the session keeps its own reset and
// scenario change, being the only instance able to apply either.
func TestASuccessorLeadsTheSessionItInherited(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A3, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	successor := apps[1]

	var led bool
	successor.World().RunSafe(func() { led = successor.World().IsSessionCoordinator() })
	if led {
		t.Fatal("a guest reports itself the session's authority before any handoff")
	}

	successor.World().RunSafe(func() {
		successor.world.Resources.Network.Authority.Store(successor.world.LocalParticipant())
		led = successor.World().IsSessionCoordinator()
	})
	if !led {
		t.Fatal("the participant the term names is still refused the authority's rules")
	}
}

// TestBeginHostingRefusesASecondSession pins the one rule the command carries: a
// run is in one session or none.
func TestBeginHostingRefusesASecondSession(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	a := mustHeadless(t, 0x3018, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	if err := a.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	if err := a.BeginHosting("127.0.0.1:0"); err == nil {
		t.Fatal("a second :host was accepted")
	}
	if err := a.BeginHosting("not-an-address"); err == nil {
		t.Fatal("a malformed address was accepted")
	}
}

// TestAHostThatCannotBindLeavesTheRunSolo: the port is bound before anything a
// session needs is written, so a run refused one plays on exactly as it was.
func TestAHostThatCannotBindLeavesTheRunSolo(t *testing.T) {
	a := mustHeadless(t, 0x301A, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	if err := a.BeginHosting(busy.Addr().String()); err == nil {
		t.Fatal("hosting on a port in use was accepted")
	}
	if w := a.World(); w.SessionShared() || w.SessionBarrier() || a.membership.sessionTransport() != nil {
		t.Fatalf("a refused port left the run shared %t, barrier %t, transport %v",
			w.SessionShared(), w.SessionBarrier(), a.membership.sessionTransport())
	}
}

// TestAnInputsHorizonOpensWithTheSession: solo a press reaches the machine at the
// tick it was made at, and once the run is hosted it may arrive as late as the
// barrier allows, which the status a scenario judges misses by says from the next tick.
func TestAnInputsHorizonOpensWithTheSession(t *testing.T) {
	// Not parallel: this binds a real socket.
	a := mustHeadless(t, 0x301B, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	horizon := func() (v int64) {
		a.World().RunSafe(func() { v = a.World().Resources.Status.Ints.Get("engine.input_horizon").Load() })
		return v
	}
	if got := horizon(); got != 0 {
		t.Fatalf("a solo run's horizon is %d ticks, want none", got)
	}
	if err := a.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	a.Tick(1)
	if got := horizon(); got != parameter.NetworkApplyHorizonTicks {
		t.Fatalf("a hosted run's horizon is %d ticks, want %d", got, parameter.NetworkApplyHorizonTicks)
	}
}

// TestHostCarriesItsAuthorityPolicy: the word :host is given is the policy every
// offer it makes names, and a word that is neither is refused before binding.
func TestHostCarriesItsAuthorityPolicy(t *testing.T) {
	a := mustHeadless(t, 0x3019, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	var err error
	a.World().RunSafe(func() { err = a.membership.beginHostingLocked("127.0.0.1:0", "leader") })
	if err == nil {
		t.Fatal("an unknown authority policy was accepted")
	}
	a.World().RunSafe(func() { err = a.membership.beginHostingLocked("127.0.0.1:0", AuthorityHost) })
	if err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	stop := tickInBackground(a)
	defer stop()
	pending, offer := dialSession(t, a.HostAddr())
	defer pending.Close()
	if !offer.FixedAuthority {
		t.Fatal("a host opened with the host policy offered a migrating session")
	}
}

// mustSocketJoiner runs the whole guest side of a join against a live host: dial,
// identity, the start gate, the capture, the install and the catch-up. It is the
// production sequence, assembled here because Loop owns it in a run with a
// terminal and this harness owns its own ticks. configure edits the join config.
func mustSocketJoiner(t *testing.T, addr string, seed uint64, w, h int, configure ...func(*Config)) (*App, *link.SocketPort) {
	t.Helper()
	pending, offered := dialSession(t, addr)
	t.Cleanup(func() { _ = pending.Close() })

	joinCfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: w, Height: h}, offered)
	if err != nil {
		t.Fatalf("join config: %v", err)
	}
	for _, f := range configure {
		f(&joinCfg)
	}
	if joinCfg.Seed != seed {
		t.Fatalf("join config drew seed %#x, host runs %#x", joinCfg.Seed, seed)
	}
	guest, err := NewHeadless(joinCfg)
	if err != nil {
		t.Fatalf("join app: %v", err)
	}
	t.Cleanup(guest.Close)
	guest.membership.pendingJoin = pending
	guest.membership.sessionOffer = offered
	if err := guest.JoinAt(offered.Anchor); err != nil {
		_ = pending.Complete(err, network.JoinerReport{})
		t.Fatalf("join identity: %v", err)
	}
	if err := pending.Complete(nil, guest.membership.joinerReport()); err != nil {
		t.Fatalf("join reply: %v", err)
	}

	// The transport takes the stream only after the gate has read the world off it,
	// which is what startJoinSession does; the port then replays what the gate held.
	port := link.NewSocketPort(pending.TransportConfig())
	t.Cleanup(func() { _ = port.Close() })
	if err := guest.membership.startJoinSession(joinDeadline(socketWait)); err != nil {
		t.Fatalf("join startup: %v", err)
	}
	if err := port.Start(); err != nil {
		t.Fatalf("guest transport: %v", err)
	}
	guest.AttachTransport(port)
	guest.membership.activateNetworkSession()
	if err := guest.membership.resumeJoinedSession(); err != nil {
		t.Fatalf("join catch-up: %v", err)
	}
	return guest, port
}

// alignTicks advances whichever instance is behind until the two stand on one tick,
// so a shared-surface comparison is comparing worlds rather than instants.
func alignTicks(t *testing.T, a, b *App) {
	t.Helper()
	for range 4 * parameter.NetworkJoinLagTicks {
		at, bt := a.Position().Tick, b.Position().Tick
		switch {
		case at == bt:
			return
		case at < bt:
			a.Tick(1)
		default:
			b.Tick(1)
		}
	}
	t.Fatalf("could not align: a at tick %d, b at tick %d", a.Position().Tick, b.Position().Tick)
}

// settleCorrections stops a host's publication cadence and drains whatever it has
// already put on the wire, so a test whose subject is not the correction can compare
// two instances without one of them being re-based mid-comparison.
func settleCorrections(t *testing.T, host, guest *App) {
	t.Helper()
	host.corrections.Close()
	for range 8 {
		guest.Tick(1)
		host.Tick(1)
	}
	guest.ApplyPendingCorrections()
}

// waitForRosterPair ticks both instances until the arrival crossing has created the
// same shared cursor on each. The bound is wall time rather than a tick count: the
// arrival travels over a real socket, so counting ticks lets a loaded machine spend
// the whole budget before the frame has crossed loopback.
func waitForRosterPair(t *testing.T, host, guest *App) {
	t.Helper()
	deadline := time.Now().Add(socketWait) // [wall] a link bound, not a game one
	for time.Now().Before(deadline) {
		var hostE, guestE core.Entity
		host.World().RunSafe(func() { hostE = host.World().Resources.Player.Slot(1) })
		guest.World().RunSafe(func() { guestE = guest.World().Resources.Player.Slot(1) })
		if hostE != 0 && hostE == guestE {
			return
		}
		host.Tick(1)
		guest.Tick(1)
		time.Sleep(joinTestTickInterval / 5)
	}
	var hostE, guestE core.Entity
	host.World().RunSafe(func() { hostE = host.World().Resources.Player.Slot(1) })
	guest.World().RunSafe(func() { guestE = guest.World().Resources.Player.Slot(1) })
	t.Fatalf("the arrival crossing left slot 1 as entity %d on the host and %d on the guest",
		hostE, guestE)
}

// TestSessionRosterStartsAndRestartsEveryParticipant captures the two places the
// monitor script used its single player_entity variable as if it described the
// whole roster: lobby admission and the gameplay-wide defeat reset.
func TestSessionRosterStartsAndRestartsEveryParticipant(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA2A2, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	assertArmed := func(phase string) {
		t.Helper()
		for i, a := range apps {
			var heat int
			var energy int64
			var count int
			a.World().RunSafe(func() {
				w := a.World()
				count = w.Resources.Player.Count()
				if c, ok := w.Components.Heat.GetComponent(local[i]); ok {
					heat = c.Current
				}
				if c, ok := w.Components.Energy.GetComponent(local[i]); ok {
					energy = c.Current
				}
			})
			if count != len(apps) || heat != 10 || energy != 100 {
				t.Fatalf("%s participant %d: roster=%d heat=%d energy=%d, want %d/10/100",
					phase, i+1, count, heat, energy, len(apps))
			}
		}
	}

	assertArmed("start")

	// Each owner drains its own cursor, staggered, because that is the only way the
	// guard is really reached: defeat is owner-authored and crosses as a derived
	// artifact, so the two latches would flip a playout lead apart if the crossing
	// were an ordinary one. The reset replaces the world, so it has to be one reset
	// at one agreed tick rather than each instance rebuilding on its own.
	for i, a := range apps {
		for range i * 2 {
			tickAll(apps)
		}
		a.Context().PushEventOrigin(event.EventEnergySetRequest,
			&event.EnergySetPayload{Entity: local[i], Value: 0}, event.OriginDebug)
		a.Context().PushEventOrigin(event.EventHeatSetRequest,
			&event.HeatSetRequestPayload{Entity: local[i], Value: 0}, event.OriginDebug)
		a.Settle()
	}

	// A defeat starts a new game, so the reset is the request :n makes.
	resets := make([][]int, len(apps))
	tick := 0
	for i, a := range apps {
		a.SetDispatchTap(func(ev event.GameEvent) {
			if ev.Type == event.EventGameResetRequest {
				resets[i] = append(resets[i], tick)
			}
		})
	}
	for tick = 1; tick <= 12; tick++ {
		tickAll(apps)
	}
	for i, at := range resets {
		if len(at) != 1 {
			t.Fatalf("participant %d reset at ticks %v, want exactly once", i+1, at)
		}
		if at[0] != resets[0][0] {
			t.Fatalf("participant %d rebuilt the world at tick %d and participant 1 at %d",
				i+1, at[0], resets[0][0])
		}
	}

	local = localCursors(t, apps)
	assertArmed("global reset")
	assertMeshParity(t, apps, 6)
}

// TestLiveSessionRefusesAnInstanceLocalPause pins the operator policy: entering a
// local overlay or command mode may inspect a live session, but it must not stop
// that participant's production clock while its peers continue.
func TestLiveSessionRefusesAnInstanceLocalPause(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A1, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	apps[0].Context().SetPaused(true)
	apps[0].Settle()

	for i, a := range apps {
		if a.Context().TimeCtl.IsPaused() {
			t.Fatalf("participant %d paused inside a live session", i+1)
		}
	}
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}
	assertMeshParity(t, apps, 0)
}

// holdSession has the host :pause and ticks until every participant holds, failing
// unless all stand at one tick, which it returns.
func holdSession(t *testing.T, apps []*App) uint64 {
	t.Helper()
	mode.ExecuteCommand(apps[0].Context(), "pause")
	apps[0].Settle()
	for range parameter.NetworkBarrierMaxDelayTicks + 2 {
		tickAll(apps)
	}
	at := apps[0].Position().Tick
	for i, a := range apps {
		if !a.Context().TimeCtl.Held() || a.Position().Tick != at {
			t.Fatalf("participant %d: held %v at tick %d, want held at the host's %d",
				i+1, a.Context().TimeCtl.Held(), a.Position().Tick, at)
		}
	}
	return at
}

// TestAHoldStopsEveryParticipantAtOneTick: the host's :pause crosses, so host and
// guest stop after one agreed tick and stand there however often they are ticked.
func TestAHoldStopsEveryParticipantAtOneTick(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A2, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	at := holdSession(t, apps)
	for range 4 {
		tickAll(apps)
	}
	for i, a := range apps {
		if got := a.Position().Tick; got != at {
			t.Fatalf("participant %d ran to tick %d under a hold at %d", i+1, got, at)
		}
	}
	if got := apps[1].Context().GetStatusMessage(); got != "Paused by the host" {
		t.Fatalf("the guest was told %q", got)
	}
	assertMeshParity(t, apps, 0)
}

// TestOnlyTheHostHoldsALiveSession: what a hold stops is everyone, so starting one is
// the host's alone, as ending one is. A guest asking is told so and nothing crosses.
func TestOnlyTheHostHoldsALiveSession(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A3, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	guest := apps[1]
	mode.ExecuteCommand(guest.Context(), "pause")
	guest.Settle()
	if got := guest.Context().GetStatusMessage(); got != "Only the host can pause a live session" {
		t.Fatalf("a guest's :pause was told %q", got)
	}
	for range parameter.NetworkBarrierMaxDelayTicks + 2 {
		tickAll(apps)
	}
	for i, a := range apps {
		if a.Context().TimeCtl.Held() {
			t.Fatalf("participant %d holds on a guest's ask", i+1)
		}
	}
	holdSession(t, apps)
	mode.ExecuteCommand(guest.Context(), "pause off")
	guest.Settle()
	if got := guest.Context().GetStatusMessage(); got != "Only the host can resume a live session" {
		t.Fatalf("a guest's :pause off was told %q", got)
	}
	tickAll(apps)
	if !guest.Context().TimeCtl.Held() {
		t.Fatal("a guest ended the host's hold")
	}
}

// TestTheHostsNextTickResumesItsGuests: a held host sends nothing, so its first epoch
// after :pause off is the resume, and the guest runs on from the tick both stood at.
func TestTheHostsNextTickResumesItsGuests(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A4, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	at := holdSession(t, apps)
	mode.ExecuteCommand(apps[0].Context(), "pause off")
	apps[0].Settle()
	if apps[0].Context().TimeCtl.Held() || !apps[1].Context().TimeCtl.Held() {
		t.Fatalf("after :pause off the host holds %v and the guest %v, want the host alone running",
			apps[0].Context().TimeCtl.Held(), apps[1].Context().TimeCtl.Held())
	}
	tickAll(apps)
	for i, a := range apps {
		if a.Context().TimeCtl.Held() || a.Position().Tick != at+1 {
			t.Fatalf("participant %d: held %v at tick %d, want running at %d",
				i+1, a.Context().TimeCtl.Held(), a.Position().Tick, at+1)
		}
	}
	if got := apps[1].Context().GetStatusMessage(); got != "Resumed by the host" {
		t.Fatalf("the guest was told %q", got)
	}
	for range 4 {
		tickAll(apps)
	}
	assertMeshParity(t, apps, 0)
}

// TestAPauseOffBeforeTheHoldLandsEndsItAtTheLanding: :pause off asked while the host's
// hold still crosses ends it as it lands: the host takes the next tick and its
// guests, held at the agreed tick, resume on the epoch that follows; a :pause asked
// again before the landing keeps the hold.
func TestAPauseOffBeforeTheHoldLandsEndsItAtTheLanding(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A6, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	host, guest := apps[0], apps[1]
	say := func(cmd string) {
		mode.ExecuteCommand(host.Context(), cmd)
		host.Settle()
	}
	say("pause")
	say("pause off")
	for range parameter.NetworkBarrierMaxDelayTicks + 4 {
		tickAll(apps)
	}
	for i, a := range apps {
		if a.Context().TimeCtl.Held() {
			t.Fatalf("participant %d holds after a :pause off asked before the landing", i+1)
		}
	}
	if h, g := host.Context().GetStatusMessage(), guest.Context().GetStatusMessage(); h != "Resumed" || g != "Resumed by the host" {
		t.Fatalf("the host was told %q and the guest %q; want the landing resumed on both", h, g)
	}
	assertMeshParity(t, apps, 0)

	say("pause")
	say("pause off")
	say("pause")
	for range parameter.NetworkBarrierMaxDelayTicks + 4 {
		tickAll(apps)
	}
	for i, a := range apps {
		if !a.Context().TimeCtl.Held() {
			t.Fatalf("participant %d runs after the host asked for its pause again", i+1)
		}
	}
}

// TestANewGameEndsAHold: the reset crossed for :new applies at a tick a held session
// would never reach, so the host ends its hold first, and every participant starts
// the new game running.
func TestANewGameEndsAHold(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A5, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	holdSession(t, apps)
	run := apps[0].Position().Run
	mode.ExecuteCommand(apps[0].Context(), "n")
	apps[0].Settle()
	for range parameter.NetworkBarrierMaxDelayTicks + 4 {
		tickAll(apps)
	}
	for i, a := range apps {
		if a.Context().TimeCtl.Held() || a.Position().Run != run+1 {
			t.Fatalf("participant %d: held %v in run %d, want running the new game, run %d",
				i+1, a.Context().TimeCtl.Held(), a.Position().Run, run+1)
		}
	}
	assertMeshParity(t, apps, 0)
}

// startPress is a player's press on a waiting game, journaled as a typed one is
func startPress(a *App) (started bool) {
	a.World().RunSafe(func() {
		a.World().WithOrigin(event.OriginInput, func() { started = a.router.TakeStart(nil) })
	})
	a.Settle()
	return started
}

// TestANewGameWaitsForItsHostsPress: the host's :n with the wait on crosses with it,
// so host and guest stand at the new game's first tick however often they are
// ticked. A guest's press starts nothing there; the host's starts both, the guest on
// the host's next tick and with no notice, since the game moving says it.
func TestANewGameWaitsForItsHostsPress(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A7, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	host, guest := apps[0], apps[1]
	host.Context().WaitStart.Store(true)
	run := host.Position().Run
	mode.ExecuteCommand(host.Context(), "n")
	host.Settle()
	for range parameter.NetworkBarrierMaxDelayTicks + 4 {
		tickAll(apps)
	}
	at := host.Position()
	waiting := func(when string) {
		t.Helper()
		for i, a := range apps {
			if p := a.Position(); !a.Context().TimeCtl.Holds(engine.HoldStart) || p.Run != at.Run || p.Tick != at.Tick {
				t.Fatalf("%s, participant %d: waiting %v at %+v, want waiting at the host's %+v",
					when, i+1, a.Context().TimeCtl.Holds(engine.HoldStart), p, at)
			}
		}
	}
	if at.Run != run+1 {
		t.Fatalf("the host plays run %d after :n, want run %d", at.Run, run+1)
	}
	waiting("after :n")
	if startPress(guest) {
		t.Fatal("a guest's press started its host's game")
	}
	for range 4 {
		tickAll(apps)
	}
	waiting("after the guest's press")
	if !startPress(host) {
		t.Fatal("the host's press did not start its game")
	}
	tickAll(apps)
	for i, a := range apps {
		if a.Context().TimeCtl.Held() || a.Position().Tick != at.Tick+1 {
			t.Fatalf("participant %d: held %v at tick %d, want running at %d",
				i+1, a.Context().TimeCtl.Held(), a.Position().Tick, at.Tick+1)
		}
	}
	if got := guest.Context().GetStatusMessage(); got == "Resumed by the host" {
		t.Fatalf("the guest was told %q of a start", got)
	}
	for range 4 {
		tickAll(apps)
	}
	assertMeshParity(t, apps, 0)
}

// TestAScenariosResetNeverWaits: the reset a scenario asks for, as monitor's defeat
// does, starts its new game running whatever the setting says, while the player's
// :n under the same setting waits.
func TestAScenariosResetNeverWaits(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0xA1A8, 120, 40)
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)
	a.Context().WaitStart.Store(true)
	run := a.Position().Run
	a.World().RunSafe(func() { // as the scenario's EmitEvent builds it
		a.World().PushEvent(event.EventGameResetRequest, event.NewPayloadStruct(event.EventGameResetRequest))
	})
	a.Settle()
	a.Tick(2)
	if p := a.Position(); a.Context().TimeCtl.Held() || p.Run != run+1 || p.Tick == 0 {
		t.Fatalf("held %v at %+v after the scenario's reset, want run %d running", a.Context().TimeCtl.Held(), p, run+1)
	}
	mode.ExecuteCommand(a.Context(), "n")
	a.Settle()
	a.Tick(2)
	if p := a.Position(); !a.Context().TimeCtl.Holds(engine.HoldStart) || p.Run != run+2 || p.Tick != 0 {
		t.Fatalf("waiting %v at %+v after :n, want run %d waiting at its first tick",
			a.Context().TimeCtl.Holds(engine.HoldStart), p, run+2)
	}
}

// TestASuccessorKeepsTheHoldTheLostHostSet: a session held when its host goes stays
// held under its successor, whose own :pause resumes everyone. A successor a script
// or bot drives has nobody to give one, so it resumes as its window closes.
func TestASuccessorKeepsTheHoldTheLostHostSet(t *testing.T) {
	t.Parallel()
	for _, scripted := range []bool{false, true} {
		t.Run(map[bool]string{false: "player", true: "script"}[scripted], func(t *testing.T) {
			t.Parallel()
			apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
			localCursors(t, apps)
			primeRetention(t, apps)
			at := holdSession(t, apps)
			apps[1].cfg.scriptedSession = scripted
			closeParticipant(apps[0])
			survivors := apps[1:]
			if !settleAuthority(t, survivors, func() bool {
				return authorityOf(survivors[0]).Term > network.FirstTerm && authorityOf(survivors[1]).Term > network.FirstTerm
			}) {
				t.Fatalf("no succession: participant 2 holds %+v, participant 3 holds %+v",
					authorityOf(survivors[0]), authorityOf(survivors[1]))
			}
			for range 4 {
				tickAll(survivors)
			}
			if !scripted {
				for i, a := range survivors {
					if !a.Context().TimeCtl.Held() || a.Position().Tick != at {
						t.Fatalf("participant %d: held %v at tick %d, want still held at %d",
							i+2, a.Context().TimeCtl.Held(), a.Position().Tick, at)
					}
				}
				mode.ExecuteCommand(survivors[0].Context(), "pause off")
				survivors[0].Settle()
			}
			for range 4 {
				tickAll(survivors)
			}
			for i, a := range survivors {
				if a.Context().TimeCtl.Held() || a.Position().Tick <= at {
					t.Fatalf("participant %d: held %v at tick %d, want running past %d",
						i+2, a.Context().TimeCtl.Held(), a.Position().Tick, at)
				}
			}
		})
	}
}

// TestAProjectionRunsPastAHold: a correction read before a hold, installed after the
// session resumed, is projected across the hold's tick. Nothing in a projection may
// stop its clock, or the world it hands over would stand short of its label.
func TestAProjectionRunsPastAHold(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A6, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	tickAll(apps)
	before := mustCaptureShared(t, apps[0])
	holdSession(t, apps)
	mode.ExecuteCommand(apps[0].Context(), "pause off")
	apps[0].Settle()
	for range 4 {
		tickAll(apps)
	}
	installCorrection(t, apps[1], before)
	assertMeshParity(t, apps, 0)
}

// TestCoordinatorResetCrossesAndPreservesRoster reproduces :new as an operator
// injection on one instance. The session must restart at one agreed barrier tick,
// and the reset must rebuild the closed roster rather than the boot cursor alone.
func TestCoordinatorResetCrossesAndPreservesRoster(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA3A3, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	// The same command on a guest is operator-local refusal, not an artifact.
	mode.ExecuteCommand(apps[1].Context(), "n")
	apps[1].Settle()
	if got := apps[1].Position().Run; got != 0 {
		t.Fatalf("guest :new changed run to %d", got)
	}

	// A run longer than the lead, so the new run's crossings cannot wait out its count
	for range 100 {
		tickAll(apps)
	}
	mode.ExecuteCommand(apps[0].Context(), "n")
	apps[0].Settle()
	for range parameter.NetworkBarrierDelayTicks + 8 {
		tickAll(apps)
	}

	for i, a := range apps {
		if got := a.Position().Run; got != 1 {
			t.Fatalf("participant %d reset run=%d, want 1", i+1, got)
		}
		var count int
		a.World().RunSafe(func() { count = a.World().Resources.Player.Count() })
		if count != len(apps) {
			t.Fatalf("participant %d roster=%d after reset, want %d", i+1, count, len(apps))
		}
	}
	assertMeshParity(t, apps, 0)

	guest := apps[1].World().Resources.Player.Entity
	before := cursorPosition(apps[0], guest)
	apps[1].Inject(intentMotion(input.MotionRight, 3))
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}
	if got := cursorPosition(apps[0], guest); got.X != before.X+3 {
		t.Fatalf("the guest's move after the reset reached the host at column %d, want %d", got.X, before.X+3)
	}
}

// TestAResetKeepsAWraithWaiting: a new game rebuilds an away participant's cursor as
// a Wraith on every instance, and its wait goes on from the last game's rather than
// starting over, so no run of resets keeps it past its time.
func TestAResetKeepsAWraithWaiting(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA3A5, 3, [][2]int{{1, 2}, {1, 3}})
	localCursors(t, apps)
	apps[2].World().RunSafe(func() {
		apps[2].World().Resources.Network.Port.(*link.MeshPort).Close()
	})
	survivors := apps[:2]
	for range parameter.NetworkBarrierDelayTicks + 40 {
		tickAll(survivors)
	}
	waited := func(a *App) (d time.Duration, away bool) {
		a.World().RunSafe(func() {
			w := a.World()
			var wraith component.WraithComponent
			wraith, away = w.Components.Wraith.GetComponent(w.Resources.Player.Slot(2))
			d = wraith.Waited
		})
		return d, away
	}
	before, away := waited(apps[0])
	if !away || before <= 0 {
		t.Fatalf("before the reset the departed participant's cursor is away %t, waiting %v", away, before)
	}

	mode.ExecuteCommand(apps[0].Context(), "n")
	apps[0].Settle()
	for range parameter.NetworkBarrierDelayTicks + 8 {
		tickAll(survivors)
	}
	for i, a := range survivors {
		if got := a.Position().Run; got != 1 {
			t.Fatalf("participant %d reset run=%d, want 1", i+1, got)
		}
		if d, away := waited(a); !away || d < before {
			t.Fatalf("participant %d after the reset: slot 2 away %t, waiting %v; want at least the %v it had waited",
				i+1, away, d, before)
		}
	}
	assertMeshParity(t, survivors, 0)
}

// TestOneSharedQuasarTriggerProducesOneSpawn is the MainEscalate fan-out: one shared
// decision reaches every participant's player-domain FuseSystem. It is one logical
// fusion and therefore must yield one shared spawn request, not N.
func TestOneSharedQuasarTriggerProducesOneSpawn(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA4A4, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	spawns := make([]int, len(apps))
	for i, a := range apps {
		i := i
		a.SetDispatchTap(func(ev event.GameEvent) {
			if ev.Type == event.EventQuasarSpawnRequest {
				spawns[i]++
			}
		})
		a.World().Resources.Status.Ints.Get("kills.drain").Store(9)
	}
	// Participant 2 produces the tenth shared defeat. The crossing is delivered
	// to both FSMs, but its causal cursor elects only participant 2's fuse system.
	apps[1].Context().PushCrossing(event.EventDrainDefeated,
		&event.DrainDefeatedPayload{Entity: local[1]})
	apps[1].Settle()

	// Fusion waits 600ms; the driven clock advances 50ms per tick.
	for range 20 + parameter.NetworkBarrierDelayTicks {
		tickAll(apps)
	}
	for i, got := range spawns {
		if got != 1 {
			t.Fatalf("participant %d observed %d quasar spawn requests, want 1", i+1, got)
		}
	}
}

// TestExplosionPresentationStaysWithItsProducer is the presentation half of the
// explosion split. The combat artifact reaches the peer; the smoke center does not.
func TestExplosionPresentationStaysWithItsProducer(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA5A5, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	apps[0].Context().PushLocal(event.EventExplosionVisualRequest,
		&event.ExplosionVisualRequestPayload{X: 10, Y: 10, Radius: 4, Type: event.ExplosionTypeMissile})
	apps[0].Context().PushCrossing(event.EventExplosionRequest,
		&event.ExplosionRequestPayload{Entity: local[0], X: 10, Y: 10, Radius: 4})
	apps[0].Settle()
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}

	for i, a := range apps {
		var centers int
		a.World().RunSafe(func() { centers = a.World().Resources.Transient.ExplosionCount })
		want := 0
		if i == 0 {
			want = 1
		}
		if centers != want {
			t.Fatalf("participant %d has %d missile visual centers, want %d", i+1, centers, want)
		}
	}
}

// TestALostLinksIdentityIsFreeOnlyOnceItsDepartureApplies: a lost link is crossed
// as a departure that applies a lead later, so its identity is free only then; a
// dial assigned it in between, as a guest redialling at once was, is the
// participant that departure disconnects.
func TestALostLinksIdentityIsFreeOnlyOnceItsDepartureApplies(t *testing.T) {
	host := mustHeadless(t, fixtureSeed, 120, 40)
	defer host.Close()
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	stop := tickInBackground(host)
	defer stop()
	guest, port := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, 120, 40)
	waitForCursors(t, host, 2)
	stop()
	lost := link.PeerID(guest.World().LocalParticipant())
	_ = port.Close()
	for deadline := time.Now().Add(socketWait); host.membership.sessionTransport().PeerCount() > 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the host never lost the guest's link")
		}
	}
	host.Tick(1)

	pending, offer := dialSession(t, host.HostAddr())
	defer pending.Close()
	if offer.Assigned == lost {
		t.Fatalf("identity %d was reassigned while its departure was still to apply", lost)
	}
}

// TestASessionCommandIsGatedByTheRoleARunHolds is the join matrix's role and
// command table: the role each kind of run holds, what :join, :host and :leave
// answer in it, and how :session names it. A run whose session is its own bots'
// takes :join and :host and has nothing to leave, one its session left may join
// another or leave, and a guest that inherited the term is a successor, which may
// open a door with :host.
func TestASessionCommandIsGatedByTheRoleARunHolds(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	ticking := func(a *App) *App {
		tickUntilCursor(t, a)
		stop := tickInBackground(a)
		t.Cleanup(func() { stop(); a.Close() })
		return a
	}
	withBot := func(a *App) *App {
		var err error
		a.World().RunSafe(func() { err = (sessionControl{a}).AddBot("roam") })
		if err != nil {
			t.Fatal(err)
		}
		waitForSeats(t, a, 1)
		return a
	}
	hosting := func(a *App) *App {
		if err := a.BeginHosting("127.0.0.1:0"); err != nil {
			t.Fatal(err)
		}
		return a
	}
	host := hosting(ticking(mustHeadless(t, fixtureSeed, BotWidth, BotHeight)))
	guest, _ := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, BotWidth, BotHeight)
	guest.cfg.JoinAddress = host.HostAddr() // what -join sets; a headless run refuses it
	gone := mustHeadless(t, 0x3104, BotWidth, BotHeight)
	tickUntilCursor(t, gone)
	stopGone := tickInBackground(gone)
	leave := sync.OnceFunc(func() { stopGone(); gone.Close() })
	t.Cleanup(leave)
	hosting(gone)
	left, leftPort := mustSocketJoiner(t, gone.HostAddr(), 0x3104, BotWidth, BotHeight)
	left.cfg.JoinAddress = gone.HostAddr()

	for _, row := range []struct {
		kind              string
		run               func() *App
		role              sessionRole
		join, host, leave string // the refusal each names; empty takes the command
		line              string // how :session opens
	}{
		{"solo", func() *App { return ticking(mustHeadless(t, 0x3101, BotWidth, BotHeight)) },
			roleSolo, "", "", "in no session", "Solo run"},
		{"solo with its bots", func() *App { return withBot(ticking(mustHeadless(t, 0x3102, BotWidth, BotHeight))) },
			roleSeats, "", "", "only its own bots", "Session hosting, "},
		{"hosting", func() *App { return host },
			roleHost, "already in a session", "already hosting", "", "Session hosting 127.0.0.1:"},
		{"hosting with its bots", func() *App { return withBot(hosting(ticking(mustHeadless(t, 0x3103, BotWidth, BotHeight)))) },
			roleHost, "already in a session", "already hosting", "", "Session hosting 127.0.0.1:"},
		{"joined", func() *App { return guest },
			roleGuest, "already in a session", "already in a session", "", "Session joined " + host.HostAddr() + ", "},
		{"inherited the term", func() *App {
			guest.World().RunSafe(func() {
				guest.world.Resources.Network.Authority.Store(guest.world.LocalParticipant())
			})
			return guest
		}, roleSuccessor, "already in a session", "", "", "Session hosting, "},
		{"its session left", func() *App {
			leave()
			for deadline := time.Now().Add(socketWait); leftPort.PeerCount() > 0; time.Sleep(time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the guest never lost its host")
				}
			}
			return left
		}, roleLeft, "", "already in a session", "", "Session joined " + gone.HostAddr() + ", "},
	} {
		a := row.run()
		var role sessionRole
		var join, hostErr, leaveErr error
		var line string
		a.World().RunSafe(func() {
			role = a.membership.sessionRoleLocked()
			mode := a.cfg.Mode
			a.cfg.Mode = ModePlay // :join and :leave serve Run's loop, which a headless run lacks
			join, leaveErr = a.membership.joinErrorLocked(), a.membership.leaveErrorLocked()
			a.cfg.Mode = mode
			hostErr = a.membership.hostErrorLocked()
			line = a.membership.sessionSummaryLocked()
		})
		if role != row.role {
			t.Errorf("a run %s holds role %d; want %d", row.kind, role, row.role)
		}
		for _, c := range []struct {
			command string
			err     error
			want    string
		}{{":join", join, row.join}, {":host", hostErr, row.host}, {":leave", leaveErr, row.leave}} {
			if c.want == "" && c.err != nil || c.want != "" && (c.err == nil || !strings.Contains(c.err.Error(), c.want)) {
				t.Errorf("%s in a run %s: %v; want %q", c.command, row.kind, c.err, cmp.Or(c.want, "taken"))
			}
		}
		if !strings.HasPrefix(line, row.line) {
			t.Errorf(":session in a run %s reads %q; want it to open %q", row.kind, line, row.line)
		}
	}
}

// leaveAs serves :leave as Run's loop does, for a harness that stands in for it. A
// harness has no loop, the one refusal its role does not decide, and may be ticking:
// the mode the refusal reads is left as it is.
func leaveAs(t *testing.T, a *App) {
	t.Helper()
	var why string
	a.World().RunSafe(func() { why = sessionRoles[a.membership.sessionRoleLocked()].leave })
	if why != "" || !a.membership.leaving.CompareAndSwap(false, true) {
		t.Fatalf(":leave refused: %q", why)
	}
	a.membership.leaveSession()
}

// TestALeavingGuestPlaysOnInASessionOfItsOwn: a guest that leaves goes on with the
// world as it stands, authoring a session of its own under the identity it had and
// the term after the one it held. Nobody it left is in that world any more, its
// host keeps its cursor as a Wraith, and nothing names the session it left, its
// -players included, so :host and :join are open to it.
func TestALeavingGuestPlaysOnInASessionOfItsOwn(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	t.Cleanup(host.Close)
	tickUntilCursor(t, host)
	stopTicking := tickInBackground(host)
	t.Cleanup(stopTicking)
	host.cfg.Participants = 3
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	guest, _ := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, BotWidth, BotHeight)
	guest.cfg.JoinAddress = host.HostAddr() // what -join sets; a headless run refuses it
	stopTicking()
	waitForRosterPair(t, host, guest)
	id, term := guest.membership.localParticipant(), guest.membership.authorityTerm()
	var own core.Entity
	guest.World().RunSafe(func() { own = guest.world.Resources.Player.Slot(1) })

	leaveAs(t, guest)
	var role sessionRole
	var authoring bool
	var left, kept core.Entity
	var hostErr, joinErr, leaveErr error
	guest.World().RunSafe(func() {
		role, authoring = guest.membership.sessionRoleLocked(), guest.world.IsSessionCoordinator()
		left, kept = guest.world.Resources.Player.Slot(0), guest.world.Resources.Player.Slot(1)
		mode := guest.cfg.Mode
		guest.cfg.Mode = ModePlay
		joinErr, leaveErr = guest.membership.joinErrorLocked(), guest.membership.leaveErrorLocked()
		guest.cfg.Mode = mode
		hostErr = guest.membership.hostErrorLocked()
	})
	if role != roleSeats || !authoring || guest.membership.localParticipant() != id || guest.membership.authorityTerm() != term+1 {
		t.Fatalf("after leaving, participant %d holds role %d, authoring %t, term %d; want role %d authoring as %d at term %d",
			guest.membership.localParticipant(), role, authoring, guest.membership.authorityTerm(), roleSeats, id, term+1)
	}
	if left != 0 || kept != own {
		t.Fatalf("after leaving, slot 0 holds %d and slot 1 %d; want the host gone and the guest's own %d kept", left, kept, own)
	}
	if hostErr != nil || joinErr != nil || leaveErr == nil {
		t.Fatalf("after leaving, :host %v, :join %v, :leave %v; want the first two taken and nothing left to leave", hostErr, joinErr, leaveErr)
	}
	if got := guest.membership.playerCapacity(); got != min(parameter.MaxPlayers, guest.scenario.MaxPlayers) {
		t.Fatalf("after leaving, the guest holds its own session to %d players; want no ceiling but the scenario's", got)
	}
	resume := tickInBackground(host)
	t.Cleanup(resume)
	waitForWraiths(t, host, 1)
}

// TestAForkGoesOnInASessionOfItsOwn: a run its session gives up, here a guest whose
// pinned host left, is taken on as :leave takes it: a session of its own under the
// identity it had and the next term, :host and :join open to it, and the loss said
// as one. The host's place stays, its cursor a Wraith the fork gives back to the
// claim it waits under.
func TestAForkGoesOnInASessionOfItsOwn(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	t.Cleanup(host.Close)
	tickUntilCursor(t, host)
	stopTicking := tickInBackground(host)
	t.Cleanup(stopTicking)
	var err error
	host.World().RunSafe(func() { err = host.membership.beginHostingLocked("127.0.0.1:0", AuthorityHost) })
	if err != nil {
		t.Fatal(err)
	}
	guest, _ := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, BotWidth, BotHeight)
	guest.World().RunSafe(func() {
		guest.cfg.JoinAddress = host.HostAddr() // what -join sets; a headless run refuses it
		guest.cfg.Mode = ModePlay               // the mode whose loop takes a fork on
	})
	stopTicking()
	waitForRosterPair(t, host, guest)
	id, term, token := guest.membership.localParticipant(), guest.membership.authorityTerm(), host.membership.claim.Load().Token
	held := places(guest, everyone)

	leaveAs(t, host)
	stopGuest := tickInBackground(guest)
	for deadline := time.Now().Add(socketWait); !guest.membership.leaving.Load() && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	stopGuest()
	if !guest.membership.leaving.Load() {
		t.Fatal("the guest lost its pinned host, and nothing asked its loop to take it on")
	}
	guest.membership.leaveSession() // as Run's loop does

	var role sessionRole
	var authoring bool
	var hostErr, joinErr, leaveErr error
	guest.World().RunSafe(func() {
		role, authoring = guest.membership.sessionRoleLocked(), guest.world.IsSessionCoordinator()
		joinErr, leaveErr = guest.membership.joinErrorLocked(), guest.membership.leaveErrorLocked()
		hostErr = guest.membership.hostErrorLocked()
	})
	if role != roleSeats || !authoring || guest.membership.localParticipant() != id || guest.membership.authorityTerm() != term+1 {
		t.Fatalf("after its fork, participant %d holds role %d, authoring %t, term %d; want role %d authoring as %d at term %d",
			guest.membership.localParticipant(), role, authoring, guest.membership.authorityTerm(), roleSeats, id, term+1)
	}
	if got := places(guest, everyone); len(held) != 2 || !maps.Equal(got, held) || !wraithIn(guest, 0) {
		t.Fatalf("after its fork the guest holds %v (entity, participant by slot), the host's a Wraith %t; want %v, the host's a Wraith",
			got, wraithIn(guest, 0), held)
	}
	if hostErr != nil || joinErr != nil || leaveErr == nil {
		t.Fatalf("after its fork, :host %v, :join %v, :leave %v; want the first two taken and nothing left to leave", hostErr, joinErr, leaveErr)
	}
	if msg := guest.Context().GetStatusMessage(); msg != "Host connection lost; playing on in a session of your own" {
		t.Fatalf("after its fork the guest says %q; want the loss said", msg)
	}
	back, err := guest.membership.assignFor(guest.JoinAnchor(), 0, []string{token})
	if err != nil || uint64(back.Assigned) != held[0][1] || assignedSlot(back) != 0 {
		t.Fatalf("the host's claim was given %d on slot %d, %v; want its own place, %d on slot 0", back.Assigned, assignedSlot(back), err, held[0][1])
	}
}

// TestAForkKeepsTheHoldItsLostHostSet: a run its session gave up goes on in a session
// of its own paused, as its host left it, and says so, for its own :pause to end; a
// freeze, which no host set, ends there.
func TestAForkKeepsTheHoldItsLostHostSet(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		stop   func(*engine.GameContext)
		held   bool
		notice string
	}{
		{"the host's hold", func(ctx *engine.GameContext) { ctx.Hold(true) }, true, "Host connection lost; paused in a session of your own"},
		{"a freeze", func(ctx *engine.GameContext) { ctx.HoldAs(engine.HoldFreeze) }, false, "Host connection lost; playing on in a session of your own"},
	} {
		a := mustHeadless(t, fixtureSeed, 120, 40)
		t.Cleanup(a.Close)
		tickUntilCursor(t, a)
		var err error
		a.World().RunSafe(func() {
			if err = a.membership.openSessionLocked(nil); err == nil {
				tt.stop(a.ctx)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		forkCell(a.world).Store(true) // as giveUp leaves it
		a.membership.leaving.Store(true)
		a.membership.leaveSession()
		if held, msg := a.ctx.TimeCtl.Held(), a.ctx.GetStatusMessage(); held != tt.held || msg != tt.notice {
			t.Errorf("a fork under %s goes on held %t, saying %q; want held %t, saying %q", tt.name, held, msg, tt.held, tt.notice)
		}
	}
}

// TestASessionARunOpensHasNobodyAwayFromIt: who was away from the session a run
// left, or on the way back to it, stays with that session, so the next one hands an
// identity out as somebody new: nobody there is yielded, dismissed or brought back.
func TestASessionARunOpensHasNobodyAwayFromIt(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.membership.sessionMu.Lock()
	a.membership.awayOrder, a.membership.yielding = []link.PeerID{2}, 2
	a.membership.returning = map[link.PeerID]returnPlace{2: {entry: network.RosterEntry{ID: 2, Slot: 1, Away: true}}}
	a.membership.sessionMu.Unlock()
	a.membership.dismissWraith.Store(2)
	var err error
	a.World().RunSafe(func() { err = a.membership.openSessionLocked(nil) })
	if err != nil {
		t.Fatal(err)
	}
	a.membership.sessionMu.Lock()
	away, yielding, back := len(a.membership.awayOrder), a.membership.yielding, len(a.membership.returning)
	a.membership.sessionMu.Unlock()
	if away != 0 || yielding != 0 || back != 0 || a.membership.dismissWraith.Load() != 0 {
		t.Fatalf("the session opened with %d away, %d yielding, %d on the way back and %d to dismiss; want nobody",
			away, yielding, back, a.membership.dismissWraith.Load())
	}
}

// TestALeavingHostTakesItsBotsAndShutsItsDoor: a host that leaves takes its bots
// along, seated again in the session it goes on in on the places they had, and shuts
// the door its guests came in by: they lose their host, and nothing answers a dial
// at its address.
func TestALeavingHostTakesItsBotsAndShutsItsDoor(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	t.Cleanup(host.Close)
	tickUntilCursor(t, host)
	stopTicking := tickInBackground(host)
	t.Cleanup(stopTicking)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	addr := host.HostAddr()
	var err error
	host.World().RunSafe(func() { err = (sessionControl{host}).AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForSeats(t, host, 1)
	seated := host.membership.botSeats()[0]
	_, port := mustSocketJoiner(t, addr, fixtureSeed, BotWidth, BotHeight)
	waitForCursors(t, host, 3)
	itsBots := heldBy(host.membership.localParticipant())
	left := places(host, itsBots)

	leaveAs(t, host)
	waitForSeats(t, host, 1)
	if again := host.membership.botSeats()[0]; again.ID == seated.ID {
		t.Fatalf("the bot kept seat %d through the leave; want it seated again", again.ID)
	}
	waitForCursors(t, host, 2) // its own and its bot's: the guest is not in its world
	waitForWraiths(t, host, 0)
	if got := places(host, itsBots); len(left) != 1 || !maps.Equal(got, left) {
		t.Fatalf("after the leave the host's bot holds %v (entity, participant by slot); want the place it had, %v", got, left)
	}
	if got := host.HostAddr(); got != "" {
		t.Fatalf("the host still answers at %q after leaving", got)
	}
	for deadline := time.Now().Add(socketWait); port.PeerCount() > 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the guest never lost its host")
		}
	}
	if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		_ = c.Close()
		t.Fatalf("a dial to %s after the host left connected", addr)
	}
}

// TestADialWhileItsHostLeavesIsRefusedAsTheSessionEnding: the term it would be
// offered ends with the leave and the door it came in by shuts, so the dial is told
// the session is ending, which a joiner does not retry against.
func TestADialWhileItsHostLeavesIsRefusedAsTheSessionEnding(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, fixtureSeed, 120, 40)
	defer host.Close()
	host.AttachTransport(network.NewMesh().Node(1))
	host.membership.leaving.Store(true)
	_, err := host.membership.assignFor(host.JoinAnchor(), 0, nil)
	var refusal *network.Refusal
	if !errors.As(err, &refusal) || refusal.Code != network.RefusalEnding {
		t.Fatalf("a dial while its host leaves was answered %v; want the session ending", err)
	}
}

// An abandoned relayed join leaves by a departure crossing that applies a lead
// later, so its identity is free only then: a dial assigned it in between, as a
// person joining right after a dropped probe of the relay's address was, is the
// participant the crossing removes.
func TestAnAbandonedRelayedIdentityIsFreeOnlyOnceItsDepartureApplies(t *testing.T) {
	host := mustHeadless(t, fixtureSeed, 120, 40)
	defer host.Close()
	host.AttachTransport(network.NewMesh().Node(1))
	anchor := host.JoinAnchor()
	relay, err := host.membership.assignFor(anchor, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := host.membership.assignFor(anchor, relay.Assigned, nil)
	if err != nil {
		t.Fatal(err)
	}
	host.world.RunSafe(func() { host.membership.abandonRelayedLocked(relay.Assigned, leaf.Assigned) })
	next, err := host.membership.assignFor(anchor, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.Assigned == leaf.Assigned {
		t.Fatalf("identity %d was reassigned while its departure was still to apply", leaf.Assigned)
	}
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	if again, err := host.membership.assignFor(anchor, 0, nil); err != nil || again.Assigned != leaf.Assigned {
		t.Fatalf("after the departure applied, assigned %d, %v; want %d back", again.Assigned, err, leaf.Assigned)
	}
}

// TestAFullSessionGivesTheLongestWaitingWraithsPlaceToANewcomer: a dial a full
// session cannot otherwise admit is told to retry in a moment while the participant
// away longest is dismissed, and then takes that place; the other Wraith keeps its.
func TestAFullSessionGivesTheLongestWaitingWraithsPlaceToANewcomer(t *testing.T) {
	host := mustHeadless(t, fixtureSeed, 120, 40)
	defer host.Close()
	host.cfg.Participants = 3
	host.AttachTransport(network.NewMesh().Node(1))
	tickUntilCursor(t, host)
	anchor := host.JoinAnchor()
	slotOf := func(o network.SessionOffer) uint8 {
		i := slices.IndexFunc(o.Roster, func(p network.RosterEntry) bool { return p.ID == o.Assigned })
		return o.Roster[i].Slot
	}
	var guests []network.SessionOffer
	for range 2 {
		offer, err := host.membership.assignFor(anchor, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		host.world.RunSafe(func() { host.membership.crossArrivalLocked(offer.Assigned, slotOf(offer)) })
		guests = append(guests, offer)
	}
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	for _, g := range guests {
		host.membership.crossDeparture(g.Assigned, slotOf(g))
		host.Tick(1)
	}
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	away := func(g network.SessionOffer) (wraith bool) {
		host.world.RunSafe(func() {
			wraith = host.world.Components.Wraith.HasEntity(host.world.Resources.Player.Slot(slotOf(g)))
		})
		return wraith
	}
	if !away(guests[0]) || !away(guests[1]) {
		t.Fatal("the departed guests' cursors are not waiting as Wraiths")
	}

	for range 2 {
		if _, err := host.membership.assignFor(anchor, 0, nil); !errors.Is(err, &network.Refusal{Code: network.RefusalYielding}) ||
			network.RetryOf(err) != network.RetrySoon {
			t.Fatalf("a full session with Wraiths answered a newcomer with %v; want a refusal to retry soon", err)
		}
	}
	host.Tick(parameter.NetworkBarrierDelayTicks + 2)
	offer, err := host.membership.assignFor(anchor, 0, nil)
	if err != nil || offer.Assigned != guests[0].Assigned || slotOf(offer) != slotOf(guests[0]) {
		t.Fatalf("after the yield the newcomer was given %+v, %v; want participant %d's place",
			offer.Assigned, err, guests[0].Assigned)
	}
	if !away(guests[1]) {
		t.Fatal("the Wraith that waited less gave up its place too")
	}
}

// assignedSlot is the slot an offer gives the participant it is addressed to.
func assignedSlot(o network.SessionOffer) uint8 {
	entry, _ := o.Entry(o.Assigned)
	return entry.Slot
}

// wraithIn reports whether a's cursor in slot waits as a Wraith.
func wraithIn(a *App, slot uint8) (away bool) {
	a.world.RunSafe(func() { away = a.world.Components.Wraith.HasEntity(a.world.Resources.Player.Slot(slot)) })
	return away
}

// awayGuest is a full session of two whose guest left under a claim, its cursor
// waiting as a Wraith: the host, the guest's offer and the token it comes back with.
func awayGuest(t *testing.T) (*App, network.SessionOffer, string) {
	t.Helper()
	host := mustHeadless(t, fixtureSeed, 120, 40)
	t.Cleanup(host.Close)
	host.cfg.Participants = 2
	host.AttachTransport(network.NewMesh().Node(1))
	tickUntilCursor(t, host)
	guest, err := host.membership.assignFor(host.JoinAnchor(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	token := network.NewClaimToken()
	if err := host.membership.noteJoinerReport(guest.Assigned, network.JoinerReport{Claim: network.ClaimDigest(token)}); err != nil {
		t.Fatal(err)
	}
	host.world.RunSafe(func() { host.membership.crossArrivalLocked(guest.Assigned, assignedSlot(guest)) })
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	host.membership.crossDeparture(guest.Assigned, assignedSlot(guest))
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	if !wraithIn(host, assignedSlot(guest)) {
		t.Fatal("the departed guest's cursor is not waiting as a Wraith")
	}
	return host, guest, token
}

// TestAFullSessionTakesBackAPlayerWhoseWraithKeepsItsPlace: a dial naming the claim
// a Wraith waits under is given that place at once, a full session's too, and no
// newcomer is meanwhile; its arrival takes the cursor back with what its player
// brought of the game it left.
func TestAFullSessionTakesBackAPlayerWhoseWraithKeepsItsPlace(t *testing.T) {
	host, guest, token := awayGuest(t)
	anchor := host.JoinAnchor()
	back, err := host.membership.assignFor(anchor, 0, []string{network.NewClaimToken(), token})
	if err != nil || back.Assigned != guest.Assigned || assignedSlot(back) != assignedSlot(guest) {
		t.Fatalf("the returning player was given %d, %v; want its own place, %d", back.Assigned, err, guest.Assigned)
	}
	if _, err := host.membership.assignFor(anchor, 0, nil); !errors.Is(err, &network.Refusal{Code: network.RefusalFull}) {
		t.Fatalf("a newcomer was answered %v while the place came back; want the session full", err)
	}
	record := &event.CursorRecord{Energy: 4321, HitPoints: 37}
	if err := host.membership.noteJoinerReport(back.Assigned, network.JoinerReport{Claim: "ignored", Record: record}); err != nil {
		t.Fatal(err)
	}
	host.world.RunSafe(func() { host.membership.crossArrivalLocked(back.Assigned, assignedSlot(back)) })
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	var energy int64
	var health int
	host.world.RunSafe(func() {
		e := host.world.Resources.Player.Slot(assignedSlot(back))
		en, _ := host.world.Components.Energy.GetComponent(e)
		combat, _ := host.world.Components.Combat.GetComponent(e)
		energy, health = en.Current, combat.HitPoints
	})
	if wraithIn(host, assignedSlot(back)) || energy != record.Energy || health != record.HitPoints {
		t.Fatalf("after the arrival the place is a Wraith %t, with energy %d and health %d; want it taken back with %+v",
			wraithIn(host, assignedSlot(back)), energy, health, *record)
	}
}

// TestAWraithWaitsUnderTheClaimItsRosterNames: a Wraith keeps the participant and
// holder it left with and takes the claim the roster names it under, as a session a
// run opens names its bots' afresh; an entry naming somebody else leaves it as it was.
func TestAWraithWaitsUnderTheClaimItsRosterNames(t *testing.T) {
	host, guest, token := awayGuest(t)
	slot := assignedSlot(guest)
	cursor := func() (c component.CursorComponent) {
		host.world.RunSafe(func() { c, _ = host.world.Components.Cursor.GetComponent(host.world.Resources.Player.Slot(slot)) })
		return c
	}
	bind := func(p network.RosterEntry) {
		host.world.RunSafe(func() {
			host.membership.bindCursorOwnersLocked([]network.RosterEntry{p}, engine.CoordinatorParticipant)
		})
	}
	bind(network.RosterEntry{ID: guest.Assigned + 1, Slot: slot, Claim: network.ClaimDigest(network.NewClaimToken())})
	if c := cursor(); c.Claim != network.ClaimDigest(token) || c.PeerID != uint32(guest.Assigned) {
		t.Fatalf("an entry naming somebody else left the Wraith as participant %d under %q; want %d under its own claim", c.PeerID, c.Claim, guest.Assigned)
	}
	carried := network.ClaimDigest(network.NewClaimToken())
	bind(network.RosterEntry{ID: guest.Assigned, Slot: slot, Holder: 9, Claim: carried})
	if c := cursor(); c.Claim != carried || c.PeerID != uint32(guest.Assigned) || c.Holder != 0 || !wraithIn(host, slot) {
		t.Fatalf("the Wraith is participant %d held by %d under %q, a Wraith %t; want %d held by nobody under %q, still waiting",
			c.PeerID, c.Holder, c.Claim, wraithIn(host, slot), guest.Assigned, carried)
	}
}

// TestAReturnThatDoesNotCompleteLeavesItsWraithWaiting: a place a claim took back
// goes back to its Wraith when the join fails, directly or through a relay, its
// wait no longer held off; the claim takes it again.
func TestAReturnThatDoesNotCompleteLeavesItsWraithWaiting(t *testing.T) {
	host, guest, token := awayGuest(t)
	anchor := host.JoinAnchor()
	took := func(holder link.PeerID) link.PeerID {
		t.Helper()
		back, err := host.membership.assignFor(anchor, holder, []string{token})
		if err != nil || back.Assigned != guest.Assigned {
			t.Fatalf("the claim took %d, %v; want participant %d's place", back.Assigned, err, guest.Assigned)
		}
		if !host.membership.wraithKept(uint32(back.Assigned)) {
			t.Fatal("the Wraith's wait may run out while its player comes back")
		}
		return back.Assigned
	}
	host.membership.releaseParticipant(took(0))
	id := took(engine.CoordinatorParticipant)
	host.world.RunSafe(func() { host.membership.abandonRelayedLocked(engine.CoordinatorParticipant, id) })
	host.Tick(parameter.NetworkBarrierDelayTicks + 2)
	if host.membership.wraithKept(uint32(id)) || !wraithIn(host, assignedSlot(guest)) {
		t.Fatalf("after a failed return through a relay the Wraith waits %t, held off %t; want it waiting, its wait running",
			wraithIn(host, assignedSlot(guest)), host.membership.wraithKept(uint32(id)))
	}
	took(0)
}

// TestAClaimOnAPlaceStillLinkedClosesTheLinkItLeft: a dial naming the claim of a
// place whose link the host still holds is told to retry in a moment, and that link
// is closed, so its departure crosses and the retry takes the place back.
func TestAClaimOnAPlaceStillLinkedClosesTheLinkItLeft(t *testing.T) {
	host := mustHeadless(t, fixtureSeed, 120, 40)
	defer host.Close()
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	host.AttachTransport(mesh.Node(1))
	tickUntilCursor(t, host)
	guest, err := host.membership.assignFor(host.JoinAnchor(), 0, nil)
	if err != nil || guest.Assigned != 2 {
		t.Fatalf("the linked guest was assigned %d, %v; want participant 2", guest.Assigned, err)
	}
	token := network.NewClaimToken()
	if err := host.membership.noteJoinerReport(guest.Assigned, network.JoinerReport{Claim: network.ClaimDigest(token)}); err != nil {
		t.Fatal(err)
	}
	host.world.RunSafe(func() { host.membership.crossArrivalLocked(guest.Assigned, assignedSlot(guest)) })
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)

	_, err = host.membership.assignParticipant([]string{token})
	if !errors.Is(err, &network.Refusal{Code: network.RefusalReturning}) || network.RetryOf(err) != network.RetrySoon {
		t.Fatalf("a claim on a place still linked was answered %v; want a refusal to retry soon", err)
	}
	if mesh.Node(1).Connected(2) {
		t.Fatal("the link the returning player left is still open")
	}
	host.Tick(parameter.NetworkBarrierDelayTicks + 2)
	back, err := host.membership.assignParticipant([]string{token})
	if err != nil || back.Assigned != guest.Assigned || assignedSlot(back) != assignedSlot(guest) {
		t.Fatalf("the retry was given %d, %v; want participant %d's place", back.Assigned, err, guest.Assigned)
	}
}

// TestASuccessorOrdersWhoIsAwayByTheirWaits: a session taken over gives a newcomer
// the place of whoever has waited longest, read off the Wraiths, not the roster.
func TestASuccessorOrdersWhoIsAwayByTheirWaits(t *testing.T) {
	host := mustHeadless(t, fixtureSeed, 120, 40)
	defer host.Close()
	host.AttachTransport(network.NewMesh().Node(1))
	tickUntilCursor(t, host)
	var guests []network.SessionOffer
	for range 2 {
		offer, err := host.membership.assignFor(host.JoinAnchor(), 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		host.world.RunSafe(func() { host.membership.crossArrivalLocked(offer.Assigned, assignedSlot(offer)) })
		guests = append(guests, offer)
	}
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	for _, g := range slices.Backward(guests) {
		host.membership.crossDeparture(g.Assigned, assignedSlot(g))
		host.Tick(4)
	}
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	var order []link.PeerID
	roster := host.instance().WorldRoster()
	host.world.RunSafe(func() { order = host.membership.awayOrderLocked(roster) })
	if want := []link.PeerID{guests[1].Assigned, guests[0].Assigned}; !slices.Equal(order, want) {
		t.Fatalf("away longest first reads %v, want %v", order, want)
	}
}

// TestAHostTakesJoinsAtEveryDoor: a host naming a tcp door and a ws door serves one
// session at both, and a joiner at the second is seated as one at the first is.
func TestAHostTakesJoinsAtEveryDoor(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	tickUntilCursor(t, host)
	if err := host.BeginHosting("tcp://127.0.0.1:0,ws://127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	stopTicks := tickInBackground(host)
	t.Cleanup(func() { stopTicks(); host.Close() })
	host.membership.sessionMu.Lock()
	doors := host.membership.midRunPort.Doors()
	host.membership.sessionMu.Unlock()
	if len(doors) != 2 || !strings.HasPrefix(doors[1].String(), "ws://") {
		t.Fatalf("the host answers at %v; want a tcp door and a ws door", doors)
	}
	if host.cfg.HostAddress != "127.0.0.1:0,ws://127.0.0.1:0" {
		t.Fatalf("the host records its doors as %q", host.cfg.HostAddress)
	}
	guest, _ := driveBot(t, Config{JoinAddress: doors[1].String() + "/"}, "patrol", nil)
	waitForCursors(t, guest, 2)
	waitForCursors(t, host, 2)
}

// TestARestartNamesTheDoorAGuestCameInBy: a host's own doors are bind addresses, so
// a guest told the session restarts there dials the address it joined by; a
// successor's address names a host, and is where the guest goes.
func TestARestartNamesTheDoorAGuestCameInBy(t *testing.T) {
	t.Parallel()
	for payload, want := range map[string]string{":7777,wss://:7778": "", "ws://:7778": "", "10.0.0.5:7777": "10.0.0.5:7777"} {
		apps := meshSession(t, 0xA1A3, 2, [][2]int{{1, 2}})
		localCursors(t, apps)
		guest := apps[1]
		guest.cfg.Mode, guest.cfg.JoinAddress = ModePlay, "10.0.0.1:7777" // a joined run with a loop; the rule is the address
		guest.World().RunSafe(func() { guest.membership.receiveSessionRestart(1, payload) })
		req := guest.restart.Load()
		if req == nil || !req.Rejoin || req.Join != want {
			t.Errorf("a restart at %q latched %+v; want a rejoin at %q", payload, req, want)
		}
	}
}

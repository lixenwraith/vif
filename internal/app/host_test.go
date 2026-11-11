package app

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	"github.com/lixenwraith/vif/internal/app/internal/membership"
	"github.com/lixenwraith/vif/internal/app/internal/scribe"
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
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
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
	for deadline := time.Now().Add(2 * time.Second); dialling(a) && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if dialling(a) || a.restart.Load() != nil || a.Position() != before || !strings.Contains(a.ctx.GetStatusMessage(), "test quota reached") {
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
			host.cfg.Membership.Participants = tt.participants
			if err := host.BeginHosting("127.0.0.1:0"); err != nil {
				t.Fatalf("begin hosting: %v", err)
			}
			if got := host.membership.SessionCapacity() + 1; got != tt.want {
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
	if got := guest.membership.LocalSlot(); got != 1 {
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
	host, _ := driveBot(t, Config{Membership: Membership{HostAddress: addr, Bots: []string{"roam"}}}, "patrol", nil)
	waitForCursors(t, host, 2)
	relayAddr := freeAddress(t)
	relay, _ := driveBot(t, Config{Membership: Membership{JoinAddress: addr, RelayAddress: relayAddr}}, "patrol", nil)
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
	person, _ := driveBot(t, Config{Membership: Membership{JoinAddress: relayAddr}}, "roam", nil)
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
	cfg := runConfig{Config: Config{
		Mode: ModeHeadless, Seed: seed, Width: 120, Height: 40,
		Resources:  resource.Options{Embedded: true},
		Membership: Membership{HostAddress: addr, Participants: 2},
	}}
	cfg.wiring.scriptedSession = true
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

	// The gate the reconnect below arrives at is the one a run arms once its start
	// gate is done. A lobby that has closed has to do both things: stop refusing
	// dials, and start serving them. Doing only the first is what left a re-dial
	// admitted onto no gate at all; doing only the second refuses it for good.
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

// dialSession dials until the coordinator answers with an offer, proving the keys a
// run that kept them proves. Both things it retries past are the protocol's own: a
// host binding on its own goroutine is not listening yet, and a refusal — a lobby
// mid-close, a Wraith giving up its place — is the answer the design tells a dialer to
// retry on. A refusal spends the address's admission budget, so it is retried at a
// rejoin's pace.
func dialSession(t *testing.T, addr string, keys ...network.Key) (*network.PendingJoin, network.SessionOffer) {
	t.Helper()
	deadline := time.Now().Add(socketWait) // [wall] a link bound, not a game one
	var last error
	for time.Now().Before(deadline) {
		pending, offered, err := dialOnce(addr, keys...)
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

// dialOnce is one dial that seats at once, proving a fresh key and then keys: what
// its host answers.
func dialOnce(addr string, keys ...network.Key) (*network.PendingJoin, network.SessionOffer, error) {
	cfg := network.DebugConfig(link.RolePeer, "")
	cfg.Keys = append([]network.Key{network.NewKey()}, keys...)
	return network.DialSession(addr, cfg)
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
	for deadline := time.Now().Add(socketWait); !strings.Contains(a.ctx.SessionCtl.BotSummary(), "slot 1 default"); {
		if time.Now().After(deadline) {
			t.Fatalf("the bot was not seated: %q", a.ctx.SessionCtl.BotSummary())
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
	host, err := NewHeadless(Config{Seed: 0x3021, Width: 120, Height: 40, Membership: Membership{SessionName: "this"},
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
	a.cfg.Mode = ModePlay // :join serves Run's loop; the rule is its dial

	a.World().RunSafe(func() { err = a.ctx.SessionCtl.Join(host.HostAddr() + "/that") })
	if err != nil {
		t.Fatalf("join refused before dialling: %v", err)
	}
	deadline := time.Now().Add(socketWait) // [wall] a link bound
	for dialling(a) {
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
	guest.World().RunSafe(func() { _, err = guest.ctx.SessionCtl.ChangeScenario("blank") })
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
	a.World().RunSafe(func() { changed, err = a.ctx.SessionCtl.ChangeScenario("duel") })
	if err != nil || changed {
		t.Fatalf("its own name: changed %v, %v; want a reset", changed, err)
	}
	a.World().RunSafe(func() { changed, err = a.ctx.SessionCtl.ChangeScenario("other") })
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
	a.World().RunSafe(func() { changed, err = a.ctx.SessionCtl.ChangeScenario("other") })
	if !errors.Is(err, scribe.ErrRunReplaced) || changed || a.restart.Load() != first {
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
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForSeats(t, a, 1)
	req := &restartRequest{}
	a.World().RunSafe(func() {
		if err = a.ctx.SessionCtl.DropBot(int(a.ctx.SessionCtl.Bots()[0].Slot)); err == nil {
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
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForCursors(t, a, 2)
	waitForSeats(t, a, 1)
	var changed bool
	a.World().RunSafe(func() { changed, err = a.ctx.SessionCtl.ChangeScenario("solo") })
	if err == nil || !strings.Contains(err.Error(), "at most 1") || changed || a.restart.Load() != nil {
		t.Fatalf("a one-player scenario over a run and its bot: changed %v, %v; want it refused", changed, err)
	}
	a.World().RunSafe(func() { changed, err = a.ctx.SessionCtl.ChangeScenario("other") })
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
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForSeats(t, a, 1)
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.BeginHosting("127.0.0.1:0", AuthorityHost) })
	if err != nil {
		t.Fatalf(":host beside its own bot: %v", err)
	}
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.BeginHosting("127.0.0.1:0", "") })
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
	a.cfg.Mode = ModePlay // :join serves Run's loop
	tickUntilCursor(t, a)
	stop := tickInBackground(a)
	replaced := sync.OnceFunc(func() { stop(); a.Close() })
	t.Cleanup(replaced)
	var err error
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForSeats(t, a, 1)
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.Join(host.HostAddr()) })
	if err != nil {
		t.Fatalf(":join beside its own bot: %v", err)
	}
	for deadline := time.Now().Add(socketWait); dialling(a); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the dial never finished")
		}
	}
	req := a.restart.Load()
	if req == nil || req.Dialled == nil || !slices.Equal(req.Bots, []string{"roam"}) {
		t.Fatalf("restart %+v; want a dialled join that seats roam again (status %q)", req, a.Context().GetStatusMessage())
	}
	// What Run does next: the replaced run goes, and the request builds the next
	// one, driven here rather than presented.
	replaced()
	next, _ := runConfig{}.restarted(req)
	driveRun(t, "bot", next, "patrol", nil)
	waitForCursors(t, host, 3)
}

// waitForSeats waits for a run's n bot seats to be admitted.
func waitForSeats(t *testing.T, a *App, n int) {
	t.Helper()
	seated := func() bool {
		b := a.ctx.SessionCtl.Bots()
		return len(b) == n && !slices.ContainsFunc(b, func(s engine.BotSeat) bool { return s.Joining })
	}
	for deadline := time.Now().Add(socketWait); !seated(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("seats %+v; want %d admitted", a.ctx.SessionCtl.Bots(), n)
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
	if w := a.World(); w.SessionShared() || w.SessionBarrier() || a.membership.SessionTransport() != nil {
		t.Fatalf("a refused port left the run shared %t, barrier %t, transport %v",
			w.SessionShared(), w.SessionBarrier(), a.membership.SessionTransport())
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
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.BeginHosting("127.0.0.1:0", "leader") })
	if err == nil {
		t.Fatal("an unknown authority policy was accepted")
	}
	a.World().RunSafe(func() { err = a.ctx.SessionCtl.BeginHosting("127.0.0.1:0", AuthorityHost) })
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

// admitPace is how often a dial its host's admission budget refused asks again: the
// budget's own rate, so a window's reopening is met within one burst's share of it.
const admitPace = parameter.NetworkAdmitWindow / parameter.NetworkAdmitBurst

// scriptedRun starts cfg's run as RunScript does. A joiner dials until its host
// answers: a host not listening yet, a refusal, or a dial its transport closed
// unanswered because its handshakes in flight are at their bound, is retried as
// dialSession retries them. Dialling and the start gate are left after wait, as a
// person at the keyboard leaves them.
func scriptedRun(cfg Config, wait time.Duration) (*App, error) {
	rc := runConfig{Config: cfg}
	rc.wiring.scriptedSession = true // what a driven run is
	deadline := time.Now().Add(wait) // [wall] a link bound, not a game one
	for {
		a, err := newScriptApp(rc, joinDeadline(wait))
		pause := parameter.SessionRejoinInterval
		switch {
		case err == nil || cfg.Membership.JoinAddress == "":
			return a, err
		case errors.Is(err, &network.Refusal{Code: network.RefusalBudget}):
			pause = admitPace
		case errors.As(err, new(*network.Refusal)), errors.Is(err, io.EOF):
		case !errors.As(err, new(net.Error)):
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(pause)
	}
}

// mustSocketJoiner joins a live host as a driven run does, on the production path:
// dial, scenario, world, seat and reply, the start gate, the capture, the install
// and the catch-up, with no door of its own. This harness owns its ticks, so the
// host must be ticking meanwhile. configure edits the run's Config before it dials.
func mustSocketJoiner(t *testing.T, addr string, seed uint64, w, h int, configure ...func(*Config)) (*App, *link.SocketPort) {
	t.Helper()
	cfg := Config{Mode: ModeHeadless, Width: w, Height: h, Membership: Membership{JoinAddress: addr, NoAdvertise: true}}
	for _, f := range configure {
		f(&cfg)
	}
	guest, err := scriptedRun(cfg, socketWait)
	if err != nil {
		t.Fatalf("join %s: %v", addr, err)
	}
	t.Cleanup(guest.Close)
	if guest.Seed() != seed {
		t.Fatalf("the joiner drew seed %#x, host runs %#x", guest.Seed(), seed)
	}
	return guest, portOf(t, guest)
}

// lobbySession is a host and players-1 guests started as RunScript starts -host and
// -join, over real sockets: the host's lobby waits for all of them, each dialling it
// as its run starts, and closes on them at once. Guests open no door of their own,
// so the session is a star. The host comes first, its guests after it in the order
// of the slots the lobby gave them. Every guest dials from one address, whose
// admission budget lets a burst in each window: a larger lobby fills over windows,
// each refused dial asking again at admitPace.
func lobbySession(t *testing.T, host Config, players int, guest Config) []*App {
	t.Helper()
	windows := max(players-2, 0) / parameter.NetworkAdmitBurst // after the first
	wait := socketWait + time.Duration(windows)*(parameter.NetworkAdmitWindow+admitPace)
	addr := freeAddress(t)
	host.Mode, host.Membership.HostAddress, host.Membership.Participants = ModeHeadless, addr, players
	guest.Mode, guest.Membership.JoinAddress, guest.Membership.NoAdvertise = ModeHeadless, addr, true
	type started struct {
		a   *App
		err error
	}
	runs := make(chan started, players)
	for i := range players {
		cfg := guest
		if i == 0 {
			cfg = host
		}
		go func() {
			a, err := scriptedRun(cfg, wait)
			runs <- started{a, err}
		}()
	}
	apps := make([]*App, 0, players)
	var failed error
	for range players {
		r := <-runs
		if r.err != nil {
			failed = cmp.Or(failed, r.err)
			continue
		}
		t.Cleanup(r.a.Close)
		apps = append(apps, r.a)
	}
	if failed != nil {
		t.Fatalf("a lobby of %d: %v", players, failed)
	}
	place := func(a *App) int {
		if a.cfg.Membership.HostAddress != "" {
			return -1
		}
		return int(a.membership.LocalSlot())
	}
	slices.SortFunc(apps, func(x, y *App) int { return cmp.Compare(place(x), place(y)) })
	return apps
}

// lobbyHost is a run that opened its startup lobby as RunScript's -host and Serve do,
// on a loopback port of its own, with its bots seated and its lifetime started. The
// lobby waits in the background until it closes or the test ends; the gate's outcome
// arrives on the channel returned, and guests dial the address its door bound.
func lobbyHost(t *testing.T, cfg Config) (*App, string, <-chan error) {
	t.Helper()
	cfg.Membership.HostAddress = "127.0.0.1:0"
	rc := runConfig{Config: cfg}
	rc.wiring.scriptedSession = true // a headless host is a script's
	a, err := newSessionApp(rc)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	t.Cleanup(a.Close)
	if err := a.hub.StartAll(); err != nil {
		t.Fatalf("host services: %v", err)
	}
	a.life.Start(time.Now())
	if err := a.membership.SeatBots(); err != nil {
		t.Fatalf("host bots: %v", err)
	}
	stop, gate, done := make(chan os.Signal, 1), make(chan error, 1), make(chan struct{})
	go func() {
		gate <- a.membership.StartHostSession(stop)
		close(done)
	}()
	// Before Close: a gate still waiting is left as its operator would leave it.
	t.Cleanup(func() {
		stop <- os.Interrupt
		<-done
	})
	return a, portOf(t, a).Addr().String(), gate
}

// rawJoiner dials addr as a client that is not a vif run: seated at once, it replies
// to its offer with report, standing in for the world the offer names with the
// identity the offer carries, and does nothing after; the test drives its stream.
func rawJoiner(t *testing.T, addr string, report network.JoinerReport) (*network.PendingJoin, network.SessionOffer) {
	t.Helper()
	pending, offered := dialSession(t, addr)
	t.Cleanup(func() { _ = pending.Close() })
	report.Identity = offered.Identity
	if err := pending.Complete(nil, report); err != nil {
		t.Fatalf("participant %d's reply: %v", offered.Assigned, err)
	}
	return pending, offered
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

// TestASessionsRateIsItsHosts: the host's :speed crosses, so every participant runs
// at it from one agreed tick, and a guest asking is told only the host can.
func TestASessionsRateIsItsHosts(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A9, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	host, guest := apps[0], apps[1]
	mode.ExecuteCommand(guest.Context(), "speed 2")
	guest.Settle()
	if got := guest.Context().GetStatusMessage(); got != "Only the host can change the speed in a live session" {
		t.Fatalf("a guest's :speed was told %q", got)
	}
	mode.ExecuteCommand(host.Context(), "speed 2")
	host.Settle()
	took := make([]uint64, len(apps))
	for range parameter.NetworkBarrierMaxDelayTicks + 2 {
		tickAll(apps)
		for i, a := range apps {
			if took[i] == 0 && a.Context().TimeCtl.Scale() == (engine.TimeScale{Num: 2, Den: 1}) {
				took[i] = a.Position().Tick
			}
		}
	}
	if took[0] == 0 || took[1] != took[0] {
		t.Fatalf("host and guest took 2x at ticks %v, want both at one tick", took)
	}
	if got := guest.Context().GetStatusMessage(); got != "The host set the speed to 2x" {
		t.Fatalf("the guest was told %q", got)
	}
	assertMeshParity(t, apps, 0)
}

// TestAShedIsToldToEveryParticipant: a host shedding load dismisses the participant
// saying why, which the dropped one keeps for its notice and every other reads.
func TestAShedIsToldToEveryParticipant(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1AA, 3, [][2]int{{1, 2}, {1, 3}})
	local := localCursors(t, apps)
	var slot uint8
	apps[0].World().RunSafe(func() {
		c, _ := apps[0].World().Components.Cursor.GetComponent(local[2])
		slot = c.Slot
	})
	if !apps[0].membership.ShedParticipant(3) {
		t.Fatal("the host shed nobody")
	}
	apps[0].Settle()
	// The dropped guest ticks first: the host closes its link as the dismissal
	// applies, and the mesh drops what a closed link still queues.
	for range parameter.NetworkBarrierMaxDelayTicks + 2 {
		tickAll([]*App{apps[2], apps[0], apps[1]})
	}
	if !apps[2].membership.Dismissed() || !apps[2].membership.Shed() {
		t.Fatal("the shed participant was not told it was shed")
	}
	if got, want := apps[1].Context().GetStatusMessage(),
		fmt.Sprintf("The host dropped the player in slot %X to keep its tick", slot); got != want {
		t.Fatalf("the other guest read %q, want %q", got, want)
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
			apps[1].cfg.wiring.scriptedSession = scripted
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

// TestAResetKeepsADepartureItInterrupted: a link lost inside a new game's lead still
// leaves a Wraith on every instance, whether its departure was scheduled past the
// reset or due at the reset's own tick, behind it, where the game was cleared.
func TestAResetKeepsADepartureItInterrupted(t *testing.T) {
	t.Parallel()
	for _, shape := range []struct {
		name  string
		ticks int // between the new game's crossing and the link's loss
	}{{"past the reset", 1}, {"at the reset's tick", 0}} {
		apps := meshSession(t, 0xA3A6, 3, [][2]int{{1, 2}, {1, 3}})
		localCursors(t, apps)
		for range 10 {
			tickAll(apps)
		}
		mode.ExecuteCommand(apps[0].Context(), "n")
		apps[0].Settle()
		for range shape.ticks {
			tickAll(apps)
		}
		apps[2].World().RunSafe(func() {
			apps[2].World().Resources.Network.Port.(*link.MeshPort).Close()
		})
		survivors := apps[:2]
		for range 2*parameter.NetworkBarrierDelayTicks + 8 {
			tickAll(survivors)
		}
		for i, a := range survivors {
			if got := a.Position().Run; got != 1 {
				t.Fatalf("%s: participant %d reset run=%d, want 1", shape.name, i+1, got)
			}
			if !wraithIn(a, 2) {
				t.Fatalf("%s: participant %d holds slot 2 live in the new game, its participant gone", shape.name, i+1)
			}
		}
		assertMeshParity(t, survivors, 0)
	}
}

// TestAResetKeepsAnArrivalItInterrupted: a participant whose arrival crosses as a new
// game begins plays in that game on every instance, and the game starts: its arrival
// due at the reset's own tick, ahead of it or behind, or scheduled past it.
func TestAResetKeepsAnArrivalItInterrupted(t *testing.T) {
	t.Parallel()
	for _, shape := range []struct {
		name  string
		ahead bool // the arrival crosses ahead of the new game
		ticks int  // between the new game's crossing and the arrival's
	}{{"at the reset's tick, ahead of it", true, 0}, {"at the reset's tick, behind it", false, 0}, {"past the reset", false, 1}} {
		apps := meshSession(t, 0xA3A6, 2, [][2]int{{1, 2}})
		localCursors(t, apps)
		for range 10 {
			tickAll(apps)
		}
		arrive := func() {
			apps[0].World().RunSafe(func() {
				apps[0].World().PushEventFull(event.EventParticipantJoined,
					&event.ParticipantJoinedPayload{Participant: 3, Slot: 2}, event.OriginSession, core.DomainPlayer)
			})
		}
		if shape.ahead {
			arrive()
		}
		mode.ExecuteCommand(apps[0].Context(), "n")
		if !shape.ahead {
			if shape.ticks > 0 {
				apps[0].Settle()
			}
			for range shape.ticks {
				tickAll(apps)
			}
			arrive()
		}
		apps[0].Settle()
		for range 2*parameter.NetworkBarrierDelayTicks + 8 {
			tickAll(apps)
		}
		for i, a := range apps {
			var seated bool
			var state string
			a.World().RunSafe(func() {
				w := a.World()
				e := w.Resources.Player.Slot(2)
				c, _ := w.Components.Cursor.GetComponent(e)
				seated = e != 0 && c.PeerID == 3 && !w.Components.Wraith.HasEntity(e)
				state = w.Resources.Status.Strings.Get("fsm.state").Load()
			})
			if got := a.Position().Run; got != 1 || !seated || state == "" {
				t.Fatalf("%s: participant %d in run %d holds the arrival in slot 2 %t, its game in state %q; want run 1, seated, started",
					shape.name, i+1, got, seated, state)
			}
		}
		assertMeshParity(t, apps, 0)
	}
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
			if p, ok := ev.Payload.(*event.SpeciesLandRequestPayload); ok && p.Species == component.SpeciesQuasar {
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

	// The fusion lands as its one-second materialize ends, and its spawn crosses a
	// lead after that; the driven clock advances 50ms per tick.
	for range 28 + parameter.NetworkBarrierDelayTicks {
		tickAll(apps)
	}
	for i, got := range spawns {
		if got != 1 {
			t.Fatalf("participant %d observed %d quasar spawn requests, want 1", i+1, got)
		}
	}
}

// TestTdFusesTheCausalParticipantsDrains: td's quasar phase opens on the drain defeat
// that crosses its threshold and fuses that participant's drains, as main's does: the
// guest whose defeat it was lands the quasar, the host takes it from the guest, and
// only the guest's drains hold while the phase runs
func TestTdFusesTheCausalParticipantsDrains(t *testing.T) {
	t.Parallel()
	cfg := Config{Seed: 0x7D, Width: 200, Height: 60, Resources: resource.Options{Dir: soakRoot, Scenario: "td"}}
	apps := meshSessionOf(t, cfg, 2, [][2]int{{1, 2}})
	state := func(a *App) string { return a.World().Resources.Status.Strings.Get("fsm.state").Load() }
	for i := 0; state(apps[0]) != "TdCalm"; i++ {
		if i > 600 {
			t.Fatalf("td never reached TdCalm, at %s", state(apps[0]))
		}
		tickAll(apps)
	}
	var fromPeer [2][]bool // each instance's quasar landings, by whether a peer sent it
	for i, a := range apps {
		a.SetDispatchTap(func(ev event.GameEvent) {
			if p, ok := ev.Payload.(*event.SpeciesLandRequestPayload); ok && p.Species == component.SpeciesQuasar {
				fromPeer[i] = append(fromPeer[i], ev.Origin == event.OriginNetwork)
			}
		})
		a.World().Resources.Status.Ints.Get("kills.drain").Store(9)
	}
	// The guest's tenth defeat names the cursor it drives now: td's start rebuilt the roster
	var cursor core.Entity
	apps[1].World().RunSafe(func() { cursor = apps[1].World().Resources.Player.Entity })
	apps[1].Context().PushCrossing(event.EventDrainDefeated, &event.DrainDefeatedPayload{Entity: cursor})
	apps[1].Settle()
	for range 40 + parameter.NetworkBarrierDelayTicks {
		tickAll(apps)
	}
	if !slices.Equal(fromPeer[0], []bool{true}) || !slices.Equal(fromPeer[1], []bool{false}) {
		t.Fatalf("quasar landings by peer: host %v, guest %v; want one each, the guest's", fromPeer[0], fromPeer[1])
	}
	for i, want := range []bool{false, true} {
		if got := statBoolOf(apps[i], "drain.paused"); got != want {
			t.Fatalf("participant %d drains held %t in the quasar phase, want %t", i+1, got, want)
		}
	}
}

// TestAGuestsDrainBreaksTheGoldOnEveryInstance: a guest's drain standing on a gold
// member breaks the sequence as a crossing, so the host and the guest lose it at one
// tick, rather than the guest alone until the host's correction puts it back
func TestAGuestsDrainBreaksTheGoldOnEveryInstance(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x601D, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)
	for i := 0; !goldActive(apps[0]) || !goldActive(apps[1]); i++ {
		if i > 200 {
			t.Fatal("no gold sequence stands on both instances")
		}
		tickAll(apps)
	}

	guest := apps[1]
	guest.World().RunSafe(func() {
		w := guest.World()
		var member core.Entity
		w.Components.Glyph.Each(func(e core.Entity, _ *component.GlyphComponent) bool {
			member = e
			return e.Domain() != core.DomainShared
		})
		pos, _ := w.Positions.GetPosition(member)
		// Heat keeps the drain population from despawning it; the stun holds it on the member
		if h, ok := w.Components.Heat.GetPtr(local[1]); ok {
			h.Current = parameter.HeatMax / 2
		}
		d := w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(d, pos)
		w.Components.Drain.SetComponent(d, component.DrainComponent{LastIntX: pos.X, LastIntY: pos.Y})
		combat := component.CombatComponent{OwnerEntity: d, CombatEntityType: component.CombatEntityDrain,
			HitPoints: parameter.CombatInitialHPDrain}
		combat.Status[component.StatusStun].Remaining = time.Second
		w.Components.Combat.SetComponent(d, combat)
		cx, cy := vmath.Point{X: pos.X, Y: pos.Y}.Center()
		w.Components.Kinetic.SetComponent(d, component.KineticComponent{Kinetic: physics.Kinetic{PreciseX: cx, PreciseY: cy}})
	})

	var broke [2]uint64
	for i := 0; broke[0] == 0 || broke[1] == 0; i++ {
		if i > 4*parameter.NetworkBarrierMaxDelayTicks {
			t.Fatalf("gold broke at ticks %v (host, guest), want on both", broke)
		}
		tickAll(apps)
		for j, a := range apps {
			if broke[j] == 0 && !goldActive(a) {
				broke[j] = a.Position().Tick
			}
		}
	}
	if broke[0] != broke[1] {
		t.Fatalf("gold broke at tick %d on the host and %d on the guest, want one tick", broke[0], broke[1])
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
	for deadline := time.Now().Add(socketWait); host.membership.SessionTransport().PeerCount() > 0; time.Sleep(time.Millisecond) {
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
		a.World().RunSafe(func() { err = a.ctx.SessionCtl.AddBot("roam") })
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
	guest.cfg.Membership.JoinAddress = host.HostAddr() // what -join sets; a headless run refuses it
	gone := mustHeadless(t, 0x3104, BotWidth, BotHeight)
	tickUntilCursor(t, gone)
	stopGone := tickInBackground(gone)
	leave := sync.OnceFunc(func() { stopGone(); gone.Close() })
	t.Cleanup(leave)
	hosting(gone)
	left, leftPort := mustSocketJoiner(t, gone.HostAddr(), 0x3104, BotWidth, BotHeight)
	left.cfg.Membership.JoinAddress = gone.HostAddr()

	// Each role is told apart by what it refuses and how :session opens.
	for _, row := range []struct {
		kind              string
		run               func() *App
		join, host, leave string // the refusal each names; empty takes the command
		line              string // how :session opens
	}{
		{"solo", func() *App { return ticking(mustHeadless(t, 0x3101, BotWidth, BotHeight)) },
			"", "", "in no session", "Solo run"},
		{"solo with its bots", func() *App { return withBot(ticking(mustHeadless(t, 0x3102, BotWidth, BotHeight))) },
			"", "", "only its own bots", "Session hosting, "},
		{"hosting", func() *App { return host },
			"already in a session", "already hosting", "", "Session hosting 127.0.0.1:"},
		{"hosting with its bots", func() *App { return withBot(hosting(ticking(mustHeadless(t, 0x3103, BotWidth, BotHeight)))) },
			"already in a session", "already hosting", "", "Session hosting 127.0.0.1:"},
		{"joined", func() *App { return guest },
			"already in a session", "already in a session", "", "Session joined " + host.HostAddr() + ", "},
		{"inherited the term", func() *App {
			guest.World().RunSafe(func() {
				guest.world.Resources.Network.Authority.Store(guest.world.LocalParticipant())
			})
			return guest
		}, "already in a session", "", "", "Session hosting, "},
		{"its session left", func() *App {
			leave()
			for deadline := time.Now().Add(socketWait); leftPort.PeerCount() > 0; time.Sleep(time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the guest never lost its host")
				}
			}
			return left
		}, "", "already in a session", "", "Session joined " + gone.HostAddr() + ", "},
	} {
		a := row.run()
		var join, hostErr, leaveErr error
		var line string
		a.World().RunSafe(func() {
			mode := a.cfg.Mode
			a.cfg.Mode = ModePlay // :join and :leave serve Run's loop, which a headless run lacks
			join, leaveErr = a.ctx.SessionCtl.JoinError(), a.ctx.SessionCtl.LeaveError()
			a.cfg.Mode = mode
			hostErr = a.ctx.SessionCtl.HostError()
			line = a.ctx.SessionCtl.SessionSummary()
		})
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

// askLeave asks :leave as Run's loop would be asked it, under the lock the command
// path holds, and as in play, since a harness has no loop, the one refusal its role
// does not decide.
func askLeave(t *testing.T, a *App) {
	t.Helper()
	var err error
	a.World().RunSafe(func() {
		mode := a.cfg.Mode
		a.cfg.Mode = ModePlay
		err = a.ctx.SessionCtl.Leave()
		a.cfg.Mode = mode
	})
	if err != nil {
		t.Fatalf(":leave refused: %v", err)
	}
}

// leaveAs serves :leave as Run's loop does, for a harness that stands in for it: the
// command, then the loop's leave.
func leaveAs(t *testing.T, a *App) {
	t.Helper()
	askLeave(t, a)
	a.membership.LeaveSession()
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
	host.cfg.Membership.Participants = 3
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	guest, _ := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, BotWidth, BotHeight)
	guest.cfg.Membership.JoinAddress = host.HostAddr() // what -join sets; a headless run refuses it
	stopTicking()
	waitForRosterPair(t, host, guest)
	id, term := guest.membership.LocalParticipant(), guest.authority.Term()
	var own core.Entity
	guest.World().RunSafe(func() { own = guest.world.Resources.Player.Slot(1) })

	leaveAs(t, guest)
	var authoring bool
	var left, kept core.Entity
	var hostErr, joinErr, leaveErr error
	guest.World().RunSafe(func() {
		authoring = guest.world.IsSessionCoordinator()
		left, kept = guest.world.Resources.Player.Slot(0), guest.world.Resources.Player.Slot(1)
		mode := guest.cfg.Mode
		guest.cfg.Mode = ModePlay
		joinErr, leaveErr = guest.ctx.SessionCtl.JoinError(), guest.ctx.SessionCtl.LeaveError()
		guest.cfg.Mode = mode
		hostErr = guest.ctx.SessionCtl.HostError()
	})
	if !authoring || guest.membership.LocalParticipant() != id || guest.authority.Term() != term+1 {
		t.Fatalf("after leaving, participant %d authoring %t at term %d; want it authoring as %d at term %d",
			guest.membership.LocalParticipant(), authoring, guest.authority.Term(), id, term+1)
	}
	if left != 0 || kept != own {
		t.Fatalf("after leaving, slot 0 holds %d and slot 1 %d; want the host gone and the guest's own %d kept", left, kept, own)
	}
	if hostErr != nil || joinErr != nil || leaveErr == nil || !strings.Contains(leaveErr.Error(), "only its own bots") {
		t.Fatalf("after leaving, :host %v, :join %v, :leave %v; want the first two taken and a session of its own to leave", hostErr, joinErr, leaveErr)
	}
	if got := guest.membership.SessionCapacity() + 1; got != min(parameter.MaxPlayers, guest.scenario.MaxPlayers) {
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
// key it waits under.
func TestAForkGoesOnInASessionOfItsOwn(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	store := &keptPlaces{}
	host, err := NewHeadless(Config{Seed: fixtureSeed, Width: BotWidth, Height: BotHeight,
		Resources: resource.Options{Embedded: true}, Membership: Membership{PlaceStore: store}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	tickUntilCursor(t, host)
	stopTicking := tickInBackground(host)
	t.Cleanup(stopTicking)
	host.World().RunSafe(func() { err = host.ctx.SessionCtl.BeginHosting("127.0.0.1:0", AuthorityHost) })
	if err != nil {
		t.Fatal(err)
	}
	guest, _ := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, BotWidth, BotHeight)
	guest.World().RunSafe(func() {
		guest.cfg.Membership.JoinAddress = host.HostAddr() // what -join sets; a headless run refuses it
		guest.cfg.Mode = ModePlay                          // the mode whose loop takes a fork on
	})
	stopTicking()
	waitForRosterPair(t, host, guest)
	id, term, key := guest.membership.LocalParticipant(), guest.authority.Term(), store.key(t)
	held := places(guest, everyone)

	leaveAs(t, host)
	stopGuest := tickInBackground(guest)
	for deadline := time.Now().Add(socketWait); !guest.membership.Leaving() && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	stopGuest()
	if !guest.membership.Leaving() {
		t.Fatal("the guest lost its pinned host, and nothing asked its loop to take it on")
	}
	guest.membership.LeaveSession() // as Run's loop does

	var authoring bool
	var hostErr, joinErr, leaveErr error
	guest.World().RunSafe(func() {
		authoring = guest.world.IsSessionCoordinator()
		joinErr, leaveErr = guest.ctx.SessionCtl.JoinError(), guest.ctx.SessionCtl.LeaveError()
		hostErr = guest.ctx.SessionCtl.HostError()
	})
	if !authoring || guest.membership.LocalParticipant() != id || guest.authority.Term() != term+1 {
		t.Fatalf("after its fork, participant %d authoring %t at term %d; want it authoring as %d at term %d",
			guest.membership.LocalParticipant(), authoring, guest.authority.Term(), id, term+1)
	}
	if got := places(guest, everyone); len(held) != 2 || !maps.Equal(got, held) || !wraithIn(guest, 0) {
		t.Fatalf("after its fork the guest holds %v (entity, participant by slot), the host's a Wraith %t; want %v, the host's a Wraith",
			got, wraithIn(guest, 0), held)
	}
	if hostErr != nil || joinErr != nil || leaveErr == nil || !strings.Contains(leaveErr.Error(), "only its own bots") {
		t.Fatalf("after its fork, :host %v, :join %v, :leave %v; want the first two taken and a session of its own to leave", hostErr, joinErr, leaveErr)
	}
	if msg := guest.Context().GetStatusMessage(); msg != "Host connection lost; playing on in a session of your own" {
		t.Fatalf("after its fork the guest says %q; want the loss said", msg)
	}
	if err := guest.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("the fork opens no door: %v", err)
	}
	stopGuest = tickInBackground(guest)
	defer stopGuest()
	back, offered := dialSession(t, guest.HostAddr(), key)
	defer back.Close()
	if uint64(offered.Assigned) != held[0][1] || assignedSlot(offered) != 0 {
		t.Fatalf("the host's key was given %d on slot %d; want its own place, %d on slot 0", offered.Assigned, assignedSlot(offered), held[0][1])
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
			if err = a.ctx.SessionCtl.BeginHosting("127.0.0.1:0", ""); err == nil {
				tt.stop(a.ctx)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		membership.ForkCell(a.world).Store(true) // as giveUp leaves it
		askLeave(t, a)                           // as a fork asks its loop
		a.membership.LeaveSession()
		if held, msg := a.ctx.TimeCtl.Held(), a.ctx.GetStatusMessage(); held != tt.held || msg != tt.notice {
			t.Errorf("a fork under %s goes on held %t, saying %q; want held %t, saying %q", tt.name, held, msg, tt.held, tt.notice)
		}
	}
}

// TestASessionARunOpensHasNobodyAwayFromIt: who was away from the session a run
// left, or on the way back to it, stays with that session, so the next one hands an
// identity out as somebody new: nobody there is dismissed or brought back. The host
// leaves while a full session gives a newcomer one Wraith's place and a key takes
// another's back, its return waiting in the start gate.
func TestASessionARunOpensHasNobodyAwayFromIt(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHosting(t, Membership{Participants: 3})
	stop := tickInBackground(host)
	defer stop()
	stores := []*keptPlaces{{}, {}}
	var ids []link.PeerID
	for _, store := range stores {
		guest, port := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, 120, 40, func(c *Config) { c.Membership.PlaceStore = store })
		id, slot := arrival(t, host, guest)
		_ = port.Close()
		guest.Close()
		waitUntil(t, func() bool { return wraithIn(host, slot) }, "the guest's cursor waiting as a Wraith")
		ids = append(ids, id)
	}
	stop() // what the session asked of its next tick stays asked
	if _, _, err := dialOnce(host.HostAddr()); !errors.Is(err, &network.Refusal{Code: network.RefusalYielding}) {
		t.Fatalf("a newcomer to the full session was answered %v; want a place being given up", err)
	}
	back, offered := dialSession(t, host.HostAddr(), stores[1].key(t))
	defer back.Close()
	if offered.Assigned != ids[1] {
		t.Fatalf("the key took %d; want participant %d's place", offered.Assigned, ids[1])
	}
	if err := back.Complete(nil, network.JoinerReport{Identity: offered.Identity, Width: 120, Height: 40}); err != nil {
		t.Fatalf("the returning player's reply: %v", err)
	}

	leaveAs(t, host)
	var dismiss uint32
	var kept bool
	host.World().RunSafe(func() {
		r := host.world.Resources.Network
		dismiss, kept = r.DismissWraith(), r.WraithKept(uint32(ids[1]))
	})
	if dismiss != 0 || kept {
		t.Fatalf("the session the host went on in dismisses participant %d and holds %d's place for a return %t; want neither",
			dismiss, ids[1], kept)
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
	host.World().RunSafe(func() { err = host.ctx.SessionCtl.AddBot("roam") })
	if err != nil {
		t.Fatal(err)
	}
	waitForSeats(t, host, 1)
	seated := host.ctx.SessionCtl.Bots()[0]
	_, port := mustSocketJoiner(t, addr, fixtureSeed, BotWidth, BotHeight)
	waitForCursors(t, host, 3)
	itsBots := heldBy(host.membership.LocalParticipant())
	left := places(host, itsBots)

	leaveAs(t, host)
	waitForSeats(t, host, 1)
	if again := host.ctx.SessionCtl.Bots()[0]; again.ID == seated.ID {
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
	// Not parallel: a real socket against wall-clock deadlines.
	host := mustHeadless(t, fixtureSeed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	askLeave(t, host) // its loop has yet to take the run out
	_, _, err := network.DialSession(host.HostAddr(), network.DebugConfig(link.RolePeer, ""))
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
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHosting(t, Membership{})
	stop := tickInBackground(host)
	defer stop()
	pending, offered := dialSession(t, host.HostAddr())
	relay := takePlace(t, pending, offered, network.JoinerReport{Width: 120, Height: 40})
	stop() // the host takes its ticks as this test gives them from here

	relayStep(host, offered.Assigned, network.RelayJoin{Step: network.RelayNonce, Nonce: 1})
	challenge := relayAnswer(t, relay, 1).Challenge
	relayStep(host, offered.Assigned, network.RelayJoin{Step: network.RelayAssign, Nonce: 2,
		Keys: []network.Proof{network.NewKey().Prove(challenge, nil)}, Challenge: challenge})
	leaf := relayAnswer(t, relay, 2).Offer.Assigned
	relayStep(host, offered.Assigned, network.RelayJoin{Step: network.RelayAbandon, Leaf: leaf})
	next, between := dialSession(t, host.HostAddr())
	defer next.Close()
	if between.Assigned == leaf {
		t.Fatalf("identity %d was reassigned while its departure was still to apply", leaf)
	}
	host.Tick(parameter.NetworkBarrierDelayTicks + 1)
	again, after := dialSession(t, host.HostAddr())
	defer again.Close()
	if after.Assigned != leaf {
		t.Fatalf("after the departure applied, assigned %d; want %d back", after.Assigned, leaf)
	}
}

// TestAFullSessionGivesTheLongestWaitingWraithsPlaceToANewcomer: a dial a full
// session cannot otherwise admit is told to retry in a moment while the participant
// away longest is dismissed, and then takes that place; the other Wraith keeps its.
func TestAFullSessionGivesTheLongestWaitingWraithsPlaceToANewcomer(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHosting(t, Membership{Participants: 3})
	stop := tickInBackground(host)
	defer stop()
	type place struct {
		id   link.PeerID
		slot uint8
	}
	var left []place
	var leave []func()
	for range 2 {
		guest, port := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, 120, 40)
		var g place
		g.id, g.slot = arrival(t, host, guest)
		left = append(left, g)
		leave = append(leave, func() { _ = port.Close(); guest.Close() })
	}
	for i, g := range left {
		leave[i]()
		waitUntil(t, func() bool { return wraithIn(host, g.slot) }, "the guest's cursor waiting as a Wraith")
	}

	// The place on its way is given up as the host ticks, so a host standing still
	// answers every newcomer the same.
	stop()
	for range 2 {
		if _, _, err := dialOnce(host.HostAddr()); !errors.Is(err, &network.Refusal{Code: network.RefusalYielding}) ||
			network.RetryOf(err) != network.RetrySoon {
			t.Fatalf("a full session with Wraiths answered a newcomer with %v; want a refusal to retry soon", err)
		}
	}
	resume := tickInBackground(host)
	defer resume()
	waitUntil(t, func() bool { return !cursorIn(host, left[0].slot) }, "the longest waiting Wraith dismissed")
	pending, offered := dialSession(t, host.HostAddr())
	defer pending.Close()
	if offered.Assigned != left[0].id || assignedSlot(offered) != left[0].slot {
		t.Fatalf("after the yield the newcomer was given %d on slot %d; want participant %d's place on slot %d",
			offered.Assigned, assignedSlot(offered), left[0].id, left[0].slot)
	}
	if !wraithIn(host, left[1].slot) {
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

// cursorIn reports whether a's world holds a cursor in slot.
func cursorIn(a *App, slot uint8) (held bool) {
	a.world.RunSafe(func() { held = a.world.Resources.Player.Slot(slot) != 0 })
	return held
}

// arrival ticks a guest that joined a host ticking meanwhile until its arrival has
// crossed on both: the participant it is and the slot it plays in.
func arrival(t *testing.T, host, guest *App) (link.PeerID, uint8) {
	t.Helper()
	for deadline := time.Now().Add(socketWait); ; time.Sleep(joinTestTickInterval / 5) { // [wall] a link bound
		if slot := guest.membership.LocalSlot(); slot != parameter.NoPlayerSlot && cursorIn(host, slot) && cursorIn(guest, slot) {
			return link.PeerID(guest.membership.LocalParticipant()), slot
		}
		if time.Now().After(deadline) {
			t.Fatalf("the guest's arrival never crossed: it plays in slot %d", guest.membership.LocalSlot())
		}
		guest.Tick(1)
	}
}

// mustHosting is a solo run that opened a session with :host on a loopback port of
// its own, under cfg: a mid-run host, with no lobby, whose every dial is a mid-run
// join. The caller ticks it.
func mustHosting(t *testing.T, cfg Membership) *App {
	t.Helper()
	a, err := NewHeadless(Config{Seed: fixtureSeed, Width: 120, Height: 40, Resources: resource.Options{Embedded: true}, Membership: cfg})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)
	if err := a.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	return a
}

// takePlace completes a seated dial as a joiner whose world is built does: its
// reply, the start gate, the world and its confirmation; the host must be ticking
// meanwhile. The port it then runs on answers its host's probes until the test ends
// or closes it.
func takePlace(t *testing.T, pending *network.PendingJoin, offered network.SessionOffer, report network.JoinerReport) *link.SocketPort {
	t.Helper()
	cfg := pending.TransportConfig() // before the stream opens, as a run takes it
	report.Identity = offered.Identity
	if err := pending.Complete(nil, report); err != nil {
		t.Fatalf("participant %d's reply: %v", offered.Assigned, err)
	}
	if _, err := pending.WaitStart(); err != nil {
		t.Fatalf("participant %d's start gate: %v", offered.Assigned, err)
	}
	if _, _, err := pending.ReceiveSnapshot(); err != nil {
		t.Fatalf("participant %d's world: %v", offered.Assigned, err)
	}
	if err := pending.Ready(); err != nil {
		t.Fatalf("participant %d's confirmation: %v", offered.Assigned, err)
	}
	port := link.NewSocketPort(cfg)
	if err := port.Start(); err != nil {
		t.Fatalf("participant %d's port: %v", offered.Assigned, err)
	}
	t.Cleanup(func() { _ = port.Close() })
	return port
}

// relayStep hands the coordinator one step of a relayed join as its network system
// does a frame from holder.
func relayStep(host *App, holder link.PeerID, req network.RelayJoin) {
	body, _ := json.Marshal(req) // plain data cannot fail to encode
	host.world.RunSafe(func() {
		host.world.Resources.Network.OnRelayJoin(uint8(network.MsgRelayJoin), uint32(holder), body)
	})
}

// relayAnswer is the coordinator's answer to the step a holder asked under nonce, as
// the holder's port receives it; relayAnswerAny takes a refusal too.
func relayAnswer(t *testing.T, holder *link.SocketPort, nonce uint32) network.RelayAdmit {
	t.Helper()
	ans := relayAnswerAny(t, holder, nonce)
	if err := ans.Refused(); err != nil {
		t.Fatalf("the relayed step %d was refused: %v", nonce, err)
	}
	return ans
}

func relayAnswerAny(t *testing.T, holder *link.SocketPort, nonce uint32) network.RelayAdmit {
	t.Helper()
	in := make([]link.Inbound, 64)
	for deadline := time.Now().Add(socketWait); time.Now().Before(deadline); {
		for _, m := range in[:holder.Drain(in)] {
			var ans network.RelayAdmit
			if m.Msg == nil || m.Msg.Type != network.MsgRelayAdmit || json.Unmarshal(m.Msg.Payload, &ans) != nil || ans.Nonce != nonce {
				continue
			}
			return ans
		}
		select {
		case <-holder.Changes():
		case <-time.After(10 * time.Millisecond): // [wall] a link bound, not a game one
		}
	}
	t.Fatalf("no answer to the relayed step %d within %s", nonce, socketWait)
	return network.RelayAdmit{}
}

// keptPlaces is a PlaceStore a test reads: the places document as a process last
// stored it, as a page's storage or a places file holds it.
type keptPlaces struct {
	mu  sync.Mutex
	doc []byte
}

func (s *keptPlaces) Load() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.doc), nil
}

func (s *keptPlaces) Store(doc []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc = slices.Clone(doc)
	return nil
}

// key is the newest place the document keeps for a person: the key the last run of
// its process to join a session joined under.
func (s *keptPlaces) key(t *testing.T) network.Key {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var kept []struct {
		Key network.Key `json:"key"`
		Bot bool        `json:"bot,omitempty"`
	}
	if err := json.Unmarshal(s.doc, &kept); err != nil {
		t.Fatalf("the places document %q: %v", s.doc, err)
	}
	for _, c := range slices.Backward(kept) {
		if !c.Bot {
			return c.Key
		}
	}
	t.Fatalf("the places document %q keeps no person's place", s.doc)
	return network.Key{}
}

// away is a full session of two whose guest left under the key its run joined
// with, its cursor waiting as a Wraith: the host, ticking until stop; the guest's
// participant and slot; and the key, as its process's store kept it.
type away struct {
	host *App
	id   link.PeerID
	slot uint8
	key  network.Key
	stop func()
}

func awayGuest(t *testing.T, cfg ...Membership) away {
	t.Helper()
	hosting := Membership{Participants: 2}
	if len(cfg) > 0 {
		hosting = cfg[0]
	}
	host := mustHosting(t, hosting)
	stop := tickInBackground(host)
	t.Cleanup(stop)
	store := &keptPlaces{}
	guest, port := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, 120, 40, func(c *Config) { c.Membership.PlaceStore = store })
	a := away{host: host, key: store.key(t), stop: stop}
	a.id, a.slot = arrival(t, host, guest)
	_ = port.Close()
	guest.Close()
	waitUntil(t, func() bool { return wraithIn(host, a.slot) }, "the departed guest's cursor waiting as a Wraith")
	return a
}

// TestAFullSessionTakesBackAPlayerWhoseWraithKeepsItsPlace: a dial proving the key a
// Wraith waits under is given that place at once, a full session's too, and no
// newcomer is meanwhile; its arrival takes the cursor back with what its player
// brought of the game it left.
func TestAFullSessionTakesBackAPlayerWhoseWraithKeepsItsPlace(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	a := awayGuest(t)
	back, offered := dialSession(t, a.host.HostAddr(), network.NewKey(), a.key)
	if offered.Assigned != a.id || assignedSlot(offered) != a.slot {
		t.Fatalf("the returning player was given %d on slot %d; want its own place, %d on slot %d",
			offered.Assigned, assignedSlot(offered), a.id, a.slot)
	}
	if _, _, err := dialOnce(a.host.HostAddr()); !errors.Is(err, &network.Refusal{Code: network.RefusalFull}) {
		t.Fatalf("a newcomer was answered %v while the place came back; want the session full", err)
	}
	record := &event.CursorRecord{Energy: 4321, HitPoints: 37}
	takePlace(t, back, offered, network.JoinerReport{Width: 120, Height: 40, Record: record})
	a.stop()
	// Read on the tick the arrival applies, the first the cursor is no Wraith on.
	var energy int64
	var health int
	for wraith, deadline := true, time.Now().Add(socketWait); wraith; time.Sleep(joinTestTickInterval / 5) {
		if time.Now().After(deadline) {
			t.Fatal("the returning player's arrival never applied")
		}
		a.host.Tick(1)
		a.host.world.RunSafe(func() {
			e := a.host.world.Resources.Player.Slot(a.slot)
			en, _ := a.host.world.Components.Energy.GetComponent(e)
			combat, _ := a.host.world.Components.Combat.GetComponent(e)
			wraith, energy, health = a.host.world.Components.Wraith.HasEntity(e), en.Current, combat.HitPoints
		})
	}
	if energy != record.Energy || health != record.HitPoints {
		t.Fatalf("after the arrival the place has energy %d and health %d; want it taken back with %+v", energy, health, *record)
	}
}

// TestAReturnThatDoesNotCompleteLeavesItsWraithWaiting: a place a key took back
// goes back to its Wraith when the join fails, directly or through a relay, its
// wait no longer held off; the key takes it again. A relay carries the dial's proofs
// over a challenge it asked the coordinator for, and proofs over none are refused.
func TestAReturnThatDoesNotCompleteLeavesItsWraithWaiting(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	a := awayGuest(t, Membership{Participants: 3})
	relay, relayPort := mustSocketJoiner(t, a.host.HostAddr(), fixtureSeed, 120, 40)
	relayID, _ := arrival(t, a.host, relay)
	kept := func() (held bool) {
		a.host.world.RunSafe(func() { held = a.host.world.Resources.Network.WraithKept(uint32(a.id)) })
		return held
	}
	took := func() *network.PendingJoin {
		t.Helper()
		pending, back := dialSession(t, a.host.HostAddr(), a.key)
		if back.Assigned != a.id || !kept() {
			t.Fatalf("the key took %d, its Wraith's wait held off %t; want participant %d's place, held off", back.Assigned, kept(), a.id)
		}
		return pending
	}
	_ = took().Close() // gone before its reply
	waitUntil(t, func() bool { return !kept() }, "the place a dial left given back to its Wraith")

	// Through a relay: the challenge first, then the dial's proofs over it.
	relayStep(a.host, relayID, network.RelayJoin{Step: network.RelayNonce, Nonce: 7})
	challenge := relayAnswer(t, relayPort, 7).Challenge
	if challenge == "" {
		t.Fatal("the coordinator issued its relay no challenge")
	}
	proofs := []network.Proof{network.NewKey().Prove(challenge, nil), a.key.Prove(challenge, nil)}
	relayStep(a.host, relayID, network.RelayJoin{Step: network.RelayAssign, Nonce: 8, Keys: proofs, Challenge: "not issued"})
	if ans := relayAnswerAny(t, relayPort, 8); !errors.Is(ans.Refused(), &network.Refusal{Code: network.RefusalCredential}) || kept() {
		t.Fatalf("proofs over a challenge the coordinator never issued were answered %v, the place held %t", ans.Refused(), kept())
	}
	relayStep(a.host, relayID, network.RelayJoin{Step: network.RelayAssign, Nonce: 9, Keys: proofs, Challenge: challenge})
	if ans := relayAnswer(t, relayPort, 9); ans.Offer.Assigned != a.id || !kept() {
		t.Fatalf("the key a relay carried took %d, the place held %t; want participant %d's place, held", ans.Offer.Assigned, kept(), a.id)
	}
	relayStep(a.host, relayID, network.RelayJoin{Step: network.RelayAbandon, Leaf: a.id})
	from := a.host.Position().Tick
	waitUntil(t, func() bool { return a.host.Position().Tick > from+parameter.NetworkBarrierDelayTicks+1 }, "a playout lead past the abandonment")
	if kept() || !wraithIn(a.host, a.slot) {
		t.Fatalf("after a failed return through a relay the Wraith waits %t, held off %t; want it waiting, its wait running",
			wraithIn(a.host, a.slot), kept())
	}
	_ = took().Close()
}

// TestAKeyOnAPlaceStillLinkedClosesTheLinkItLeft: a dial proving the key of a
// place whose link the host still holds is told to retry in a moment, and that link
// is closed, so its departure crosses and the retry takes the place back.
func TestAKeyOnAPlaceStillLinkedClosesTheLinkItLeft(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHosting(t, Membership{})
	stop := tickInBackground(host)
	defer stop()
	store := &keptPlaces{}
	guest, _ := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, 120, 40, func(c *Config) { c.Membership.PlaceStore = store })
	id, slot := arrival(t, host, guest)

	_, _, err := dialOnce(host.HostAddr(), store.key(t))
	if !errors.Is(err, &network.Refusal{Code: network.RefusalReturning}) || network.RetryOf(err) != network.RetrySoon {
		t.Fatalf("a key on a place still linked was answered %v; want a refusal to retry soon", err)
	}
	if portOf(t, host).Connected(uint32(id)) {
		t.Fatal("the link the returning player left is still open")
	}
	waitUntil(t, func() bool { return wraithIn(host, slot) }, "the closed link's departure")
	back, offered := dialSession(t, host.HostAddr(), store.key(t))
	defer back.Close()
	if offered.Assigned != id || assignedSlot(offered) != slot {
		t.Fatalf("the retry was given %d on slot %d; want participant %d's place on slot %d", offered.Assigned, assignedSlot(offered), id, slot)
	}
}

// TestAPlayerWhoLeftAsANewGameBeganComesBack: a link lost inside a new game's lead
// keeps its place on the host's roster and waits as a Wraith in the new game, so
// the key it left under takes that place back.
func TestAPlayerWhoLeftAsANewGameBeganComesBack(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	for _, shape := range []struct {
		name  string
		ticks int // between the new game's crossing and the link's loss
	}{{"past the reset", 1}, {"at the reset's tick", 0}} {
		host := mustHosting(t, Membership{Participants: 2})
		stop := tickInBackground(host)
		t.Cleanup(stop)
		store := &keptPlaces{}
		guest, port := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, 120, 40, func(c *Config) { c.Membership.PlaceStore = store })
		id, slot := arrival(t, host, guest)
		stop()
		run := host.Position().Run
		mode.ExecuteCommand(host.Context(), "n")
		host.Settle()
		host.Tick(shape.ticks)
		_ = port.Close()
		guest.Close()
		waitUntil(t, func() bool { return host.membership.SessionTransport().PeerCount() == 0 }, "the host losing the guest's link")
		host.Tick(2 * parameter.NetworkBarrierMaxDelayTicks)
		if got := host.Position().Run; got != run+1 || !wraithIn(host, slot) || host.membership.GuestCount() != 1 {
			t.Fatalf("%s: in run %d the departed guest waits as a Wraith %t, the roster holds %d guest(s); want run %d, waiting, its place kept",
				shape.name, got, wraithIn(host, slot), host.membership.GuestCount(), run+1)
		}
		back, offered := dialSession(t, host.HostAddr(), store.key(t))
		defer back.Close()
		if offered.Assigned != id || assignedSlot(offered) != slot {
			t.Fatalf("%s: the returning player was given %d on slot %d; want its own place, %d on slot %d",
				shape.name, offered.Assigned, assignedSlot(offered), id, slot)
		}
	}
}

// departing is each participant a's barrier holds crossed out and not yet turned.
func departing(a *App) (out []uint32) {
	a.World().RunSafe(func() {
		for _, sys := range a.World().Systems() {
			if d, ok := sys.(interface{ Departing() []uint32 }); ok {
				out = d.Departing()
			}
		}
	})
	return out
}

// heldAway is a session held by its host whose guest's link dropped under the hold,
// its departure crossed and unapplied: the host, no longer ticking, a witness guest
// when asked for, the lost guest's participant and slot, and its process's store.
type heldAway struct {
	host, witness *App
	id            link.PeerID
	slot          uint8
	store         *keptPlaces
}

// heldAwayGuest holds by a pause or, start, by a new game waiting for its press.
func heldAwayGuest(t *testing.T, start, witnessed bool) heldAway {
	t.Helper()
	h := heldAway{host: mustHosting(t, Membership{Participants: 3}), store: &keptPlaces{}}
	stop := tickInBackground(h.host)
	t.Cleanup(stop)
	if witnessed {
		h.witness, _ = mustSocketJoiner(t, h.host.HostAddr(), fixtureSeed, 120, 40)
		arrival(t, h.host, h.witness)
	}
	guest, port := mustSocketJoiner(t, h.host.HostAddr(), fixtureSeed, 120, 40, func(c *Config) { c.Membership.PlaceStore = h.store })
	h.id, h.slot = arrival(t, h.host, guest)
	if witnessed {
		waitUntil(t, func() bool { h.witness.Tick(1); return cursorIn(h.witness, h.slot) }, "the witness seeing the guest arrive")
	}
	stop()
	if start {
		h.host.Context().WaitStart.Store(true)
		mode.ExecuteCommand(h.host.Context(), "n")
	} else {
		mode.ExecuteCommand(h.host.Context(), "pause")
	}
	h.host.Settle()
	// Crossed before the hold lands, the guest's own moves wait on the barrier with it
	for range parameter.NetworkBarrierMaxDelayTicks + 4 {
		inject(t, guest, intentMotion(input.MotionRight, 1))
		h.step(1, guest)
	}
	if !h.host.Context().TimeCtl.Held() {
		t.Fatal("the host's session did not hold")
	}
	h.lose(t, guest, port)
	return h
}

// step ticks the host, its witness and each of others n times, a link's moment apart.
func (h heldAway) step(n int, others ...*App) {
	for range n {
		for _, a := range append([]*App{h.host, h.witness}, others...) {
			if a != nil {
				a.Tick(1)
			}
		}
		time.Sleep(joinTestTickInterval / 5)
	}
}

// lose closes a run in the guest's place and waits for the host to cross it out.
func (h heldAway) lose(t *testing.T, run *App, port *link.SocketPort) {
	t.Helper()
	_ = port.Close()
	run.Close()
	waitUntil(t, func() bool { h.step(1); return slices.Contains(departing(h.host), uint32(h.id)) },
		"the held host crossing the lost guest out")
}

// back joins a run under the lost guest's key while the host stands held, and
// fails unless it is seated in that guest's place, the cursor there driven by nobody.
func (h heldAway) back(t *testing.T, opts ...func(*Config)) (*App, *link.SocketPort) {
	t.Helper()
	stop := tickInBackground(h.host)
	run, port := mustSocketJoiner(t, h.host.HostAddr(), fixtureSeed, 120, 40,
		append(opts, func(c *Config) { c.Membership.PlaceStore = h.store })...)
	stop()
	if got := run.membership.LocalParticipant(); got != uint32(h.id) || run.membership.LocalSlot() != parameter.NoPlayerSlot ||
		!cursorIn(h.host, h.slot) || wraithIn(h.host, h.slot) || !run.Context().TimeCtl.Held() {
		t.Fatalf("under the hold the key was seated as %d driving slot %d, held %v, the host's cursor a Wraith %v; "+
			"want participant %d seated held, driving none, its cursor live",
			got, run.membership.LocalSlot(), run.Context().TimeCtl.Held(), wraithIn(h.host, h.slot), h.id)
	}
	assertSharedParity(t, h.host, run, 0)
	return run, port
}

// resume ends the host's pause and ticks everyone until what it held has applied.
func (h heldAway) resume(others ...*App) {
	mode.ExecuteCommand(h.host.Context(), "pause off")
	h.host.Settle()
	h.step(2*parameter.NetworkBarrierMaxDelayTicks, others...)
}

// TestAHeldSessionTakesBackAPlayerWhoseLinkDropped: a held session reads a departure
// it crossed and cannot apply as away, so the key takes its place back held, its
// cursor live and driven by nobody. However the hold ends, the departure and the
// arrival behind it apply in order on every instance, and the player drives it.
func TestAHeldSessionTakesBackAPlayerWhoseLinkDropped(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	for _, shape := range []struct {
		name           string
		start, newGame bool // held waiting for its start, not by a pause; ended by a new game
	}{{name: "paused, resumed"}, {name: "paused, a new game", newGame: true},
		{name: "waiting for its start, pressed", start: true}, {name: "waiting for its start, a new game", start: true, newGame: true}} {
		t.Run(shape.name, func(t *testing.T) {
			h := heldAwayGuest(t, shape.start, !shape.start && !shape.newGame)
			back, _ := h.back(t)
			switch {
			case shape.newGame:
				h.host.Context().WaitStart.Store(false)
				mode.ExecuteCommand(h.host.Context(), "n")
				h.host.Settle()
				h.step(2*parameter.NetworkBarrierMaxDelayTicks, back)
			case shape.start:
				if !startPress(h.host) {
					t.Fatal("the host's press did not start its game")
				}
				h.step(2*parameter.NetworkBarrierMaxDelayTicks, back)
			default:
				h.resume(back)
			}
			for _, a := range []*App{h.host, back, h.witness} {
				if a != nil && (wraithIn(a, h.slot) || !cursorIn(a, h.slot) || a.Context().TimeCtl.Held()) {
					t.Fatalf("participant %d: the place's cursor waits as a Wraith %v, held %v; want it live and the session running",
						a.membership.LocalParticipant(), wraithIn(a, h.slot), a.Context().TimeCtl.Held())
				}
			}
			if got := back.membership.LocalSlot(); got != h.slot || !portOf(t, h.host).Connected(uint32(h.id)) {
				t.Fatalf("the returned run drives slot %d, linked %v; want its own slot, %d, on the link it came back by",
					got, portOf(t, h.host).Connected(uint32(h.id)), h.slot)
			}
			for step, a := range []*App{back, h.witness} {
				if a != nil {
					settleCorrections(t, h.host, a)
					alignTicks(t, h.host, a)
					assertSharedParity(t, h.host, a, step+1)
				}
			}
			var refusal *network.Refusal
			if _, _, err := dialOnce(h.host.HostAddr(), h.store.key(t)); !errors.As(err, &refusal) || refusal.Code != network.RefusalReturning {
				t.Fatalf("a further dial under the key of a place its run plays in: %v; want it refused as linked", err)
			}
		})
	}
}

// TestAPlayerLostAgainUnderOneHoldIsCrossedOutAgain: a player taken back held whose
// link drops again under the same hold is crossed out again behind its arrival: it
// waits as a Wraith once the hold ends, its key good for the place, unless it came
// back once more, and then it plays.
func TestAPlayerLostAgainUnderOneHoldIsCrossedOutAgain(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	for _, again := range []bool{false, true} {
		h := heldAwayGuest(t, false, false)
		back, port := h.back(t)
		h.lose(t, back, port)
		if again {
			back, _ = h.back(t)
		}
		if !again {
			h.resume()
			if !wraithIn(h.host, h.slot) {
				t.Fatal("a player lost twice under one hold does not wait as a Wraith once it ends")
			}
			stop := tickInBackground(h.host)
			pending, offered, err := dialOnce(h.host.HostAddr(), h.store.key(t))
			stop()
			if err != nil || offered.Assigned != h.id || assignedSlot(offered) != h.slot {
				t.Fatalf("its key was given %d on slot %d (%v); want its own place, %d on slot %d",
					offered.Assigned, assignedSlot(offered), err, h.id, h.slot)
			}
			_ = pending.Close()
			continue
		}
		h.resume(back)
		if got := back.membership.LocalSlot(); got != h.slot || wraithIn(h.host, h.slot) || wraithIn(back, h.slot) {
			t.Fatalf("back a second time the run drives slot %d, its cursor a Wraith on the host %v and its own world %v; want slot %d, live",
				got, wraithIn(h.host, h.slot), wraithIn(back, h.slot), h.slot)
		}
		settleCorrections(t, h.host, back)
		alignTicks(t, h.host, back)
		assertSharedParity(t, h.host, back, 1)
	}
}

// TestAHoldEndingInsideAReturnLeavesItsDialLinked: a hold that ends between a
// returning dial's seat and its world applies the departure with no arrival behind
// it yet. The place waits as a Wraith, the link is the dial's and stays, and its
// arrival takes the Wraith back.
func TestAHoldEndingInsideAReturnLeavesItsDialLinked(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	h := heldAwayGuest(t, false, false)
	stop := tickInBackground(h.host)
	pending, offered := dialSession(t, h.host.HostAddr(), h.store.key(t))
	cfg := pending.TransportConfig()
	if err := pending.Complete(nil, network.JoinerReport{Identity: offered.Identity}); err != nil {
		t.Fatal(err)
	}
	if _, err := pending.WaitStart(); err != nil {
		t.Fatal(err)
	}
	stop()
	h.resume()
	if !wraithIn(h.host, h.slot) || !portOf(t, h.host).Connected(uint32(h.id)) {
		t.Fatalf("resumed inside the return, the place waits as a Wraith %v and its dial is linked %v; want both",
			wraithIn(h.host, h.slot), portOf(t, h.host).Connected(uint32(h.id)))
	}
	stop = tickInBackground(h.host)
	defer stop()
	if _, _, err := pending.ReceiveSnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Ready(); err != nil {
		t.Fatal(err)
	}
	port := link.NewSocketPort(cfg)
	if err := port.Start(); err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	waitUntil(t, func() bool { return cursorIn(h.host, h.slot) && !wraithIn(h.host, h.slot) }, "the returning dial taking its Wraith back")
}

// TestARunWhoseHostGoesBeforeItsArrivalDrivesItsOwnCursor: a run that took its
// place back under a hold and loses its host before the arrival could apply plays
// on alone in the cursor its place holds, which nothing else will give it.
func TestARunWhoseHostGoesBeforeItsArrivalDrivesItsOwnCursor(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	h := heldAwayGuest(t, false, false)
	back, _ := h.back(t)
	back.World().RunSafe(func() { back.cfg.Mode, back.cfg.Membership.JoinAddress = ModePlay, h.host.HostAddr() })
	h.host.Close()
	stop := tickInBackground(back)
	waitUntil(t, back.membership.Leaving, "the returned run noticing its host is gone")
	stop()
	back.membership.LeaveSession()
	var before, after component.PositionComponent
	back.World().RunSafe(func() { before, _ = back.World().Positions.GetPosition(back.World().Resources.Player.Slot(h.slot)) })
	back.Tick(2)
	inject(t, back, intentMotion(input.MotionRight, 3))
	back.Tick(4)
	back.World().RunSafe(func() { after, _ = back.World().Positions.GetPosition(back.World().Resources.Player.Slot(h.slot)) })
	if got := back.membership.LocalSlot(); got != h.slot || back.World().Session().Superseding || after.X <= before.X {
		t.Fatalf("alone, the run drives slot %d (superseding %v) and its move took the cursor from x=%d to x=%d; want slot %d, moved right",
			got, back.World().Session().Superseding, before.X, after.X, h.slot)
	}
}

// TestAPlayerComesBackThroughItsRelayWhileTheSessionIsHeld: a relay's door reads
// what the authority's does, so a player who joined through it and dropped under a
// hold is taken back through it held, and drives its cursor once the hold ends. A
// hold that ends inside the return leaves the dial its relay's link all the same.
func TestAPlayerComesBackThroughItsRelayWhileTheSessionIsHeld(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	for _, inside := range []bool{false, true} {
		addr, relayAddr := freeAddress(t), freeAddress(t)
		host, _ := driveBot(t, Config{Membership: Membership{HostAddress: addr, Bots: []string{"roam"}}}, "patrol", nil)
		waitForCursors(t, host, 2)
		relay, _ := driveBot(t, Config{Membership: Membership{JoinAddress: addr, RelayAddress: relayAddr}}, "patrol", nil)
		waitForCursors(t, host, 3)
		waitListening(t, relayAddr)
		store := &keptPlaces{}
		person, leave := driveBot(t, Config{Membership: Membership{JoinAddress: relayAddr, PlaceStore: store}}, "roam", nil)
		for _, a := range []*App{host, relay, person} {
			waitForCursors(t, a, 4)
		}
		id, slot := person.membership.LocalParticipant(), person.membership.LocalSlot()
		hold := func(held bool) {
			host.world.RunSafe(func() { host.ctx.PushLocal(event.EventGameHoldRequest, &event.GamePausePayload{Paused: held}) })
			host.Settle()
		}
		hold(true)
		waitUntil(t, func() bool {
			return host.Context().TimeCtl.Held() && relay.Context().TimeCtl.Held() && person.Context().TimeCtl.Held()
		}, "the session standing held")
		leave()
		person.Close()
		waitUntil(t, func() bool { return slices.Contains(departing(host), id) }, "the held host crossing the lost player out")
		if !inside {
			back, _ := driveBot(t, Config{Membership: Membership{JoinAddress: relayAddr, PlaceStore: store}}, "roam", nil)
			waitUntil(t, func() bool { return back.Context().TimeCtl.Held() }, "the returning run landing held")
			if got := back.membership.LocalParticipant(); got != id || back.membership.LocalSlot() != parameter.NoPlayerSlot || wraithIn(host, slot) {
				t.Fatalf("under the hold the relay seated the key as %d driving slot %d, the host's cursor a Wraith %v; want %d, driving none",
					got, back.membership.LocalSlot(), wraithIn(host, slot), id)
			}
			hold(false)
			waitUntil(t, func() bool {
				return back.membership.LocalSlot() == slot && !wraithIn(host, slot) && !wraithIn(relay, slot)
			},
				"the returned run driving its cursor")
			continue
		}
		pending, offered := dialSession(t, relayAddr, store.key(t))
		if uint32(offered.Assigned) != id || assignedSlot(offered) != slot {
			t.Fatalf("the relay seated the key as %d on slot %d; want its own place, %d on slot %d", offered.Assigned, assignedSlot(offered), id, slot)
		}
		cfg := pending.TransportConfig()
		if err := pending.Complete(nil, network.JoinerReport{Identity: offered.Identity}); err != nil {
			t.Fatal(err)
		}
		if _, err := pending.WaitStart(); err != nil {
			t.Fatal(err)
		}
		hold(false)
		waitUntil(t, func() bool { return wraithIn(host, slot) && wraithIn(relay, slot) }, "the departure applying on the host and the relay")
		if _, _, err := pending.ReceiveSnapshot(); err != nil {
			t.Fatalf("the relay closed the link of the dial taking the place back: %v", err)
		}
		if err := pending.Ready(); err != nil {
			t.Fatal(err)
		}
		port := link.NewSocketPort(cfg)
		if err := port.Start(); err != nil {
			t.Fatal(err)
		}
		defer port.Close()
		waitUntil(t, func() bool { return cursorIn(host, slot) && !wraithIn(host, slot) && !wraithIn(relay, slot) },
			"the returning dial taking its Wraith back")
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
	doors := portOf(t, host).Doors()
	if len(doors) != 2 || !strings.HasPrefix(doors[1].String(), "ws://") {
		t.Fatalf("the host answers at %v; want a tcp door and a ws door", doors)
	}
	if host.cfg.Membership.HostAddress != "127.0.0.1:0,ws://127.0.0.1:0" {
		t.Fatalf("the host records its doors as %q", host.cfg.Membership.HostAddress)
	}
	guest, _ := driveBot(t, Config{Membership: Membership{JoinAddress: doors[1].String() + "/"}}, "patrol", nil)
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
		guest.cfg.Mode, guest.cfg.Membership.JoinAddress = ModePlay, "10.0.0.1:7777" // a joined run with a loop; the rule is the address
		guest.World().RunSafe(func() { guest.world.Resources.Network.OnSessionRestart(1, payload) })
		req := guest.restart.Load()
		if req == nil || !req.Rejoin || req.Join != want {
			t.Errorf("a restart at %q latched %+v; want a rejoin at %q", payload, req, want)
		}
	}
}

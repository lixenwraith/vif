package app

import (
	"bytes"
	"cmp"
	"compress/flate"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	link "github.com/lixenwraith/vif/pkg/network"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestNetcodeReadingsOverSockets takes doc/netcode-optimization.md §2's readings on
// the socket path, which the in-process mesh has none of: bytes by kind before each
// link's deflate and on the wire after it, with the rounds, proofs and repairs
// beside them, in multi-player §6's shape paced at the tick. It prints what it
// measured and runs only when asked, VIF_NETCODE_READINGS=1: it is minutes of whole
// worlds, and an approach is judged by it, not a build.
// readingRepeats is how many times a td row is taken: its rows vary run to run with
// what the storm does to the roster over real sockets, so the run at the median
// keyframe rate is reported, every run's rate beside it. VIF_NETCODE_READINGS_RUNS
// sets the count for every row.
const readingRepeats = 3

func TestNetcodeReadingsOverSockets(t *testing.T) {
	if os.Getenv("VIF_NETCODE_READINGS") == "" {
		t.Skip("set VIF_NETCODE_READINGS=1 to take the readings")
	}
	td := Config{Mode: ModeHeadless, Width: 200, Height: 60, Resources: resource.Options{Dir: soakRoot, Scenario: "td"}}
	for _, row := range []struct {
		name    string
		cfg     Config
		players int
		region  string
		runs    int
	}{
		{"tower", towerScenario(t, 0x70E4), 2, "tower", 1},
		{"tower", towerScenario(t, 0x70E4), 4, "tower", 1},
		{"tower", towerScenario(t, 0x70E4), 16, "tower", 1},
		{"td", td, 2, "", readingRepeats},
		{"td", td, 4, "", readingRepeats},
	} {
		t.Run(fmt.Sprintf("%s-%d", row.name, row.players), func(t *testing.T) {
			runs := row.runs
			if n, err := strconv.Atoi(os.Getenv("VIF_NETCODE_READINGS_RUNS")); err == nil && n > 0 {
				runs = n
			}
			readings := make([]reading, 0, runs)
			for i := range runs {
				// Each run in a test of its own, so its session is closed before the next.
				t.Run(fmt.Sprintf("run-%d", i+1), func(t *testing.T) {
					readings = append(readings, takeReading(t, row.name, row.cfg, row.players, row.region, i))
				})
			}
			if len(readings) == 0 {
				return
			}
			slices.SortStableFunc(readings, func(a, b reading) int { return cmp.Compare(a.keyframes, b.keyframes) })
			for _, line := range readings[len(readings)/2].lines {
				t.Log(line)
			}
			if len(readings) > 1 {
				rates := make([]string, len(readings))
				for i, r := range readings {
					rates[i] = fmt.Sprintf("%.2f/s at %.1f%%", r.keyframes, r.hashOnly)
				}
				t.Logf("  %d runs, keyframes and hash-only share each, sorted: %s; the run at the median keyframe rate is the one above",
					len(readings), strings.Join(rates, ", "))
			}
		})
	}
}

// reading is one run of a row: its lines, and what picks the run to report.
type reading struct {
	lines     []string
	keyframes float64 // the host's keyframes a second
	hashOnly  float64 // the guests' hash-only share, in percent
}

// takeReading runs §6's profile over sockets: a star, every participant moving one
// tick in three and firing one in six, 300 ticks after 120 of warm-up. attempt
// numbers a repeated row's runs, for the profile each writes.
func takeReading(t *testing.T, name string, cfg Config, players int, region string, attempt int) reading {
	t.Helper()
	var r reading
	say := func(format string, args ...any) { r.lines = append(r.lines, fmt.Sprintf(format, args...)) }
	apps, hostPort := socketSession(t, cfg, players)
	host, guests := apps[0], apps[1:]
	for _, a := range apps {
		injectExCommand(t, a, "god")
	}
	if region != "" {
		for _, a := range apps {
			a.World().RunSafe(func() {
				a.World().PushEventDomain(event.EventFSMRegionRequest,
					&event.FSMRegionPayload{Op: event.RegionSpawn, Region: region, State: "TowerSetup"},
					core.DomainShared)
			})
			a.Settle()
		}
	}
	rng := vmath.NewFastRand(7)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	var hostTime, guestTime time.Duration
	run := func(ticks int, measure bool) {
		next := time.Now()
		for range ticks {
			for _, a := range apps {
				if rng.Intn(3) == 0 {
					inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
				}
				if rng.Intn(6) == 0 {
					inject(t, a, &input.Intent{Type: input.IntentFireMain, Count: 1})
				}
			}
			start := time.Now()
			host.Tick(1)
			_ = host.corrections.PublishDue()
			if measure {
				hostTime += time.Since(start)
			}
			start = time.Now()
			for _, g := range guests {
				g.Tick(1)
				g.ApplyPendingCorrections()
			}
			if measure {
				guestTime += time.Since(start)
			}
			next = next.Add(parameter.GameUpdateInterval)
			time.Sleep(time.Until(next))
		}
	}
	const warm, measured = 120, 300
	run(warm, false)
	seconds := float64(measured) * parameter.GameUpdateInterval.Seconds()

	before := hostPort.LinkTotals()
	hostBefore := snapshotStats(host)
	guestBefore := make([]map[string]int64, len(guests))
	for i, g := range guests {
		guestBefore[i] = snapshotStats(g)
	}
	// VIF_NETCODE_PROFILE names a directory a CPU profile of each measured window is
	// written to, one per row, for the stage shares §2 asks for.
	if dir := os.Getenv("VIF_NETCODE_PROFILE"); dir != "" {
		suffix := ""
		if attempt > 0 {
			suffix = fmt.Sprintf("-run%d", attempt+1)
		}
		f, err := os.Create(fmt.Sprintf("%s/%s-%d%s.prof", dir, name, players, suffix))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			t.Fatal(err)
		}
		defer pprof.StopCPUProfile()
	}
	cpuBefore := cpuSeconds()
	run(measured, true)
	cpuAfter := cpuSeconds()
	after := hostPort.LinkTotals()
	hostAfter := snapshotStats(host)
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	perSecond := func(a, b uint64) float64 { return float64(b-a) / seconds }
	framed := 0.0
	var kinds []string
	for k := range after.Framed {
		rate := perSecond(before.Framed[k], after.Framed[k])
		framed += rate
		kinds = append(kinds, fmt.Sprintf("%s %.0f", link.ByteKind(k), rate))
	}
	wire := perSecond(before.WireOut, after.WireOut)
	delta := func(key string) float64 { return float64(hostAfter[key]-hostBefore[key]) / seconds }
	say("%s, %d players, %d peers, %.0f s simulated: host egress framed %.0f B/s (%s), wire %.0f B/s (%.2f of framed), ingress wire %.0f B/s",
		name, players, hostPort.PeerCount(), seconds, framed, strings.Join(kinds, ", "),
		wire, wire/framed, perSecond(before.WireIn, after.WireIn))
	say("  host: manifests %.1f/s, corrections %.1f/s, keyframes %.2f/s (fallbacks %.2f/s), shards %.1f/s; "+
		"bodies %.0f B/s, repairs %.0f B/s, manifests %.0f B/s; ticks %.1f ms each; chain %d",
		delta("snapshot.manifests_sent"), delta("snapshot.corrections_sent"), delta("snapshot.keyframes"),
		delta("snapshot.keyframe_fallbacks"), delta("snapshot.shards_sent"), delta("snapshot.correction_bytes_sent"),
		delta("snapshot.shard_bytes_sent"), delta("snapshot.manifest_bytes_sent"),
		float64(hostTime.Microseconds())/1000/measured, statOf(host, "network.chain"))
	var manifests, hashOnly, inPlace, applied, repaired, projected, lag int64
	for i, g := range guests {
		now := snapshotStats(g)
		manifests += now["snapshot.manifests_received"] - guestBefore[i]["snapshot.manifests_received"]
		hashOnly += now["snapshot.corrections_hash_only"] - guestBefore[i]["snapshot.corrections_hash_only"]
		inPlace += now["snapshot.manifests_in_place"] - guestBefore[i]["snapshot.manifests_in_place"]
		applied += now["snapshot.corrections_applied"] - guestBefore[i]["snapshot.corrections_applied"]
		repaired += now["snapshot.pages_repaired"] - guestBefore[i]["snapshot.pages_repaired"]
		projected = max(projected, now["snapshot.projected_ticks"])
		lag = max(lag, now["network.lag_ticks"])
	}
	share := 0.0
	if manifests > 0 {
		share = 100 * float64(hashOnly) / float64(manifests)
	}
	say("  guests: manifests %d, hash-only %.1f%% (%d in place), corrections applied %d, pages repaired %d, projected ticks at most %d, lag at most %d; "+
		"guest ticks %.1f ms each over all guests", manifests, share, inPlace, applied, repaired, projected, lag,
		float64(guestTime.Microseconds())/1000/measured)
	say("  process: %.2f CPU-s per simulated second over %d instances, heap in use %d MiB",
		(cpuAfter-cpuBefore)/seconds, players, mem.HeapInuse>>20)
	r.keyframes, r.hashOnly = delta("snapshot.keyframes"), share
	return r
}

// socketSession is a host and players-1 guests joined over real sockets, each link
// deflated as a session's is, ticked by the caller; the host's port is returned for
// its readings.
func socketSession(t *testing.T, cfg Config, players int) ([]*App, *link.SocketPort) {
	t.Helper()
	cfg.Mode = ModeHeadless
	host, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	host.cfg.Participants = players

	hostCfg := network.DebugConfig(link.RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = engine.CoordinatorParticipant
	hostCfg.MaxPeers = host.membership.sessionCapacity()
	hostCfg.AcceptSession = network.HostAcceptor(network.Coordinator{
		Assign: host.membership.assignParticipant, Release: host.membership.releaseParticipant, Scenario: host.membership.scenarioBody,
	}, socketWait)
	hostPort := link.NewSocketPort(&hostCfg.Config)
	t.Cleanup(func() { _ = hostPort.Close() })
	if err := hostPort.Start(); err != nil {
		t.Fatal(err)
	}
	host.AttachTransport(hostPort)
	hostStarted := make(chan error, 1)
	go func() { hostStarted <- host.membership.startHostSessionOn(hostPort, nil) }()

	guests := make([]*App, 0, players-1)
	for i := range players - 1 {
		pending, offered, err := network.DialSession(hostPort.Addr().String(), network.DebugConfig(link.RolePeer, ""))
		if err != nil {
			t.Fatalf("guest %d dial: %v", i+1, err)
		}
		t.Cleanup(func() { _ = pending.Close() })
		joinCfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: cfg.Width, Height: cfg.Height, Resources: cfg.Resources}, offered)
		if err != nil {
			t.Fatalf("guest %d config: %v", i+1, err)
		}
		g, err := NewHeadless(joinCfg)
		if err != nil {
			t.Fatalf("guest %d app: %v", i+1, err)
		}
		t.Cleanup(g.Close)
		g.membership.pendingJoin, g.membership.sessionOffer = pending, offered
		if err := g.Join(offered.Anchor); err != nil {
			_ = pending.Complete(err, network.JoinerReport{})
			t.Fatalf("guest %d identity: %v", i+1, err)
		}
		if err := pending.Complete(nil, g.membership.joinerReport()); err != nil {
			t.Fatalf("guest %d reply: %v", i+1, err)
		}
		guests = append(guests, g)
	}
	gated := make(chan error, len(guests))
	for _, g := range guests {
		go func() { gated <- g.membership.startJoinSession(nil) }()
	}
	for range guests {
		if err := <-gated; err != nil {
			t.Fatalf("guest startup: %v", err)
		}
	}
	if err := <-hostStarted; err != nil {
		t.Fatalf("host startup: %v", err)
	}
	for i, g := range guests {
		port := link.NewSocketPort(g.membership.pendingJoin.TransportConfig())
		t.Cleanup(func() { _ = port.Close() })
		if err := port.Start(); err != nil {
			t.Fatalf("guest %d transport: %v", i+1, err)
		}
		g.AttachTransport(port)
		waitSocket(t, port, func() bool { return port.PeerCount() == 1 }, fmt.Sprintf("guest %d peer", i+1))
	}
	waitSocket(t, hostPort, func() bool { return hostPort.PeerCount() == len(guests) }, "host peers")
	apps := append([]*App{host}, guests...)
	for _, a := range apps {
		a.membership.activateNetworkSession()
	}
	return apps, hostPort
}

// snapshotStats reads every counter a reading differences.
func snapshotStats(a *App) map[string]int64 {
	out := map[string]int64{}
	for _, key := range []string{"snapshot.manifests_sent", "snapshot.corrections_sent", "snapshot.keyframes",
		"snapshot.keyframe_fallbacks", "snapshot.shards_sent", "snapshot.correction_bytes_sent", "snapshot.shard_bytes_sent",
		"snapshot.manifest_bytes_sent", "snapshot.manifests_received", "snapshot.corrections_hash_only",
		"snapshot.corrections_applied", "snapshot.pages_repaired", "snapshot.projected_ticks", "network.lag_ticks"} {
		out[key] = statOf(a, key)
	}
	return out
}

// cpuSeconds is the process's user and system time so far.
func cpuSeconds() float64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6 + float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
}

// TestNetcodeReadingsForBatch3 takes the readings netcode design §4 batch 3 is
// decided and measured on, over a td session of two on sockets: a keyframe's bytes a
// section at a time, plain and deflated, as the JSON rows the manifest hashes and as
// the columns the wire carries; what a guest pays to decode and install one; and the
// heap by owner once the session has run. VIF_NETCODE_BATCH3=1 runs it, and
// VIF_NETCODE_PROFILE names a directory its heap profile is written to.
func TestNetcodeReadingsForBatch3(t *testing.T) {
	if os.Getenv("VIF_NETCODE_BATCH3") == "" {
		t.Skip("set VIF_NETCODE_BATCH3=1 to take the batch 3 readings")
	}
	td := Config{Mode: ModeHeadless, Width: 200, Height: 60, Resources: resource.Options{Dir: soakRoot, Scenario: "td"}}
	apps, _ := socketSession(t, td, 2)
	host, guest := apps[0], apps[1]
	for _, a := range apps {
		injectExCommand(t, a, "god")
	}
	rng := vmath.NewFastRand(7)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	peak := map[string]int64{}
	next := time.Now()
	for range 420 {
		for _, a := range apps {
			if rng.Intn(3) == 0 {
				inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
			}
			if rng.Intn(6) == 0 {
				inject(t, a, &input.Intent{Type: input.IntentFireMain, Count: 1})
			}
		}
		host.Tick(1)
		_ = host.corrections.PublishDue()
		guest.Tick(1)
		guest.ApplyPendingCorrections()
		for _, key := range []string{"snapshot.stage_us", "snapshot.commit_us", "snapshot.project_us", "snapshot.write_us", "snapshot.projected_ticks"} {
			peak[key] = max(peak[key], statOf(guest, key))
		}
		for _, key := range []string{"snapshot.capture_us", "snapshot.encode_us", "snapshot.hash_us"} {
			peak[key] = max(peak[key], statOf(host, key))
		}
		next = next.Add(parameter.GameUpdateInterval)
		time.Sleep(time.Until(next))
	}

	cap, err := host.CaptureShared()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("td keyframe at tick %d, a section at a time: rows, JSON plain, JSON deflated, columns plain, columns deflated", cap.Header.Tick)
	world := reflect.ValueOf(cap.World)
	var total [4]int
	for i, name := range engine.SharedWorldStoreNames {
		rows := world.Field(i + 3) // after NextEntity, Created, Destroyed
		if rows.Len() == 0 {
			continue
		}
		plain, err := json.Marshal(rows.Interface())
		if err != nil {
			t.Fatal(err)
		}
		columns := engine.SharedWorldEncodeStoreColumns(nil, &cap.World, i)
		line := [4]int{len(plain), len(deflated(plain)), len(columns), len(deflated(columns))}
		for j := range line {
			total[j] += line[j]
		}
		t.Logf("  %-12s %6d %9d %9d %9d %9d", name, rows.Len(), line[0], line[1], line[2], line[3])
	}
	t.Logf("  %-12s %6s %9d %9d %9d %9d", "all stores", "", total[0], total[1], total[2], total[3])

	body, err := snapshot.EncodeCapture(cap)
	if err != nil {
		t.Fatal(err)
	}
	var decode, install time.Duration
	for range 5 {
		start := time.Now()
		back, err := snapshot.DecodeCapture(body)
		if err != nil {
			t.Fatal(err)
		}
		decode += time.Since(start)
		start = time.Now()
		staged, err := guest.StageShared(back)
		if err != nil {
			t.Fatal(err)
		}
		if err := staged.Commit(); err != nil {
			t.Fatal(err)
		}
		install += time.Since(start)
	}
	t.Logf("  keyframe body %d bytes; a guest decodes it in %.1f ms and installs it in %.1f ms (stage and commit, no ticks projected)",
		len(body), float64(decode.Microseconds())/5000, float64(install.Microseconds())/5000)
	t.Logf("  over the run, the guest's deepest install: stage %d us, commit %d us (project %d us, write %d us), %d ticks projected; the host's capture %d us, encode %d us, hash %d us",
		peak["snapshot.stage_us"], peak["snapshot.commit_us"], peak["snapshot.project_us"], peak["snapshot.write_us"],
		peak["snapshot.projected_ticks"], peak["snapshot.capture_us"], peak["snapshot.encode_us"], peak["snapshot.hash_us"])

	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	t.Logf("  heap in use %d MiB over the two instances after a collection", mem.HeapInuse>>20)
	if dir := os.Getenv("VIF_NETCODE_PROFILE"); dir != "" {
		f, err := os.Create(dir + "/td-heap.prof")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
			t.Fatal(err)
		}
	}
}

// deflated is plain at the level the wire deflates a body at.
func deflated(plain []byte) []byte {
	var out bytes.Buffer
	w, _ := flate.NewWriter(&out, flate.DefaultCompression)
	_, _ = w.Write(plain)
	_ = w.Close()
	return out.Bytes()
}

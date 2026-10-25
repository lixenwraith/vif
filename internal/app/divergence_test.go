package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestADivergenceOverAShapedLinkNamesItsRows is the divergence harness, run with
// VIF_DIVERGENCE=1: a host and a guest fight over a shaped mesh link, a storm is
// raised on both at one tick, and every repair the guest applies logs the rows it
// moved, which the test prints with the link's late commits, pace steps and repairs.
// VIF_DIVERGENCE_SHAPE=latency,jitter,every,loss sets the shape, in ticks and frames.
func TestADivergenceOverAShapedLinkNamesItsRows(t *testing.T) {
	if os.Getenv("VIF_DIVERGENCE") == "" {
		t.Skip("set VIF_DIVERGENCE=1 to run the divergence harness")
	}
	shape := link.LinkShape{LatencyTicks: 3, JitterTicks: 3, JitterEvery: 97}
	if spec := os.Getenv("VIF_DIVERGENCE_SHAPE"); spec != "" {
		var loss int
		if _, err := fmt.Sscanf(spec, "%d,%d,%d,%d", &shape.LatencyTicks, &shape.JitterTicks, &shape.JitterEvery, &loss); err != nil {
			t.Fatalf("VIF_DIVERGENCE_SHAPE %q: %v", spec, err)
		}
		shape.LossEvery = loss
	}
	dir := t.TempDir()
	vlog.Configure(vlog.Config{Dir: dir, Level: "debug"})
	path, err := vlog.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vlog.Stop)

	host, guest, fight, leadMax := shapedFight(t, stormWorldSeed, shape)
	fight(400)
	before := divergenceReading(host, guest, *leadMax)
	raiseStormTogether(t, host, guest, fight)
	fight(800)
	after := divergenceReading(host, guest, *leadMax)
	t.Logf("shape %+v", shape)
	t.Logf("before the storm: %s", before)
	t.Logf("after the storm:  %s", after)
	vlog.Stop()
	for _, line := range fightLogged(t, path) {
		t.Log(line)
	}
}

// shapedFight is towerFight on the embedded scenario: two participants moving one
// tick in three and firing one in six over a shaped link, the host publishing on
// its pump and the guest applying what arrives, paced as its scheduler would pace
// it (shapedFightOf).
func shapedFight(t *testing.T, seed uint64, shape link.LinkShape) (host, guest *App, fight func(ticks int), leadMax *int64) {
	t.Helper()
	apps := meshSessionOf(t, Config{Seed: seed, Width: 120, Height: 40, Resources: resource.Options{Embedded: true}},
		2, [][2]int{{1, 2}})
	fight, leadMax = shapedFightOf(t, apps, shape)
	return apps[0], apps[1], fight, leadMax
}

// shapedFightOf is the fight over a session already built, whatever its shape:
// every participant moving one tick in three and firing one in six, the host
// publishing on its pump and each guest applying what arrives, paced as its
// scheduler would (the trim and step owed as fractions of a tick, a whole one
// skipping or adding a guest tick). leadMax is the highest lead any guest held.
func shapedFightOf(t *testing.T, apps []*App, shape link.LinkShape) (fight func(ticks int), leadMax *int64) {
	t.Helper()
	leadMax = new(int64)
	host, guests := apps[0], apps[1:]
	// A port shapes what it receives, so the shape on every port is each link both ways.
	for _, a := range apps {
		transportOf(t, a).SetShape(shape)
		injectExCommand(t, a, "god")
	}
	for range 120 {
		tickAll(apps)
	}
	for range shape.LatencyTicks {
		host.Tick(1)
		_ = host.corrections.PublishDue()
	}
	rng := vmath.NewFastRand(7)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	owed := make([]float64, len(guests))
	return func(ticks int) {
		for range ticks {
			for _, g := range guests {
				*leadMax = max(*leadMax, statOf(g, "network.barrier_delay_ticks"))
			}
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
			for i, g := range guests {
				for n := 1; n > 0 || owed[i] <= -1; n-- {
					if owed[i] >= 1 {
						owed[i]--
						break
					}
					if n <= 0 {
						owed[i]++
					}
					g.Tick(1)
					trim, step := g.scheduler.TakePace()
					owed[i] += float64(trim)/1000 + float64(step)
				}
				g.ApplyPendingCorrections()
			}
		}
	}, leadMax
}

// raiseStormTogether raises the storm at one simulation tick on both ends: on the
// host now and on the guest when its clock reaches the host's, so the request is no
// divergence of the harness's own, as the scenario FSM's would not be.
func raiseStormTogether(t *testing.T, host, guest *App, fight func(ticks int)) {
	t.Helper()
	raiseStormTogetherOn(t, []*App{host, guest}, fight)
}

// raiseStormTogetherOn raises the storm on the host now and on each guest as its
// clock reaches the host's tick, the fight going on meanwhile.
func raiseStormTogetherOn(t *testing.T, apps []*App, fight func(ticks int)) {
	t.Helper()
	host := apps[0]
	at := tickOf(host)
	raiseStorm(t, host)
	pending := slices.Clone(apps[1:])
	for len(pending) > 0 {
		fight(1)
		pending = slices.DeleteFunc(pending, func(guest *App) bool {
			switch tick := tickOf(guest); {
			case tick < at:
				return false
			case tick > at:
				t.Fatalf("guest at tick %d cannot raise the storm at %d", tick, at)
			}
			raiseStorm(t, guest)
			return true
		})
	}
}

func tickOf(a *App) uint64 { return a.World().Resources.Event.Queue.Stamp().Tick }

// divergenceReading is what the link did to the session so far, on both ends.
func divergenceReading(host, guest *App, leadMax int64) string {
	manifests := statOf(guest, "snapshot.manifests_received")
	hashOnly := statOf(guest, "snapshot.corrections_hash_only")
	share := 0.0
	if manifests > 0 {
		share = 100 * float64(hashOnly) / float64(manifests)
	}
	return strings.Join([]string{
		"manifests " + itoa(manifests), "hash-only " + ftoa(share) + "%",
		"in place " + itoa(statOf(guest, "snapshot.manifests_in_place")),
		"pages repaired " + itoa(statOf(guest, "snapshot.pages_repaired")),
		"cells repaired " + itoa(statOf(guest, "snapshot.cells_repaired")),
		"keyframes " + itoa(statOf(host, "snapshot.keyframes")),
		"late commits " + itoa(statOf(host, "network.commit_late")),
		"late copies " + itoa(statOf(guest, "network.barrier_late")),
		"guest lead " + itoa(statOf(guest, "network.barrier_delay_ticks")) + " (max " + itoa(leadMax) + ")",
		"pace steps " + itoa(statOf(guest, "network.pace_steps")),
		"storm spawned " + itoa(statOf(host, "storm.spawned")),
		"cadence " + cadenceOf(host, guest),
	}, ", ")
}

// cadenceOf is what the host's controller decided for the guest's link: the cadence
// in ticks, whether the link read as the limit and as constrained, and the rate the
// meter read it at.
func cadenceOf(host, guest *App) string {
	id := guest.membership.localParticipant()
	for _, p := range host.corrections.Cadence().Peers {
		if p.Participant == id {
			return itoa(int64(p.CadenceTicks)) + " ticks (constrained " + strconv.FormatBool(p.Constrained) +
				", priced on " + itoa(int64(p.CapacityBps)) + " B/s, link " + itoa(int64(p.ThroughputBps)) +
				" B/s, saturated " + strconv.FormatBool(p.Saturated) + ")"
		}
	}
	return "none"
}

// fightLogged reads the repairs and keyframes the guest installed, the rows each
// repair moved and the storm region's transitions, one line per repair, row or
// transition; both instances log a transition, so one taken alike appears twice.
func fightLogged(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var rec struct {
			Tick   uint64 `json:"tick"`
			Fields struct {
				Msg      string `json:"msg"`
				Tick     uint64 `json:"tick"`
				Rows     string `json:"rows"`
				Sections string `json:"sections"`
				Cells    int    `json:"cells"`
				Entities int    `json:"correction_entities"`
				Region   string `json:"region"`
				From     string `json:"from"`
				To       string `json:"to"`
				Via      string `json:"via"`
			} `json:"fields"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch rec.Fields.Msg {
		case "capture installed":
			out = append(out, "tick "+itoa(int64(rec.Fields.Tick))+" keyframe installed ("+itoa(int64(rec.Fields.Entities))+" entities)")
		case "repair applied":
			out = append(out, "tick "+itoa(int64(rec.Tick))+" repaired "+rec.Fields.Sections+" ("+itoa(int64(rec.Fields.Cells))+" cells)")
		case "repair rows":
			for _, row := range strings.Split(rec.Fields.Rows, " | ") {
				out = append(out, "    "+row)
			}
		case "transition":
			if rec.Fields.Region == "storm" {
				out = append(out, "tick "+itoa(int64(rec.Tick))+" storm "+rec.Fields.From+" -> "+rec.Fields.To+" via "+rec.Fields.Via)
			}
		}
	}
	return out
}

func itoa(v int64) string   { return strconv.FormatInt(v, 10) }
func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

// TestARelayedTopologyConvergesUnderShape: a host, a relay and a person behind it
// fight over links shaped both ways, through a storm. The person converges on
// manifests answered hash-only as the relay does, its artifacts reaching the
// authority through the relay and the authority's reaching it a hop later; the
// authority sends to its one link alone, and the relay fans out to the person.
func TestARelayedTopologyConvergesUnderShape(t *testing.T) {
	t.Parallel()
	shape := link.LinkShape{LatencyTicks: 3, JitterTicks: 3, JitterEvery: 97}
	apps := meshSessionOf(t, Config{Seed: stormWorldSeed, Width: 120, Height: 40, Resources: resource.Options{Embedded: true}},
		3, [][2]int{{1, 2}, {2, 3}})
	host, relay, person := apps[0], apps[1], apps[2]
	local := localCursors(t, apps)
	fight, leadMax := shapedFightOf(t, apps, shape)
	fight(300)
	raiseStormTogetherOn(t, apps, fight)
	fight(600)
	for name, guest := range map[string]*App{"relay": relay, "person": person} {
		reading := divergenceReading(host, guest, *leadMax)
		t.Logf("%s: %s", name, reading)
		manifests, hashOnly := statOf(guest, "snapshot.manifests_received"), statOf(guest, "snapshot.corrections_hash_only")
		if manifests < 50 || hashOnly*100 < manifests*85 {
			t.Fatalf("the %s converged on %d of %d manifests hash-only; want most of many", name, hashOnly, manifests)
		}
	}
	if peers := transportOf(t, host).PeerCount(); peers != 1 {
		t.Fatalf("the authority sends to %d links, want its one", peers)
	}
	if forwarded, refused := statOf(relay, "network.relay_forwarded"), statOf(relay, "network.relay_refused"); forwarded == 0 || refused != 0 {
		t.Fatalf("the relay forwarded %d batches and refused %d, want many and none", forwarded, refused)
	}
	if moved := cursorPosition(host, local[2]); moved == cursorPosition(person, local[2]) && statOf(host, "network.crossings_received") == 0 {
		t.Fatal("the person's crossings never reached the authority through the relay")
	}
}

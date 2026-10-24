package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
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
	shape := network.LinkShape{LatencyTicks: 3, JitterTicks: 3, JitterEvery: 97}
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
	for _, line := range repairRowsLogged(t, path) {
		t.Log(line)
	}
}

// shapedFight is towerFight on the embedded scenario: two participants moving one
// tick in three and firing one in six over a shaped link, the host publishing on
// its pump and the guest applying what arrives, paced as its scheduler would pace
// it: the trim and step each tick leaves are owed as fractions of a tick, and a
// whole one owed skips or adds a guest tick. leadMax is the highest lead the guest
// held, the input latency the link's spikes cost it.
func shapedFight(t *testing.T, seed uint64, shape network.LinkShape) (host, guest *App, fight func(ticks int), leadMax *int64) {
	t.Helper()
	leadMax = new(int64)
	apps := meshSessionOf(t, Config{Seed: seed, Width: 120, Height: 40, Resources: resource.Options{Embedded: true}},
		2, [][2]int{{1, 2}})
	host, guest = apps[0], apps[1]
	// A port shapes what it receives, so the shape on both is the link both ways.
	transportOf(t, host).SetShape(shape)
	transportOf(t, guest).SetShape(shape)
	for _, a := range apps {
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
	owed := 0.0
	return host, guest, func(ticks int) {
		for range ticks {
			*leadMax = max(*leadMax, statOf(guest, "network.barrier_delay_ticks"))
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
			for n := 1; n > 0 || owed <= -1; n-- {
				if owed >= 1 {
					owed--
					break
				}
				if n <= 0 {
					owed++
				}
				guest.Tick(1)
				trim, step := guest.scheduler.TakePace()
				owed += float64(trim)/1000 + float64(step)
			}
			guest.ApplyPendingCorrections()
		}
	}, leadMax
}

// raiseStormTogether raises the storm at one simulation tick on both ends: on the
// host now and on the guest when its clock reaches the host's, so the request is no
// divergence of the harness's own, as the scenario FSM's would not be.
func raiseStormTogether(t *testing.T, host, guest *App, fight func(ticks int)) {
	t.Helper()
	at := tickOf(host)
	raiseStorm(t, host)
	for tickOf(guest) < at {
		fight(1)
	}
	if tickOf(guest) != at {
		t.Fatalf("guest at tick %d cannot raise the storm at %d", tickOf(guest), at)
	}
	raiseStorm(t, guest)
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
		"pages repaired " + itoa(statOf(guest, "snapshot.pages_repaired")),
		"cells repaired " + itoa(statOf(guest, "snapshot.cells_repaired")),
		"keyframes " + itoa(statOf(host, "snapshot.keyframes")),
		"late commits " + itoa(statOf(host, "network.commit_late")),
		"late copies " + itoa(statOf(guest, "network.barrier_late")),
		"guest lead " + itoa(statOf(guest, "network.barrier_delay_ticks")) + " (max " + itoa(leadMax) + ")",
		"pace steps " + itoa(statOf(guest, "network.pace_steps")),
		"storm spawned " + itoa(statOf(host, "storm.spawned")),
	}, ", ")
}

// repairRowsLogged reads the repairs and keyframes the guest installed and the rows
// each repair moved, one line per repair or row.
func repairRowsLogged(t *testing.T, path string) []string {
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
		}
	}
	return out
}

func itoa(v int64) string   { return strconv.FormatInt(v, 10) }
func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

package app

import (
	"runtime"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// The storm high water. Phase 2 measured a quiet world — around four kilobytes with
// three swarms and a gold sequence — and said plainly that the figure Phase 4's
// snapshot cadence has to be chosen from was still owed. The cadence question is
// "how often can the host afford to send this", and the answer is decided by the
// worst case, not by the resting one. A storm is the worst case this world has: it
// is the encounter that puts the most shared entities on the map at once.
//
// What is measured, and why each one:
//
//   - Bytes, because that is what the link carries and what the 2–5 Hz hypothesis
//     in §2.1 was a guess about.
//   - Capture time, because it is taken under the world lock and is therefore a
//     tick the host does not run. It is the bounded pause Phase 3 is allowed.
//   - Encode time separately, because it runs outside the lock and does not stall
//     the host — a number that matters for the link, not for the pause.
//   - Stage and commit time, because those are the joiner's cost and they decide
//     how many ticks a join has to catch up afterwards.
//   - Allocated bytes, because a capture at cadence allocates at cadence.
//
// The numbers are reported rather than asserted. A threshold here would be a
// performance test pretending to be a correctness one, and would fail on a loaded
// machine for reasons that say nothing about the code. The one thing that is
// asserted is that the storm actually reached its high water, because a measurement
// of a world without one measures nothing.

// TestSnapshotCostAtTheStormHighWater reports what a capture costs when the world
// is at its fullest.
func TestSnapshotCostAtTheStormHighWater(t *testing.T) {
	quiet := measureCaptureCost(t, stormWorld(t, 0))
	t.Logf("quiet world  : tick %4d | entities %4d shared %4d | %6d bytes | "+
		"capture %9s encode %9s | stage %9s commit %9s | %d KiB allocated",
		quiet.tick, quiet.entities, quiet.shared, quiet.bytes, quiet.capture, quiet.encode,
		quiet.stage, quiet.commit, quiet.allocKiB)

	// The peak is found on one world and measured on a second driven to the same
	// tick. The simulation is deterministic, so the two are the same world; walking
	// and measuring at once would have to capture at every rising tick, and the
	// captures themselves are what is being timed.
	peakTick, peakShared := findStormHighWater(t)
	storm := measureCaptureCost(t, stormWorld(t, peakTick))
	if storm.shared != peakShared {
		t.Fatalf("the second world holds %d shared entities at tick %d, the first held %d; "+
			"the walk and the measurement are not on the same world",
			storm.shared, peakTick, peakShared)
	}
	if storm.shared <= quiet.shared {
		t.Fatalf("the storm added no shared entities (%d then %d); this measures a quiet world",
			quiet.shared, storm.shared)
	}
	t.Logf("storm high water: tick %4d | entities %4d shared %4d | %6d bytes | "+
		"capture %9s encode %9s | stage %9s commit %9s | %d KiB allocated",
		storm.tick, storm.entities, storm.shared, storm.bytes, storm.capture, storm.encode,
		storm.stage, storm.commit, storm.allocKiB)

	// The figure Phase 4 chooses a cadence from, stated as what the cadence is
	// actually limited by: the share of a tick one capture costs the host, and the
	// uplink a full snapshot at each end of the hypothesised range would need.
	t.Logf("host stall %.2f%% of a 50ms tick | full snapshots: %.1f KiB/s at 5 Hz, %.1f KiB/s at 2 Hz | "+
		"%.1fx the quiet world",
		100*float64(storm.capture)/float64(50*time.Millisecond),
		5*float64(storm.bytes)/1024, 2*float64(storm.bytes)/1024,
		float64(storm.bytes)/float64(quiet.bytes))
}

// stormWorldSeed and stormWorldWarm put the run somewhere a storm can be raised.
const (
	stormWorldSeed = 0x570124
	stormWorldWarm = 200
)

// stormWorld builds a run, raises a storm, and drives it to the given tick. A tick
// of zero stops before the storm, which is the quiet baseline.
func stormWorld(t *testing.T, until uint64) *App {
	t.Helper()
	a := mustHeadless(t, stormWorldSeed, 120, 40)
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)
	a.Tick(stormWorldWarm)
	if until == 0 {
		return a
	}
	raiseStorm(t, a)
	for a.Position().Tick < until {
		a.Tick(1)
	}
	return a
}

// raiseStorm requests the encounter that fills the map.
func raiseStorm(t *testing.T, a *App) {
	t.Helper()
	a.World().RunSafe(func() {
		a.World().PushEventDomain(event.EventStormSpawnRequest, nil, core.DomainShared)
	})
	a.Settle()
}

// findStormHighWater walks a storm and returns the tick its shared population
// peaked at, with the count.
func findStormHighWater(t *testing.T) (uint64, int) {
	t.Helper()
	a := mustHeadless(t, stormWorldSeed, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(stormWorldWarm)
	raiseStorm(t, a)

	var peakTick uint64
	peak, falling := 0, 0
	for range 600 {
		a.Tick(1)
		shared := sharedPlacements(a)
		if shared > peak {
			peak, peakTick, falling = shared, a.Position().Tick, 0
			continue
		}
		// The peak is over once the population has stayed below it long enough that
		// one despawn cannot be mistaken for the top of the curve.
		if falling++; falling > 60 && peak > 0 {
			break
		}
	}
	if peak == 0 {
		t.Fatal("the storm produced no shared entities")
	}
	return peakTick, peak
}

// sharedPlacements counts the shared half of the placement store.
func sharedPlacements(a *App) int {
	var n int
	a.World().RunSafe(func() {
		for _, e := range a.World().Positions.Entities() {
			if e.Domain() == core.DomainShared {
				n++
			}
		}
	})
	return n
}

type captureCost struct {
	entities, shared int
	bytes            int
	capture, encode  time.Duration
	stage, commit    time.Duration
	allocKiB         uint64
	tick             uint64
}

// measureCaptureCost takes one capture and one staged install and reports both.
func measureCaptureCost(t *testing.T, a *App) captureCost {
	t.Helper()
	var out captureCost
	a.World().RunSafe(func() { out.entities = a.World().Positions.CountEntities() })
	out.shared = sharedPlacements(a)
	out.tick = a.Position().Tick

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	start := time.Now() // [wall] cost measurement, not simulation
	cap, err := a.CaptureShared()
	out.capture = time.Since(start)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	start = time.Now() // [wall]
	body, err := EncodeCapture(cap)
	out.encode = time.Since(start)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out.bytes = len(body)

	runtime.ReadMemStats(&after)
	out.allocKiB = (after.TotalAlloc - before.TotalAlloc) / 1024

	// The joiner's half, measured on a second instance so the staging world is
	// resolved against a world that is not the one it came from.
	receiver := mustHeadless(t, a.Seed(), 120, 40)
	defer receiver.Close()
	tickUntilCursor(t, receiver)
	decoded, err := DecodeCapture(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	staged, err := receiver.StageShared(decoded)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := staged.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	out.stage, out.commit = staged.Timings()
	return out
}

// BenchmarkCaptureAtStormHighWater is the same measurement as a benchmark, for a
// bisect that wants to see the cost move rather than read it once.
func BenchmarkCaptureAtStormHighWater(b *testing.B) {
	a, err := NewHeadless(Config{Seed: stormWorldSeed, Width: 120, Height: 40, ForceDefault: true})
	if err != nil {
		b.Fatalf("headless: %v", err)
	}
	defer a.Close()
	a.Tick(stormWorldWarm)
	a.World().RunSafe(func() {
		a.World().PushEventDomain(event.EventStormSpawnRequest, nil, core.DomainShared)
	})
	a.Settle()
	a.Tick(2)

	var bytes int
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		cap, err := a.CaptureShared()
		if err != nil {
			b.Fatalf("capture: %v", err)
		}
		body, err := EncodeCapture(cap)
		if err != nil {
			b.Fatalf("encode: %v", err)
		}
		bytes = len(body)
	}
	b.ReportMetric(float64(bytes), "bytes/capture")
}

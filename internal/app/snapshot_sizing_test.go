package app

import (
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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

// The correction at the storm high water, which is the measurement Phase 4's
// cadence is actually chosen from.
//
// The original plain-JSON figures answered "what does one world cost": 176 KiB at
// 5 Hz was 859 KiB/s, in a game whose artifact stream is 3–38 KB/s. Deltas brought
// that to 216 KiB/s; the wire envelope now compresses both shapes. This measures the
// current operating point, so the number that decides cadence is the *average*
// compressed correction rather than either historical peak.
//
// Reported rather than asserted, for the same reason as above: a byte threshold
// here would fail on a world that got busier for honest reasons. What is asserted is
// that the delta is smaller than the capture and that it carries something, because
// a measurement of an unchanging world measures nothing.
func TestCorrectionCostAtTheStormHighWater(t *testing.T) {
	peakTick, _ := findStormHighWater(t)
	a := stormWorld(t, peakTick)

	base, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("baseline capture: %v", err)
	}
	baseBody, err := EncodeCorrection(base)
	if err != nil {
		t.Fatalf("baseline encode: %v", err)
	}
	basePlain, err := json.Marshal(correctionEnvelope{Full: &base})
	if err != nil {
		t.Fatalf("baseline plain encode: %v", err)
	}

	// One cadence later: the difference a correction actually carries.
	a.Tick(parameter.SnapshotCorrectionTicks)
	next, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	diffStart := time.Now() // [wall] cost measurement; runs outside the world lock
	delta := DiffCapture(base, next)
	diffDur := time.Since(diffStart)

	deltaBody, err := EncodeCorrectionDelta(delta)
	if err != nil {
		t.Fatalf("delta encode: %v", err)
	}
	deltaPlain, err := json.Marshal(correctionEnvelope{Delta: &delta})
	if err != nil {
		t.Fatalf("delta plain encode: %v", err)
	}

	applyStart := time.Now() // [wall]
	rebuilt, err := ApplyCaptureDelta(base, delta)
	applyDur := time.Since(applyStart)
	if err != nil {
		t.Fatalf("apply delta: %v", err)
	}
	rebuiltBody, err := EncodeCapture(rebuilt)
	if err != nil {
		t.Fatalf("rebuilt encode: %v", err)
	}
	if string(rebuiltBody) != string(mustEncode(t, next)) {
		t.Fatal("the delta did not reproduce the capture it was computed for")
	}
	if len(deltaBody) >= len(baseBody) {
		t.Fatalf("delta is %d bytes against a %d-byte capture; it is buying nothing",
			len(deltaBody), len(baseBody))
	}
	if len(baseBody)*2 >= len(basePlain) || len(deltaBody)*2 >= len(deltaPlain) {
		t.Fatalf("wire compression bought too little: keyframe %d/%d, delta %d/%d",
			len(baseBody), len(basePlain), len(deltaBody), len(deltaPlain))
	}
	if delta.World.DeltaEntries() == 0 {
		t.Fatalf("one cadence at the storm high water moved nothing; this measures a still world")
	}

	// The install, on a receiver that already holds the baseline — which is what a
	// guest is, and is a different measurement from the join the test above makes.
	// The staging world is built by the first install and re-used by the second, and
	// the commit reconciles rather than replaces, so both halves of what Phase 3 left
	// on the doorstep show up here as the difference between the two numbers.
	receiver := mustHeadless(t, a.Seed(), 120, 40)
	defer receiver.Close()
	tickUntilCursor(t, receiver)
	joinStaged, err := receiver.StageShared(base)
	if err != nil {
		t.Fatalf("stage baseline: %v", err)
	}
	if err := joinStaged.Commit(); err != nil {
		t.Fatalf("commit baseline: %v", err)
	}
	joinStage, joinCommit := joinStaged.Timings()

	correctionStaged, err := receiver.StageShared(rebuilt)
	if err != nil {
		t.Fatalf("stage correction: %v", err)
	}
	if err := correctionStaged.Commit(); err != nil {
		t.Fatalf("commit correction: %v", err)
	}
	corrStage, corrCommit := correctionStaged.Timings()
	magnitude := correctionStaged.Difference()

	// The cadence's actual uplink: one keyframe every SnapshotKeyframeCorrections
	// corrections and a delta the rest of the time.
	perSecond := func(hz float64) float64 {
		k := float64(parameter.SnapshotKeyframeCorrections)
		avg := (float64(len(baseBody)) + (k-1)*float64(len(deltaBody))) / k
		return hz * avg / 1024
	}
	t.Logf("storm high water: keyframe %6d wire / %6d JSON (%.1f%%) | "+
		"delta %6d wire / %6d JSON (%.1f%%) | "+
		"%d component cells over %d shared placements | diff %9s apply %9s",
		len(baseBody), len(basePlain), 100*float64(len(baseBody))/float64(len(basePlain)),
		len(deltaBody), len(deltaPlain), 100*float64(len(deltaBody))/float64(len(deltaPlain)),
		delta.World.DeltaEntries(), sharedPlacements(a), diffDur, applyDur)
	t.Logf("install: first (a join) stage %9s commit %9s | second (a correction) stage %9s commit %9s | "+
		"magnitude %d cells over %d entities, %d cells of placement",
		joinStage, joinCommit, corrStage, corrCommit,
		magnitude.Entries, magnitude.Entities, magnitude.CellShift)
	t.Logf("cadence uplink with a keyframe every %d corrections: %.1f KiB/s at 5 Hz, %.1f KiB/s at 2 Hz "+
		"(full snapshots would be %.1f and %.1f)",
		parameter.SnapshotKeyframeCorrections, perSecond(5), perSecond(2),
		5*float64(len(baseBody))/1024, 2*float64(len(baseBody))/1024)
}

// mustEncode is EncodeCapture with the error folded into the test.
func mustEncode(t *testing.T, cap SharedCapture) []byte {
	t.Helper()
	body, err := EncodeCapture(cap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return body
}

// TestSelectiveCorrectionCostAtTheStormHighWater is the Phase 6 measurement, and it
// is the one that decides whether the index was worth building.
//
// Four shapes are reported for one world at its fullest, all of them real bodies
// produced by the paths a session uses:
//
//   - the plain schema, which is what the wire would carry with no codec at all;
//   - the compressed keyframe and the compressed exact delta, which are Phase 5's
//     two shapes and the operating point this phase started from;
//   - the correction manifest, which is what a healthy correction now costs in
//     full — root, section summaries and header, and no state;
//   - the shard set an injected disagreement provokes, which is what a repair
//     costs when there is something to repair. The disagreement used here is the
//     widest one this world produces — a receiver whose state is a whole cadence
//     stale, which is what a guest that predicted *nothing* would hold — so it is
//     an upper bound rather than a typical repair. A repair wider than the
//     keyframe is not sent at all: the host answers with the whole world instead,
//     so the selective path can never cost more than the stream it replaced.
//
// The projections are the numbers a cadence is chosen from: a keyframe every
// SnapshotKeyframeCorrections corrections and the rest of the interval spent on
// whichever non-keyframe shape the session is using.
//
// Everything here is reported. The one assertion is the reason the phase exists: a
// converged correction has to be materially cheaper than the delta it replaces, on
// this fixed fixture, or the round trip buys nothing.
func TestSelectiveCorrectionCostAtTheStormHighWater(t *testing.T) {
	peakTick, _ := findStormHighWater(t)
	a := stormWorld(t, peakTick)

	base, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("baseline capture: %v", err)
	}
	basePlain, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("plain encode: %v", err)
	}
	keyframeBody, err := EncodeCorrection(base)
	if err != nil {
		t.Fatalf("keyframe encode: %v", err)
	}

	a.Tick(parameter.SnapshotCorrectionTicks)
	next, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	deltaBody, err := EncodeCorrectionDelta(DiffCapture(base, next))
	if err != nil {
		t.Fatalf("delta encode: %v", err)
	}

	// The index, built and hashed outside the world lock, as the publisher does.
	indexStart := time.Now() // [wall] cost measurement; runs outside the world lock
	index, err := buildManifest(next, 1)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	indexDur := time.Since(indexStart)
	manifestBody, err := EncodeManifest(index.Summary())
	if err != nil {
		t.Fatalf("manifest encode: %v", err)
	}

	// A converged receiver: one root comparison, an empty request, no state.
	mirror, err := buildManifest(cloneCapture(t, next), 1)
	if err != nil {
		t.Fatalf("mirror manifest: %v", err)
	}
	compareStart := time.Now() // [wall]
	converged, sections, hashedPages := compareRequest(mirror, index.Summary())
	compareDur := time.Since(compareStart)
	if !converged.Converged() {
		t.Fatal("two indexes over one capture did not agree")
	}
	convergedBody, err := EncodeCorrectionRequest(converged)
	if err != nil {
		t.Fatalf("request encode: %v", err)
	}

	// A diverged receiver: the storm's whole swarm has moved a cadence under it,
	// which is the widest ordinary disagreement this world produces.
	stale, err := buildManifest(cloneCapture(t, base), 1)
	if err != nil {
		t.Fatalf("stale manifest: %v", err)
	}
	req, _, staleHashed := compareRequest(stale, index.Summary())
	if req.Converged() {
		t.Fatal("a cadence of storm motion left the two agreeing")
	}
	requestBody, err := EncodeCorrectionRequest(req)
	if err != nil {
		t.Fatalf("request encode: %v", err)
	}
	shardStart := time.Now() // [wall]
	set, pages, err := buildShardSet(index, req)
	if err != nil {
		t.Fatalf("shard set: %v", err)
	}
	shardDur := time.Since(shardStart)
	shardBody, err := EncodeShardSet(set)
	if err != nil {
		t.Fatalf("shard encode: %v", err)
	}

	// The repair reproduces the authority exactly, which is what makes the byte
	// figures above a comparison of equals rather than of a shortcut.
	repaired := cloneCapture(t, base)
	repairedIndex, err := buildManifest(repaired, 1)
	if err != nil {
		t.Fatalf("repaired manifest: %v", err)
	}
	if err := validateShardSet(set, next.Header.Tick, 1, next.Header); err != nil {
		t.Fatalf("validate: %v", err)
	}
	rep, err := applyShardSet(&repaired, repairedIndex, set)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if repairedIndex.Root() != index.Root() {
		t.Fatal("the repaired capture does not reproduce the authority's root")
	}

	perSecond := func(hz float64, nonKeyframe int) float64 {
		k := float64(parameter.SnapshotKeyframeCorrections)
		avg := (float64(len(keyframeBody)) + (k-1)*float64(nonKeyframe)) / k
		return hz * avg / 1024
	}
	convergedWire := len(manifestBody) + len(convergedBody)
	repairWire := len(manifestBody) + len(requestBody) + len(shardBody)

	t.Logf("storm high water: %d shared placements | plain schema %d | "+
		"compressed keyframe %d | compressed delta %d",
		sharedPlacements(a), len(basePlain), len(keyframeBody), len(deltaBody))
	t.Logf("index: %d sections, %d bytes | converged exchange %d bytes "+
		"(%d section hashes compared, %d page hashes) | build %9s compare %9s",
		len(index.Summary().Sections), len(manifestBody), convergedWire,
		sections, hashedPages, indexDur, compareDur)
	t.Logf("repair: %d pages over %d entities and %d cells | request %d bytes "+
		"(%d page hashes) | shards %d bytes | whole exchange %d bytes | build %9s | "+
		"cheaper than the keyframe: %v",
		rep.Pages, rep.Entities, rep.Rows, len(requestBody), staleHashed,
		len(shardBody), repairWire, shardDur, len(shardBody) < len(keyframeBody))
	t.Logf("projected uplink with a keyframe every %d corrections: "+
		"converged %.1f KiB/s at 5 Hz and %.1f at 2 Hz | "+
		"a receiver that predicted nothing %.1f and %.1f | Phase 5 deltas %.1f and %.1f",
		parameter.SnapshotKeyframeCorrections,
		perSecond(5, convergedWire), perSecond(2, convergedWire),
		perSecond(5, repairWire), perSecond(2, repairWire),
		perSecond(5, len(deltaBody)), perSecond(2, len(deltaBody)))
	t.Logf("pages: %d shards of %d requested, %d bytes each on average",
		pages, staleHashed, len(shardBody)/max(1, pages))

	// The correctness claim: proving convergence costs materially less than
	// carrying it. Half is conservative — the measured figure on this fixture is
	// closer to a fifth — and it is asserted rather than reported because a change
	// that lost it would have removed the phase's reason to exist.
	if convergedWire*2 >= len(deltaBody) {
		t.Fatalf("a converged correction costs %d bytes against a %d-byte delta; "+
			"the index is not buying its round trip", convergedWire, len(deltaBody))
	}
}

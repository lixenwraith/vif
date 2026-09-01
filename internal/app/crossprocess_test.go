package app

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// The cross-process gate. Phase 2's 500-tick gate ran two worlds in one process,
// and said so: the capture never left the address space, both halves read one
// process's clock, and any instant that happened to be a process-wide constant
// agreed for free. This is the same gate with the two halves in different
// processes, which is the form the plan owed and the form a join actually is.
//
// What the separation buys, in the order it matters:
//
//   - The capture is bytes on a disk. Anything the in-process gate got by sharing
//     a pointer — a slice the sender still owns, a map iterated in one process's
//     order — is gone.
//   - The two processes start at different wall times and pace their ticks
//     differently. A simulation that read the pacing clock would diverge; D-21 says
//     it cannot, and this is where that is demonstrated rather than asserted.
//   - The receiver installs into a world it built itself, from the same seed but at
//     a different tick, and then runs 500 ticks against the sender's continuation.
//     Equal state at the install tick is the easy half; equal futures is the gate.
//
// The child is a second copy of this test binary. That is the whole point: a
// helper that imported the package and ran in-process would be the gate this
// replaces.
const (
	crossProcessRoleEnv = "VIF_CROSS_ROLE"
	crossProcessDirEnv  = "VIF_CROSS_DIR"
	crossProcessSeedEnv = "VIF_CROSS_SEED"

	// crossProcessSeed is one seed, because each run costs two processes. The
	// in-process gate covers the seed sweep.
	crossProcessSeed = 0x5A4E

	// crossProcessWarmTicks puts species, a gold sequence and the escalation FSM in
	// the world before anything is captured, so the continuation draws the shared
	// streams rather than idling.
	crossProcessWarmTicks = 400

	// crossProcessContinueTicks is the gate itself.
	crossProcessContinueTicks = 500

	captureFile   = "capture.json"
	originSurface = "origin.surface"
	joinerSurface = "joiner.surface"
	epochSurface  = "epoch.surface"
)

// TestCaptureContinuesInAnotherProcess is the gate.
func TestCaptureContinuesInAnotherProcess(t *testing.T) {
	if os.Getenv(crossProcessRoleEnv) != "" {
		t.Skip("child process")
	}
	dir := t.TempDir()

	// The origin runs first and writes both the capture and its own continuation.
	runCrossProcessChild(t, "origin", dir)
	// The receiver starts later, by a wall interval the origin never saw, and paces
	// its own ticks in bursts. Neither can reach the simulation, which is the claim.
	runCrossProcessChild(t, "joiner", dir)

	origin := readSurface(t, filepath.Join(dir, originSurface))
	joiner := readSurface(t, filepath.Join(dir, joinerSurface))
	if len(origin) == 0 {
		t.Fatal("the origin process wrote an empty surface")
	}
	if idx, lx, ly, differs := FirstDiff(origin, joiner); differs {
		t.Fatalf("the two processes disagree %d ticks after the install, at line %d\n"+
			"  origin: %s\n  joiner: %s\n%s",
			crossProcessContinueTicks, idx, lx, ly,
			strings.Join(Diff(origin, joiner, 8), "\n"))
	}

	info, err := os.Stat(filepath.Join(dir, captureFile))
	if err != nil {
		t.Fatalf("capture file: %v", err)
	}
	t.Logf("capture %d bytes carried %d ticks of identical evolution across two processes",
		info.Size(), crossProcessContinueTicks)
}

// TestSimulationEpochIsSessionIdentity is the negative control, and it documents
// the one part of §4.2's hazard that D-21 did not remove so much as relocate.
//
// A shared component carries absolute instants — a genotype's spawn time, a
// quasar's last speed step, a shield's last drain — and a capture carries them as
// they are, not as durations. That is sound, but only for one reason: SimEpoch is a
// build constant, so tick N names the same instant in every process of the same
// build. It is not a per-process origin any more, so there is nothing left for a
// transfer to get wrong.
//
// The way to show that reason is load-bearing is to break it. A receiver whose
// SimEpoch differs installs the same bytes and diverges, because every
// now.Sub(stored) it computes is wrong by the offset. So SimEpoch belongs to
// session identity as much as the seed does, and a build that changes it cannot
// receive a capture from one that did not.
func TestSimulationEpochIsSessionIdentity(t *testing.T) {
	if os.Getenv(crossProcessRoleEnv) != "" {
		t.Skip("child process")
	}
	dir := t.TempDir()
	runCrossProcessChild(t, "origin", dir)
	runCrossProcessChild(t, "epoch", dir)

	origin := readSurface(t, filepath.Join(dir, originSurface))
	shifted := readSurface(t, filepath.Join(dir, epochSurface))
	if _, _, _, differs := FirstDiff(origin, shifted); !differs {
		t.Fatal("a receiver on a different simulation epoch reproduced the sender's " +
			"continuation exactly; the absolute instants a capture carries are then " +
			"not epoch-relative and this control proves nothing")
	}
}

// TestCrossProcessChild is the child. It does nothing unless the parent asks.
func TestCrossProcessChild(t *testing.T) {
	role := os.Getenv(crossProcessRoleEnv)
	if role == "" {
		t.Skip("not a child process")
	}
	dir := os.Getenv(crossProcessDirEnv)
	seed, err := strconv.ParseUint(os.Getenv(crossProcessSeedEnv), 0, 64)
	if err != nil {
		t.Fatalf("child seed: %v", err)
	}

	switch role {
	case "origin":
		crossProcessOrigin(t, dir, seed)
	case "joiner":
		crossProcessReceiver(t, dir, seed, joinerSurface)
	case "epoch":
		// The offset is deliberately not a whole number of ticks: a receiver that
		// happened to be shifted by an exact multiple would land every deadline on
		// a tick again and hide what this control is for.
		engine.SimEpoch = engine.SimEpoch.Add(37*time.Hour + 17*time.Millisecond)
		engine.ManualEpoch = engine.SimEpoch
		crossProcessReceiver(t, dir, seed, epochSurface)
	default:
		t.Fatalf("unknown child role %q", role)
	}
}

// crossProcessOrigin warms a world, writes the capture, and writes what its own
// continuation of that world comes to.
func crossProcessOrigin(t *testing.T, dir string, seed uint64) {
	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(crossProcessWarmTicks)

	// A capture carries no player state by design (D-2, D-6), so a crossing produced
	// by one instance's own drains would move the shared FSM there and nowhere else.
	// Both sides stop the same producers; what is being compared is the shared
	// simulation's own evolution.
	quiescePlayerDomain(t, a)
	spawnSharedSpecies(t, a)
	spawnCrossProcessQuasar(t, a)

	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	body, err := EncodeCapture(cap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, captureFile), body, 0o600); err != nil {
		t.Fatalf("write capture: %v", err)
	}

	var swarms, quasars int
	a.World().RunSafe(func() {
		swarms = a.World().Components.Swarm.CountEntities()
		quasars = a.World().Components.Quasar.CountEntities()
	})
	if swarms == 0 || quasars == 0 {
		t.Fatalf("captured world holds %d swarms and %d quasars; the continuation would "+
			"draw no shared streams and the epoch control would have nothing to bite on",
			swarms, quasars)
	}
	t.Logf("captured at tick %d with %d swarms and %d quasars, %d bytes",
		cap.Header.Tick, swarms, quasars, len(body))

	a.Tick(crossProcessContinueTicks)
	writeSurface(t, filepath.Join(dir, originSurface), a.SnapshotShared())
}

// crossProcessReceiver builds its own world, drives it to a different tick, then
// installs the capture and continues.
func crossProcessReceiver(t *testing.T, dir string, seed uint64, out string) {
	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	// A different tick, so an install that wrote nothing could not pass, and a
	// different wall pacing, so a simulation that read the pacing clock could not.
	a.Tick(90)
	quiescePlayerDomain(t, a)

	body, err := os.ReadFile(filepath.Join(dir, captureFile))
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	cap, err := DecodeCapture(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The join's own path: staged into a second world, then swapped in.
	staged, err := a.StageShared(cap)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := staged.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Bursts with real pauses between them. The origin ran its five hundred ticks
	// in one go, in another process, at another wall instant; if any of that could
	// reach the simulation the two surfaces would not match.
	const burst = 50
	for range crossProcessContinueTicks / burst {
		a.Tick(burst)
		time.Sleep(2 * time.Millisecond) // [wall] deliberate pacing noise
	}
	writeSurface(t, filepath.Join(dir, out), a.SnapshotShared())
}

// spawnCrossProcessQuasar puts the one species whose future depends on an absolute
// instant into the captured world.
//
// QuasarSystem steps SpeedMultiplier when now.Sub(LastSpeedIncreaseAt) passes an
// interval, and that instant travels inside the shared component as it stands. It is
// the entity behind the 2026-08-31 kinetics divergence and it is the entity the
// epoch control needs: without one in the world, a receiver on a different
// simulation epoch has nothing to get wrong.
func spawnCrossProcessQuasar(t *testing.T, a *App) {
	t.Helper()
	a.World().RunSafe(func() {
		a.World().PushEventDomain(event.EventQuasarSpawnRequest,
			&event.QuasarSpawnRequestPayload{X: 60, Y: 20}, core.DomainShared)
	})
	a.Settle()
	a.Tick(1)
	var n int
	a.World().RunSafe(func() { n = a.World().Components.Quasar.CountEntities() })
	if n == 0 {
		t.Fatal("no quasar was created; the epoch control has nothing to bite on")
	}
}

// runCrossProcessChild runs this test binary again in the named role.
func runCrossProcessChild(t *testing.T, role, dir string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrossProcessChild$", "-test.timeout=5m")
	cmd.Env = append(os.Environ(),
		crossProcessRoleEnv+"="+role,
		crossProcessDirEnv+"="+dir,
		crossProcessSeedEnv+"="+fmt.Sprintf("%#x", uint64(crossProcessSeed)),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child %q failed: %v\n%s", role, err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("child %q did not pass:\n%s", role, out)
	}
}

func writeSurface(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, line := range lines {
		if _, err := w.WriteString(line + "\n"); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush %s: %v", path, err)
	}
}

func readSurface(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

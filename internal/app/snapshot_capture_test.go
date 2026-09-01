package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TestCaptureReconstructsTheSharedWorld is Phase 2's construction proof at its
// simplest: a capture taken from one run, encoded, decoded and installed into a
// second run that had reached a different state, must leave the second run's
// shared surface equal to the first's.
//
// Equality is asserted on SnapshotShared, not on the capture bytes. Two worlds
// holding the same state through different insertion histories are the same
// world, and it is the state a session has to agree on.
func TestCaptureReconstructsTheSharedWorld(t *testing.T) {
	origin := mustHeadless(t, 0x5A4E, 120, 40)
	defer origin.Close()
	receiver := mustHeadless(t, 0x5A4E, 120, 40)
	defer receiver.Close()

	tickUntilCursor(t, origin)
	tickUntilCursor(t, receiver)

	// Drive the two apart, so an install that did nothing would be visible.
	origin.Tick(180)
	receiver.Tick(40)
	if !sharedSurfacesDiffer(origin, receiver) {
		t.Fatal("the two runs already agree; the install would prove nothing")
	}

	cap, err := origin.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	encoded, err := EncodeCapture(cap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeCapture(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := receiver.InstallShared(decoded); err != nil {
		t.Fatalf("install: %v", err)
	}

	if idx, lx, ly, differs := FirstDiff(origin.SnapshotShared(), receiver.SnapshotShared()); differs {
		t.Fatalf("installed world differs at line %d\n  origin:   %s\n  receiver: %s", idx, lx, ly)
	}
}

// TestCaptureCarriesEveryDeclaredSystem asserts the capture actually contains
// what the manifest declares. A capture that quietly omitted a carrier would
// install a world whose learned routing or maze generator is this instance's
// rather than the sender's, and nothing in the shared digest hashes either.
func TestCaptureCarriesEveryDeclaredSystem(t *testing.T) {
	a := mustHeadless(t, 0x5A4E, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(20)

	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	present := make(map[string]bool, len(cap.Systems))
	for _, rec := range cap.Systems {
		present[rec.System] = true
		if len(rec.Data) == 0 {
			t.Errorf("%s: capture carries an empty record", rec.System)
		}
	}

	declared := 0
	for name, profile := range manifest.SnapshotDeclarations() {
		if profile != engine.SnapshotState {
			continue
		}
		declared++
		if !present[name] {
			t.Errorf("%s declares snapshot state but is missing from the capture", name)
		}
	}
	if declared == 0 {
		t.Fatal("no system declares snapshot state; the check passed vacuously")
	}

	// Every stream the run issued is in the inventory, which is the hidden-state
	// survey's "~24 per-system RNG streams" answered by construction.
	if len(cap.Streams) < 20 {
		t.Errorf("capture carries %d RNG streams; the run issues far more", len(cap.Streams))
	}
}

// TestCaptureCarriesNoPlayerState pins the boundary. A capture describes the
// shared world; a participant's own simulation does not exist on any other
// instance (D-2) and its effects are per-instance (D-6), so a capture that
// carried one would install another participant's private world.
func TestCaptureCarriesNoPlayerState(t *testing.T) {
	a := mustHeadless(t, 0x5A4E, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(120)

	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	var player int
	for _, e := range cap.World.Positions {
		if e.Entity.Domain() != core.DomainShared {
			player++
		}
	}
	if player > 0 {
		t.Fatalf("%d player-domain placements reached the capture", player)
	}

	// The run must actually have player entities, or the assertion is vacuous.
	var live int
	a.World().RunSafe(func() {
		for _, e := range a.World().Positions.Entities() {
			if e.Domain() != core.DomainShared {
				live++
			}
		}
	})
	if live == 0 {
		t.Fatal("the run holds no player-domain entities; the exclusion proves nothing")
	}
}

// TestVerifyCaptureRejectsATamperedBody keeps a corrupted or truncated transfer
// from being installed as if it were intact. Integrity and identity answer
// different questions and a capture has to pass both before anything is written.
func TestVerifyCaptureRejectsATamperedBody(t *testing.T) {
	a := mustHeadless(t, 0x5A4E, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(30)

	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := a.VerifyCapture(cap); err != nil {
		t.Fatalf("a fresh capture failed its own verification: %v", err)
	}

	tampered := cap
	tampered.World.NextEntity += 7
	if err := a.VerifyCapture(tampered); err == nil {
		t.Fatal("a modified body passed the integrity check")
	}

	wrongSeed := cap
	wrongSeed.Header.Seed ^= 1
	// The seed is inside the integrity hash, so restore it there before asserting
	// the identity check is what rejects this one.
	wrongSeed.Header.Integrity, _ = captureIntegrity(wrongSeed)
	err = a.VerifyCapture(wrongSeed)
	if err == nil {
		t.Fatal("a capture from a different seed was accepted")
	}
	if !strings.Contains(err.Error(), "seed") {
		t.Fatalf("expected the seed mismatch to be named, got: %v", err)
	}
}

// sharedSurfacesDiffer reports whether two runs disagree on the compared shared
// surface.
func sharedSurfacesDiffer(a, b *App) bool {
	_, _, _, differs := FirstDiff(a.SnapshotShared(), b.SnapshotShared())
	return differs
}

// TestInstalledWorldStaysIdenticalForFiveHundredTicks is Phase 2's construction
// proof: not that a capture reproduces a world, but that it reproduces a world
// whose *future* is the same.
//
// Equal state at the install tick is necessary and nowhere near sufficient.
// Everything the hidden-state survey listed — RNG positions, the maze generator,
// EXP3 route weights, genetic populations, the D-17 recompute phase, the FSM's
// time in state, the telemetry throttles — is invisible at the install tick and
// decides what happens after it. A capture missing any of them passes an equality
// check and then drifts, which is exactly how each of them was found: the loop
// below named one carrier at a time until nothing moved for 500 ticks.
//
// The player domain is stopped in both runs first, and that is the honest limit
// of what this phase can assert. A capture carries no player state by design
// (D-2, D-6), so two instances holding one shared world still hold different
// drains, and a drain defeated on one advances the shared escalation FSM there
// and nowhere else. That is not a capture defect — it is a crossing, and
// delivering crossings is Phase 4's subject. What is provable here is the shared
// simulation's own evolution, which is what every piece of hidden state feeds.
// The plan's record-stream-driven, cross-process form of this gate belongs with
// the wire that carries a capture, and is named in the phase's remaining work.
func TestInstalledWorldStaysIdenticalForFiveHundredTicks(t *testing.T) {
	for _, seed := range []uint64{0x5A4E, 0xC0FFEE, 0x1234ABCD} {
		t.Run(seedName(seed), func(t *testing.T) {
			origin := mustHeadless(t, seed, 120, 40)
			defer origin.Close()
			receiver := mustHeadless(t, seed, 120, 40)
			defer receiver.Close()

			tickUntilCursor(t, origin)
			tickUntilCursor(t, receiver)

			// Far enough in that species, gold and the escalation FSM are all live,
			// and the two runs are driven apart first so an install that did
			// nothing cannot pass.
			origin.Tick(400)
			receiver.Tick(90)
			if !sharedSurfacesDiffer(origin, receiver) {
				t.Fatal("the two runs already agree; the install would prove nothing")
			}
			quiescePlayerDomain(t, origin)
			quiescePlayerDomain(t, receiver)

			// Shared species are what draw the shared streams and exercise the
			// navigation phase, the genetic populations and the route learning.
			// Without them the 500 ticks are quiet and the gate proves far less
			// than it appears to: a capture that dropped every stream position
			// still passed until this was added.
			spawnSharedSpecies(t, origin)
			// Advance to a status-cadence boundary before capturing. Gauges like
			// spatial.indexed_shared are published every StatSnapshotTicks, which
			// is a function of the tick counter, so two instances agree on them at
			// a boundary and need not between two. Capturing on one keeps the
			// comparison about the capture rather than about publish phase.
			tickToStatBoundary(origin)

			cap, err := origin.CaptureShared()
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			encoded, err := EncodeCapture(cap)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			decoded, err := DecodeCapture(encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if err := receiver.InstallShared(decoded); err != nil {
				t.Fatalf("install: %v", err)
			}
			if idx, lx, ly, differs := FirstDiff(origin.SnapshotShared(), receiver.SnapshotShared()); differs {
				t.Fatalf("install tick differs at line %d\n  origin:   %s\n  receiver: %s", idx, lx, ly)
			}

			// The part that matters.
			for step := range 10 {
				origin.Tick(50)
				receiver.Tick(50)
				if idx, lx, ly, differs := FirstDiff(origin.SnapshotShared(), receiver.SnapshotShared()); differs {
					t.Fatalf("diverged %d ticks after install, at line %d\n  origin:   %s\n  receiver: %s\n%s",
						(step+1)*50, idx, lx, ly,
						strings.Join(diffSharedWorld(origin, receiver, 8), "\n"))
				}
			}
			t.Logf("capture %d bytes; %d streams, %d system records, %d regions",
				len(encoded), len(cap.Streams), len(cap.Systems), len(cap.FSM.Regions))
		})
	}
}

func seedName(seed uint64) string {
	return "seed_" + strconv.FormatUint(seed, 16)
}

// quiescePlayerDomain stops the player-domain systems whose output crosses into
// the shared world (D-3), so two instances holding one shared world evolve it the
// same way. Without this the comparison measures crossing delivery, which no
// capture provides and Phase 4 does.
//
// Drains are the load-bearing one: they are player-domain entities whose defeat
// crosses as EventDrainDefeated and drives the shared escalation FSM, so two
// runs with different drain populations reach MainSpawnGold on different ticks.
func quiescePlayerDomain(t *testing.T, a *App) {
	t.Helper()
	for _, name := range []string{"drain", "fuse", "typing", "nugget", "weapon", "glyph"} {
		a.World().RunSafe(func() {
			a.World().PushEventDomain(event.EventMetaSystemCommandRequest,
				&event.MetaSystemCommandPayload{SystemName: name, Enabled: false},
				core.DomainShared)
		})
	}
	a.Settle()
}

// spawnSharedSpecies puts moving shared entities in the world, so the ticks after
// an install actually draw from the shared streams rather than idling. It is
// applied to the origin only: the capture then carries the species, and an
// install that reconstructs them is what the comparison is measuring.
func spawnSharedSpecies(t *testing.T, a *App) {
	t.Helper()
	a.World().RunSafe(func() {
		w := a.World()
		for i := range 3 {
			w.PushEventDomain(event.EventSwarmSpawnRequest,
				&event.SwarmSpawnRequestPayload{X: 20 + i*12, Y: 8 + i*6}, core.DomainShared)
		}
	})
	a.Settle()
}

// tickToStatBoundary advances until the next status-snapshot boundary, so the
// cadenced gauges in the compared surface have just been published.
func tickToStatBoundary(a *App) {
	for range parameter.StatSnapshotTicks + 1 {
		var ticks uint64
		a.World().RunSafe(func() { ticks = a.World().Resources.Game.State.GetGameTicks() })
		if ticks%uint64(parameter.StatSnapshotTicks) == 0 && ticks > 0 {
			return
		}
		a.Tick(1)
	}
}

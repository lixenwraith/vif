package app

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// TestCaptureReconstructsTheSharedWorld is the construction proof at its simplest: a
// capture encoded, decoded and installed into a run that had reached a different
// state leaves that run's shared surface equal to the sender's. Equality is asserted
// on SnapshotShared rather than on bytes — two worlds holding the same state through
// different insertion histories are the same world.
func TestCaptureReconstructsTheSharedWorld(t *testing.T) {
	t.Parallel()
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
	encoded, err := snapshot.EncodeCapture(cap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := snapshot.DecodeCapture(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := receiver.InstallShared(decoded); err != nil {
		t.Fatalf("install: %v", err)
	}

	if idx, lx, ly, differs := snapshot.FirstDiff(origin.SnapshotShared(), receiver.SnapshotShared()); differs {
		t.Fatalf("installed world differs at line %d\n  origin:   %s\n  receiver: %s", idx, lx, ly)
	}
}

// TestCorrectionReconcilesPersistentLocalFSMEffects covers the multiplayer stall
// that a shared-world comparison could not see. The authoritative capture can
// jump over the transition that released a player-local drain/grayout hold; the
// shared FSM then looks correct while that participant can no longer spawn drains
// and therefore cannot advance the encounter again.
func TestCorrectionReconcilesPersistentLocalFSMEffects(t *testing.T) {
	t.Parallel()
	const seed = 0xC011EC7
	source := mustHeadless(t, seed, 120, 40)
	defer source.Close()
	receiver := mustHeadless(t, seed, 120, 40)
	defer receiver.Close()
	tickUntilCursor(t, source)
	tickUntilCursor(t, receiver)

	var cursor core.Entity
	source.World().RunSafe(func() {
		cursor = source.World().Resources.Player.Slot(0)
		source.World().Resources.Status.Ints.Get("kills.drain").Store(9)
	})
	source.Context().PushCrossing(event.EventDrainDefeated,
		&event.DrainDefeatedPayload{Entity: cursor})
	source.Settle()
	source.Tick(1) // publishes DrainSystem's local hold telemetry
	if quasarState(source) == "-" || !grayedOut(source) || !drainsPaused(source) {
		t.Fatalf("source did not enter the quasar hold: state=%s grayout=%v paused=%v",
			quasarState(source), grayedOut(source), drainsPaused(source))
	}

	active, err := source.CaptureShared()
	if err != nil {
		t.Fatalf("capture active quasar: %v", err)
	}
	staged, err := receiver.StageShared(active)
	if err != nil {
		t.Fatalf("stage active quasar: %v", err)
	}
	if grayedOut(receiver) || drainsPaused(receiver) {
		t.Fatal("staging changed the live participant before commit")
	}
	if err := staged.Commit(); err != nil {
		t.Fatalf("commit active quasar: %v", err)
	}
	receiver.Settle()
	receiver.Tick(1)
	if quasarState(receiver) == "-" || !grayedOut(receiver) || !drainsPaused(receiver) {
		t.Fatalf("active correction did not establish local holds: state=%s grayout=%v paused=%v",
			quasarState(receiver), grayedOut(receiver), drainsPaused(receiver))
	}

	// Retire the region authoritatively. The receiver never observes this state
	// transition; its next capture simply has no quasar region.
	for range 20 {
		if quasarState(source) != "QuasarFuse" {
			break
		}
		source.Tick(1)
	}
	if state := quasarState(source); state == "-" || state == "QuasarFuse" {
		t.Fatalf("source quasar never reached its killable cycle: %s", state)
	}
	source.Context().PushEventDomain(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
		KillerEntity: cursor,
		Species:      component.SpeciesQuasar,
	}, core.DomainShared)
	source.Settle()
	source.Tick(1)
	if quasarState(source) != "-" || grayedOut(source) || drainsPaused(source) {
		t.Fatalf("source did not leave the quasar hold: state=%s grayout=%v paused=%v",
			quasarState(source), grayedOut(source), drainsPaused(source))
	}

	recovered, err := source.CaptureShared()
	if err != nil {
		t.Fatalf("capture retired quasar: %v", err)
	}
	staged, err = receiver.StageShared(recovered)
	if err != nil {
		t.Fatalf("stage retired quasar: %v", err)
	}
	if !grayedOut(receiver) || !drainsPaused(receiver) {
		t.Fatal("staging released the live participant's holds before commit")
	}
	if err := staged.Commit(); err != nil {
		t.Fatalf("commit retired quasar: %v", err)
	}
	receiver.Settle()
	receiver.Tick(1)
	if quasarState(receiver) != "-" || grayedOut(receiver) || drainsPaused(receiver) {
		t.Fatalf("retirement correction stranded local holds: state=%s grayout=%v paused=%v",
			quasarState(receiver), grayedOut(receiver), drainsPaused(receiver))
	}
}

// TestCaptureCarriesEveryDeclaredSystem asserts the capture actually contains
// what the manifest declares. A capture that quietly omitted a carrier would
// install a world whose learned routing or maze generator is this instance's
// rather than the sender's, and nothing in the shared digest hashes either.
func TestCaptureCarriesEveryDeclaredSystem(t *testing.T) {
	t.Parallel()
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

// TestACaptureExcludesThePlayerDomainByStoreAndByReference pins the boundary at both
// levels it can be crossed. Every store loop skips player-domain entities by
// construction, which is the first assertion; the second is on the *reference*, since
// a shared component field is free to hold whatever entity its writer put there. It
// is made by reflection, so a new field on any shared component is covered.
func TestACaptureExcludesThePlayerDomainByStoreAndByReference(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x5A4E, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(120)

	// The cursor is armed because the reference check needs a shared entity that
	// *has* something player-domain to point at: an unarmed run holds no orbs and the
	// walk would pass over an empty array.
	var cursor core.Entity
	a.World().RunSafe(func() { cursor = a.World().Resources.Player.Slot(0) })
	for _, wt := range []component.WeaponType{component.WeaponRod, component.WeaponLauncher, component.WeaponDisruptor} {
		a.Context().PushLocal(event.EventWeaponAddRequest,
			&event.WeaponAddRequestPayload{Entity: cursor, Weapon: wt})
	}
	a.Settle()
	a.Tick(2)
	if got := orbsPerWeapon(a, cursor); got != [component.WeaponCount]int{1, 1, 1} {
		t.Fatalf("orbs = %v; the reference walk would have no player entity to find", got)
	}

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
	if found := namedPlayerEntities(reflect.ValueOf(cap.World), "world"); len(found) > 0 {
		sort.Strings(found)
		t.Fatalf("a capture names player-domain entities:\n  %s", strings.Join(found, "\n  "))
	}

	// Non-vacuous: the run must actually hold player-domain entities.
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

// namedPlayerEntities walks a value and returns the path of every core.Entity in it
// that names a player-domain entity. Zero is not one: it is the absence of a
// reference, which several shared components use as a sentinel.
func namedPlayerEntities(v reflect.Value, path string) []string {
	if v.Type() == reflect.TypeOf(core.Entity(0)) {
		if e := core.Entity(v.Uint()); e != 0 && e.Domain() != core.DomainShared {
			return []string{path + " = " + strconv.FormatUint(uint64(e), 10) +
				" (domain " + strconv.Itoa(int(e.Domain())) + ")"}
		}
		return nil
	}
	var out []string
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			out = append(out, namedPlayerEntities(v.Field(i), path+"."+v.Type().Field(i).Name)...)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			out = append(out, namedPlayerEntities(v.Index(i), path+"["+strconv.Itoa(i)+"]")...)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			out = append(out, namedPlayerEntities(k, path+"<key>")...)
			out = append(out, namedPlayerEntities(v.MapIndex(k), path+"["+k.String()+"]")...)
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			out = append(out, namedPlayerEntities(v.Elem(), path)...)
		}
	}
	return out
}

// TestVerifyCaptureRejectsATamperedBody keeps a corrupted or truncated transfer
// from being installed as if it were intact. Integrity and identity answer
// different questions and a capture has to pass both before anything is written.
func TestVerifyCaptureRejectsATamperedBody(t *testing.T) {
	t.Parallel()
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
	wrongSeed.Header.Integrity, _ = snapshot.Integrity(wrongSeed)
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
	_, _, _, differs := snapshot.FirstDiff(a.SnapshotShared(), b.SnapshotShared())
	return differs
}

// TestInstalledWorldStaysIdenticalForFiveHundredTicks is the continuation proof: not
// that a capture reproduces a world, but that it reproduces a world whose future is
// the same. Everything hidden at the install tick — RNG positions, the maze generator,
// route weights, genetic populations, the recompute phase — decides what follows. The
// player domain is stopped first: crossings are the correction protocol's subject.
func TestInstalledWorldStaysIdenticalForFiveHundredTicks(t *testing.T) {
	t.Parallel()
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

			// Shared species draw the shared streams and exercise the navigation
			// phase, the genetic populations and the route learning; without them the
			// 500 ticks are quiet. The capture is taken on a status-cadence boundary,
			// where two instances agree on the gauges. The species are spawned after
			// that advance — the escalation FSM sweeps during it — and asserted alive.
			tickToStatBoundary(origin)
			spawnSharedSpecies(t, origin)
			origin.Tick(1)
			var swarms int
			origin.World().RunSafe(func() { swarms = origin.World().Components.Swarm.CountEntities() })
			if swarms == 0 {
				t.Fatal("the captured world holds no shared species; the 500 ticks would be quiet")
			}

			cap, err := origin.CaptureShared()
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			encoded, err := snapshot.EncodeCapture(cap)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			decoded, err := snapshot.DecodeCapture(encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if err := receiver.InstallShared(decoded); err != nil {
				t.Fatalf("install: %v", err)
			}
			if idx, lx, ly, differs := snapshot.FirstDiff(origin.SnapshotShared(), receiver.SnapshotShared()); differs {
				t.Fatalf("install tick differs at line %d\n  origin:   %s\n  receiver: %s", idx, lx, ly)
			}

			// The part that matters.
			for step := range 10 {
				origin.Tick(50)
				receiver.Tick(50)
				if idx, lx, ly, differs := snapshot.FirstDiff(origin.SnapshotShared(), receiver.SnapshotShared()); differs {
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

// quiescePlayerDomain stops the player-domain systems whose output crosses into the
// shared world (D-3), so two instances evolve it the same way; without it the
// comparison measures crossing delivery instead. Drains are load-bearing: their
// defeat crosses as EventDrainDefeated and drives the shared escalation FSM, so two
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

// A refusal has to arrive before the world is written, and the staging pass cannot
// always make it. StageShared asks whether this build can load the capture, which is
// the whole question for most carriers but not for one whose acceptance depends on
// state the staging world lacks: the genetic registry's species set is entered by a
// level region a staging world has never been in. The pre-flight below is for that.

// TestACarrierRefusalLeavesTheLiveWorldUntouched drives the refusal through the
// live install path and asserts the world is exactly what it was.
func TestACarrierRefusalLeavesTheLiveWorldUntouched(t *testing.T) {
	t.Parallel()
	author := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer author.Close()
	tickUntilCursor(t, author)
	author.Tick(40)

	receiver := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer receiver.Close()
	tickUntilCursor(t, receiver)
	receiver.Tick(55)

	cap, err := author.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	unloadable := withUnregisterableGeneticState(t, cap)

	before := receiver.Snapshot()
	err = receiver.InstallShared(unloadable)
	if err == nil {
		t.Fatal("a capture no carrier could load was installed")
	}
	if !strings.Contains(err.Error(), "genetic") {
		t.Fatalf("the refusal does not name the carrier that made it: %v", err)
	}
	after := receiver.Snapshot()
	if idx, want, got, differs := snapshot.FirstDiff(before, after); differs {
		t.Fatalf("a refused capture changed the live world, line %d\n  before: %s\n  after:  %s",
			idx, want, got)
	}
}

// withUnregisterableGeneticState rewrites one carrier's record so that no build can
// install it: a population for a species with no declaration beside it.
//
// The integrity hash is recomputed, so the capture is intact and describes this
// session — the refusal under test is the carrier's, not the envelope's.
func withUnregisterableGeneticState(t *testing.T, cap snapshot.SharedCapture) snapshot.SharedCapture {
	t.Helper()
	out := cloneCapture(t, cap)
	replaced := false
	for i := range out.Systems {
		if out.Systems[i].System != "genetic" {
			continue
		}
		var snap map[string]any
		if err := json.Unmarshal(out.Systems[i].Data, &snap); err != nil {
			t.Fatalf("decode genetic record: %v", err)
		}
		snap["registrations"] = nil
		snap["registry"] = []map[string]any{{"id": 7, "name": "species_7"}}
		data, err := json.Marshal(snap)
		if err != nil {
			t.Fatalf("encode genetic record: %v", err)
		}
		out.Systems[i].Data = data
		replaced = true
	}
	if !replaced {
		t.Fatal("the capture carries no genetic record to make unloadable")
	}
	integrity, err := snapshot.Integrity(out)
	if err != nil {
		t.Fatalf("integrity: %v", err)
	}
	out.Header.Integrity = integrity
	return out
}

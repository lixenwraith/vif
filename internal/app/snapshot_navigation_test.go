package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// The navigation phase, demonstrated rather than asserted.
//
// D-17 throttles the flow-field recompute: a field is derived at most once every
// few ticks, and *which* ticks decides how old the field is a shared species steers
// by. The phase is therefore shared state, it is in no component store, and the
// navigation system declares and carries it under D-19.
//
// A gate that passes with the carrier present says nothing about a world without
// one, so each test here sabotages one part of what the carrier promises — in the
// encoded capture rather than in the system — and requires the gate to notice, with
// an unmodified control installed beside it to show the gate is catching the
// sabotage and not the setup.

// navRecord returns the navigation system's record from a capture.
func navRecord(t *testing.T, cap snapshot.SharedCapture) (int, map[string]any) {
	t.Helper()
	for i, rec := range cap.Systems {
		if rec.System != "navigation" {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Data, &body); err != nil {
			t.Fatalf("navigation record: %v", err)
		}
		return i, body
	}
	t.Fatal("the capture carries no navigation record; the sabotage would prove nothing")
	return 0, nil
}

// resealCapture writes a modified navigation record back and restores the integrity
// hash, so what the install rejects is the state and not the envelope.
func resealCapture(t *testing.T, cap snapshot.SharedCapture, idx int, body map[string]any) snapshot.SharedCapture {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("navigation record encode: %v", err)
	}
	out := cap
	out.Systems = append([]snapshot.SystemStateRecord(nil), cap.Systems...)
	out.Systems[idx] = snapshot.SystemStateRecord{System: "navigation", Data: data}
	out.Header.Integrity, err = snapshot.Integrity(out)
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	return out
}

// TestNavigationPhaseIsLoadBearing is the failing case the carrier needs, with its
// own control beside it.
//
// A capture whose navigation phase says something else installs cleanly, produces
// an identical world at the install tick, and then recomputes its flow fields on
// different ticks from the run it came from. The compared surface has to catch
// that, and the tick it catches it on is the first recompute either side makes.
// The unmodified capture through the same path must not be caught, or the
// sabotages are catching the setup rather than the sabotage.
//
// One origin serves every case: each receiver installs a differently sabotaged
// copy of one capture and is then driven against the same evolution, so what
// separates a caught case from the control is the sabotage and nothing else.
func TestNavigationPhaseIsLoadBearing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		sabotage func(map[string]any)
	}{
		{
			// The control. Nothing is edited, so this capture has to survive the
			// same 120 ticks the others are caught inside.
			name: "unmodified",
		},
		{
			name: "no_carrier",
			sabotage: func(body map[string]any) {
				// Exactly what a world with no carrier at all holds: a fresh cache,
				// armed to recompute immediately. It is the sabotage that matters
				// most, because it is the state the install would be in if the
				// declaration were dropped.
				for _, g := range body["groups"].([]any) {
					group := g.(map[string]any)
					group["point_ticks"] = float64(parameter.NavFlowMinTicksBetweenCompute)
					group["composite_ticks"] = float64(parameter.NavFlowMinTicksBetweenCompute)
					group["point_pending"] = true
					group["composite_pending"] = true
				}
			},
		},
		{
			// The targets the phase belongs to. LastTargets is not decoration: it is
			// what the dirty-distance test compares this tick's targets against, and
			// it is what the install derives the field from. A capture that named
			// different ones installs a field the sender never held, and species
			// steer by it from the next tick.
			name: "wrong_last_targets",
			sabotage: func(body map[string]any) {
				for _, g := range body["groups"].([]any) {
					group := g.(map[string]any)
					moved := []any{map[string]any{"X": 12.0, "Y": 6.0}}
					group["point_last_targets"] = moved
					group["composite_last_targets"] = moved
				}
			},
		},
	}

	origin, cap := navGateOrigin(t)
	defer origin.Close()
	receivers := make([]*App, len(cases))
	caught := make([]int, len(cases))
	for i, tc := range cases {
		idx, body := navRecord(t, cap)
		if groups, ok := body["groups"].([]any); !ok || len(groups) == 0 {
			t.Fatal("the navigation record carries no target group; nothing to sabotage")
		}
		install := cap
		if tc.sabotage != nil {
			tc.sabotage(body)
			install = resealCapture(t, cap, idx, body)
		}
		r := mustHeadless(t, navGateSeed, 120, 40)
		defer r.Close()
		tickUntilCursor(t, r)
		r.Tick(40)
		quiescePlayerDomain(t, r)
		if err := r.InstallShared(install); err != nil {
			t.Fatalf("%s install: %v", tc.name, err)
		}
		receivers[i], caught[i] = r, -1
	}

	// Tick by tick, because the signal is a gauge. nav.recomputes reports what
	// *this* tick recomputed and is overwritten by the next one, so a comparison
	// every twenty ticks reads two zeroes and calls it agreement. The phase decides
	// which tick a recompute lands on; a check that cannot see one tick cannot see
	// the phase.
	for step := range navSabotageTicks {
		origin.Tick(1)
		want := origin.SnapshotShared()
		for i, r := range receivers {
			r.Tick(1)
			if caught[i] >= 0 {
				continue
			}
			if idx, lx, ly, differs := snapshot.FirstDiff(want, r.SnapshotShared()); differs {
				caught[i] = step + 1
				t.Logf("%s: caught %d ticks after the install, at line %d\n  origin:   %s\n  receiver: %s",
					cases[i].name, step+1, idx, lx, ly)
			}
		}
	}
	for i, tc := range cases {
		switch {
		case tc.sabotage == nil && caught[i] >= 0:
			t.Fatalf("the unmodified capture diverged %d ticks after the install; "+
				"the sabotage cases are catching their setup", caught[i])
		case tc.sabotage != nil && caught[i] < 0:
			t.Fatalf("%s: a capture carrying a different navigation phase reproduced the "+
				"sender's evolution for %d ticks; the phase this carrier exists to "+
				"transfer is then not deciding anything the gate compares", tc.name, navSabotageTicks)
		}
	}
}

// navSabotageTicks is how long a sabotaged phase is given to show itself. A
// throttle interval is a handful of ticks and a route-rebuild interval is longer,
// so this is comfortably past both.
const navSabotageTicks = 120

const navGateSeed = 0x0FA57

// navGateOrigin builds a world with shared species steering by flow fields, and
// returns it beside a capture of it. The species are what make the phase matter:
// with nothing navigating, every recompute schedule looks the same.
func navGateOrigin(t *testing.T) (*App, snapshot.SharedCapture) {
	t.Helper()
	a := mustHeadless(t, navGateSeed, 120, 40)
	tickUntilCursor(t, a)
	a.Tick(400)
	quiescePlayerDomain(t, a)
	tickToStatBoundary(a)
	spawnSharedSpecies(t, a)
	a.Tick(1)

	var swarms int
	a.World().RunSafe(func() { swarms = a.World().Components.Swarm.CountEntities() })
	if swarms == 0 {
		a.Close()
		t.Fatal("no shared species navigate this world; the phase decides nothing")
	}

	cap, err := a.CaptureShared()
	if err != nil {
		a.Close()
		t.Fatalf("capture: %v", err)
	}
	encoded, err := snapshot.EncodeCapture(cap)
	if err != nil {
		a.Close()
		t.Fatalf("encode: %v", err)
	}
	decoded, err := snapshot.DecodeCapture(encoded)
	if err != nil {
		a.Close()
		t.Fatalf("decode: %v", err)
	}
	return a, decoded
}

// navRouteSeed is the world the gateway scenario builds on top of.
const navRouteSeed = 0x0FA57

// TestTheGatewayWorldKeepsItsRebuildScheduleAndGeneticStream is the route-rebuild
// budget's failing case, its control, and the genetic continuation gate over one
// scenario.
//
// The sabotage is the value a world with no carrier holds: a zeroed budget, which
// puts the next rebuild a whole interval away from where the sender had it. The
// receiver installs cleanly, holds the sender's world at the install tick, and then
// rebuilds its gateways' routes on different ticks. The unmodified capture beside
// it must rebuild on the sender's ticks *and* keep the genotype stream equal —
// including pending evaluations already attached to live eyes — as the gateway
// world keeps spawning GA-managed eyes.
//
// One origin drives both, because the tower scenario is the most expensive fixture
// in this package and the two receivers are answering the same question from
// opposite sides.
func TestTheGatewayWorldKeepsItsRebuildScheduleAndGeneticStream(t *testing.T) {
	t.Parallel()
	origin, cap := navRouteOrigin(t)
	defer origin.Close()
	if routeGraphSignature(origin) == "" {
		t.Fatal("no gateway holds a route graph; the budget would pace nothing")
	}

	idx, body := navRecord(t, cap)
	ticks, ok := body["route_rebuild_ticks"].(float64)
	if !ok {
		t.Fatal("the navigation record carries no route_rebuild_ticks; nothing to sabotage")
	}
	if ticks == 0 {
		t.Fatal("the capture was taken on a rebuild tick, so the sabotage below is a no-op")
	}
	body["route_rebuild_ticks"] = float64(0)

	clean, sabotaged := navRouteWorld(t), navRouteWorld(t)
	defer clean.Close()
	defer sabotaged.Close()
	if err := clean.InstallShared(cap); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := sabotaged.InstallShared(resealCapture(t, cap, idx, body)); err != nil {
		t.Fatalf("sabotaged install: %v", err)
	}

	// The motion is the point. A route graph is stale only when its target has moved
	// off the cell it was computed for, so a world whose target stands still rebuilds
	// nothing and the budget decides nothing — which is exactly the state the shipped
	// scenario leaves it in, and why this had no failing case. The same placement is
	// written to every instance at the same tick, so what differs is the schedule the
	// carrier restored and not the input.
	//
	// The route graph is the narrow observable — the cell each gateway was computed
	// to reach and how many routes came out — so an unrelated shared-state difference
	// cannot masquerade as a route-rebuild phase failure. A rebuild is precisely the
	// moment a graph adopts the target's current cell, so two instances whose budgets
	// stand apart hold graphs aimed at different cells for as long as the gap lasts.
	_, initialMax := genotypeSignature(origin)
	diverged, spawned := 0, false
	for step := range navSabotageTicks {
		for _, a := range []*App{origin, clean, sabotaged} {
			moveRouteTarget(a, step)
			a.Tick(1)
		}
		want := routeGraphSignature(origin)
		if got := routeGraphSignature(clean); want != got {
			t.Fatalf("the unmodified capture rebuilt on different ticks %d ticks after install;\n"+
				"  the sabotage case is catching its setup\n  origin:   %s\n  receiver: %s",
				step+1, want, got)
		}
		if diverged == 0 && want != routeGraphSignature(sabotaged) {
			diverged = step + 1
		}
		wantGen, wantMax := genotypeSignature(origin)
		gotGen, gotMax := genotypeSignature(clean)
		if wantGen != gotGen || wantMax != gotMax {
			t.Fatalf("genetic continuation diverged %d ticks after install\n"+
				"  origin:   %s\n  receiver: %s", step+1, wantGen, gotGen)
		}
		spawned = spawned || wantMax > initialMax
	}
	if diverged == 0 {
		t.Fatal("a capture carrying a different route-rebuild budget rebuilt on the sender's " +
			"ticks anyway; the budget this carrier transfers is then deciding nothing")
	}
	if !spawned {
		t.Fatal("no genotype changed after install; the continuation gate exercised no new sample")
	}
	t.Logf("route graphs came apart %d ticks after the install", diverged)
}

// genotypeSignature renders the captured shared genotype store. It excludes
// adaptation telemetry deliberately: this is the genetic stream's contract, and the
// route-learning carrier is compared through routeGraphSignature instead.
func genotypeSignature(a *App) (string, uint64) {
	var (
		data  []byte
		maxID uint64
	)
	a.World().RunSafe(func() {
		entries := a.World().CaptureSharedWorld().Genotype
		data, _ = json.Marshal(entries)
		for _, entry := range entries {
			if entry.Value.EvalID > maxID {
				maxID = entry.Value.EvalID
			}
		}
	})
	return string(data), maxID
}

// routeGraphSignature renders every gateway's route graph: the cell it was computed
// to reach and the number of routes it produced, in gateway order.
func routeGraphSignature(a *App) string {
	var b strings.Builder
	a.World().RunSafe(func() {
		w := a.World()
		ids := make([]uint32, 0, 8)
		for _, e := range w.Components.Gateway.Entities() {
			if gw, ok := w.Components.Gateway.GetPtr(e); ok && gw.RouteDistID != 0 {
				ids = append(ids, gw.RouteDistID)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			rg := w.Resources.RouteGraph.Get(id)
			if rg == nil {
				fmt.Fprintf(&b, "%d:none ", id)
				continue
			}
			fmt.Fprintf(&b, "%d:(%d,%d):%d ", id, rg.TargetX, rg.TargetY, len(rg.Routes))
		}
	})
	return b.String()
}

// moveRouteTarget walks the gateways' target group in a slow rectangle.
//
// It writes the placement directly rather than emitting a request, for the same
// reason the sabotages above edit an encoded capture: what is being tested is the
// schedule, and the cleanest way to hold everything else equal is to give both
// instances the identical write at the identical tick. Group zero is the cursors and
// is not what a gateway steers by; the tower chain's gateways name group one, whose
// target is the anchor this moves.
func moveRouteTarget(a *App, step int) {
	if step%navRouteTargetStride != 0 {
		return
	}
	ring := []struct{ x, y int }{{40, 20}, {96, 20}, {96, 30}, {40, 30}}
	p := ring[(step/navRouteTargetStride)%len(ring)]
	a.World().RunSafe(func() {
		w := a.World()
		for _, e := range w.Components.TargetAnchor.Entities() {
			anchor, ok := w.Components.TargetAnchor.GetPtr(e)
			if !ok || anchor.GroupID == 0 {
				continue
			}
			w.Positions.SetPosition(e, component.PositionComponent{X: p.x, Y: p.y})
		}
	})
}

// navRouteTargetStride is how often the target moves, in ticks. Half the rebuild
// interval, so a graph is stale every time the budget allows a rebuild and the
// budget is what decides which tick that is.
const navRouteTargetStride = parameter.NavRouteRebuildInterval / 2

// navRouteWorld builds the one scenario this game has that engages gateways.
//
// The tower region is it: its chain spawns four pylons and attaches a route-graph
// gateway to each, which is the only path in any shipped config that makes
// route_rebuild_ticks pace anything. Nothing in the default escalation reaches it
// inside a test-length run, so the region is entered outright — the same thing the
// tower soak does, for the same reason.
func navRouteWorld(t *testing.T) *App {
	t.Helper()
	a, err := NewHeadless(towerConfig(t, navRouteSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	a.Tick(1)
	a.Region(event.RegionSpawn, "tower", "TowerSetup")
	// The chain has to finish before the target starts moving: a pylon is spawned,
	// its gateway attached, and its route graph computed once, and a target that
	// moves before that leaves every graph unbuilt rather than stale.
	a.Tick(navRouteSettleTicks)
	// Player-domain production is stopped for the same reason the other gates stop
	// it: a capture carries no player state, so two instances holding one shared
	// world still hold different drains, and a drain that reaches a shared entity
	// moves the compared surface for a reason that is not the phase under test.
	quiescePlayerDomain(t, a)
	for step := range navRouteWarmupTicks {
		moveRouteTarget(a, step)
		a.Tick(1)
	}
	return a
}

// navRouteSettleTicks is how long the tower chain is given to attach its gateways
// and compute their first route graphs.
const navRouteSettleTicks = 120

// navRouteOrigin returns the gateway world beside a capture of it, having checked
// that the scenario is exercising the phase under test rather than sitting past it.
func navRouteOrigin(t *testing.T) (*App, snapshot.SharedCapture) {
	t.Helper()
	a := navRouteWorld(t)
	tickToStatBoundary(a)

	var gateways, routed int
	a.World().RunSafe(func() {
		w := a.World()
		gateways = w.Components.Gateway.CountEntities()
		w.Components.Navigation.Each(func(_ core.Entity, nav *component.NavigationComponent) bool {
			if nav.UseRouteGraph {
				routed++
			}
			return true
		})
	})
	if gateways == 0 {
		a.Close()
		t.Fatal("the tower region built no gateway; route_rebuild_ticks would pace nothing")
	}
	if routed == 0 {
		a.Close()
		t.Fatal("no entity follows a route graph; a rebuild would change nothing observable")
	}

	cap, err := a.CaptureShared()
	if err != nil {
		a.Close()
		t.Fatalf("capture: %v", err)
	}
	encoded, err := snapshot.EncodeCapture(cap)
	if err != nil {
		a.Close()
		t.Fatalf("encode: %v", err)
	}
	decoded, err := snapshot.DecodeCapture(encoded)
	if err != nil {
		a.Close()
		t.Fatalf("decode: %v", err)
	}
	return a, decoded
}

// navRouteWarmupTicks is long enough for the tower chain to have attached its four
// gateways and for the eyes that follow their routes to exist and be moving.
const navRouteWarmupTicks = 160

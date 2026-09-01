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
)

// The navigation phase, demonstrated rather than asserted.
//
// D-17 throttles the flow-field recompute: a field is derived at most once every
// few ticks, and *which* ticks decides how old the field is that a shared species
// steers by. The phase is therefore shared state, it is not in any component store,
// and the navigation system declares it under D-19 and carries it.
//
// Phase 2 landed that carrier with no failing case behind it. The 500-tick gate
// passed with the carrier present, but nothing showed it would fail without one —
// so its coverage was a claim about the code rather than a result. These tests are
// the missing half. Each sabotages one part of what the carrier promises, in the
// encoded capture rather than in the system, and requires the gate to notice.
//
// Two things had to change before either sabotage could be caught, and both were
// defects rather than test scaffolding. The install left the field itself underived,
// so the first tick after it took Update's !Field.Valid branch — deriving from that
// tick's targets rather than the ones the phase belongs to, and zeroing the throttle
// on the way, which destroyed the phase the carrier had just restored. And the
// composite passability grid was still the one derived from the walls the install
// had replaced. Both are derived inside the install now.
//
// route_rebuild_ticks used to be the exception, for the same reason: it paces one
// gateway route graph rebuild per interval and the shipped scenario builds no
// gateways, so a sabotaged value changed nothing to observe. The gateway scenario at
// the bottom of this file is what closes it.

// navRecord returns the navigation system's record from a capture.
func navRecord(t *testing.T, cap SharedCapture) (int, map[string]any) {
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
func resealCapture(t *testing.T, cap SharedCapture, idx int, body map[string]any) SharedCapture {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("navigation record encode: %v", err)
	}
	out := cap
	out.Systems = append([]SystemStateRecord(nil), cap.Systems...)
	out.Systems[idx] = SystemStateRecord{System: "navigation", Data: data}
	out.Header.Integrity, err = captureIntegrity(out)
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	return out
}

// TestNavigationPhaseIsLoadBearing is the failing case Phase 2 owed.
//
// A capture whose navigation phase says something else installs cleanly, produces
// an identical world at the install tick, and then recomputes its flow fields on
// different ticks from the run it came from. The compared surface has to catch
// that, and the tick it catches it on is the first recompute either side makes.
func TestNavigationPhaseIsLoadBearing(t *testing.T) {
	cases := []struct {
		name     string
		sabotage func(map[string]any)
	}{
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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origin, cap := navGateOrigin(t)
			defer origin.Close()

			idx, body := navRecord(t, cap)
			if groups, ok := body["groups"].([]any); !ok || len(groups) == 0 {
				t.Fatal("the navigation record carries no target group; nothing to sabotage")
			}
			tc.sabotage(body)
			sabotaged := resealCapture(t, cap, idx, body)

			receiver := mustHeadless(t, navGateSeed, 120, 40)
			defer receiver.Close()
			tickUntilCursor(t, receiver)
			receiver.Tick(40)
			quiescePlayerDomain(t, receiver)
			if err := receiver.InstallShared(sabotaged); err != nil {
				t.Fatalf("install: %v", err)
			}

			// Tick by tick, because the signal is a gauge. nav.recomputes reports
			// what *this* tick recomputed and is overwritten by the next one, so a
			// comparison every twenty ticks reads two zeroes and calls it agreement.
			// The phase decides which tick a recompute lands on; a check that cannot
			// see one tick cannot see the phase.
			diverged := false
			for step := range navSabotageTicks {
				origin.Tick(1)
				receiver.Tick(1)
				if idx, lx, ly, differs := FirstDiff(origin.SnapshotShared(), receiver.SnapshotShared()); differs {
					t.Logf("caught %d ticks after the install, at line %d\n  origin:   %s\n  receiver: %s",
						step+1, idx, lx, ly)
					diverged = true
					break
				}
			}
			if !diverged {
				t.Fatal("a capture carrying a different navigation phase reproduced the " +
					"sender's evolution for 200 ticks; the phase this carrier exists to " +
					"transfer is then not deciding anything the gate compares")
			}
		})
	}
}

// TestNavigationPhaseSurvivesAnInstall is the positive half: the unmodified capture
// through the same path has to hold, or the sabotages above are catching the setup
// rather than the sabotage.
func TestNavigationPhaseSurvivesAnInstall(t *testing.T) {
	origin, cap := navGateOrigin(t)
	defer origin.Close()

	receiver := mustHeadless(t, navGateSeed, 120, 40)
	defer receiver.Close()
	tickUntilCursor(t, receiver)
	receiver.Tick(40)
	quiescePlayerDomain(t, receiver)
	if err := receiver.InstallShared(cap); err != nil {
		t.Fatalf("install: %v", err)
	}

	for step := range navSabotageTicks {
		origin.Tick(1)
		receiver.Tick(1)
		if idx, lx, ly, differs := FirstDiff(origin.SnapshotShared(), receiver.SnapshotShared()); differs {
			t.Fatalf("the unmodified capture diverged %d ticks after the install, at line %d\n"+
				"  origin:   %s\n  receiver: %s", step+1, idx, lx, ly)
		}
	}
}

// navSabotageTicks is how long a sabotaged phase is given to show itself. A
// throttle interval is a handful of ticks and a route-rebuild interval is longer,
// so this is comfortably past both.
const navSabotageTicks = 200

const navGateSeed = 0x0FA57

// navGateOrigin builds a world with shared species steering by flow fields, and
// returns it beside a capture of it. The species are what make the phase matter:
// with nothing navigating, every recompute schedule looks the same.
func navGateOrigin(t *testing.T) (*App, SharedCapture) {
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
	encoded, err := EncodeCapture(cap)
	if err != nil {
		a.Close()
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeCapture(encoded)
	if err != nil {
		a.Close()
		t.Fatalf("decode: %v", err)
	}
	return a, decoded
}

// The route-rebuild phase, which Phase 3 left uncovered.
//
// `route_rebuild_ticks` paces one gateway route graph rebuild per interval, and the
// navigation carrier has always carried it. Nothing exercised it: the shipped
// scenario builds no gateways, so no graph is ever rebuilt, so a sabotaged value
// changes nothing an install can be caught by. That is a coverage claim resting on
// the code rather than on a result, and it is the shape of defect this whole plan
// exists to stop having.
//
// What closes it is a scenario with gateways. A gateway that steers by a route
// graph rebuilds it whenever its target moves off the cell the graph was computed
// for, and the interval decides *which tick* that rebuild lands on; the entities the
// gateway spawns then follow the routes it produced, so a rebuild a tick early moves
// them differently for the rest of the run.

// navRouteSeed is the world the gateway scenario builds on top of.
const navRouteSeed = 0x0FA57

// TestNavigationRouteRebuildPhaseIsLoadBearing is doorstep item 5.
//
// The sabotage is the value a world with no carrier holds: a zeroed budget, which
// puts the next rebuild a whole interval away from where the sender had it. The
// receiver installs cleanly, holds the sender's world at the install tick, and then
// rebuilds its gateways' routes on different ticks — which is what the compared
// surface has to catch.
func TestNavigationRouteRebuildPhaseIsLoadBearing(t *testing.T) {
	origin, cap := navRouteOrigin(t)
	defer origin.Close()

	idx, body := navRecord(t, cap)
	ticks, ok := body["route_rebuild_ticks"].(float64)
	if !ok {
		t.Fatal("the navigation record carries no route_rebuild_ticks; nothing to sabotage")
	}
	if ticks == 0 {
		t.Fatal("the capture was taken on a rebuild tick, so the sabotage below is a no-op")
	}
	body["route_rebuild_ticks"] = float64(0)
	sabotaged := resealCapture(t, cap, idx, body)

	receiver := navRouteWorld(t)
	defer receiver.Close()
	if err := receiver.InstallShared(sabotaged); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !navRouteDiverges(t, origin, receiver) {
		t.Fatal("a capture carrying a different route-rebuild budget rebuilt on the sender's " +
			"ticks anyway; the budget this carrier transfers is then deciding nothing")
	}
}

// TestNavigationRouteRebuildSurvivesAnInstall is the positive half: without the
// sabotage the same scenario has to hold, or the case above is catching its setup.
func TestNavigationRouteRebuildSurvivesAnInstall(t *testing.T) {
	origin, cap := navRouteOrigin(t)
	defer origin.Close()

	receiver := navRouteWorld(t)
	defer receiver.Close()
	if err := receiver.InstallShared(cap); err != nil {
		t.Fatalf("install: %v", err)
	}
	if navRouteDiverges(t, origin, receiver) {
		t.Fatal("the unmodified capture rebuilt on different ticks; the sabotage case is " +
			"catching its setup rather than the sabotage")
	}
}

// navRouteDiverges drives both instances through the same target motion and reports
// whether their gateways rebuilt their route graphs on different ticks.
//
// The motion is the point. A route graph is stale only when its target has moved off
// the cell it was computed for, so a world whose target stands still rebuilds nothing
// and the budget decides nothing — which is exactly the state the shipped scenario
// leaves it in, and why this had no failing case. The same placement is written to
// both instances at the same tick, so what differs between them is the schedule the
// carrier restored and not the input.
//
// What is compared is the graph each gateway holds — the cell it was computed to
// reach and how many routes came out — rather than the whole shared surface. That is
// the budget's own consequence and nothing else's: a rebuild is precisely the moment
// a graph adopts the target's current cell, so two instances whose budgets stand
// apart hold graphs aimed at different cells for as long as the gap lasts.
//
// The whole surface cannot be the observable here, and the reason is a defect this
// scenario found rather than a weakness of this test: with gateways spawning, a
// receiver hands the next eye a different genotype than the sender did, and the two
// worlds come apart within ten ticks for a reason that has nothing to do with
// navigation. The note at the end of this file says what that is.
func navRouteDiverges(t *testing.T, origin, receiver *App) bool {
	t.Helper()
	if routeGraphSignature(origin) == "" {
		t.Fatal("no gateway holds a route graph; the budget would pace nothing")
	}
	for step := range navSabotageTicks {
		moveRouteTarget(origin, step)
		moveRouteTarget(receiver, step)
		origin.Tick(1)
		receiver.Tick(1)
		want, got := routeGraphSignature(origin), routeGraphSignature(receiver)
		if want != got {
			t.Logf("route graphs came apart %d ticks after the install\n  origin:   %s\n  receiver: %s",
				step+1, want, got)
			return true
		}
	}
	return false
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
func navRouteOrigin(t *testing.T) (*App, SharedCapture) {
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
	encoded, err := EncodeCapture(cap)
	if err != nil {
		a.Close()
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeCapture(encoded)
	if err != nil {
		a.Close()
		t.Fatalf("decode: %v", err)
	}
	return a, decoded
}

// navRouteWarmupTicks is long enough for the tower chain to have attached its four
// gateways and for the eyes that follow their routes to exist and be moving.
const navRouteWarmupTicks = 240

// What this scenario found, and did not fix.
//
// The route-rebuild budget is covered now. The gateway world it needed also showed
// something else, and it belongs to D-19 rather than to navigation: a receiver that
// installs a capture and then lets a gateway spawn its next eye gets a *different
// genotype* than the sender did.
//
// The genetic carrier exports each species' archive — its members and its generation
// — and that is all `Registry.Export` has to give it. `pkg/genetic`'s streaming
// engine holds more than an archive: its own `math/rand/v2` generator, a ring of
// offspring it has *already produced* and will hand out before it makes any more, a
// pending-evaluation table and the id counter that names them. All four decide the
// next genotype, none of them is in the export, and an installed world therefore
// resumes evolution from the sender's population and the receiver's queue.
//
// It is the same shape as the two defects Phase 2 fixed for the adaptation resource,
// which carries its pre-sampled pool and consumer head for exactly this reason, and
// as the maze generator, which carries its PCG's binary form. It is out of Phase 4's
// scope — it is in pkg/, it is not on the transport path, and the export contract
// this needs is a piece of work rather than a line — so it is recorded here and in
// the plan rather than fixed in passing. Its practical effect today is bounded: it
// changes what a *newly spawned* species looks like after an install, and the
// correction that follows replaces it.

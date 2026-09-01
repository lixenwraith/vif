package app

import (
	"encoding/json"
	"testing"

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
// What is *not* covered here: route_rebuild_ticks. It paces one gateway route graph
// rebuild per interval, and the shipped scenario builds no gateways, so nothing in
// the default world is paced by it and a sabotaged value changes nothing to observe.
// Its coverage still rests on the code rather than on a result, and a scenario with
// gateways is what would close it.

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

package app

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
)

// fsmConfigTrees names every configuration a build can boot: the two shipped
// scripts, the empty one, and the copy embedded in the binary. A rule that holds
// for one of them and not the others is not a rule.
func fsmConfigTrees(t *testing.T) map[string]func() (map[string]any, error) {
	t.Helper()
	root := repoRoot(t)
	trees := map[string]func() (map[string]any, error){
		"asset(embedded)": func() (map[string]any, error) {
			return fsm.ResolveConfig(asset.DefaultFSMConfig, asset.DefaultFSMEntry)
		},
	}
	for _, dir := range []string{"main", "td", "blank"} {
		d := filepath.Join(root, "config", dir)
		if _, err := os.Stat(filepath.Join(d, "game.toml")); err != nil {
			continue
		}
		trees["config/"+dir] = func() (map[string]any, error) {
			return fsm.ResolveConfig(os.DirFS(d), "game.toml")
		}
	}
	return trees
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("module root not found above the test's working directory")
	return ""
}

// TestFSMTriggersAreReplicated is D-20 made mechanical.
//
// Every FSM region is shared state (§4): each instance re-derives the same region
// in the same state at the same tick, and fsm.<region> is compared across the
// session. A region can only stay in agreement if every event that moves it is an
// event every instance holds. A ClassLocal trigger is not: by definition it never
// replicates, so the region advances on the one instance whose participant
// produced it and nowhere else, and the two never converge again — nothing
// re-derives a missing local event.
//
// This is not hypothetical. MonitorActive transitioned on EventHeatBurst, which
// HeatSystem pushes with PushLocal for the cursor that overheated. In the
// 2026-08-31 session that fired at tick 1903; the shared surface reported
// reg|stat|fsm.monitor divergent from tick 1914 and the session was marked
// DIVERGED at 1934. The sweep it wanted is a per-instance effect (D-6) and
// HeatSystem emits it directly now.
func TestFSMTriggersAreReplicated(t *testing.T) {
	event.EnsureRegistry()

	totalChecked := 0
	trees := fsmConfigTrees(t)
	if len(trees) < 2 {
		t.Fatalf("only %d config tree(s) reachable; the check would barely cover anything", len(trees))
	}
	for name, load := range trees {
		t.Run(name, func(t *testing.T) {
			merged, err := load()
			if err != nil {
				t.Fatalf("resolve config: %v", err)
			}
			var root fsm.RootConfig
			if err := toml.Decode(merged, &root); err != nil {
				t.Fatalf("decode config: %v", err)
			}
			if len(root.States) == 0 {
				t.Fatal("no states decoded; the check would pass vacuously")
			}

			var offenders []string
			checked := 0
			for stateName, state := range root.States {
				if state == nil {
					continue
				}
				for _, tr := range state.Transitions {
					// "Tick" is the machine's own pulse, not a game event, and it
					// arrives on every instance by construction.
					if tr.Trigger == "" || tr.Trigger == "Tick" {
						continue
					}
					et, ok := event.GetEventType(tr.Trigger)
					if !ok {
						offenders = append(offenders, stateName+": unknown trigger "+tr.Trigger)
						continue
					}
					checked++
					// Stamped is resolved per event from the producer's domain, so
					// the type alone cannot condemn it; a Stamped trigger is the one
					// case this check hands to the producer's own domain rules.
					if c := event.ClassOf(et); c == event.ClassLocal || c == event.ClassUnset {
						offenders = append(offenders,
							stateName+" transitions on "+tr.Trigger+" ("+c.String()+")")
					}
				}
			}
			// config/blank declares no transitions at all, so a per-tree floor
			// would fail it; the suite-wide floor below is what keeps the check
			// from passing vacuously.
			totalChecked += checked
			if len(offenders) > 0 {
				sort.Strings(offenders)
				t.Fatalf("a shared FSM region is steered by an event that does not "+
					"replicate, so only the producing instance advances it:\n  %v",
					offenders)
			}
		})
	}
	if totalChecked == 0 {
		t.Fatal("no event triggers found in any config tree; the check passed vacuously")
	}
}

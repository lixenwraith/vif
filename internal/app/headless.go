// Package app: headless harness.
//
// A headless App has no terminal, audio service or renderer, and runs on a
// ManualClock advanced only by Tick. Nothing runs on another goroutine, so a
// run is a pure function of its seed, its config, and the injected intent
// sequence. Close is the caller's responsibility.
package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
)

// NewHeadless builds and starts a headless runtime without spawning the
// scheduler or event goroutines. Tick drives the simulation instead.
func NewHeadless(cfg Config) (*App, error) {
	cfg.Headless = true

	a, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if err := a.hub.StartAll(); err != nil {
		a.Close()
		return nil, err
	}

	// Seal the system set and freeze the metric set before anything dispatches,
	// so a Settle preceding the first Tick cannot register a late metric.
	a.scheduler.Prepare()
	return a, nil
}

// Tick advances the simulation by n ticks, servicing a pending FSM reset before
// each one. Pause does not gate a stepped tick, so n ticks always execute.
func (a *App) Tick(n int) {
	if n < 0 {
		return
	}
	a.scheduler.RunTicks(n)
}

// Settle dispatches queued events without advancing time
func (a *App) Settle() {
	a.scheduler.Settle()
}

// Inject applies intents at a tick boundary and settles what they emit.
// Returns false once an intent quits the game; remaining intents are dropped.
// Intents are the sole injection path: hand-built ones must carry Count >= 1
// where a motion count applies, since no input.Machine normalizes them.
func (a *App) Inject(intents ...*input.Intent) bool {
	cont := true
	for _, intent := range intents {
		if !a.handleIntent(intent) {
			cont = false
			break
		}
	}
	a.scheduler.Settle()
	return cont
}

// InputTick advances input-driven work: auto-fire, held-button repeat and macro
// playback. Their intervals are game time, so a caller that never Ticks emits at
// most one round. Returns false when a macro intent quits the game.
func (a *App) InputTick() bool {
	emitted := a.router.ProcessInputTick()

	macroIntents := a.router.ProcessMacroTick()
	for _, intent := range macroIntents {
		if !a.handleIntent(intent) {
			return false
		}
	}

	if emitted || len(macroIntents) > 0 {
		a.scheduler.Settle()
	}
	return true
}

// SetupLevel resizes the map independently of the viewport; cropOnResize=false
// decouples them, so a headless run emulates either mode.
func (a *App) SetupLevel(width, height int, clearEntities, cropOnResize bool) {
	a.ctx.PushEvent(event.EventLevelSetup, &event.LevelSetupPayload{
		Width:         width,
		Height:        height,
		ClearEntities: clearEntities,
		CropOnResize:  cropOnResize,
	})
	a.scheduler.Settle()
}

// Reset requests a new game; purge additionally clears operator session state.
// MetaSystem's synchronous cleanup lands here, the FSM reset at the next Tick,
// matching the interactive ordering.
func (a *App) Reset(purge bool) {
	a.ctx.PushEvent(event.EventGameResetRequest, &event.GameResetPayload{Purge: purge})
	a.scheduler.Settle()
}

// Context returns the game context, for assertions the snapshot does not carry
func (a *App) Context() *engine.GameContext { return a.ctx }

// World returns the ECS world
func (a *App) World() *engine.World { return a.world }

// Seed returns the root seed the run resolved, including a drawn one
func (a *App) Seed() uint64 { return a.cfg.Seed }

// === Snapshot ===

// Snapshot returns the sorted context and registry state as comparable lines.
// Two runs of one seed must produce identical slices.
func (a *App) Snapshot() []string {
	lines := make([]string, 0, 64)

	// One critical section: SnapshotContext reads world state, and the registry
	// reading belongs to the same instant
	a.world.RunSafe(func() {
		wd := a.worldDigestLocked()
		lines = append(lines, "ctx|digest"+
			"|positions="+wd.Positions.String()+
			"|kinetics="+wd.Kinetics.String()+
			"|combat="+wd.Combat.String()+
			"|entities="+wd.Entities.String())

		a.ctx.SnapshotContext(func(sub string, args ...any) {
			lines = append(lines, snapshotLine("ctx", sub, args))
		})
		a.world.Resources.Status.Snapshot(func(sub string, args ...any) {
			lines = append(lines, snapshotLine("reg", sub, args))
		})
	})

	slices.Sort(lines)
	return lines
}

// snapshotLine renders one emitted record as "source|sub|key=value|...".
// The source tag separates the two emitters, which share group names.
func snapshotLine(source, sub string, args []any) string {
	var b strings.Builder
	b.WriteString(source)
	b.WriteByte('|')
	b.WriteString(sub)
	for i := 0; i+1 < len(args); i += 2 {
		key, _ := args[i].(string)
		b.WriteByte('|')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(formatSnapshotValue(args[i+1]))
	}
	return b.String()
}

// formatSnapshotValue renders a metric for comparison. Floats use the shortest
// round-tripping form, which is exact for same-process equality.
func formatSnapshotValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

// FirstDiff returns the index and both values of the first differing snapshot
// line; ok is false when the snapshots are identical.
func FirstDiff(x, y []string) (idx int, lineX, lineY string, ok bool) {
	n := min(len(x), len(y))
	for i := range n {
		if x[i] != y[i] {
			return i, x[i], y[i], true
		}
	}
	switch {
	case len(x) > n:
		return n, x[n], "", true
	case len(y) > n:
		return n, "", y[n], true
	}
	return 0, "", "", false
}

// Region applies an FSM region operation and settles what it emits.
// State is required by event.RegionSpawn and ignored otherwise.
func (a *App) Region(op, region, state string) {
	a.ctx.PushEvent(event.EventFSMRegionRequest, &event.FSMRegionPayload{
		Op: op, Region: region, State: state,
	})
	a.scheduler.Settle()
}

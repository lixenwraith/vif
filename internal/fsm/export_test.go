package fsm_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/fsm/std"
)

const reconcileConfig = `
[regions.base]
initial = "Idle"

[regions.quasar]

[regions.storm]

[states.Idle]
parent = "Root"

[states.QuasarHold]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventDrainPause", reconcile = true },
]
on_exit = [
    { action = "EmitEvent", event = "EventDrainResume", reconcile = true },
]

[states.QuasarLive]
parent = "QuasarHold"
on_enter = [
    { action = "EmitEvent", event = "EventStrobeRequest" },
]

[states.StormHold]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventDrainPause", reconcile = true },
]
on_exit = [
    { action = "EmitEvent", event = "EventDrainResume", reconcile = true },
]

[states.StormLive]
parent = "StormHold"
`

type eventTrace []event.EventType

func newReconcileMachine(t *testing.T) (*fsm.Machine[*eventTrace], *eventTrace) {
	t.Helper()
	event.EnsureRegistry()
	trace := new(eventTrace)
	m := fsm.NewMachine[*eventTrace]()
	std.Register(m, std.Host[*eventTrace]{
		Emit: func(dst *eventTrace, et event.EventType, _ any) {
			*dst = append(*dst, et)
		},
	})
	if err := m.LoadConfig([]byte(reconcileConfig)); err != nil {
		t.Fatalf("load test machine: %v", err)
	}
	if err := m.Init(trace); err != nil {
		t.Fatalf("init test machine: %v", err)
	}
	return m, trace
}

func spawnState(t *testing.T, m *fsm.Machine[*eventTrace], trace *eventTrace, region, state string) {
	t.Helper()
	id, ok := m.GetStateID(state)
	if !ok {
		t.Fatalf("state %q was not compiled", state)
	}
	if err := m.SpawnRegion(trace, region, id); err != nil {
		t.Fatalf("spawn %s at %s: %v", region, state, err)
	}
}

func TestImportReconcilesOnlyCrossedLocalLifecycle(t *testing.T) {
	source, sourceTrace := newReconcileMachine(t)
	spawnState(t, source, sourceTrace, "storm", "StormLive")
	target := source.Export()

	live, got := newReconcileMachine(t)
	spawnState(t, live, got, "quasar", "QuasarLive")
	*got = nil // discard ordinary spawn entry: pause followed by the one-shot strobe

	actions, err := live.ImportReconciled(got, target)
	if err != nil {
		t.Fatalf("reconciled import: %v", err)
	}
	want := eventTrace{event.EventDrainResume, event.EventDrainPause}
	if len(*got) != len(want) {
		t.Fatalf("reconciled events = %v, want %v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("reconciled events = %v, want %v", *got, want)
		}
	}
	if actions != len(want) {
		t.Fatalf("reconciled action count = %d, want %d", actions, len(want))
	}
	if live.HasRegion("quasar") || !live.HasRegion("storm") {
		t.Fatalf("regions after import = %v, want base and storm", live.ActiveRegions())
	}
}

func TestImportReconcilesChangedLifecycleScope(t *testing.T) {
	event.EnsureRegistry()
	const cfg = `
[regions.base]
initial = "Idle"
[regions.quasar]

[states.Idle]
parent = "Root"
[states.Hold]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventDrainPause", payload = { entity = 0 }, payload_vars = { entity = "owner" }, reconcile = true },
]
on_exit = [
    { action = "EmitEvent", event = "EventDrainResume", payload = { entity = 0 }, payload_vars = { entity = "owner" }, reconcile = true },
]
[states.Live]
parent = "Hold"
`
	type record struct {
		typeID event.EventType
		owner  int64
	}
	type trace []record
	build := func(t *testing.T, owner int64) (*fsm.Machine[*trace], *trace) {
		t.Helper()
		got := new(trace)
		m := fsm.NewMachine[*trace]()
		std.Register(m, std.Host[*trace]{
			Emit: func(dst *trace, et event.EventType, payload any) {
				var owner int64
				if p, ok := payload.(*event.CursorScopePayload); ok {
					owner = int64(p.Entity)
				}
				*dst = append(*dst, record{typeID: et, owner: owner})
			},
		})
		if err := m.LoadConfig([]byte(cfg)); err != nil {
			t.Fatalf("load scoped machine: %v", err)
		}
		if err := m.Init(got); err != nil {
			t.Fatalf("init scoped machine: %v", err)
		}
		m.SetVar("owner", owner)
		id, _ := m.GetStateID("Live")
		if err := m.SpawnRegion(got, "quasar", id); err != nil {
			t.Fatalf("spawn scoped hold: %v", err)
		}
		*got = nil
		return m, got
	}

	source, _ := build(t, 22)
	live, got := build(t, 11)
	actions, err := live.ImportReconciled(got, source.Export())
	if err != nil {
		t.Fatalf("reconcile changed owner: %v", err)
	}
	want := trace{
		{typeID: event.EventDrainResume, owner: 11},
		{typeID: event.EventDrainPause, owner: 22},
	}
	if len(*got) != len(want) {
		t.Fatalf("scoped events = %+v, want %+v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("scoped events = %+v, want %+v", *got, want)
		}
	}
	if actions != len(want) {
		t.Fatalf("reconciled action count = %d, want %d", actions, len(want))
	}
}

func TestStagingImportHasNoLocalSideEffects(t *testing.T) {
	source, sourceTrace := newReconcileMachine(t)
	spawnState(t, source, sourceTrace, "storm", "StormLive")
	target := source.Export()

	staging, got := newReconcileMachine(t)
	spawnState(t, staging, got, "quasar", "QuasarLive")
	*got = nil

	if err := staging.Import(got, target); err != nil {
		t.Fatalf("staging import: %v", err)
	}
	if len(*got) != 0 {
		t.Fatalf("staging import emitted %v", *got)
	}
}

func TestDelayedActionsRoundTripByCompiledIdentity(t *testing.T) {
	event.EnsureRegistry()
	tests := []struct {
		name     string
		config   string
		schedule func(*fsm.Machine[*eventTrace], *eventTrace) bool
	}{
		{
			name: "entry",
			config: `
[regions.main]
initial = "Active"

[states.Active]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventNavigationRegraph", delay_ms = 100 },
]
`,
			schedule: func(*fsm.Machine[*eventTrace], *eventTrace) bool { return true },
		},
		{
			name: "update",
			config: `
[regions.main]
initial = "Active"

[states.Active]
parent = "Root"
on_update = [
    { action = "EmitEvent", event = "EventNavigationRegraph", delay_ms = 100 },
]
`,
			schedule: func(m *fsm.Machine[*eventTrace], trace *eventTrace) bool {
				m.Update(trace, time.Millisecond)
				return true
			},
		},
		{
			name: "external transition",
			config: `
[regions.main]
initial = "Watch"

[states.Watch]
parent = "Root"
transitions = [
    { trigger = "EventGoldSpawnRequest", target = "Active", actions = [
        { action = "EmitEvent", event = "EventNavigationRegraph", delay_ms = 100 },
    ] },
]

[states.Active]
parent = "Root"
`,
			schedule: func(m *fsm.Machine[*eventTrace], trace *eventTrace) bool {
				return m.HandleEvent(trace, event.GameEvent{Type: event.EventGoldSpawnRequest})
			},
		},
		{
			name: "internal transition",
			config: `
[regions.main]
initial = "Watch"

[states.Watch]
parent = "Root"
transitions = [
    { trigger = "EventGoldSpawnRequest", internal = true, actions = [
        { action = "SetVar", payload = { name = "observed", value = 1 } },
        { action = "EmitEvent", event = "EventNavigationRegraph", delay_ms = 100 },
    ] },
]
			`,
			schedule: func(m *fsm.Machine[*eventTrace], trace *eventTrace) bool {
				return m.HandleEvent(trace, event.GameEvent{Type: event.EventGoldSpawnRequest})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := func(t *testing.T) (*fsm.Machine[*eventTrace], *eventTrace) {
				t.Helper()
				trace := new(eventTrace)
				m := fsm.NewMachine[*eventTrace]()
				std.Register(m, std.Host[*eventTrace]{
					Emit: func(dst *eventTrace, et event.EventType, _ any) {
						*dst = append(*dst, et)
					},
				})
				if err := m.LoadConfig([]byte(tt.config)); err != nil {
					t.Fatalf("load delayed-action machine: %v", err)
				}
				if err := m.Init(trace); err != nil {
					t.Fatalf("init delayed-action machine: %v", err)
				}
				return m, trace
			}

			source, sourceTrace := build(t)
			if !tt.schedule(source, sourceTrace) {
				t.Fatal("action source did not handle its trigger")
			}
			state := source.Export()
			if len(state.Delayed) != 1 || state.Delayed[0].ActionID == 0 {
				t.Fatalf("exported delayed actions = %+v, want one compiled identity", state.Delayed)
			}

			restored, got := build(t)
			if err := restored.Import(got, state); err != nil {
				t.Fatalf("restore delayed action: %v", err)
			}
			restored.Update(got, 50*time.Millisecond)
			if len(*got) != 0 {
				t.Fatalf("delayed event fired early: %v", *got)
			}
			restored.Update(got, 50*time.Millisecond)
			want := eventTrace{event.EventNavigationRegraph}
			if len(*got) != 1 || (*got)[0] != want[0] {
				t.Fatalf("restored delayed events = %v, want %v", *got, want)
			}

			// An identity this build cannot resolve must reject the whole import
			// before the otherwise-valid position or variables move.
			rejected, rejectedTrace := build(t)
			before := rejected.Export()
			invalid := state
			invalid.Delayed = append([]fsm.DelayedSnapshot(nil), state.Delayed...)
			invalid.Delayed[0].ActionID = ^uint32(0)
			if err := rejected.Import(rejectedTrace, invalid); err == nil || !strings.Contains(err.Error(), "not present in this build") {
				t.Fatalf("invalid delayed identity error = %v", err)
			}
			if after := rejected.Export(); !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected import mutated machine:\n before=%+v\n  after=%+v", before, after)
			}
		})
	}
}

func TestReconcileMarkerValidation(t *testing.T) {
	event.EnsureRegistry()
	tests := []struct {
		name   string
		action string
		want   string
	}{
		{
			name:   "shared event",
			action: `{ action = "EmitEvent", event = "EventGoldSpawnRequest", reconcile = true }`,
			want:   "want local",
		},
		{
			name:   "one-shot update",
			action: `{ action = "EmitEvent", event = "EventDrainPause", reconcile = true }`,
			want:   "only on state on_enter/on_exit",
		},
		{
			name:   "delayed lifecycle",
			action: `{ action = "EmitEvent", event = "EventDrainPause", reconcile = true, delay_ms = 10 }`,
			want:   "cannot be delayed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := "on_enter"
			if tt.name == "one-shot update" {
				field = "on_update"
			}
			cfg := `[regions.main]
initial = "Active"
[states.Active]
parent = "Root"
` + field + ` = [` + tt.action + `]
`
			m := fsm.NewMachine[struct{}]()
			std.Register(m, std.Host[struct{}]{})
			err := m.LoadConfig([]byte(cfg))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("load error = %v, want text %q", err, tt.want)
			}
		})
	}
}

package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/genetic"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
)

// TestABrokenGraphFailsAtLoadNamingTheFault: a bot graph is resolved by name with no
// author watching, so every reference, key and value is checked when it is parsed.
func TestABrokenGraphFailsAtLoadNamingTheFault(t *testing.T) {
	const region = "\n[regions.play]\ninitial = \"A\"\n"
	// a always steers and b only while the scenario's boss region runs
	const two = "[network.a]\ninputs = [\"column\"]\n[network.b]\ninputs = [\"column\"]\nwhile = { name = \"InState\", args = { region = \"boss\" } }"
	for _, tt := range []struct{ doc, want string }{
		{"[systems]\ndisabled_systems = []" + region + "[states.A]", `unknown table "systems"`},
		{"[bot]\nactions_per_minute = 1" + region + "[states.A]", `[bot]: unknown key "actions_per_minute"`},
		{"[bot]\nactions_per_second = 0" + region + "[states.A]", "actions_per_second must be positive"},
		{region + "file = \"more.toml\"\n[states.A]", "names a file"},
		{region + "enabled_systems = [\"gold\"]\n[states.A]", `"enabled_systems" does nothing in a bot graph`},
		{region + `[states.A]
on_enter = [{ action = "EmitEvent", event = "EventGoldCancel" }]`, "a bot acts only through intents"},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "HasTarget", guard_args = { target = "glyph", whithin = 5 } }]`, `guard 'HasTarget': unknown key "whithin"`},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "HasTarget", guard_args = { target = "glyph", within = "5" } }]`, "within: cannot convert string to int"},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "ge", value = 5 } }]`, `op: "ge" is not one of eq, neq, gt, gte, lt, lte`},
		{region + `[states.A]
on_entr = [{ action = "TypeGlyph" }]`, `state 'A': unknown key "on_entr"`},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", gaurd = "Idle" }]`, `state 'A': unknown key "transitions.gaurd"`},
		{region + `[states.A]
on_enter = [{ action = "Text", payload = { text = "a" }, payload_vars = { text = "n" } }]`, "action 'Text' takes no payload_vars"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_nowhere" } }]`, `unknown intent action "motion_nowhere"`},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", cnt = 2 } }]`, `action 'Intent': payload: unknown key "cnt"`},
		{region + `[states.A]
on_update = [{ action = "PointAt", payload = { target = "treasure" } }]`, `unknown target "treasure"`},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "OnTarget", guard_args = { target = "random" } }]`, `unknown target "random"`},
		{region + `[states.A]
on_update = [{ action = "PointAt", payload = { target = "random", away = true } }]`, `unknown target "random"`},
		{region + `[states.A]
on_update = [{ action = "PointAt", payload = { target = "threat", away = true, fire = true } }]`, "fire and away do not combine"},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "InMode", guard_args = { mode = "replace" } }]`, `unknown mode "replace"`},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "mode_command" } }]`, "a bot issues no commands"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = ["motion_left", "config_menu"] } }]`, "a bot issues no commands"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", count = [] } }]`, "count range must be [min, max]"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", count = [9, 2] } }]`, "count range must be [min, max]"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = ["motion_left", 3] } }]`, "not an action name"},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "Not", guard_args = {} }]`, "guard 'Not': requires 'guard'"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", hold = 2 } }]`, "hold applies only to press"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "press", char = "a", hold = 0 } }]`, "hold must be a number of ticks or true"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "press", char = "a", hold = false } }]`, "hold must be a number of ticks or true"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "release" } }]`, `intent "release" requires one printable ASCII char`},
		{region + `[states.A]
on_update = [{ action = "PointAt", payload = { target = "glyph", key = "1" } }]`, "key applies only to the button target"},
		{region + `[states.A]
on_update = [{ action = "PointAt", payload = { target = "button", away = true } }]`, `unknown target "button"`},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "OnTarget", guard_args = { target = "button", key = "12" } }]`, "key must be one character"},
		{"[bot]\nauto_fire = \"both\"" + region + "[states.A]", `auto_fire "both" is not off, main or on`},
		{"[bot]\nround_seconds = -1" + region + "[states.A]", "round_seconds must not be negative"},
		{region + `[states.A]
on_update = [{ action = "Steer" }]`, `network "" is none of the graph's [network.<name>] tables`},
		{"[network.a]\ninputs = [\"column\"]\n[network.b]\ninputs = [\"column\"]" + region + `[states.A]
on_update = [{ action = "Steer" }]`, `network "" is none of the graph's`},
		{"[network.a]\ninputs = [\"column\"]" + region + `[states.A]
on_update = [{ action = "Steer", payload = { network = "b" } }]`, `network "b" is none of the graph's`},
		{"[bot]\nlearn = true" + region + "[states.A]", "learn needs a [network.<name>]"},
		{"[network]\ninputs = [\"column\"]" + region + "[states.A]", `[network]: "inputs" is no table: a network is named`},
		{"[network.\"../a\"]\ninputs = [\"column\"]" + region + "[states.A]", `[network]: name "../a" is not lowercase letters`},
		{"[network.a]\ninputs = [\"column\", \"radar\"]" + region + "[states.A]", `[network]: a: inputs: "radar" is no sensor`},
		{"[network.a]\ninputs = [\"column\"]\ny = { energy = 1.0 }" + region + "[states.A]", `[network]: a: y: weight "energy": not an input`},
		{"[network.a]\ninputs = [\"column\"]\nspecial = { column = 4.5 }" + region + "[states.A]", `[network]: a: special: weight "column" = 4.5`},
		{"[network.a]\ninputs = [\"column\"]\nx = { column = \"far\" }" + region + "[states.A]", `weight "column" = far: no number within 4 of zero`},
		{"[network.a]\ninputs = [\"column\"]\nhidden = 1\nx = { hidden = [1.0, 2.0] }" + region + "[states.A]", "x: hidden: wants a weight a hidden unit, 1 at most"},
		{"[network.a]\ninputs = [\"column\"]\nhidden = 1\nunits = [{ column = 1.0 }, { column = 1.0 }]" + region + "[states.A]", "units: 2 tables of weights for 1 hidden units"},
		{"[network.a]\ninputs = [\"column\"]\nhidden = 1\nunits = [{ hidden = [1.0] }]" + region + "[states.A]", `units[0]: weight "hidden": not an input`},
		{"[network.a]\ninputs = [\"column\"]\noutputs = [\"move\", \"jump\"]" + region + "[states.A]", `outputs: "jump" is no output`},
		{"[network.a]\ninputs = [\"column\"]\noutputs = [\"move\"]\nspecial = { bias = 1.0 }" + region + "[states.A]", "special: the network lists no output of that unit"},
		{"[bot]\ninputs = [\"column\"]\n[network.a]\ninputs = [\"column\"]" + region + "[states.A]", `inputs: "column" is no sensor, or is named twice`},
		{"[network.a]\ninputs = [\"column\"]\nwhile = { name = \"StateTimeExceeds\", args = { ms = 5 } }" + region + "[states.A]", "[network]: a: while: guard 'StateTimeExceeds': reads the state it guards"},
		{"[network.a]\ninputs = [\"column\"]\nwhile = { name = \"Own\", args = { field = \"entity\" } }" + region + "[states.A]", "sees no payload"},
		{"[network.a]\ninputs = [\"column\"]\nwhile = { name = \"InState\" }" + region + "[states.A]", "while: guard 'InState': requires 'region'"},
		{two + region + `[states.A]
on_update = [{ action = "Steer", payload = { network = ["a", "c"] } }]`, `network "c" is none of the graph's`},
		{two + region + `[states.A]
on_update = [{ action = "Steer", payload = { network = ["b", "b"] } }]`, `network "b" is listed twice`},
		{two + region + `[states.A]
on_update = [{ action = "Steer", payload = { network = ["a", "b"] } }]`, `network "b" never steers: "a" before it has no while`},
		{two + region + `[states.A]
on_update = [{ action = "Steer", payload = { network = 3 } }]`, "network must be a network's name or a list of them"},
	} {
		_, err := ParseGraph("broken", []byte(tt.doc))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("graph %q: error %v, want one naming %s", tt.doc, err, tt.want)
		}
	}
}

// soloWorld is an 80x24 instance whose own cursor stands at x, y and whose cave
// scrolls a column each tick
func soloWorld(x, y int) (*engine.World, *engine.GameContext) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	cursor := w.CreateEntity(core.DomainShared)
	w.Components.Cursor.SetComponent(cursor, component.CursorComponent{Slot: 0})
	w.Resources.Player.Bind(0, cursor)
	w.Resources.Player.SetLocal(0)
	w.Positions.SetPosition(cursor, component.PositionComponent{X: x, Y: y})
	w.Resources.Cave.Steps = 1 // its cave scrolls a column a tick, so a round travels its ticks
	return w, ctx
}

// place puts a new entity at x, y, set giving it its components
func place(w *engine.World, d core.Domain, x, y int, set func(core.Entity)) core.Entity {
	e := w.CreateEntity(d)
	set(e)
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	return e
}

// botFor drives a graph on ctx's instance, stepping it n times
func botFor(t *testing.T, ctx *engine.GameContext, doc string, n int) (*Driver, *recorder) {
	t.Helper()
	g, err := ParseGraph("test", []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	d, err := NewDriver(rec, ctx, g, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range n {
		if _, err := d.Step(); err != nil {
			t.Fatal(err)
		}
	}
	return d, rec
}

// recorder is an instance that keeps what reaches it and the tick it came at, and
// dispatches the next of dispatch on each tick
type recorder struct {
	got      []input.Intent
	at       []int
	ticks    int
	releases bool
	tap      func(event.GameEvent)
	dispatch []event.GameEvent
}

func (r *recorder) Tick(int) {
	r.ticks++
	if len(r.dispatch) > 0 {
		r.tap(r.dispatch[0])
		r.dispatch = r.dispatch[1:]
	}
}
func (r *recorder) SetDispatchTap(fn func(event.GameEvent)) { r.tap = fn }
func (r *recorder) InputTick() bool                         { return true }
func (r *recorder) ReportReleases()                         { r.releases = true }
func (r *recorder) Inject(intents ...*input.Intent) bool {
	for _, intent := range intents {
		r.got = append(r.got, *intent)
		r.at = append(r.at, r.ticks)
	}
	return true
}

// TestAGraphReleasesItsIntentsInOrderAtItsRate: ten a second is one every other
// 50 ms tick, however many one state queued at once.
func TestAGraphReleasesItsIntentsInOrderAtItsRate(t *testing.T) {
	g, err := ParseGraph("burst", []byte(`
[bot]
actions_per_second = 10

[regions.play]
initial = "A"

[states.A]
on_enter = [
    { action = "Intent", payload = { name = "motion_left", count = 1 } },
    { action = "Intent", payload = { name = "motion_left", count = 2 } },
    { action = "Intent", payload = { name = "motion_left", count = 3 } },
    { action = "Intent", payload = { name = "motion_left", count = 4 } },
]
`))
	if err != nil {
		t.Fatal(err)
	}
	_, ctx := soloWorld(0, 0)
	inst := &recorder{}
	d, err := NewDriver(inst, ctx, g, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for step := 1; step <= 10; step++ {
		if more, err := d.Step(); !more || err != nil {
			t.Fatalf("step %d: more %v, err %v", step, more, err)
		}
		if want := min(step/2, 4); len(inst.got) != want {
			t.Fatalf("after step %d released %d intents, want %d", step, len(inst.got), want)
		}
	}
	for i, intent := range inst.got {
		if intent.Count != i+1 {
			t.Fatalf("intent %d has count %d; the queue reordered them", i, intent.Count)
		}
	}
}

func TestDefaultSeeksRunStartsAndPatrolsForNuggetsWithoutText(t *testing.T) {
	w, ctx := soloWorld(12, 3)
	var glyphs []core.Entity
	for x := 5; x <= 9; x++ {
		e := w.CreateEntity(core.DomainPlayer)
		w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 'a', Type: component.GlyphGreen})
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: 3})
		glyphs = append(glyphs, e)
	}
	data, err := fs.ReadFile(asset.DefaultBots, "default.toml")
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParseGraph("default", data)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	d, err := NewDriver(rec, ctx, g, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	step := func(n int) {
		for range n {
			if _, err := d.Step(); err != nil {
				t.Fatal(err)
			}
		}
	}
	step(10)
	for _, intent := range rec.got {
		if intent.Type == input.IntentMouseMove && (intent.X != 5 || intent.Y != 3) {
			t.Fatalf("approached a run from its end: %+v", intent)
		}
	}
	if len(rec.got) == 0 {
		t.Fatal("did not approach text")
	}
	for _, e := range glyphs {
		w.DestroyEntity(e)
	}
	rec.got = nil
	step(100)
	if len(rec.got) == 0 {
		t.Fatal("stood still without glyphs")
	}
	nugget := w.CreateEntity(core.DomainShared)
	w.Components.Nugget.SetComponent(nugget, component.NuggetComponent{})
	w.Positions.SetPosition(nugget, component.PositionComponent{X: 60, Y: 10})
	rec.got = nil
	step(100)
	jump, err := input.IntentFor("nugget_jump", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range rec.got {
		if intent.Type == jump.Type {
			return
		}
	}
	t.Fatal("ignored a distant nugget between patrol movements")
}

// TestAPointerAimedPastTheViewportLandsOnTheNearestOpenCell: the router refuses a
// move onto a wall, so a goal clamped onto one lands on the open cell nearest it.
func TestAPointerAimedPastTheViewportLandsOnTheNearestOpenCell(t *testing.T) {
	w, ctx := soloWorld(10, 5)
	cfg := w.Resources.Config
	cfg.MapWidth = 200
	w.Positions.Resize(cfg.MapWidth, cfg.MapHeight)
	_, _, maxX, _, _ := cfg.VisibleMapRect()
	place(w, core.DomainPlayer, 150, 5, func(e core.Entity) {
		w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 'a', Type: component.GlyphGreen})
	})
	for y := 4; y <= 5; y++ {
		place(w, core.DomainShared, maxX, y, func(e core.Entity) {
			w.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: component.WallBlockCursor})
		})
	}
	_, rec := botFor(t, ctx, `
[regions.play]
initial = "A"

[states.A]
on_update = [{ action = "PointAt", payload = { target = "glyph" } }]
`, 4)
	if len(rec.got) == 0 {
		t.Fatal("pointed nowhere")
	}
	for _, intent := range rec.got {
		if intent.Type != input.IntentMouseMove || intent.X != maxX || intent.Y != 6 {
			t.Fatalf("pointed at %+v, want a move to the open edge cell (%d, 6)", intent, maxX)
		}
	}
}

// TestAGraphTransitionsOnEventsItsInstanceDispatchedLastStep: what the bot's own
// step dispatched is a present fact at its next update, and Own keeps another
// cursor's event from counting as the bot's.
func TestAGraphTransitionsOnEventsItsInstanceDispatchedLastStep(t *testing.T) {
	w, ctx := soloWorld(0, 0)
	d, rec := botFor(t, ctx, `
[regions.play]
initial = "A"

[states.A]
transitions = [{ trigger = "EventCursorDefeatState", target = "B", guard = "Own", guard_args = { field = "entity" } }]

[states.B]
on_enter = [{ action = "Intent", payload = { name = "motion_left" } }]
`, 0)
	defeat := func(e core.Entity) event.GameEvent {
		return event.GameEvent{Type: event.EventCursorDefeatState,
			Payload: &event.CursorDefeatStatePayload{Entity: e, Defeated: true}}
	}
	own := w.Resources.Player.Entity
	rec.dispatch = []event.GameEvent{defeat(own + 1), defeat(own)}
	for step, want := range []int{0, 0, 1} {
		if _, err := d.Step(); err != nil {
			t.Fatal(err)
		}
		if len(rec.got) != want {
			t.Fatalf("after step %d released %d intents, want %d", step+1, len(rec.got), want)
		}
	}
}

// TestEvasionGainsGroundOnTheNearestThreat: away aims at the nearest hostile thing's
// mirror through the cursor, a friendly bullet is no threat, and a cursor pinned to
// the view's edge in line with the threat still moves farther from it.
func TestEvasionGainsGroundOnTheNearestThreat(t *testing.T) {
	for _, c := range []struct{ cursorX, threatX int }{{40, 35}, {0, 3}} {
		w, ctx := soloWorld(c.cursorX, 10)
		place(w, core.DomainShared, c.threatX, 10, func(e core.Entity) {
			w.Components.Bullet.SetComponent(e, component.BulletComponent{Hostile: true})
		})
		place(w, core.DomainShared, c.cursorX+2, 10, func(e core.Entity) {
			w.Components.Bullet.SetComponent(e, component.BulletComponent{})
		})
		_, rec := botFor(t, ctx, `
[regions.play]
initial = "A"

[states.A]
on_update = [{ action = "PointAt", payload = { target = "threat", away = true } }]
`, 3)
		if len(rec.got) == 0 {
			t.Fatalf("cursor at %d did not evade a threat at %d", c.cursorX, c.threatX)
		}
		for _, intent := range rec.got {
			if intent.Type != input.IntentMouseMove || dist2(intent.X-c.threatX, intent.Y-10) <= dist2(c.cursorX-c.threatX, 0) {
				t.Fatalf("cursor at %d evaded a threat at %d to %+v, no farther from it", c.cursorX, c.threatX, intent)
			}
		}
	}
}

// TestAGraphWaitingOnIdleNeverDrops: Intent queues whatever the rate, so a graph
// that waits for its queue to empty is one that loses nothing.
func TestAGraphWaitingOnIdleNeverDrops(t *testing.T) {
	_, ctx := soloWorld(0, 0)
	d, _ := botFor(t, ctx, `
[bot]
actions_per_second = 1

[regions.play]
initial = "A"

[states.A]
on_update = [{ action = "Intent", payload = { name = "motion_left" }, guard = "Idle" }]
`, 400)
	if st := d.Stats(); st.Injected == 0 || st.Dropped != 0 {
		t.Fatalf("released %d and dropped %d, want some released and none dropped", st.Injected, st.Dropped)
	}
}

// TestABotIsWeighedOnlyOnItsOwnEvents: a bot is weighed on what its own steps dispatched
// for the cursor it drives, a predicted kill only once confirmed.
func TestABotIsWeighedOnlyOnItsOwnEvents(t *testing.T) {
	w, ctx := soloWorld(0, 0)
	d, rec := botFor(t, ctx, "[regions.play]\ninitial = \"A\"\n[states.A]\n", 0)
	own := w.Resources.Player.Entity
	killed := &event.SpeciesKilledPayload{KillerEntity: own}
	rec.dispatch = []event.GameEvent{
		{Type: event.EventBoostReward, Payload: &event.BoostRewardPayload{Entity: own}},
		{Type: event.EventBoostReward, Payload: &event.BoostRewardPayload{Entity: own + 1}},
		{Type: event.EventSpeciesKilled, Payload: killed, Phase: event.PhasePredicted},
		{Type: event.EventSpeciesKillConfirmed, Payload: killed},
		{Type: event.EventCursorDefeatState, Payload: &event.CursorDefeatStatePayload{Entity: own}},
		{Type: event.EventCursorDefeatState, Payload: &event.CursorDefeatStatePayload{Entity: own, Defeated: true}},
	}
	step := func() {
		for range rec.dispatch {
			if _, err := d.Step(); err != nil {
				t.Fatal(err)
			}
		}
	}
	step()
	rec.tap(event.GameEvent{Type: event.EventBoostReward, Payload: &event.BoostRewardPayload{Entity: own}})
	// A joiner's Player.Entity names another participant's cursor until its slot binds.
	w.Components.Cursor.SetComponent(own, component.CursorComponent{Control: component.ControlRemote})
	rec.dispatch = []event.GameEvent{{Type: event.EventBoostReward, Payload: &event.BoostRewardPayload{Entity: own}}}
	step()
	weight := func(t event.EventType) (sum int64) {
		for _, row := range fitness {
			if row.event == t {
				sum += row.weight
			}
		}
		return sum
	}
	want := weight(event.EventBoostReward) + weight(event.EventSpeciesKillConfirmed) + weight(event.EventCursorDefeatState)
	if got := d.Stats().Fitness; got != want {
		t.Fatalf("fitness %d, want %d", got, want)
	}
}

// TestABotLetsGoOfEveryKeyItPresses: the instance takes the bot's keys as releasing,
// and the bot lets go of each press itself: a tap at once, a hold when its ticks are
// up, an open hold at its release, and a held key before pressing it again. A
// release of a key not held sends nothing, and a press with its release is one action.
func TestABotLetsGoOfEveryKeyItPresses(t *testing.T) {
	sent := func(rec *recorder) []string {
		var out []string
		for i, intent := range rec.got {
			out = append(out, fmt.Sprintf("%d %s %c", rec.at[i], intent.Type, intent.Char))
		}
		return out
	}
	_, ctx := soloWorld(0, 0)
	_, rec := botFor(t, ctx, `
[bot]
actions_per_second = 1000

[regions.play]
initial = "A"

[states.A]
on_enter = [
    { action = "Intent", payload = { name = "press", char = "a", hold = 3 } },
    { action = "Intent", payload = { name = "press", char = "b" } },
    { action = "Intent", payload = { name = "press", char = "c", hold = true } },
    { action = "Intent", payload = { name = "press", char = "d", hold = 20 } },
]
transitions = [{ trigger = "Tick", target = "B", guard = "StateTimeExceeds", guard_args = { ms = 200 } }]

[states.B]
on_enter = [
    { action = "Intent", payload = { name = "release", char = "c" } },
    { action = "Intent", payload = { name = "release", char = "e" } },
    { action = "Intent", payload = { name = "press", char = "d", hold = 2 } },
]
`, 12)
	if !rec.releases {
		t.Fatal("the bot's instance was not asked to take its keys as releasing")
	}
	if got, want := sent(rec), []string{
		"0 IntentKeyPress a", "0 IntentKeyPress b", "0 IntentKeyRelease b", "0 IntentKeyPress c", "0 IntentKeyPress d",
		"3 IntentKeyRelease a",
		"4 IntentKeyRelease c", "4 IntentKeyRelease d", "4 IntentKeyPress d",
		"6 IntentKeyRelease d",
	}; !slices.Equal(got, want) {
		t.Fatalf("sent %q, want %q", got, want)
	}

	_, rec = botFor(t, ctx, `
[bot]
actions_per_second = 20

[regions.play]
initial = "A"

[states.A]
on_enter = [
    { action = "Intent", payload = { name = "press", char = "a" } },
    { action = "Intent", payload = { name = "press", char = "b" } },
]
`, 3)
	if got, want := sent(rec), []string{"0 IntentKeyPress a", "0 IntentKeyRelease a", "1 IntentKeyPress b", "1 IntentKeyRelease b"}; !slices.Equal(got, want) {
		t.Fatalf("at one action a tick sent %q, want %q", got, want)
	}
}

// TestABotSightsAndClicksButtons: a button is sighted by its cell nearest the cursor,
// a key picking one; a click presses a shown cell of it wherever the cursor is, and
// is no click while the view shows none, which a move heads for. On a map with no
// cursor a click takes the leftmost button's middle, a move goes nowhere, and no
// button is within reach or under the cursor.
func TestABotSightsAndClicksButtons(t *testing.T) {
	down := func(x, y int) input.Intent {
		return input.Intent{Type: input.IntentMouseLeftDown, X: x, Y: y, MapCell: true}
	}
	up := input.Intent{Type: input.IntentMouseLeftUp}
	move := func(x, y int) input.Intent {
		return input.Intent{Type: input.IntentMouseMove, X: x, Y: y, MapCell: true}
	}
	// On 79x21, three buttons span x 13-28, 31-46 and 49-64, y 5-14; on 200 columns
	// the first starts at x 74 and the third at 110, past the view's edge at 78
	world := func(cursor bool, mapW int) *engine.GameContext {
		w, ctx := soloWorld(10, 2)
		if !cursor {
			w.Positions.RemoveEntity(w.Resources.Player.Entity)
		}
		cfg := w.Resources.Config
		cfg.MapWidth = mapW
		w.Positions.Resize(cfg.MapWidth, cfg.MapHeight)
		for slot := 1; slot <= 3; slot++ {
			e := w.CreateEntity(core.DomainShared)
			w.Components.Button.SetComponent(e, component.ButtonComponent{Slot: slot, Key: rune('0' + slot)})
		}
		return ctx
	}
	point := func(payload string) string {
		return "[bot]\nactions_per_second = 1000\n[regions.play]\ninitial = \"A\"\n[states.A]\n" +
			"on_update = [{ action = \"PointAt\", payload = " + payload + " }]\n"
	}
	for _, c := range []struct {
		name    string
		cursor  bool
		mapW    int
		payload string
		want    []input.Intent
	}{
		{"a click on a key's button", true, 79, `{ target = "button", key = "2", fire = true }`, []input.Intent{down(31, 5), up}},
		{"a click on the nearest", true, 79, `{ target = "button", fire = true }`, []input.Intent{down(13, 5), up}},
		{"a move onto it", true, 79, `{ target = "button", key = "3" }`, []input.Intent{move(49, 5)}},
		{"a click on its shown part", true, 200, `{ target = "button", key = "1", fire = true }`, []input.Intent{down(74, 5), up}},
		{"no click on one unshown", true, 200, `{ target = "button", key = "3", fire = true }`, nil},
		{"a move toward one unshown", true, 200, `{ target = "button", key = "3" }`, []input.Intent{move(78, 5)}},
		{"a click with no cursor", false, 79, `{ target = "button", fire = true }`, []input.Intent{down(20, 9), up}},
		{"no move with no cursor", false, 79, `{ target = "button" }`, nil},
	} {
		_, rec := botFor(t, world(c.cursor, c.mapW), point(c.payload), 2)
		if !slices.Equal(rec.got, c.want) {
			t.Errorf("%s: sent %+v, want %+v", c.name, rec.got, c.want)
		}
	}

	passes := func(ctx *engine.GameContext, guard, args string) bool {
		_, rec := botFor(t, ctx, `
[regions.play]
initial = "A"

[states.A]
transitions = [{ trigger = "Tick", target = "B", guard = "`+guard+`", guard_args = `+args+` }]

[states.B]
on_enter = [{ action = "Intent", payload = { name = "motion_left" } }]
`, 3)
		return len(rec.got) > 0
	}
	on := world(true, 79)
	on.World.Positions.SetPosition(on.World.Resources.Player.Entity, component.PositionComponent{X: 35, Y: 7})
	for _, c := range []struct {
		ctx         *engine.GameContext
		guard, args string
		want        bool
	}{
		{on, "OnTarget", `{ target = "button", key = "2" }`, true},
		{on, "OnTarget", `{ target = "button", key = "1" }`, false},
		{on, "HasTarget", `{ target = "button", key = "1", within = 3 }`, false},
		{on, "HasTarget", `{ target = "button", key = "1", within = 8 }`, true},
		{on, "HasTarget", `{ target = "button", key = "9" }`, false},
		{world(false, 79), "HasTarget", `{ target = "button" }`, true},
		{world(false, 79), "HasTarget", `{ target = "button", within = 100 }`, false},
		{world(false, 79), "OnTarget", `{ target = "button" }`, false},
	} {
		if got := passes(c.ctx, c.guard, c.args); got != c.want {
			t.Errorf("%s %s with the cursor %s: passes %v, want %v", c.guard, c.args,
				map[bool]string{true: "at 35,7", false: "off the map"}[c.ctx == on], got, c.want)
		}
	}
}

// steering is a graph whose one state steers by its one network, fly, of the given
// table
func steering(network string) string {
	return "[bot]\nactions_per_second = 20\n[network.fly]\n" + network + `
[regions.play]
initial = "A"

[states.A]
on_update = [{ action = "Steer" }]
`
}

// TestSteerPointsAtTheCellItsNetworksOutputsName: a sensor reads an offset from the
// cursor in view widths and heights, and Steer moves by its outputs in the same,
// so a direct weight of one lands the pointer on what the sensor sighted.
func TestSteerPointsAtTheCellItsNetworksOutputsName(t *testing.T) {
	w, ctx := soloWorld(10, 2)
	place(w, core.DomainShared, 60, 16, func(e core.Entity) {
		w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm})
	})
	_, rec := botFor(t, ctx, steering(`inputs = ["species_x", "species_y"]
hidden = 2
x = { species_x = 1.0 }
y = { species_y = 1.0 }`), 2)
	want := []input.Intent{{Type: input.IntentMouseMove, X: 60, Y: 16, MapCell: true}}
	if !slices.Equal(rec.got, want) {
		t.Fatalf("steered %+v, want one move onto the species at 60,16, most of the view away", rec.got)
	}
}

// TestANetworkAddsItsHiddenUnitsToItsDirectTerm: an output's unit is its direct
// weights over the inputs, its weights over the hidden units' tanh, and its bias,
// and a graph writes them all: a unit's by input name, hidden and bias, and each
// hidden unit's as a table of units, none of which an input's name may be.
func TestANetworkAddsItsHiddenUnitsToItsDirectTerm(t *testing.T) {
	g, err := ParseGraph("net", []byte(steering(`inputs = ["column", "energy"]
hidden = 1
units = [{ column = 1.0, energy = -2, bias = 0.5 }]
x = { column = 2.0, bias = 0.25, hidden = [3.0] }
y = { energy = -1.0, hidden = [-1] }
special = { column = 1.0, bias = -0.5, hidden = [2.0] }`)))
	if err != nil {
		t.Fatal(err)
	}
	n := g.nets[0].playing()
	n.in[0], n.in[1] = 0.5, 0.25
	out := n.pass()
	unit := math.Tanh(1*0.5 - 2*0.25 + 0.5)
	for o, want := range []float64{2*0.5 + 3*unit + 0.25, -0.25 - unit, 0.5 + 2*unit - 0.5} {
		if math.Abs(out[o]-want) > 1e-12 {
			t.Fatalf("unit %s is %g, want %g", n.units[o], out[o], want)
		}
	}
	if len(n.genes) != 1*(2+1)+3*(2+1+1) {
		t.Fatalf("%d genes for two inputs, one hidden unit and three output units", len(n.genes))
	}
	for _, s := range sensors {
		if s.name == biasName || s.name == hiddenName {
			t.Fatalf("sensor %s takes the name a table of weights gives its %s", s.name, s.name)
		}
	}
}

// TestALayoutIsWhatItsNetworkReadsHidesAndAsksFor: a network reads the document's
// inputs ahead of its own, and an output it does not list has no unit among its
// genes; a population is kept under the digest of all three, which names the
// outputs only where one is left out.
func TestALayoutIsWhatItsNetworkReadsHidesAndAsksFor(t *testing.T) {
	layout := func(bot, table string) *network {
		t.Helper()
		g, err := ParseGraph("net", []byte(strings.Replace(steering(table), "[bot]\n", "[bot]\n"+bot+"\n", 1)))
		if err != nil {
			t.Fatal(err)
		}
		return g.nets[0]
	}
	own := layout("", `inputs = ["column", "energy"]`)
	shared := layout(`inputs = ["column"]`, "inputs = [\"energy\"]\ny = { column = 1.5 }")
	every := layout("", "inputs = [\"column\", \"energy\"]\noutputs = [\"move\", \"special\"]")
	move := layout("", "inputs = [\"column\", \"energy\"]\noutputs = [\"move\"]")
	if shared.digest != own.digest || every.digest != own.digest || move.digest == own.digest {
		t.Fatalf("layouts %s as listed, %s with an input shared, %s with every output listed and %s with move alone: want the first three one layout and the last another",
			own.digest, shared.digest, every.digest, move.digest)
	}
	if got := shared.genes[shared.output(1)]; got != 1.5 {
		t.Fatalf("y's weight on the shared column is %g, want the 1.5 written", got)
	}
	if !slices.Equal(own.units, []string{"x", "y", "special"}) || !slices.Equal(move.units, []string{"x", "y"}) || len(move.genes) != 2*(2+1) {
		t.Fatalf("units %v of every output and %v of move alone, in %d genes", own.units, move.units, len(move.genes))
	}
}

// TestSteerPlaysTheFirstNetworkItListsWhoseWhileHolds: a Steer over a list asks
// each network's while in turn, once an update, and plays the first that holds,
// one with none always; InState holds while the scenario's region is in a state
// it names.
func TestSteerPlaysTheFirstNetworkItListsWhoseWhileHolds(t *testing.T) {
	w, ctx := soloWorld(10, 2)
	fight, asked := false, 0
	w.Resources.Scenario.Bind(nil, func(region string, states []string) bool {
		asked++
		return fight && region == "boss" && slices.Equal(states, []string{"BossFight"})
	})
	d, rec := botFor(t, ctx, `[bot]
actions_per_second = 20

[network.fly]
inputs = ["column"]
x = { bias = 0.1 }

[network.kraken]
inputs = ["column"]
x = { bias = -0.1 }
while = { name = "InState", args = { region = "boss", states = ["BossFight"] } }

[regions.play]
initial = "A"

[states.A]
on_update = [{ action = "Steer", payload = { network = ["kraken", "fly"] } }]
`, 2)
	fight = true
	if _, err := d.Step(); err != nil {
		t.Fatal(err)
	}
	minX, _, maxX, _, _ := ctx.World.Resources.Config.VisibleMapRect()
	reach := int(math.Round(0.1 * float64(maxX-minX+1)))
	want := []input.Intent{{Type: input.IntentMouseMove, X: 10 + reach, Y: 2, MapCell: true}, {Type: input.IntentMouseMove, X: 10 - reach, Y: 2, MapCell: true}}
	if !slices.Equal(rec.got, want) || asked != 2 {
		t.Fatalf("steered %+v asking the scenario %d times, want fly's move right, then kraken's left once its state came, asked once an update", rec.got, asked)
	}
}

// TestANetworkAsksForTheSpecialAttackWhileItsOutputIsPositive: a positive special
// asks for the special attack ahead of the move, as often as auto-fire would, and
// takes the update's one intent where the rate leaves no second; at zero, as with
// no weights, it asks for none.
func TestANetworkAsksForTheSpecialAttackWhileItsOutputIsPositive(t *testing.T) {
	for _, c := range []struct {
		rate  int
		bias  float64
		asked []int // the ticks the special attack was asked for at
		moves int
	}{{40, 0.5, []int{1, 6}, 6}, {20, 0.5, []int{1, 6}, 5}, {40, 0, nil, 6}} {
		_, ctx := soloWorld(10, 2)
		doc := strings.Replace(steering(fmt.Sprintf("inputs = [\"column\"]\nx = { bias = 0.1 }\nspecial = { bias = %g }", c.bias)),
			"actions_per_second = 20", fmt.Sprintf("actions_per_second = %d", c.rate), 1)
		_, rec := botFor(t, ctx, doc, 7) // the first step starts the graph and steers nothing
		var asked []int
		moves := 0
		for i, intent := range rec.got {
			switch intent.Type {
			case input.IntentFireSpecial:
				asked = append(asked, rec.at[i])
			case input.IntentMouseMove:
				moves++
			}
		}
		if !slices.Equal(asked, c.asked) || moves != c.moves {
			t.Errorf("at %d a second with special at %g: asked at ticks %v with %d moves, want %v and %d",
				c.rate, c.bias, asked, moves, c.asked, c.moves)
		}
	}
}

// TestARoundScoresHowFarItTravelledAndWhatItEarned: a new game closes a round,
// scored by the columns its cave scrolled and its events' worth, a data species'
// kill its hit points. Its end weighs once on the fitness alone, a crush and the
// defeat it brings together, as one ends every round.
func TestARoundScoresHowFarItTravelledAndWhatItEarned(t *testing.T) {
	w, ctx := soloWorld(0, 0)
	w.Resources.Cave.Steps = 2
	w.Resources.Behaviors = []component.BehaviorPrototype{{HitPoints: 28}}
	d, rec := botFor(t, ctx, "[regions.play]\ninitial = \"A\"\n[states.A]\n", 0)
	own := w.Resources.Player.Entity
	rec.dispatch = []event.GameEvent{
		{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: own, Species: component.SpeciesDataFirst}},
		{Type: event.EventCursorCrushed, Payload: &event.CursorCrushedPayload{Entity: own}},
		{Type: event.EventCursorDefeatState, Payload: &event.CursorDefeatStatePayload{Entity: own, Defeated: true}},
		{Type: event.EventGameResetRequest, Payload: &event.GameResetPayload{}},
		{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: own}},
	}
	for range 5 {
		if _, err := d.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if st := d.Stats(); st.Rounds != 1 || st.Score != 4*2+28 || st.Fitness != 28-100+5 {
		t.Fatalf("rounds %d, score %g, fitness %d: want one round of 4 ticks at two columns each and a kill worth 28, and a fitness of %d",
			st.Rounds, st.Score, st.Fitness, 28-100+5)
	}
}

// memory is a population store that lasts a test
type memory struct{ doc []byte }

func (m *memory) Load() ([]byte, error)  { return m.doc, nil }
func (m *memory) Store(doc []byte) error { m.doc = doc; return nil }

// keeping is a store holding members as a population of layout, past the rounds
// that draw its first genomes at random
func keeping(t *testing.T, layout string, members ...member) *memory {
	t.Helper()
	doc, err := json.Marshal(population{Version: populationVersion, Layout: layout, Rounds: learnPool, Members: members})
	if err != nil {
		t.Fatal(err)
	}
	return &memory{doc: doc}
}

// learning is a learning graph, a driver on it and the instance it plays
func learning(t *testing.T) (*Graph, *Driver, *recorder) {
	t.Helper()
	doc := strings.Replace(steering("inputs = [\"column\", \"energy\"]\nhidden = 2\nx = { column = -0.5 }"), "[bot]", "[bot]\nlearn = true", 1)
	g, err := ParseGraph("net", []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	_, ctx := soloWorld(0, 0)
	rec := &recorder{}
	d, err := NewDriver(rec, ctx, g, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return g, d, rec
}

// TestALearnerScoresTheRoundsItsBotPlayedOutAlone: a candidate plays a round, the
// hand-written genome first, and is scored as the game ends it. A round an operator
// ended scores nothing, and a bot not alone plays what the learner fields.
func TestALearnerScoresTheRoundsItsBotPlayedOutAlone(t *testing.T) {
	g, d, rec := learning(t)
	l, err := NewLearner(g, "fly", 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	alone := true
	Learn(func() bool { return alone }, []*Learner{l}, d)
	// A round is two ticks: the network steers in the first and the game ends the next
	round := func(origin event.Origin) []float64 {
		rec.dispatch = []event.GameEvent{{}, {Type: event.EventGameResetRequest, Payload: &event.GameResetPayload{}, Origin: origin}}
		for range 2 {
			if _, err := d.Step(); err != nil {
				t.Fatal(err)
			}
		}
		return d.mind.nets[0].genes
	}
	scored := func() int { return len(l.engine.Snapshot().Members) }
	written := g.nets[0].genes

	if !slices.Equal(d.mind.nets[0].genes, written) {
		t.Fatal("the first round does not play the hand-written genome")
	}
	if next := round(event.OriginSystem); scored() != 1 || slices.Equal(next, written) {
		t.Fatalf("a round played out scored %d genomes and handed on %v, want the hand-written one scored and a child of it next", scored(), next)
	}
	if round(event.OriginCommand); scored() != 1 {
		t.Fatalf("a round an operator ended left %d genomes scored, want it to score nothing", scored())
	}
	alone = false
	if next := round(event.OriginSystem); scored() != 1 || !slices.Equal(next, written) {
		t.Fatalf("a bot not alone scored %d genomes and plays %v, want nothing scored and the genome fielded", scored(), next)
	}
}

// TestALearnerKeepsItsPopulationByItsLayout: a population kept under the layout's
// digest comes back with its rounds, and the best member its rounds have proved is
// the one fielded, not the best scored; one of another layout, or none, starts
// over from the hand-written genome. A save says each member's rounds and birth.
func TestALearnerKeepsItsPopulationByItsLayout(t *testing.T) {
	g, d, _ := learning(t)
	net := g.nets[0]
	genome := func(v float64) []float64 { return slices.Repeat([]float64{v}, len(net.genes)) }
	kept := func(layout string) *memory {
		return keeping(t, layout,
			member{Genes: genome(0.1), Score: 50, Rounds: learnProven - 1, Born: 3},
			member{Genes: genome(0.2), Score: 40, Rounds: learnProven, Born: 5},
			member{Genes: genome(0.3), Score: 10, Rounds: learnProven + 4, Born: 7})
	}
	for name, c := range map[string]struct {
		store Store
		want  []float64
	}{
		"kept":    {kept(net.digest), genome(0.2)},
		"another": {kept("another layout"), net.genes},
		"none":    {&memory{}, net.genes},
	} {
		l, err := NewLearner(g, "fly", 1, 1, c.store)
		if err != nil {
			t.Fatal(err)
		}
		Learn(func() bool { return false }, []*Learner{l}, d)
		if got := d.mind.nets[0].genes; !slices.Equal(got, c.want) {
			t.Errorf("%s: fields %v, want %v", name, got[:2], c.want[:2])
		}
	}

	store := kept(net.digest)
	l, err := NewLearner(g, "fly", 1, 1, store)
	if err != nil {
		t.Fatal(err)
	}
	l.take(0, true)
	l.settle(0, 45, true)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	var saved population
	if err := json.Unmarshal(store.doc, &saved); err != nil {
		t.Fatal(err)
	}
	entered := slices.IndexFunc(saved.Members, func(m member) bool { return m.Score == 45 })
	if saved.Layout != net.digest || saved.Rounds != learnPool+1 || len(saved.Members) != 4 || entered < 0 ||
		saved.Members[entered].Born != learnPool+1 || saved.Members[entered].Rounds != 1 || saved.Fielded != 2 || saved.Members[2].Born != 5 {
		t.Fatalf("saved %+v: want the three kept with their births, the genome scored 45 born at round 1, and the proved member fielded",
			saved)
	}
}

// failing is a population store whose load or save fails with the error it is
// given, counting the saves asked of it
type failing struct {
	memory
	load, save error
	saves      int
}

func (f *failing) Load() ([]byte, error) {
	if f.load != nil {
		return nil, f.load
	}
	return f.memory.Load()
}

func (f *failing) Store(doc []byte) error {
	if f.saves++; f.save != nil {
		return f.save
	}
	return f.memory.Store(doc)
}

// TestALearnerItsFileFailsLearnsOnAndSaysSoOnce: a population that cannot be read
// is left as it is and the run keeps none, which its end reports; one that cannot
// be parsed starts over and the next save replaces it. Either is said once.
func TestALearnerItsFileFailsLearnsOnAndSaysSoOnce(t *testing.T) {
	g, _, _ := learning(t)
	for name, c := range map[string]struct {
		store *failing
		kept  bool
	}{
		"unreadable": {&failing{load: errors.New("denied")}, false},
		"unparsable": {&failing{memory: memory{doc: []byte("{")}}, true},
	} {
		l, err := NewLearner(g, "fly", 1, 1, c.store)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		said := 0
		l.Lost = func(error) { said++ }
		for range 3 {
			l.take(0, true)
			l.settle(0, 45, true)
		}
		err = l.Close()
		var saved population
		replaced := json.Unmarshal(c.store.doc, &saved) == nil && saved.Rounds == 3
		if said != 1 || l.rounds != 3 || (err == nil) != c.kept || replaced != c.kept || (c.store.saves > 0) != c.kept {
			t.Errorf("%s: said %d times, %d rounds scored, %d saves, replaced %t, closing with %v: want it said once, three rounds, and kept %t",
				name, said, l.rounds, c.store.saves, replaced, err, c.kept)
		}
	}
}

// TestALearnerItsSaveFailsLearnsOnAndSaysSoOnce: a save that fails is said once, not
// those failing after it, the rounds go on being scored, and the run's end reports
// it unless a later save kept the population.
func TestALearnerItsSaveFailsLearnsOnAndSaysSoOnce(t *testing.T) {
	g, _, _ := learning(t)
	store := &failing{save: errors.New("full")}
	l, err := NewLearner(g, "fly", 1, 1, store)
	if err != nil {
		t.Fatal(err)
	}
	said := 0
	l.Lost = func(error) { said++ }
	for range 3 {
		l.take(0, true)
		l.settle(0, 45, true)
		l.save()
	}
	if err := l.Close(); said != 1 || l.rounds != 3 || store.saves != 4 || err == nil {
		t.Fatalf("said %d times over %d failed saves, %d rounds scored, closing with %v: want it said once, three rounds and the failure reported",
			said, store.saves, l.rounds, err)
	}
	store.save = nil
	if err := l.Close(); err != nil || len(store.doc) == 0 {
		t.Fatalf("a save that works again closes with %v and keeps %d bytes, want the population kept", err, len(store.doc))
	}
}

// TestEveryThirdCandidateIsTheBestMemberYetToBeProved: a member's score is a guess
// until its rounds prove it, so the repeats go to the best one short of them,
// which a run fields once it has them.
func TestEveryThirdCandidateIsTheBestMemberYetToBeProved(t *testing.T) {
	g, _, _ := learning(t)
	net := g.nets[0]
	genome := func(v float64) []float64 { return slices.Repeat([]float64{v}, len(net.genes)) }
	l, err := NewLearner(g, "fly", 1, 1, keeping(t, net.digest,
		member{Genes: genome(0.1), Score: 500, Rounds: learnProven - 2},
		member{Genes: genome(0.2), Score: 40, Rounds: learnProven},
		member{Genes: genome(0.3), Score: 30, Rounds: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if fielded := l.take(0, false); !slices.Equal(fielded, genome(0.2)) {
		t.Fatalf("fields %v, want the member its rounds have proved", fielded[:1])
	}
	// Two repeats bring the best member's rounds to learnProven, or one and a child
	// bred its copy
	for i := range 2 * learnRepeat {
		if candidate := l.take(0, true); i+1 == learnRepeat && !slices.Equal(candidate, genome(0.1)) {
			t.Fatalf("candidate %d is %v, want the best member short of its rounds", i+1, candidate[:1])
		}
		l.settle(0, 20, true)
	}
	if fielded := l.take(0, false); !slices.Equal(fielded, genome(0.1)) {
		t.Fatalf("with those rounds fields %v, want the member they proved, still the best", fielded[:1])
	}
}

// TestACandidateThatWouldEnterAFullArchiveIsScoredByItsNextRound: a first round
// good enough to enter only qualifies its genome, which plays again and enters,
// or not, on that round alone, as the first was picked for its luck. The
// hand-written genome plays first, whatever the store kept.
func TestACandidateThatWouldEnterAFullArchiveIsScoredByItsNextRound(t *testing.T) {
	g, _, _ := learning(t)
	net := g.nets[0]
	// played is the archive after the hand-written genome's rounds score as given,
	// on a store whose population fills it
	played := func(scores ...float64) (first, last []float64, held func(float64) bool) {
		var full []member
		for i := range learnPool {
			full = append(full, member{Genes: slices.Repeat([]float64{float64(i+1) / 100}, len(net.genes)), Score: float64(200 - i), Rounds: 1})
		}
		l, err := NewLearner(g, "fly", 1, 1, keeping(t, net.digest, full...))
		if err != nil {
			t.Fatal(err)
		}
		first = l.take(0, true)
		for _, score := range scores {
			l.settle(0, score, true)
			last = l.take(0, true)
		}
		if st := l.Stats(); st.Scored != len(scores) {
			t.Fatalf("%d rounds scored of %d played", st.Scored, len(scores))
		}
		return first, last, func(score float64) bool {
			return slices.ContainsFunc(l.engine.Snapshot().Members, func(c genetic.Candidate[[]float64, float64]) bool { return c.Score == score })
		}
	}

	first, again, held := played(500)
	if !slices.Equal(first, net.genes) {
		t.Fatal("the run's first round does not play the hand-written genome")
	}
	if !slices.Equal(again, first) || held(500) {
		t.Fatal("a first round of 500 against an archive of 169 to 200 entered at once: it only qualifies its genome for another round")
	}
	if _, next, held := played(500, 300); slices.Equal(next, first) || !held(300) || held(500) || held(400) {
		t.Fatal("a genome that qualified on 500 and then scored 300 is not held at 300 alone")
	}
	if _, _, held := played(500, 60); held(60) || held(500) || held(280) {
		t.Fatal("a genome that qualified on 500 and then scored 60 entered")
	}
	if _, next, held := played(100); slices.Equal(next, first) || held(100) {
		t.Fatal("a first round under the archive's worst did not close its evaluation unentered")
	}
}

// TestARoundInFlightCountsTowardAMembersProof: games that learn together draw from
// one count of candidates, and a repeat goes to the best member whose rounds,
// those other games are playing among them, are short of its proof.
func TestARoundInFlightCountsTowardAMembersProof(t *testing.T) {
	g, _, _ := learning(t)
	net := g.nets[0]
	genome := func(v float64) []float64 { return slices.Repeat([]float64{v}, len(net.genes)) }
	l, err := NewLearner(g, "fly", 1, learnRepeat+1, keeping(t, net.digest,
		member{Genes: genome(0.1), Score: 500, Rounds: learnProven - 1},
		member{Genes: genome(0.2), Score: 40, Rounds: 1}))
	if err != nil {
		t.Fatal(err)
	}
	l.take(0, true)
	l.settle(0, 1, true) // the hand-written genome, scored under both
	l.drawn = learnRepeat - 1
	first := l.take(0, true)
	for game := 1; game < learnRepeat; game++ {
		l.take(game, true)
	}
	if next := l.take(learnRepeat, true); !slices.Equal(first, genome(0.1)) || !slices.Equal(next, genome(0.2)) {
		t.Fatalf("the repeats are %v and %v, want the best member, a round short of its proof, and then the next best, as that round is in play",
			first[:1], next[:1])
	}
}

// TestANewPopulationsFirstRoundsPlayGenomesDrawnAtRandom: a population with no
// rounds behind it has nothing to breed from, so its first learnPool rounds play
// genomes drawn about the hand-written one, zero where a graph writes none, each
// its own; one past them breeds, and a graph that writes no weight tries none.
func TestANewPopulationsFirstRoundsPlayGenomesDrawnAtRandom(t *testing.T) {
	g, err := ParseGraph("net", []byte(strings.Replace(steering("inputs = [\"column\", \"energy\"]\nhidden = 2"), "[bot]", "[bot]\nlearn = true", 1)))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewLearner(g, "fly", 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	var drawn [][]float64
	for range learnPool {
		genes := l.take(0, true)
		if !slices.ContainsFunc(genes, func(w float64) bool { return w != 0 }) || slices.ContainsFunc(drawn, func(d []float64) bool { return slices.Equal(d, genes) }) {
			t.Fatalf("round %d plays %v, want a genome of its own drawn at random", len(drawn)+1, genes[:3])
		}
		drawn = append(drawn, slices.Clone(genes))
		l.settle(0, float64(len(drawn)), true)
	}
	if members := l.engine.Snapshot().Members; len(members) != learnPool {
		t.Fatalf("%d members after %d rounds, want each genome drawn scored into the archive", len(members), learnPool)
	}
	bred := l.take(0, true)
	parent := slices.IndexFunc(drawn, func(d []float64) bool {
		same := 0
		for i := range d {
			if d[i] == bred[i] {
				same++
			}
		}
		return same > len(d)/2
	})
	if parent < 0 {
		t.Fatalf("the round after plays %v, want a child bred from the archive, most of its weights a member's", bred[:3])
	}
}

// TestEachGameSettlesItsOwnCandidate: a learner holds a candidate a game, so a
// round scores the genome its own game played, in whatever order the games' rounds
// close.
func TestEachGameSettlesItsOwnCandidate(t *testing.T) {
	g, _, _ := learning(t)
	l, err := NewLearner(g, "fly", 1, 3, keeping(t, g.nets[0].digest))
	if err != nil {
		t.Fatal(err)
	}
	written := l.take(0, true)
	l.settle(0, 10, true)
	// The third candidate drawn is the hand-written genome again, its only member
	first, again, second := l.take(0, true), l.take(1, true), l.take(2, true)
	if slices.Equal(first, second) || !slices.Equal(again, written) {
		t.Fatal("three games were not handed a candidate each, the second the member to prove")
	}
	l.settle(2, 7, true)
	l.settle(0, 9, true)
	l.settle(1, 30, true)
	for score, genes := range map[float64][]float64{9: first, 7: second, 20: written} {
		if !slices.ContainsFunc(l.engine.Snapshot().Members, func(c genetic.Candidate[[]float64, float64]) bool {
			return c.Score == score && slices.Equal(c.Data, genes)
		}) {
			t.Errorf("no genome is held at %g: want each round under the genome its game played, the member's two as their mean", score)
		}
	}
}

// paced is an instance whose rounds last by the genome its driver has in play, so
// what a learner hands a game decides when that game next closes a round; its
// graph quits once it has played quit ticks, never for none
type paced struct {
	recorder
	d    *Driver
	left uint64
	quit int
}

func (p *paced) InputTick() bool { return p.quit == 0 || p.ticks < p.quit }

func (p *paced) Tick(int) {
	if p.ticks++; p.left == 0 {
		p.left = 2 + genomeKey(p.d.mind.nets[0].genes)%5
	}
	if p.left--; p.left == 0 {
		p.tap(event.GameEvent{Type: event.EventGameResetRequest, Payload: &event.GameResetPayload{}})
	}
}

// together has games paced games learn from one learner as play steps them, each
// quitting at the tick quit gives it, and returns the population they kept and
// its rounds
func together(t *testing.T, games int, quit func(game int) int, play func(drivers []*Driver)) (string, int) {
	t.Helper()
	g, _, _ := learning(t)
	store := &memory{}
	l, err := NewLearner(g, "fly", 1, games, store)
	if err != nil {
		t.Fatal(err)
	}
	drivers := make([]*Driver, games)
	for i := range drivers {
		_, ctx := soloWorld(0, 0)
		inst := &paced{quit: quit(i)}
		if inst.d, err = NewDriver(inst, ctx, g, 1, 0); err != nil {
			t.Fatal(err)
		}
		drivers[i] = inst.d
	}
	Learn(func() bool { return true }, []*Learner{l}, drivers...)
	play(drivers)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	var doc population
	if err := json.Unmarshal(store.doc, &doc); err != nil {
		t.Fatal(err)
	}
	return string(store.doc), doc.Rounds
}

// inTurn steps drivers a tick each in turn, each until it has no more to play or
// has played ticks, for ticks above zero
func inTurn(ticks int) func(drivers []*Driver) {
	return func(drivers []*Driver) {
		for playing := true; playing; {
			playing = false
			for _, d := range drivers {
				if ticks > 0 && d.stats.Ticks >= uint64(ticks) {
					continue
				}
				if more, _ := d.Step(); more {
					playing = true
				}
			}
		}
	}
}

// TestGamesThatLearnTogetherSearchAsIfTheyTookTurns: rounds close into a learner
// in the order of their ticks and then of their games, so games stepped side by
// side, each as its core runs it, keep the population they keep stepped in turn,
// and a game whose graph quits holds none of the rest back.
func TestGamesThatLearnTogetherSearchAsIfTheyTookTurns(t *testing.T) {
	const games = 4
	quit := func(game int) int { return 400 - 40*game }
	turns, rounds := together(t, games, quit, inTurn(0))
	if rounds < 200 {
		t.Fatalf("the games scored %d rounds in turn, too few to tell one order from another", rounds)
	}
	for range 20 {
		beside, scored := together(t, games, quit, func(drivers []*Driver) {
			var wg sync.WaitGroup
			for i, d := range drivers {
				wg.Go(func() {
					for more := true; more; more, _ = d.Step() {
						if i%2 == 1 {
							runtime.Gosched()
						}
					}
				})
			}
			wg.Wait()
		})
		if beside != turns {
			t.Fatalf("side by side the games kept %d rounds and a population that differs from the %d rounds kept in turn", scored, rounds)
		}
	}
}

// TestNoRoundThatClosesPastARunsEndIsScored: a run ends at the tick its own game
// reached, and the games beside it, which play on as their cores allow, stop at
// that tick and score no round past it, so what the run kept follows from its seed.
func TestNoRoundThatClosesPastARunsEndIsScored(t *testing.T) {
	const games, ticks = 4, 300
	never := func(int) int { return 0 }
	turns, rounds := together(t, games, never, inTurn(ticks))
	for range 20 {
		beside, scored := together(t, games, never, func(drivers []*Driver) {
			var wg sync.WaitGroup
			for _, d := range drivers[1:] {
				wg.Go(func() {
					for more := true; more; more, _ = d.Step() {
					}
				})
			}
			for range ticks {
				drivers[0].Step()
				runtime.Gosched()
			}
			drivers[0].End(ticks)
			wg.Wait()
		})
		if beside != turns {
			t.Fatalf("a run ended at tick %d kept %d rounds and a population that differs from the %d rounds its games keep stepped in turn to that tick",
				ticks, scored, rounds)
		}
	}
}

// twoNetworks is a learning graph of two networks, fly steering while no Kraken is
// up and kraken, whose table takes the keys given, while one is, a driver on it
// and each network's learner
func twoNetworks(t *testing.T, keys ...string) (*engine.World, *Driver, *recorder, []*Learner) {
	t.Helper()
	w, ctx := soloWorld(10, 2)
	g, err := ParseGraph("net", []byte(`[bot]
actions_per_second = 20
learn = true

[network.fly]
inputs = ["column"]
x = { bias = 0.1 }

[network.kraken]
inputs = ["column"]
x = { bias = -0.1 }
while = { name = "HasTarget", args = { target = "kraken" } }
`+strings.Join(keys, "\n")+`

[regions.play]
initial = "A"

[states.A]
on_update = [{ action = "Steer", payload = { network = ["kraken", "fly"] } }]
`))
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	d, err := NewDriver(rec, ctx, g, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var learners []*Learner
	for _, name := range g.Networks() {
		l, err := NewLearner(g, name, 1, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		learners = append(learners, l)
	}
	Learn(func() bool { return true }, learners, d)
	if _, err := d.Step(); err != nil { // the step that starts the graph steers nothing
		t.Fatal(err)
	}
	return w, d, rec, learners
}

// rounds plays a round for each count of ticks, which the game ends on its last
func (r *recorder) rounds(t *testing.T, d *Driver, ticks ...int) {
	t.Helper()
	for _, n := range ticks {
		r.dispatch = append(make([]event.GameEvent, n-1), event.GameEvent{Type: event.EventGameResetRequest, Payload: &event.GameResetPayload{}})
		for range n {
			if _, err := d.Step(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestALearnerIsScoredFromWhereItsNetworkFirstSteered: the learner trying a
// candidate takes the round from the tick its network first steered in it, the
// ticks and what they earned since; the others' fielded members are not scored.
func TestALearnerIsScoredFromWhereItsNetworkFirstSteered(t *testing.T) {
	w, d, rec, learners := twoNetworks(t)
	fly, kraken := learners[0], learners[1]
	rec.rounds(t, d, 4)
	if got := fly.Stats().Recent; got != 4 || kraken.Stats().Scored != 0 {
		t.Fatalf("four ticks steered with no Kraken: fly scored %g and kraken %d rounds, want 4 and none", got, kraken.Stats().Scored)
	}
	// Kraken's turn. A kill is worth 5: one lands before the Kraken rises, two
	// ticks in, and one after
	kill := event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: w.Resources.Player.Entity}}
	rec.dispatch = []event.GameEvent{kill, {}}
	for range 2 {
		d.Step()
	}
	place(w, core.DomainShared, 40, 10, func(e core.Entity) { w.Components.Kraken.SetComponent(e, component.KrakenComponent{}) })
	rec.dispatch = []event.GameEvent{kill, {}, {Type: event.EventGameResetRequest, Payload: &event.GameResetPayload{}}}
	for range 3 {
		d.Step()
	}
	if st := kraken.Stats(); st.Scored != 1 || st.Recent != 3+5 || fly.Stats().Scored != 1 {
		t.Fatalf("kraken scored %d rounds at %g and fly %d: want one of 8, the three ticks from the Kraken's rise and the kill after it, and fly's fielded member unscored",
			st.Scored, st.Recent, fly.Stats().Scored)
	}
}

// TestARoundScoresNoTickItsCaveStoodStill: time alone moves no score, so the ticks
// of a fight the scroll halts for add nothing to the round or to the candidate
// that fought it, which what it shot down pays.
func TestARoundScoresNoTickItsCaveStoodStill(t *testing.T) {
	w, d, rec, learners := twoNetworks(t)
	rec.rounds(t, d, 4) // fly's turn: the tick that starts the graph and four steered
	kill := event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: w.Resources.Player.Entity}}
	rec.dispatch = []event.GameEvent{kill, {}}
	for range 2 {
		d.Step()
	}
	place(w, core.DomainShared, 40, 10, func(e core.Entity) { w.Components.Kraken.SetComponent(e, component.KrakenComponent{}) })
	w.Resources.Cave.Steps = 0
	rec.dispatch = []event.GameEvent{kill, {}, {Type: event.EventGameResetRequest, Payload: &event.GameResetPayload{}}}
	for range 3 {
		d.Step()
	}
	if got := d.Stats().Score; got != 5+2+10 {
		t.Fatalf("two rounds score %g, want 17: the first's five columns, the second's two before the scroll halted and its two kills of 5", got)
	}
	if got := learners[1].Stats().Recent; got != 5 {
		t.Fatalf("kraken's candidate scored %g, want the kill after the Kraken rose alone", got)
	}
}

// TestAKrakensKillIsWorthItsTicksAtParLessTheSlowerItDied: a Kraken shot down at
// four hit points a second or faster earns the ticks that pace takes, a slower
// kill the share its pace is of that, and never less than a quarter.
func TestAKrakensKillIsWorthItsTicksAtParLessTheSlowerItDied(t *testing.T) {
	w, ctx := soloWorld(0, 0)
	d, rec := botFor(t, ctx, "[regions.play]\ninitial = \"A\"\n[states.A]\n", 0)
	own := w.Resources.Player.Entity
	// 120 hit points are thirty seconds at par
	par := int64(30 * time.Second / w.TickInterval())
	for _, lived := range []float64{20, 60, 600} {
		kraken := place(w, core.DomainShared, 40, 10, func(e core.Entity) {
			w.Components.Kraken.SetComponent(e, component.KrakenComponent{MaxHitPoints: 120, Time: lived})
		})
		rec.dispatch = append(rec.dispatch, event.GameEvent{Type: event.EventSpeciesKilled,
			Payload: &event.SpeciesKilledPayload{KillerEntity: own, Species: component.SpeciesKraken, Entity: kraken}})
	}
	for range 3 {
		if _, err := d.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := d.Stats().Fitness, par+par/2+par/4; got != want {
		t.Fatalf("three Krakens shot down in 20, 60 and 600 seconds weigh %d, want %d: par's %d ticks, half and a quarter", got, want, par)
	}
}

// TestLearnersExploreInTurnWhileTheRestPlayWhatTheyField: one learner plays a
// candidate, the hand-written genome first, and the rest the members they field.
// The turn passes as a round settles the candidate, and at once where the round
// never reached the explorer's network, whose candidate goes unscored.
func TestLearnersExploreInTurnWhileTheRestPlayWhatTheyField(t *testing.T) {
	w, d, rec, learners := twoNetworks(t)
	fly, kraken := learners[0], learners[1]
	written := func(i int) bool { return slices.Equal(d.mind.nets[i].genes, d.learners[i].seed) }

	rec.rounds(t, d, 2)
	if d.explorer != 1 || fly.Stats().Scored != 1 || !written(0) || !written(1) {
		t.Fatalf("after fly's hand-written genome is scored: explorer %d, fly scored %d: want kraken's turn on its own hand-written genome and fly fielding",
			d.explorer, fly.Stats().Scored)
	}
	rec.rounds(t, d, 2)
	if d.explorer != 0 || fly.Stats().Scored != 1 || kraken.Stats().Scored != 0 || written(0) {
		t.Fatalf("a round with no Kraken: explorer %d, fly scored %d, kraken %d: want neither scored and fly's turn again, on a candidate",
			d.explorer, fly.Stats().Scored, kraken.Stats().Scored)
	}
	place(w, core.DomainShared, 40, 10, func(e core.Entity) { w.Components.Kraken.SetComponent(e, component.KrakenComponent{}) })
	rec.rounds(t, d, 2)
	if d.explorer != 1 || fly.Stats().Scored != 1 || !written(0) || !written(1) {
		t.Fatalf("a round at the Kraken: explorer %d, fly scored %d: want fly's candidate dropped unscored and kraken's turn on its hand-written genome",
			d.explorer, fly.Stats().Scored)
	}
	rec.rounds(t, d, 2)
	if d.explorer != 0 || kraken.Stats().Scored != 1 || fly.Stats().Scored != 1 {
		t.Fatalf("a second round at the Kraken: explorer %d, kraken scored %d, fly %d: want kraken's candidate scored and fly's turn",
			d.explorer, kraken.Stats().Scored, fly.Stats().Scored)
	}
}

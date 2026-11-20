package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/rand/v2"
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
		{"[bot]\nlearn = true\n[network.a]\ninputs = [\"column\"]" + region + "[states.A]", "learn needs a [bot] score"},
		{"[bot]\nscore = { loot = 1 }" + region + "[states.A]", `"loot" is no source`},
		{"[bot]\nscore = { travel = 0 }" + region + "[states.A]", "a number other than zero"},
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

// TestAScorePaysOnlyWhatTheBotsOwnCursorDid: a score pays for what the bot's own
// steps dispatched for the cursor it drives, each source what its document gives it.
func TestAScorePaysOnlyWhatTheBotsOwnCursorDid(t *testing.T) {
	w, ctx := soloWorld(0, 0)
	d, rec := botFor(t, ctx, "[bot]\nscore = { typed = 1, kill = 5 }\n[regions.play]\ninitial = \"A\"\n[states.A]\n", 0)
	own := w.Resources.Player.Entity
	rec.dispatch = []event.GameEvent{
		{Type: event.EventBoostReward, Payload: &event.BoostRewardPayload{Entity: own}},
		{Type: event.EventBoostReward, Payload: &event.BoostRewardPayload{Entity: own + 1}},
		{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: own}},
		{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: own + 1}},
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
	if got := d.Stats().Score; got != 1+5 {
		t.Fatalf("score %g, want 6: the character its own cursor typed and the kill its own cursor made", got)
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

// TestAsksHoldsWhileANetworksOutputReadsAboveItsMark: a graph hands its states
// over to what a network asks for. Asks holds while the one-unit output it names
// reads above its mark, which no network without weights does. The wheel is had
// only by listing it, Steer does nothing with it, and a network that lists no
// output keeps special and move.
func TestAsksHoldsWhileANetworksOutputReadsAboveItsMark(t *testing.T) {
	doc := func(weights, args string) string {
		return "[bot]\nactions_per_second = 20\n[network.fly]\ninputs = [\"column\"]\noutputs = [\"move\", \"wheel\"]\n" + weights + `
[regions.play]
initial = "Own"

[states.Own]
transitions = [{ trigger = "Tick", target = "Asked", guard = "Asks", guard_args = { ` + args + ` } }]

[states.Asked]
on_update = [{ action = "Steer" }]
`
	}
	for _, c := range []struct{ weights, args, want string }{
		{"", `output = "wheel"`, "play:Own"},
		{"wheel = { bias = 0.5 }", `output = "wheel"`, "play:Asked"},
		{"wheel = { bias = 0.5 }", `output = "wheel", above = 1`, "play:Own"},
		{"wheel = { bias = 1.5 }", `output = "wheel", above = 1, network = "fly"`, "play:Asked"},
	} {
		_, ctx := soloWorld(10, 2)
		d, rec := botFor(t, ctx, doc(c.weights, c.args), 4)
		if got := d.Stats().State; got != c.want || len(rec.got) != 0 {
			t.Fatalf("with weights %q and %s the graph is in %s having sent %+v, want %s and nothing sent", c.weights, c.args, got, rec.got, c.want)
		}
	}
	for _, args := range []string{`output = "move"`, `output = "special"`, `output = "wheel", network = "walk"`} {
		if _, err := ParseGraph("net", []byte(doc("", args))); err == nil {
			t.Fatalf("an Asks of %s loaded, want it refused: an output of one unit that a network of the graph has", args)
		}
	}
	g, err := ParseGraph("net", []byte(steering(`inputs = ["column"]`)))
	if err != nil || !slices.Equal(g.nets[0].units, []string{"x", "y", "special"}) {
		t.Fatalf("a network listing no output has the units %v (%v), want x, y and special", g.nets[0].units, err)
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

// TestASteerActsOnTheOutputsItNames: a Steer that names outputs acts on those of its
// network alone, so one region has the special attack asked for whatever another
// does, and a move is made only where a Steer takes it. An output its network
// lacks, or one named twice, is refused.
func TestASteerActsOnTheOutputsItNames(t *testing.T) {
	doc := func(play, fire string) string {
		return `[bot]
actions_per_second = 40
[network.fly]
inputs = ["column"]
x = { bias = 0.1 }
special = { bias = 0.5 }

[regions.play]
initial = "Play"

[regions.fire]
initial = "Fire"

[states.Play]
on_update = [{ action = "Steer"` + play + ` }]

[states.Fire]
on_update = [{ action = "Steer"` + fire + ` }]
`
	}
	only := func(outputs string) string { return ", payload = { outputs = [" + outputs + "] }" }
	for _, c := range []struct {
		play, fire     string
		special, moves bool
	}{
		{only(`"move"`), only(`"special"`), true, true},
		{only(`"special"`), only(`"special"`), true, false},
		{only(`"move"`), only(`"move"`), false, true},
		{"", only(`"special"`), true, true},
	} {
		_, ctx := soloWorld(10, 2)
		_, rec := botFor(t, ctx, doc(c.play, c.fire), 7)
		var special, moves bool
		for _, intent := range rec.got {
			special = special || intent.Type == input.IntentFireSpecial
			moves = moves || intent.Type == input.IntentMouseMove
		}
		if special != c.special || moves != c.moves {
			t.Errorf("play steering%q and fire%q: the special attack asked for %t and a move made %t, want %t and %t",
				c.play, c.fire, special, moves, c.special, c.moves)
		}
	}
	for _, outputs := range []string{`"wheel"`, `"move", "move"`, `"aim"`} {
		if _, err := ParseGraph("net", []byte(doc(only(outputs), ""))); err == nil {
			t.Fatalf("a Steer of outputs %s loaded, want it refused: each once, and one its network has", outputs)
		}
	}
}

// TestDefaultTypesAheadOfTheSpecialAttackItAsksFor: default asks for the special
// attack from a region that updates after the one that types, so a step that does
// both sends the key first. Asked ahead of it, the blast takes the key's glyph and
// the key is an error.
func TestDefaultTypesAheadOfTheSpecialAttackItAsksFor(t *testing.T) {
	data, err := fs.ReadFile(asset.DefaultBots, "default.toml")
	if err != nil {
		t.Fatal(err)
	}
	// At forty a second every step has room for the key and the special attack both
	doc := strings.Replace(string(data), "actions_per_second = 16", "actions_per_second = 40", 1)
	if doc == string(data) {
		t.Fatal("default's rate is no longer sixteen a second")
	}
	w, ctx := soloWorld(10, 2)
	ctx.SetMode(core.ModeInsert)
	place(w, core.DomainShared, 10, 2, func(e core.Entity) {
		w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 'k'})
	})
	_, rec := botFor(t, ctx, doc, 12)
	both := 0
	for i := 1; i < len(rec.got); i++ {
		if rec.at[i] != rec.at[i-1] {
			continue
		}
		switch first, second := rec.got[i-1].Type, rec.got[i].Type; {
		case first == input.IntentFireSpecial && second == input.IntentTextChar:
			t.Errorf("step %d asks for the special attack ahead of its key", rec.at[i])
		case first == input.IntentTextChar && second == input.IntentFireSpecial:
			both++
		}
	}
	if both == 0 {
		t.Errorf("no step of twelve both typed and asked for the special attack: %+v", rec.got)
	}
}

// TestARoundScoresWhatItsDocumentPays: a new game closes a round, scored by what
// the graph's score paid in it: here three a column scrolled and twelve a hit
// point of a declared species' kill. The run's score holds the round in play
// too, a kill no source of the document measures pays nothing, and a graph with
// no score scores nothing.
func TestARoundScoresWhatItsDocumentPays(t *testing.T) {
	const states = "[regions.play]\ninitial = \"A\"\n[states.A]\n"
	w, ctx := soloWorld(0, 0)
	w.Resources.Cave.Steps = 2
	w.Resources.Behaviors = []component.BehaviorPrototype{{HitPoints: 28}}
	d, rec := botFor(t, ctx, "[bot]\nscore = { travel = 3, hit_points = 12 }\n"+states, 0)
	own := w.Resources.Player.Entity
	round := 0.0
	d.Round = func(_ uint64, score float64) { round = score }
	declared := event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: own, Species: component.SpeciesDataFirst}}
	rec.dispatch = []event.GameEvent{declared, {}, {},
		{Type: event.EventGameResetRequest, Payload: &event.GameResetPayload{}},
		{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: own}},
	}
	for range 5 {
		if _, err := d.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if st := d.Stats(); st.Rounds != 1 || round != 4*2*3+12*28 || st.Score != round+2*3 {
		t.Fatalf("rounds %d, the round %g, the run %g: want one round of 4 ticks at two columns each paid three times and a kill of 28 hit points paid twelve times, and the run two columns more",
			st.Rounds, round, st.Score)
	}
	none, rec := botFor(t, ctx, states, 0)
	rec.dispatch = []event.GameEvent{declared}
	if _, err := none.Step(); err != nil || none.Stats().Score != 0 {
		t.Fatalf("a graph with no score scored %g, %v: want nothing", none.Stats().Score, err)
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
	doc, err := json.Marshal(population{Version: populationVersion, Layout: layout, ScoredBy: learnsBy, Rounds: learnPool, Members: members})
	if err != nil {
		t.Fatal(err)
	}
	return &memory{doc: doc}
}

// learns is the [bot] table of a graph that learns, and learnsBy how its score
// is spelt, which a population is kept under
const (
	learns   = "[bot]\nlearn = true\nscore = { travel = 1, kill = 5 }"
	learnsBy = "kill=5 travel=1"
)

// learning is a learning graph, a driver on it and the instance it plays
func learning(t *testing.T) (*Graph, *Driver, *recorder) {
	t.Helper()
	doc := strings.Replace(steering("inputs = [\"column\", \"energy\"]\nhidden = 2\nx = { column = -0.5 }"), "[bot]", learns, 1)
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
// over from the hand-written genome. A save says each member's rounds and birth,
// the proved first with the fielded at their head.
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
	if saved.Layout != net.digest || saved.ScoredBy != learnsBy || saved.Rounds != learnPool+1 || len(saved.Members) != 4 || entered < 2 ||
		saved.Members[entered].Born != learnPool+1 || saved.Members[entered].Rounds != 1 || saved.Fielded != 0 ||
		saved.Members[0].Born != 5 || saved.Members[1].Born != 7 {
		t.Fatalf("saved %+v: want the three kept with their births, the two proved first and the more trusted fielded, and the genome scored 45 born at round 1",
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

// TestRepeatsTestTheTitleAndProveTheBestGuessByTurns: every third candidate is a
// member again, by turns the less played of the fielded and its rival, the two
// most trusted, and the most trusted short of its proof. What is fielded is the
// most trusted of the proved: a few lucky rounds do not unseat many good ones.
func TestRepeatsTestTheTitleAndProveTheBestGuessByTurns(t *testing.T) {
	g, _, _ := learning(t)
	net := g.nets[0]
	genome := func(v float64) []float64 { return slices.Repeat([]float64{v}, len(net.genes)) }
	l, err := NewLearner(g, "fly", 1, 1, keeping(t, net.digest,
		member{Genes: genome(0.1), Score: 500, Rounds: learnProven - 2},
		member{Genes: genome(0.2), Score: 60, Rounds: learnProven},
		member{Genes: genome(0.3), Score: 50, Rounds: 4 * learnProven},
		member{Genes: genome(0.4), Score: 30, Rounds: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if fielded := l.take(0, false); !slices.Equal(fielded, genome(0.3)) {
		t.Fatalf("fields %v, want the member of many rounds over the luckier one of few", fielded[:1])
	}
	for i, want := range [][]float64{genome(0.2), genome(0.1)} {
		for l.drawn%learnRepeat != learnRepeat-1 {
			l.take(0, true)
			l.settle(0, 20, true)
		}
		if candidate := l.take(0, true); !slices.Equal(candidate, want) {
			t.Fatalf("repeat %d is %v, want %v: the fielded's rival, the less played, then the most trusted short of its proof", i+1, candidate[:1], want[:1])
		}
		l.settle(0, 60, true)
	}
}

// probation is a learner on a full archive: the proved it has room for, eight
// rounds each at 100 less a tenth a place, and members of one round at 300 less
// one a place, every one of which outscores every proved. A genome of 0.5 with
// a round short of its proof at mean takes the last's place where mean is given.
func probation(t *testing.T, mean ...float64) (*Learner, func(float64) []float64) {
	t.Helper()
	g, _, _ := learning(t)
	net := g.nets[0]
	genome := func(v float64) []float64 { return slices.Repeat([]float64{v}, len(net.genes)) }
	var full []member
	for i := range learnPool {
		if i < learnPool-learnProbation {
			full = append(full, member{Genes: genome(float64(i+1) / 100), Score: 100 - float64(i)/10, Rounds: learnProven})
		} else {
			full = append(full, member{Genes: genome(float64(i+1) / 100), Score: float64(300 - i), Rounds: 1})
		}
	}
	if len(mean) > 0 {
		full[learnPool-1] = member{Genes: genome(0.5), Score: mean[0], Rounds: learnProven - 1}
	}
	l, err := NewLearner(g, "fly", 1, 1, keeping(t, net.digest, full...))
	if err != nil {
		t.Fatal(err)
	}
	return l, genome
}

// TestANewcomerDisplacesOnlyAMemberOnProbation: the archive ranks by what rounds
// prove, the proved above the rest, so a newcomer's one round, however high,
// takes the place of the last member short of its proof and of no proved one;
// and one whose round ranks it under them all does not enter.
func TestANewcomerDisplacesOnlyAMemberOnProbation(t *testing.T) {
	l, genome := probation(t)
	foot := genome(float64(learnPool) / 100)
	l.engine.CompleteEvaluation(l.engine.BeginEvaluation(genome(0.99)), 5000)
	members := l.engine.Snapshot().Members
	proved := learnPool - learnProbation
	for i, c := range members {
		if (c.Count >= learnProven) != (i < proved) {
			t.Fatalf("member %d of the archive has %d rounds: want the %d proved first, as a round of 5000 displaces none of them", i, c.Count, proved)
		}
	}
	if !l.holds(genome(0.99)) || l.holds(foot) || !slices.Equal(members[proved].Data, genome(0.99)) {
		t.Fatal("a round of 5000 did not take the place of the last member on probation, at the head of the rest")
	}
	l.engine.CompleteEvaluation(l.engine.BeginEvaluation(genome(0.98)), 200)
	if l.holds(genome(0.98)) {
		t.Fatal("a round under every member's on probation entered")
	}
}

// TestAMemberProvedTakesAProvedMembersPlaceOrLeaves: a member whose round proves
// it stands among the proved by its trusted mean, and as the next newcomer
// enters the last of them leaves: another where the one just proved ranks above
// it, and the one just proved where it does not, level with the last included.
func TestAMemberProvedTakesAProvedMembersPlaceOrLeaves(t *testing.T) {
	level := 100 - float64(learnPool-learnProbation-1)/10
	for name, c := range map[string]struct {
		mean  float64
		stays bool
	}{"above": {120, true}, "below": {80, false}, "level": {level, false}} {
		l, genome := probation(t, c.mean)
		last := genome(float64(learnPool-learnProbation) / 100)
		l.engine.CompleteEvaluation(l.engine.BeginEvaluation(genome(0.5)), c.mean)
		if !l.holds(genome(0.5)) || !l.holds(last) {
			t.Fatalf("%s: a member proved left, or pushed a proved one off, before any newcomer wanted the place", name)
		}
		l.engine.CompleteEvaluation(l.engine.BeginEvaluation(genome(0.99)), 1)
		if !l.holds(genome(0.99)) || l.holds(genome(0.5)) != c.stays || l.holds(last) == c.stays {
			t.Errorf("%s: after the next newcomer the member proved at %g is held %t and the last of the proved %t: want the lower of the two gone and the newcomer in its place",
				name, c.mean, l.holds(genome(0.5)), l.holds(last))
		}
	}
}

// TestACandidateThatWouldEnterAFullArchiveIsScoredByItsNextRound: a first round
// good enough to enter only qualifies its genome, which plays again and enters,
// or not, on that round alone, as the first was picked for its luck; it is a
// round counted and kept all the same. The hand-written genome plays first,
// whatever the store kept.
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
		store := keeping(t, net.digest, full...)
		l, err := NewLearner(g, "fly", 1, 1, store)
		if err != nil {
			t.Fatal(err)
		}
		first = l.take(0, true)
		for _, score := range scores {
			l.settle(0, score, true)
			last = l.take(0, true)
		}
		var kept population
		if err := errors.Join(l.Close(), json.Unmarshal(store.doc, &kept)); err != nil || l.Stats().Scored != len(scores) || kept.Rounds != learnPool+len(scores) {
			t.Fatalf("%d rounds scored and %d kept of %d played on %d, closing with %v", l.Stats().Scored, kept.Rounds, len(scores), learnPool, err)
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

// TestACandidateTheArchiveHoldsIsThatMemberAgain: a genome the archive holds is
// no new one to qualify, as the hand-written genome of a kept population is, so
// its round joins the member's mean whichever way it went.
func TestACandidateTheArchiveHoldsIsThatMemberAgain(t *testing.T) {
	g, _, _ := learning(t)
	net := g.nets[0]
	for _, round := range []float64{100, 6000} {
		full := []member{{Genes: slices.Clone(net.genes), Score: 3000, Rounds: 10}}
		for i := range learnPool - 1 {
			full = append(full, member{Genes: slices.Repeat([]float64{float64(i+1) / 100}, len(net.genes)), Score: float64(2000 - i), Rounds: 1})
		}
		l, err := NewLearner(g, "fly", 1, 1, keeping(t, net.digest, full...))
		if err != nil {
			t.Fatal(err)
		}
		l.take(0, true)
		l.settle(0, round, true)
		if best := l.engine.Snapshot().Members[0]; !slices.Equal(best.Data, net.genes) || best.Count != 11 || best.Score != genetic.MeanScore(3000, round, 10) {
			t.Fatalf("a round of %g leaves the best member at %g over %d: want the hand-written one, its ten rounds at 3000 and this one", round, best.Score, best.Count)
		}
	}
}

// TestAMemberThatLeftTheArchiveComesBackOnTheMeanOfItsRounds: a member played
// again in two games may be pushed off before the second's round closes, once
// the first's has brought it to the foot of probation and a newcomer entered. It
// re-enters on the mean of its rounds with this one, not on this one alone as a
// new genome would.
func TestAMemberThatLeftTheArchiveComesBackOnTheMeanOfItsRounds(t *testing.T) {
	g, _, _ := learning(t)
	net := g.nets[0]
	genome := func(v float64) []float64 { return slices.Repeat([]float64{v}, len(net.genes)) }
	var full []member
	for i := range learnPool {
		full = append(full, member{Genes: genome(float64(i+1) / 100), Score: float64(1000 - i), Rounds: 1})
		if i < learnPool-learnProbation {
			full[i].Rounds = learnProven
		}
	}
	tail := &full[learnPool-1]
	tail.Rounds = 3
	l, err := NewLearner(g, "fly", 1, 2, keeping(t, net.digest, full...))
	if err != nil {
		t.Fatal(err)
	}
	l.take(0, true)
	l.settle(0, 1, true) // the hand-written genome, scored under them all
	l.drawn = 2*learnRepeat - 1
	if again := l.take(0, true); !slices.Equal(again, tail.Genes) {
		t.Fatalf("the repeat is %v, want the member short of its proof its three rounds make the most trusted", again[:1])
	}
	l.engine.CompleteEvaluation(l.engine.BeginEvaluation(tail.Genes), -1e6)
	l.engine.CompleteEvaluation(l.engine.BeginEvaluation(genome(0.99)), 990.5)
	if l.holds(tail.Genes) {
		t.Fatal("a round that brought the member to the foot and a newcomer's entry did not push it off the archive")
	}
	l.settle(0, 5000, true)
	want := genetic.MeanScore(tail.Score, 5000, tail.Rounds)
	if !slices.ContainsFunc(l.engine.Snapshot().Members, func(c genetic.Candidate[[]float64, float64]) bool {
		return slices.Equal(c.Data, tail.Genes) && c.Score == want
	}) {
		t.Fatalf("the member is not back at %g, the mean of its three rounds at %g and one of 5000", want, tail.Score)
	}
}

// TestASaveLeavesAPopulationFurtherOnAsItIs: two runs may learn one population, a
// watched one opened beside a headless one. The file is the one's with more
// rounds: a save onto more fails, said once and reported as the run ends, and
// passes once this run is the one further on.
func TestASaveLeavesAPopulationFurtherOnAsItIs(t *testing.T) {
	g, _, _ := learning(t)
	store := &memory{}
	var runs [2]*Learner
	for i := range runs {
		var err error
		if runs[i], err = NewLearner(g, "fly", uint64(i+1), 1, store); err != nil {
			t.Fatal(err)
		}
	}
	play := func(l *Learner, rounds int) error {
		for range rounds {
			l.take(0, true)
			l.settle(0, 45, true)
		}
		return l.Close()
	}
	said := 0
	runs[1].Lost = func(error) { said++ }
	if err := play(runs[0], 5); err != nil {
		t.Fatal(err)
	}
	further := string(store.doc)
	for _, rounds := range []int{3, 1} {
		if err := play(runs[1], rounds); err == nil || string(store.doc) != further {
			t.Fatalf("a save %d rounds behind the file closed with %v: want it refused and the file as it was", 5-runs[1].rounds, err)
		}
	}
	var kept population
	if err := errors.Join(play(runs[1], 2), json.Unmarshal(store.doc, &kept)); err != nil || kept.Rounds != 6 || said != 1 {
		t.Fatalf("a run a round further on kept %d rounds, closing with %v, having said %d times it could not: want its six kept and one notice", kept.Rounds, err, said)
	}
}

// TestADamagedCountOfRoundsStillSaves: a kept document whose rounds read below
// none counts from none, or the recent mean divides by nothing and no save of
// the run can be written.
func TestADamagedCountOfRoundsStillSaves(t *testing.T) {
	g, _, _ := learning(t)
	doc, err := json.Marshal(population{Version: populationVersion, Layout: g.nets[0].digest, ScoredBy: learnsBy, Rounds: -1})
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewLearner(g, "fly", 1, 1, &memory{doc: doc})
	if err != nil {
		t.Fatal(err)
	}
	l.take(0, true)
	l.settle(0, 45, true)
	if err := l.Close(); err != nil || l.rounds != 1 {
		t.Fatalf("a round on a document of -1 rounds closes with %v at %d rounds, want it kept as the first", err, l.rounds)
	}
}

// TestAPopulationStartsFromTheWeightsItsGraphWrites: a population whose graph
// writes weights plays them first and then their children, a weight or few
// stepped, as a start drawn about on every weight is lost among copies that fly
// worse.
func TestAPopulationStartsFromTheWeightsItsGraphWrites(t *testing.T) {
	g, err := ParseGraph("net", []byte(strings.Replace(steering("inputs = [\"column\", \"energy\"]\nhidden = 2\nx = { column = -0.5 }"), "[bot]", learns, 1)))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewLearner(g, "fly", 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for round := range learnPool {
		genes := l.take(0, true)
		changed := 0
		for i, w := range genes {
			if w != l.seed[i] {
				changed++
			}
		}
		if round == 0 && changed != 0 || changed > len(genes)/2 {
			t.Fatalf("round %d plays a genome %d of %d weights off the written one, want the written weights first and their children after", round+1, changed, len(genes))
		}
		l.settle(0, float64(round+1), true)
	}
}

// TestANewPopulationsFirstRoundsPlayGenomesDrawnAtRandom: a population with no
// rounds behind it has nothing to breed from, so where its graph writes no weight
// its first learnPool rounds play genomes drawn about zero, each its own; one
// past them breeds, and the written genome, having no weight, is not tried.
func TestANewPopulationsFirstRoundsPlayGenomesDrawnAtRandom(t *testing.T) {
	g, err := ParseGraph("net", []byte(strings.Replace(steering("inputs = [\"column\", \"energy\"]\nhidden = 2"), "[bot]", learns, 1)))
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

// TestNoRoundThatClosesPastARunsEndIsScored: a run ends at the tick its own game's
// graph quit at, and the games beside it, which play on as their cores allow,
// stop at that tick and score no round past it, however late the run says so
// again, so what the run kept follows from its seed.
func TestNoRoundThatClosesPastARunsEndIsScored(t *testing.T) {
	const games, ticks = 4, 300
	quit := func(game int) int {
		if game == 0 {
			return ticks
		}
		return 0
	}
	turns, rounds := together(t, games, quit, inTurn(0))
	for range 20 {
		beside, scored := together(t, games, quit, func(drivers []*Driver) {
			var wg sync.WaitGroup
			for _, d := range drivers[1:] {
				wg.Go(func() {
					for more := true; more; more, _ = d.Step() {
					}
				})
			}
			for more := true; more; more, _ = drivers[0].Step() {
				runtime.Gosched()
			}
			time.Sleep(200 * time.Microsecond) // a run closes its game before it ends the learning
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
// up and kraken while one is, a driver on it and each network's learner
func twoNetworks(t *testing.T) (*engine.World, *Driver, *recorder, []*Learner) {
	t.Helper()
	w, ctx := soloWorld(10, 2)
	g, err := ParseGraph("net", []byte(learns+`
actions_per_second = 20

[network.fly]
inputs = ["column"]
x = { bias = 0.1 }

[network.kraken]
inputs = ["column"]
x = { bias = -0.1 }

[regions.play]
initial = "A"

[states.A]
on_update = [
    { action = "Steer", payload = { network = "kraken" }, guard = "HasTarget", guard_args = { target = "kraken" } },
    { action = "Steer", payload = { network = "fly" }, guard = "Not", guard_args = { guard = { name = "HasTarget", args = { target = "kraken" } } } },
]
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

// TestALearnerIsScoredByTheTicksItsNetworkSteered: what a round's ticks were paid
// is the network's that steered last, so one that steers only while a Kraken is
// up is scored by the fight and the other by the rest of the round.
func TestALearnerIsScoredByTheTicksItsNetworkSteered(t *testing.T) {
	w, d, rec, learners := twoNetworks(t)
	fly, kraken := learners[0], learners[1]
	w.Resources.Cave.Steps = 2
	// A kill pays 5: one lands in fly's first two ticks and one in the three a
	// Kraken is up for, before the round's last two
	kill := event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: w.Resources.Player.Entity}}
	rec.dispatch = []event.GameEvent{kill, {}}
	for range 2 {
		d.Step()
	}
	up := place(w, core.DomainShared, 40, 10, func(e core.Entity) { w.Components.Kraken.SetComponent(e, component.KrakenComponent{}) })
	rec.dispatch = []event.GameEvent{kill, {}, {}}
	for range 3 {
		d.Step()
	}
	w.DestroyEntity(up)
	rec.rounds(t, d, 2)
	if f, k := fly.Stats(), kraken.Stats(); f.Scored != 1 || f.Recent != 4*2+5 || k.Scored != 1 || k.Recent != 3*2+5 {
		t.Fatalf("fly scored %d rounds at %g and kraken %d at %g: want one each, the two columns of fly's four ticks and its kill, 13, and of kraken's three and its own, 11",
			f.Scored, f.Recent, k.Scored, k.Recent)
	}
}

// TestAKrakensKillIsWorthItsTicksAtParLessTheSlowerItDied: a Kraken shot down at
// four hit points a second or faster earns the ticks that pace takes, a slower
// kill the share its pace is of that, and never less than a quarter. A kill only
// predicted is weighed then and paid once proved.
func TestAKrakensKillIsWorthItsTicksAtParLessTheSlowerItDied(t *testing.T) {
	w, ctx := soloWorld(0, 0)
	d, rec := botFor(t, ctx, "[bot]\nscore = { kraken = 1 }\n[regions.play]\ninitial = \"A\"\n[states.A]\n", 0)
	own := w.Resources.Player.Entity
	// 120 hit points are thirty seconds at par
	par := float64(30 * time.Second / w.TickInterval())
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
	if got, want := d.Stats().Score, par+par/2+par/4; got != want {
		t.Fatalf("three Krakens shot down in 20, 60 and 600 seconds pay %g, want %g: par's %g ticks, half and a quarter", got, want, par)
	}
	// A session's guest predicts the death and is paid as it is proved, the Kraken gone
	kraken := place(w, core.DomainShared, 40, 10, func(e core.Entity) {
		w.Components.Kraken.SetComponent(e, component.KrakenComponent{MaxHitPoints: 120, Time: 20})
	})
	killed := &event.SpeciesKilledPayload{KillerEntity: own, Species: component.SpeciesKraken, Entity: kraken}
	rec.dispatch = []event.GameEvent{{Type: event.EventSpeciesKilled, Payload: killed, Phase: event.PhasePredicted}}
	d.Step()
	w.DestroyEntity(kraken)
	rec.dispatch = []event.GameEvent{{Type: event.EventSpeciesKillConfirmed, Payload: killed}}
	d.Step()
	if got, want := d.Stats().Score, 2*par+par/2+par/4; got != want {
		t.Fatalf("a predicted kill proved after its Kraken is gone brings the score to %g, want %g, par's ticks more", got, want)
	}
}

// TestALearnersCandidateWaitsForTheRoundItsNetworkSteers: alone, every learner
// plays a candidate, the hand-written genome first. A round scores those whose
// networks steered in it, and the candidate of one that never did plays on
// until a round reaches its network.
func TestALearnersCandidateWaitsForTheRoundItsNetworkSteers(t *testing.T) {
	w, d, rec, learners := twoNetworks(t)
	fly, kraken := learners[0], learners[1]
	plays := func(i int) []float64 { return d.mind.nets[i].genes }
	if !slices.Equal(plays(0), fly.seed) || !slices.Equal(plays(1), kraken.seed) {
		t.Fatal("the first round does not play each learner's hand-written genome")
	}
	rec.rounds(t, d, 2, 2)
	if fly.Stats().Scored != 2 || kraken.Stats().Scored != 0 || kraken.flying() != 1 || !slices.Equal(plays(1), kraken.seed) {
		t.Fatalf("two rounds with no Kraken: fly scored %d and kraken %d: want both rounds fly's and kraken's first candidate still in flight",
			fly.Stats().Scored, kraken.Stats().Scored)
	}
	place(w, core.DomainShared, 40, 10, func(e core.Entity) { w.Components.Kraken.SetComponent(e, component.KrakenComponent{}) })
	waiting := slices.Clone(plays(0))
	rec.rounds(t, d, 2)
	if fly.Stats().Scored != 2 || kraken.Stats().Scored != 1 || slices.Equal(plays(1), kraken.seed) || !slices.Equal(plays(0), waiting) {
		t.Fatalf("a round at the Kraken: fly scored %d and kraken %d: want kraken's candidate scored and its next dealt, and fly's still in flight",
			fly.Stats().Scored, kraken.Stats().Scored)
	}
}

// weighing is a graph of two networks whose table is given, each steering in turn
func weighing(t *testing.T, name, fly, kraken string) *Graph {
	t.Helper()
	g, err := ParseGraph(name, []byte("# a bot that flies\n"+learns+"\ninputs = [\"column\"]\n\n[network.fly]\n"+fly+
		"\n[network.kraken]\n"+kraken+"\n[regions.play]\ninitial = \"A\"\n\n[states.A] # steers\non_update = [{ action = \"Steer\", payload = { network = \"fly\" } }]\n"))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestADocumentWritesTheWeightsItIsGivenAndReadsThemBack: a genome written into a
// bot's document replaces the weights it wrote, every other line kept, and reads
// back bit for bit, whatever the weights; with none it is a bot that starts from
// nothing.
func TestADocumentWritesTheWeightsItIsGivenAndReadsThemBack(t *testing.T) {
	g := weighing(t, "pilot", "inputs = [\"energy\", \"heat\"]\nhidden = 3\nx = { column = -0.5, hidden = [0.25] } # by hand\nunits = [{ energy = 1.0 }]",
		"inputs = [\"row\"]\noutputs = [\"move\"]\n[network.kraken.y]\nrow = -1.0\n")
	rng := rand.New(rand.NewPCG(1, 2))
	for round := range 50 {
		w := g.Weights()
		for _, genes := range w {
			for i := range genes {
				switch rng.IntN(3) {
				case 0:
					genes[i] = 0
				case 1:
					genes[i] = (rng.Float64()*2 - 1) * weightBound
				default:
					genes[i] = math.Float64frombits(rng.Uint64()>>12|0x3f10000000000000) * float64(rng.IntN(3)-1)
				}
			}
		}
		if round == 0 {
			w = g.Bare()
		}
		text, err := g.Document(w)
		if err != nil {
			t.Fatal(err)
		}
		back, err := ParseGraph("pilot", text)
		if err != nil {
			t.Fatal(err)
		}
		for name, genes := range back.Weights() {
			if !slices.EqualFunc(genes, w[name], func(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }) {
				t.Fatalf("round %d: network %s reads back %v for %v", round, name, genes, w[name])
			}
		}
		for _, kept := range []string{"# a bot that flies", "[states.A] # steers", "inputs = [\"energy\", \"heat\"]"} {
			if !strings.Contains(string(text), kept) {
				t.Fatalf("round %d: the document lost %q:\n%s", round, kept, text)
			}
		}
		if round == 0 && (strings.Contains(string(text), "units") || strings.Contains(string(text), "by hand") || strings.Contains(string(text), "network.kraken.y")) {
			t.Fatalf("a bot with no weight still writes some:\n%s", text)
		}
	}
}

// TestABotTakesAnothersWeightsByNameOrNothing: one bot takes another's network by
// its inputs' names and its units' places, so the two steer alike on the readings
// they share, whatever order and whatever more the taker reads. A network, an
// input, an output or a hidden unit the taker lacks fits nothing.
func TestABotTakesAnothersWeightsByNameOrNothing(t *testing.T) {
	from := weighing(t, "source", "inputs = [\"energy\", \"heat\"]\nhidden = 2\nx = { column = -0.5, energy = 0.3, hidden = [0.25, -1.5], bias = 0.1 }\ny = { heat = 2.0 }\n"+
		"special = { hidden = [0.0, 1.0], bias = -0.2 }\nunits = [{ energy = 1.0, bias = 0.5 }, { column = -2.0, heat = 0.7 }]",
		"inputs = [\"row\"]\ny = { row = -1.0 }")
	to := weighing(t, "taker", "inputs = [\"row\", \"heat\", \"energy\"]\nhidden = 3\nx = { row = 3.0 }", "inputs = [\"row\"]")
	w, err := to.Fit(from, "fly")
	if err != nil || len(w) != 1 {
		t.Fatalf("taking one network gave %d, %v", len(w), err)
	}
	a, b := from.net("fly").playing(), to.net("fly").playing()
	b.genes = w["fly"]
	rng := rand.New(rand.NewPCG(3, 4))
	for range 100 {
		column, energy, heat := rng.Float64()*2-1, rng.Float64(), rng.Float64()
		copy(a.in, []float64{column, energy, heat})
		copy(b.in, []float64{column, rng.Float64()*2 - 1, heat, energy})
		if got, want := slices.Clone(b.pass()), a.pass(); !slices.Equal(got, want) {
			t.Fatalf("the taker steers %v where its source steers %v", got, want)
		}
	}
	// kraken's y, the second of its units, reads column and then row
	if all, err := to.Fit(from); err != nil || len(all) != 2 || all["kraken"][4] != -1 {
		t.Fatalf("taking every network gave %v, %v", all, err)
	}
	for fault, taker := range map[string]*Graph{
		"an input it lacks":      weighing(t, "taker", "inputs = [\"energy\"]\nhidden = 2", "inputs = [\"row\"]"),
		"a hidden unit it lacks": weighing(t, "taker", "inputs = [\"energy\", \"heat\"]\nhidden = 1", "inputs = [\"row\"]"),
		"an output it lacks":     weighing(t, "taker", "inputs = [\"energy\", \"heat\"]\nhidden = 2\noutputs = [\"move\"]", "inputs = [\"row\"]"),
	} {
		if _, err := taker.Fit(from, "fly"); err == nil {
			t.Errorf("%s: the weights were taken", fault)
		}
	}
	if _, err := to.Fit(from, "none"); err == nil {
		t.Error("a network neither has was taken")
	}
}

// TestWhatAPopulationFieldsIsReadWhateverScoredIt: a population's document gives
// its rounds and the first member eight rounds have proved, for the layout it was
// kept under and none other, whatever version scored it: a model is its genes.
func TestWhatAPopulationFieldsIsReadWhateverScoredIt(t *testing.T) {
	g, _, _ := learning(t)
	net := g.nets[0]
	genome := func(v float64) []float64 { return slices.Repeat([]float64{v}, len(net.genes)) }
	doc := func(version int, layout, scoredBy string) *memory {
		data, err := json.Marshal(population{Version: version, Layout: layout, ScoredBy: scoredBy, Rounds: 70, Recent: 12.5, Members: []member{
			{Genes: genome(0.1), Score: 90, Rounds: 2}, {Genes: genome(0.2), Score: 60, Rounds: learnProven + 1}, {Genes: genome(0.3), Score: 50, Rounds: 30}}})
		if err != nil {
			t.Fatal(err)
		}
		return &memory{doc: data}
	}
	k, ok, err := g.Kept("fly", doc(populationVersion, net.digest, learnsBy))
	if err != nil || !ok || k.Rounds != 70 || k.Recent != 12.5 || k.Score != 50 || k.Proved != 30 || !slices.Equal(k.Fielded, genome(0.3)) {
		t.Fatalf("kept %+v, %v, %v: want its 70 rounds and the member a run fields, the most trusted", k, ok, err)
	}
	if _, ok, err := g.Kept("fly", doc(populationVersion, "another", learnsBy)); ok || err != nil {
		t.Fatalf("a population of another layout was read: %v, %v", ok, err)
	}
	if _, ok, err := g.Kept("fly", doc(populationVersion-1, net.digest, learnsBy)); ok || err != nil {
		t.Fatalf("a population another version scored was read: %v, %v", ok, err)
	}
	if _, ok, err := g.Kept("fly", doc(populationVersion, net.digest, "kill=6 travel=1")); ok || err != nil {
		t.Fatalf("a population another score paid was read: %v, %v", ok, err)
	}
	if _, ok, err := g.Kept("fly", &memory{}); ok || err != nil {
		t.Fatalf("no population was read as one: %v, %v", ok, err)
	}
}

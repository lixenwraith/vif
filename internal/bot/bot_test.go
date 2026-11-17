package bot

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"slices"
	"strings"
	"testing"

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
		{region + `[states.A]
on_update = [{ action = "Steer" }]`, "the graph declares no [network] to steer by"},
		{"[bot]\nlearn = true" + region + "[states.A]", "learn needs a [network]"},
		{"[network]\ninputs = [\"column\", \"radar\"]" + region + "[states.A]", `[network]: inputs: "radar" is no sensor`},
		{"[network]\ninputs = [\"column\"]\ny = { energy = 1.0 }" + region + "[states.A]", `[network]: weight "energy" = 1: not an input or bias`},
		{"[network]\ninputs = [\"column\"]\nx = { column = 4.5 }" + region + "[states.A]", `[network]: weight "column" = 4.5`},
	} {
		_, err := ParseGraph("broken", []byte(tt.doc))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("graph %q: error %v, want one naming %s", tt.doc, err, tt.want)
		}
	}
}

// soloWorld is an 80x24 instance whose own cursor stands at x, y
func soloWorld(x, y int) (*engine.World, *engine.GameContext) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	cursor := w.CreateEntity(core.DomainShared)
	w.Components.Cursor.SetComponent(cursor, component.CursorComponent{Slot: 0})
	w.Resources.Player.Bind(0, cursor)
	w.Resources.Player.SetLocal(0)
	w.Positions.SetPosition(cursor, component.PositionComponent{X: x, Y: y})
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

// steering is a graph whose one state steers by a network of the given table
func steering(network string) string {
	return "[bot]\nactions_per_second = 20\n[network]\n" + network + `
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

// TestTheSensorsReadWhatTheViewShows: each sensor is an offset from the cursor
// scaled to the view, of the nearest of its kind the view shows, the rock's edges
// at the cursor's column and ahead, and zero for what is not sighted.
func TestTheSensorsReadWhatTheViewShows(t *testing.T) {
	w, ctx := soloWorld(16, 10)
	cfg := w.Resources.Config
	cfg.MapWidth, cfg.CameraX = 200, 10
	w.Positions.Resize(cfg.MapWidth, cfg.MapHeight)
	minX, minY, maxX, maxY, _ := cfg.VisibleMapRect()
	width, height := maxX-minX+1, maxY-minY+1
	own := w.Resources.Player.Entity
	w.Components.Energy.SetComponent(own, component.EnergyComponent{Current: -energyHalf})

	cols := make([]engine.RockColumn, cfg.MapWidth)
	for x := range cols {
		cols[x] = engine.RockColumn{Ceil: 2, Floor: cfg.MapHeight - 3}
	}
	cols[16+width/8] = engine.RockColumn{Ceil: 5, Floor: 15}
	cols[16+width/4] = engine.RockColumn{Ceil: 8, Floor: 12}
	w.Positions.SetRock(component.WallBlockCursor, 0, cols)

	// Each kind has one at 9,10, nearer than any the view shows and left of it
	species := func(x, y int, enraged bool) {
		place(w, core.DomainShared, x, y, func(e core.Entity) {
			w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm, IsEnraged: enraged})
		})
	}
	species(30, 10, false)
	species(24, 12, true)
	species(9, 10, true)
	for i, b := range []component.BehaviorComponent{
		{Landing: true, TargetX: 18, TargetY: 6}, {Landing: true, TargetX: 9, TargetY: 10}, {TargetX: 16, TargetY: 10},
	} {
		place(w, core.DomainShared, 150, i, func(e core.Entity) { w.Components.Behavior.SetComponent(e, b) })
	}
	for _, l := range []struct {
		x, y  int
		owner core.Entity
	}{{26, 10, own}, {9, 10, own}, {16, 11, own + 1}} {
		place(w, core.DomainPlayer, l.x, l.y, func(e core.Entity) {
			w.Components.Loot.SetComponent(e, component.LootComponent{Owner: l.owner})
		})
	}

	d, _ := botFor(t, ctx, steering("inputs = [\"column\"]\nhidden = 0"), 0)
	from, _ := w.LocalCursor()
	got := d.mind.look(from, minX, minY, maxX, maxY)
	across := func(cells int) float64 { return float64(cells) / float64(width) }
	down := func(cells int) float64 { return float64(cells) / float64(height) }
	// Nearness points at the thing, rows counted twice, and falls from 1 beside the
	// cursor to nothing a quarter of the view's width away
	near := func(dx, dy int) (float64, float64) {
		r := math.Sqrt(float64(dx*dx + 4*dy*dy))
		pull := max(1-r/(float64(width)/4), 0)
		return pull * float64(dx) / r, pull * float64(2*dy) / r
	}
	want := scene{
		rock:    [3][2]float64{{down(1 - 10), down(cfg.MapHeight - 3 - 10)}, {down(4 - 10), down(15 - 10)}, {down(7 - 10), down(12 - 10)}},
		landing: sighted{x: across(2), y: down(-4)},
		loot:    [2]float64{across(10), 0},
		column:  2*float64(16-minX)/float64(width-1) - 1,
		energy:  0.5,
	}
	want.species[0] = sighted{x: across(8), y: down(2), enraged: 1}
	want.species[1] = sighted{x: across(14)}
	want.species[0].nearX, want.species[0].nearY = near(8, 2)
	want.species[1].nearX, want.species[1].nearY = near(14, 0)
	want.landing.nearX, want.landing.nearY = near(2, -4)
	if got != want {
		t.Fatalf("the sensors read\n%+v, want\n%+v", got, want)
	}
	if on := sight(0, 0, float64(width), float64(height), false); on.nearX != 1 || on.nearY != 0 {
		t.Fatalf("a thing on the cursor's cell reads a nearness of %g, %g, want it beside the cursor on the right", on.nearX, on.nearY)
	}
}

// TestANetworkAddsItsHiddenUnitsToItsDirectTerm: an output is its direct weights
// over the inputs, its weights over the hidden units' tanh, and its bias.
func TestANetworkAddsItsHiddenUnitsToItsDirectTerm(t *testing.T) {
	g, err := ParseGraph("net", []byte(steering(`inputs = ["column", "energy"]
hidden = 1
x = { column = 2.0, bias = 0.25 }
y = { energy = -1.0 }`)))
	if err != nil {
		t.Fatal(err)
	}
	n := g.net.playing()
	copy(n.genes[n.unit(0):], []float64{1, -2, 0.5}) // the unit's two input weights and its bias
	n.genes[n.output(0)+2], n.genes[n.output(1)+2] = 3, -1
	x, y := n.run(&scene{column: 0.5, energy: 0.25})
	unit := math.Tanh(1*0.5 - 2*0.25 + 0.5)
	if wantX, wantY := 2*0.5+3*unit+0.25, -0.25-unit; math.Abs(x-wantX) > 1e-12 || math.Abs(y-wantY) > 1e-12 {
		t.Fatalf("outputs %g, %g, want %g, %g", x, y, wantX, wantY)
	}
	if len(n.genes) != 1*(2+1)+2*(2+1+1) {
		t.Fatalf("%d genes for two inputs, one hidden unit and two outputs", len(n.genes))
	}
}

// TestARoundScoresItsTicksAndWhatItEarnedADefeatAside: a new game closes a round,
// scored by its ticks and its events' worth, a data species' kill its hit points;
// a crush weighs as a defeat does, on the fitness alone, as one ends every round.
func TestARoundScoresItsTicksAndWhatItEarnedADefeatAside(t *testing.T) {
	w, ctx := soloWorld(0, 0)
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
	if st := d.Stats(); st.Rounds != 1 || st.Score != 4+28 || st.Fitness != 28-200+5 {
		t.Fatalf("rounds %d, score %g, fitness %d: want one round of 4 ticks and a kill worth 28, and a fitness of %d",
			st.Rounds, st.Score, st.Fitness, 28-200+5)
	}
}

// memory is a population store that lasts a test
type memory struct{ doc []byte }

func (m *memory) Load() ([]byte, error)  { return m.doc, nil }
func (m *memory) Store(doc []byte) error { m.doc = doc; return nil }

// keeping is a store holding members as a population of layout
func keeping(t *testing.T, layout string, members ...member) *memory {
	t.Helper()
	doc, err := json.Marshal(population{Version: populationVersion, Layout: layout, Members: members})
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
	l, err := NewLearner(g, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	alone := true
	d.Learn(l, func() bool { return alone })
	round := func(origin event.Origin) []float64 {
		rec.dispatch = []event.GameEvent{{Type: event.EventGameResetRequest, Payload: &event.GameResetPayload{}, Origin: origin}}
		if _, err := d.Step(); err != nil {
			t.Fatal(err)
		}
		return d.mind.net.genes
	}
	scored := func() int { return len(l.engine.Snapshot().Members) }

	if !slices.Equal(d.mind.net.genes, g.net.genes) {
		t.Fatal("the first round does not play the hand-written genome")
	}
	if next := round(event.OriginSystem); scored() != 1 || slices.Equal(next, g.net.genes) {
		t.Fatalf("a round played out scored %d genomes and handed on %v, want the hand-written one scored and a child of it next", scored(), next)
	}
	if round(event.OriginCommand); scored() != 1 {
		t.Fatalf("a round an operator ended left %d genomes scored, want it to score nothing", scored())
	}
	alone = false
	if next := round(event.OriginSystem); scored() != 1 || !slices.Equal(next, g.net.genes) {
		t.Fatalf("a bot not alone scored %d genomes and plays %v, want nothing scored and the genome fielded", scored(), next)
	}
}

// TestALearnerKeepsItsPopulationByItsLayout: a population kept under the layout's
// digest comes back with its rounds, and the best member its rounds have proved is
// the one fielded, not the best scored; one of another layout, or none, starts
// over from the hand-written genome. A save says each member's rounds and birth.
func TestALearnerKeepsItsPopulationByItsLayout(t *testing.T) {
	g, d, _ := learning(t)
	genome := func(v float64) []float64 { return slices.Repeat([]float64{v}, len(g.net.genes)) }
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
		"kept":    {kept(g.net.digest), genome(0.2)},
		"another": {kept("another layout"), g.net.genes},
		"none":    {&memory{}, g.net.genes},
	} {
		l, err := NewLearner(g, 1, c.store)
		if err != nil {
			t.Fatal(err)
		}
		d.Learn(l, func() bool { return false })
		if !slices.Equal(d.mind.net.genes, c.want) {
			t.Errorf("%s: fields %v, want %v", name, d.mind.net.genes[:2], c.want[:2])
		}
	}

	store := kept(g.net.digest)
	l, err := NewLearner(g, 1, store)
	if err != nil {
		t.Fatal(err)
	}
	l.turn(0, false, true)
	l.turn(45, true, true)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	var saved population
	if err := json.Unmarshal(store.doc, &saved); err != nil {
		t.Fatal(err)
	}
	entered := slices.IndexFunc(saved.Members, func(m member) bool { return m.Score == 45 })
	if saved.Layout != g.net.digest || saved.Rounds != 1 || len(saved.Members) != 4 || entered < 0 ||
		saved.Members[entered].Born != 1 || saved.Members[entered].Rounds != 1 || saved.Fielded != 2 || saved.Members[2].Born != 5 {
		t.Fatalf("saved %+v: want the three kept with their births, the genome scored 45 born at round 1, and the proved member fielded",
			saved)
	}
}

// TestACandidateThatWouldEnterAFullArchiveIsScoredByItsNextRound: a first round
// good enough to enter only qualifies its genome, which plays again and enters,
// or not, on that round alone, as the first was picked for its luck. The
// hand-written genome plays first, whatever the store kept.
func TestACandidateThatWouldEnterAFullArchiveIsScoredByItsNextRound(t *testing.T) {
	g, _, _ := learning(t)
	// played is the archive after the hand-written genome's rounds score as given,
	// on a store whose population fills it
	played := func(scores ...float64) (first, last []float64, held func(float64) bool) {
		var full []member
		for i := range learnPool {
			full = append(full, member{Genes: slices.Repeat([]float64{float64(i+1) / 100}, len(g.net.genes)), Score: float64(200 - i), Rounds: 1})
		}
		l, err := NewLearner(g, 1, keeping(t, g.net.digest, full...))
		if err != nil {
			t.Fatal(err)
		}
		first = l.turn(0, false, true)
		for _, score := range scores {
			last = l.turn(score, true, true)
		}
		if st := l.Stats(); st.Scored != len(scores) {
			t.Fatalf("%d rounds scored of %d played", st.Scored, len(scores))
		}
		return first, last, func(score float64) bool {
			return slices.ContainsFunc(l.engine.Snapshot().Members, func(c genetic.Candidate[[]float64, float64]) bool { return c.Score == score })
		}
	}

	first, again, held := played(500)
	if !slices.Equal(first, g.net.genes) {
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

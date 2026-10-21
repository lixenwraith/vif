package bot

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
)

// TestABrokenGraphFailsAtLoadNamingTheFault: a bot graph is resolved by name with no
// author watching, so every reference is checked when it is parsed.
func TestABrokenGraphFailsAtLoadNamingTheFault(t *testing.T) {
	const region = "\n[regions.play]\ninitial = \"A\"\n"
	for _, tt := range []struct{ doc, want string }{
		{"[systems]\ndisabled_systems = []" + region + "[states.A]", `unknown table "systems"`},
		{"[bot]\nactions_per_minute = 1" + region + "[states.A]", `unknown [bot] setting "actions_per_minute"`},
		{"[bot]\nactions_per_second = 0" + region + "[states.A]", "actions_per_second must be positive"},
		{region + "file = \"more.toml\"\n[states.A]", "names a file"},
		{region + "enabled_systems = [\"gold\"]\n[states.A]", `"enabled_systems" does nothing in a bot graph`},
		{region + `[states.A]
on_enter = [{ action = "EmitEvent", event = "EventGoldCancel" }]`, "a bot acts only through intents"},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "HasTarget", guard_args = { target = "glyph", whithin = 5 } }]`, `unknown guard arg "whithin"`},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_nowhere" } }]`, `unknown intent action "motion_nowhere"`},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", cnt = 2 } }]`, `unknown payload field "cnt"`},
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
on_enter = [{ action = "Command", payload = { text = " : " } }]`, "command must not be empty"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", count = [] } }]`, "count range must be [min, max]"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", count = [9, 2] } }]`, "count range must be [min, max]"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = ["motion_left", 3] } }]`, "not an action name"},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "Not", guard_args = {} }]`, "Not: 'guard' is not a table"},
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
	w.Positions.ResizeGrid(cfg.MapWidth, cfg.MapHeight)
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
		w.Positions.ResizeGrid(cfg.MapWidth, cfg.MapHeight)
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

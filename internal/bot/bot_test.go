package bot

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
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
on_enter = [{ action = "SetStatusInt", payload = { key = "typing.correct", value = 1 } }]`, "a bot acts only through intents"},
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
transitions = [{ trigger = "Tick", target = "A", guard = "InMode", guard_args = { mode = "replace" } }]`, `unknown mode "replace"`},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", count = [] } }]`, "count range must be [min, max]"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = "motion_left", count = [9, 2] } }]`, "count range must be [min, max]"},
		{region + `[states.A]
on_enter = [{ action = "Intent", payload = { name = ["motion_left", 3] } }]`, "not an action name"},
		{region + `[states.A]
transitions = [{ trigger = "Tick", target = "A", guard = "Not", guard_args = {} }]`, "Not: 'guard' is not a table"},
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

// recorder is an instance that keeps what reaches it
type recorder struct{ got []input.Intent }

func (r *recorder) Tick(int)        {}
func (r *recorder) InputTick() bool { return true }
func (r *recorder) Inject(intents ...*input.Intent) bool {
	for _, intent := range intents {
		r.got = append(r.got, *intent)
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

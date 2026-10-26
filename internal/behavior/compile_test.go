package behavior

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/pkg/machine"
)

const warden = `
initial = "Roam"
suspend = { guard = "Stunned" }

[prototype]
width = 4
height = 2
header = { x = 1, y = 0 }
hp = 40
combat = "swarm"
homing = "swarm"
mass = 2.0
drops = "swarm"
contact = { energy = 1000, heat = 10 }
lifetime_ms = 35000
glyphs = [["▄▀▀▄", "▀▄▄▀"], ["▀▄▄▀", "▄▀▀▄"]]
pattern_ms = 500
color = { r = 200, g = 120, b = 40 }

[states.Roam]
on_update = [{ action = "Home" }]
transitions = [{ trigger = "Tick", guard = "TargetDistance", guard_args = { op = "gt", cells = 12 }, target = "Wind" }]

[states.Wind]
duration_ms = 1500
flags = ["Enraged"]
on_enter = [{ action = "Telegraph", payload = { color = "cyan" } }]
on_update = [{ action = "Freeze" }]
on_exit = [{ action = "CancelTelegraph" }]
cues = [{ at = 800, action = "Materialize", payload = { spawn = "swarm" } }]
transitions = [{ trigger = "Timeout", target = ["Strike", "Roam"] }]

[states.Strike]
duration_ms = 800
flags = ["Shielded"]
probe = "ResolveTarget"
on_enter = [{ action = "StoreTarget" }]
on_update = [{ action = "ChargeToStored" }]
transitions = [{ trigger = "Timeout", target = "Roam" }, { trigger = "AfterTick", guard = "HitWall", target = "Roam" }]

[[global]]
from = ["Wind", "Strike"]
guard = "Stunned"
target = "Roam"
`

const (
	flagEnraged = 1 << iota
	flagShielded
)

// testCtx is what the test verbs read and the log of what they ran
type testCtx struct {
	calls    []string
	distance int
	target   bool
	stunned  bool
	hitWall  bool
}

func testVocabulary() *Vocabulary[*testCtx] {
	v := NewVocabulary[*testCtx]()
	type distance struct {
		Op    dialect.Op `toml:"op"`
		Cells int        `toml:"cells"`
	}
	v.Predicate("TargetDistance", func(a *distance) (func(*testCtx) bool, error) {
		return func(c *testCtx) bool { return a.Op.Holds(c.distance, a.Cells) }, nil
	})
	reads := func(f func(*testCtx) bool) func(*struct{}) (func(*testCtx) bool, error) {
		return func(*struct{}) (func(*testCtx) bool, error) { return f, nil }
	}
	v.Predicate("Stunned", reads(func(c *testCtx) bool { return c.stunned }))
	v.Predicate("HitWall", reads(func(c *testCtx) bool { return c.hitWall }))
	v.Probe("ResolveTarget", reads(func(c *testCtx) bool { return c.target }))

	record := func(name string) func(*struct{}) (func(*testCtx), error) {
		return func(*struct{}) (func(*testCtx), error) {
			return func(c *testCtx) { c.calls = append(c.calls, name) }, nil
		}
	}
	type telegraph struct {
		Color string `toml:"color"`
	}
	v.Effect("Telegraph", func(a *telegraph) (func(*testCtx), error) {
		return func(c *testCtx) { c.calls = append(c.calls, "Telegraph "+a.Color) }, nil
	})
	type materialize struct {
		Spawn string `toml:"spawn"`
	}
	v.Effect("Materialize", func(a *materialize) (func(*testCtx), error) {
		return func(c *testCtx) { c.calls = append(c.calls, "Materialize "+a.Spawn) }, nil
	})
	v.Effect("CancelTelegraph", record("CancelTelegraph"))
	v.Effect("StoreTarget", record("StoreTarget"))
	v.Sustained("Home", record("Home"))
	v.Sustained("ChargeToStored", record("ChargeToStored"))
	v.Sustained("Freeze", record("Freeze"))
	v.Flag("Enraged", flagEnraged)
	v.Flag("Shielded", flagShielded)
	v.Integrate(func(c *testCtx) { c.calls = append(c.calls, "integrate") })
	v.Entered(func(c *testCtx) { c.calls = append(c.calls, "entered") })
	return v
}

func parse(t *testing.T, src string) map[string]any {
	t.Helper()
	doc, err := toml.NewParser([]byte(src)).Parse()
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestProgramsTakeIDsInSortedNameOrder(t *testing.T) {
	set, err := Compile(testVocabulary(), map[string]map[string]any{"b": parse(t, warden), "a": parse(t, warden)})
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Names(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("names %v, want [a b]", got)
	}
	for i, p := range set.Prototypes() {
		if want := component.SpeciesDataFirst + component.SpeciesType(i); p.Species != want {
			t.Errorf("program %s is species %d, want %d", p.Name, p.Species, want)
		}
	}
	g := set.Graph(0)
	for want, name := range []string{"Roam", "Strike", "Wind"} {
		if got := g.Name(component.BehaviorState(want)); got != name {
			t.Errorf("state %d is %q, want %q", want, got, name)
		}
	}
	if set.Initial(0) != 0 {
		t.Errorf("initial %d, want Roam (0)", set.Initial(0))
	}
}

// A step runs the program's edges in the machine's phases with the test verbs:
// each line is what one step ran and the state it ended in. Every entry, the
// start's included, ends with the host's entered hook.
func TestACompiledProgramStepsAsItsStatesSay(t *testing.T) {
	set, err := Compile(testVocabulary(), map[string]map[string]any{"warden": parse(t, warden)})
	if err != nil {
		t.Fatal(err)
	}
	g := set.Graph(0)
	var in machine.Instance[component.BehaviorState]
	c := &testCtx{}
	g.Start(&in, c, set.Initial(0), 0)
	if !slices.Equal(c.calls, []string{"entered"}) {
		t.Errorf("start ran %v, want the entered hook", c.calls)
	}
	step := func(dt time.Duration, setup func(), want string) {
		t.Helper()
		setup()
		c.calls = c.calls[:0]
		flags, suspended := g.Step(&in, c, dt)
		got := fmt.Sprintf("%s -> %s flags=%d suspended=%v", strings.Join(c.calls, ", "), g.Name(in.State), flags, suspended)
		if got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
	}
	ms := time.Millisecond
	step(100*ms, func() {}, "Home, integrate -> Roam flags=0 suspended=false")
	step(100*ms, func() { c.distance = 13 }, "Telegraph cyan, entered, Freeze, integrate -> Wind flags=1 suspended=false")
	step(1100*ms, func() {}, "Freeze, integrate -> Wind flags=1 suspended=false")
	step(100*ms, func() {}, "Materialize swarm, Freeze, integrate -> Wind flags=1 suspended=false")
	step(200*ms, func() {}, "Freeze, integrate -> Wind flags=1 suspended=false")
	step(100*ms, func() { c.distance = 0 }, "CancelTelegraph, entered, Home, integrate -> Roam flags=0 suspended=false")
	step(100*ms, func() { c.distance = 13 }, "Telegraph cyan, entered, Freeze, integrate -> Wind flags=1 suspended=false")
	step(1500*ms, func() { c.target = true },
		"Materialize swarm, CancelTelegraph, StoreTarget, entered, ChargeToStored, integrate -> Strike flags=2 suspended=false")
	step(100*ms, func() { c.hitWall = true }, "ChargeToStored, integrate, entered -> Roam flags=0 suspended=false")
	step(100*ms, func() { c.hitWall = false }, "Telegraph cyan, entered, Freeze, integrate -> Wind flags=1 suspended=false")
	step(100*ms, func() { c.stunned = true }, "CancelTelegraph, entered -> Roam flags=0 suspended=true")
	step(100*ms, func() {}, " -> Roam flags=0 suspended=true")
}

// Every fault is an error that names its program, where it sits and the name or
// key at fault; a program with several faults reports each on its own line
func TestACompileErrorNamesProgramPlaceAndKey(t *testing.T) {
	cases := []struct {
		name      string
		old, new  string
		want      string
		wantLines int
	}{
		{"sustained", `action = "Home"`, `action = "Hover"`, "program 'warden': state 'Roam': on_update[0]: unknown sustained action 'Hover'", 1},
		{"effect", `action = "Materialize"`, `action = "Materialise"`, "program 'warden': state 'Wind': cues[0]: unknown effect 'Materialise'", 1},
		{"flag", `flags = ["Enraged"]`, `flags = ["Angry"]`, "program 'warden': state 'Wind': flags: unknown flag 'Angry', not one of Enraged, Shielded", 1},
		{"state", `from = ["Wind", "Strike"]`, `from = ["Wind", "Strik"]`, "program 'warden': global[0]: from: unknown state 'Strik'", 1},
		{"target", `target = ["Strike", "Roam"]`, `target = ["Strike", "Rome"]`, "program 'warden': state 'Wind': transitions[0]: target[1]: unknown state 'Rome'", 1},
		{"key", `on_update = [{ action = "Home" }]`, `on_update = [{ action = "Home", delay_ms = 5 }]`, `program 'warden': unknown key "states.Roam.on_update.delay_ms"`, 1},
		{"payload key", `payload = { color = "cyan" }`, `payload = { colour = "cyan" }`, `program 'warden': state 'Wind': on_enter[0]: action 'Telegraph': payload: unknown key "colour"`, 1},
		{"no payload", `action = "CancelTelegraph"`, `action = "CancelTelegraph", payload = { now = true }`, "program 'warden': state 'Wind': on_exit[0]: action 'CancelTelegraph' takes no payload", 1},
		{"guard arg", `op = "gt"`, `op = "far"`, `program 'warden': state 'Roam': transitions[0]: guard 'TargetDistance': op: "far" is not one of`, 1},
		{"probe as guard", `guard = "HitWall"`, `guard = "ResolveTarget"`, "program 'warden': state 'Strike': transitions[1]: guard 'ResolveTarget': probe verb, not a guard", 1},
		{"effect on update", `on_update = [{ action = "Home" }]`, `on_update = [{ action = "StoreTarget" }]`, "program 'warden': state 'Roam': on_update[0]: action 'StoreTarget' is an effect", 1},
		{"sustained on enter", `on_enter = [{ action = "StoreTarget" }]`, `on_enter = [{ action = "Home" }]`, "program 'warden': state 'Strike': on_enter[0]: action 'Home' is sustained", 1},
		{"guarded timeout", `{ trigger = "Timeout", target = "Roam" }`, `{ trigger = "Timeout", guard = "HitWall", target = "Roam" }`, "program 'warden': state 'Strike': transitions[0]: a Timeout takes no guard", 1},
		{"trigger", `trigger = "AfterTick"`, `trigger = "OnTick"`, "program 'warden': state 'Strike': transitions[1]: trigger 'OnTick' is not Timeout, Tick or AfterTick", 1},
		{"fallback", `target = ["Strike", "Roam"]`, `target = ["Strike"]`, `program 'warden': machine: timeout edge 1 from state "Wind": every candidate can fail and no fallback is declared`, 1},
		{"combat row", `combat = "swarm"`, `combat = "drain"`, `program 'warden': prototype: combat "drain" is not one of swarm, quasar`, 1},
		{"homing", `homing = "swarm"`, `homing = "bee"`, `program 'warden': prototype: homing "bee" is not one of drain, quasar, snake, swarm`, 1},
		{"drops", `drops = "swarm"`, `drops = "tower"`, `program 'warden': prototype: drops "tower": that species drops nothing`, 1},
		{"glyph width", `["▄▀▀▄", "▀▄▄▀"], [`, `["▄▀▀", "▀▄▄▀"], [`, `program 'warden': prototype: glyphs[0][0] "▄▀▀" has 3 cells, want width 4`, 1},
		{"header", `header = { x = 1, y = 0 }`, `header = { x = 4, y = 0 }`, "program 'warden': prototype: header {4, 0} outside the 4×2 footprint", 1},
		{"two faults", "hp = 40\ncombat = \"swarm\"", "hp = 0\ncombat = \"drain\"", "program 'warden': prototype: hp 0 is not positive", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(warden, tc.old, tc.new, 1)
			if src == warden {
				t.Fatalf("%q is not in the program", tc.old)
			}
			_, err := Compile(testVocabulary(), map[string]map[string]any{"warden": parse(t, src)})
			if err == nil {
				t.Fatal("compiled")
			}
			lines := strings.Split(err.Error(), "\n")
			if !strings.Contains(err.Error(), tc.want) || len(lines) != tc.wantLines {
				t.Fatalf("error %q\nwant %d line(s) holding %q", err, tc.wantLines, tc.want)
			}
			for _, l := range lines {
				if !strings.HasPrefix(l, "program 'warden': ") {
					t.Errorf("line %q does not name its program", l)
				}
			}
		})
	}
}

func TestAProgramNameKeepsToTheRules(t *testing.T) {
	doc := parse(t, warden)
	nine := map[string]map[string]any{}
	for i := range 9 {
		nine[fmt.Sprintf("w%d", i)] = doc
	}
	for _, tc := range []struct {
		programs map[string]map[string]any
		want     string
	}{
		{nine, "9 behavior programs, at most 8"},
		{map[string]map[string]any{"Warden": doc}, `program name "Warden" is not [a-z][a-z0-9_]*`},
		{map[string]map[string]any{"swarm": doc}, `program name "swarm" is a code species`},
		{map[string]map[string]any{"total": doc}, `program name "total" is reserved`},
	} {
		if _, err := Compile(testVocabulary(), tc.programs); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("error %v, want %q", err, tc.want)
		}
	}
}

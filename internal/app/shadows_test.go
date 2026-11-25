package app

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestShadowsWalkedToItsGoalLightsTheMazeAndLaysTheNext plays the shipped shadows
// headless: the cursor stands in the dark far from the goal, the walk along the
// maze's way reaches it, the maze lights with the walk's trail, and a key lays the
// next maze in the dark.
func TestShadowsWalkedToItsGoalLightsTheMazeAndLaysTheNext(t *testing.T) {
	t.Parallel()
	opts := resource.Options{Dir: soakRoot, Scenario: "shadows"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("shadows scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 90, Height: 32, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	w := a.World()
	var at, goal vmath.Point
	var sight engine.SightState
	read := func() (placed bool) {
		w.RunSafe(func() {
			pos, ok := w.LocalCursor()
			at, placed = vmath.Point{X: pos.X, Y: pos.Y}, ok
			goal = vmath.Point{X: int(w.Resources.Scenario.Var("goal_x")), Y: int(w.Resources.Scenario.Var("goal_y"))}
			sight = w.Resources.View.Sight
		})
		return placed
	}
	for range 100 {
		if read() && sight.Dark {
			break
		}
		a.Tick(1)
	}
	if !sight.Dark || at == goal || goal == (vmath.Point{}) {
		t.Fatalf("the game stands with the cursor at %v, the goal at %v, sight %+v: want a dark maze to cross", at, goal, sight)
	}

	// The blocks' way to the goal, as the walk takes it
	open := func(b vmath.Point) (free bool) {
		w.RunSafe(func() {
			free = !w.Positions.IsBlocked(vmath.Point{X: b.X*5 + 2, Y: b.Y*3 + 1}, component.WallBlockCursor)
		})
		return free
	}
	ways := []struct {
		d vmath.Point
		m input.MotionOp
	}{{vmath.Point{X: 1}, input.MotionRight}, {vmath.Point{X: -1}, input.MotionLeft}, {vmath.Point{Y: 1}, input.MotionDown}, {vmath.Point{Y: -1}, input.MotionUp}}
	from, to := vmath.Point{X: at.X / 5, Y: at.Y / 3}, vmath.Point{X: goal.X / 5, Y: goal.Y / 3}
	came := map[vmath.Point]vmath.Point{from: from}
	for queue := []vmath.Point{from}; len(queue) > 0; queue = queue[1:] {
		for _, way := range ways {
			if n := (vmath.Point{X: queue[0].X + way.d.X, Y: queue[0].Y + way.d.Y}); open(n) {
				if _, seen := came[n]; !seen {
					came[n], queue = queue[0], append(queue, n)
				}
			}
		}
	}
	if _, reached := came[to]; !reached {
		t.Fatalf("no way through the maze from %v to the goal %v", at, goal)
	}
	var path []vmath.Point
	for b := to; b != from; b = came[b] {
		path = append([]vmath.Point{b}, path...)
	}
	prev := from
	for _, b := range path {
		step := vmath.Point{X: b.X - prev.X, Y: b.Y - prev.Y}
		for _, way := range ways {
			if way.d == step {
				a.Inject(&input.Intent{Type: input.IntentMotion, Motion: way.m, Count: 1})
			}
		}
		want := vmath.Point{X: b.X*5 + 2, Y: b.Y*3 + 1}
		for i := 0; i < 20 && at != want; i++ {
			a.Tick(1)
			a.InputTick()
			read()
		}
		if at != want {
			t.Fatalf("the walk to block %v stands at %v, want %v", b, at, want)
		}
		prev = b
	}
	a.Tick(2)
	if read(); sight.Dark || !sight.Trail || !a.Context().KeyCapture.Has('x') {
		t.Fatalf("at the goal the sight is %+v and x captured %t: want the maze lit with the trail, keys taken",
			sight, a.Context().KeyCapture.Has('x'))
	}

	a.Tick(12)
	a.Inject(a.inputMachine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'x'}))
	for i := 0; i < 50 && (!sight.Dark || at == goal); i++ {
		a.Tick(1)
		read()
	}
	if !sight.Dark || at == goal {
		t.Fatalf("after a key the cursor stands at %v by the goal %v, sight %+v: want a new dark maze", at, goal, sight)
	}
}

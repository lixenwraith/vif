package app

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestMagicFightsThroughACursorFixedOnItsSquare plays the shipped magic on its
// smallest map: the cursor stands on the square's middle under a shield that covers
// the square, each side line faces three gateways and each middle one two, and
// neither a motion nor a pointer press moves the cursor.
func TestMagicFightsThroughACursorFixedOnItsSquare(t *testing.T) {
	t.Parallel()
	opts := resource.Options{Dir: soakRoot, Scenario: "magic"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("magic scenario not present")
	}
	sc, err := resource.LoadScenario(opts)
	if err != nil {
		t.Fatalf("scenario: %v", err)
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: sc.MapMinWidth, Height: sc.MapMinHeight, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	w := a.World()
	var sq component.SquareComponent
	var cursor core.Entity
	var at vmath.Point
	var radius float64
	var faced [component.SquareLines]int
	read := func() {
		w.RunSafe(func() {
			for _, e := range w.Components.Square.Entities() {
				sq, _ = w.Components.Square.GetComponent(e)
			}
			cursor = w.Resources.Player.Slot(0)
			at, _ = w.Positions.GetPosition(cursor)
			if shield, ok := w.Components.Shield.GetPtr(cursor); ok {
				radius = shield.Radius
			}
			faced = [component.SquareLines]int{}
			w.Components.Gateway.Each(func(_ core.Entity, g *component.GatewayComponent) bool {
				gate, _ := w.Positions.GetPosition(g.AnchorEntity)
				for l := range faced {
					if sq.Faces(l, gate.X, gate.Y) {
						faced[l]++
					}
				}
				return true
			})
		})
	}
	want := [component.SquareLines]int{3, 2, 3, 3, 2, 3}
	for range 100 {
		if read(); faced == want {
			break
		}
		a.Tick(1)
	}
	if faced != want {
		t.Fatalf("the lines face %v gateways, want three a side line and two a middle one", faced)
	}
	cx, cy := sq.Center()
	if at.X != cx || at.Y != cy {
		t.Fatalf("the cursor stands at (%d,%d), want the square's middle (%d,%d)", at.X, at.Y, cx, cy)
	}
	if cover := sq.ShieldRadius(w.Resources.Config.CellAspect); radius != cover {
		t.Fatalf("the shield reaches %.2f columns, want the %.2f that cover the square", radius, cover)
	}

	a.Inject(&input.Intent{Type: input.IntentMotion, Motion: input.MotionRight, Count: 5},
		&input.Intent{Type: input.IntentMouseLeftDown, X: 3, Y: 3, MapCell: true, Count: 1},
		&input.Intent{Type: input.IntentMouseLeftUp})
	a.Tick(2)
	read()
	if at.X != cx || at.Y != cy {
		t.Fatalf("a motion and a pointer press moved the cursor to (%d,%d) from (%d,%d)", at.X, at.Y, cx, cy)
	}
}

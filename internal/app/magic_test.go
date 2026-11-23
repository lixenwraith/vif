package app

import (
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// newMagic starts the shipped magic headless on its smallest map and plays it until
// its keys are the square's
func newMagic(t *testing.T) *App {
	t.Helper()
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
	t.Cleanup(a.Close)
	for range 100 {
		if a.Context().KeyCapture.Has('h') {
			return a
		}
		a.Tick(1)
	}
	t.Fatal("magic never took its keys")
	return nil
}

// TestMagicFightsThroughACursorFixedOnItsSquare plays the shipped magic on its
// smallest map: the cursor stands on the square's middle under a shield that covers
// the square, each side line faces three gateways and each middle one two, and
// neither a motion nor a pointer press moves the cursor.
func TestMagicFightsThroughACursorFixedOnItsSquare(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
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

// TestMagicAnswersAHeatBurstWithADeathWave: heat filling its overheat in the shipped
// magic runs a death wave out from the cursor, and leaves the meter full with the
// ember main lights put out
func TestMagicAnswersAHeatBurstWithADeathWave(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	cursor := w.Resources.Player.Slot(0)
	a.Context().PushEventOrigin(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Entity: cursor, Delta: parameter.HeatMaxOverheat}, event.OriginDebug)
	a.Settle()
	a.Tick(3)
	var heat component.HeatComponent
	waves := 0
	w.RunSafe(func() {
		heat, _ = w.Components.Heat.GetComponent(cursor)
		w.Components.Pulse.Each(func(_ core.Entity, p *component.PulseComponent) bool {
			if p.Attack == component.CombatAttackDeathWave && p.Owner == cursor {
				waves++
			}
			return true
		})
	})
	if waves != 1 || heat.Current != parameter.HeatMax || heat.Overheat != 0 || heat.EmberActive {
		t.Fatalf("after the burst %d death waves run, over heat %d+%d with the ember lit %t; want one, 100+0 and no ember",
			waves, heat.Current, heat.Overheat, heat.EmberActive)
	}
}

// TestMagicsTurnsDriveItsMusic: the shipped magic leaves the music on, and the keys
// that turn its square count toward the tempo the music follows
func TestMagicsTurnsDriveItsMusic(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	for _, s := range w.Systems() {
		if toggled, ok := s.(engine.Toggled); ok && s.Name() == "music" && !toggled.Enabled() {
			t.Fatal("magic runs with its music system off")
		}
	}
	for _, k := range "2h3l" {
		a.Inject(a.inputMachine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: k}))
	}
	a.Tick(int(2 * time.Second / parameter.GameUpdateInterval))
	if apm := w.Resources.Status.Ints.Get("engine.music_apm").Load(); apm == 0 {
		t.Fatal("four turns of the square left the music's tempo at rest")
	}
}

// TestMagicHoldsItsMapThroughAResize: a resize leaves the shipped magic's map and
// its eight gateways as they stood, a screen smaller than the map looks at the
// square from its middle, and the next game takes the screen's size
func TestMagicHoldsItsMapThroughAResize(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	cfg := w.Resources.Config
	var mapW, mapH, viewW, viewH, gates int
	var seen vmath.Point
	read := func() {
		w.RunSafe(func() {
			mapW, mapH, viewW, viewH = cfg.MapWidth, cfg.MapHeight, cfg.ViewportWidth, cfg.ViewportHeight
			gates = w.Components.Gateway.CountEntities()
			at, _ := w.Positions.GetPosition(w.Resources.Player.Slot(0))
			seen = vmath.Point{X: at.X - cfg.CameraX, Y: at.Y - cfg.CameraY}
		})
	}
	open := func() {
		t.Helper()
		for range 200 {
			if read(); gates == 8 {
				return
			}
			a.Tick(1)
		}
		t.Fatalf("%d gateways opened, want eight", gates)
	}
	open()
	wasW, wasH := mapW, mapH

	a.Resize(60, 30)
	a.Tick(2)
	read()
	if mapW != wasW || mapH != wasH || gates != 8 {
		t.Fatalf("a smaller screen left a %dx%d map with %d gateways, want the %dx%d it was and eight", mapW, mapH, gates, wasW, wasH)
	}
	if dx, dy := seen.X-(viewW-1)/2, seen.Y-(viewH-1)/2; dx < -1 || dx > 1 || dy < -1 || dy > 1 {
		t.Fatalf("the square's middle shows at (%d,%d) of a %dx%d view, want its middle", seen.X, seen.Y, viewW, viewH)
	}

	a.Resize(140, 60)
	a.Tick(2)
	if read(); mapW != wasW || mapH != wasH || gates != 8 {
		t.Fatalf("a larger screen left a %dx%d map with %d gateways, want the %dx%d it was and eight", mapW, mapH, gates, wasW, wasH)
	}
	a.Reset(false)
	a.Tick(2)
	open()
	if mapW != viewW || mapH != viewH {
		t.Fatalf("the next game plays a %dx%d map on a %dx%d view, want the view's size", mapW, mapH, viewW, viewH)
	}
}

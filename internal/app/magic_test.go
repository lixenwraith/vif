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
	return newMagicOn(t, 0, 0)
}

// newMagicOn is newMagic on a screen of width by height, the smallest map's when zero
func newMagicOn(t *testing.T, width, height int) *App {
	t.Helper()
	opts := resource.Options{Dir: soakRoot, Scenario: "magic"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("magic scenario not present")
	}
	sc, err := resource.LoadScenario(opts)
	if err != nil {
		t.Fatalf("scenario: %v", err)
	}
	if width == 0 {
		width, height = sc.MapMinWidth, sc.MapMinHeight
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: width, Height: height, Resources: opts})
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
// the square, each edge faces the two gateways on its side's corners, and neither a
// motion nor a pointer press moves the cursor.
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
					if sq.Faces(l)(gate.X, gate.Y) {
						faced[l]++
					}
				}
				return true
			})
		})
	}
	want := [component.SquareLines]int{2, 2, 2, 2}
	for range 100 {
		if read(); faced == want {
			break
		}
		a.Tick(1)
	}
	if faced != want {
		t.Fatalf("the lines face %v gateways, want two an edge", faced)
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

// TestMagicsWeaponsWearEveryBody: no body the shipped magic declares refuses the
// damage of a weapon's family or of a blast
func TestMagicsWeaponsWearEveryBody(t *testing.T) {
	t.Parallel()
	w := newMagic(t).World()
	families := []component.CombatAttackType{component.CombatAttackExplosion}
	for _, spec := range component.WeaponSpecs {
		families = append(families, spec.Attack)
	}
	for _, body := range w.Resources.Behaviors {
		for _, atk := range families {
			if body.Attacks[atk].Damage <= 0 {
				t.Errorf("%s takes %d from %s", body.Name, body.Attacks[atk].Damage, component.CombatAttackNames[atk])
			}
		}
	}
}

// TestMagicKeepsAHeatBurstForEnter: heat filling its overheat in the shipped magic
// leaves the overheat full with the ember that fighter lights put out, whole through
// what special fire spends, and Enter then runs a death wave out from the cursor
// and empties it
func TestMagicKeepsAHeatBurstForEnter(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	cursor := w.Resources.Player.Slot(0)
	var heat component.HeatComponent
	waves := func() (n int) {
		w.RunSafe(func() {
			heat, _ = w.Components.Heat.GetComponent(cursor)
			w.Components.Pulse.Each(func(_ core.Entity, p *component.PulseComponent) bool {
				if p.Attack == component.CombatAttackDeathWave && p.Owner == cursor {
					n++
				}
				return true
			})
		})
		return n
	}
	enter := func() {
		a.Inject(a.inputMachine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEnter}))
		a.Tick(3)
	}
	if enter(); waves() != 0 {
		t.Fatal("Enter ran a death wave with no overheat to spend")
	}
	a.Context().PushEventOrigin(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Entity: cursor, Delta: parameter.HeatMaxOverheat}, event.OriginDebug)
	a.Settle()
	a.Tick(3)
	if n := waves(); n != 0 || heat.Current != parameter.HeatMax || heat.Overheat != parameter.HeatMaxOverheat || heat.EmberActive {
		t.Fatalf("a filled overheat runs %d death waves over heat %d+%d with the ember lit %t; want none, 100+100 and no ember",
			n, heat.Current, heat.Overheat, heat.EmberActive)
	}
	// What a dust blast spends is given back, so the burst stays whole
	a.Context().PushEventOrigin(event.EventHeatSpendRequest, &event.HeatSpendRequestPayload{Entity: cursor, Amount: parameter.SpecialAttackHeatCost}, event.OriginDebug)
	a.Settle()
	a.Tick(2)
	if enter(); waves() != 1 || heat.Current != parameter.HeatMax || heat.Overheat != 0 {
		t.Fatalf("Enter on a full overheat runs %d death waves and leaves heat %d+%d; want one and 100+0", waves(), heat.Current, heat.Overheat)
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

// magicLayout is where the shipped magic stands its things: the map, the square's
// corner, the cursor and the gateways' pylons in the order they were stood
type magicLayout struct {
	mapW, mapH int
	square     vmath.Point
	cursor     vmath.Point
	gates      [4]vmath.Point
}

// layMagic plays a magic until its four gateways stand and reads its layout
func layMagic(t *testing.T, a *App) (lay magicLayout) {
	t.Helper()
	w := a.World()
	for range 200 {
		n := 0
		w.RunSafe(func() {
			lay.mapW, lay.mapH = w.Resources.Config.MapWidth, w.Resources.Config.MapHeight
			for _, e := range w.Components.Square.Entities() {
				sq, _ := w.Components.Square.GetComponent(e)
				lay.square = vmath.Point{X: sq.X, Y: sq.Y}
			}
			at, _ := w.Positions.GetPosition(w.Resources.Player.Slot(0))
			lay.cursor = vmath.Point{X: at.X, Y: at.Y}
			for _, e := range w.Components.Pylon.Entities() {
				if at, ok := w.Positions.GetPosition(e); ok && n < len(lay.gates) {
					lay.gates[n] = vmath.Point{X: at.X, Y: at.Y}
					n++
				}
			}
		})
		if n == len(lay.gates) {
			return lay
		}
		a.Tick(1)
	}
	t.Fatal("magic never stood its four gateways")
	return lay
}

// TestMagicKeepsItsLayoutThroughAResize: the shipped magic's map follows a resize to
// any screen that holds its smallest map, and its gateways, square and cursor then
// stand as a new game on that screen stands them, the game running on; a smaller
// screen leaves the smallest map and looks at the square from its middle
func TestMagicKeepsItsLayoutThroughAResize(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	layMagic(t, a)
	for _, screen := range [][2]int{{140, 60}, {100, 46}, {180, 48}} {
		a.Resize(screen[0], screen[1])
		a.Tick(2)
		got, want := layMagic(t, a), layMagic(t, newMagicOn(t, screen[0], screen[1]))
		if got != want {
			t.Fatalf("resized to %dx%d magic stands %+v, want a new game's %+v", screen[0], screen[1], got, want)
		}
	}
	if tick := w.Resources.Game.State.GetGameTicks(); tick < 6 {
		t.Fatalf("the game stands at tick %d after three resizes, want it running on", tick)
	}

	small := layMagic(t, newMagic(t))
	a.Resize(60, 30)
	a.Tick(2)
	var seen, view vmath.Point
	w.RunSafe(func() {
		cfg := w.Resources.Config
		at, _ := w.Positions.GetPosition(w.Resources.Player.Slot(0))
		seen, view = vmath.Point{X: at.X - cfg.CameraX, Y: at.Y - cfg.CameraY}, vmath.Point{X: cfg.ViewportWidth, Y: cfg.ViewportHeight}
	})
	if got := layMagic(t, a); got != small {
		t.Fatalf("a screen under the smallest map leaves magic at %+v, want the smallest map's %+v", got, small)
	}
	if dx, dy := seen.X-(view.X-1)/2, seen.Y-(view.Y-1)/2; dx < -1 || dx > 1 || dy < -1 || dy > 1 {
		t.Fatalf("the square's middle shows at (%d,%d) of a %dx%d view, want its middle", seen.X, seen.Y, view.X, view.Y)
	}
}

// TestMagicHoldsItsGatewaysOnceACrowdStands: with as many bodies on the map as the
// shipped magic lets gather, every gateway holds its spawns
func TestMagicHoldsItsGatewaysOnceACrowdStands(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	layMagic(t, a)
	held := func() (n int) {
		w.RunSafe(func() {
			w.Components.Gateway.Each(func(_ core.Entity, g *component.GatewayComponent) bool {
				if g.Held > 0 {
					n++
				}
				return true
			})
		})
		return n
	}
	if n := held(); n != 0 {
		t.Fatalf("%d gateways hold over an empty map", n)
	}
	for i := range 300 {
		a.Context().PushEventOrigin(event.EventBehaviorSpawnRequest,
			&event.BehaviorSpawnRequestPayload{Program: "spark", X: 2 + i%70, Y: 2 + i%30}, event.OriginDebug)
	}
	a.Settle()
	a.Tick(2)
	if n := held(); n != 4 {
		t.Fatalf("%d gateways hold with 300 bodies on the map, want all four", n)
	}
}

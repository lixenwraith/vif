package app

import (
	"fmt"
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

// newMagicOn is newMagic on a screen of width by height, the one showing the
// smallest map whole when zero
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
		width, height = sc.MapMinWidth+parameter.LeftMargin, sc.MapMinHeight+parameter.TopMargin+parameter.BottomMargin
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
	var faced [component.SquareEdges]int
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
			faced = [component.SquareEdges]int{}
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
	want := [component.SquareEdges]int{2, 2, 2, 2}
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

// TestMagicsBurstFillsByHeatAndHitsForEnter: in the shipped magic each hit the
// cursor's shield takes and each heat it gains fill its burst meter, overheat
// bursting with no ember, and Enter runs a death wave out from the cursor on a full
// meter alone, emptying it
func TestMagicsBurstFillsByHeatAndHitsForEnter(t *testing.T) {
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
	push := func(et event.EventType, p any) {
		a.Context().PushEventOrigin(et, p, event.OriginDebug)
		a.Settle()
	}
	hit := func(n int) {
		for range n {
			push(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{Entity: cursor, Value: -1})
		}
	}
	short := parameter.BurstFull - parameter.BurstPerShieldHit
	hit(short / parameter.BurstPerShieldHit)
	if enter(); waves() != 0 || heat.Burst != short {
		t.Fatalf("Enter on a burst a hit short runs %d death waves and leaves it at %d; want none at %d", waves(), heat.Burst, short)
	}
	hit(1)
	if enter(); waves() != 1 || heat.Burst != 0 {
		t.Fatalf("Enter on a burst filled by hits runs %d death waves and leaves it at %d; want one and empty", waves(), heat.Burst)
	}
	push(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Entity: cursor, Delta: parameter.HeatMaxOverheat})
	if enter(); waves() != 2 || heat.Burst != 0 || heat.EmberActive {
		t.Fatalf("Enter on a burst filled by %d heat, which burst the overheat, ran %d death waves in all, left it at %d with the ember lit %t; want two, empty, no ember",
			parameter.HeatMaxOverheat, waves(), heat.Burst, heat.EmberActive)
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
// corner and its blocks' rows, the cursor and the gateways' pylons in the order
// they were stood
type magicLayout struct {
	mapW, mapH int
	square     vmath.Point
	rows       int
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
				lay.square, lay.rows = vmath.Point{X: sq.X, Y: sq.Y}, sq.Height
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
	for _, screen := range [][2]int{{140, 60}, {130, 30}, {80, 24}, {100, 46}, {180, 26}} {
		a.Resize(screen[0], screen[1])
		a.Tick(2)
		got, want := layMagic(t, a), layMagic(t, newMagicOn(t, screen[0], screen[1]))
		if got != want {
			t.Fatalf("resized to %dx%d magic stands %+v, want a new game's %+v", screen[0], screen[1], got, want)
		}
	}
	if tick := w.Resources.Game.State.GetGameTicks(); tick < 10 {
		t.Fatalf("the game stands at tick %d after five resizes, want it running on", tick)
	}

	small := layMagic(t, newMagic(t))
	a.Resize(40, 20)
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

// TestMagicShowsItsWholeLayoutOnEveryScreenDownToItsFloor: on every screen down to
// the one its smallest map fills, tall or wide, the shipped magic shows that layout
// whole (magicOnScreen), and on a map a column or a row under its floor it cannot
func TestMagicShowsItsWholeLayoutOnEveryScreenDownToItsFloor(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	floor := layMagic(t, a)
	// The sweep plays minutes on: a shield that never falls keeps one game standing
	w.RunSafe(func() {
		w.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: w.Resources.Player.Slot(0), Value: parameter.GodEnergyAmount})
	})
	least := func(mapW, mapH int) (int, int) {
		return mapW + parameter.LeftMargin, mapH + parameter.TopMargin + parameter.BottomMargin
	}
	minW, minH := least(floor.mapW, floor.mapH)
	widths := []int{129, 130, 131, 200, 320}
	for width := minW; width <= 100; width++ {
		widths = append(widths, width)
	}
	for height := minH; height <= 64; height++ {
		for _, width := range widths {
			a.Resize(width, height)
			a.Tick(2)
			w.RunSafe(func() {
				if err := magicOnScreen(w); err != "" {
					t.Errorf("on a %dx%d screen %s", width, height, err)
				}
			})
		}
	}
	w.RunSafe(func() { w.Resources.Config.MapMinWidth, w.Resources.Config.MapMinHeight = 1, 1 })
	for _, under := range [][2]int{{floor.mapW - 1, floor.mapH}, {floor.mapW, floor.mapH - 1}} {
		a.Resize(least(under[0], under[1]))
		a.Tick(2)
		w.RunSafe(func() {
			if magicOnScreen(w) == "" {
				t.Errorf("a %dx%d map holds the whole layout, under the %dx%d floor", under[0], under[1], floor.mapW, floor.mapH)
			}
		})
	}
}

// magicOnScreen says what of the magic's layout the screen does not show whole,
// empty when the map is its game area, the cursor's orbs and the outer orbit circle
// on it, the blocks as tall as that leaves room for, and a pylon stands in each
// corner a cell clear of the orbs' orbit and off the outer one
func magicOnScreen(w *engine.World) string {
	cfg := w.Resources.Config
	if cfg.MapWidth != cfg.ViewportWidth || cfg.MapHeight != cfg.ViewportHeight {
		return fmt.Sprintf("the map is %dx%d in a %dx%d view", cfg.MapWidth, cfg.MapHeight, cfg.ViewportWidth, cfg.ViewportHeight)
	}
	shield, _ := w.Components.Shield.GetPtr(w.Resources.Player.Slot(0))
	squares := w.Components.Square.Entities()
	if len(squares) != 1 || shield == nil {
		return "no square stands under a shield"
	}
	sq, _ := w.Components.Square.GetPtr(squares[0])
	cx, cy := sq.Center()
	ox, oy := float64(cx)+vmath.CellCenter, float64(cy)+vmath.CellCenter
	// The orbs' orbit about a shield of radius: whether it stays on the map, and
	// whether a cell's middle lies within it
	orbit := func(radius float64) (on bool, within func(x, y int) bool) {
		rx := parameter.OrbOrbitRadiusX + radius - parameter.PlayerShieldRadius
		ry := cfg.CellAspect.FromCircular(rx)
		on = ox-rx >= 0 && oy-ry >= 0 && ox+rx < float64(cfg.MapWidth) && oy+ry < float64(cfg.MapHeight)
		return on, func(x, y int) bool {
			dx, dy := (float64(x)+vmath.CellCenter-ox)/rx, (float64(y)+vmath.CellCenter-oy)/ry
			return dx*dx+dy*dy <= 1
		}
	}
	on, within := orbit(shield.Radius)
	if !on {
		return fmt.Sprintf("the cursor's orbs circle off it, %d-row blocks under a %.2f-column shield", sq.Height, shield.Radius)
	}
	taller := *sq
	if taller.Height++; sq.Height < sq.MaxHeight {
		if on, _ := orbit(taller.ShieldRadius(cfg.CellAspect)); on {
			return fmt.Sprintf("the blocks stand %d rows where %d leave the orbs room", sq.Height, taller.Height)
		}
	}
	var path []vmath.Point
	for i := range 360 {
		at := sq.OrbAt(float64(i)*vmath.TwoPi/360, cfg.CellAspect, cfg.MapWidth, cfg.MapHeight)
		if at.X < 0 || at.Y < 0 || at.X >= cfg.MapWidth || at.Y >= cfg.MapHeight {
			return fmt.Sprintf("the outer orbit leaves it at %v", at)
		}
		path = append(path, at)
	}
	corners := map[vmath.Point]bool{}
	for _, e := range w.Components.Pylon.Entities() {
		at, _ := w.Positions.GetPosition(e)
		if at.X < 2 || at.Y < 1 || at.X+2 >= cfg.MapWidth || at.Y+1 >= cfg.MapHeight {
			return fmt.Sprintf("the pylon at %v is cut off", at)
		}
		for y := at.Y - 2; y <= at.Y+2; y++ {
			for x := at.X - 3; x <= at.X+3; x++ {
				if within(x, y) {
					return fmt.Sprintf("the pylon at %v stands within a cell of the orbs' orbit", at)
				}
			}
		}
		for _, p := range path {
			if p.X >= at.X-2 && p.X <= at.X+2 && p.Y >= at.Y-1 && p.Y <= at.Y+1 {
				return fmt.Sprintf("the outer orbit crosses the pylon at %v", at)
			}
		}
		corners[vmath.Point{X: min(at.X, cfg.MapWidth-1-at.X), Y: min(at.Y, cfg.MapHeight-1-at.Y)}] = true
	}
	if len(w.Components.Pylon.Entities()) != 4 || len(corners) != 1 || !corners[vmath.Point{X: 2, Y: 1}] {
		return fmt.Sprintf("the pylons stand %v from the corners, want one in each", corners)
	}
	return ""
}

// TestMagicsBodiesLeaveThePocketsBehindItsGateways: through two minutes of bodies
// no weapon wears, on the smallest map, none stands five seconds inside a pylon's
// push on its corner's side of it
func TestMagicsBodiesLeaveThePocketsBehindItsGateways(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	layMagic(t, a)
	w.RunSafe(func() {
		w.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: w.Resources.Player.Slot(0), Value: parameter.GodEnergyAmount})
	})
	held := map[core.Entity]int{}
	cornerward := func(v, c, size int) bool { return v != c && (c < size/2) == (v < c) }
	for range 2400 {
		a.Tick(1)
		w.RunSafe(func() {
			cfg := w.Resources.Config
			invRx, invRy := vmath.EllipseInvRadiiSq(parameter.PylonCollisionRadiusX, cfg.CellAspect.Y(parameter.PylonCollisionRadiusY))
			for _, e := range w.Components.Behavior.Entities() {
				at, _ := w.Positions.GetPosition(e)
				in := false
				w.Components.Pylon.Each(func(_ core.Entity, p *component.PylonComponent) bool {
					in = vmath.EllipseContainsPoint(at.X, at.Y, p.SpawnX, p.SpawnY, invRx, invRy) &&
						(cornerward(at.X, p.SpawnX, cfg.MapWidth) || cornerward(at.Y, p.SpawnY, cfg.MapHeight))
					return !in
				})
				if !in {
					delete(held, e)
				} else if held[e]++; held[e] == int(5*time.Second/parameter.GameUpdateInterval) {
					t.Errorf("a body has stood five seconds at (%d,%d), behind a gateway's pylon", at.X, at.Y)
				}
			}
		})
	}
}

// TestMagicIsLostToTheFirstTouchOnItsBlocks: once the shield is down, the first
// body on the blocks takes all the heat there is and the game starts over
func TestMagicIsLostToTheFirstTouchOnItsBlocks(t *testing.T) {
	t.Parallel()
	a := newMagic(t)
	w := a.World()
	layMagic(t, a)
	cursor := w.Resources.Player.Slot(0)
	w.RunSafe(func() {
		w.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: cursor, Value: 0})
	})
	read := func() (heat int, energy int64) {
		w.RunSafe(func() {
			h, _ := w.Components.Heat.GetComponent(w.Resources.Player.Slot(0))
			e, _ := w.Components.Energy.GetComponent(w.Resources.Player.Slot(0))
			heat, energy = h.Current, e.Current
		})
		return heat, energy
	}
	start, _ := read()
	touched := 0
	for tick := range 2400 {
		a.Tick(1)
		heat, energy := read()
		if touched == 0 && heat != start {
			if heat != 0 {
				t.Fatalf("the first touch on the blocks left %d of %d heat, want none", heat, start)
			}
			touched = tick
		}
		if touched > 0 && energy > 0 {
			return
		}
		if touched > 0 && tick-touched > int(time.Second/parameter.GameUpdateInterval) {
			t.Fatalf("a second after the first touch on the blocks the game stands, energy %d", energy)
		}
	}
	t.Fatal("no body touched the blocks in two minutes")
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
			&event.BehaviorSpawnRequestPayload{Program: "spark", X: 2 + i%37, Y: 2 + i%15}, event.OriginDebug)
	}
	a.Settle()
	a.Tick(2)
	if n := held(); n != 4 {
		t.Fatalf("%d gateways hold with 300 bodies on the map, want all four", n)
	}
}

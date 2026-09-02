package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// spawnWall drops one blocking cell into a test world.
func spawnWall(w *engine.World, x, y int) {
	e := w.CreateEntity(core.DomainShared)
	w.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: component.WallBlockAll})
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
}

// wallWithGap builds a full-height barrier at column x with one opening.
func wallWithGap(w *engine.World, x, gapY, height int) {
	for y := range height {
		if y == gapY {
			continue
		}
		spawnWall(w, x, y)
	}
}

// soloCursorWorld is testCursorWorld reduced to one cursor, so the roster's target
// group holds exactly the drop's owner and the multi-cursor case is a deliberate
// choice rather than the default.
func soloCursorWorld(t *testing.T) (*engine.World, core.Entity) {
	t.Helper()
	w, first, second := testCursorWorld(t)
	w.Positions.RemoveEntity(second)
	w.Components.Cursor.RemoveEntity(second)
	w.Resources.Player.Clear()
	w.Resources.Player.Bind(0, first)
	w.Resources.Player.SetLocal(0)
	return w, first
}

// dropLoot spawns one drop for an owner and returns its entity.
func dropLoot(w *engine.World, loot *LootSystem, x, y int, owner core.Entity) core.Entity {
	before := make(map[core.Entity]bool, w.Components.Loot.CountEntities())
	for _, e := range w.Components.Loot.GetAllEntities() {
		before[e] = true
	}
	loot.spawnLootWithBurst(component.LootEnergy, x, y, 0, 0, owner)
	for _, e := range w.Components.Loot.GetAllEntities() {
		if !before[e] {
			return e
		}
	}
	return 0
}

// lootFlight is what one drop's journey looked like.
type lootFlight struct {
	ticks     int     // ticks to collection, -1 if never collected
	peakSpeed float64 // fastest it travelled (cells/sec)
	maxDist   float64 // furthest it ever got from its owner (cells)
	pinned    int     // longest run of consecutive ticks it did not move
}

// flyLoot drives one drop to collection or exhaustion. The navigation system runs
// beside the loot system exactly as the schedule runs it, so a drop that depends on
// shared navigation state is driven the same way as one that does not.
func flyLoot(t *testing.T, w *engine.World, lootEntity, owner core.Entity, maxTicks int) lootFlight {
	t.Helper()
	nav := NewNavigationSystem(w).(*NavigationSystem)
	loot := NewLootSystem(w).(*LootSystem)
	deaths := NewDeathSystem(w).(*DeathSystem)

	out := lootFlight{ticks: -1}
	var lastX, lastY float64
	run := 0
	for i := range maxTicks {
		nav.Update()
		loot.Update()
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if ev.Type == event.EventDeathBatch {
				deaths.HandleEvent(ev)
			}
		}

		k, ok := w.Components.Kinetic.GetPtr(lootEntity)
		if !ok {
			out.ticks = i
			return out
		}
		if speed := vmath.MagnitudeF(k.VelX, k.VelY); speed > out.peakSpeed {
			out.peakSpeed = speed
		}
		if pos, ok := w.Positions.GetPosition(owner); ok {
			cx, cy := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
			if d := vmath.MagnitudeF(k.PreciseX-cx, k.PreciseY-cy); d > out.maxDist {
				out.maxDist = d
			}
		}
		if i > 0 && vmath.AbsF(k.PreciseX-lastX) < 0.02 && vmath.AbsF(k.PreciseY-lastY) < 0.02 {
			run++
			if run > out.pinned {
				out.pinned = run
			}
		} else {
			run = 0
		}
		lastX, lastY = k.PreciseX, k.PreciseY
	}
	return out
}

// TestLootRoutesToItsOwnCursorNotTheNearestOne is the defect this file was written
// for, and it needs two cursors to exist at all.
//
// A drop belongs to one participant. Navigation's target group zero is *every* live
// cursor, so the flow field it maintains leads to whichever cursor is nearest and
// the line-of-sight flag beside it is computed against that same nearest cursor —
// while the drop homes at its owner. A drop standing beside somebody else's cursor
// therefore read "direct path", drove straight at an owner on the far side of a
// wall, and stayed pressed against that wall for the rest of the run: with two
// instances open, every drop that landed near the other participant's cursor did
// this. The route is the owner's own now, so the barrier is one the drop goes
// around.
func TestLootRoutesToItsOwnCursorNotTheNearestOne(t *testing.T) {
	w, owner, other := testCursorWorld(t) // (5,5) and (15,5)
	wallWithGap(w, 10, 20, 24)

	loot := NewLootSystem(w).(*LootSystem)
	// Behind the barrier from its owner, and two cells from the other cursor.
	drop := dropLoot(w, loot, 13, 5, owner)
	if drop == 0 {
		t.Fatal("the drop was not created")
	}
	if !w.Positions.HasLineOfSight(13, 5, 15, 5, component.WallBlockKinetic) {
		t.Fatal("the drop can not see the wrong cursor; the case it reproduces is gone")
	}
	if w.Positions.HasLineOfSight(13, 5, 5, 5, component.WallBlockKinetic) {
		t.Fatal("the drop can see its own cursor; the barrier does not separate them")
	}
	_ = other

	flight := flyLoot(t, w, drop, owner, 300)
	if flight.ticks < 0 {
		t.Fatalf("the drop never reached its owner: %+v", flight)
	}
	if flight.pinned > 3 {
		t.Fatalf("the drop spent %d consecutive ticks against the barrier: %+v",
			flight.pinned, flight)
	}
}

// TestLootWithoutLineOfSightKeepsItsOwnRoute is the same rule one layer down: the
// per-owner field is what the drop steers by, so a second cursor beside it changes
// nothing about where it goes.
func TestLootWithoutLineOfSightKeepsItsOwnRoute(t *testing.T) {
	solo, soloOwner := soloCursorWorld(t)
	wallWithGap(solo, 10, 20, 24)
	soloLoot := NewLootSystem(solo).(*LootSystem)
	alone := flyLoot(t, solo, dropLoot(solo, soloLoot, 13, 5, soloOwner), soloOwner, 300)

	shared, sharedOwner, _ := testCursorWorld(t)
	wallWithGap(shared, 10, 20, 24)
	sharedLoot := NewLootSystem(shared).(*LootSystem)
	beside := flyLoot(t, shared, dropLoot(shared, sharedLoot, 13, 5, sharedOwner), sharedOwner, 300)

	if alone.ticks < 0 || beside.ticks != alone.ticks {
		t.Fatalf("a second cursor changed the journey home: alone %+v, beside %+v", alone, beside)
	}
}

// TestLootSettlesInsteadOfOrbitingItsOwner pins the long-standing complaint.
//
// Homing is a constant pull toward the owner, and nothing was removing the sideways
// component of a drop's velocity. A constant attraction with no damping holds an
// orbit — the radius it sustains is speed²/accel — and the only thing that ever
// removed energy was the profile's arrival damping inside five cells. At the old
// cruising speed of 60 cells per second that orbit radius was thirty cells, so a
// drop knocked sideways circled its cursor, wandering off walls and the map
// boundary, until something happened to stop it.
//
// The cornering brake every homing species applies is on the drop now, so a
// sideways heading is damped at any distance. The scenario is fixed rather than
// derived from the profile, so the numbers stay comparable when it is retuned.
func TestLootSettlesInsteadOfOrbitingItsOwner(t *testing.T) {
	w, owner := soloCursorWorld(t)
	loot := NewLootSystem(w).(*LootSystem)

	const dropX, dropY = 20, 12
	drop := dropLoot(w, loot, dropX, dropY, owner)
	if drop == 0 {
		t.Fatal("the drop was not created")
	}
	k, ok := w.Components.Kinetic.GetPtr(drop)
	if !ok {
		t.Fatal("the drop carries no kinetic component")
	}
	ownerPos, _ := w.Positions.GetPosition(owner)
	radius := vmath.MagnitudeF(float64(dropX-ownerPos.X), float64(dropY-ownerPos.Y))
	k.VelX, k.VelY = 0, 40 // across the line to its owner, hard

	flight := flyLoot(t, w, drop, owner, 300)
	if flight.ticks < 0 {
		t.Fatalf("the drop circled for 300 ticks without being collected: %+v", flight)
	}
	// The kick has to be spent before the drop can close, so one excursion is
	// expected; a lap around the cursor is not, and neither is being flung across
	// the map and back.
	if flight.maxDist > radius*1.25 {
		t.Fatalf("the drop swung out to %.1f cells from %.1f: %+v",
			flight.maxDist, radius, flight)
	}
	if flight.ticks > 35 {
		t.Fatalf("the drop took %d ticks to settle: %+v", flight.ticks, flight)
	}
}

// TestLootReachesItsOwnerAcrossAMaze is the tower region's own geometry: the maze
// that config/main/tower.toml builds, with drops scattered across it.
func TestLootReachesItsOwnerAcrossAMaze(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 120, 40, engine.NewManualClock())

	cursors := NewCursorSystem(w).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: 60, Y: 20, Slot: 0, Control: uint8(component.ControlHuman),
		},
	})
	owner := w.Resources.Player.Slot(0)
	if owner == 0 {
		t.Fatal("no cursor")
	}

	walls := NewWallSystem(w).(*WallSystem)
	walls.HandleEvent(event.GameEvent{
		Type: event.EventMazeSpawnRequest,
		Payload: &event.MazeSpawnRequestPayload{
			CellWidth: 5, CellHeight: 3, Braiding: 0.5,
			BlockMask: component.WallBlockAll,
			Rooms:     []event.MazeRoomSpec{{CenterX: 60, CenterY: 20, Width: 15, Height: 11}},
		},
	})
	w.Resources.Event.Queue.Consume()
	if walls := w.Components.Wall.CountEntities(); walls < 200 {
		t.Fatalf("the maze produced %d walls; the crossing proves nothing", walls)
	}

	loot := NewLootSystem(w).(*LootSystem)
	nav := NewNavigationSystem(w).(*NavigationSystem)
	deaths := NewDeathSystem(w).(*DeathSystem)

	rng := w.Rand(core.DomainPlayer, "loot_maze_probe")
	drops := make([]core.Entity, 0, 32)
	for range 40 {
		x, y := rng.Intn(120), rng.Intn(40)
		if w.Positions.IsBlocked(x, y, component.WallBlockKinetic) {
			continue
		}
		if e := dropLoot(w, loot, x, y, owner); e != 0 {
			drops = append(drops, e)
		}
	}
	if len(drops) < 8 {
		t.Fatalf("only %d drops found a free cell; the crossing proves little", len(drops))
	}
	w.Resources.Event.Queue.Consume()

	for range 800 {
		nav.Update()
		loot.Update()
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if ev.Type == event.EventDeathBatch {
				deaths.HandleEvent(ev)
			}
		}
	}

	var stranded []core.Entity
	for _, e := range drops {
		if w.Components.Loot.HasEntity(e) {
			stranded = append(stranded, e)
		}
	}
	if len(stranded) > 0 {
		for _, e := range stranded {
			pos, _ := w.Positions.GetPosition(e)
			t.Errorf("drop %d never crossed the maze; it rests at (%d,%d)", e, pos.X, pos.Y)
		}
		t.Fatalf("%d of %d drops never reached the cursor", len(stranded), len(drops))
	}
}

// TestLootWalledOffComesToRest is the negative control for the route: a drop with no
// path to its owner stops instead of grinding against whatever is in the way, and
// says so on the surface rather than looking like ordinary flight.
func TestLootWalledOffComesToRest(t *testing.T) {
	w, owner := soloCursorWorld(t)
	for _, c := range [][2]int{{19, 11}, {20, 11}, {21, 11}, {19, 12}, {21, 12}, {19, 13}, {20, 13}, {21, 13}} {
		spawnWall(w, c[0], c[1])
	}
	loot := NewLootSystem(w).(*LootSystem)
	drop := dropLoot(w, loot, 20, 12, owner)
	if drop == 0 {
		t.Fatal("the drop was not created")
	}

	flight := flyLoot(t, w, drop, owner, 120)
	if flight.ticks >= 0 {
		t.Fatalf("a drop sealed away from its owner was collected at tick %d", flight.ticks)
	}
	k, ok := w.Components.Kinetic.GetPtr(drop)
	if !ok {
		t.Fatal("the drop is gone")
	}
	if k.VelX != 0 || k.VelY != 0 {
		t.Fatalf("the sealed drop is still moving at (%.3f, %.3f)", k.VelX, k.VelY)
	}
	if got := w.Resources.Status.Ints.Get("loot.unreachable").Load(); got != 1 {
		t.Fatalf("loot.unreachable = %d, want the one drop that cannot get home", got)
	}
}

// TestLootRoutesFollowTheirOwnersLifetime pins what the fields cost: one per cursor
// that has dropped something, held while that cursor lives and released with it.
func TestLootRoutesFollowTheirOwnersLifetime(t *testing.T) {
	w, first, second := testCursorWorld(t)
	loot := NewLootSystem(w).(*LootSystem)

	dropLoot(w, loot, 8, 8, first)
	loot.Update()
	if got := len(loot.ownerRoutes); got != 1 {
		t.Fatalf("one owner with loot holds %d routes, want 1", got)
	}

	dropLoot(w, loot, 18, 8, second)
	loot.Update()
	if got := len(loot.ownerRoutes); got != 2 {
		t.Fatalf("two owners with loot hold %d routes, want 2", got)
	}
	if got := w.Resources.Status.Ints.Get("loot.routes").Load(); got != 2 {
		t.Fatalf("loot.routes = %d, want 2", got)
	}

	// A cursor that leaves takes its route with it; the other keeps its own.
	w.Positions.RemoveEntity(second)
	loot.Update()
	if got := len(loot.ownerRoutes); got != 1 {
		t.Fatalf("a departed cursor left %d routes, want the survivor's alone", got)
	}
	if _, held := loot.ownerRoutes[first]; !held {
		t.Fatal("the surviving cursor lost its route")
	}
}

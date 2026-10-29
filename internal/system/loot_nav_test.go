package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func TestLootSurvivesAnEncounterClearAndMapShrinkUntilReset(t *testing.T) {
	w, owner, _ := testCursorWorld(t)
	loot := ready(NewLootSystem(w)).(*LootSystem)
	drop := dropLoot(w, loot, 35, 20, owner)
	w.SetupLevel(20, 12, true, false, true)
	loot.Update()
	pos, ok := w.Positions.GetPosition(drop)
	if !ok || !w.Components.Loot.HasEntity(drop) || w.Positions.IsOutOfBounds(pos.X, pos.Y) {
		t.Fatal("encounter cleanup lost its uncollected loot")
	}
	if flight := flyLoot(t, w, drop, owner, 300); flight.ticks < 0 {
		t.Fatal("preserved loot could not reach its owner after the resize")
	}
	drop = dropLoot(w, loot, 15, 10, owner)
	w.SetupLevel(20, 12, true, false, false)
	if w.Components.Loot.HasEntity(drop) {
		t.Fatal("a game reset preserved the previous run's loot")
	}
}

// A capped weapon yields to its tier's siblings, and a tier with none left adds its
// fallback to every later tier: Storm's pulse after bullets, Kraken's 3+3 after rays.
func TestDropTableYieldsCappedWeaponsThenFallsBack(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	loot := ready(NewLootSystem(w)).(*LootSystem)
	capWeapon := func(wt component.WeaponType) {
		weapons, _ := w.Components.Weapon.GetPtr(cursor)
		weapons.Charges[wt] = component.WeaponSpecs[wt].MaxCharges
	}
	roll := func(species component.SpeciesType) map[component.LootType]int {
		got := map[component.LootType]int{}
		for _, r := range loot.rollDropTable(species, cursor, 0) {
			got[r.Loot] += r.Count
		}
		return got
	}
	capWeapon(component.WeaponTurret)
	if got := roll(component.SpeciesStorm); got[component.LootDisruptor] != 1 || got[component.LootEnergy] != 3 {
		t.Fatalf("storm with bullets capped dropped %v", got)
	}
	capWeapon(component.WeaponDisruptor)
	if got := roll(component.SpeciesStorm); len(got) != 1 || got[component.LootEnergy] != 5 {
		t.Fatalf("storm with both capped dropped %v", got)
	}
	if got := roll(component.SpeciesKraken); got[component.LootEmitter] != 1 || got[component.LootEnergy] != 2 || got[component.LootHeat] != 2 {
		t.Fatalf("kraken dropped %v", got)
	}
	capWeapon(component.WeaponEmitter)
	if got := roll(component.SpeciesKraken); len(got) != 2 || got[component.LootEnergy] != 3 || got[component.LootHeat] != 3 {
		t.Fatalf("kraken with rays capped dropped %v", got)
	}
}

// A data species rolls the table its program names, a code species' here; one that
// names none, or a species past the scenario's programs, drops nothing
func TestADataSpeciesRollsTheDropsItsPrototypeNames(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	w.Resources.Behaviors = []component.BehaviorPrototype{
		{Name: "warden", Species: component.SpeciesDataFirst, Drops: "kraken"},
		{Name: "wisp", Species: component.SpeciesDataFirst + 1},
	}
	loot := ready(NewLootSystem(w)).(*LootSystem)
	roll := func(species component.SpeciesType) map[component.LootType]int {
		got := map[component.LootType]int{}
		for _, r := range loot.rollDropTable(species, cursor, 0) {
			got[r.Loot] += r.Count
		}
		return got
	}
	if got := roll(component.SpeciesDataFirst); len(got) != 3 || got[component.LootEmitter] != 1 || got[component.LootEnergy] != 2 || got[component.LootHeat] != 2 {
		t.Fatalf("a data species dropping as the kraken dropped %v", got)
	}
	for _, species := range []component.SpeciesType{component.SpeciesDataFirst + 1, component.SpeciesDataFirst + 2} {
		if got := roll(species); len(got) != 0 {
			t.Fatalf("species %d dropped %v, want nothing", species, got)
		}
	}
}

// spawnWall drops one blocking cell into a test world.
func spawnWall(w *engine.World, x, y int) {
	e := w.CreateEntity(core.DomainShared)
	w.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: component.WallBlockAll})
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
}

// wallWithGap builds a barrier down column x of the map, open on its last row only.
func wallWithGap(w *engine.World, x int) {
	for y := range w.Resources.Config.MapHeight - 1 {
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
	loot.spawnLootWithBurst(component.LootEnergy, x, y, 0, 0, owner, component.LootComponent{})
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
	nav := ready(NewNavigationSystem(w)).(*NavigationSystem)
	loot := ready(NewLootSystem(w)).(*LootSystem)
	deaths := ready(NewDeathSystem(w)).(*DeathSystem)

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
		if speed := vmath.Magnitude(k.VelX, k.VelY); speed > out.peakSpeed {
			out.peakSpeed = speed
		}
		if pos, ok := w.Positions.GetPosition(owner); ok {
			cx, cy := vmath.Point{X: pos.X, Y: pos.Y}.Center()
			if d := vmath.Magnitude(k.PreciseX-cx, k.PreciseY-cy); d > out.maxDist {
				out.maxDist = d
			}
		}
		if i > 0 && vmath.Abs(k.PreciseX-lastX) < 0.02 && vmath.Abs(k.PreciseY-lastY) < 0.02 {
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

// TestLootRoutesToItsOwnCursorNotTheNearestOne: navigation's target group zero is
// every cursor, so a drop beside another participant's cursor read "direct path"
// and pressed against a wall between it and its owner. A drop routes to its owner,
// so the barrier is one it goes around.
func TestLootRoutesToItsOwnCursorNotTheNearestOne(t *testing.T) {
	w, owner, other := testCursorWorld(t) // (5,5) and (15,5)
	wallWithGap(w, 10)

	loot := ready(NewLootSystem(w)).(*LootSystem)
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
	wallWithGap(solo, 10)
	soloLoot := ready(NewLootSystem(solo)).(*LootSystem)
	alone := flyLoot(t, solo, dropLoot(solo, soloLoot, 13, 5, soloOwner), soloOwner, 300)

	shared, sharedOwner, _ := testCursorWorld(t)
	wallWithGap(shared, 10)
	sharedLoot := ready(NewLootSystem(shared)).(*LootSystem)
	beside := flyLoot(t, shared, dropLoot(shared, sharedLoot, 13, 5, sharedOwner), sharedOwner, 300)

	if alone.ticks < 0 || beside.ticks != alone.ticks {
		t.Fatalf("a second cursor changed the journey home: alone %+v, beside %+v", alone, beside)
	}
}

// TestLootSettlesInsteadOfOrbitingItsOwner: an undamped constant pull holds an
// orbit of radius speed²/accel, so a drop knocked sideways circled its cursor. The
// cornering brake damps a sideways heading at any distance. The scenario is fixed
// rather than derived from the profile, so the numbers survive a retune.
func TestLootSettlesInsteadOfOrbitingItsOwner(t *testing.T) {
	w, owner := soloCursorWorld(t)
	loot := ready(NewLootSystem(w)).(*LootSystem)

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
	radius := vmath.Magnitude(float64(dropX-ownerPos.X), float64(dropY-ownerPos.Y))
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
// that internal/asset/scenario/tower.toml builds, with drops scattered across it.
func TestLootReachesItsOwnerAcrossAMaze(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 120, 40, engine.NewManualClock())

	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: 60, Y: 20, Slot: 0, Control: uint8(component.ControlLocal),
		},
	})
	owner := w.Resources.Player.Slot(0)
	if owner == 0 {
		t.Fatal("no cursor")
	}

	loot := ready(NewLootSystem(w)).(*LootSystem)
	nav := ready(NewNavigationSystem(w)).(*NavigationSystem)
	warmDrop := dropLoot(w, loot, 65, 20, owner)
	nav.Update()
	if w.Resources.Pursuit.Field(0) == nil {
		t.Fatal("no field toward the owner on the open map; the maze replaces nothing")
	}
	w.DestroyEntity(warmDrop)

	walls := ready(NewWallSystem(w)).(*WallSystem)
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

	deaths := ready(NewDeathSystem(w)).(*DeathSystem)

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
	loot := ready(NewLootSystem(w)).(*LootSystem)
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

// TestADropsFieldIsItsOwnersPrivateGroup: navigation keeps a point field toward
// every cursor on the map, which a drop reads for its owner's slot, and retires
// it with the cursor; nothing of it lives in the loot system.
func TestADropsFieldIsItsOwnersPrivateGroup(t *testing.T) {
	w, _, second := testCursorWorld(t)
	nav := ready(NewNavigationSystem(w)).(*NavigationSystem)
	nav.Update()
	if w.Resources.Pursuit.Field(0) == nil || w.Resources.Pursuit.Field(1) == nil {
		t.Fatal("a cursor on the map has no field toward it")
	}
	if got := len(nav.groups); got != 3 {
		t.Fatalf("two cursors and the roster hold %d groups, want 3", got)
	}

	w.Positions.RemoveEntity(second)
	nav.Update()
	if w.Resources.Pursuit.Field(1) != nil || w.Resources.Pursuit.Field(0) == nil {
		t.Fatal("a departed cursor kept its field, or took the survivor's")
	}
}

// TestAStillDropStaysAndReturnsAfterCollection: an event drop marked still stays at
// its cell while its owner is away, and one with a return is off the map from its
// collection until that long after, then back at its cell to be collected again.
func TestAStillDropStaysAndReturnsAfterCollection(t *testing.T) {
	w, owner := soloCursorWorld(t)
	loot := ready(NewLootSystem(w)).(*LootSystem)
	w.Positions.SetPosition(owner, component.PositionComponent{X: 3, Y: 3})
	loot.HandleEvent(event.GameEvent{Type: event.EventLootSpawnRequest, Payload: &event.LootSpawnRequestPayload{
		Type: component.LootRod, X: 20, Y: 10, Still: true, ReturnMs: 3000,
	}})
	drops := w.Components.Loot.GetAllEntities()
	if len(drops) != 1 {
		t.Fatalf("%d drops, want one", len(drops))
	}
	at := func() (component.PositionComponent, bool) { return w.Positions.GetPosition(drops[0]) }
	step := func(ticks int) {
		for range ticks {
			loot.Update()
		}
	}

	step(100)
	if pos, ok := at(); !ok || pos.X != 20 || pos.Y != 10 {
		t.Fatalf("the still drop is at %+v (%t), want its cell", pos, ok)
	}
	w.Positions.SetPosition(owner, component.PositionComponent{X: 20, Y: 10})
	step(1)
	w.Positions.SetPosition(owner, component.PositionComponent{X: 3, Y: 3})
	if _, ok := at(); ok || !w.Components.Loot.HasEntity(drops[0]) {
		t.Fatal("a collected drop with a return stayed on the map, or was destroyed")
	}
	away := int(3 * time.Second / w.Resources.Time.DeltaTime)
	step(away - 1)
	if _, ok := at(); ok {
		t.Fatal("the drop came back before its return")
	}
	step(1)
	if pos, ok := at(); !ok || pos.X != 20 || pos.Y != 10 {
		t.Fatalf("the drop is at %+v (%t) after its return, want its cell", pos, ok)
	}
}

// TestADropAWallLandedOnIsDisplacedAndCollected: a wall that arrived under a drop
// without displacing it — a correction installs shared walls under player-domain
// loot — leaves it where no field has a direction and physics bounces every step.
func TestADropAWallLandedOnIsDisplacedAndCollected(t *testing.T) {
	w, owner := soloCursorWorld(t)
	for _, c := range [][2]int{{18, 10}, {19, 10}, {20, 10}, {18, 11}, {20, 11}} {
		spawnWall(w, c[0], c[1]) // a notch open to the south
	}
	w.Positions.SetPosition(owner, component.PositionComponent{X: 3, Y: 3})
	drop := dropLoot(w, ready(NewLootSystem(w)).(*LootSystem), 19, 11, owner)
	spawnWall(w, 19, 11)

	if flight := flyLoot(t, w, drop, owner, 400); flight.ticks < 0 {
		t.Fatalf("the drop never left the wall that landed on it: %+v", flight)
	}
}

// A reader out of sight reads its field every field_refresh_ticks and holds the way
// it read between, a navigator its flow and a drop its owner's field's, so the field
// is computed on the reading ticks alone; one that went out of sight since the last
// read holds no way, and homes on its target meanwhile. An install takes the phase.
func TestAReaderOutOfSightReadsItsFieldEveryRefresh(t *testing.T) {
	w, owner := soloCursorWorld(t)
	w.Resources.Config.NavRefreshTicks = 4
	wallWithGap(w, 20)
	mover := w.CreateEntity(core.DomainShared) // closes a cell every tick, so a field read must be computed
	w.Components.Wall.SetComponent(mover, component.WallComponent{BlockMask: component.WallBlockAll})
	w.Positions.SetPosition(mover, component.PositionComponent{X: 38, Y: 0})
	walker := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(walker, component.PositionComponent{X: 30, Y: 5})
	w.Components.Navigation.SetComponent(walker, component.NavigationComponent{FlowLookahead: 12})
	nav := ready(NewNavigationSystem(w)).(*NavigationSystem)
	loot := ready(NewLootSystem(w)).(*LootSystem)
	drop := dropLoot(w, loot, 32, 8, owner)
	held := 0.123
	for tick := range 9 {
		w.Positions.Move(mover, component.PositionComponent{X: 38, Y: tick % 3})
		computed := nav.groups[0].fields[0].cache.Field.CurrentGen
		nav.Update()
		loot.Update()
		w.Resources.Event.Queue.Consume()
		n, _ := w.Components.Navigation.GetPtr(walker)
		l, _ := w.Components.Loot.GetPtr(drop)
		read := nav.groups[0].fields[0].cache.Field.CurrentGen != computed
		if fresh := tick%4 == 0; read != fresh || fresh == (n.FlowX == held) || fresh == (l.FlowX == held) {
			t.Fatalf("tick %d: reading tick %t, the field computed %t, the walker's flow held %t, the drop's %t",
				tick, fresh, read, n.FlowX == held, l.FlowX == held)
		}
		n.FlowX, l.FlowX = held, held
	}
	w.Positions.Move(walker, component.PositionComponent{X: 10, Y: 5})
	nav.Update() // in sight, between reads
	w.Positions.Move(walker, component.PositionComponent{X: 30, Y: 5})
	nav.Update() // out of sight again, still before the next read
	if n, _ := w.Components.Navigation.GetPtr(walker); n.HasDirectPath || n.FlowX != 0 || n.FlowY != 0 {
		t.Fatalf("a navigator out of sight since the last read steers %+v, want no flow", n)
	}
	phase, _ := nav.SaveShared()
	joiner := ready(NewNavigationSystem(w)).(*NavigationSystem)
	if err := joiner.LoadShared(phase); err != nil || joiner.refreshTicks != nav.refreshTicks || nav.refreshTicks == 0 {
		t.Fatalf("an install took refresh phase %d of the sender's %d (%v)", joiner.refreshTicks, nav.refreshTicks, err)
	}
}

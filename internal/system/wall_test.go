package system

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/pattern"
	"github.com/lixenwraith/vif/pkg/cave"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

func TestWallDisplacementMaskUsesEntityCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*engine.World, core.Entity)
		want  component.WallBlockMask
	}{
		{
			name: "kinetic header is physical",
			setup: func(w *engine.World, e core.Entity) {
				w.Components.Kinetic.SetComponent(e, component.KineticComponent{})
				w.Components.Header.SetComponent(e, component.HeaderComponent{})
			},
			want: component.WallBlockKinetic,
		},
		{
			name: "plain composite header is an anchor",
			setup: func(w *engine.World, e core.Entity) {
				w.Components.Header.SetComponent(e, component.HeaderComponent{})
			},
			want: component.WallBlockNone,
		},
		{
			name: "particle",
			setup: func(w *engine.World, e core.Entity) {
				w.Components.Particle.SetComponent(e, component.ParticleComponent{Behavior: component.ParticleDecay})
			},
			want: component.WallBlockParticle,
		},
		{
			name:  "ordinary spawn blocker",
			setup: func(*engine.World, core.Entity) {},
			want:  component.WallBlockSpawn,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
			e := w.CreateEntity(core.DomainShared)
			tc.setup(w, e)
			walls := ready(NewWallSystem(w)).(*WallSystem)
			if got := walls.getMaskForEntity(e); got != tc.want {
				t.Fatalf("mask = %v, want %v", got, tc.want)
			}
		})
	}
}

// newCaveWorld is a map with a cursor in slot 0 and a wall system, its clock one
// column of a 10 column/second cave per tick
func newCaveWorld(t *testing.T, cursorX, cursorY int) (*engine.World, *WallSystem, core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	ready(NewCursorSystem(w)).(*CursorSystem).HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{X: cursorX, Y: cursorY, Control: uint8(component.ControlLocal)}})
	w.Resources.Event.Queue.Consume()
	w.Resources.Time.DeltaTime = 100 * time.Millisecond
	return w, ready(NewWallSystem(w)).(*WallSystem), w.Resources.Player.Slot(0)
}

// rockAt reports the blocking wall cells of row y as a string of '#' and '.'
func rockAt(w *engine.World, y int) string {
	row := make([]byte, w.Resources.Config.MapWidth)
	for x := range row {
		row[x] = '.'
		if w.Positions.HasBlockingWallAt(x, y, component.WallBlockAll) {
			row[x] = '#'
		}
	}
	return string(row)
}

func TestACaveLaysItsProfileAndScrollsItAColumnAStep(t *testing.T) {
	w, walls, _ := newCaveWorld(t, 30, 12)
	start := &event.CaveStartPayload{Seed: 7, Speed: 10, MinGap: 6, MaxGap: 12, MinWall: 1, Wavelength: 8, Detail: 1, Ramp: 4}
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: start})
	mapW, mapH := w.Resources.Config.MapWidth, w.Resources.Config.MapHeight
	profile := cave.Config{Seed: 7, Height: mapH, MinGap: 6, MaxGap: 12, MinWall: 1, Wavelength: 8, Detail: 1, Ramp: 4}

	for step := range 3 {
		for y := range mapH {
			want := make([]byte, mapW)
			for x := range want {
				col := profile.At(int64(x + step))
				want[x] = '.'
				if y < col.Top || y >= mapH-col.Bottom {
					want[x] = '#'
				}
			}
			if got := rockAt(w, y); got != string(want) {
				t.Fatalf("step %d row %d:\n got  %s\n want %s", step, y, got, want)
			}
		}
		walls.Update()
	}
}

// A cave's rock is a field no wall entity holds, so a capture of the world carries
// no rock and the record is the whole of it
func TestACavesRockIsAFieldNoWallHolds(t *testing.T) {
	w, walls, _ := newCaveWorld(t, 30, 12)
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7, MinWall: 1}})
	for range 20 {
		walls.stepCave()
	}
	rock := w.Positions.Rock()
	if rock == nil || rock.Width() != w.Resources.Config.MapWidth || rock.Origin != walls.cave.Offset {
		t.Fatalf("field %+v after 20 steps, want the map's width from offset %d", rock, walls.cave.Offset)
	}
	if n := w.Components.Wall.CountEntities(); n != 0 || len(w.CaptureSharedWorld().Wall) != 0 {
		t.Fatalf("%d wall entities hold the rock, want none", n)
	}
}

// A stopped cave's rock stands where it stopped, and its record carries it there
func TestAStoppedCaveStandsAndItsRecordCarriesIt(t *testing.T) {
	w, walls, _ := newCaveWorld(t, 30, 12)
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7, Speed: 10, MinWall: 1, Detail: 1}})
	walls.Update()
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStop})
	before := rockAt(w, 1)
	for range 5 {
		walls.Update()
	}
	if after := rockAt(w, 1); after != before || walls.cave.Offset != 1 {
		t.Fatalf("a stopped cave moved to offset %d:\n before %s\n after  %s", walls.cave.Offset, before, after)
	}
	record, err := walls.SaveShared()
	if err != nil {
		t.Fatal(err)
	}
	receiver, receiverWalls, _ := newCaveWorld(t, 30, 12)
	if err := receiverWalls.LoadShared(record); err != nil {
		t.Fatal(err)
	}
	if got := rockAt(receiver, 1); got != before || !receiverWalls.cave.Halted {
		t.Fatalf("the record laid %s (halted %t), want %s", got, receiverWalls.cave.Halted, before)
	}
}

// Every wall going takes the cave's rock with it, and a record with no cave lifts it
func TestRemovingEveryWallLiftsTheCave(t *testing.T) {
	for _, remove := range []event.GameEvent{
		{Type: event.EventWallDespawnAll},
		{Type: event.EventWallDespawnRequest, Payload: &event.WallDespawnRequestPayload{All: true}},
	} {
		w, walls, _ := newCaveWorld(t, 30, 12)
		walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7}})
		walls.HandleEvent(remove)
		if w.Positions.Rock() != nil || walls.cave.Active || w.Positions.HasBlockingWallAt(0, 0, component.WallBlockAll) {
			t.Fatalf("%v left the cave standing", remove.Type)
		}
	}
	w, walls, _ := newCaveWorld(t, 30, 12)
	idle, _ := walls.SaveShared()
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7}})
	if err := walls.LoadShared(idle); err != nil || w.Positions.Rock() != nil || walls.cave.Active {
		t.Fatalf("a record with no cave left one standing (err %v)", err)
	}
}

// A resized map takes the rock reshaped to its height, and what the rock now covers
// is pushed out
func TestAResizedMapReshapesTheRock(t *testing.T) {
	w, walls, cursor := newCaveWorld(t, 5, 10)
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7, MinWall: 1}})
	w.Resources.Event.Queue.Consume()
	config := w.Resources.Config
	profile := walls.cave.Config
	profile.Height = config.MapHeight + 10
	// The cursor stands on the reshaped ceiling's lowest row
	w.Positions.SetPosition(cursor, component.PositionComponent{X: 5, Y: profile.At(5).Top - 1})
	config.MapHeight = profile.Height
	w.Positions.ResizeGrid(config.MapWidth, config.MapHeight)
	walls.Update()
	for x := range config.MapWidth {
		col := profile.At(walls.cave.Offset + int64(x))
		if got, _ := w.Positions.Rock().Column(x); got.Ceil != col.Top || got.Floor != config.MapHeight-col.Bottom {
			t.Fatalf("column %d is %+v after the resize, want the profile at height %d: %+v", x, got, config.MapHeight, col)
		}
	}
	walls.Update()
	if moves, _ := movesOf(w); len(moves) != 1 || moves[0].Entity != cursor {
		t.Fatalf("the reshaped rock covers the cursor and moved it %+v", moves)
	}
}

// A level setup clears every wall entity but not the cave: its rock stands, laid
// across the new map's bounds from where the cave stood
func TestALevelSetupKeepsTheCave(t *testing.T) {
	w, walls, _ := newCaveWorld(t, 5, 10)
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7, Speed: 10, MinWall: 1}})
	walls.Update()
	w.SetupLevel(60, 30, true, false, false)
	walls.Update()
	config := w.Resources.Config
	profile := walls.cave.Config
	profile.Height = config.MapHeight
	rock := w.Positions.Rock()
	if !walls.cave.Active || rock == nil || rock.Width() != config.MapWidth || walls.cave.Offset != 2 {
		t.Fatalf("after a level setup to %dx%d the cave is %+v", config.MapWidth, config.MapHeight, walls.cave)
	}
	for x := range config.MapWidth {
		col := profile.At(walls.cave.Offset + int64(x))
		if got, _ := rock.Column(x); got.Ceil != col.Top || got.Floor != config.MapHeight-col.Bottom {
			t.Fatalf("column %d is %+v after the level setup, want %+v at height %d", x, got, col, config.MapHeight)
		}
	}
}

// caveChannel starts a cave of one rock row above and below, then hangs a block from
// the ceiling at column x down to row bottom, each cell its own wall
func caveChannel(w *engine.World, walls *WallSystem, x, bottom int) {
	gap := w.Resources.Config.MapHeight - 2
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{
		Speed: 10, MinGap: gap, MaxGap: gap, MinWall: 1, Wavelength: 1}})
	batch := &event.WallBatchSpawnRequestPayload{BlockMask: component.WallBlockAll}
	for y := 1; y <= bottom; y++ {
		batch.Cells = append(batch.Cells, component.WallCellDef{OffsetX: x, OffsetY: y})
	}
	walls.HandleEvent(event.GameEvent{Type: event.EventWallBatchSpawnRequest, Payload: batch})
	w.Resources.Event.Queue.Consume()
}

// movesOf returns the cursor placements and crushes a tick queued
func movesOf(w *engine.World) (moves []event.CursorMoveRequestPayload, crushed []core.Entity) {
	for _, ev := range w.Resources.Event.Queue.Consume() {
		switch p := ev.Payload.(type) {
		case *event.CursorMoveRequestPayload:
			moves = append(moves, *p)
		case *event.CursorCrushedPayload:
			crushed = append(crushed, p.Entity)
		}
	}
	return moves, crushed
}

func TestArrivingRockPushesACursorAColumnLeftAndTowardTheChannel(t *testing.T) {
	w, walls, cursor := newCaveWorld(t, 9, 5)
	caveChannel(w, walls, 10, 5)
	walls.Update()
	moves, crushed := movesOf(w)
	if want := (event.CursorMoveRequestPayload{Entity: cursor, X: 8, Y: 6}); len(moves) != 1 || moves[0] != want || len(crushed) != 0 {
		t.Fatalf("moves %+v and crushes %v, want only %+v", moves, crushed, want)
	}
}

// A crushed cursor stays where the rock found it: no squeeze moves it elsewhere
func TestRockPressingACursorAgainstTheLeftEdgeCrushesIt(t *testing.T) {
	w, walls, cursor := newCaveWorld(t, 0, 5)
	caveChannel(w, walls, 1, 5)
	walls.Update()
	moves, crushed := movesOf(w)
	if len(crushed) != 1 || crushed[0] != cursor || len(moves) != 0 {
		t.Fatalf("crushes %v and moves %+v, want cursor %d crushed and left in place", crushed, moves, cursor)
	}
}

// newBody is a flying composite, header at (x, y) with members over the w×h cells
// from there, each a cell's centre
func newBody(w *engine.World, x, y, width, height int) core.Entity {
	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: x, Y: y})
	px, py := vmath.Point{X: x, Y: y}.Center()
	w.Components.Kinetic.SetComponent(header, component.KineticComponent{Kinetic: physics.Kinetic{PreciseX: px + 0.25, PreciseY: py}})
	var members []component.MemberEntry
	for dy := range height {
		for dx := range width {
			m := w.CreateEntity(core.DomainShared)
			w.Positions.SetPosition(m, component.PositionComponent{X: x + dx, Y: y + dy})
			w.Components.Member.SetComponent(m, component.MemberComponent{HeaderEntity: header})
			members = append(members, component.MemberEntry{Entity: m, OffsetX: dx, OffsetY: dy})
		}
	}
	w.Components.Header.SetComponent(header, component.HeaderComponent{Behavior: component.BehaviorSwarm, MemberEntries: members})
	return header
}

// bodyAt reports where a composite's header stands and its sub-cell position, and
// whether any of its members stands on a wall
func bodyAt(w *engine.World, header core.Entity) (x, y int, px float64, walled bool) {
	pos, _ := w.Positions.GetPosition(header)
	k, _ := w.Components.Kinetic.GetComponent(header)
	h, _ := w.Components.Header.GetComponent(header)
	for _, m := range h.MemberEntries {
		mp, _ := w.Positions.GetPosition(m.Entity)
		walled = walled || w.Positions.HasBlockingWallAt(mp.X, mp.Y, component.WallBlockKinetic)
	}
	return pos.X, pos.Y, k.PreciseX, walled
}

// Rock reaching any member, not the header alone, moves the whole composite a column
// left and toward the channel, its sub-cell position with it
func TestRockReachingAnyMemberMovesTheWholeComposite(t *testing.T) {
	w, walls, _ := newCaveWorld(t, 30, 12)
	caveChannel(w, walls, 10, 4)
	body := newBody(w, 8, 4, 2, 2)
	walls.stepCave()
	if x, y, px, walled := bodyAt(w, body); x != 7 || y != 5 || px != 7.75 || walled {
		t.Fatalf("body at (%d,%d) precise x %v, on a wall %t; want (7,5) at 7.75, clear", x, y, px, walled)
	}
}

// A composite the rock pins at the left edge goes to the nearest place it fits, and
// one with nowhere to go is crushed with no kill to reward
func TestACompositeWithNoRoomBackFindsTheNearestOrIsCrushed(t *testing.T) {
	w, walls, _ := newCaveWorld(t, 30, 12)
	caveChannel(w, walls, 2, 4)
	body := newBody(w, 0, 4, 2, 2)
	walls.stepCave()
	if x, y, _, walled := bodyAt(w, body); walled || (x == 0 && y == 4) {
		t.Fatalf("pinned body at (%d,%d), on a wall %t; want it moved clear", x, y, walled)
	}

	// Rock everywhere else leaves the edge no 2×2 room but its own vacated column
	w, walls, _ = newCaveWorld(t, 30, 12)
	body = newBody(w, 0, 5, 2, 2)
	batch := &event.WallBatchSpawnRequestPayload{BlockMask: component.WallBlockAll}
	for y := range w.Resources.Config.MapHeight {
		for x := range w.Resources.Config.MapWidth {
			if x > 1 || y < 5 || y > 6 {
				batch.Cells = append(batch.Cells, component.WallCellDef{OffsetX: x, OffsetY: y})
			}
		}
	}
	walls.HandleEvent(event.GameEvent{Type: event.EventWallBatchSpawnRequest, Payload: batch})
	w.Resources.Event.Queue.Consume()
	walls.stepCave()
	var destroyed, killed bool
	for _, ev := range w.Resources.Event.Queue.Consume() {
		p, ok := ev.Payload.(*event.CompositeDestroyRequestPayload)
		destroyed = destroyed || (ok && p.HeaderEntity == body)
		killed = killed || ev.Type == event.EventSpeciesKilled
	}
	if !destroyed || killed {
		t.Fatalf("a body with no room: destroyed %t, a kill %t; want destroyed and no kill", destroyed, killed)
	}
}

// A record taken between columns carries the part period, and the record alone lays
// the rock, so a receiver that never saw the start steps on the origin's ticks
func TestACaveResumesFromItsRecordBetweenColumns(t *testing.T) {
	start := &event.CaveStartPayload{Seed: 7, Speed: 6, MinGap: 6, MaxGap: 12, MinWall: 1, Wavelength: 8, Detail: 1, Ramp: 4}
	origin, originWalls, _ := newCaveWorld(t, 30, 12)
	originWalls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: start})
	originWalls.Update()
	if originWalls.cave.Phase == 0 || originWalls.cave.Offset != 0 {
		t.Fatalf("cave %+v: the record must fall between columns", originWalls.cave)
	}
	record, err := originWalls.SaveShared()
	if err != nil {
		t.Fatal(err)
	}

	receiver, receiverWalls, _ := newCaveWorld(t, 30, 12)
	if err := receiverWalls.LoadShared(record); err != nil {
		t.Fatal(err)
	}
	for tick := range 12 {
		originWalls.Update()
		receiverWalls.Update()
		for y := range origin.Resources.Config.MapHeight {
			if a, b := rockAt(origin, y), rockAt(receiver, y); a != b {
				t.Fatalf("tick %d row %d:\n origin   %s\n receiver %s", tick, y, a, b)
			}
		}
	}
	if originWalls.cave.Offset == 0 || originWalls.cave != receiverWalls.cave {
		t.Fatalf("caves after the run:\n origin   %+v\n receiver %+v", originWalls.cave, receiverWalls.cave)
	}
}

// A cave draws the stone art it names in place of its visual, with the cave's seed,
// whatever the seed, and its record carries it; a name no preset has starts no cave
func TestACaveWearsTheStoneArtItNames(t *testing.T) {
	for i, art := range []string{"strata", "moss", "masonry", "strata", "moss", "masonry"} {
		seed := uint64(i + 1)
		w, walls, _ := newCaveWorld(t, 30, 12)
		walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: seed, MinWall: 1, Art: art}})
		preset, _ := pattern.ParseArtPreset(art)
		want := engine.RockLook{Visual: orStone(component.WallVisualConfig{}), Stone: true, Art: uint8(preset), Seed: seed}
		if rock := w.Positions.Rock(); rock == nil || rock.Look != want {
			t.Fatalf("seed %d laid the cave %+v, want the look %+v", seed, walls.cave, want)
		}
		record, err := walls.SaveShared()
		if err != nil {
			t.Fatal(err)
		}
		receiver, receiverWalls, _ := newCaveWorld(t, 30, 12)
		if err := receiverWalls.LoadShared(record); err != nil {
			t.Fatal(err)
		}
		if got := receiver.Positions.Rock().Look; got != want {
			t.Fatalf("seed %d: the record laid the look %+v, want %+v", seed, got, want)
		}
	}

	w, walls, _ := newCaveWorld(t, 30, 12)
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7, MinWall: 1}})
	if got, want := w.Positions.Rock().Look, (engine.RockLook{Visual: orStone(component.WallVisualConfig{})}); got != want {
		t.Fatalf("a cave with no art laid the look %+v, want %+v", got, want)
	}
	w, walls, _ = newCaveWorld(t, 30, 12)
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7, MinWall: 1, Art: "granite"}})
	if walls.cave.Active || w.Positions.Rock() != nil {
		t.Fatalf("an unknown art started the cave %+v", walls.cave)
	}
}

// A capture with no cave carries the generator's record alone
func TestARecordWithNoCaveIsTheGeneratorAlone(t *testing.T) {
	_, walls, _ := newCaveWorld(t, 30, 12)
	record, err := walls.SaveShared()
	if err != nil {
		t.Fatal(err)
	}
	if gen, _ := walls.genPCG.MarshalBinary(); string(record) != string(gen) || len(gen) != genRecordLen {
		t.Fatalf("record %q, generator %q of %d bytes", record, gen, genRecordLen)
	}
}

func TestACaveRecordNoStartLeavesIsRefused(t *testing.T) {
	_, walls, _ := newCaveWorld(t, 30, 12)
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{Seed: 7}})
	good := walls.cave
	gen, _ := walls.genPCG.MarshalBinary()
	for i, bad := range []func(c *caveState){
		func(c *caveState) { c.Period = 0 },
		func(c *caveState) { c.Phase = 2 * c.Period },
		func(c *caveState) { c.Offset = -1 },
		func(c *caveState) { c.Mask = component.WallBlockNone },
		func(c *caveState) { c.Config.MinGap = 0 },
		func(c *caveState) { *c = caveState{Offset: 3} },
		func(c *caveState) { c.Art = "granite" },
	} {
		c := good
		bad(&c)
		cave, _ := json.Marshal(c)
		if err := walls.LoadShared(append(gen[:len(gen):len(gen)], cave...)); err == nil || walls.cave != good {
			t.Fatalf("record %d loaded (err %v), cave now %+v", i, err, walls.cave)
		}
	}
}

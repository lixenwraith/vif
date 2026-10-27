package system

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/pkg/cave"
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

func TestRockPressingACursorAgainstTheLeftEdgeCrushesIt(t *testing.T) {
	w, walls, cursor := newCaveWorld(t, 0, 5)
	caveChannel(w, walls, 1, 5)
	walls.Update()
	moves, crushed := movesOf(w)
	if len(crushed) != 1 || crushed[0] != cursor || len(moves) != 1 || moves[0].Entity != cursor {
		t.Fatalf("crushes %v and moves %+v, want cursor %d crushed and squeezed out once", crushed, moves, cursor)
	}
}

// A record taken between columns carries the part period, so a receiver laid from
// the same start steps on the origin's ticks
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
	receiverWalls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: start})
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
		func(c *caveState) { c.Next = c.Offset - 1 },
		func(c *caveState) { c.Mask = component.WallBlockNone },
		func(c *caveState) { c.Config.MinGap = 0 },
		func(c *caveState) { *c = caveState{Offset: 3} },
	} {
		c := good
		bad(&c)
		cave, _ := json.Marshal(c)
		if err := walls.LoadShared(append(gen[:len(gen):len(gen)], cave...)); err == nil || walls.cave != good {
			t.Fatalf("record %d loaded (err %v), cave now %+v", i, err, walls.cave)
		}
	}
}

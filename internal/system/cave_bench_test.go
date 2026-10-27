package system

import (
	"bytes"
	"compress/flate"
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// caveBenchSizes are terminals: a wide one, a large one and the largest map there
// is (parameter.MaxMapCells)
var caveBenchSizes = []struct{ w, h int }{{200, 60}, {400, 120}, {500, 254}}

// newCaveBench starts vanguard's cave on a w×h map with no cursor
func newCaveBench(b testing.TB, w, h int) (*engine.World, *WallSystem) {
	b.Helper()
	world := engine.NewWorld()
	engine.NewGameContextWithClock(world, w, h, engine.NewManualClock())
	walls := ready(NewWallSystem(world)).(*WallSystem)
	walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{
		Seed: 7, Speed: 8, MinGap: 7, MaxGap: 15, MinWall: 2, Wavelength: 44, Swing: 9, Detail: 1, Ramp: 96,
	}})
	world.Resources.Event.Queue.Consume()
	world.Resources.Time.DeltaTime = 125 * time.Millisecond
	return world, walls
}

// BenchmarkCaveStep is a column's scroll: the rock field shifts, the entering column
// is laid and what the rock arrived on is displaced
func BenchmarkCaveStep(b *testing.B) {
	for _, size := range caveBenchSizes {
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			world, walls := newCaveBench(b, size.w, size.h)
			b.ReportAllocs()
			for b.Loop() {
				walls.stepCave()
				world.Resources.Event.Queue.Consume()
			}
			rock, h := 0, world.Resources.Config.MapHeight
			for _, c := range world.Positions.Rock().Columns() {
				rock += min(c.Ceil, h) + h - min(c.Floor, h)
			}
			b.ReportMetric(float64(rock), "rock-cells")
		})
	}
}

// BenchmarkCaveStart lays the whole map as a cave starts
func BenchmarkCaveStart(b *testing.B) {
	for _, size := range caveBenchSizes {
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				newCaveBench(b, size.w, size.h)
			}
		})
	}
}

// BenchmarkCaveStepNavigation is the navigation's wall diff after a step, what
// every tick that steps pays on top of the step itself
func BenchmarkCaveStepNavigation(b *testing.B) {
	for _, size := range caveBenchSizes {
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			world, walls := newCaveBench(b, size.w, size.h)
			nav := ready(NewNavigationSystem(world)).(*NavigationSystem)
			nav.Update()
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				walls.stepCave()
				world.Resources.Event.Queue.Consume()
				b.StartTimer()
				nav.applyWalls()
			}
		})
	}
}

// BenchmarkCaveCapture is a shared capture of the cave and a step's delta, the
// bytes a join and a correction carry, raw and deflated
func BenchmarkCaveCapture(b *testing.B) {
	deflated := func(raw []byte) int {
		var buf bytes.Buffer
		zw, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		zw.Write(raw)
		zw.Close()
		return buf.Len()
	}
	for _, size := range caveBenchSizes {
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			world, walls := newCaveBench(b, size.w, size.h)
			var full, delta []byte
			b.ReportAllocs()
			for b.Loop() {
				base := world.CaptureSharedWorld()
				full = full[:0]
				for i := range engine.SharedWorldStoreCount {
					full = engine.SharedWorldEncodeStoreColumns(full, &base, i)
				}
				b.StopTimer()
				walls.stepCave()
				world.Resources.Event.Queue.Consume()
				b.StartTimer()
				d := engine.DiffSharedWorld(base, world.CaptureSharedWorld())
				delta = engine.SharedWorldEncodeDelta(delta[:0], &d)
			}
			b.ReportMetric(float64(len(full)), "capture-B")
			b.ReportMetric(float64(deflated(full)), "capture-deflate-B")
			b.ReportMetric(float64(len(delta)), "delta-B")
			b.ReportMetric(float64(deflated(delta)), "delta-deflate-B")
		})
	}
}

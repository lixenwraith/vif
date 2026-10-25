package app

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
)

// BenchmarkHeadlessTicks measures one simulation tick on a driven App: the embedded
// scenario played by one cursor, and td, which an unattended run restarts every few
// seconds, so its ticks carry the maze spawn and route graphs a level costs. Run with
// -benchtime=<n>x so every run simulates the same ticks of one seed and allocations
// compare exactly.
func BenchmarkHeadlessTicks(b *testing.B) {
	cases := []struct {
		name   string
		cursor bool
		cfg    Config
	}{
		{"main-160x50", true, Config{Seed: 42, Width: 160, Height: 50, Resources: resource.Options{Embedded: true}}},
		{"td-200x60", false, Config{Seed: 42, Width: 200, Height: 60, Resources: resource.Options{Dir: "../../wad", Scenario: "td"}}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			a, err := NewHeadless(c.cfg)
			if err != nil {
				b.Fatalf("headless: %v", err)
			}
			defer a.Close()
			a.Tick(40)
			if c.cursor {
				a.Context().PushEventOrigin(event.EventCursorSpawnRequest,
					&event.CursorSpawnRequestPayload{Auto: true, Center: true}, event.OriginDebug)
				a.Settle()
			}
			a.Tick(60) // past the opening spawns, into play
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				a.Tick(1)
			}
		})
	}
}

// TestTickProfileTD16 reads a td tick with sixteen cursors on one core, the shape the
// map-limit trims are measured in: VIF_TICK_PROFILE names a directory its CPU profile
// is written to, and the mean tick is logged.
func TestTickProfileTD16(t *testing.T) {
	dir := os.Getenv("VIF_TICK_PROFILE")
	if dir == "" {
		t.Skip("set VIF_TICK_PROFILE=<dir> to profile a td tick with sixteen cursors")
	}
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	a, err := NewHeadless(Config{Seed: 42, Width: 200, Height: 60, Resources: resource.Options{Dir: soakRoot, Scenario: "td"}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Tick(40)
	for slot := range parameter.MaxPlayers {
		p := &event.CursorSpawnRequestPayload{Slot: uint8(slot), X: 20 + 28*(slot%16), Y: 30 + 7*(slot%3)}
		if slot > 0 {
			p.Control, p.PeerID = uint8(component.ControlRemote), uint32(slot+1)
		}
		a.Context().PushEventOrigin(event.EventCursorSpawnRequest, p, event.OriginDebug)
		a.Settle()
	}
	a.Tick(60)
	f, err := os.Create(filepath.Join(dir, "td16-tick.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		t.Fatal(err)
	}
	const ticks = 5000
	start := time.Now()
	a.Tick(ticks)
	elapsed := time.Since(start)
	pprof.StopCPUProfile()
	w, h := a.World().Positions.GridDimensions()
	t.Logf("td %dx%d, %d cursors, %d ticks on one core: %.2f ms a tick", w, h, parameter.MaxPlayers, ticks, float64(elapsed.Microseconds())/1000/ticks)
}

// BenchmarkWorldCopyAtMapLimit reads a td world, whose map is the ceiling, into a
// checkpoint's copy, first into fresh storage and then into the last copy's.
func BenchmarkWorldCopyAtMapLimit(b *testing.B) {
	a, err := NewHeadless(Config{Seed: 42, Width: 200, Height: 60, Resources: resource.Options{Dir: soakRoot, Scenario: "td"}})
	if err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	a.Tick(100)
	w := a.World()
	b.Run("fresh", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			w.RunSafe(func() { w.CopyOut(nil) })
		}
	})
	b.Run("reused", func(b *testing.B) {
		var c *engine.WorldCopy
		b.ReportAllocs()
		for b.Loop() {
			w.RunSafe(func() { c = w.CopyOut(c) })
		}
	})
}

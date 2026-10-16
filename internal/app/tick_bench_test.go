package app

import (
	"testing"

	"github.com/lixenwraith/vif/internal/event"
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

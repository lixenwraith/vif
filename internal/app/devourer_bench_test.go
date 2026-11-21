package app

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/render/renderer"
	"github.com/lixenwraith/vif/internal/resource"
)

// BenchmarkDevourer measures the Devourer in the embedded scenario's devourer region
// at 200x60 once its bezel has grown and two guts hang across it: "tick" is one
// simulation tick, "frames" the frames of a tick its renderer draws, which paints the
// body once a frame of it, every other tick at the default tick
func BenchmarkDevourer(b *testing.B) {
	for _, frames := range []bool{false, true} {
		name := "tick"
		if frames {
			name = "frames"
		}
		b.Run(name, func(b *testing.B) {
			a, err := NewHeadless(Config{Seed: 42, Width: 200, Height: 60, Resources: resource.Options{Embedded: true}})
			if err != nil {
				b.Fatalf("headless: %v", err)
			}
			defer a.Close()
			a.Tick(1)
			w := a.World()
			a.Region(event.RegionPause, "main", "")
			a.Region(event.RegionSpawn, "devourer", "DevourerSetup")
			for range 2000 {
				// A cursor in the open space, kept alive by heat whatever a gut does to it
				if cursor := w.Resources.Player.Slot(0); cursor != 0 {
					a.Context().PushEventOrigin(event.EventHeatSetRequest,
						&event.HeatSetRequestPayload{Entity: cursor, Value: parameter.HeatMax}, event.OriginDebug)
					a.Settle()
				}
				a.Tick(1)
				if e := w.Components.Devourer.Entities(); len(e) == 1 {
					if d, _ := w.Components.Devourer.GetPtr(e[0]); d.Guts[1] != 0 {
						break
					}
				}
			}
			if w.Components.DevourerGut.CountEntities() < 2 {
				b.Fatal("no devourer with two guts")
			}
			w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
			devourer := renderer.NewDevourerRenderer(a.Context())
			ctx := render.NewRenderContextFromGame(a.Context(), *w.Resources.Time, 0, 0, false)
			buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, a.Context().Width, a.Context().Height)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				a.Context().PushEventOrigin(event.EventHeatSetRequest,
					&event.HeatSetRequestPayload{Entity: w.Resources.Player.Slot(0), Value: parameter.HeatMax}, event.OriginDebug)
				a.Settle()
				if !frames {
					b.StartTimer()
					a.Tick(1)
					continue
				}
				a.Tick(1)
				ctx = render.NewRenderContextFromGame(a.Context(), *w.Resources.Time, 0, 0, false)
				b.StartTimer()
				for range int(parameter.GameUpdateInterval / parameter.FrameUpdateInterval) {
					buf.Clear()
					devourer.Render(ctx, buf)
				}
			}
		})
	}
}

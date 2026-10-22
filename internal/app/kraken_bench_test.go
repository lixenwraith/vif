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

// BenchmarkKrakenRegion measures the Kraken in the embedded scenario's kraken region,
// chasing a cursor held at full heat from corner to corner: "tick" is one simulation
// tick, "frames" the frames of a tick the Kraken renderer draws. The browser build is
// where its cost showed, so compare a change there too, from internal/app:
//
//	GOOS=js GOARCH=wasm go test -c -o kraken.wasm .
//	node "$(go env GOROOT)/lib/wasm/wasm_exec_node.js" kraken.wasm \
//		-test.run '^$' -test.bench BenchmarkKrakenRegion -test.benchtime 1500x
func BenchmarkKrakenRegion(b *testing.B) {
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
			a.Tick(40)
			a.Context().PushEventOrigin(event.EventCursorSpawnRequest,
				&event.CursorSpawnRequestPayload{Auto: true, Center: true}, event.OriginDebug)
			a.Settle()
			a.Tick(20)
			w := a.World()
			cursor := w.Resources.Player.Slot(0)
			a.Context().PushEventOrigin(event.EventCursorMoveRequest,
				&event.CursorMoveRequestPayload{Entity: cursor, X: 2, Y: 2}, event.OriginDebug)
			a.Settle()
			a.Region(event.RegionPause, "main", "")
			a.Region(event.RegionSpawn, "kraken", "KrakenSetup")
			a.Tick(25) // past the spawn, into the chase
			if w.Components.Kraken.CountEntities() != 1 {
				b.Fatal("no kraken")
			}
			w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
			kraken := renderer.NewKrakenRenderer(a.Context())
			ctx := render.NewRenderContextFromGame(a.Context(), *w.Resources.Time, 0, 0, false)
			buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, a.Context().Width, a.Context().Height)
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				b.StopTimer()
				if i%60 == 0 { // three seconds a corner, so the Kraken keeps moving
					x, y := 2, 2
					if i/60%2 == 1 {
						x, y = w.Resources.Config.MapWidth-3, w.Resources.Config.MapHeight-3
					}
					a.Context().PushEventOrigin(event.EventCursorMoveRequest,
						&event.CursorMoveRequestPayload{Entity: cursor, X: x, Y: y}, event.OriginDebug)
				}
				a.Context().PushEventOrigin(event.EventHeatSetRequest,
					&event.HeatSetRequestPayload{Entity: cursor, Value: parameter.HeatMax}, event.OriginDebug)
				a.Settle()
				if !frames {
					b.StartTimer()
					a.Tick(1)
					continue
				}
				a.Tick(1)
				b.StartTimer()
				for range int(parameter.GameUpdateInterval / parameter.FrameUpdateInterval) {
					kraken.Render(ctx, buf)
				}
			}
			b.StopTimer()
			if w.Components.Kraken.CountEntities() != 1 {
				b.Fatal("kraken gone")
			}
		})
	}
}

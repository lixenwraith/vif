package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/render/renderer"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/system"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// BenchmarkHeadlessTicks measures one simulation tick on a driven App: the embedded
// scenario played by one cursor; td, which an unattended run restarts every few
// seconds, so its ticks carry a level's maze and route graphs; and vanguard, its
// scenery drifting over the scrolling cave. Run with -benchtime=<n>x so every run
// simulates the same ticks of one seed and allocations compare exactly.
func BenchmarkHeadlessTicks(b *testing.B) {
	cases := []struct {
		name   string
		cursor bool
		cfg    Config
	}{
		{"main-160x50", true, Config{Seed: 42, Width: 160, Height: 50, Resources: resource.Options{Embedded: true}}},
		{"td-200x60", false, Config{Seed: 42, Width: 200, Height: 60, Resources: resource.Options{Dir: "../../wad", Scenario: "td"}}},
		{"vanguard-200x60", false, Config{Seed: 42, Width: 200, Height: 60, Resources: resource.Options{Dir: "../../wad", Scenario: "vanguard"}}},
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

// BenchmarkQuasars measures one tick with n quasars at once: the embedded scenario at
// the map limit with main paused, and a cursor holding :god's energy and no heat, so
// nothing else spawns and the game cannot end. The cursor crosses between two corners
// every five seconds, farther than the zap radius, so the far half charges, zaps and
// turns back to homing. Profile one count with -benchtime=<n>x and -cpuprofile.
func BenchmarkQuasars(b *testing.B) {
	for _, n := range []int{1, 16, 64, 256} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			a, err := NewHeadless(Config{Seed: 42, Width: 200, Height: 60, Resources: resource.Options{Embedded: true}})
			if err != nil {
				b.Fatalf("headless: %v", err)
			}
			defer a.Close()
			a.Tick(60)
			a.Region(event.RegionPause, "main", "")
			w := a.World()
			if got := w.Resources.Player.Count(); got != 1 {
				b.Fatalf("%d cursors, want the scenario's one", got)
			}
			cursor := w.Resources.Player.Slot(0)
			a.Context().PushEventOrigin(event.EventEnergySetRequest,
				&event.EnergySetPayload{Entity: cursor, Value: parameter.GodEnergyAmount}, event.OriginDebug)
			a.Context().PushEventOrigin(event.EventHeatSetRequest,
				&event.HeatSetRequestPayload{Entity: cursor, Value: 0}, event.OriginDebug)
			mapW, mapH := w.Resources.Config.MapWidth, w.Resources.Config.MapHeight
			cols := min(n, 32)
			rows := (n + cols - 1) / cols
			for i := range n {
				a.Context().PushEventOrigin(event.EventQuasarSpawnRequest, &event.QuasarSpawnRequestPayload{
					X: 4 + i%cols*(mapW-8)/max(cols-1, 1),
					Y: 3 + i/cols*(mapH-6)/max(rows-1, 1),
				}, event.OriginDebug)
			}
			a.Settle()
			a.Tick(1)
			if got := w.Components.Quasar.CountEntities(); got != n {
				b.Fatalf("%d quasars spawned, want %d", got, n)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if i%100 == 0 {
					b.StopTimer()
					x, y := 2, 2
					if i/100%2 == 1 {
						x, y = mapW-3, mapH-3
					}
					a.Context().PushEventOrigin(event.EventCursorMoveRequest,
						&event.CursorMoveRequestPayload{Entity: cursor, X: x, Y: y}, event.OriginDebug)
					a.Settle()
					b.StartTimer()
				}
				a.Tick(1)
			}
			b.StopTimer()
			if got := w.Components.Quasar.CountEntities(); got != n {
				b.Fatalf("%d quasars left of %d; the shape changed under the benchmark", got, n)
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
	w, h := a.World().Positions.Width, a.World().Positions.Height
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

// effectRig is a bare world running the death effects alone: their systems, the
// timer and death systems that end them, and their renderers
type effectRig struct {
	w        *engine.World
	systems  []engine.System
	handlers map[event.EventType][]event.Handler
	draw     []render.SystemRenderer
	ctx      render.RenderContext
	buf      *render.RenderBuffer
}

func newEffectRig(tb testing.TB) *effectRig {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 200, 60, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	r := &effectRig{w: w, handlers: map[event.EventType][]event.Handler{}}
	for _, build := range []func(*engine.World) engine.System{
		system.NewTimerSystem, system.NewSpiritSystem, system.NewFlashSystem, system.NewFadeoutSystem, system.NewDeathSystem,
	} {
		s := build(w)
		s.Init()
		r.systems = append(r.systems, s)
		h := s.(event.Handler)
		for _, et := range h.EventTypes() {
			r.handlers[et] = append(r.handlers[et], h)
		}
	}
	r.draw = []render.SystemRenderer{renderer.NewFlashRenderer(game), renderer.NewFadeoutRenderer(game), renderer.NewSpiritRenderer(game)}
	config := w.Resources.Config
	r.ctx = render.NewRenderContextFromGame(game, *w.Resources.Time, 0, 0, false)
	r.buf = render.NewRenderBuffer(terminal.ColorModeTrueColor, game.Width, game.Height)
	if config.MapWidth < 100 || config.MapHeight < 40 {
		tb.Fatalf("map %dx%d", config.MapWidth, config.MapHeight)
	}
	return r
}

// tick runs one tick: every system's update, then the settle
func (r *effectRig) tick() {
	for _, s := range r.systems {
		s.Update()
	}
	q := r.w.Resources.Event.Queue
	for {
		events := q.Consume()
		if len(events) == 0 {
			return
		}
		for _, ev := range events {
			for _, s := range r.handlers[ev.Type] {
				s.HandleEvent(ev)
			}
		}
	}
}

func (r *effectRig) frames() {
	for range int(parameter.GameUpdateInterval / parameter.FrameUpdateInterval) {
		for _, d := range r.draw {
			d.Render(r.ctx, r.buf)
		}
	}
}

// BenchmarkDeathEffects measures the effects a death leaves and the converging
// spirit, on a bare world: "tick" is one tick's share of spawning, ageing and ending
// them, "frames" a tick's three frames of drawing them. bodies is a 5x3 body dying
// every tick, storm 600 cells going at once every ten ticks.
func BenchmarkDeathEffects(b *testing.B) {
	loads := []struct {
		name         string
		cells, every int
	}{{"bodies", 15, 1}, {"storm", 600, 10}}
	for _, effect := range []string{"flash", "fadeout", "spirit", "scatter"} {
		for _, load := range loads {
			for _, frames := range []bool{false, true} {
				name := "tick"
				if frames {
					name = "frames"
				}
				b.Run(fmt.Sprintf("%s/%s/%s", effect, load.name, name), func(b *testing.B) {
					r := newEffectRig(b)
					rng := vmath.NewFastRand(7)
					width, height := r.w.Resources.Config.MapWidth, r.w.Resources.Config.MapHeight
					spawn := func() {
						x0, y0 := rng.Intn(width-40), rng.Intn(height-20)
						cols := 5
						if load.cells > 15 {
							cols = 30
						}
						switch effect {
						case "flash":
							batch := event.EffectBatchPool.Acquire()
							for i := range load.cells {
								batch.Entries = append(batch.Entries, component.EffectCell{X: x0 + i%cols, Y: y0 + i/cols, Rune: 'x'})
							}
							r.w.PushLocal(event.EventFlashSpawnBatchRequest, batch)
						case "fadeout":
							batch := event.EffectBatchPool.Acquire()
							for i := range load.cells {
								batch.Entries = append(batch.Entries, component.EffectCell{X: x0 + i%cols, Y: y0 + i/cols, Rune: 'x',
									Fg: color.RGB{R: 200, G: 100, B: 50}, Bg: color.RGB{R: 20, G: 40, B: 60}, Solid: true})
							}
							r.w.PushLocal(event.EventFadeoutSpawnBatch, batch)
						case "spirit":
							for i := range load.cells {
								r.w.PushEventDomain(event.EventSpiritSpawnRequest, &event.SpiritSpawnRequestPayload{
									StartX: x0 + i%cols, StartY: y0 + i/cols, TargetX: x0 + 2, TargetY: y0 + 1, Char: 'x',
									BaseColor: component.SpiritCyan}, core.DomainPlayer)
							}
						case "scatter":
							batch := event.EffectBatchPool.Acquire()
							for i := range load.cells {
								batch.Entries = append(batch.Entries, component.EffectCell{X: x0 + i%cols, Y: y0 + i/cols, Rune: 'x',
									Fg: color.RGB{R: 200, G: 100, B: 50}})
							}
							r.w.PushLocal(event.EventSpiritScatterRequest, batch)
						}
					}
					step := func(i int) {
						if i%load.every == 0 {
							spawn()
						}
						r.tick()
					}
					for i := range 40 { // to the steady state
						step(i)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := range b.N {
						if !frames {
							step(i)
							continue
						}
						b.StopTimer()
						step(i)
						r.buf.Clear()
						b.StartTimer()
						r.frames()
					}
				})
			}
		}
	}
}

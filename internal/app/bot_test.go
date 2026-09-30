package app

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/resource"
)

// playBot runs one embedded graph solo for n ticks on a fresh headless instance,
// journaling into capture when one is given, and returns the instance, still open.
// The graph is read from the binary rather than resolved, so no installed copy
// stands in for it.
func playBot(t *testing.T, name string, seed uint64, n int, capture *journal.Capture) (*App, bot.Stats) {
	t.Helper()
	data, err := fs.ReadFile(asset.DefaultBots, name+".toml")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := bot.ParseGraph(name, data)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Seed: seed, Width: 120, Height: 40, Resources: resource.Options{Embedded: true}}
	if capture != nil {
		cfg.Journal, cfg.JournalSink = true, capture
	}
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	d, err := bot.NewDriver(a, a.ctx, graph, a.Seed(), a.localParticipant())
	if err != nil {
		t.Fatal(err)
	}
	for range n {
		if more, err := d.Step(); !more || err != nil {
			t.Fatalf("%s: more %v, err %v", name, more, err)
		}
	}
	return a, d.Stats()
}

// TestAMapCellPointerReachesOnlyWhatTheViewportShows: a bot's pointer names map
// cells, and like a mouse it moves the cursor onto a cell the viewport shows and
// onto no other.
func TestAMapCellPointerReachesOnlyWhatTheViewportShows(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 100, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	if !a.SetupLevel(400, 120, true, false) {
		t.Fatal("a solo run refused a map larger than its viewport")
	}
	tickUntilCursor(t, a)

	var shownX, shownY, hiddenX, hiddenY int
	a.World().RunSafe(func() {
		cfg := a.World().Resources.Config
		pos, _ := a.World().LocalCursor()
		shownX, shownY = pos.X+1, pos.Y
		hiddenX, hiddenY = cfg.CameraX+cfg.ViewportWidth+5, pos.Y
		if _, _, ok := cfg.MapToViewport(shownX, shownY); !ok {
			t.Fatalf("the cell beside the cursor, %d,%d, is off screen", shownX, shownY)
		}
		if _, _, ok := cfg.MapToViewport(hiddenX, hiddenY); ok || hiddenX >= cfg.MapWidth {
			t.Fatalf("%d,%d is not a map cell off screen", hiddenX, hiddenY)
		}
	})
	point := func(x, y int) (atX, atY int) {
		a.handleIntent(&input.Intent{Type: input.IntentMouseMove, X: x, Y: y, MapCell: true})
		a.World().RunSafe(func() {
			pos, _ := a.World().LocalCursor()
			atX, atY = pos.X, pos.Y
		})
		return atX, atY
	}

	if x, y := point(hiddenX, hiddenY); x == hiddenX && y == hiddenY {
		t.Fatalf("the pointer reached %d,%d, which the viewport does not show", x, y)
	}
	if x, y := point(shownX, shownY); x != shownX || y != shownY {
		t.Fatalf("the pointer named %d,%d and the cursor is on %d,%d", shownX, shownY, x, y)
	}
}

// TestASoloBotRunIsAPureFunctionOfItsSeed: a graph draws from its own stream and reads
// a deterministic world, so one seed journals one run.
func TestASoloBotRunIsAPureFunctionOfItsSeed(t *testing.T) {
	t.Parallel()
	var runs [2][]event.JournalRecord
	for i := range runs {
		capture := journal.NewCapture()
		a, _ := playBot(t, "roam", fixtureSeed, 600, capture)
		a.Close()
		runs[i] = capture.Records()
	}
	typed := slices.ContainsFunc(runs[0], func(r event.JournalRecord) bool { return r.Type == event.EventCharacterTyped })
	if !typed {
		t.Fatal("the run typed nothing, so its equality proves nothing")
	}
	if !slices.Equal(runs[0], runs[1]) {
		t.Fatalf("one seed journaled %d and %d records that differ", len(runs[0]), len(runs[1]))
	}
}

// TestEveryShippedGraphPlays: each embedded graph loads, acts, and never queues more
// than its rate releases.
func TestEveryShippedGraphPlays(t *testing.T) {
	t.Parallel()
	files, err := fs.Glob(asset.DefaultBots, "*.toml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no shipped graphs: %v", err)
	}
	for _, file := range files {
		a, st := playBot(t, strings.TrimSuffix(file, ".toml"), fixtureSeed, 1200, nil)
		a.Close()
		if st.Injected == 0 || st.Dropped != 0 {
			t.Errorf("%s: injected %d, dropped %d", file, st.Injected, st.Dropped)
		}
	}
}

// TestRoamTypesWhatItReaches: roam reads the glyph under its cursor before it types.
// A miss is a glyph the tick's own fire destroyed after the read, which is rare.
func TestRoamTypesWhatItReaches(t *testing.T) {
	t.Parallel()
	a, _ := playBot(t, "roam", fixtureSeed, 1200, nil)
	defer a.Close()
	reg := a.world.Resources.Status
	typed, missed := reg.Ints.Get("typing.correct").Load(), reg.Ints.Get("typing.errors").Load()
	if typed < 100 || missed*20 > typed {
		t.Fatalf("roam typed %d and missed %d in a minute of play", typed, missed)
	}
}

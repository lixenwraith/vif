package app

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestAReplayRestoredFromACheckpointContinuesAsItsSource: a copy restored where
// another replay stood steps on through the same simulation surface, across a game
// reset and in a guest's journal, whose session it stands in for.
func TestAReplayRestoredFromACheckpointContinuesAsItsSource(t *testing.T) {
	t.Parallel()
	t.Run("solo across a reset", func(t *testing.T) {
		data, err := fs.ReadFile(asset.DefaultBots, "default.toml")
		if err != nil {
			t.Fatal(err)
		}
		graph, err := bot.ParseGraph("default", data)
		if err != nil {
			t.Fatal(err)
		}
		rec := journal.NewCapture()
		a, err := NewHeadless(Config{Seed: 0xC1, Width: 120, Height: 40,
			Resources: resource.Options{Embedded: true}, Journal: true, JournalSink: rec})
		if err != nil {
			t.Fatal(err)
		}
		d, err := bot.NewDriver(a, a.ctx, graph, a.Seed(), a.localParticipant())
		if err != nil {
			t.Fatal(err)
		}
		for i := range 1100 {
			if i == 600 {
				a.Reset(false)
			}
			if more, err := d.Step(); !more || err != nil {
				t.Fatalf("bot: more %v, err %v", more, err)
			}
		}
		a.Close()
		continuesFromCheckpoints(t, rec, []event.Stamp{{Run: 0, Tick: 450}, {Run: 1, Tick: 200}}, 250)
	})
	t.Run("guest", func(t *testing.T) {
		const seed = 0xC2
		host := mustHeadless(t, seed, 120, 40)
		defer host.Close()
		tickUntilCursor(t, host)
		host.Tick(120)
		if err := host.BeginHosting("127.0.0.1:0"); err != nil {
			t.Fatalf("host: %v", err)
		}
		stop := tickInBackground(host)
		rec := journal.NewCapture()
		guest, _ := mustSocketJoiner(t, host.HostAddr(), seed, 120, 40, func(c *Config) {
			c.Journal, c.JournalSink = true, rec
		})
		stop()
		waitForRosterPair(t, host, guest)
		rng := vmath.NewFastRand(7)
		motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
		for range 500 {
			for _, p := range []*App{host, guest} {
				if rng.Intn(3) == 0 {
					inject(t, p, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
				}
				if rng.Intn(5) == 0 {
					inject(t, p, &input.Intent{Type: input.IntentFireMain, Count: 1})
				}
			}
			host.Tick(1)
			guest.Tick(1)
			guest.ApplyPendingCorrections()
		}
		guest.Close()
		joined := rec.Captures()[0].Tick
		continuesFromCheckpoints(t, rec, []event.Stamp{{Tick: joined + 150}}, 250)
	})
}

// continuesFromCheckpoints replays a journal, and at each stamp restores a second
// copy from a checkpoint of the first and steps both, comparing every step.
func continuesFromCheckpoints(t *testing.T, rec *journal.Capture, at []event.Stamp, steps int) {
	t.Helper()
	cfg, err := ConfigFromAnchor(rec.Anchors()[0])
	if err != nil {
		t.Fatal(err)
	}
	end := rec.End()
	set := journal.Set{Anchors: rec.Anchors(), Records: rec.Records(), Captures: rec.Captures(), Digests: rec.Digests(), End: &end}
	src, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	d, err := replayDriver(src, set)
	if err != nil {
		t.Fatal(err)
	}
	for _, stamp := range at {
		for p := src.Position(); p.Run < stamp.Run || p.Run == stamp.Run && p.Tick < stamp.Tick; p = src.Position() {
			if more, err := d.Step(); err != nil || !more {
				t.Fatalf("replay to %+v: more %v, err %v", stamp, more, err)
			}
		}
		var ck *checkpoint
		src.world.RunSafe(func() { ck, err = src.checkpointLocked(d) })
		if err != nil {
			t.Fatal(err)
		}
		copied, err := NewHeadless(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cd, err := replayDriver(copied, set)
		if err != nil {
			t.Fatal(err)
		}
		if err := copied.restore(ck, cd); err != nil {
			t.Fatal(err)
		}
		for i := range steps {
			want, got := src.SnapshotSimulation(), copied.SnapshotSimulation()
			if n, _, _, ok := snapshot.FirstDiff(want, got); ok {
				t.Fatalf("restored at %+v, differs %d steps on at line %d:\n%s", stamp, i, n,
					strings.Join(snapshot.Diff(want, got, 12), "\n"))
			}
			more, err := d.Step()
			cmore, cerr := cd.Step()
			if err != nil || cerr != nil || more != cmore {
				t.Fatalf("step %d from %+v: source %v %v, copy %v %v", i, stamp, more, err, cmore, cerr)
			}
			if !more {
				break
			}
		}
		if st := cd.Stats(); st.Diverged != nil {
			t.Fatalf("the restored copy failed a digest: %v", st.Diverged)
		}
		copied.Close()
	}
}

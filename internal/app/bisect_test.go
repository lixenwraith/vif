package app

import (
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/journal"
)

var bisectSeed = flag.Uint64("soak.seed", 0, "seed to bisect in TestReplayBisect")

// bisectOnce journals the first steps script actions and replays them
func bisectOnce(t *testing.T, seed uint64, steps int) error {
	t.Helper()
	cap := journal.NewCapture()
	cfg := scriptConfig(seed)
	cfg.Journal, cfg.JournalSink = true, cap

	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}
	if _, err := journal.RunFuzz(a, journal.DefaultFuzz(seed, steps)); err != nil {
		a.Close()
		t.Fatalf("script: %v", err)
	}
	want, end := a.SnapshotSimulation(), a.Position()
	a.Close()
	if len(cap.Records()) == 0 {
		return nil // nothing recorded yet, nothing to reproduce
	}
	return replayInto(cap.Anchors(), cap.Records(), want, end)
}

// TestReplayBisect reports the shortest script prefix whose replay diverges, then
// dumps the records of the last two ticks of that prefix
func TestReplayBisect(t *testing.T) {
	if *bisectSeed == 0 {
		t.Skip("set -soak.seed=0x... to bisect one seed")
	}
	seed := *bisectSeed
	if err := bisectOnce(t, seed, soakSteps); err == nil {
		t.Fatalf("seed %#x reproduces at %d steps", seed, soakSteps)
	}

	lo, hi := 0, soakSteps // lo reproduces, hi diverges
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if bisectOnce(t, seed, mid) == nil {
			lo = mid
		} else {
			hi = mid
		}
	}

	cap := journal.NewCapture()
	cfg := scriptConfig(seed)
	cfg.Journal, cfg.JournalSink = true, cap
	a, _ := NewHeadless(cfg)
	_, _ = journal.RunFuzz(a, journal.DefaultFuzz(seed, hi))
	end := a.Position()
	a.Close()

	t.Logf("seed %#x first diverges at step %d, ending at %+v", seed, hi, end)
	for _, r := range cap.Records() {
		if r.Run == end.Run && r.Tick+2 >= end.Tick {
			t.Logf("jseq %d run %d tick %d boundary %d seq %d %s %s payload=%q",
				r.JSeq, r.Run, r.Tick, r.Boundary, r.Seq,
				r.Origin, event.GetEventName(r.Type), r.Payload)
		}
	}
	t.Fatalf("bisected to step %d", hi)
}

// TestReplayLockstep replays against the source's own per-tick snapshots, so a
// divergence names the tick it started on rather than the run that ended wrong
func TestReplayLockstep(t *testing.T) {
	if *bisectSeed == 0 {
		t.Skip("set -soak.seed=0x... to lockstep one seed")
	}
	seed := *bisectSeed

	cap := journal.NewCapture()
	cfg := scriptConfig(seed)
	cfg.Journal, cfg.JournalSink = true, cap

	src, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}
	// Perturb runs after every action and draws from no stream, so snapshotting
	// here records the source's state at the last action of each tick
	want := make(map[event.Stamp][]string, soakSteps)
	opt := journal.DefaultFuzz(seed, soakSteps)
	opt.Perturb = func() {
		p := src.Position()
		want[event.Stamp{Run: p.Run, Tick: p.Tick}] = src.SnapshotSimulation()
	}
	if _, err := journal.RunFuzz(src, opt); err != nil {
		src.Close()
		t.Fatalf("script: %v", err)
	}
	src.Close()

	anchors := cap.Anchors()
	rcfg, err := ConfigFromAnchor(anchors[0])
	if err != nil {
		t.Fatalf("config from anchor: %v", err)
	}
	rep, err := NewHeadless(rcfg)
	if err != nil {
		t.Fatalf("replay run: %v", err)
	}
	defer rep.Close()
	if err := rep.VerifyAnchor(anchors[0]); err != nil {
		t.Fatalf("verify anchor: %v", err)
	}
	d, err := newReplayDriver(rep, cap.Records())
	if err != nil {
		t.Fatalf("driver: %v", err)
	}

	for {
		more, err := d.Step()
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if !more {
			return
		}
		p := rep.Position()
		w, ok := want[event.Stamp{Run: p.Run, Tick: p.Tick}]
		if !ok {
			continue // the source took no snapshot on this tick
		}
		got := rep.SnapshotSimulation()
		if i, _, _, ok := FirstDiff(w, got); ok {
			t.Fatalf("diverged in run %d tick %d at line %d:\n%s\nrecords on this tick:\n%s",
				p.Run, p.Tick, i, strings.Join(Diff(w, got, 12), "\n"),
				strings.Join(recordsAt(cap.Records(), p.Run, p.Tick), "\n"))
		}
	}
}

// recordsAt renders every record stamped on one tick
func recordsAt(recs []event.JournalRecord, run, tick uint64) []string {
	var out []string
	for _, r := range recs {
		if r.Run == run && r.Tick == tick {
			out = append(out, fmt.Sprintf("  jseq %d boundary %d seq %d %s %s payload=%q",
				r.JSeq, r.Boundary, r.Seq, r.Origin, event.GetEventName(r.Type), r.Payload))
		}
	}
	return out
}

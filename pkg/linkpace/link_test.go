package linkpace

import (
	"testing"
	"time"
)

// clock is the deterministic stand-in for wall time. Nothing in this package
// reads a clock — every duration arrives inside a Sample — so the tests carry
// their own and the adaptation logic is reproducible by construction.
type clock struct{ at time.Time }

func newClock() *clock { return &clock{at: time.Unix(0, 0)} }

func (c *clock) advance(d time.Duration) time.Time {
	c.at = c.at.Add(d)
	return c.at
}

func steady(rtt time.Duration, delivered int64, elapsed time.Duration) Sample {
	return Sample{RTT: rtt, Delivered: delivered, Elapsed: elapsed}
}

func TestLinkSmoothsTheRoundTripAndTracksItsMinimum(t *testing.T) {
	l := NewLink(LinkConfig{})
	for range 20 {
		l.Observe(steady(80*time.Millisecond, 0, 0))
	}
	m := l.Metrics()
	if m.RTT < 79*time.Millisecond || m.RTT > 81*time.Millisecond {
		t.Fatalf("rtt settled at %s, want about 80ms", m.RTT)
	}
	if m.MinRTT != 80*time.Millisecond {
		t.Fatalf("minimum rtt %s, want 80ms", m.MinRTT)
	}
	if m.Jitter > time.Millisecond {
		t.Fatalf("a steady link reported %s of jitter", m.Jitter)
	}
	if !m.Ready {
		t.Fatal("twenty round trips did not make the estimate steerable")
	}

	// One excursion must not move the minimum, which is the baseline every
	// inflation check is measured against.
	l.Observe(steady(400*time.Millisecond, 0, 0))
	if got := l.Metrics().MinRTT; got != 80*time.Millisecond {
		t.Fatalf("a slow sample moved the minimum to %s", got)
	}
}

func TestLinkJitterRisesWithVariationAndFallsWithout(t *testing.T) {
	l := NewLink(LinkConfig{})
	for i := range 60 {
		rtt := 60 * time.Millisecond
		if i%2 == 0 {
			rtt = 140 * time.Millisecond
		}
		l.Observe(steady(rtt, 0, 0))
	}
	varying := l.Metrics().Jitter
	if varying < 10*time.Millisecond {
		t.Fatalf("an alternating 60/140ms link reported %s of jitter", varying)
	}
	for range 200 {
		l.Observe(steady(60*time.Millisecond, 0, 0))
	}
	if settled := l.Metrics().Jitter; settled >= varying/2 {
		t.Fatalf("jitter stayed at %s after the link steadied (was %s)", settled, varying)
	}
}

// TestLinkReportsSaturationOnlyWhenTheLinkWasTheLimit is the estimator's central
// claim. A delivery rate measured while the sender had nothing queued is a lower
// bound on capacity and not a measurement of it; a controller that throttled on
// one would narrow a link that was merely idle.
func TestLinkReportsSaturationOnlyWhenTheLinkWasTheLimit(t *testing.T) {
	cfg := LinkConfig{BacklogBytes: 32 << 10, InflationRatio: 1.5, InflationFloor: 20 * time.Millisecond}

	idle := NewLink(cfg)
	for range 10 {
		idle.Observe(Sample{RTT: 20 * time.Millisecond, Delivered: 4096, Elapsed: time.Second})
	}
	if m := idle.Metrics(); m.Saturated {
		t.Fatal("an idle sender's 4 KB/s was reported as the link's limit")
	}

	queued := NewLink(cfg)
	for range 10 {
		queued.Observe(Sample{RTT: 20 * time.Millisecond, Delivered: 4096, Elapsed: time.Second, Backlog: 64 << 10})
	}
	if m := queued.Metrics(); !m.Saturated {
		t.Fatal("64 KiB standing in the queue was not read as saturation")
	}

	inflated := NewLink(cfg)
	for range 10 {
		inflated.Observe(Sample{RTT: 20 * time.Millisecond, Delivered: 4096, Elapsed: time.Second})
	}
	inflated.Observe(Sample{RTT: 200 * time.Millisecond, Delivered: 4096, Elapsed: time.Second})
	if m := inflated.Metrics(); !m.Saturated {
		t.Fatal("a round trip ten times its own baseline was not read as queueing")
	}
}

func TestLinkMissMovesLossAndLeavesTimingAlone(t *testing.T) {
	l := NewLink(LinkConfig{})
	for range 10 {
		l.Observe(steady(50*time.Millisecond, 0, 0))
	}
	before := l.Metrics()
	for range 10 {
		l.Miss()
	}
	after := l.Metrics()
	if after.Loss <= before.Loss {
		t.Fatalf("ten unanswered probes left loss at %.3f", after.Loss)
	}
	if after.RTT != before.RTT || after.MinRTT != before.MinRTT || after.Samples != before.Samples {
		t.Fatal("an unanswered probe invented a round trip")
	}
}

func TestLinkRefusesANonPositiveRoundTrip(t *testing.T) {
	l := NewLink(LinkConfig{})
	l.Observe(steady(50*time.Millisecond, 0, 0))
	l.Observe(steady(-time.Second, 0, 0))
	if got := l.Metrics().MinRTT; got != 50*time.Millisecond {
		t.Fatalf("a backwards clock dragged the minimum to %s", got)
	}
	if got := l.Metrics().Samples; got != 1 {
		t.Fatalf("a refused sample was counted: %d", got)
	}
}

// TestBulkTransferIsASaturatedMeasurement covers the one rate an admission
// decision has before any probe has completed: the join's own capture.
func TestBulkTransferIsASaturatedMeasurement(t *testing.T) {
	l := NewLink(LinkConfig{})
	l.ObserveTransfer(200_000, 2*time.Second)
	m := l.Metrics()
	if !m.Saturated {
		t.Fatal("a completed bulk transfer was not treated as a measurement of the link")
	}
	if m.Throughput < 99_000 || m.Throughput > 101_000 {
		t.Fatalf("200 KB in two seconds measured %.0f B/s", m.Throughput)
	}
	if got := TransferRate(200_000, 2*time.Second); got < 99_000 || got > 101_000 {
		t.Fatalf("TransferRate reported %.0f B/s", got)
	}
	if got := TransferRate(0, time.Second); got != 0 {
		t.Fatalf("an empty transfer measured %.0f B/s", got)
	}
}

func TestLinkResetForgetsTheConnectionThatIsGone(t *testing.T) {
	l := NewLink(LinkConfig{})
	for range 10 {
		l.Observe(Sample{RTT: 50 * time.Millisecond, Delivered: 1000, Elapsed: time.Second})
	}
	l.Reset()
	if m := l.Metrics(); m.Samples != 0 || m.RTT != 0 || m.Throughput != 0 || m.Ready {
		t.Fatalf("a reset link still reports %+v", m)
	}
}

// TestClockIsCarriedByTheCaller pins the property the package's testability
// rests on: an estimate is a function of the durations it is handed, so the same
// samples in the same order always produce the same metrics.
func TestClockIsCarriedByTheCaller(t *testing.T) {
	run := func() Metrics {
		c := newClock()
		l := NewLink(LinkConfig{})
		last := c.at
		for i := range 25 {
			now := c.advance(200 * time.Millisecond)
			l.Observe(Sample{
				RTT:       time.Duration(40+i%7) * time.Millisecond,
				Delivered: int64(2000 + i*10),
				Elapsed:   now.Sub(last),
				Backlog:   int64(i * 512),
			})
			last = now
		}
		return l.Metrics()
	}
	if a, b := run(), run(); a != b {
		t.Fatalf("the same samples produced %+v then %+v", a, b)
	}
}

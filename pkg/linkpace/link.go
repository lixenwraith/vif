// Package linkpace measures a link (Link: round trip, jitter, delivery rate, and
// whether that rate was the link's limit) and chooses how fast to publish over it
// (Controller: a cadence and keyframe interval inside declared bounds, never past
// the convergence floor; Admit refuses a link that cannot carry it). Standard
// library only, so no measurement can reach a world and become a game decision.
package linkpace

import (
	"math"
	"time"
)

// Cell is a participant's reported interest centre — where its own cursor stands.
// It is a hint for relevance and priority, never an input to simulation: the
// worst a wrong or stale one can do is send a correction sooner than it was
// needed.
type Cell struct {
	X     int32
	Y     int32
	Valid bool
}

// Sample is one completed round trip, plus what the far end reported inside it
// and what this end knows of its own sending.
//
// Delivered and Elapsed are a pair: how many bytes the far end confirmed having
// received, and the wall interval that confirmation covers. Backlog is what this
// sender has put on the link and not yet seen confirmed, which is the difference
// between "the link is fast" and "the link is idle". Held says the sender's
// frames have waited on the link longer than its echoes are stale: a sender pacing
// to the link never shows a backlog past the bound it paces to.
type Sample struct {
	RTT time.Duration

	Delivered int64
	Elapsed   time.Duration
	Backlog   int64
	Held      bool

	LagTicks  uint64
	Magnitude int
	Interest  Cell
}

// Metrics is one link's current estimate.
//
// Saturated is the field a consumer must read before believing Throughput. A
// rate measured while nothing was queued is a floor on capacity and not a
// measurement of it; a rate measured while bytes were waiting is the capacity.
type Metrics struct {
	RTT    time.Duration
	MinRTT time.Duration
	Jitter time.Duration

	// Throughput is the observed delivery rate in bytes per second.
	Throughput float64

	// Backlog is what the sender has queued for the peer beyond what the peer had
	// received at its latest echo, in bytes. The transport that owns both counters
	// fills it when the estimate is read; Link never does.
	Backlog int64

	// Saturated reports that the link, rather than the sender, was the limit
	// while Throughput was measured: a backlog stood across two echoes, or the
	// sender's frames were held waiting while the round trip stood inflated past
	// its baseline, by the queue they wait in. Neither half alone: jitter inflates
	// a trip as readily, and a sender's own bound holds frames a long link carries.
	Saturated bool

	// Loss is the share of probes that went unanswered, exponentially weighted.
	Loss float64

	LagTicks  uint64
	Magnitude int
	Interest  Cell

	Samples int

	// Ready reports that enough round trips have completed to steer from. A
	// controller given an unready link keeps its nominal operating point rather
	// than adapting to one measurement.
	Ready bool
}

// LinkConfig is what a [Link] needs to interpret its samples. The zero value is
// not usable; [DefaultLinkConfig] is.
type LinkConfig struct {
	// Smoothing weights the newest sample against the running estimate, for both
	// round-trip time and delivery rate. RFC 3550's 1/16 is the reference point
	// and the reason this is not simply an average: a link's interesting
	// behaviour is a step, not a mean.
	Smoothing float64

	// JitterSmoothing weights the newest inter-sample difference. Jitter moves
	// faster than the trip time it is derived from and is smoothed harder.
	JitterSmoothing float64

	// BacklogBytes is the queue depth past which the link, and not the sender,
	// is deciding the rate.
	BacklogBytes int64

	// InflationRatio and InflationFloor say when a round trip stands inflated: grown
	// past minRTT by both this ratio and this absolute margin, so that a fast
	// link's ordinary microsecond wobble is not read as a queue. Inflation reads
	// with held frames alone: the queue they wait in is what inflates the trip.
	InflationRatio float64
	InflationFloor time.Duration

	// ReadyAfter is how many completed round trips make an estimate steerable.
	ReadyAfter int
}

// DefaultLinkConfig is the production estimator.
func DefaultLinkConfig() LinkConfig {
	return LinkConfig{
		Smoothing:       1.0 / 8.0,
		JitterSmoothing: 1.0 / 16.0,
		BacklogBytes:    48 << 10,
		InflationRatio:  1.5,
		InflationFloor:  20 * time.Millisecond,
		ReadyAfter:      3,
	}
}

// Link is one peer's link estimate. It is not safe for concurrent use; the
// transport that owns the peer owns the estimate with it.
type Link struct {
	cfg LinkConfig

	rtt     time.Duration
	minRTT  time.Duration
	jitter  time.Duration
	lastRTT time.Duration

	throughput float64
	saturated  bool
	backlogged bool // the previous sample's backlog was past BacklogBytes

	loss float64

	lagTicks  uint64
	magnitude int
	interest  Cell

	samples int
}

// NewLink returns an estimator with no observations. A configuration field left
// zero takes its default, so a caller may override one without restating the rest.
func NewLink(cfg LinkConfig) *Link {
	def := DefaultLinkConfig()
	if cfg.Smoothing <= 0 || cfg.Smoothing > 1 {
		cfg.Smoothing = def.Smoothing
	}
	if cfg.JitterSmoothing <= 0 || cfg.JitterSmoothing > 1 {
		cfg.JitterSmoothing = def.JitterSmoothing
	}
	if cfg.BacklogBytes <= 0 {
		cfg.BacklogBytes = def.BacklogBytes
	}
	if cfg.InflationRatio <= 1 {
		cfg.InflationRatio = def.InflationRatio
	}
	if cfg.InflationFloor <= 0 {
		cfg.InflationFloor = def.InflationFloor
	}
	if cfg.ReadyAfter <= 0 {
		cfg.ReadyAfter = def.ReadyAfter
	}
	return &Link{cfg: cfg}
}

// Observe folds one completed round trip into the estimate.
//
// A sample with a non-positive round trip is refused rather than smoothed: a
// clock that ran backwards between the probe and its echo would otherwise drag
// minRTT below anything the link can do and make every later sample look
// inflated.
func (l *Link) Observe(s Sample) {
	if s.RTT <= 0 {
		return
	}
	if l.samples == 0 {
		l.rtt, l.minRTT = s.RTT, s.RTT
	} else {
		diff := s.RTT - l.lastRTT
		if diff < 0 {
			diff = -diff
		}
		l.jitter += time.Duration(l.cfg.JitterSmoothing * float64(diff-l.jitter))
		l.rtt += time.Duration(l.cfg.Smoothing * float64(s.RTT-l.rtt))
		if s.RTT < l.minRTT {
			l.minRTT = s.RTT
		}
	}
	l.lastRTT = s.RTT
	l.samples++

	if s.Elapsed > 0 && s.Delivered > 0 {
		rate := float64(s.Delivered) / s.Elapsed.Seconds()
		if l.throughput == 0 {
			l.throughput = rate
		} else {
			l.throughput += l.cfg.Smoothing * (rate - l.throughput)
		}
	}
	// A backlog counts once it stands across two echoes: one sample can land just
	// after the sender queued a burst larger than the threshold, a whole keyframe,
	// which a fast link drains long before the next.
	backlogged := s.Backlog >= l.cfg.BacklogBytes
	l.saturated = (backlogged && l.backlogged) || (s.Held && l.inflated(s.RTT))
	l.backlogged = backlogged

	l.loss += l.cfg.Smoothing * (0 - l.loss)
	l.lagTicks, l.magnitude = s.LagTicks, s.Magnitude
	if s.Interest.Valid {
		l.interest = s.Interest
	}
}

// Miss records a probe that was never answered. It moves loss and nothing else:
// an unanswered probe carries no timing, and inventing one would let a dead link
// report a healthy round trip.
func (l *Link) Miss() { l.loss += l.cfg.Smoothing * (1 - l.loss) }

// inflated reports a round trip past its baseline by the ratio and the floor.
func (l *Link) inflated(rtt time.Duration) bool {
	if l.minRTT <= 0 {
		return false
	}
	ratio := time.Duration(float64(l.minRTT) * l.cfg.InflationRatio)
	return rtt >= ratio && rtt-l.minRTT >= l.cfg.InflationFloor
}

// Metrics reports the current estimate.
func (l *Link) Metrics() Metrics {
	return Metrics{
		RTT:        l.rtt,
		MinRTT:     l.minRTT,
		Jitter:     l.jitter,
		Throughput: l.throughput,
		Saturated:  l.saturated,
		Loss:       l.loss,
		LagTicks:   l.lagTicks,
		Magnitude:  l.magnitude,
		Interest:   l.interest,
		Samples:    l.samples,
		Ready:      l.samples >= l.cfg.ReadyAfter,
	}
}

// Reset returns the estimator to its unobserved state, for a peer identity that
// has been reused by a different connection.
func (l *Link) Reset() {
	cfg := l.cfg
	*l = Link{cfg: cfg}
}

// ObserveTransfer folds a bulk transfer into the estimate — a join's capture,
// which is a throughput measurement nothing else on the link can make so early.
//
// It is a delivery rate by construction: the bytes went out, the far end
// answered when it had them all, and the sender was pushing the whole time. So
// unlike a probe sample it is unconditionally saturated, and it is the one
// measurement available before any probe has completed a round trip.
func (l *Link) ObserveTransfer(bytes int64, elapsed time.Duration) {
	if bytes <= 0 || elapsed <= 0 {
		return
	}
	rate := float64(bytes) / elapsed.Seconds()
	if l.throughput == 0 {
		l.throughput = rate
	} else {
		l.throughput += l.cfg.Smoothing * (rate - l.throughput)
	}
	l.saturated = true
}

// TransferRate is the delivery rate a completed bulk transfer demonstrates,
// which is what an admission decision has before any probe has returned.
func TransferRate(bytes int64, elapsed time.Duration) float64 {
	if bytes <= 0 || elapsed <= 0 {
		return 0
	}
	return float64(bytes) / elapsed.Seconds()
}

// milliseconds renders a duration for telemetry without importing a formatter.
func milliseconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(math.Round(float64(d) / float64(time.Millisecond)))
}

// RTTMillis and JitterMillis are the telemetry forms of the two timings. A measured
// round trip is at least 1: a loopback link would otherwise read as unmeasured.
func (m Metrics) RTTMillis() int64 {
	if m.RTT <= 0 {
		return 0
	}
	return max(1, milliseconds(m.RTT))
}

func (m Metrics) JitterMillis() int64 { return milliseconds(m.Jitter) }

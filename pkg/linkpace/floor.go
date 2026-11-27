package linkpace

import (
	"fmt"
	"time"
)

// FloorError is a link that cannot sustain the convergence floor. It carries the
// two numbers that make the condition actionable rather than a verdict: what the
// floor costs on this world, and what the link was measured to allow.
type FloorError struct {
	// RequiredBps is one whole world per floor window — the cheapest schedule
	// that honors the floor. AvailableBps is the measured delivery rate after
	// the utilization share.
	RequiredBps  float64
	AvailableBps float64

	// KeyframeBytes and Window are what those rates are derived from, so a log
	// line says which of the two moved.
	KeyframeBytes int64
	Window        time.Duration
}

func (e *FloorError) Error() string {
	return fmt.Sprintf(
		"link cannot sustain the convergence floor: a %d-byte world every %s needs %.0f B/s, the link carries %.0f B/s",
		e.KeyframeBytes, e.Window, e.RequiredBps, e.AvailableBps)
}

// FloorWindow is how long the floor allows between whole authoritative worlds.
func (b Bounds) FloorWindow() time.Duration {
	return time.Duration(b.FloorKeyframeTicks) * b.TickInterval
}

// FloorBps is what honoring the floor costs on a world of this size.
func (b Bounds) FloorBps(s Sizes) float64 { return floorRate(b, s) }

// Admit decides whether a link may join a session at all: a participant on a link
// that cannot carry one whole world per floor window would play wrong with nothing
// scheduled to repair it, so it is refused. A rate of zero is nothing measured,
// admitted rather than refused on no evidence, or nobody could join before a probe's
// round trip; the mid-session report covers a link worse than it looked.
func Admit(b Bounds, availableBps float64, s Sizes) error {
	if availableBps <= 0 {
		return nil
	}
	required := floorRate(b, s)
	if required <= availableBps*b.Utilization {
		return nil
	}
	return &FloorError{
		RequiredBps:   required,
		AvailableBps:  availableBps * b.Utilization,
		KeyframeBytes: s.Keyframe,
		Window:        b.FloorWindow(),
	}
}

// AdmitMetrics is Admit against a measured link, using the throughput only when
// the link was the limit while it was measured. An unsaturated rate is a floor
// on capacity and not a measurement of it, so refusing on one would turn a quiet
// moment into a rejected join.
func AdmitMetrics(b Bounds, m Metrics, s Sizes) error {
	if !m.Ready || !m.Saturated {
		return nil
	}
	return Admit(b, m.Throughput, s)
}

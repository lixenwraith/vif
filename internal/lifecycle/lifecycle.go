// Package lifecycle decides when a supervised session has outlived its reason to
// exist.
//
// A dedicated host started by hand is a long-lived thing: it waits, it is joined,
// it empties, and it waits again, because the person who started it is the one who
// will stop it. A session a website allocates has no such person. Something created
// a container on a player's behalf, and if that player never arrives, or arrives
// and leaves, nothing else will ever notice: the pod keeps a slot in a fleet whose
// capacity is counted in tens, and the cluster's answer to "is anyone in there" is
// a roster only the process can see.
//
// So the process answers it. This package holds the three durations that bound an
// allocated session — the window a first guest has to connect, the grace after the
// last one leaves, and how long a termination request waits for the roster to
// empty — and turns a sequence of roster observations into one of five phases and
// a single question: may this run still be joined, and should it still be running.
//
// It is deliberately a pure state machine over an injected clock. It opens no
// listener, reads no roster, and terminates nothing; the run supplies observations
// and acts on the phase. That keeps the policy testable at the resolution of a
// nanosecond rather than of a ninety-second integration test, and keeps the
// dedicated host's loop free of the arithmetic.
package lifecycle

import (
	"fmt"
	"sync"
	"time"
)

// Phase is where a session is in its allocated life.
//
// The phases are ordered by how much of that life is left, and the machine only
// ever moves forward through them with one exception: a guest arriving returns a
// vacant session to occupied, because a session that emptied and refilled inside
// its grace window is a session somebody is still playing.
type Phase uint8

const (
	// PhaseUnstarted is a controller that has not been given its start instant.
	// Nothing is counting down and nothing may be concluded from it — including
	// any refusal, which is why an unstarted controller still admits: a run whose
	// policy is not in force is one this package is not governing.
	PhaseUnstarted Phase = iota

	// PhaseWaiting is an allocated session no guest has ever reached. Its deadline
	// is the whole reason this package exists: an allocation nobody claims must
	// cost the fleet one window rather than one pod.
	PhaseWaiting

	// PhaseOccupied is a session with at least one guest in the roster. It has no
	// deadline; a game is not something to time out.
	PhaseOccupied

	// PhaseVacant is a session whose roster emptied after having been occupied.
	// The grace is what distinguishes a player who disconnected from a player who
	// left: the first dials back into the slot the departure released.
	PhaseVacant

	// PhaseDraining is a session asked to terminate. It admits nobody and waits for
	// the guests it still holds, which is the difference between an upgrade that
	// ends a match and one that ends after it.
	PhaseDraining

	// PhaseExpired is terminal. A deadline passed or a drain completed, and the run
	// should exit; the phase does not change again.
	PhaseExpired
)

// phaseNames indexes Phase for diagnostics and for the probe's reason line.
var phaseNames = [...]string{"unstarted", "waiting", "occupied", "vacant", "draining", "expired"}

// String returns the diagnostic name.
func (p Phase) String() string {
	if int(p) >= len(phaseNames) {
		return "invalid"
	}
	return phaseNames[p]
}

// Policy is the three durations an allocated session is bounded by. A zero
// duration disables its bound, which is what an interactively started host wants:
// it is supervised by the person who started it.
type Policy struct {
	// FirstJoin is how long an allocated session waits for its first guest before
	// concluding nobody is coming. It runs from Start, not from process start, so
	// image pull and world construction do not consume a player's window.
	FirstJoin time.Duration

	// Empty is the grace after the last guest leaves. It is a separate value from
	// FirstJoin because the two answer different questions — nobody came, versus
	// everybody left — even where a deployment happens to give them the same number.
	Empty time.Duration

	// Drain is how long a termination request waits for the roster to empty before
	// exiting anyway. Zero means a termination request exits at once, which is the
	// behaviour of a run that never asked for a drain.
	Drain time.Duration
}

// Bounded reports whether any deadline is set. An unbounded policy is a long-lived
// host: the machine still tracks phases, and nothing ever expires.
func (p Policy) Bounded() bool {
	return p.FirstJoin > 0 || p.Empty > 0 || p.Drain > 0
}

// Validate rejects a negative duration. A negative bound is not a disabled one —
// the caller meant something by it and this cannot tell what.
func (p Policy) Validate() error {
	for _, f := range []struct {
		name string
		d    time.Duration
	}{{"first-join", p.FirstJoin}, {"empty", p.Empty}, {"drain", p.Drain}} {
		if f.d < 0 {
			return fmt.Errorf("lifecycle: -%s %s is negative; 0 disables it", f.name, f.d)
		}
	}
	return nil
}

// State is one read of the machine: what phase the session is in, whether it may
// still be joined, whether it should still be running, and when the current
// countdown ends.
//
// Admit is a refusal only where the machine has concluded one. A controller that
// was never started admits, so a caller can consult it unconditionally without
// having to know whether this run is an allocated session or an interactive host.
//
// Deadline is zero when nothing is counting down, which is both the occupied case
// and every disabled bound. A caller renders Remaining rather than computing it, so
// two readers of one state report the same number.
type State struct {
	Phase   Phase
	Reason  string
	Guests  int
	Admit   bool
	Expired bool

	Deadline  time.Time
	Remaining time.Duration

	// Vacant is how long the roster has been empty, and zero in every other phase.
	// A bounded session reads Remaining and ends on it; an unbounded one has no
	// deadline to read, and this is what a long-lived host parks and restarts on
	// instead of ending.
	Vacant time.Duration
}

// Controller is the machine. The zero value is not usable; call New.
type Controller struct {
	policy Policy

	mu      sync.Mutex
	phase   Phase
	guests  int
	startAt time.Time // when PhaseWaiting began
	since   time.Time // when the current countdown began
	reason  string
}

// New builds an unstarted controller. Construction is separate from Start because
// a run builds its world before it opens its listener, and a first-guest window
// that began at construction would spend itself on the world.
func New(p Policy) *Controller {
	return &Controller{policy: p}
}

// Policy returns the durations this controller enforces.
func (c *Controller) Policy() Policy { return c.policy }

// Start opens the first-guest window. Calling it twice keeps the first instant:
// the window belongs to the allocation, and a restarted countdown would hand a
// second window to an allocation nobody claimed.
func (c *Controller) Start(now time.Time) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.phase == PhaseUnstarted {
		c.phase, c.startAt, c.since = PhaseWaiting, now, now
	}
	return c.evalLocked(now)
}

// Observe folds one roster reading into the machine. guests counts guests, not
// participants: a dedicated host holds a roster entry and no cursor, and a session
// consisting only of its coordinator is an empty one.
func (c *Controller) Observe(guests int, now time.Time) State {
	if guests < 0 {
		guests = 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	previous := c.guests
	c.guests = guests

	switch c.phase {
	case PhaseUnstarted, PhaseExpired:
		// Nothing to fold into. An unstarted controller has no window to spend and
		// an expired one has spent it; both are answered by evalLocked unchanged.

	case PhaseDraining:
		// The drain ends when the roster does. Reaching zero here is the whole
		// point of draining, so it completes rather than waiting out the deadline.
		if guests == 0 {
			c.expireLocked(now, "drained")
		}

	default:
		switch {
		case guests > 0:
			// Occupied has no countdown, so `since` is only informational here —
			// but a vacant session that refilled must not keep its old one.
			if c.phase != PhaseOccupied {
				c.phase, c.since = PhaseOccupied, now
			}
		case c.phase == PhaseOccupied:
			c.phase, c.since = PhaseVacant, now
		case c.phase == PhaseWaiting && previous > 0:
			// Unreachable through the branch above, and cheap to be sure of: a
			// roster that was non-empty has been occupied, whatever the phase says.
			c.phase, c.since = PhaseVacant, now
		}
	}
	return c.evalLocked(now)
}

// Drain asks the session to end. It stops admitting immediately and then waits for
// the roster, which is what makes a rollout end after a match rather than during
// one. A drain with no configured deadline, or one that finds an empty roster,
// expires at once.
//
// Draining an expired controller changes nothing: the run is already leaving, and
// the reason it is leaving is the one worth reporting.
func (c *Controller) Drain(now time.Time, reason string) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.phase == PhaseExpired {
		return c.evalLocked(now)
	}
	if reason == "" {
		reason = "drain requested"
	}
	if c.guests == 0 || c.policy.Drain <= 0 {
		c.expireLocked(now, reason)
		return c.evalLocked(now)
	}
	c.phase, c.since, c.reason = PhaseDraining, now, reason
	return c.evalLocked(now)
}

// Interrupt is a termination request arriving from outside the process.
//
// The first one drains: it stops admitting and waits for the guests the session
// still holds, which is what makes a rollout or a node maintenance end after a
// match rather than during one. A second one ends the run, because an operator who
// asks twice is asking for the second answer, and because a supervisor's own
// escalation must not find a process that decided to keep waiting.
func (c *Controller) Interrupt(now time.Time, reason string) State {
	c.mu.Lock()
	draining := c.phase == PhaseDraining
	c.mu.Unlock()
	if draining {
		return c.Expire(now, reason)
	}
	return c.Drain(now, reason)
}

// Expire ends the session now, whatever it is holding. It is the path a fault
// takes: the caller has already decided, and this records why.
func (c *Controller) Expire(now time.Time, reason string) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.phase != PhaseExpired {
		c.expireLocked(now, reason)
	}
	return c.evalLocked(now)
}

// State reads the machine without supplying an observation. It settles a deadline
// that has passed since the last read, so a probe and the run's own loop cannot
// disagree about whether the session is still alive.
func (c *Controller) State(now time.Time) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evalLocked(now)
}

// Admit reports whether a dial may still be given a roster slot.
func (c *Controller) Admit(now time.Time) bool {
	return c.State(now).Admit
}

// expireLocked latches the terminal phase and the reason it was reached. Caller
// holds mu.
func (c *Controller) expireLocked(now time.Time, reason string) {
	c.phase, c.since, c.reason = PhaseExpired, now, reason
}

// evalLocked applies the policy to the stored phase and returns one read of it.
// Expiry is latched rather than recomputed so the reason a session ended survives
// the observation that follows it. Caller holds mu.
func (c *Controller) evalLocked(now time.Time) State {
	switch c.phase {
	case PhaseWaiting:
		if c.policy.FirstJoin > 0 && !now.Before(c.startAt.Add(c.policy.FirstJoin)) {
			c.expireLocked(now, fmt.Sprintf("no guest connected within %s", c.policy.FirstJoin))
		}
	case PhaseVacant:
		if c.policy.Empty > 0 && !now.Before(c.since.Add(c.policy.Empty)) {
			c.expireLocked(now, fmt.Sprintf("roster empty for %s", c.policy.Empty))
		}
	case PhaseDraining:
		if c.policy.Drain > 0 && !now.Before(c.since.Add(c.policy.Drain)) {
			c.expireLocked(now, fmt.Sprintf("drain deadline %s reached holding %d guest(s)",
				c.policy.Drain, c.guests))
		}
	}

	out := State{
		Phase:   c.phase,
		Reason:  c.reason,
		Guests:  c.guests,
		Admit:   c.phase != PhaseDraining && c.phase != PhaseExpired,
		Expired: c.phase == PhaseExpired,
	}
	if out.Reason == "" {
		out.Reason = c.phase.String()
	}
	if d, ok := c.deadlineLocked(); ok {
		out.Deadline = d
		if remaining := d.Sub(now); remaining > 0 {
			out.Remaining = remaining
		}
	}
	// After the expiry switch above, so a session that has just ended on its
	// vacancy grace reports the grace rather than the phase it left.
	if out.Phase == PhaseVacant {
		if vacant := now.Sub(c.since); vacant > 0 {
			out.Vacant = vacant
		}
	}
	return out
}

// deadlineLocked is when the current countdown ends, if one is running. Caller
// holds mu.
func (c *Controller) deadlineLocked() (time.Time, bool) {
	switch {
	case c.phase == PhaseWaiting && c.policy.FirstJoin > 0:
		return c.startAt.Add(c.policy.FirstJoin), true
	case c.phase == PhaseVacant && c.policy.Empty > 0:
		return c.since.Add(c.policy.Empty), true
	case c.phase == PhaseDraining && c.policy.Drain > 0:
		return c.since.Add(c.policy.Drain), true
	}
	return time.Time{}, false
}

package lifecycle

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// base is an arbitrary fixed instant. Every test drives the clock explicitly, so
// no case depends on wall time passing.
var base = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

// fleet is the deployed policy: ninety seconds for a guest to arrive, ninety after
// the last one leaves, and a drain that waits out a pod's grace period.
var fleet = Policy{FirstJoin: 90 * time.Second, Empty: 90 * time.Second, Drain: 25 * time.Second}

func TestUnstartedConcludesNothing(t *testing.T) {
	c := New(fleet)
	st := c.State(base.Add(time.Hour))
	if st.Phase != PhaseUnstarted {
		t.Fatalf("phase = %s, want unstarted", st.Phase)
	}
	if st.Expired {
		t.Fatal("an unstarted controller expired; its window has not opened")
	}
	if !st.Admit {
		t.Fatal("an unstarted controller refused a dial; it governs nothing yet")
	}
	if !st.Deadline.IsZero() {
		t.Fatalf("deadline = %v, want zero before Start", st.Deadline)
	}
}

func TestFirstJoinWindowExpiresWhenNobodyArrives(t *testing.T) {
	c := New(fleet)
	st := c.Start(base)
	if st.Phase != PhaseWaiting || !st.Admit {
		t.Fatalf("after Start: phase %s admit %v, want waiting/true", st.Phase, st.Admit)
	}
	if want := base.Add(90 * time.Second); !st.Deadline.Equal(want) {
		t.Fatalf("deadline = %v, want %v", st.Deadline, want)
	}

	if st = c.Observe(0, base.Add(89*time.Second)); st.Expired {
		t.Fatal("expired one second inside the window")
	}
	if st.Remaining != time.Second {
		t.Fatalf("remaining = %s, want 1s", st.Remaining)
	}

	st = c.Observe(0, base.Add(90*time.Second))
	if !st.Expired || st.Phase != PhaseExpired {
		t.Fatalf("phase %s expired %v at the deadline, want expired/true", st.Phase, st.Expired)
	}
	if !strings.Contains(st.Reason, "no guest connected") {
		t.Fatalf("reason = %q, want the unclaimed-allocation reason", st.Reason)
	}
	if st.Admit {
		t.Fatal("an expired session admitted a dial")
	}
}

func TestFirstGuestClosesTheWindowPermanently(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	st := c.Observe(1, base.Add(89*time.Second))
	if st.Phase != PhaseOccupied {
		t.Fatalf("phase = %s, want occupied", st.Phase)
	}
	if !st.Deadline.IsZero() || st.Remaining != 0 {
		t.Fatalf("occupied carries deadline %v/%s; a game is not timed out", st.Deadline, st.Remaining)
	}
	// Far past the first-join window: an occupied session is not on a clock.
	if st = c.Observe(1, base.Add(4*time.Hour)); st.Expired {
		t.Fatalf("an occupied session expired: %q", st.Reason)
	}
}

func TestVacancyGraceRunsFromTheDeparture(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Observe(2, base.Add(10*time.Second))

	left := base.Add(600 * time.Second)
	st := c.Observe(0, left)
	if st.Phase != PhaseVacant {
		t.Fatalf("phase = %s, want vacant", st.Phase)
	}
	if want := left.Add(90 * time.Second); !st.Deadline.Equal(want) {
		t.Fatalf("deadline = %v, want %v (grace runs from the departure)", st.Deadline, want)
	}
	if st = c.Observe(0, left.Add(89*time.Second)); st.Expired {
		t.Fatal("expired inside the vacancy grace")
	}
	st = c.Observe(0, left.Add(90*time.Second))
	if !st.Expired || !strings.Contains(st.Reason, "roster empty") {
		t.Fatalf("phase %s reason %q, want the empty-roster expiry", st.Phase, st.Reason)
	}
}

func TestReconnectInsideTheGraceKeepsTheSession(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Observe(1, base.Add(time.Second))

	left := base.Add(100 * time.Second)
	c.Observe(0, left)
	back := left.Add(80 * time.Second)
	if st := c.Observe(1, back); st.Phase != PhaseOccupied {
		t.Fatalf("phase = %s after a reconnect inside the grace, want occupied", st.Phase)
	}
	// The old countdown must not survive the reconnect: 20s after `back` is past
	// the original vacancy deadline and well inside a fresh one.
	if st := c.State(left.Add(95 * time.Second)); st.Expired {
		t.Fatalf("a reconnected session expired on its old countdown: %q", st.Reason)
	}
	// Leaving again starts a whole new grace from the second departure.
	second := back.Add(30 * time.Second)
	c.Observe(0, second)
	if st := c.State(second.Add(89 * time.Second)); st.Expired {
		t.Fatal("the second grace did not restart from the second departure")
	}
	if st := c.State(second.Add(90 * time.Second)); !st.Expired {
		t.Fatal("the second grace did not expire")
	}
}

func TestDrainStopsAdmissionAndEndsOnAnEmptyRoster(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Observe(2, base.Add(time.Second))

	at := base.Add(10 * time.Second)
	st := c.Drain(at, "SIGTERM")
	if st.Phase != PhaseDraining {
		t.Fatalf("phase = %s, want draining", st.Phase)
	}
	if st.Admit {
		t.Fatal("a draining session admitted a dial")
	}
	if st.Reason != "SIGTERM" {
		t.Fatalf("reason = %q, want the caller's", st.Reason)
	}
	if st = c.Observe(1, at.Add(5*time.Second)); st.Expired {
		t.Fatal("a drain ended while it still held a guest")
	}
	st = c.Observe(0, at.Add(9*time.Second))
	if !st.Expired || st.Reason != "drained" {
		t.Fatalf("phase %s reason %q, want a completed drain", st.Phase, st.Reason)
	}
}

func TestDrainDeadlineEndsASessionNobodyLeaves(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Observe(3, base.Add(time.Second))

	at := base.Add(10 * time.Second)
	c.Drain(at, "node drain")
	if st := c.Observe(3, at.Add(24*time.Second)); st.Expired {
		t.Fatal("expired one second inside the drain deadline")
	}
	st := c.Observe(3, at.Add(25*time.Second))
	if !st.Expired || !strings.Contains(st.Reason, "drain deadline") {
		t.Fatalf("phase %s reason %q, want the drain deadline", st.Phase, st.Reason)
	}
	if !strings.Contains(st.Reason, "3 guest(s)") {
		t.Fatalf("reason = %q, want the roster it gave up on", st.Reason)
	}
}

func TestDrainWithNoDeadlineExitsAtOnce(t *testing.T) {
	c := New(Policy{FirstJoin: time.Minute, Empty: time.Minute})
	c.Start(base)
	c.Observe(2, base)
	st := c.Drain(base.Add(time.Second), "SIGTERM")
	if !st.Expired {
		t.Fatalf("phase = %s; a drain with no deadline waits for nothing", st.Phase)
	}
}

func TestDrainOfAnEmptySessionExitsAtOnce(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	st := c.Drain(base.Add(time.Second), "SIGTERM")
	if !st.Expired {
		t.Fatalf("phase = %s; there was nobody to wait for", st.Phase)
	}
}

func TestExpiryIsTerminalAndKeepsItsReason(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Observe(0, base.Add(90*time.Second)) // expires: nobody came

	after := base.Add(91 * time.Second)
	for _, st := range []State{
		c.Observe(4, after),
		c.Drain(after, "SIGTERM"),
		c.Start(after),
		c.State(after),
	} {
		if st.Phase != PhaseExpired {
			t.Fatalf("phase = %s after expiry, want expired", st.Phase)
		}
		if !strings.Contains(st.Reason, "no guest connected") {
			t.Fatalf("reason = %q, want the original expiry reason", st.Reason)
		}
	}
}

func TestInterruptDrainsOnceThenEnds(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Observe(2, base.Add(time.Second))

	first := base.Add(10 * time.Second)
	st := c.Interrupt(first, "SIGTERM")
	if st.Phase != PhaseDraining || st.Expired {
		t.Fatalf("phase %s expired %v on the first interrupt, want a drain", st.Phase, st.Expired)
	}
	st = c.Interrupt(first.Add(2*time.Second), "SIGTERM again")
	if !st.Expired || st.Reason != "SIGTERM again" {
		t.Fatalf("phase %s reason %q on the second interrupt, want an immediate exit", st.Phase, st.Reason)
	}
}

func TestExpireRecordsTheCallersReason(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Observe(1, base.Add(time.Second))
	st := c.Expire(base.Add(2*time.Second), "probe reported a stalled clock")
	if !st.Expired || st.Reason != "probe reported a stalled clock" {
		t.Fatalf("phase %s reason %q, want the caller's fault reason", st.Phase, st.Reason)
	}
}

func TestUnboundedPolicyNeverExpiresOnItsOwn(t *testing.T) {
	c := New(Policy{})
	if c.Policy().Bounded() {
		t.Fatal("the zero policy reported itself bounded")
	}
	c.Start(base)
	if st := c.State(base.Add(72 * time.Hour)); st.Expired {
		t.Fatalf("an unbounded waiting session expired: %q", st.Reason)
	}
	if st := c.Observe(1, base.Add(time.Hour)); st.Vacant != 0 {
		t.Fatalf("an occupied session reported %s of vacancy", st.Vacant)
	}
	c.Observe(0, base.Add(2*time.Hour))
	st := c.State(base.Add(96 * time.Hour))
	if st.Expired {
		t.Fatalf("an unbounded vacant session expired: %q", st.Reason)
	}
	if st.Phase != PhaseVacant || !st.Admit {
		t.Fatalf("phase %s admit %v, want a vacant host still open to a dial", st.Phase, st.Admit)
	}
	// Nothing is counting down, so Remaining says nothing; how long the roster has
	// been empty is what a long-lived host parks and restarts on instead.
	if st.Remaining != 0 {
		t.Fatalf("an unbounded session reported %s remaining", st.Remaining)
	}
	if want := 94 * time.Hour; st.Vacant != want {
		t.Fatalf("vacant for %s, want %s since the last guest left", st.Vacant, want)
	}
}

func TestStartIsNotRestartedBySecondCall(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Start(base.Add(60 * time.Second)) // a second call must not hand out a second window
	if st := c.State(base.Add(90 * time.Second)); !st.Expired {
		t.Fatalf("phase = %s; the window restarted", st.Phase)
	}
}

func TestNegativeGuestCountIsReadAsEmpty(t *testing.T) {
	c := New(fleet)
	c.Start(base)
	c.Observe(1, base.Add(time.Second))
	st := c.Observe(-3, base.Add(2*time.Second))
	if st.Phase != PhaseVacant || st.Guests != 0 {
		t.Fatalf("phase %s guests %d, want vacant/0", st.Phase, st.Guests)
	}
}

func TestPolicyValidateRejectsNegativeBounds(t *testing.T) {
	if err := (Policy{FirstJoin: 90 * time.Second}).Validate(); err != nil {
		t.Fatalf("a positive bound was rejected: %v", err)
	}
	for name, p := range map[string]Policy{
		"first-join": {FirstJoin: -time.Second},
		"empty":      {Empty: -time.Second},
		"drain":      {Drain: -time.Second},
	} {
		err := p.Validate()
		if err == nil {
			t.Fatalf("%s: a negative bound was accepted", name)
		}
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: error %q does not name the field", name, err)
		}
	}
}

// TestConcurrentReadersAndObserverAgree runs the probe's read path against the
// loop's write path. It asserts the invariant the two share: once any reader sees
// the session expired, no reader ever sees it live again.
func TestConcurrentReadersAndObserverAgree(t *testing.T) {
	c := New(Policy{FirstJoin: time.Millisecond, Empty: time.Millisecond})
	c.Start(base)

	var wg sync.WaitGroup
	var mu sync.Mutex
	expired := false

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				st := c.State(base.Add(time.Duration(i) * time.Microsecond))
				mu.Lock()
				if expired && !st.Expired {
					mu.Unlock()
					t.Error("a session came back to life after expiring")
					return
				}
				expired = expired || st.Expired
				mu.Unlock()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 500 {
			c.Observe(i%2, base.Add(time.Duration(i)*time.Microsecond))
		}
	}()
	wg.Wait()
}

package engine

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// TimeScale is an exact rational rate: game time advances Num/Den per real unit.
// Rational rather than float so a rate reproduces exactly across platforms.
type TimeScale struct {
	Num int64
	Den int64
}

// ScaleNormal is real-time pacing
var ScaleNormal = TimeScale{Num: 1, Den: 1}

// ScaleLadder is the operator-facing rate set; ScaleNormalIndex is real time
var ScaleLadder = []TimeScale{
	{1, 8}, {1, 4}, {1, 2}, {1, 1}, {2, 1}, {4, 1}, {8, 1},
}

// ScaleNormalIndex locates ScaleNormal within ScaleLadder
const ScaleNormalIndex = 3

// Valid reports whether s is a usable positive rate
func (s TimeScale) Valid() bool { return s.Num > 0 && s.Den > 0 }

// IsNormal reports whether s is exactly real time
func (s TimeScale) IsNormal() bool { return s.Num == s.Den }

// Faster reports whether s runs ahead of real time
func (s TimeScale) Faster() bool { return s.Num > s.Den }

// Percent renders the rate as hundredths of real time; 100 = 1x
func (s TimeScale) Percent() int64 {
	if s.Den == 0 {
		return 0
	}
	return s.Num * 100 / s.Den
}

// String renders the rate as a ladder token: "1/4", "1", "8"
func (s TimeScale) String() string {
	if s.Den == 1 {
		return strconv.FormatInt(s.Num, 10)
	}
	return strconv.FormatInt(s.Num, 10) + "/" + strconv.FormatInt(s.Den, 10)
}

// ParseScale resolves a ladder token; ok is false for anything off the ladder
func ParseScale(tok string) (TimeScale, bool) {
	for _, t := range ScaleLadder {
		if t.String() == tok {
			return t, true
		}
	}
	return TimeScale{}, false
}

// ScaleStep returns the ladder entry n positions from s, clamped at the ends.
// An off-ladder rate resolves to real time first.
func ScaleStep(s TimeScale, n int) TimeScale {
	i := ScaleNormalIndex
	for k, t := range ScaleLadder {
		if t == s {
			i = k
			break
		}
	}
	return ScaleLadder[max(0, min(i+n, len(ScaleLadder)-1))]
}

// clockAnchor is the immutable rate segment in force since realAt.
// Swapping a whole anchor keeps Now lock-free and continuous across changes.
type clockAnchor struct {
	realAt time.Time
	gameAt time.Time
	scale  TimeScale
	paused bool
}

// project returns the game instant this anchor maps wall time now onto
func (a *clockAnchor) project(now time.Time) time.Time {
	if a.paused {
		return a.gameAt
	}
	d := now.Sub(a.realAt)
	if !a.scale.IsNormal() {
		d = time.Duration(int64(d) * a.scale.Num / a.scale.Den)
	}
	return a.gameAt.Add(d)
}

// PausableClock is the game time source: pausable, rate-scalable, and
// steppable while frozen. Reads are lock-free; writes swap the anchor.
type PausableClock struct {
	anchor atomic.Pointer[clockAnchor]

	mu          sync.Mutex // serializes anchor swaps
	pauseStart  time.Time  // wall instant of the current pause, zero when running
	totalPaused time.Duration
}

// NewPausableClock creates a clock running at real time from now
func NewPausableClock() *PausableClock {
	now := time.Now()
	pc := &PausableClock{}
	pc.anchor.Store(&clockAnchor{realAt: now, gameAt: now, scale: ScaleNormal})
	return pc
}

// Now returns current game time: frozen while paused, dilated by the scale
func (pc *PausableClock) Now() time.Time {
	return pc.anchor.Load().project(time.Now())
}

// RealTime returns wall clock time, never paused or scaled
func (pc *PausableClock) RealTime() time.Time { return time.Now() }

// reanchor folds elapsed game time into a fresh segment, then applies mutate.
// Caller MUST hold mu.
func (pc *PausableClock) reanchor(mutate func(*clockAnchor)) {
	now := time.Now()
	cur := pc.anchor.Load()
	next := &clockAnchor{
		realAt: now,
		gameAt: cur.project(now),
		scale:  cur.scale,
		paused: cur.paused,
	}
	mutate(next)
	pc.anchor.Store(next)
}

// SetScale changes the rate; the current game instant is preserved, so no
// jump and no catch-up burst follows a change
func (pc *PausableClock) SetScale(s TimeScale) {
	if !s.Valid() {
		return
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.reanchor(func(a *clockAnchor) { a.scale = s })
}

// Scale returns the active rate
func (pc *PausableClock) Scale() TimeScale { return pc.anchor.Load().scale }

// ToReal converts a game duration to the wall time it occupies at the current
// rate. Returns 0 while paused: no wall interval maps to frozen game time.
func (pc *PausableClock) ToReal(d time.Duration) time.Duration {
	a := pc.anchor.Load()
	if a.paused {
		return 0
	}
	if a.scale.IsNormal() {
		return d
	}
	return time.Duration(int64(d) * a.scale.Den / a.scale.Num)
}

// Step advances frozen game time by d; a no-op while running.
// Reserved for the paused step path.
func (pc *PausableClock) Step(d time.Duration) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if !pc.anchor.Load().paused {
		return
	}
	pc.reanchor(func(a *clockAnchor) { a.gameAt = a.gameAt.Add(d) })
}

// Pause freezes game time advancement
func (pc *PausableClock) Pause() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pc.anchor.Load().paused {
		return
	}
	pc.pauseStart = time.Now()
	pc.reanchor(func(a *clockAnchor) { a.paused = true })
}

// Resume restarts game time from where it froze
func (pc *PausableClock) Resume() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if !pc.anchor.Load().paused {
		return
	}
	if !pc.pauseStart.IsZero() {
		pc.totalPaused += time.Since(pc.pauseStart)
		pc.pauseStart = time.Time{}
	}
	pc.reanchor(func(a *clockAnchor) { a.paused = false })
}

// IsPaused returns current pause state
func (pc *PausableClock) IsPaused() bool { return pc.anchor.Load().paused }

// GetTotalPauseDuration returns cumulative real time spent paused
func (pc *PausableClock) GetTotalPauseDuration() time.Duration {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	total := pc.totalPaused
	if !pc.pauseStart.IsZero() {
		total += time.Since(pc.pauseStart)
	}
	return total
}

// GetCurrentPauseDuration returns the length of the current pause, 0 if running
func (pc *PausableClock) GetCurrentPauseDuration() time.Duration {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pc.pauseStart.IsZero() {
		return 0
	}
	return time.Since(pc.pauseStart)
}

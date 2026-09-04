package system

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// boundsBatch is one peer's epoch carrying n artifacts that all apply at applyTick.
// The payload is real: every frame these tests schedule must be one applyDue would
// try to decode, or a cap that only ever held malformed frames would prove nothing.
func boundsBatch(t *testing.T, source uint32, produced, applyTick uint64, n, pad int) []byte {
	t.Helper()
	frames := make([]event.ScheduledWireFrame, 0, n)
	for i := range n {
		frames = append(frames, event.ScheduledWireFrame{
			Frame: event.WireFrame{
				Event:   "EventGoldJumpRequest",
				Domain:  "shared",
				Payload: strings.Repeat("x", pad),
				Seq:     uint64(i + 1),
			},
			ApplyTick: applyTick,
		})
	}
	body, err := event.EncodeWireBatch(event.WireBatch{
		Frames: frames, Source: source, ProducedTick: produced,
	})
	if err != nil {
		t.Fatalf("encode batch: %v", err)
	}
	return body
}

func boundsSystem(t *testing.T) *NetworkSystem {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	s := NewNetworkSystem(w).(*NetworkSystem)
	s.Init()
	return s
}

func scheduledCount(s *NetworkSystem) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.scheduled)
}

// TestTheScheduleRefusesATickItWillNotReach covers the forward window.
//
// The schedule keeps what is not yet due, so an apply tick beyond anything this
// run reaches is not a schedule but a reservation nothing retires. The window has
// to admit a fresh join — the largest gap two participants legitimately hold — and
// refuse the tick that is only a number.
func TestTheScheduleRefusesATickItWillNotReach(t *testing.T) {
	t.Parallel()
	s := boundsSystem(t)

	inside := uint64(parameter.NetworkApplyWindowTicks)
	s.scheduleCrossings(2, boundsBatch(t, 2, 1, inside, 3, 0))
	if got := scheduledCount(s); got != 3 {
		t.Fatalf("artifacts inside the window scheduled = %d, want 3", got)
	}

	// A frame past the horizon inside a batch that is not: the apply tick is per
	// frame and is not derived from the epoch's.
	s.scheduleCrossings(2, boundsBatch(t, 2, 2, inside+1, 4, 0))
	if got := scheduledCount(s); got != 3 {
		t.Fatalf("a frame past the horizon was scheduled: held %d, want 3", got)
	}
	if s.statRefusedTick.Load() == 0 {
		t.Fatal("a refused apply tick was not counted")
	}
}

// TestAnEpochFromBeyondTheHorizonDoesNotPoisonItsSource is the reason the window
// runs before the epoch window rather than after it.
//
// admit() takes any tick above a source's high-water mark. A single frame naming a
// tick near the end of the range would carry that mark there, and every ordinary
// epoch that followed — on this instance and on everything it relays to — would
// then be refused as late.
func TestAnEpochFromBeyondTheHorizonDoesNotPoisonItsSource(t *testing.T) {
	t.Parallel()
	s := boundsSystem(t)

	s.scheduleCrossings(2, boundsBatch(t, 2, 1<<62, 1<<62, 1, 0))
	if got := scheduledCount(s); got != 0 {
		t.Fatalf("an epoch from beyond the horizon scheduled %d artifacts", got)
	}

	// The source is still usable, which is the whole point.
	s.scheduleCrossings(2, boundsBatch(t, 2, 1, 2, 2, 0))
	if got := scheduledCount(s); got != 2 {
		t.Fatalf("ordinary artifacts after a refused epoch scheduled = %d, want 2", got)
	}
}

// TestTheScheduleIsBoundedByCountAndByBytes covers both ceilings. Two bounds
// because a peer can spend the budget either way: many small artifacts, or few
// large ones.
func TestTheScheduleIsBoundedByCountAndByBytes(t *testing.T) {
	t.Parallel()

	t.Run("count", func(t *testing.T) {
		t.Parallel()
		s := boundsSystem(t)
		for epoch := range uint64(64) {
			s.scheduleCrossings(2, boundsBatch(t, 2, epoch+1, 10, 128, 0))
		}
		if got := scheduledCount(s); got > parameter.NetworkScheduledMax {
			t.Fatalf("held %d artifacts, ceiling is %d", got, parameter.NetworkScheduledMax)
		}
		if s.statScheduleFull.Load() == 0 {
			t.Fatal("the count ceiling turned nothing away")
		}
	})

	t.Run("bytes", func(t *testing.T) {
		t.Parallel()
		s := boundsSystem(t)
		for epoch := range uint64(64) {
			s.scheduleCrossings(2, boundsBatch(t, 2, epoch+1, 10, 8, 8<<10))
		}
		s.mu.Lock()
		held := s.scheduledBytes
		s.mu.Unlock()
		if held > parameter.NetworkScheduledBytes {
			t.Fatalf("held %d bytes, ceiling is %d", held, parameter.NetworkScheduledBytes)
		}
		if s.statScheduleFull.Load() == 0 {
			t.Fatal("the byte ceiling turned nothing away")
		}
	})
}

// TestDrainingTheScheduleReturnsItsBudget pins the accounting. A ceiling that only
// ever rose would stop a session that had done nothing wrong.
func TestDrainingTheScheduleReturnsItsBudget(t *testing.T) {
	t.Parallel()
	s := boundsSystem(t)

	s.scheduleCrossings(2, boundsBatch(t, 2, 1, 4, 6, 1<<10))
	s.mu.Lock()
	before := s.scheduledBytes
	s.mu.Unlock()
	if before == 0 {
		t.Fatal("scheduling recorded no bytes")
	}

	s.applyDue(4)
	s.mu.Lock()
	after, held := s.scheduledBytes, len(s.scheduled)
	s.mu.Unlock()
	if held != 0 || after != 0 {
		t.Fatalf("after draining every due artifact: %d held, %d bytes; want 0 and 0", held, after)
	}
}

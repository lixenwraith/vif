package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// The retention half of Phase 6's replay, tested where it lives.
//
// The app-level suite proves a guest's own actions survive a correction. These
// prove the three things retention itself promises, without a session: what it
// refuses to hold, what it offers, and what it does when a bound is reached.

// TestRosterAndResetArtifactsAreNeverRetained is deliverable 2's exclusion. The
// three artifacts that decide what the world *is* — an arrival, a departure and a
// reset — apply at one agreed tick on every instance including their producer, so
// Cross takes ownership of them and never puts them in the replay suffix. A replay
// that carried one would create a roster entry, or a run, the rest of the session
// numbers differently.
func TestRosterAndResetArtifactsAreNeverRetained(t *testing.T) {
	for _, et := range []event.EventType{
		event.EventParticipantJoined,
		event.EventParticipantDeparted,
		event.EventGameResetRequest,
	} {
		if !barrierBound(et) {
			t.Errorf("%s is not barrier-bound, so a replay could carry it",
				event.GetEventName(et))
		}
	}
	// And the ordinary crossing is not barrier-bound, or nothing would be retained
	// at all and the exclusion above would be vacuous.
	if barrierBound(event.EventCursorMoveRequest) {
		t.Error("an ordinary crossing is barrier-bound; nothing would ever be replayed")
	}
}

// TestTheReplayWindowIsMeasuredInProductionTicks pins the membership rule the
// "exactly once" argument rests on: an artifact produced after the correction's
// baseline is offered, one produced at or before it is not.
func TestTheReplayWindowIsMeasuredInProductionTicks(t *testing.T) {
	s := &NetworkSystem{}
	for _, produced := range []uint64{8, 9, 10, 11} {
		s.retainLocked(frameAt(produced), produced, produced+3, event.OriginInput)
	}

	frames, origins, ok := s.LocalReplaySuffix(9)
	if !ok {
		t.Fatal("a suffix inside every bound was reported unavailable")
	}
	if len(frames) != 2 || len(origins) != 2 {
		t.Fatalf("baseline 9 offered %d records, want the two produced after it", len(frames))
	}
	for _, f := range frames {
		if f.ApplyTick <= 9 {
			t.Fatalf("the suffix offered an artifact applying at tick %d, which the capture holds",
				f.ApplyTick)
		}
	}
	if frames[0].Frame.Seq >= frames[1].Frame.Seq {
		t.Fatalf("the suffix is not in production order: %d then %d",
			frames[0].Frame.Seq, frames[1].Frame.Seq)
	}

	// A baseline past everything retained offers nothing, which is the ordinary
	// case on a quiet participant rather than a failure.
	if frames, _, ok := s.LocalReplaySuffix(99); !ok || len(frames) != 0 {
		t.Fatalf("a baseline past the suffix offered %d records (ok=%v)", len(frames), ok)
	}
}

// TestADroppedRecordMakesTheSuffixUnavailable is the never-guess rule: retention
// that lost a record the caller would have needed offers nothing at all rather
// than a shorter history.
func TestADroppedRecordMakesTheSuffixUnavailable(t *testing.T) {
	s := &NetworkSystem{}
	for i := range parameter.SnapshotReplayRecords + 4 {
		produced := uint64(100 + i)
		s.retainLocked(frameAt(produced), produced, produced+3, event.OriginInput)
	}
	retained, dropped := s.ReplaySuffixSize()
	if dropped == 0 {
		t.Fatalf("retention held %d records without reaching a bound", retained)
	}
	if retained > parameter.SnapshotReplayRecords {
		t.Fatalf("retention holds %d records, past the %d-record bound",
			retained, parameter.SnapshotReplayRecords)
	}
	if _, _, ok := s.LocalReplaySuffix(100); ok {
		t.Fatal("a baseline behind a dropped record was offered a suffix anyway")
	}
	// A baseline past everything that was dropped is still answerable: the hole is
	// behind it, so the history after it is whole.
	if _, _, ok := s.LocalReplaySuffix(uint64(100 + parameter.SnapshotReplayRecords + 3)); !ok {
		t.Fatal("a baseline past the hole was refused a suffix it holds in full")
	}
}

// TestTheTickSpanBoundsRetention pins the second bound: a record older than the
// window is dropped even when the count and the byte bounds are nowhere near.
func TestTheTickSpanBoundsRetention(t *testing.T) {
	s := &NetworkSystem{}
	s.retainLocked(frameAt(1), 1, 4, event.OriginInput)
	s.retainLocked(frameAt(2), parameter.SnapshotReplayTicks+10, parameter.SnapshotReplayTicks+13,
		event.OriginInput)
	retained, dropped := s.ReplaySuffixSize()
	if retained != 1 || dropped != 1 {
		t.Fatalf("a record %d ticks old left %d retained and %d dropped",
			parameter.SnapshotReplayTicks+9, retained, dropped)
	}
}

// frameAt is one retained crossing, distinguishable by sequence.
func frameAt(seq uint64) event.WireFrame {
	return event.WireFrame{
		Event:   "EventCursorMoveRequest",
		Domain:  "player",
		Payload: "x = 1\n",
		Seq:     seq,
	}
}

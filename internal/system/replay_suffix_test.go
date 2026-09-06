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

// TestReplayMembershipUsesTheSourceSequence pins the boundary a correction is
// judged by. A capture describes a world, and what that world holds of this
// participant's stream is the sequence its fence names — not the ticks its copies
// were scheduled to apply at.
func TestReplayMembershipUsesTheSourceSequence(t *testing.T) {
	s := &NetworkSystem{}
	for _, seq := range []uint64{1, 2, 3, 4} {
		s.retainLocked(frameAt(seq), 100+seq, 103+seq, event.OriginInput)
	}

	frames, origins, ok := s.LocalReplaySuffix(0)
	if !ok {
		t.Fatal("a suffix inside every bound was reported unavailable")
	}
	if len(frames) != 4 || len(origins) != 4 {
		t.Fatalf("fence 0 offered %d records, want everything retained", len(frames))
	}

	frames, _, ok = s.LocalReplaySuffix(2)
	if !ok || len(frames) != 2 || frames[0].Frame.Seq != 3 || frames[1].Frame.Seq != 4 {
		t.Fatalf("fence 2 offered sequences %v (ok=%v), want 3 and 4",
			frameSequences(frames), ok)
	}

	// A fence past everything retained offers nothing, which is the ordinary case
	// on a participant the authority is fully caught up with.
	if frames, _, ok := s.LocalReplaySuffix(99); !ok || len(frames) != 0 {
		t.Fatalf("a fence past the suffix offered %d records (ok=%v)", len(frames), ok)
	}
}

// TestALateCrossingSurvivesACorrectionThatMissedIt is the regression this fence
// exists for, at the level where the decision is made.
//
// A participant produces a crossing for apply tick 103 and its link misses the
// playout lead. The authority reads its world at tick 110 without having received
// it, so the capture cannot contain it — but its apply tick is seven ticks in the
// past. Judged by tick, the producer concludes the correction already holds its
// action and discards it; the action disappears until the authority applies the late
// frame and publishes another capture, which is the visible undo-and-redo.
//
// Judged by the fence, the producer knows the authority had completed only sequence
// 1 and replays the rest. Nothing disappears.
func TestALateCrossingSurvivesACorrectionThatMissedIt(t *testing.T) {
	s := &NetworkSystem{}
	s.retainLocked(frameAt(1), 99, 102, event.OriginInput)  // arrived; the capture has it
	s.retainLocked(frameAt(2), 100, 103, event.OriginInput) // in flight when the capture was read
	s.retainLocked(frameAt(3), 101, 104, event.OriginInput) // likewise

	const captureTick = 110
	for _, rec := range s.suffix {
		if rec.frame.ApplyTick > captureTick {
			t.Fatalf("the fixture is not the late case: apply tick %d is still ahead of the capture",
				rec.frame.ApplyTick)
		}
	}

	frames, _, ok := s.LocalReplaySuffix(1)
	if !ok {
		t.Fatal("the suffix was reported unavailable")
	}
	if got := frameSequences(frames); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("replayed sequences %v, want 2 and 3 — the ones the authority had not applied", got)
	}
}

// TestReplayOrderingKeepsEachFramesOrigin verifies the parallel metadata follows
// the frame when a correction rebase makes retained apply ticks non-monotonic.
func TestReplayOrderingKeepsEachFramesOrigin(t *testing.T) {
	s := &NetworkSystem{}
	s.retainLocked(frameAt(1), 10, 20, event.OriginInput)
	s.retainLocked(frameAt(2), 11, 15, event.OriginMacro)

	frames, origins, ok := s.LocalReplaySuffix(0)
	if !ok || len(frames) != 2 || len(origins) != 2 {
		t.Fatalf("offered %d frames and %d origins (ok=%v), want two of each",
			len(frames), len(origins), ok)
	}
	if frames[0].Frame.Seq != 2 || origins[0] != event.OriginMacro ||
		frames[1].Frame.Seq != 1 || origins[1] != event.OriginInput {
		t.Fatalf("ordered frames/origins = (%d,%s), (%d,%s)",
			frames[0].Frame.Seq, origins[0], frames[1].Frame.Seq, origins[1])
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
	if _, _, ok := s.LocalReplaySuffix(0); ok {
		t.Fatal("a fence behind a dropped record was offered a suffix anyway")
	}
	// A fence at the newest dropped sequence is still answerable: the hole is inside
	// what the capture already holds, so the history after it is whole. The tick-span
	// bound may drop more than the record-count overflow above, so read the exact
	// boundary rather than assuming which bound fired.
	if _, _, ok := s.LocalReplaySuffix(s.lostSeq); !ok {
		t.Fatal("a fence past the hole was refused a suffix it holds in full")
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

func frameSequences(frames []event.ScheduledWireFrame) []uint64 {
	out := make([]uint64, len(frames))
	for i, frame := range frames {
		out[i] = frame.Frame.Seq
	}
	return out
}

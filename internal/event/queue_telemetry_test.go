package event

import "testing"

func TestEventQueueTelemetryTracksDispatchAndDeadLettersByType(t *testing.T) {
	q := NewEventQueue()
	q.RecordDispatch(EventDeathOne, false)
	q.RecordDispatch(EventDeathOne, true)
	q.RecordDispatch(EventDeathBatch, false)
	q.RecordDispatch(EventNone, true)

	var dispatched, dead [EventTypeCount]int64
	q.SnapshotTelemetry(&dispatched, &dead)
	if dispatched[EventDeathOne] != 2 || dead[EventDeathOne] != 1 {
		t.Fatalf("death-one telemetry = (%d dispatched, %d dead), want (2, 1)",
			dispatched[EventDeathOne], dead[EventDeathOne])
	}
	if dispatched[EventDeathBatch] != 1 || dead[EventDeathBatch] != 0 {
		t.Fatalf("death-batch telemetry = (%d dispatched, %d dead), want (1, 0)",
			dispatched[EventDeathBatch], dead[EventDeathBatch])
	}

	q.ResetTelemetry()
	q.SnapshotTelemetry(&dispatched, &dead)
	if dispatched[EventDeathOne] != 0 || dead[EventDeathOne] != 0 || dispatched[EventDeathBatch] != 0 {
		t.Fatal("queue telemetry survived reset")
	}
}

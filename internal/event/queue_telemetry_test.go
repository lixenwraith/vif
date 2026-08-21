package event

import "testing"

func TestEventQueueTelemetryTracksDispatchAndDeadLettersByType(t *testing.T) {
	q := NewEventQueue()
	q.RecordDispatch(EventDeathBatch, false)
	q.RecordDispatch(EventDeathBatch, true)
	q.RecordDispatch(EventNone, true)

	var dispatched, dead [EventTypeCount]int64
	q.SnapshotTelemetry(&dispatched, &dead)
	if dispatched[EventDeathBatch] != 2 || dead[EventDeathBatch] != 1 {
		t.Fatalf("death telemetry = (%d dispatched, %d dead), want (2, 1)",
			dispatched[EventDeathBatch], dead[EventDeathBatch])
	}

	q.ResetTelemetry()
	q.SnapshotTelemetry(&dispatched, &dead)
	if dispatched[EventDeathBatch] != 0 || dead[EventDeathBatch] != 0 {
		t.Fatal("queue telemetry survived reset")
	}
}

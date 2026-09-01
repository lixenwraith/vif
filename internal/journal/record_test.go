package journal

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/event"
)

func TestRecorderAttachesAccountsAndDetaches(t *testing.T) {
	queue := event.NewEventQueue()
	capture := NewCapture()
	recorder, err := Start(queue, event.JournalAnchor{Seed: 7}, capture)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if queue.Journal() == nil || len(capture.Anchors()) != 1 {
		t.Fatal("Start() did not attach the journal and emit its anchor")
	}

	queue.Push(event.GameEvent{Type: event.EventGameResetRequest, Origin: event.OriginDebug})
	if stats := recorder.Stats(); stats.Emitted != 1 || stats.EncodeFailed != 0 || stats.Path != "" {
		t.Fatalf("live stats = %+v", stats)
	}

	stats, err := recorder.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if stats.Emitted != 1 || queue.Journal() != nil {
		t.Fatalf("closed stats = %+v, attached = %t", stats, queue.Journal() != nil)
	}
	queue.Push(event.GameEvent{Type: event.EventGameResetRequest, Origin: event.OriginDebug})
	if got := len(capture.Records()); got != 1 {
		t.Fatalf("records after close = %d, want 1", got)
	}
	if again, err := recorder.Close(); err != nil || again != stats {
		t.Fatalf("second Close() = (%+v, %v), want (%+v, nil)", again, err, stats)
	}
}

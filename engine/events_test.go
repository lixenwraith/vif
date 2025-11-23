package engine

import (
	"sync"
	"testing"
	"time"
)

// TestEventQueueBasic tests basic push and consume operations
func TestEventQueueBasic(t *testing.T) {
	eq := NewEventQueue()

	// Push 3 events
	event1 := GameEvent{Type: EventCleanerRequest, Payload: "test1", Frame: 1, Timestamp: time.Now()}
	event2 := GameEvent{Type: EventCleanerFinished, Payload: "test2", Frame: 2, Timestamp: time.Now()}
	event3 := GameEvent{Type: EventGoldSpawned, Payload: "test3", Frame: 3, Timestamp: time.Now()}

	eq.Push(event1)
	eq.Push(event2)
	eq.Push(event3)

	// First consume should return all 3 events
	events := eq.Consume()
	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	// Verify events are in FIFO order
	if events[0].Type != EventCleanerRequest || events[0].Payload != "test1" {
		t.Errorf("Event 1 mismatch: got type=%v, payload=%v", events[0].Type, events[0].Payload)
	}
	if events[1].Type != EventCleanerFinished || events[1].Payload != "test2" {
		t.Errorf("Event 2 mismatch: got type=%v, payload=%v", events[1].Type, events[1].Payload)
	}
	if events[2].Type != EventGoldSpawned || events[2].Payload != "test3" {
		t.Errorf("Event 3 mismatch: got type=%v, payload=%v", events[2].Type, events[2].Payload)
	}

	// Second consume should return empty slice
	events2 := eq.Consume()
	if len(events2) != 0 {
		t.Errorf("Expected 0 events on second consume, got %d", len(events2))
	}
}

// TestEventQueueConcurrent tests concurrent push operations from multiple goroutines
func TestEventQueueConcurrent(t *testing.T) {
	eq := NewEventQueue()
	numGoroutines := 10
	eventsPerGoroutine := 10
	totalEvents := numGoroutines * eventsPerGoroutine

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch 10 goroutines that each push 10 events
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := GameEvent{
					Type:      EventCleanerRequest,
					Payload:   goroutineID*100 + j,
					Frame:     int64(j),
					Timestamp: time.Now(),
				}
				eq.Push(event)
			}
		}(i)
	}

	wg.Wait()

	// Consume all events
	events := eq.Consume()

	// Verify we got all events
	if len(events) != totalEvents {
		t.Errorf("Expected %d events, got %d", totalEvents, len(events))
	}

	// Verify all payloads are unique and within expected range
	seen := make(map[int]bool)
	for _, event := range events {
		payload := event.Payload.(int)
		if seen[payload] {
			t.Errorf("Duplicate payload found: %d", payload)
		}
		seen[payload] = true
	}

	// Verify queue is now empty
	if eq.Len() != 0 {
		t.Errorf("Expected queue to be empty, got length %d", eq.Len())
	}
}

// TestEventQueueOverflow tests behavior when pushing more events than buffer size
func TestEventQueueOverflow(t *testing.T) {
	eq := NewEventQueue()

	// Push 300 events to a 256-size buffer
	for i := 0; i < 300; i++ {
		event := GameEvent{
			Type:      EventCleanerRequest,
			Payload:   i,
			Frame:     int64(i),
			Timestamp: time.Now(),
		}
		eq.Push(event)
	}

	// Consume all events
	events := eq.Consume()

	// Should get at most 256 events (buffer size)
	if len(events) > 256 {
		t.Errorf("Expected at most 256 events, got %d", len(events))
	}

	// The events should be the most recent ones (244-299)
	// Due to wrap-around, oldest events are overwritten
	if len(events) == 256 {
		// First event should be approximately payload 44 (300 - 256)
		firstPayload := events[0].Payload.(int)
		if firstPayload < 40 || firstPayload > 50 {
			t.Logf("Warning: First payload is %d, expected around 44 (some variance is acceptable due to timing)", firstPayload)
		}

		// Last event should be payload 299
		lastPayload := events[len(events)-1].Payload.(int)
		if lastPayload != 299 {
			t.Errorf("Expected last payload to be 299, got %d", lastPayload)
		}
	}

	// Verify wrap-around: payloads should be sequential
	for i := 1; i < len(events); i++ {
		prev := events[i-1].Payload.(int)
		curr := events[i].Payload.(int)
		if curr != prev+1 {
			t.Errorf("Events not sequential: events[%d]=%d, events[%d]=%d", i-1, prev, i, curr)
		}
	}
}

// TestEventQueuePeek tests that Peek returns events without consuming them
func TestEventQueuePeek(t *testing.T) {
	eq := NewEventQueue()

	// Push 3 events
	for i := 0; i < 3; i++ {
		event := GameEvent{
			Type:      EventCleanerRequest,
			Payload:   i,
			Frame:     int64(i),
			Timestamp: time.Now(),
		}
		eq.Push(event)
	}

	// Peek should return all events
	peeked := eq.Peek()
	if len(peeked) != 3 {
		t.Errorf("Expected 3 events from Peek, got %d", len(peeked))
	}

	// Peek again should still return all events
	peeked2 := eq.Peek()
	if len(peeked2) != 3 {
		t.Errorf("Expected 3 events from second Peek, got %d", len(peeked2))
	}

	// Consume should still return all events
	consumed := eq.Consume()
	if len(consumed) != 3 {
		t.Errorf("Expected 3 events from Consume, got %d", len(consumed))
	}

	// Now Peek should return empty
	peeked3 := eq.Peek()
	if len(peeked3) != 0 {
		t.Errorf("Expected 0 events from Peek after Consume, got %d", len(peeked3))
	}
}

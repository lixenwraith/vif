package engine

import (
	"sync/atomic"
	"time"
)

// EventType represents the type of game event
type EventType int

const (
	// EventCleanerRequest is fired when cleaners should be triggered
	EventCleanerRequest EventType = iota

	// EventCleanerFinished is fired when cleaner animation completes
	EventCleanerFinished

	// EventGoldSpawned is fired when a gold sequence is spawned
	EventGoldSpawned

	// EventGoldComplete is fired when a gold sequence is completed
	EventGoldComplete
)

// String returns the name of the event type for debugging
func (e EventType) String() string {
	switch e {
	case EventCleanerRequest:
		return "CleanerRequest"
	case EventCleanerFinished:
		return "CleanerFinished"
	case EventGoldSpawned:
		return "GoldSpawned"
	case EventGoldComplete:
		return "GoldComplete"
	default:
		return "Unknown"
	}
}

// GameEvent represents a single game event
type GameEvent struct {
	Type      EventType   // Type of event
	Payload   interface{} // Optional event data
	Frame     int64       // Frame number when event was created
	Timestamp time.Time   // When the event was created
}

// EventQueue is a lock-free ring buffer for game events
// Uses atomic operations for thread-safe push/consume operations
// Size is fixed at 256 events
type EventQueue struct {
	events [256]GameEvent  // Fixed-size ring buffer
	head   atomic.Uint64   // Read index (next position to read from)
	tail   atomic.Uint64   // Write index (next position to write to)
}

// NewEventQueue creates a new event queue
func NewEventQueue() *EventQueue {
	eq := &EventQueue{}
	eq.head.Store(0)
	eq.tail.Store(0)
	return eq
}

// Push adds an event to the queue
// Uses lock-free CAS loop to handle concurrent pushes
// If the queue is full, the oldest event is overwritten
func (eq *EventQueue) Push(event GameEvent) {
	for {
		// Read current tail position
		currentTail := eq.tail.Load()
		nextTail := currentTail + 1

		// Try to claim this slot
		if eq.tail.CompareAndSwap(currentTail, nextTail) {
			// Successfully claimed the slot, write the event
			eq.events[currentTail%256] = event

			// Check if we're overwriting unread events
			// If head is more than 256 behind tail, advance it
			currentHead := eq.head.Load()
			if nextTail-currentHead > 256 {
				// Try to advance head to prevent reading stale data
				// This is best-effort; if it fails, the consumer will handle it
				eq.head.CompareAndSwap(currentHead, nextTail-256)
			}

			return
		}
		// CAS failed, retry
	}
}

// Consume returns all pending events and clears the queue
// This is designed for single-consumer use (the game loop)
// Returns a slice of events in FIFO order
func (eq *EventQueue) Consume() []GameEvent {
	// Atomically claim all pending events
	currentHead := eq.head.Load()
	currentTail := eq.tail.Load()

	// Calculate number of available events
	available := currentTail - currentHead
	if available == 0 {
		return nil
	}

	// Cap at buffer size to handle wrap-around
	if available > 256 {
		available = 256
		currentHead = currentTail - 256
	}

	// Copy events to result slice
	result := make([]GameEvent, available)
	for i := uint64(0); i < available; i++ {
		result[i] = eq.events[(currentHead+i)%256]
	}

	// Advance head to mark events as consumed
	// Use CAS to ensure atomicity, but we expect this to succeed
	// since we're the only consumer
	for !eq.head.CompareAndSwap(currentHead, currentTail) {
		// If CAS fails, recalculate and retry
		currentHead = eq.head.Load()
		currentTail = eq.tail.Load()
		if currentTail == currentHead {
			// Queue was emptied by another consumer or became empty
			return result
		}
	}

	return result
}

// Peek returns all pending events without removing them from the queue
// Useful for read-only inspection
// Returns a slice of events in FIFO order
func (eq *EventQueue) Peek() []GameEvent {
	currentHead := eq.head.Load()
	currentTail := eq.tail.Load()

	// Calculate number of available events
	available := currentTail - currentHead
	if available == 0 {
		return nil
	}

	// Cap at buffer size to handle wrap-around
	if available > 256 {
		available = 256
		currentHead = currentTail - 256
	}

	// Copy events to result slice
	result := make([]GameEvent, available)
	for i := uint64(0); i < available; i++ {
		result[i] = eq.events[(currentHead+i)%256]
	}

	return result
}

// Len returns the current number of events in the queue
// Note: This is a snapshot and may change immediately after reading
func (eq *EventQueue) Len() int {
	currentHead := eq.head.Load()
	currentTail := eq.tail.Load()
	available := currentTail - currentHead

	if available > 256 {
		return 256
	}
	return int(available)
}

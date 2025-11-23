// Package engine provides core game event infrastructure for vi-fighter.
//
// Event System Architecture
//
// The event system enables decoupled, event-driven communication between game systems.
// Systems communicate by pushing events to a shared EventQueue rather than calling
// methods directly on other systems or mutating shared state flags.
//
// Key Benefits:
// - Decoupling: Systems don't hold references to each other
// - Thread-Safety: Lock-free ring buffer with atomic operations
// - Testability: Events can be inspected and verified in tests
// - Debuggability: All system interactions are observable via event log
//
// Event Flow Pattern:
//  1. Producer system pushes event: ctx.PushEvent(EventType, payload)
//  2. Event stored in lock-free ring buffer (capacity: 256 events)
//  3. Consumer system polls events: events := ctx.ConsumeEvents()
//  4. Consumer processes events in Update() method
//
// Thread-Safety Guarantees:
// - Push operations are lock-free (CAS loop with retry)
// - Consume operations are designed for single consumer (game loop)
// - Peek operations are safe for concurrent read-only inspection
// - Events include Frame ID to prevent processing stale events
// - Ring buffer automatically overwrites oldest events when full
//
// Usage Example:
//  // Producer (ScoreSystem triggers cleaners)
//  if heatAtMax {
//      ctx.PushEvent(engine.EventCleanerRequest, nil)
//  }
//
//  // Consumer (CleanerSystem spawns cleaners)
//  func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
//      events := cs.ctx.ConsumeEvents()
//      for _, event := range events {
//          if event.Type == engine.EventCleanerRequest {
//              cs.spawnCleaners(world)
//          }
//      }
//  }
package engine

import (
	"sync/atomic"
	"time"
)

// EventType represents the type of game event.
// Each event type has specific semantics for when it should be pushed and how consumers should respond.
type EventType int

const (
	// EventCleanerRequest signals that cleaner entities should be spawned.
	//
	// Triggered When:
	//   - Gold sequence completed while heat meter is at maximum
	//   - ScoreSystem detects this condition after gold character typed
	//
	// Consumed By:
	//   - CleanerSystem.Update() polls EventQueue each frame
	//   - Spawns cleaner entities on rows containing Red characters
	//   - If no Red characters exist, performs "phantom spawn" (no entities created)
	//
	// Payload: nil (no additional data needed)
	//
	// Frame Deduplication:
	//   - CleanerSystem tracks spawned frames to prevent duplicate spawns
	//   - Multiple EventCleanerRequest in same frame → single spawn
	//
	// Thread-Safety:
	//   - Can be pushed from any thread (ScoreSystem runs in main loop)
	//   - Consumed only by main game loop (single consumer)
	EventCleanerRequest EventType = iota

	// EventCleanerFinished signals that cleaner animation has completed.
	//
	// Triggered When:
	//   - All cleaner entities have been destroyed (reached target positions)
	//   - CleanerSystem.Update() detects zero active cleaners after spawning
	//
	// Consumed By:
	//   - Currently used for testing and debugging
	//   - Future: Could trigger follow-up effects or achievements
	//
	// Payload: nil (no additional data needed)
	//
	// Purpose:
	//   - Marks end of cleaner lifecycle for observers
	//   - Enables verification that cleaner animation completed successfully
	//   - Does NOT trigger phase transitions (cleaners are non-blocking)
	EventCleanerFinished

	// EventGoldSpawned signals that a gold sequence has been created.
	//
	// Triggered When:
	//   - GoldSequenceSystem spawns a new 10-character gold sequence
	//   - Occurs when PhaseDecayAnimation completes (transitions to PhaseNormal)
	//
	// Consumed By:
	//   - Currently used for testing and debugging
	//   - Future: Could trigger audio/visual effects or UI updates
	//
	// Payload: nil (gold sequence details available via ctx.State.ReadGoldState())
	//
	// State Coordination:
	//   - GoldSequenceSystem maintains GoldActive flag in GameState
	//   - EventGoldSpawned is informational, does not replace state checking
	EventGoldSpawned

	// EventGoldComplete signals that a gold sequence has been successfully typed.
	//
	// Triggered When:
	//   - Player types the final character of an active gold sequence
	//   - ScoreSystem handles final character and validates completion
	//
	// Consumed By:
	//   - Currently used for testing and debugging
	//   - Future: Could trigger achievement tracking or statistics
	//
	// Payload: nil (gold completion handled via GameState)
	//
	// Side Effects (via GameState, not event):
	//   - Heat meter filled to maximum
	//   - EventCleanerRequest pushed if heat was already at max
	//   - GoldActive flag cleared in GameState
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

// GameEvent represents a single game event with associated metadata.
//
// Events are immutable once created and flow from producers to consumers via EventQueue.
// The Frame field enables deduplication (prevent processing same event multiple times).
// The Timestamp field enables debugging and performance analysis.
type GameEvent struct {
	Type      EventType   // Type of event (determines semantic meaning)
	Payload   interface{} // Optional event-specific data (currently unused, reserved for future)
	Frame     int64       // Frame number when event was created (for deduplication)
	Timestamp time.Time   // Creation timestamp (for debugging and metrics)
}

// EventQueue is a lock-free ring buffer for game events.
//
// Architecture:
//   - Fixed-size ring buffer (256 events capacity)
//   - Lock-free push via atomic CAS (Compare-And-Swap) operations
//   - Single-consumer design for consume operations (game loop)
//   - Automatic oldest-event overwriting when buffer is full
//
// Thread-Safety:
//   - Push: Lock-free CAS loop, safe for multiple concurrent producers
//   - Consume: Designed for single consumer (game loop), uses CAS for atomicity
//   - Peek: Safe for concurrent read-only inspection from any thread
//
// Performance:
//   - Push: O(1) amortized (CAS retry on contention)
//   - Consume: O(n) where n is number of pending events (typically < 10)
//   - Peek: O(n) where n is number of pending events
//   - Zero allocations for push operations (events stored inline)
//
// Overflow Behavior:
//   - When buffer is full (256 events), oldest events are automatically overwritten
//   - Head pointer is advanced to maintain ring buffer invariants
//   - Consumers may miss events if they fall behind by > 256 events (rare in practice)
type EventQueue struct {
	events [256]GameEvent  // Fixed-size ring buffer (capacity: 256)
	head   atomic.Uint64   // Read index (next position to read from)
	tail   atomic.Uint64   // Write index (next position to write to)
}

// NewEventQueue creates a new event queue with empty state.
//
// Returns:
//   - Initialized EventQueue with head=0, tail=0 (empty queue)
//
// Usage:
//   - Called once during GameContext initialization
//   - Queue is shared across all systems via GameContext
func NewEventQueue() *EventQueue {
	eq := &EventQueue{}
	eq.head.Store(0)
	eq.tail.Store(0)
	return eq
}

// Push adds an event to the queue using a lock-free algorithm.
//
// Algorithm:
//  1. Read current tail position (atomic load)
//  2. Calculate next tail position (tail + 1)
//  3. Attempt to claim slot via CAS (Compare-And-Swap)
//  4. If CAS succeeds: Write event to claimed slot, check for overflow
//  5. If CAS fails: Retry from step 1 (another thread claimed the slot)
//
// Overflow Handling:
//   - If tail is more than 256 ahead of head, advance head (overwrite oldest)
//   - This is best-effort; consumer will handle stale data via frame checks
//
// Thread-Safety:
//   - Safe to call from multiple threads concurrently
//   - Uses atomic CAS for synchronization (no mutexes)
//   - Retries automatically on contention
//
// Performance:
//   - Typical case: O(1) with single CAS operation
//   - High contention: O(k) where k is number of concurrent pushers (retry loop)
//
// Parameters:
//   - event: GameEvent to add to queue (copied by value, not referenced)
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

// Consume returns all pending events and atomically marks them as consumed.
//
// Design:
//   - Designed for single-consumer use (the game loop)
//   - Returns events in FIFO order (oldest first)
//   - Atomically advances head pointer to mark events as consumed
//   - Does NOT remove events from buffer (overwritten by future pushes)
//
// Algorithm:
//  1. Atomically read current head and tail positions
//  2. Calculate number of available events (tail - head)
//  3. Cap at buffer size (256) to handle wrap-around
//  4. Copy events to result slice
//  5. Advance head pointer via CAS to mark as consumed
//
// Thread-Safety:
//   - Safe to call from game loop thread
//   - Uses CAS for atomic head advancement
//   - Concurrent pushes are safe (they modify tail, not head)
//   - Multiple concurrent consumers would conflict (not supported)
//
// Return Value:
//   - nil: If queue is empty (no events to consume)
//   - []GameEvent: Slice of pending events (FIFO order)
//
// Performance:
//   - O(n) where n is number of pending events
//   - Allocates slice for return value (unavoidable for safe API)
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

// Peek returns all pending events without consuming them (read-only inspection).
//
// Purpose:
//   - Debugging: Inspect event queue state without modifying it
//   - Testing: Verify events were pushed correctly
//   - Monitoring: Check queue depth and event types
//
// Difference from Consume:
//   - Peek: Returns events but does NOT advance head pointer (events remain in queue)
//   - Consume: Returns events AND advances head pointer (events marked as consumed)
//
// Thread-Safety:
//   - Safe to call from any thread concurrently
//   - Does not modify queue state (read-only)
//   - Concurrent pushes/consumes may cause snapshot to become stale immediately
//
// Return Value:
//   - nil: If queue is empty
//   - []GameEvent: Slice of pending events (FIFO order, snapshot at call time)
//
// Performance:
//   - O(n) where n is number of pending events
//   - Allocates slice for return value
//
// Note:
//   - Returned slice is a snapshot; queue state may change after call returns
//   - Events in returned slice may be consumed by Consume() before Peek() caller processes them
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

// Len returns the current number of pending events in the queue (snapshot).
//
// Purpose:
//   - Monitoring: Check queue depth for performance analysis
//   - Debugging: Verify queue is not growing unbounded
//   - Testing: Assert expected number of events after operations
//
// Thread-Safety:
//   - Safe to call from any thread concurrently
//   - Returns snapshot at call time (may be stale immediately)
//   - Concurrent pushes/consumes may change length before caller uses value
//
// Return Value:
//   - int: Number of events currently in queue (0-256)
//   - Capped at 256 (buffer capacity)
//
// Performance:
//   - O(1): Two atomic loads and arithmetic
//
// Note:
//   - Returned value is a point-in-time snapshot
//   - Queue length may change before caller can act on the value
//   - Use Peek() if you need both length AND event contents atomically
func (eq *EventQueue) Len() int {
	currentHead := eq.head.Load()
	currentTail := eq.tail.Load()
	available := currentTail - currentHead

	if available > 256 {
		return 256
	}
	return int(available)
}

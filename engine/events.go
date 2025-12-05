package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
)

// EventType represents the type of game event.
// Each event type has specific semantics for when it should be pushed and how consumers should respond.
type EventType int

const (
	// EventCleanerRequest signals that cleaner entities should be spawned.
	//
	// Triggered When:
	//   - Gold sequence completed while heat meter is at maximum
	//   - EnergySystem detects this condition after gold character typed
	//
	// Consumed By:
	//   - CleanerSystem.HandleEvent() via EventRouter
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
	//   - Can be pushed from any thread (EnergySystem runs in main loop)
	//   - Consumed only by main game loop (single consumer)
	EventCleanerRequest EventType = iota

	// EventDirectionalCleanerRequest signals that directional cleaner entities should be spawned.
	//
	// Triggered When:
	//   - Nugget collected while heat meter is at maximum
	//   - Enter key pressed in Normal mode with heat >= 10
	//
	// Consumed By:
	//   - CleanerSystem.HandleEvent() via EventRouter
	//   - Spawns 4 cleaner entities from origin position (up/down/left/right)
	//
	// Payload: *DirectionalCleanerPayload containing origin coordinates
	//
	// Animation:
	//   - Each cleaner locks its row (horizontal) or column (vertical) at spawn
	//   - Cleaners clear entities in their path and despawn at screen edge
	//   - Same animation duration as EventCleanerRequest cleaners
	EventDirectionalCleanerRequest

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

	// EventGoldSpawned signals that a gold sequence has been created
	//
	// Triggered When:
	//   - GoldSequenceSystem spawns a new gold sequence (PhaseNormal)
	//
	// Consumed By:
	//   - SplashSystem: Spawns the anchored Gold Timer entity
	//   - AudioSystem (future): Play spawn sound
	//
	// Payload: *GoldSpawnedPayload
	//   - Contains SequenceID, position, dimensions, and duration for timer anchoring
	EventGoldSpawned

	// EventGoldComplete signals that a gold sequence has been successfully typed
	//
	// Triggered When:
	//   - Player types the final character of an active gold sequence
	//
	// Consumed By:
	//   - SplashSystem: Destroys the anchored Gold Timer immediately
	//   - EnergySystem (internal handling): Fills heat, points
	//
	// Payload: *GoldCompletionPayload
	//   - Contains SequenceID to identify which timer to destroy
	EventGoldComplete

	// EventGoldTimeout signals that a gold sequence expired before completion
	//
	// Triggered When:
	//   - GoldSystem detects timeout in update loop
	//
	// Consumed By:
	//   - SplashSystem: Destroys the anchored Gold Timer
	//
	// Payload: *GoldCompletionPayload
	//   - Contains SequenceID to identify which timer to destroy
	EventGoldTimeout

	// EventGoldDestroyed signals that a gold sequence was destroyed by an external factor (e.g. Drain)
	//
	// Triggered When:
	//   - DrainSystem (or other mechanics) destroys a Gold entity
	//
	// Consumed By:
	//   - SplashSystem: Destroys the anchored Gold Timer
	//
	// Payload: *GoldCompletionPayload
	//   - Contains SequenceID to identify which timer to destroy
	EventGoldDestroyed

	// EventCharacterTyped signals that a character was typed in insert mode.
	//
	// Triggered When:
	//   - User presses a printable key in Insert mode
	//   - InputHandler captures the keystroke and cursor position
	//
	// Consumed By:
	//   - EnergySystem via EventRouter
	//   - Processes character match, scoring, cursor movement, effects
	//
	// Payload: *CharacterTypedPayload containing typed rune and position
	//
	// Latency:
	//   - Event pushed immediately on keypress
	//   - Processed on next game tick (max 50ms delay)
	//   - Visual feedback (cursor move, entity destroy) occurs after processing
	EventCharacterTyped

	// EventEnergyTransaction signals an energy change request.
	//
	// Triggered When:
	//   - Nugget jump costs energy (-10)
	//   - Future: power-ups, penalties, etc.
	//
	// Consumed By:
	//   - EnergySystem via EventRouter
	//   - Applies atomic energy delta
	//
	// Payload: *EnergyTransactionPayload containing amount
	EventEnergyTransaction

	// EventSplashRequest signals a request for transient visual feedback (text flash)
	//
	// Triggered When:
	//   - Character typed (Input/Energy systems)
	//   - Command executed
	//   - Nugget collected
	//
	// Consumed By:
	//   - SplashSystem: Creates a transient splash entity
	//     - Enforces uniqueness (replaces existing transient splash)
	//     - Calculates smart layout to avoid Cursor and Gold Sequence
	//
	// Payload: *SplashRequestPayload
	//   - Content, color, and origin (cursor) position
	EventSplashRequest
)

// String returns the name of the event type for debugging
func (e EventType) String() string {
	switch e {
	case EventCleanerRequest:
		return "CleanerRequest"
	case EventDirectionalCleanerRequest:
		return "DirectionalCleanerRequest"
	case EventCleanerFinished:
		return "CleanerFinished"
	case EventGoldSpawned:
		return "GoldSpawned"
	case EventGoldComplete:
		return "GoldComplete"
	case EventGoldTimeout:
		return "GoldTimeout"
	case EventGoldDestroyed:
		return "GoldDestroyed"
	case EventCharacterTyped:
		return "CharacterTyped"
	case EventEnergyTransaction:
		return "EnergyTransaction"
	case EventSplashRequest:
		return "SplashRequest"
	default:
		return "Unknown"
	}
}

// DirectionalCleanerPayload contains origin coordinates for directional cleaner spawning.
//
// Used By:
//   - EventDirectionalCleanerRequest events
//   - CleanerSystem.spawnDirectionalCleaners()
//
// Fields:
//   - OriginX: X coordinate where 4-way cleaners spawn from
//   - OriginY: Y coordinate where 4-way cleaners spawn from
type DirectionalCleanerPayload struct {
	OriginX int
	OriginY int
}

// Event Flow Pattern:
//  1. Producer system pushes event: ctx.PushEvent(EventType, payload)
//  2. Event stored in lock-free ring buffer (capacity: 256 events)
//  3. ClockScheduler triggers EventRouter.DispatchAll()
//  4. EventRouter invokes HandleEvent() on registered handlers synchronously

// GameEvent represents a single game event with associated metadata
type GameEvent struct {
	Type      EventType // Type of event (determines semantic meaning)
	Payload   any       // Optional event-specific data
	Frame     int64     // Frame number when event was created (for deduplication)
	Timestamp time.Time // Creation timestamp (for debugging and metrics)
}

// EventQueue is a lock-free ring buffer for game events.
//
// Architecture:
//   - Fixed-size ring buffer (EventQueueSize events capacity)
//   - Lock-free push via atomic CAS (Compare-And-Swap) operations
//   - Single-consumer design for consume operations (game loop)
//   - Automatic oldest-event overwriting when buffer is full
//   - Published flags pattern prevents reading partially-written events
//
// Thread-Safety:
//   - Push: Lock-free CAS loop, safe for multiple concurrent producers
//   - Consume: Designed for single consumer (game loop), uses CAS for atomicity
//   - Peek: Safe for concurrent read-only inspection from any thread
//   - Published flags ensure readers never see partially-written events
//
// Performance:
//   - Push: O(1) amortized (CAS retry on contention)
//   - Consume: O(n) where n is number of pending events (typically < 10)
//   - Peek: O(n) where n is number of pending events
//   - Zero allocations for push operations (events stored inline)
//
// Overflow Behavior:
//   - When buffer is full (EventQueueSize events), oldest events are automatically overwritten
//   - Head pointer is advanced to maintain ring buffer invariants
//   - Consumers may miss events if they fall behind by > EventQueueSize events (rare in practice)
type EventQueue struct {
	events    [constants.EventQueueSize]GameEvent   // Fixed-size ring buffer
	published [constants.EventQueueSize]atomic.Bool // Published flags (true = event is fully written and ready to read)
	head      atomic.Uint64                         // Read index (next position to read from)
	tail      atomic.Uint64                         // Write index (next position to write to)
}

// CharacterTypedPayload contains data for EventCharacterTyped
//
// Fields capture the state at the moment of keypress
// EnergySystem uses X,Y to find the target entity
type CharacterTypedPayload struct {
	Char rune // The character typed by the user
	X    int  // Cursor X position when typed
	Y    int  // Cursor Y position when typed
}

// CharacterTypedPayloadPool recycles payload objects to reduce GC pressure during high-frequency typing
// NOTE: Likely not a real issue, but useful for stress testing
var CharacterTypedPayloadPool = sync.Pool{
	New: func() any {
		return &CharacterTypedPayload{}
	},
}

// EnergyTransactionPayload contains data for EventEnergyTransaction
//
// Positive Amount = gain, Negative Amount = cost
type EnergyTransactionPayload struct {
	Amount int    // Energy delta (can be negative)
	Source string // Debug identifier (e.g., "NuggetJump", "Penalty")
}

// GoldSpawnedPayload contains details about a newly spawned gold sequence.
// Used by SplashSystem to anchor the countdown timer relative to the sequence.
type GoldSpawnedPayload struct {
	SequenceID int           // Unique ID of the sequence
	OriginX    int           // X position of the sequence start
	OriginY    int           // Y position of the sequence
	Length     int           // Length of the sequence in characters
	Duration   time.Duration // Total duration for the timer
}

// GoldCompletionPayload contains details about a completed or timed-out gold sequence.
// Used by SplashSystem to identify and remove the associated timer.
type GoldCompletionPayload struct {
	SequenceID int // Unique ID of the sequence that finished
}

// SplashRequestPayload contains data for creating a transient visual flash.
type SplashRequestPayload struct {
	Text    string                 // The text to display
	Color   components.SplashColor // Semantic color enum
	OriginX int                    // X position of the cause (usually cursor)
	OriginY int                    // Y position of the cause (usually cursor)
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

// Push adds an event to the queue using a lock-free algorithm with published flags.
//
// Algorithm:
//  1. Read current tail position (atomic load)
//  2. Calculate next tail position (tail + 1)
//  3. Attempt to claim slot via CAS (Compare-And-Swap)
//  4. If CAS succeeds:
//     a. Write event to claimed slot (non-atomic write)
//     b. Set published flag to true (atomic store) - readers can now see this event
//     c. Check for overflow and advance head if needed
//  5. If CAS fails: Retry from step 1 (another thread claimed the slot)
//
// Published Flags Pattern:
//   - Prevents readers from seeing partially-written events
//   - Writer sets published[index] = true AFTER writing event data
//   - Reader checks published[index] == true BEFORE reading event data
//   - Eliminates data race between concurrent Push() and Consume()
//
// Overflow Handling:
//   - If tail is more than EventQueueSize ahead of head, advance head (overwrite oldest)
//   - This is best-effort; consumer will handle stale data via frame checks
//
// Thread-Safety:
//   - Safe to call from multiple threads concurrently
//   - Uses atomic CAS for synchronization (no mutexes)
//   - Published flags prevent reading partially-written events
//   - Retries automatically on contention
//
// Performance:
//   - Typical case: O(1) with single CAS operation + one atomic store
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
			idx := currentTail & constants.EventBufferMask

			// Write event data to claimed slot
			eq.events[idx] = event

			// Mark slot as published (readers can now safely read this event)
			// This MUST happen AFTER writing event data to prevent data race
			eq.published[idx].Store(true)

			// Check if we're overwriting unread events
			// If head is more than EventQueueSize behind tail, advance it
			currentHead := eq.head.Load()
			if nextTail-currentHead > constants.EventQueueSize {
				// Try to advance head to prevent reading stale data
				// This is best-effort; if it fails, the consumer will handle it
				eq.head.CompareAndSwap(currentHead, nextTail-constants.EventQueueSize)
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
//   - Atomically advances head pointer using CAS to prevent regression
//   - Checks published flags to avoid reading partially-written events
//
// Algorithm (Published Flags Pattern):
//  1. Read current head and tail positions
//  2. Loop from head to tail:
//     a. Check if published[index] is true
//     b. If false: Stop consuming (writer hasn't finished writing yet)
//     c. If true: Read event, reset published[index] to false, add to result
//  3. CAS loop to advance head pointer (prevents regression from concurrent Push overflow)
//
// Thread-Safety:
//   - Safe to call from game loop thread (single consumer)
//   - Published flags prevent reading partially-written events
//   - CAS ensures head never regresses even under producer overflow pressure
//   - Multiple concurrent consumers would conflict (not supported)
//
// Return Value:
//   - nil: If queue is empty (no events to consume)
//   - []GameEvent: Slice of pending events (FIFO order)
func (eq *EventQueue) Consume() []GameEvent {
	for {
		// Read current positions
		currentHead := eq.head.Load()
		currentTail := eq.tail.Load()

		// Check if queue is empty
		if currentTail == currentHead {
			return nil
		}

		// Calculate maximum available events (cap at buffer size)
		maxAvailable := currentTail - currentHead
		if maxAvailable > constants.EventQueueSize {
			maxAvailable = constants.EventQueueSize
			currentHead = currentTail - constants.EventQueueSize
		}

		// Read events one by one, checking published flag
		result := make([]GameEvent, 0, maxAvailable)
		for i := uint64(0); i < maxAvailable; i++ {
			idx := (currentHead + i) & constants.EventBufferMask

			// Check if this slot is published (fully written)
			if !eq.published[idx].Load() {
				// Writer hasn't finished writing this event yet, stop here
				break
			}

			// Read the event (safe now that published flag is true)
			result = append(result, eq.events[idx])

			// Reset published flag for future reuse
			eq.published[idx].Store(false)
		}

		// Advance head atomically using CAS to prevent regression
		// If Push advanced head due to overflow, our CAS will fail and we retry
		newHead := currentHead + uint64(len(result))
		if eq.head.CompareAndSwap(currentHead, newHead) {
			if len(result) == 0 {
				return nil
			}
			return result
		}
		// CAS failed: head was modified by Push overflow, retry with fresh state
	}
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
//   - Checks published flags to avoid reading partially-written events
//   - Concurrent pushes/consumes may cause snapshot to become stale immediately
//
// Return Value:
//   - nil: If queue is empty
//   - []GameEvent: Slice of pending events (FIFO order, snapshot at call time)
//
// Performance:
//   - O(n) where n is number of pending events
//   - Allocates slice for return value
//   - Additional atomic loads for published flags
//
// Note:
//   - Returned slice is a snapshot; queue state may change after call returns
//   - Events in returned slice may be consumed by Consume() before Peek() caller processes them
//   - Only returns events with published flag set to true (fully written)
//
// NOTE: Currently dead code, kept for debugging
func (eq *EventQueue) Peek() []GameEvent {
	currentHead := eq.head.Load()
	currentTail := eq.tail.Load()

	// Check if queue is empty
	if currentTail == currentHead {
		return nil
	}

	// Calculate maximum available events
	maxAvailable := currentTail - currentHead
	if maxAvailable > constants.EventQueueSize {
		maxAvailable = constants.EventQueueSize
		currentHead = currentTail - constants.EventQueueSize
	}

	// Read events one by one, checking published flag
	result := make([]GameEvent, 0, maxAvailable)
	for i := uint64(0); i < maxAvailable; i++ {
		idx := (currentHead + i) & constants.EventBufferMask

		// Check if this slot is published (fully written)
		if !eq.published[idx].Load() {
			// Writer hasn't finished writing this event yet, stop here
			break
		}

		// Read the event (safe now that published flag is true)
		result = append(result, eq.events[idx])
	}

	// Return nil if no published events
	if len(result) == 0 {
		return nil
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
//   - int: Number of events currently in queue (0-EventQueueSize)
//   - Capped at EventQueueSize (buffer capacity)
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

	if available > constants.EventQueueSize {
		return constants.EventQueueSize
	}
	return int(available)
}

// Reset clears the event queue, removing all pending events
// Not thread-safe against concurrent Push/Consume operations;
// MUST be called within a safe context (e.g., world lock during reset)
func (eq *EventQueue) Reset() {
	eq.head.Store(0)
	eq.tail.Store(0)

	// Reset published flags and clear events to release references
	var zeroEvent GameEvent
	for i := range eq.events {
		eq.published[i].Store(false)
		eq.events[i] = zeroEvent
	}
}
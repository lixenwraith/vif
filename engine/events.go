package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
)

// EventType represents the type of game event
type EventType int

const (
	// EventCleanerRequest spawns cleaners on rows with Red characters
	// Trigger: Gold sequence completed at max heat
	// Consumer: CleanerSystem | Payload: nil
	EventCleanerRequest EventType = iota

	// EventDirectionalCleanerRequest spawns 4-way cleaners from origin
	// Trigger: Nugget collected at max heat, Enter in Normal mode with heat >= 10
	// Consumer: CleanerSystem | Payload: *DirectionalCleanerPayload
	EventDirectionalCleanerRequest

	// EventCleanerFinished marks cleaner animation completion
	// Trigger: All cleaner entities destroyed | Payload: nil
	EventCleanerFinished

	// EventNuggetJumpRequest signals player intent to jump to active nugget
	// Trigger: InputHandler (Tab key)
	// Consumer: NuggetSystem | Payload: nil
	EventNuggetJumpRequest

	// EventGoldSpawned signals gold sequence creation
	// Trigger: GoldSystem spawns sequence in PhaseNormal
	// Consumer: SplashSystem (timer) | Payload: *GoldSpawnedPayload
	EventGoldSpawned

	// EventGoldComplete signals successful gold sequence completion
	// Trigger: Final gold character typed
	// Consumer: SplashSystem (destroy timer) | Payload: *GoldCompletionPayload
	EventGoldComplete

	// EventGoldTimeout signals gold sequence expiration
	// Trigger: GoldSystem timeout | Payload: *GoldCompletionPayload
	EventGoldTimeout

	// EventGoldDestroyed signals external gold destruction (e.g., Drain)
	// Payload: *GoldCompletionPayload
	EventGoldDestroyed

	// EventCharacterTyped signals Insert mode keypress
	// Trigger: InputHandler on printable key
	// Consumer: EnergySystem | Payload: *CharacterTypedPayload
	// Latency: max 50ms (next tick)
	EventCharacterTyped

	// EventEnergyTransaction signals energy delta
	// Trigger: Nugget jump (-10), power-ups, penalties
	// Consumer: EnergySystem | Payload: *EnergyTransactionPayload
	EventEnergyTransaction

	// EventSplashRequest signals transient visual feedback
	// Trigger: Character typed, command executed, nugget collected
	// Consumer: SplashSystem | Payload: *SplashRequestPayload
	EventSplashRequest

	// EventShieldActivate signals shield should become active
	// Trigger: EnergySystem when energy > 0 and shield inactive
	// Consumer: ShieldSystem | Payload: nil
	EventShieldActivate

	// EventShieldDeactivate signals shield should become inactive
	// Trigger: EnergySystem when energy <= 0 and shield active
	// Consumer: ShieldSystem | Payload: nil
	EventShieldDeactivate

	// EventShieldDrain signals energy drain from external source
	// Trigger: DrainSystem when drain inside shield zone
	// Consumer: ShieldSystem | Payload: *ShieldDrainPayload
	EventShieldDrain
)

// ShieldDrainPayload contains energy drain amount from external sources
type ShieldDrainPayload struct {
	Amount int
}

// DirectionalCleanerPayload contains origin for 4-way cleaner spawn
type DirectionalCleanerPayload struct {
	OriginX int
	OriginY int
}

// Event Flow:
//  1. Producer: ctx.PushEvent(type, payload)
//  2. Storage: Lock-free ring buffer (256 capacity)
//  3. Dispatch: ClockScheduler → EventRouter.DispatchAll()
//  4. Consume: EventRouter → registered handlers synchronously

// GameEvent represents a single game event with metadata
type GameEvent struct {
	Type      EventType
	Payload   any
	Frame     int64 // For deduplication
	Timestamp time.Time
}

// EventQueue is a lock-free MPSC ring buffer for game events
// Thread-Safety:
//   - Push: Lock-free CAS, multiple producers OK
//   - Consume: Single consumer (game loop)
//   - Published flags prevent reading partial writes
//
// Overflow: Oldest events overwritten when full
type EventQueue struct {
	events    [constants.EventQueueSize]GameEvent
	published [constants.EventQueueSize]atomic.Bool // True = slot fully written
	head      atomic.Uint64                         // Read index
	tail      atomic.Uint64                         // Write index
}

// CharacterTypedPayload captures keypress state for EnergySystem
type CharacterTypedPayload struct {
	Char rune
	X    int // Cursor position when typed
	Y    int
}

// CharacterTypedPayloadPool reduces GC pressure during high-frequency typing
var CharacterTypedPayloadPool = sync.Pool{
	New: func() any { return &CharacterTypedPayload{} },
}

// EnergyTransactionPayload contains energy delta (positive = gain, negative = cost)
type EnergyTransactionPayload struct {
	Amount int
	Source string // Debug identifier
}

// GoldSpawnedPayload anchors countdown timer to sequence position
type GoldSpawnedPayload struct {
	SequenceID int
	OriginX    int
	OriginY    int
	Length     int
	Duration   time.Duration
}

// GoldCompletionPayload identifies which timer to destroy
type GoldCompletionPayload struct {
	SequenceID int
}

// SplashRequestPayload creates transient visual flash
type SplashRequestPayload struct {
	Text    string
	Color   components.SplashColor
	OriginX int // Origin position (usually cursor)
	OriginY int
}

func NewEventQueue() *EventQueue {
	eq := &EventQueue{}
	eq.head.Store(0)
	eq.tail.Store(0)
	return eq
}

// Push adds event using lock-free CAS with published flags pattern
// Safe for concurrent producers. O(1) amortized
func (eq *EventQueue) Push(event GameEvent) {
	for {
		currentTail := eq.tail.Load()
		nextTail := currentTail + 1

		if eq.tail.CompareAndSwap(currentTail, nextTail) {
			idx := currentTail & constants.EventBufferMask

			eq.events[idx] = event
			eq.published[idx].Store(true) // MUST be after write

			// Advance head if overwriting unread events
			currentHead := eq.head.Load()
			if nextTail-currentHead > constants.EventQueueSize {
				eq.head.CompareAndSwap(currentHead, nextTail-constants.EventQueueSize)
			}
			return
		}
	}
}

// Consume returns all pending events in FIFO order and advances head
// Single-consumer design (game loop). Checks published flags for safety
func (eq *EventQueue) Consume() []GameEvent {
	for {
		currentHead := eq.head.Load()
		currentTail := eq.tail.Load()

		if currentTail == currentHead {
			return nil
		}

		maxAvailable := currentTail - currentHead
		if maxAvailable > constants.EventQueueSize {
			maxAvailable = constants.EventQueueSize
			currentHead = currentTail - constants.EventQueueSize
		}

		result := make([]GameEvent, 0, maxAvailable)
		for i := uint64(0); i < maxAvailable; i++ {
			idx := (currentHead + i) & constants.EventBufferMask

			if !eq.published[idx].Load() {
				break // Writer incomplete
			}

			result = append(result, eq.events[idx])
			eq.published[idx].Store(false)
		}

		newHead := currentHead + uint64(len(result))
		if eq.head.CompareAndSwap(currentHead, newHead) {
			if len(result) == 0 {
				return nil
			}
			return result
		}
	}
}

// Peek returns pending events without consuming (read-only snapshot)
// Safe from any thread. May be stale immediately after return
func (eq *EventQueue) Peek() []GameEvent {
	currentHead := eq.head.Load()
	currentTail := eq.tail.Load()

	if currentTail == currentHead {
		return nil
	}

	maxAvailable := currentTail - currentHead
	if maxAvailable > constants.EventQueueSize {
		maxAvailable = constants.EventQueueSize
		currentHead = currentTail - constants.EventQueueSize
	}

	result := make([]GameEvent, 0, maxAvailable)
	for i := uint64(0); i < maxAvailable; i++ {
		idx := (currentHead + i) & constants.EventBufferMask

		if !eq.published[idx].Load() {
			break
		}
		result = append(result, eq.events[idx])
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// Len returns current pending event count (snapshot, 0-EventQueueSize)
func (eq *EventQueue) Len() int {
	currentHead := eq.head.Load()
	currentTail := eq.tail.Load()
	available := currentTail - currentHead

	if available > constants.EventQueueSize {
		return constants.EventQueueSize
	}
	return int(available)
}

// Reset clears queue. NOT thread-safe; call within world lock
func (eq *EventQueue) Reset() {
	eq.head.Store(0)
	eq.tail.Store(0)

	var zeroEvent GameEvent
	for i := range eq.events {
		eq.published[i].Store(false)
		eq.events[i] = zeroEvent
	}
}
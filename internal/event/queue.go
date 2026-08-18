package event

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// EventQueue is a lock-free MPSC ring buffer for game events
// Thread-Safety:
//   - Push: Lock-free CAS, multiple producers OK
//   - Consume: Single consumer (game loop)
//   - Published flags prevent reading partial writes
//
// Overflow: Oldest events overwritten when full
type EventQueue struct {
	events    [parameter.EventQueueSize]GameEvent
	published [parameter.EventQueueSize]atomic.Bool // True = slot fully written
	head      atomic.Uint64                         // Read index
	tail      atomic.Uint64                         // Write index

	overwritten atomic.Uint64           // Events evicted unread by producer overrun
	journal     atomic.Pointer[Journal] // nil = journaling off
	stamp       atomic.Pointer[Stamp]   // record position; advanced under the world lock
}

func NewEventQueue() *EventQueue {
	eq := &EventQueue{}
	eq.stamp.Store(&Stamp{})
	return eq
}

// Push adds event using lock-free CAS with published flags pattern
// Safe for concurrent producers. O(1) amortized
func (eq *EventQueue) Push(event GameEvent) {
	for {
		currentTail := eq.tail.Load()
		nextTail := currentTail + 1

		if eq.tail.CompareAndSwap(currentTail, nextTail) {
			idx := currentTail & parameter.EventBufferMask

			// The claimed slot is the sequence, so record order and dispatch
			// order cannot disagree between concurrent producers
			event.Seq = currentTail

			// Last point the producer owns the payload: once published a
			// handler may recycle it. The origin compare keeps the system
			// hot path to one register test.
			if event.Origin != OriginSystem {
				if j := eq.journal.Load(); j != nil {
					j.record(&event, *eq.stamp.Load())
				}
			}

			eq.events[idx] = event
			eq.published[idx].Store(true) // MUST be after write

			// Advance head if overwriting unread events
			currentHead := eq.head.Load()
			if nextTail-currentHead > parameter.EventQueueSize {
				newHead := nextTail - parameter.EventQueueSize
				// Count on the winning CAS only; a loser's eviction is
				// already accounted by the producer that advanced head
				if eq.head.CompareAndSwap(currentHead, newHead) {
					eq.overwritten.Add(newHead - currentHead)
				}
			}
			return
		}
	}
}

// Pushed returns the total events pushed since construction. A producer compares it
// across a call to learn whether anything needs settling; a settle that dispatches
// only pending system events leaves no journal record and cannot be replayed.
func (eq *EventQueue) Pushed() uint64 { return eq.tail.Load() }

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
		if maxAvailable > parameter.EventQueueSize {
			maxAvailable = parameter.EventQueueSize
			currentHead = currentTail - parameter.EventQueueSize
		}

		result := make([]GameEvent, 0, maxAvailable)
		for i := uint64(0); i < maxAvailable; i++ {
			idx := (currentHead + i) & parameter.EventBufferMask

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

// Len returns approximate pending event count
// Lock-free; used for pre-lock heuristics
func (eq *EventQueue) Len() int {
	head := eq.head.Load()
	tail := eq.tail.Load()
	if tail <= head {
		return 0
	}
	diff := int(tail - head)
	if diff > parameter.EventQueueSize {
		return parameter.EventQueueSize
	}
	return diff
}

// Dropped returns the monotonic count of events evicted unread.
// Non-zero means producers outran the consumer and game state was lost.
func (eq *EventQueue) Dropped() uint64 {
	return eq.overwritten.Load()
}

// SetJournal installs or clears the replay journal; nil disables capture
func (eq *EventQueue) SetJournal(j *Journal) { eq.journal.Store(j) }

// Journal returns the installed journal, nil when disabled
func (eq *EventQueue) Journal() *Journal { return eq.journal.Load() }

// Stamp returns the current record position. The counters advance whether or not
// a journal is installed, so one attached mid-run stamps correctly from its first record.
func (eq *EventQueue) Stamp() Stamp { return *eq.stamp.Load() }

// NextRun opens the next run at tick 0. Called where the tick counter is re-based,
// so the two can never disagree. Caller MUST hold the world lock.
func (eq *EventQueue) NextRun() uint64 {
	run := eq.stamp.Load().Run + 1
	eq.stamp.Store(&Stamp{Run: run})
	return run
}

// BeginTick opens tick t at settle group 0. Caller MUST hold the world lock.
func (eq *EventQueue) BeginTick(tick uint64) {
	eq.stamp.Store(&Stamp{Run: eq.stamp.Load().Run, Tick: tick})
}

// NextBoundary closes the current settle group; a replay settles the same groups.
// Caller MUST hold the world lock.
func (eq *EventQueue) NextBoundary() {
	s := eq.stamp.Load()
	eq.stamp.Store(&Stamp{Run: s.Run, Tick: s.Tick, Boundary: s.Boundary + 1})
}

// AnchorJournal re-emits the anchor at the current stamp; a no-op when journaling is off
func (eq *EventQueue) AnchorJournal(session uint64, speed string, width, height int) {
	if j := eq.journal.Load(); j != nil {
		j.Anchor(*eq.stamp.Load(), session, speed, width, height)
	}
}

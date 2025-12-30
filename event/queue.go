// @lixen: #dev{feature[shield(render,system)],feature[spirit(render,system)]}
package event

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/constant"
)

// EventQueue is a lock-free MPSC ring buffer for game events
// Thread-Safety:
//   - Push: Lock-free CAS, multiple producers OK
//   - Consume: Single consumer (game loop)
//   - Published flags prevent reading partial writes
//
// Overflow: Oldest events overwritten when full
type EventQueue struct {
	events    [constant.EventQueueSize]GameEvent
	published [constant.EventQueueSize]atomic.Bool // True = slot fully written
	head      atomic.Uint64                        // Read index
	tail      atomic.Uint64                        // Write index
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
			idx := currentTail & constant.EventBufferMask

			eq.events[idx] = event
			eq.published[idx].Store(true) // MUST be after write

			// Advance head if overwriting unread events
			currentHead := eq.head.Load()
			if nextTail-currentHead > constant.EventQueueSize {
				eq.head.CompareAndSwap(currentHead, nextTail-constant.EventQueueSize)
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
		if maxAvailable > constant.EventQueueSize {
			maxAvailable = constant.EventQueueSize
			currentHead = currentTail - constant.EventQueueSize
		}

		result := make([]GameEvent, 0, maxAvailable)
		for i := uint64(0); i < maxAvailable; i++ {
			idx := (currentHead + i) & constant.EventBufferMask

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
	if diff > constant.EventQueueSize {
		return constant.EventQueueSize
	}
	return diff
}
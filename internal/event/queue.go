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

	overwritten atomic.Uint64 // Events evicted unread by producer overrun
	dispatched  [EventTypeCount]atomic.Int64
	deadLetter  [EventTypeCount]atomic.Int64
	journal     atomic.Pointer[Journal] // nil = journaling off
	wire        atomic.Pointer[wireHolder]
	stamp       atomic.Pointer[Stamp] // record position; advanced under the world lock
}

// wireHolder boxes the interface so the pointer swap stays atomic
type wireHolder struct{ sink WireSink }

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

			// Transport gate. Separate from the journal: a crossing pushed by a
			// system carries OriginSystem and is never journaled, and the wire set
			// is narrower than the journal's anyway (OnWire).
			if w := eq.wire.Load(); w != nil && OnWire(event) {
				w.sink.Cross(event)
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

// SetWireSink installs or clears the transport sink; nil disconnects.
// Not synchronized against in-flight pushes: a crossing racing a disconnect may
// still reach the departing sink, which is why Cross must not retain the event.
func (eq *EventQueue) SetWireSink(w WireSink) {
	if w == nil {
		eq.wire.Store(nil)
		return
	}
	eq.wire.Store(&wireHolder{sink: w})
}

// ReceiveWire admits a peer's artifacts into the tick about to settle; a no-op
// with no transport. Caller MUST hold the world lock and MUST NOT have settled yet.
func (eq *EventQueue) ReceiveWire() {
	if w := eq.wire.Load(); w != nil {
		w.sink.Receive()
	}
}

// FlushWire sends the tick's accumulated crossings; a no-op with no transport.
// Caller MUST hold the world lock and MUST have settled the tick.
func (eq *EventQueue) FlushWire() {
	if w := eq.wire.Load(); w != nil {
		w.sink.Flush()
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

// RecordDispatch accounts one routed event and whether no consumer accepted it.
// ClockScheduler calls this under the world lock after the routing verdict is known.
func (eq *EventQueue) RecordDispatch(t EventType, dead bool) {
	if !validType(t) {
		return
	}
	eq.dispatched[t].Add(1)
	if dead {
		eq.deadLetter[t].Add(1)
	}
}

// SnapshotTelemetry copies per-type counters for cadence-bound publication.
func (eq *EventQueue) SnapshotTelemetry(dispatch, dead *[EventTypeCount]int64) {
	for i := 1; i < EventTypeCount; i++ {
		dispatch[i] = eq.dispatched[i].Load()
		dead[i] = eq.deadLetter[i].Load()
	}
}

// ResetTelemetry starts queue diagnostics for a new game session.
// Caller MUST hold the world lock and have drained stale queued events.
func (eq *EventQueue) ResetTelemetry() {
	eq.overwritten.Store(0)
	for i := 1; i < EventTypeCount; i++ {
		eq.dispatched[i].Store(0)
		eq.deadLetter[i].Store(0)
	}
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
func (eq *EventQueue) AnchorJournal(live AnchorLive) {
	if j := eq.journal.Load(); j != nil {
		j.Anchor(*eq.stamp.Load(), live)
	}
}

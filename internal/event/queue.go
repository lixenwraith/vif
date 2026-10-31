package event

import (
	"reflect"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/parameter"
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
	wake        chan struct{}         // raised by every publish; a consumer sleeps on it

	// consumeBuf is what Consume returns, reused: one consumer, and a pass's slice
	// is dead by the next call.
	consumeBuf []GameEvent

	// deferred waits for the next tick's body, where ReceiveWire publishes it: a
	// replay's recorded crossings. Written and read under the world lock.
	deferred []GameEvent
}

// wireHolder boxes the interface so the pointer swap stays atomic
type wireHolder struct{ sink WireSink }

func NewEventQueue() *EventQueue {
	eq := &EventQueue{wake: make(chan struct{}, 1)}
	eq.stamp.Store(&Stamp{Boundary: 1}) // between ticks: no body precedes tick 1
	return eq
}

// Wake fires when an event is published, so a consumer sleeps on it rather than
// polling and reads the queue after each; a pending wake absorbs repeats.
func (eq *EventQueue) Wake() <-chan struct{} { return eq.wake }

func (eq *EventQueue) signal() {
	select {
	case eq.wake <- struct{}{}:
	default:
	}
}

// Push adds event using lock-free CAS with published flags pattern
// Safe for concurrent producers. O(1) amortized
func (eq *EventQueue) Push(ev GameEvent) {
	if w := eq.wire.Load(); w != nil && OnWire(ev) && w.sink.Cross(ev) {
		return
	}
	eq.publish(ev)
}

// PushReady publishes an artifact already admitted by the barrier without
// offering it to the wire again. The original origin is preserved for replay.
func (eq *EventQueue) PushReady(ev GameEvent) { eq.publish(ev) }

// Note journals an event that is never dispatched, at the slot the next publish
// takes, so a replay orders it before that event.
func (eq *EventQueue) Note(ev GameEvent) {
	if !ev.Origin.Journaled() {
		return
	}
	if j := eq.journal.Load(); j != nil {
		ev.Seq = eq.tail.Load()
		j.record(&ev, *eq.stamp.Load())
	}
}

// publish journals and enqueues one event after any wire deferral decision.
func (eq *EventQueue) publish(event GameEvent) {
	for {
		currentTail := eq.tail.Load()
		nextTail := currentTail + 1

		if eq.tail.CompareAndSwap(currentTail, nextTail) {
			idx := currentTail & parameter.EventBufferMask

			// The claimed slot is the sequence, so record order and dispatch
			// order cannot disagree between concurrent producers
			event.Seq = currentTail

			// Last point the producer owns the payload: once published a
			// handler may recycle it. Journaled is register compares, which
			// keeps the system hot path cheap. An own crossing is a record
			// whatever produced it: a replay's barrier applies none of its own.
			if event.Origin.Journaled() || event.CrossingSeq != 0 {
				if j := eq.journal.Load(); j != nil {
					j.record(&event, *eq.stamp.Load())
				}
			}

			eq.events[idx] = event
			eq.published[idx].Store(true) // MUST be after write
			eq.signal()

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

// Defer holds an event for the next tick's body, where ReceiveWire publishes it
// ahead of the barrier's: a crossing a replay's recorded run applied there. Caller
// MUST hold the world lock.
func (eq *EventQueue) Defer(ev GameEvent) { eq.deferred = append(eq.deferred, ev) }

// ReceiveWire publishes what was deferred to this tick, then admits the barrier's
// artifacts due before nextTick. Caller holds the world lock.
func (eq *EventQueue) ReceiveWire(nextTick uint64) {
	for _, ev := range eq.deferred {
		eq.publish(ev)
	}
	eq.deferred = eq.deferred[:0]
	if w := eq.wire.Load(); w != nil {
		w.sink.Receive(nextTick)
	}
}

// FlushWire closes completedTick's production epoch. Caller holds the world lock.
func (eq *EventQueue) FlushWire(completedTick uint64) {
	if w := eq.wire.Load(); w != nil {
		w.sink.Flush(completedTick)
	}
}

// Pushed returns the total events pushed since construction. A producer compares it
// across a call to learn whether anything needs settling; a settle that dispatches
// only pending system events leaves no journal record and cannot be replayed.
func (eq *EventQueue) Pushed() uint64 { return eq.tail.Load() }

// Consume returns all pending events in FIFO order and advances head.
// Single-consumer design (game loop). Checks published flags for safety. The
// slice is the queue's and is overwritten by the next Consume.
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

		result := eq.consumeBuf[:0]
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
			eq.consumeBuf = result
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
// Scheduler calls this under the world lock after the routing verdict is known.
func (eq *EventQueue) RecordDispatch(t EventType, dead bool) {
	if !validType(t) {
		return
	}
	eq.dispatched[t].Add(1)
	if dead {
		eq.deadLetter[t].Add(1)
	}
}

// RecordCrossingApplied tells the current wire sink that the locally published
// copy of one ordinary crossing has run through every handler. A zero sequence is
// not a crossing, and a sink swap between publication and dispatch is harmless:
// one world owns one NetworkSystem for its lifetime.
func (eq *EventQueue) RecordCrossingApplied(sequence uint64) {
	if sequence == 0 {
		return
	}
	if w := eq.wire.Load(); w != nil {
		w.sink.CrossingApplied(sequence)
	}
}

// SnapshotTelemetry copies per-type counters for cadence-bound publication.
func (eq *EventQueue) SnapshotTelemetry(dispatch, dead *[EventTypeCount]int64) {
	for i := 1; i < EventTypeCount; i++ {
		dispatch[i] = eq.dispatched[i].Load()
		dead[i] = eq.deadLetter[i].Load()
	}
}

// QueueTelemetry is the queue's tallies by event name, which a journal's opening
// carries so the run's diagnostics go on from where they stood.
type QueueTelemetry struct {
	Dispatched  map[string]int64 `json:"dispatched,omitempty"`
	Dead        map[string]int64 `json:"dead,omitempty"`
	Overwritten uint64           `json:"overwritten"`
}

// Telemetry reads the tallies. Caller MUST hold the world lock.
func (eq *EventQueue) Telemetry() QueueTelemetry {
	t := QueueTelemetry{Dispatched: map[string]int64{}, Dead: map[string]int64{}, Overwritten: eq.overwritten.Load()}
	for i := 1; i < EventTypeCount; i++ {
		name := GetEventName(EventType(i))
		if n := eq.dispatched[i].Load(); n != 0 {
			t.Dispatched[name] = n
		}
		if n := eq.deadLetter[i].Load(); n != 0 {
			t.Dead[name] = n
		}
	}
	return t
}

// SetTelemetry resumes them; a name this build does not dispatch counts nothing.
// Caller MUST hold the world lock.
func (eq *EventQueue) SetTelemetry(t QueueTelemetry) {
	eq.overwritten.Store(t.Overwritten)
	for i := 1; i < EventTypeCount; i++ {
		name := GetEventName(EventType(i))
		eq.dispatched[i].Store(t.Dispatched[name])
		eq.deadLetter[i].Store(t.Dead[name])
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

// NextRun opens the next run at tick 0, between ticks: no body precedes its first
// tick. Called where the tick counter is re-based, so the two can never disagree.
// Caller MUST hold the world lock.
func (eq *EventQueue) NextRun() uint64 {
	run := eq.stamp.Load().Run + 1
	eq.stamp.Store(&Stamp{Run: run, Boundary: 1})
	return run
}

// BeginTick opens tick t's body, settle group 0, which the body's own settle closes
// with NextBoundary. Caller MUST hold the world lock.
func (eq *EventQueue) BeginTick(tick uint64) {
	eq.stamp.Store(&Stamp{Run: eq.stamp.Load().Run, Tick: tick})
}

// RebaseStamp moves the record position onto an installed world's run and tick.
//
// It is the counterpart of NextRun for a capture rather than a reset: the tick is
// shared identity, and an instance that adopted a world without adopting the
// position it was taken at would stamp its records — and schedule every crossing's
// apply tick — against a tick number the session left behind. Settle group 1,
// because an install lands between two ticks, after the tick's body. Caller MUST
// hold the world lock.
func (eq *EventQueue) RebaseStamp(run, tick uint64) {
	eq.stamp.Store(&Stamp{Run: run, Tick: tick, Boundary: 1})
}

// NextBoundary closes the current settle group; a replay settles the same groups.
// Caller MUST hold the world lock.
func (eq *EventQueue) NextBoundary() {
	s := eq.stamp.Load()
	eq.stamp.Store(&Stamp{Run: s.Run, Tick: s.Tick, Boundary: s.Boundary + 1})
}

// DigestJournal emits a world digest at the current stamp. digest runs only when a
// journal is installed. Caller MUST hold the world lock.
func (eq *EventQueue) DigestJournal(digest func() JournalDigest) {
	if j := eq.journal.Load(); j != nil {
		d, st := digest(), eq.stamp.Load()
		d.Run, d.Tick = st.Run, st.Tick
		j.sink.Digest(d)
	}
}

// AnchorJournal re-emits the anchor at the current stamp; a no-op when journaling is off
func (eq *EventQueue) AnchorJournal(live AnchorLive) {
	if j := eq.journal.Load(); j != nil {
		j.Anchor(*eq.stamp.Load(), live)
	}
}

// QueueCopy is a queue's undispatched and deferred events, slot counter, telemetry
// and stamp, detached from it, for a replay copy restored where another stood.
type QueueCopy struct {
	pending, deferred      []GameEvent
	tail, overwritten      uint64
	dispatched, deadLetter [EventTypeCount]int64
	stamp                  Stamp
}

// CopyOut reads the queue. Caller MUST hold the world lock and no producer may run.
func (eq *EventQueue) CopyOut() QueueCopy {
	c := QueueCopy{tail: eq.tail.Load(), overwritten: eq.overwritten.Load(), stamp: *eq.stamp.Load()}
	for i := eq.head.Load(); i < c.tail; i++ {
		ev := eq.events[i&parameter.EventBufferMask]
		ev.Payload = ClonePayload(ev.Payload)
		c.pending = append(c.pending, ev)
	}
	for _, ev := range eq.deferred {
		ev.Payload = ClonePayload(ev.Payload)
		c.deferred = append(c.deferred, ev)
	}
	eq.SnapshotTelemetry(&c.dispatched, &c.deadLetter)
	return c
}

// CopyIn replaces the queue's contents with a copy; its wire and journal stay its
// own. Caller MUST hold the world lock and no producer may run.
func (eq *EventQueue) CopyIn(c QueueCopy) {
	eq.Consume()
	head := c.tail - uint64(len(c.pending))
	for i, ev := range c.pending {
		idx := (head + uint64(i)) & parameter.EventBufferMask
		ev.Payload = ClonePayload(ev.Payload)
		eq.events[idx] = ev
		eq.published[idx].Store(true)
	}
	eq.head.Store(head)
	eq.tail.Store(c.tail)
	eq.deferred = eq.deferred[:0]
	for _, ev := range c.deferred {
		ev.Payload = ClonePayload(ev.Payload)
		eq.deferred = append(eq.deferred, ev)
	}
	eq.overwritten.Store(c.overwritten)
	for i := 1; i < EventTypeCount; i++ {
		eq.dispatched[i].Store(c.dispatched[i])
		eq.deadLetter[i].Store(c.deadLetter[i])
	}
	st := c.stamp
	eq.stamp.Store(&st)
	if len(c.pending) > 0 {
		eq.signal()
	}
}

// ClonePayload returns a payload sharing no storage with p: a queued one may be
// pooled, and whoever dispatches it recycles it.
func ClonePayload(p any) any {
	if p == nil {
		return nil
	}
	return deepCopy(reflect.ValueOf(p)).Interface()
}

func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		n := reflect.New(v.Type().Elem())
		n.Elem().Set(deepCopy(v.Elem()))
		return n
	case reflect.Struct:
		n := reflect.New(v.Type()).Elem()
		n.Set(v)
		for i := range v.NumField() {
			if n.Field(i).CanSet() {
				n.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
		return n
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		n := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			n.Index(i).Set(deepCopy(v.Index(i)))
		}
		return n
	case reflect.Array:
		n := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			n.Index(i).Set(deepCopy(v.Index(i)))
		}
		return n
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		n := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			n.SetMapIndex(deepCopy(it.Key()), deepCopy(it.Value()))
		}
		return n
	}
	return v
}

package event

import "fmt"

// Handler processes specific event types
// Systems implement this interface to receive routed events
type Handler interface {
	// HandleEvent processes a single event
	// Called synchronously during the dispatch phase
	HandleEvent(event GameEvent)

	// EventTypes returns the event types this handler processes
	// The router uses this for registration
	EventTypes() []EventType
}

// Router dispatches events to registered handlers
//
// Architecture:
//   - Single-threaded dispatch
//   - Multiple handlers can register for the same event type
//   - Handlers are invoked in registration order
type Router struct {
	queue *EventQueue
	// EventType is contiguous in [0, EventTypeCount)
	handlers [EventTypeCount][]Handler
}

// NewRouter creates a router attached to the given queue
func NewRouter(queue *EventQueue) *Router {
	return &Router{queue: queue}
}

// validType reports whether t indexes a real event
// EventNone is excluded: it is the FSM tick sentinel, never a dispatched type
func validType(t EventType) bool {
	return t > EventNone && int(t) < EventTypeCount
}

// Register adds a handler for its declared event types
func (r *Router) Register(handler Handler) {
	for _, t := range handler.EventTypes() {
		if !validType(t) {
			panic(fmt.Sprintf("event: handler %T declared out-of-range type %d", handler, t))
		}
		r.handlers[t] = append(r.handlers[t], handler)
	}
}

// DispatchAll consumes all pending events and routes to handlers
// Event are processed in FIFO order
func (r *Router) DispatchAll() {
	for _, ev := range r.queue.Consume() {
		if !validType(ev.Type) {
			continue
		}
		for _, h := range r.handlers[ev.Type] {
			h.HandleEvent(ev)
		}
	}
}

// HasHandlers returns true if any handlers are registered for the given type
func (r *Router) HasHandlers(t EventType) bool {
	return validType(t) && len(r.handlers[t]) > 0
}

// HandlerCount returns the number of handlers registered for the given type
func (r *Router) HandlerCount(t EventType) int {
	if !validType(t) {
		return 0
	}
	return len(r.handlers[t])
}

// GetHandlers returns the slice of handlers for a specific event type
// Exposed to allow ClockScheduler to manually iterate handlers after FSM processing
func (r *Router) GetHandlers(t EventType) ([]Handler, bool) {
	if !validType(t) {
		return nil, false
	}
	h := r.handlers[t]
	return h, len(h) > 0
}


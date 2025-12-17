package events

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
	handlers map[EventType][]Handler
	queue    *EventQueue
}

// NewRouter creates a router attached to the given queue
func NewRouter(queue *EventQueue) *Router {
	return &Router{
		handlers: make(map[EventType][]Handler),
		queue:    queue,
	}
}

// Register adds a handler for its declared event types
func (r *Router) Register(handler Handler) {
	for _, t := range handler.EventTypes() {
		r.handlers[t] = append(r.handlers[t], handler)
	}
}

// DispatchAll consumes all pending events and routes to handlers
// Events are processed in FIFO order
func (r *Router) DispatchAll() {
	events := r.queue.Consume()
	for _, ev := range events {
		handlers := r.handlers[ev.Type]
		for _, h := range handlers {
			h.HandleEvent(ev)
		}
	}
}

// HasHandlers returns true if any handlers are registered for the given type
func (r *Router) HasHandlers(t EventType) bool {
	return len(r.handlers[t]) > 0
}

// HandlerCount returns the number of handlers registered for the given type
func (r *Router) HandlerCount(t EventType) int {
	return len(r.handlers[t])
}

// GetHandlers returns the slice of handlers for a specific event type
// Exposed to allow ClockScheduler to manually iterate handlers after FSM processing
func (r *Router) GetHandlers(t EventType) ([]Handler, bool) {
	handlers, ok := r.handlers[t]
	return handlers, ok
}
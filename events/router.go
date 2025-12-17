package events

// Handler processes specific event types within a context T
// Systems implement this interface to receive routed events
type Handler[T any] interface {
	// HandleEvent processes a single event
	// Called synchronously during the dispatch phase
	HandleEvent(ctx T, event GameEvent)

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
//   - Context T is passed to handlers (typically *engine.World)
type Router[T any] struct {
	handlers map[EventType][]Handler[T]
	queue    *EventQueue
}

// NewRouter creates a router attached to the given queue
func NewRouter[T any](queue *EventQueue) *Router[T] {
	return &Router[T]{
		handlers: make(map[EventType][]Handler[T]),
		queue:    queue,
	}
}

// Register adds a handler for its declared event types
func (r *Router[T]) Register(handler Handler[T]) {
	for _, t := range handler.EventTypes() {
		r.handlers[t] = append(r.handlers[t], handler)
	}
}

// DispatchAll consumes all pending events and routes to handlers
// Events are processed in FIFO order
func (r *Router[T]) DispatchAll(ctx T) {
	events := r.queue.Consume()
	for _, ev := range events {
		handlers := r.handlers[ev.Type]
		for _, h := range handlers {
			h.HandleEvent(ctx, ev)
		}
	}
}

// HasHandlers returns true if any handlers are registered for the given type
func (r *Router[T]) HasHandlers(t EventType) bool {
	return len(r.handlers[t]) > 0
}

// HandlerCount returns the number of handlers registered for the given type
func (r *Router[T]) HandlerCount(t EventType) int {
	return len(r.handlers[t])
}

// GetHandlers returns the slice of handlers for a specific event type
// Exposed to allow ClockScheduler to manually iterate handlers after FSM processing
func (r *Router[T]) GetHandlers(t EventType) ([]Handler[T], bool) {
	handlers, ok := r.handlers[t]
	return handlers, ok
}
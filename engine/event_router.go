package engine

// EventHandler processes specific event types
// Systems implement this interface to receive routed events
type EventHandler interface {
	// HandleEvent processes a single event
	// Called synchronously during the dispatch phase, before World.Update()
	HandleEvent(world *World, event GameEvent)

	// EventTypes returns the event types this handler processes
	// The router uses this for registration
	EventTypes() []EventType
}

// EventRouter dispatches events to registered handlers
//
// Architecture:
//   - Single-threaded dispatch (no concurrency issues with World mutation)
//   - Multiple handlers can register for the same event type
//   - Handlers are invoked in registration order
//   - All events consumed and dispatched before World.Update() runs
//
// Usage:
//  1. Create router: NewEventRouter(queue)
//  2. Register handlers: router.Register(system)
//  3. Each tick: router.DispatchAll(world) before world.Update()
type EventRouter struct {
	handlers map[EventType][]EventHandler
	queue    *EventQueue
}

// NewEventRouter creates a router attached to the given queue
func NewEventRouter(queue *EventQueue) *EventRouter {
	return &EventRouter{
		handlers: make(map[EventType][]EventHandler),
		queue:    queue,
	}
}

// Register adds a handler for its declared event types
// A handler can register for multiple event types
// Multiple handlers can register for the same event type
func (r *EventRouter) Register(handler EventHandler) {
	for _, t := range handler.EventTypes() {
		r.handlers[t] = append(r.handlers[t], handler)
	}
}

// DispatchAll consumes all pending events and routes to handlers
// Events are processed in FIFO order
// All handlers for an event type are called before moving to the next event
//
// Must be called once per tick, BEFORE World.Update()
func (r *EventRouter) DispatchAll(world *World) {
	events := r.queue.Consume()
	for _, ev := range events {
		handlers := r.handlers[ev.Type]
		for _, h := range handlers {
			h.HandleEvent(world, ev)
		}
	}
}

// HasHandlers returns true if any handlers are registered for the given type
// Useful for debugging and testing
func (r *EventRouter) HasHandlers(t EventType) bool {
	return len(r.handlers[t]) > 0
}

// HandlerCount returns the number of handlers registered for the given type
func (r *EventRouter) HandlerCount(t EventType) int {
	return len(r.handlers[t])
}
package event

import (
	"strings"
	"sync"
)

var (
	nameToType   = make(map[string]EventType)
	typeToName   = make(map[EventType]string)
	registryInit = false
)

// RegisterType maps a string name to an EventType; its payload, if any, is the
// generated table's (payload_gen.go). Called only from generated code.
func RegisterType(name string, et EventType) {
	if _, dup := nameToType[name]; dup {
		panic("event: duplicate registration for " + name)
	}
	nameToType[name] = et
	// First registration wins, so an alias cannot displace the canonical name in the reverse map
	if _, exists := typeToName[et]; !exists {
		typeToName[et] = name
	}
}

// GetEventType returns the EventType for a given name
func GetEventType(name string) (EventType, bool) {
	// EventNone is special case for FSM "Tick"
	if strings.EqualFold(name, "Tick") {
		return EventNone, true
	}
	et, ok := nameToType[name]
	return et, ok
}

// GetEventName returns the string name for an EventType
func GetEventName(et EventType) string {
	if et == EventNone {
		return "Tick"
	}
	return typeToName[et]
}

// RangeEvents iterates registered events; payload is NewPayloadStruct(et) (nil for payload-less events)
func RangeEvents(fn func(name string, et EventType, payload any)) {
	for name, et := range nameToType {
		fn(name, et, NewPayloadStruct(et))
	}
}

// NewPayloadStruct returns a new pointer to a zero-value payload struct for the
// event type, nil for an event with no payload or none in the registry's range.
func NewPayloadStruct(et EventType) any {
	if et < 0 || et >= EventTypeCount || payloadNew[et] == nil {
		return nil
	}
	return payloadNew[et]()
}

// EnsureRegistry initialises the event registry exactly once, safe against
// concurrent App construction. InitRegistry is generated and carries no guard.
func EnsureRegistry() { registryOnce.Do(InitRegistry) }

var registryOnce sync.Once

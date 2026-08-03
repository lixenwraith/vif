package event

import (
	"reflect"
	"strings"
)

var (
	nameToType    = make(map[string]EventType)
	typeToName    = make(map[EventType]string)
	typeToPayload = make(map[EventType]reflect.Type)
	registryInit  = false
)

// RegisterType maps a string name to an EventType and its payload struct type
// payloadInstance should be a pointer to the payload struct (e.g., &SpawnChangePayload{})
// Pass nil if the event has no payload
// Called only from generated code and registerAliases
func RegisterType(name string, et EventType, payloadInstance any) {
	// Don't allow duplicate events
	if _, dup := nameToType[name]; dup {
		panic("event: duplicate registration for " + name)
	}
	nameToType[name] = et

	// First registration wins, so an alias cannot displace the canonical name in the reverse map
	if _, exists := typeToName[et]; !exists {
		typeToName[et] = name
	}

	if payloadInstance != nil {
		t := reflect.TypeOf(payloadInstance)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		typeToPayload[et] = t
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

// NewPayloadStruct returns a new pointer to a zero-value payload struct for the event type
// Returns nil if no payload is registered
func NewPayloadStruct(et EventType) any {
	t, ok := typeToPayload[et]
	if !ok {
		return nil
	}
	return reflect.New(t).Interface()
}

package status

import (
	"sync/atomic"
)

// MaxStringLen is the display width fixed-layout consumers should assume.
// The store is unbounded; truncation is a rendering concern, not a storage one.
const MaxStringLen = 20

// AtomicString provides atomic string access
// Zero value is ready to use (represents empty string)
type AtomicString struct {
	ptr atomic.Pointer[string]
}

// Store sets the string value
func (s *AtomicString) Store(val string) {
	s.ptr.Store(&val)
}

// StoreIfChanged stores val only when it differs from the current value.
// Store's parameter escapes, so a caller that writes the same value every tick
// allocates a header per call; FSM state names are stable for many ticks.
func (s *AtomicString) StoreIfChanged(val string) {
	if p := s.ptr.Load(); p != nil && *p == val {
		return
	}
	s.ptr.Store(&val)
}

// Load returns the current string value
func (s *AtomicString) Load() string {
	if p := s.ptr.Load(); p != nil {
		return *p
	}
	return ""
}

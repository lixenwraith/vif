package status
// @lixen: #dev{feature[dust(render,system)]}

import (
	"sync/atomic"
)

// MaxStringLen is the maximum length for atomic strings
const MaxStringLen = 20

// AtomicString provides atomic string access with fixed max length
// Zero value is ready to use (represents empty string)
type AtomicString struct {
	ptr atomic.Pointer[string]
}

// Store sets the string value, truncating to MaxStringLen
func (s *AtomicString) Store(val string) {
	if len(val) > MaxStringLen {
		val = val[:MaxStringLen]
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
package status

import (
	"math"
	"sync/atomic"
)

// AtomicFloat provides atomic float64 operations using bit conversion
// Zero value is ready to use (represents 0.0)
type AtomicFloat struct {
	bits atomic.Uint64
}

// Set stores a float64 value atomically
func (f *AtomicFloat) Set(val float64) {
	f.bits.Store(math.Float64bits(val))
}

// Get loads the float64 value atomically
func (f *AtomicFloat) Get() float64 {
	return math.Float64frombits(f.bits.Load())
}

// Add atomically adds delta to the current value and returns the new value
func (f *AtomicFloat) Add(delta float64) float64 {
	for {
		old := f.bits.Load()
		newVal := math.Float64frombits(old) + delta
		if f.bits.CompareAndSwap(old, math.Float64bits(newVal)) {
			return newVal
		}
	}
}
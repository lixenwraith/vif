//go:build !wasm

package engine

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/vlog"
)

// // UpdateMutex wraps sync.Mutex for game tick serialization
// type UpdateMutex struct {
// 	mu sync.Mutex
// }
//
// func (m *UpdateMutex) Lock()         { m.mu.Lock() }
// func (m *UpdateMutex) Unlock()       { m.mu.Unlock() }
// func (m *UpdateMutex) TryLock() bool { return m.mu.TryLock() }

// ===

// LockHoldWarn is the update-mutex hold time above which a hold is reported
const LockHoldWarn = 20 * time.Millisecond

// UpdateMutex wraps sync.Mutex for game tick serialization.
// Hold time is sampled only while debug logging is active.
type UpdateMutex struct {
	acquired time.Time // holder-exclusive between Lock and Unlock
	mu       sync.Mutex
}

func (m *UpdateMutex) Lock() {
	m.mu.Lock()
	m.mark()
}

func (m *UpdateMutex) TryLock() bool {
	if !m.mu.TryLock() {
		return false
	}
	m.mark()
	return true
}

func (m *UpdateMutex) Unlock() {
	m.report()
	m.mu.Unlock()
}

// mark stamps the acquisition when sampling is active
func (m *UpdateMutex) mark() {
	if vlog.On("lock", vlog.LevelDebug) {
		m.acquired = time.Now()
		return
	}
	m.acquired = time.Time{}
}

// report emits holds exceeding LockHoldWarn, with the holder's call chain
func (m *UpdateMutex) report() {
	if m.acquired.IsZero() {
		return
	}
	if held := time.Since(m.acquired); held > LockHoldWarn {
		vlog.Trace("lock", vlog.LevelWarn, 4, "msg", "long hold", "us", held.Microseconds())
	}
	m.acquired = time.Time{}
}

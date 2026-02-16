//go:build unix

package engine

import "sync"

// WorldMutex wraps sync.RWMutex for entity/system registration
type WorldMutex struct {
	mu sync.RWMutex
}

func (m *WorldMutex) Lock()    { m.mu.Lock() }
func (m *WorldMutex) Unlock()  { m.mu.Unlock() }
func (m *WorldMutex) RLock()   { m.mu.RLock() }
func (m *WorldMutex) RUnlock() { m.mu.RUnlock() }

// UpdateMutex wraps sync.Mutex for game tick serialization
type UpdateMutex struct {
	mu sync.Mutex
}

func (m *UpdateMutex) Lock()         { m.mu.Lock() }
func (m *UpdateMutex) Unlock()       { m.mu.Unlock() }
func (m *UpdateMutex) TryLock() bool { return m.mu.TryLock() }
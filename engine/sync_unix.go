//go:build unix

package engine

import "sync"

// WorldMutex is real mutex on Unix
type WorldMutex struct {
	mu sync.RWMutex
}

func (m *WorldMutex) Lock()         { m.mu.Lock() }
func (m *WorldMutex) Unlock()       { m.mu.Unlock() }
func (m *WorldMutex) RLock()        { m.mu.RLock() }
func (m *WorldMutex) RUnlock()      { m.mu.RUnlock() }
func (m *WorldMutex) TryLock() bool { return m.mu.TryLock() }
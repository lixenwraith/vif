//go:build wasm

package engine

// WorldMutex is no-op on WASM (single-threaded)
type WorldMutex struct{}

func (m *WorldMutex) Lock()         {}
func (m *WorldMutex) Unlock()       {}
func (m *WorldMutex) RLock()        {}
func (m *WorldMutex) RUnlock()      {}
func (m *WorldMutex) TryLock() bool { return true }
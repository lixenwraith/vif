//go:build wasm

package engine

// WorldMutex is no-op on WASM (single-threaded)
type WorldMutex struct{}

func (m *WorldMutex) Lock()    {}
func (m *WorldMutex) Unlock()  {}
func (m *WorldMutex) RLock()   {}
func (m *WorldMutex) RUnlock() {}

// UpdateMutex is no-op on WASM (single-threaded)
type UpdateMutex struct{}

func (m *UpdateMutex) Lock()         {}
func (m *UpdateMutex) Unlock()       {}
func (m *UpdateMutex) TryLock() bool { return true }
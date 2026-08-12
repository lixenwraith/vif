//go:build wasm

package engine

// UpdateMutex is no-op on WASM (single-threaded)
type UpdateMutex struct{}

func (m *UpdateMutex) Lock()         {}
func (m *UpdateMutex) Unlock()       {}
func (m *UpdateMutex) TryLock() bool { return true }
func SetLockSampling(on bool)        {}

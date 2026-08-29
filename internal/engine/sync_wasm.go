//go:build wasm

package engine

import "github.com/lixenwraith/vi-fighter/internal/status"

// UpdateMutex is no-op on WASM (single-threaded)
type UpdateMutex struct{}

func (m *UpdateMutex) Lock()                       {}
func (m *UpdateMutex) Unlock()                     {}
func (m *UpdateMutex) TryLock() bool               { return true }
func (m *UpdateMutex) BindStatus(*status.Registry) {}
func (m *UpdateMutex) SetSampling(bool)            {}

//go:build wasm

package app

const buildHasSocketNetwork = false

func (a *App) initNetworkService() error { return nil }

func (a *App) bindSessionController() {}

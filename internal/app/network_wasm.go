//go:build wasm

package app

import "github.com/lixenwraith/vi-fighter/internal/service"

const buildHasSocketNetwork = false

// A browser run registers the transport only for a join, and takes the one the
// handshake already built. It binds nothing, so there is no listener to roll back
// and no mid-run host for the session controller to open.
func (a *App) initNetworkService() error {
	if a.cfg.JoinAddress == "" {
		return nil
	}
	a.networkSvc = service.NewNetworkService(a.cfg.networkConfig)
	return a.hub.Register(a.networkSvc)
}

func (a *App) bindSessionController() {}

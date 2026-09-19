//go:build !wasm

package app

import "github.com/lixenwraith/vi-fighter/internal/service"

const buildHasSocketNetwork = true

func (a *App) initNetworkService() error {
	if a.cfg.Mode != ModePlay && a.cfg.HostAddress == "" && a.cfg.JoinAddress == "" {
		return nil
	}
	a.networkSvc = service.NewNetworkService(a.cfg.networkConfig)
	return a.hub.Register(a.networkSvc)
}

func (a *App) bindSessionController() { a.ctx.SessionCtl = sessionControl{a} }

//go:build !wasm

package app

import "github.com/lixenwraith/vif/internal/service"

const buildHasSocketNetwork = true

// pageHidden is false outside a browser: a terminal is drawn whether seen or not.
func pageHidden() bool { return false }

func (a *App) initNetworkService() error {
	if a.cfg.Mode != ModePlay && a.cfg.Membership.HostAddress == "" && a.cfg.Membership.JoinAddress == "" {
		return nil
	}
	a.networkSvc = service.NewNetworkService(a.cfg.networkConfig)
	a.membership.networkSvc = a.networkSvc
	return a.hub.Register(a.networkSvc)
}

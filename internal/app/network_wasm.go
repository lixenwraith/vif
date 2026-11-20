//go:build wasm

package app

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/service"
)

// pageHidden reports a page nobody sees: a hidden tab's, or one behind its start screen
func pageHidden() bool { return terminal.PageHidden() }

// A browser run registers the transport only for a join, and takes the one the
// handshake already built. It binds nothing, so there is no listener to roll back.
func (a *App) initNetworkService() error {
	if a.cfg.Membership.JoinAddress == "" {
		return nil
	}
	a.networkSvc = service.NewNetworkService(a.cfg.wiring.networkConfig)
	a.membership.SetNetworkService(a.networkSvc)
	return a.hub.Register(a.networkSvc)
}

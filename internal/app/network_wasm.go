//go:build wasm

package app

import (
	"syscall/js"

	"github.com/lixenwraith/vif/internal/service"
)

const buildHasSocketNetwork = false

// pageHidden reports the page's visibility, which web/worker.js keeps in vifHidden.
func pageHidden() bool { return js.Global().Get("vifHidden").Truthy() }

// A browser run registers the transport only for a join, and takes the one the
// handshake already built. It binds nothing, so there is no listener to roll back.
func (a *App) initNetworkService() error {
	if a.cfg.JoinAddress == "" {
		return nil
	}
	a.networkSvc = service.NewNetworkService(a.cfg.networkConfig)
	return a.hub.Register(a.networkSvc)
}

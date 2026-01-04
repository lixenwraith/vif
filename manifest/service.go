package manifest

import (
	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/network"
	"github.com/lixenwraith/vi-fighter/registry"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// RegisterServices registers all service factories
// Terminal is excluded - it's a bootstrap service registered directly
func RegisterServices() {
	registry.RegisterService("terminal", func() any {
		return terminal.NewService()
	})

	registry.RegisterService("content", func() any {
		return content.NewService()
	})

	registry.RegisterService("audio", func() any {
		return audio.NewService()
	})

	registry.RegisterService("network", func() any {
		return network.NewService()
	})
}

// ActiveServices returns the ordered list of services to instantiate
// Terminal is excluded - handled separately as bootstrap
func ActiveServices() []string {
	return []string{
		"terminal",
		"content",
		"audio",
		"network",
	}
}
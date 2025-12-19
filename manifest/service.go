package manifest

import (
	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/engine/registry"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// RegisterServices registers all service factories
// Terminal is excluded - it's a bootstrap service registered directly
func RegisterServices() {
	registry.RegisterService("terminal", func() any {
		return terminal.NewService()
	})

	registry.RegisterService("audio", func() any {
		return audio.NewService()
	})

	registry.RegisterService("content", func() any {
		return content.NewService()
	})
}

// ActiveServices returns the ordered list of services to instantiate
// Terminal is excluded - handled separately as bootstrap
func ActiveServices() []string {
	return []string{
		"terminal",
		"audio",
		"content",
	}
}
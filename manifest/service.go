package manifest

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/network"
	"github.com/lixenwraith/vi-fighter/service"
)

// BuildServices constructs every active service
func BuildServices() []service.Service {
	return []service.Service{
		terminal.NewService(),
		content.NewService(),
		audio.NewService(),
		network.NewService(), // Placeholder: disabled unless a Role is configured
	}
}

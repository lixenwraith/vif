package manifest

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// profileWidth and profileHeight size the scratch world; the geometry never
// reaches a declaration, it only has to leave a viewport
const (
	profileWidth  = 80
	profileHeight = 24
)

// SystemProfile is one active system's declared identity
type SystemProfile struct {
	Name     string
	Domain   engine.SystemDomain
	Requires engine.SystemDependencies
}

// SystemProfiles returns what every active system declares, by constructing the
// set on a scratch world and reading it back. Config validation needs the
// declarations before a run exists; the systems are discarded on return.
func SystemProfiles() []SystemProfile {
	event.EnsureRegistry()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, profileWidth, profileHeight, engine.NewManualClock())

	built := BuildSystems(w)
	profiles := make([]SystemProfile, 0, len(built))
	for _, s := range built {
		profiles = append(profiles, SystemProfile{
			Name:     s.Name(),
			Domain:   s.Domain(),
			Requires: s.Requires(),
		})
	}
	return profiles
}

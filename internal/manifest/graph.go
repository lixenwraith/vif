package manifest

import "github.com/lixenwraith/vi-fighter/internal/engine"

// ProfileFor returns a system's declared profile. An unknown name is a wiring
// regression, not a runtime condition.
func ProfileFor(name string) engine.SystemProfile {
	p, ok := systemProfiles[name]
	if !ok {
		panic("manifest: system " + name + " is not declared")
	}
	return p
}

// SystemProfile is one active system's declared identity
type SystemProfile struct {
	Name     string
	Domain   engine.SystemDomain
	Requires engine.SystemDependencies
}

// SystemProfiles returns every manifest system's profile in manifest order.
// Context-scoped systems are excluded from FSM system-name validation.
func SystemProfiles() []SystemProfile {
	profiles := make([]SystemProfile, 0, len(Systems))
	for _, def := range Systems {
		p := ProfileFor(def.Name)
		profiles = append(profiles, SystemProfile{
			Name:     def.Name,
			Domain:   p.Domain,
			Requires: p.Requires,
		})
	}
	return profiles
}

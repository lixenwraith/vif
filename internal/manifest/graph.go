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

// SnapshotFor returns a system's declared D-19 obligation. Panics on an
// undeclared name for the same reason ProfileFor does: a system reaching the
// registry without a manifest entry is a generator bug, not a runtime condition.
func SnapshotFor(name string) engine.SnapshotProfile {
	p, ok := systemSnapshots[name]
	if !ok {
		panic("manifest: system " + name + " is not declared")
	}
	return p
}

// SnapshotDeclarations returns every system's declared obligation, for the
// boundary suite that asserts each against the code.
func SnapshotDeclarations() map[string]engine.SnapshotProfile {
	out := make(map[string]engine.SnapshotProfile, len(systemSnapshots))
	for name, p := range systemSnapshots {
		out[name] = p
	}
	return out
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

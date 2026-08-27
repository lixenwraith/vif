package engine

// Entity is defined in core package to avoid cyclic dependency

// Components are handled in Store

// SystemDomain classifies which domains a system reads and writes.
// It is declared, not inferred; internal/system/domain_test.go checks each
// declaration against the RNG streams, entity domains and component stores the
// system actually touches.
type SystemDomain uint8

const (
	// SystemShared reads and writes shared state only, so every write is
	// re-derived identically on every instance
	SystemShared SystemDomain = iota
	// SystemPlayer reads shared state and writes this instance's participant:
	// player-domain entities, and the owner-authored cursor components of D-13
	SystemPlayer
	// SystemDual resolves both domains, by ambient stamping (D-7) or by claimed
	// geometry (D-12)
	SystemDual
)

// systemDomainNames indexes SystemDomain for diagnostics
var systemDomainNames = [...]string{"shared", "player", "dual"}

// String returns the profile name, or "?" when out of range
func (d SystemDomain) String() string {
	if int(d) >= len(systemDomainNames) {
		return "?"
	}
	return systemDomainNames[d]
}

// DependencyStrength grades what a missing dependency costs its dependent
type DependencyStrength uint8

const (
	// DepRequired marks a dependency the dependent cannot function without.
	// Disabling it is refused and reported.
	DepRequired DependencyStrength = iota
	// DepOptional marks a dependency whose absence only degrades the dependent.
	// Disabling it is legal and reported once.
	DepOptional
)

// SystemDependency names a system another system needs, and how badly.
// Dependencies order initialization and gate the enable and disable commands.
// Tick order is Priority()'s business; the two often agree but are not the same
// relation and neither may be derived from the other.
type SystemDependency struct {
	Name     string
	Strength DependencyStrength
}

// SystemDependencies is one system's declared dependency set
type SystemDependencies []SystemDependency

// Require names systems the caller cannot function without
func Require(names ...string) SystemDependencies { return dependencies(names, DepRequired) }

// Optional names systems whose absence only degrades the caller
func Optional(names ...string) SystemDependencies { return dependencies(names, DepOptional) }

// dependencies pairs each name with one strength
func dependencies(names []string, strength DependencyStrength) SystemDependencies {
	deps := make(SystemDependencies, len(names))
	for i, n := range names {
		deps[i] = SystemDependency{Name: n, Strength: strength}
	}
	return deps
}

// System is an interface that all systems must implement
type System interface {
	Init()
	Priority() int // Lower values run first
	Name() string
	// Domain declares which domains the system reads and writes
	Domain() SystemDomain
	// Requires declares the systems this one needs, and how badly
	Requires() SystemDependencies
	Update()
}

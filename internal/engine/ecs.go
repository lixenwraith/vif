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

// systemDomainNames indexes SystemDomain for diagnostics and generated tables
var systemDomainNames = [...]string{"shared", "player", "dual"}

// String returns the profile name, or "?" when out of range
func (d SystemDomain) String() string {
	if int(d) >= len(systemDomainNames) {
		return "?"
	}
	return systemDomainNames[d]
}

// ParseSystemDomain resolves a profile name; the inverse of SystemDomain.String
func ParseSystemDomain(s string) (SystemDomain, bool) {
	for i, n := range systemDomainNames {
		if n == s {
			return SystemDomain(i), true
		}
	}
	return 0, false
}

// System is an interface that all systems must implement
type System interface {
	Init()
	Priority() int // Lower values run first
	Name() string
	// Domain declares which domains the system reads and writes
	Domain() SystemDomain
	Update()
}

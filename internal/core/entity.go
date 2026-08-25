package core

// Entity is a unique identifier for an entity.
// Layout: [domain:8][id:56]. ID 0 is never issued, so a zero Entity means "none".
type Entity uint64

// Domain identifies replication scope: shared state is identical on every
// instance, player state is local and never replicated.
type Domain uint8

const (
	DomainShared Domain = iota
	DomainPlayer
	DomainCount
)

const (
	// entityIDBits is the width of the id field; the domain tag occupies the rest
	entityIDBits = 56

	// EntityIDMax is the largest id one domain can issue
	EntityIDMax = uint64(1)<<entityIDBits - 1
)

// MakeEntity packs a domain and id; id is truncated to 56 bits
func MakeEntity(d Domain, id uint64) Entity {
	return Entity(uint64(d)<<entityIDBits | id&EntityIDMax)
}

// Domain returns the entity's replication scope
func (e Entity) Domain() Domain { return Domain(uint64(e) >> entityIDBits) }

// ID returns the domain-local identifier
func (e Entity) ID() uint64 { return uint64(e) & EntityIDMax }

// Valid reports whether the entity names a real entity
func (e Entity) Valid() bool { return e.ID() != 0 }

// DomainNames indexes Domain for seed derivation, telemetry keys and log fields.
// These strings are part of the RNG derivation input; changing one re-keys every
// stream in that domain.
var DomainNames = [DomainCount]string{
	DomainShared: "shared",
	DomainPlayer: "player",
}

// String returns the domain name, or "?" when out of range
func (d Domain) String() string {
	if int(d) >= len(DomainNames) {
		return "?"
	}
	return DomainNames[d]
}

// ParseDomain resolves a domain name; the inverse of Domain.String
func ParseDomain(s string) (Domain, bool) {
	for i, n := range DomainNames {
		if n == s {
			return Domain(i), true
		}
	}
	return 0, false
}

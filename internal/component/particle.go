package component

// ParticleBehavior selects the gameplay rules applied by ParticleSystem.
// Keep zero invalid so an omitted event discriminator cannot silently create
// the wrong particle behavior.
type ParticleBehavior uint8

const (
	ParticleNone ParticleBehavior = iota
	ParticleDecay
	ParticleBlossom
	ParticleDust
	ParticleBehaviorCount
)

// Valid reports whether the behavior is implemented by ParticleSystem.
func (b ParticleBehavior) Valid() bool {
	return b > ParticleNone && b < ParticleBehaviorCount
}

// String returns the stable behavior name used for telemetry.
func (b ParticleBehavior) String() string {
	switch b {
	case ParticleDecay:
		return "decay"
	case ParticleBlossom:
		return "blossom"
	case ParticleDust:
		return "dust"
	default:
		return "particle"
	}
}

// ParticleComponent marks a non-typeable moving glyph whose gameplay is
// selected by Behavior. Kinetic owns its sub-cell motion, Sigil its render
// state and Timer its lifetime, when the behavior has one.
type ParticleComponent struct {
	LastIntX int
	LastIntY int
	Rune     rune
	Behavior ParticleBehavior

	// Orbiting holds while the behavior's anchor pulls the particle
	Orbiting      bool
	OrbitRadius   float64 // circular-space cells
	ChaseBoost    float64 // stiffness multiplier after a cursor jump; 1 is none
	ResponseGroup uint8   // chase stagger group, 0-2
}

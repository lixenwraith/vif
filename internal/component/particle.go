package component

// ParticleBehavior selects the gameplay rules applied by ParticleSystem; zero is invalid
type ParticleBehavior uint8

const (
	ParticleNone ParticleBehavior = iota
	ParticleDecay
	ParticleBlossom
	ParticleDust
	ParticleBehaviorCount
)

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

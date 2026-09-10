package component

// ParticleBehavior selects the gameplay rules applied by ParticleSystem.
// Keep zero invalid so an omitted event discriminator cannot silently create
// the wrong particle behavior.
type ParticleBehavior uint8

const (
	ParticleNone ParticleBehavior = iota
	ParticleDecay
	ParticleBlossom
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
	default:
		return "particle"
	}
}

// ParticleComponent marks a non-typeable moving glyph whose gameplay is
// selected by Behavior. Kinetic owns its sub-cell motion and Sigil owns render
// state; this component retains only particle identity and cell-entry state.
type ParticleComponent struct {
	LastIntX int
	LastIntY int
	Rune     rune
	Behavior ParticleBehavior
}

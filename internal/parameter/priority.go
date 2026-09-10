package parameter

// System execution priorities are a contiguous, deterministic order; lower runs
// first. Inline comments call out the non-obvious producer/consumer constraints.
const (
	PriorityNetwork int = iota // Before every consumer: a peer's crossing must be queued for this tick's settle
	PriorityCursor
	PriorityCamera
	PriorityShield
	PriorityHeat
	PriorityEnergy
	PriorityBoost
	PriorityWeapon
	PriorityTyping    // After boost, before composite
	PriorityComposite // After boost, before spawning systems (position sync)
	PriorityWall      // After composite
	PriorityGlyph
	PriorityNugget
	PriorityGold
	PriorityCleaner
	PriorityDust       // Before Explosion
	PriorityExplosion  // After Dust
	PriorityFuse       // After Cleaner, before Drain
	PrioritySpirit     // After Fuse, before Drain
	PriorityNavigation // Before systems that move entities
	PrioritySoftCollision
	PriorityEnvironment // Applies external forces before every species integrator
	PriorityDrain
	PriorityMaterialize // After Drain
	PriorityQuasar      // After Drain
	PrioritySnake       // After Quasar
	PrioritySwarm       // After Drain
	PriorityStorm       // After Swarm
	PriorityPylon       // After Storm
	PriorityTower       // Before Eye

	PriorityGateway // After Tower, before Eye — spawns eyes for the tick
	PriorityEye     // After Gateway
	PriorityCombat
	PriorityLoot // After species entities and combat
	PriorityParticle
	PriorityLightning // After Quasar
	PriorityMissile   // After Weapon
	PriorityFlash
	PriorityFadeout
	PriorityUI
	PriorityEffect
	PriorityMarker       // Before splash, after game logic
	PrioritySplash       // After game logic, before rendering
	PriorityMotionMarker // After game logic and splash, before rendering
	PriorityDeath        // After game logic, before TimeKeeper
	PriorityTimekeeper   // After game logic
	PriorityAdaptation   // Before genetic
	PriorityGenetic      // After death and timer, observes entity lifecycle
	PriorityDiagnostics  // After all others, telemetry collection
)

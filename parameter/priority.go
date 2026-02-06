package parameter

// TODO: review and reorder, use iota? add comment for all
// System Execution Priorities (lower runs first)
const (
	PriorityShield int = iota
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
	PriorityFuse   // After Cleaner, before Drain
	PrioritySpirit // After Fuse, before Drain
	PriorityDrain
	PriorityMaterialize // After Drain
	PriorityQuasar      // After Drain
	PriorityExplosion   // After Quasar, before Dust
	PriorityDust        // After Quasar, before Decay
	PriorityStorm       // After Drain, before Swarm and Lightning
	PrioritySwarm
	PriorityCombat
	PriorityLoot // After enemy entities and combat
	PriorityDecay
	PriorityBlossom
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
	PriorityGenetic      // After death and timekeeper, observes entity lifecycle
	PriorityDiagnostics  // After all others, telemetry collection
)
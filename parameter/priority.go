package parameter

// TODO: review and reorder, use iota? add comment for all
// System Execution Priorities (lower runs first)
const (
	PriorityShield       = 10
	PriorityHeat         = 20
	PriorityEnergy       = 30
	PriorityBoost        = 40
	PriorityBuff         = 50
	PriorityTyping       = 60 // After boost, before composite
	PriorityComposite    = 70 // After boost, before spawning systems (position sync)
	PriorityWall         = 80 // After composite
	PriorityGlyph        = 90
	PriorityNugget       = 100
	PriorityGold         = 110
	PriorityCleaner      = 120
	PriorityFuse         = 130 // After Cleaner, before Drain
	PrioritySpirit       = 140 // After Fuse, before Drain
	PriorityDrain        = 150
	PriorityMaterialize  = 160 // After Drain
	PriorityQuasar       = 170 // After Drain
	PriorityExplosion    = 180 // After Quasar, before Dust
	PriorityDust         = 190 // After Quasar, before Decay
	PriorityStorm        = 200 // After Drain, before Swarm and Lightning
	PrioritySwarm        = 210
	PriorityCombat       = 220
	PriorityDecay        = 230
	PriorityBlossom      = 240
	PriorityLightning    = 250 // After Quasar
	PriorityMissile      = 260 // After Buff
	PriorityFlash        = 270
	PriorityUI           = 280
	PriorityEffect       = 500
	PriorityMarker       = 510  // Before splash, after game logic
	PrioritySplash       = 800  // After game logic, before rendering
	PriorityMotionMarker = 810  // After game logic and splash, before rendering
	PriorityDeath        = 850  // After game logic, before TimeKeeper
	PriorityTimekeeper   = 900  // After game logic
	PriorityGenetic      = 950  // After death and timekeeper, observes entity lifecycle
	PriorityDiagnostics  = 1000 // After all others, telemetry collection
)
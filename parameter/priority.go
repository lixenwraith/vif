package parameter

// TODO: review and reorder
// System Execution Priorities (lower runs first)
const (
	PriorityShield       = 10
	PriorityHeat         = 20
	PriorityEnergy       = 30
	PriorityBoost        = 40
	PriorityBuff         = 45
	PriorityTyping       = 50 // After boost, before composite
	PriorityComposite    = 60 // After boost, before spawn (position sync)
	PriorityGlyph        = 70
	PriorityNugget       = 80
	PriorityGold         = 90
	PriorityCleaner      = 100
	PriorityVampire      = 110 // After Cleaner, before Drain, Quasar, Swarm, Lightning
	PriorityFuse         = 110 // After Cleaner, before Drain
	PrioritySpirit       = 120 // After Fuse, before Drain
	PriorityDrain        = 130
	PriorityMaterialize  = 140 // PriorityDrain + 1
	PriorityQuasar       = 150 // After Drain
	PriorityExplosion    = 160 // After Quasar, before Dust
	PriorityDust         = 170 // After Quasar, before Decay
	PriorityStorm        = 180 // After Drain, before Swarm and Lightning
	PrioritySwarm        = 190
	PriorityCombat       = 195
	PriorityDecay        = 200
	PriorityBlossom      = 210
	PriorityLightning    = 220 // After Quasar
	PriorityFlash        = 230
	PriorityUI           = 240
	PriorityEffect       = 500
	PriorityMarker       = 510  // Before splash, after game logic
	PrioritySplash       = 800  // After game logic, before rendering
	PriorityMotionMarker = 810  // After game logic and splash, before rendering
	PriorityDeath        = 850  // After game logic, before TimeKeeper
	PriorityTimekeeper   = 900  // After game logic
	PriorityGenetic      = 950  // After death and timekeeper, observes entity lifecycle
	PriorityDiagnostics  = 1000 // After all others, telemetry collection
)
package constant

// TODO: review and reorder
// System Execution Priorities (lower runs first)
const (
	PriorityShield      = 10
	PriorityHeat        = 20
	PriorityEnergy      = 30
	PriorityBoost       = 40
	PriorityTyping      = 50 // After boost, before composite
	PriorityComposite   = 60 // After boost, before spawn (position sync)
	PrioritySpawn       = 70
	PriorityNugget      = 80
	PriorityGold        = 90
	PriorityCleaner     = 100
	PriorityFuse        = 110 // After Cleaner, before Drain
	PrioritySpirit      = 120 // After Fuse, before Drain
	PriorityDrain       = 130
	PriorityMaterialize = 140 // PriorityDrain + 1
	PriorityQuasar      = 150 // After Drain
	PriorityExplosion   = 160 // After Quasar, before Dust
	PriorityDust        = 170 // After Quasar, before Decay
	PrioritySwarm       = 180
	PriorityDecay       = 190
	PriorityBlossom     = 200
	PriorityLightning   = 210 // After Quasar
	PriorityFlash       = 220
	PriorityUI          = 230
	PriorityEffect      = 500
	PrioritySplash      = 800 // After game logic, before rendering
	PriorityDeath       = 850 // After game logic, before TimeKeeper
	PriorityTimekeeper  = 900 // After game logic, final
	PriorityDiagnostics = 950 // After TimeKeeper, telemetry collection
)
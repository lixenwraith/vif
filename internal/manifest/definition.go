package manifest

// ComponentDef defines a component for registration, store generation, and the
// domain audit. Domain is "shared", "player", or "" when the bit attaches in either.
type ComponentDef struct {
	Field  string // ComponentStore field name (e.g., "Drain")
	Type   string // Type name without package (e.g., "DrainComponent")
	Domain string // Entity domain the bit may attach to; "" = either
}

// SystemDef defines a system for registration
// Order in slice determines ActiveSystems() order
type SystemDef struct {
	Name        string // Registry key (e.g., "drain")
	Constructor string // Constructor name without package (e.g., "NewDrainSystem")
	Domain      string // Domain profile: "shared", "player" or "dual"
}

// RendererDef defines a renderer for registration
// Order in slice determines ActiveRenderers() order
type RendererDef struct {
	Name        string // Registry key (e.g., "drain")
	Constructor string // Constructor name without package (e.g., "NewDrainRenderer")
	Priority    string // Priority constant without package (e.g., "PriorityDrain")
}

// Components is the authoritative component list
// Generator produces: ComponentStore, GetComponentStore(), RegisterComponents(),
// and the componentDomains audit table
var Components = []ComponentDef{
	// --- Core Gameplay ---
	// Glyph is player except gold sequence members, which are shared composite members
	{"Glyph", "GlyphComponent", ""},
	{"Sigil", "SigilComponent", ""},
	{"Nugget", "NuggetComponent", "shared"},
	{"Cursor", "CursorComponent", "shared"},
	{"Protection", "ProtectionComponent", ""},
	{"Kinetic", "KineticComponent", ""},
	{"Wall", "WallComponent", "shared"},
	{"Loot", "LootComponent", "player"},
	{"Gateway", "GatewayComponent", "shared"},

	// --- Player State ---
	{"Energy", "EnergyComponent", "shared"},
	{"Heat", "HeatComponent", "shared"},
	// Shield is cursor and quasar state, and the loot pickup glow
	{"Shield", "ShieldComponent", ""},
	{"Boost", "BoostComponent", "shared"},
	{"Weapon", "WeaponComponent", "shared"},
	{"Orb", "OrbComponent", "player"},
	{"Ping", "PingComponent", "shared"},
	{"CursorView", "CursorViewComponent", "shared"},

	// --- Entity Behaviors ---
	{"Decay", "DecayComponent", "player"},
	{"Blossom", "BlossomComponent", "player"},
	{"Cleaner", "CleanerComponent", ""},
	{"Dust", "DustComponent", "player"},
	{"Navigation", "NavigationComponent", ""},
	{"Combat", "CombatComponent", ""},
	{"Genotype", "GenotypeComponent", "shared"},
	{"Lightning", "LightningComponent", "player"},
	{"Missile", "MissileComponent", "player"},
	{"Pulse", "PulseComponent", "shared"},
	{"Spirit", "SpiritComponent", ""},
	{"Materialize", "MaterializeComponent", ""},

	// --- Species ---
	{"Target", "TargetComponent", "shared"},
	{"TargetAnchor", "TargetAnchorComponent", "shared"},
	{"Drain", "DrainComponent", "player"},
	{"Quasar", "QuasarComponent", "shared"},
	{"Swarm", "SwarmComponent", "shared"},
	{"Storm", "StormComponent", "shared"},
	{"StormCircle", "StormCircleComponent", "shared"},
	{"Bullet", "BulletComponent", "player"},
	{"Pylon", "PylonComponent", "shared"},
	{"Snake", "SnakeComponent", "shared"},
	{"SnakeHead", "SnakeHeadComponent", "shared"},
	{"SnakeBody", "SnakeBodyComponent", "shared"},
	{"SnakeMember", "SnakeMemberComponent", "shared"},
	{"Eye", "EyeComponent", "shared"},
	{"Tower", "TowerComponent", "shared"},

	// --- Composite ---
	{"Header", "HeaderComponent", "shared"},
	{"Member", "MemberComponent", "shared"},

	// --- Effects ---
	{"Flash", "FlashComponent", "player"},
	{"Fadeout", "FadeoutComponent", "player"},
	{"Splash", "SplashComponent", "player"},
	{"Marker", "MarkerComponent", ""},

	// --- Lifecycle ---
	{"Death", "DeathComponent", ""},
	{"Timer", "TimerComponent", ""},
}

// Systems is the authoritative system list: order, construction, and domain profile
// Generator produces: RegisterSystems(), ActiveSystems(), systemDomains
var Systems = []SystemDef{
	// --- Core / Frame Setup ---
	{"cursor", "NewCursorSystem", "shared"}, // creates the shared cursor; creation order is replicated (D-11)
	{"ping", "NewPingSystem", "player"},
	{"transient", "NewTransientSystem", "player"},
	{"camera", "NewCameraSystem", "player"},

	// --- Player State: owner-authored cursor components (D-13) ---
	{"energy", "NewEnergySystem", "player"},
	{"shield", "NewShieldSystem", "player"},
	{"heat", "NewHeatSystem", "player"},
	{"boost", "NewBoostSystem", "player"},
	{"weapon", "NewWeaponSystem", "player"},

	// --- Input Processing ---
	{"typing", "NewTypingSystem", "player"},

	// --- Composite / Structure ---
	{"composite", "NewCompositeSystem", "shared"},
	{"wall", "NewWallSystem", "shared"},
	{"tower", "NewTowerSystem", "shared"},
	{"gateway", "NewGatewaySystem", "shared"},

	// --- Entity Behaviors ---
	{"loot", "NewLootSystem", "player"}, // rolled per participant against owner-authored inventory (D-6)
	{"glyph", "NewGlyphSystem", "player"},
	{"nugget", "NewNuggetSystem", "shared"}, // contested: the claim is a shared outcome
	{"decay", "NewDecaySystem", "player"},
	{"blossom", "NewBlossomSystem", "player"},
	{"gold", "NewGoldSystem", "shared"}, // contested: the sequence is shared, the reward owner-authored

	// --- Spawning / Materialize ---
	{"materialize", "NewMaterializeSystem", "dual"}, // stamped from the requester (D-7)
	{"cleaner", "NewCleanerSystem", "dual"},         // nugget-spawned shared, weapon-spawned player (D-7)
	{"fuse", "NewFuseSystem", "player"},
	{"spirit", "NewSpiritSystem", "dual"},

	// --- Projectiles ---
	{"lightning", "NewLightningSystem", "player"},
	{"missile", "NewMissileSystem", "player"},

	// --- Movement / Collision ---
	{"navigation", "NewNavigationSystem", "shared"},
	{"soft_collision", "NewSoftCollisionSystem", "dual"}, // one impulse stream per domain (D-8)

	// --- Combat ---
	{"combat", "NewCombatSystem", "dual"}, // one knockback stream per domain (D-8)

	// --- Species ---
	{"drain", "NewDrainSystem", "player"},
	{"quasar", "NewQuasarSystem", "shared"},
	{"swarm", "NewSwarmSystem", "shared"},
	{"storm", "NewStormSystem", "shared"},
	{"pylon", "NewPylonSystem", "shared"},
	{"snake", "NewSnakeSystem", "shared"},
	{"eye", "NewEyeSystem", "shared"},
	{"bullet", "NewBulletSystem", "player"},

	// --- Particles / Effects: player-domain by D-6 ---
	{"dust", "NewDustSystem", "player"},
	{"flash", "NewFlashSystem", "player"},
	{"fadeout", "NewFadeoutSystem", "player"},
	{"marker", "NewMarkerSystem", "shared"},
	{"explosion", "NewExplosionSystem", "shared"}, // the crossing artifact, not an effect
	{"motion_marker", "NewMotionMarkerSystem", "player"},
	{"splash", "NewSplashSystem", "player"},

	// --- Environment ---
	{"environment", "NewEnvironmentSystem", "shared"},

	// --- Lifecycle ---
	{"death", "NewDeathSystem", "dual"},
	{"timer", "NewTimerSystem", "dual"},
	{"adaptation", "NewAdaptationSystem", "shared"},
	{"genetic", "NewGeneticSystem", "shared"},

	// --- Audio ---
	{"audio", "NewAudioSystem", "player"},
	{"music", "NewMusicSystem", "player"},
}

// ContextSystems are context-scoped systems App registers directly: they take a
// GameContext rather than a World, so BuildSystems cannot construct them.
// TODO(phase6): fold into Systems once construction takes a capability set.
var ContextSystems = []SystemDef{
	{"meta", "NewMetaSystem", "shared"}, // world writes are replicated or the D-14 map writer
}

// Renderers is the authoritative renderer list
// Order determined by render priority
// Generator produces: RegisterRenderers(), ActiveRenderers()
var Renderers = []RendererDef{
	// --- Background / Grid ---
	{"ping", "NewPingRenderer", "PriorityPing"},
	{"chargeline", "NewChargeLineRenderer", "PriorityChargeLine"},

	// --- Environment ---
	{"wall", "NewWallRenderer", "PriorityWall"},

	// --- Base Entities ---
	{"glyph", "NewGlyphRenderer", "PriorityGlyph"},
	{"sigil", "NewSigilRenderer", "PrioritySigil"},
	{"gold", "NewGoldRenderer", "PriorityGold"},
	{"healthbar", "NewHealthBarRenderer", "PriorityHealthBar"},

	// --- Species (back to front) ---
	{"pylon", "NewPylonRenderer", "PriorityPylon"},
	{"tower", "NewTowerRenderer", "PriorityTower"},
	{"eye", "NewEyeRenderer", "PriorityEye"},
	{"snake", "NewSnakeRenderer", "PrioritySnake"},
	{"drain", "NewDrainRenderer", "PriorityDrain"},
	{"quasar", "NewQuasarRenderer", "PriorityQuasar"},
	{"swarm", "NewSwarmRenderer", "PrioritySwarm"},
	{"storm", "NewStormRenderer", "PriorityStorm"},

	// --- Cleaner ---
	{"cleaner", "NewCleanerRenderer", "PriorityCleaner"},

	// --- Materialize ---
	{"materialize", "NewMaterializeRenderer", "PriorityMaterialize"},
	{"teleportline", "NewTeleportLineRenderer", "PriorityTeleportLine"},

	// --- Field Effects ---
	{"shield", "NewShieldRenderer", "PriorityShield"},
	{"ember", "NewEmberRenderer", "PriorityEmber"},
	{"orb", "NewOrbRenderer", "PriorityOrb"},
	{"lightning", "NewLightningRenderer", "PriorityLightning"},
	{"missile", "NewMissileRenderer", "PriorityMissile"},
	{"pulse", "NewPulseRenderer", "PriorityPulse"},
	{"bullet", "NewBulletRenderer", "PriorityBullet"},

	// --- Particles ---
	{"flash", "NewFlashRenderer", "PriorityFlash"},
	{"fadeout", "NewFadeoutRenderer", "PriorityFadeout"},
	{"explosion", "NewExplosionRenderer", "PriorityExplosion"},
	{"spirit", "NewSpiritRenderer", "PrioritySpirit"},

	// --- Overlays ---
	{"splash", "NewSplashRenderer", "PrioritySplash"},
	{"marker", "NewMarkerRenderer", "PriorityMarker"},

	// --- Post-Processing ---
	{"grayout", "NewGrayoutRenderer", "PriorityGrayout"},
	{"strobe", "NewStrobeRenderer", "PriorityStrobe"},
	{"dim", "NewDimRenderer", "PriorityDim"},

	// --- UI ---
	{"heat", "NewHeatRenderer", "PriorityHeat"},
	{"indicator", "NewIndicatorRenderer", "PriorityIndicator"},
	{"statusbar", "NewStatusBarRenderer", "PriorityStatusBar"},
	{"cursor", "NewCursorRenderer", "PriorityCursor"},

	// --- Debug ---
	{"flowfield", "NewFlowFieldDebugRenderer", "PriorityFlowField"},
	{"pinned_state", "NewPinnedStatsRenderer", "PriorityPinnedState"},
	{"overlay", "NewOverlayRenderer", "PriorityOverlay"},
}

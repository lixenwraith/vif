package manifest

// ComponentDef defines a component for registration and store generation
type ComponentDef struct {
	Field string // ComponentStore field name (e.g., "Drain")
	Type  string // Type name without package (e.g., "DrainComponent")
}

// SystemDef defines a system for registration
// Order in slice determines ActiveSystems() order
type SystemDef struct {
	Name        string // Registry key (e.g., "drain")
	Constructor string // Constructor name without package (e.g., "NewDrainSystem")
}

// RendererDef defines a renderer for registration
// Order in slice determines ActiveRenderers() order
type RendererDef struct {
	Name        string // Registry key (e.g., "drain")
	Constructor string // Constructor name without package (e.g., "NewDrainRenderer")
	Priority    string // Priority constant without package (e.g., "PriorityDrain")
}

// Components is the authoritative component list
// Generator produces: ComponentStore, GetComponentStore(), RegisterComponents()
var Components = []ComponentDef{
	// --- Core Gameplay ---
	{"Glyph", "GlyphComponent"},
	{"Sigil", "SigilComponent"},
	{"Nugget", "NuggetComponent"},
	{"Cursor", "CursorComponent"},
	{"Protection", "ProtectionComponent"},
	{"Kinetic", "KineticComponent"},
	{"Wall", "WallComponent"},
	{"Loot", "LootComponent"},

	// --- Player State ---
	{"Energy", "EnergyComponent"},
	{"Heat", "HeatComponent"},
	{"Shield", "ShieldComponent"},
	{"Boost", "BoostComponent"},
	{"Weapon", "WeaponComponent"},
	{"Orb", "OrbComponent"},
	{"Ping", "PingComponent"},

	// --- Entity Behaviors ---
	{"Decay", "DecayComponent"},
	{"Blossom", "BlossomComponent"},
	{"Cleaner", "CleanerComponent"},
	{"Dust", "DustComponent"},
	{"Navigation", "NavigationComponent"},
	{"Combat", "CombatComponent"},
	{"Genotype", "GenotypeComponent"},
	{"Lightning", "LightningComponent"},
	{"Missile", "MissileComponent"},
	{"Pulse", "PulseComponent"},
	{"Spirit", "SpiritComponent"},
	{"Materialize", "MaterializeComponent"},

	// --- Species ---
	{"Target", "TargetComponent"},
	{"TargetAnchor", "TargetAnchorComponent"},
	{"Drain", "DrainComponent"},
	{"Quasar", "QuasarComponent"},
	{"Swarm", "SwarmComponent"},
	{"Storm", "StormComponent"},
	{"StormCircle", "StormCircleComponent"},
	{"Bullet", "BulletComponent"},
	{"Pylon", "PylonComponent"},
	{"Snake", "SnakeComponent"},
	{"SnakeHead", "SnakeHeadComponent"},
	{"SnakeBody", "SnakeBodyComponent"},
	{"SnakeMember", "SnakeMemberComponent"},
	{"Eye", "EyeComponent"},
	{"Tower", "TowerComponent"},

	// --- Composite ---
	{"Header", "HeaderComponent"},
	{"Member", "MemberComponent"},

	// --- Effects ---
	{"Flash", "FlashComponent"},
	{"Fadeout", "FadeoutComponent"},
	{"Splash", "SplashComponent"},
	{"Marker", "MarkerComponent"},

	// --- Lifecycle ---
	{"Death", "DeathComponent"},
	{"Timer", "TimerComponent"},
}

// Systems is the authoritative system list
// Order determined by priority in parameters
// Generator produces: RegisterSystems(), ActiveSystems()
var Systems = []SystemDef{
	// --- Core / Frame Setup ---
	{"ping", "NewPingSystem"},
	{"transient", "NewTransientSystem"},
	{"camera", "NewCameraSystem"},

	// --- Player State ---
	{"energy", "NewEnergySystem"},
	{"shield", "NewShieldSystem"},
	{"heat", "NewHeatSystem"},
	{"boost", "NewBoostSystem"},
	{"weapon", "NewWeaponSystem"},

	// --- Input Processing ---
	{"typing", "NewTypingSystem"},

	// --- Composite / Structure ---
	{"composite", "NewCompositeSystem"},
	{"wall", "NewWallSystem"},

	// --- Entity Behaviors ---
	{"loot", "NewLootSystem"},
	{"glyph", "NewGlyphSystem"},
	{"nugget", "NewNuggetSystem"},
	{"decay", "NewDecaySystem"},
	{"blossom", "NewBlossomSystem"},
	{"gold", "NewGoldSystem"},

	// --- Spawning / Materialize ---
	{"materialize", "NewMaterializeSystem"},
	{"cleaner", "NewCleanerSystem"},
	{"fuse", "NewFuseSystem"},
	{"spirit", "NewSpiritSystem"},

	// --- Projectiles ---
	{"lightning", "NewLightningSystem"},
	{"missile", "NewMissileSystem"},

	// --- Movement / Collision ---
	{"navigation", "NewNavigationSystem"},
	{"soft_collision", "NewSoftCollisionSystem"},

	// --- Combat ---
	{"combat", "NewCombatSystem"},

	// --- Species ---
	{"drain", "NewDrainSystem"},
	{"quasar", "NewQuasarSystem"},
	{"swarm", "NewSwarmSystem"},
	{"storm", "NewStormSystem"},
	{"pylon", "NewPylonSystem"},
	{"tower", "NewTowerSystem"},
	{"snake", "NewSnakeSystem"},
	{"eye", "NewEyeSystem"},
	{"bullet", "NewBulletSystem"},

	// --- Particles / Effects ---
	{"dust", "NewDustSystem"},
	{"flash", "NewFlashSystem"},
	{"fadeout", "NewFadeoutSystem"},
	{"marker", "NewMarkerSystem"},
	{"explosion", "NewExplosionSystem"},
	{"motion_marker", "NewMotionMarkerSystem"},
	{"splash", "NewSplashSystem"},

	// --- Environment ---
	{"environment", "NewEnvironmentSystem"},

	// --- Lifecycle ---
	{"death", "NewDeathSystem"},
	{"timekeeper", "NewTimeKeeperSystem"},
	{"genetic", "NewGeneticSystem"},

	// --- Audio ---
	{"audio", "NewAudioSystem"},
	{"music", "NewMusicSystem"},

	// --- Diagnostics ---
	{"diag", "NewDiagSystem"},
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
	{"overlay", "NewOverlayRenderer", "PriorityOverlay"},
	{"flowfield", "NewFlowFieldDebugRenderer", "PriorityFlowField"},
}
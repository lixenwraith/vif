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
	Priority    string // Priority constant without package (e.g., "PriorityField")
}

// Components is the authoritative component list
// Generator produces: ComponentStore, GetComponentStore(), RegisterComponents()
var Components = []ComponentDef{
	// Core gameplay
	{"Glyph", "GlyphComponent"},
	{"Sigil", "SigilComponent"},
	{"Nugget", "NuggetComponent"},
	{"Cursor", "CursorComponent"},
	{"Protection", "ProtectionComponent"},
	{"Kinetic", "KineticComponent"},
	{"Wall", "WallComponent"},
	{"Loot", "LootComponent"},

	// Player state
	{"Energy", "EnergyComponent"},
	{"Heat", "HeatComponent"},
	{"Shield", "ShieldComponent"},
	{"Boost", "BoostComponent"},
	{"Weapon", "WeaponComponent"},
	{"Orb", "OrbComponent"},
	{"Ping", "PingComponent"},

	// Entity behaviors
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

	// Enemies
	{"Drain", "DrainComponent"},
	{"Quasar", "QuasarComponent"},
	{"Swarm", "SwarmComponent"},
	{"Storm", "StormComponent"},
	{"StormCircle", "StormCircleComponent"},
	{"Bullet", "BulletComponent"},

	// Composite
	{"Header", "HeaderComponent"},
	{"Member", "MemberComponent"},

	// Effects
	{"Flash", "FlashComponent"},
	{"Fadeout", "FadeoutComponent"},
	{"Splash", "SplashComponent"},
	{"Marker", "MarkerComponent"},
	{"Environment", "EnvironmentComponent"},

	// Lifecycle
	{"Death", "DeathComponent"},
	{"Timer", "TimerComponent"},
}

// Systems is the authoritative system list
// Order determines execution priority via registration and ActiveSystems() order
// Generator produces: RegisterSystems(), ActiveSystems()
var Systems = []SystemDef{
	{"ping", "NewPingSystem"},
	{"energy", "NewEnergySystem"},
	{"shield", "NewShieldSystem"},
	{"heat", "NewHeatSystem"},
	{"boost", "NewBoostSystem"},
	{"weapon", "NewWeaponSystem"},
	{"typing", "NewTypingSystem"},
	{"composite", "NewCompositeSystem"},
	{"wall", "NewWallSystem"},
	{"loot", "NewLootSystem"},
	{"glyph", "NewGlyphSystem"},
	{"nugget", "NewNuggetSystem"},
	{"decay", "NewDecaySystem"},
	{"blossom", "NewBlossomSystem"},
	{"gold", "NewGoldSystem"},
	{"materialize", "NewMaterializeSystem"},
	{"cleaner", "NewCleanerSystem"},
	{"fuse", "NewFuseSystem"},
	{"spirit", "NewSpiritSystem"},
	{"lightning", "NewLightningSystem"},
	{"missile", "NewMissileSystem"},
	{"navigation", "NewNavigationSystem"},
	{"combat", "NewCombatSystem"},
	{"drain", "NewDrainSystem"},
	{"quasar", "NewQuasarSystem"},
	{"swarm", "NewSwarmSystem"},
	{"storm", "NewStormSystem"},
	{"bullet", "NewBulletSystem"},
	{"dust", "NewDustSystem"},
	{"flash", "NewFlashSystem"},
	{"fadeout", "NewFadeoutSystem"},
	{"marker", "NewMarkerSystem"},
	{"explosion", "NewExplosionSystem"},
	{"motion_marker", "NewMotionMarkerSystem"},
	{"splash", "NewSplashSystem"},
	{"environment", "NewEnvironmentSystem"},
	{"death", "NewDeathSystem"},
	{"timekeeper", "NewTimeKeeperSystem"},
	{"genetic", "NewGeneticSystem"},
	{"audio", "NewAudioSystem"},
	{"music", "NewMusicSystem"},
	{"camera", "NewCameraSystem"},
	{"diag", "NewDiagSystem"},
}

// Renderers is the authoritative renderer list
// Order determines ActiveRenderers() order (visual layering)
// Generator produces: RegisterRenderers(), ActiveRenderers()
var Renderers = []RendererDef{
	{"ping", "NewPingRenderer", "PriorityGrid"},
	{"chargeline", "NewChargeLineRenderer", "PriorityChargeLine"},
	{"splash", "NewSplashRenderer", "PrioritySplash"},
	{"glyph", "NewGlyphRenderer", "PriorityGlyph"},
	{"healthbar", "NewHealthBarRenderer", "PriorityHealthBar"},
	{"sigil", "NewSigilRenderer", "PriorityEntities"},
	{"gold", "NewGoldRenderer", "PriorityEntities"},
	{"shield", "NewShieldRenderer", "PriorityField"},
	{"cleaner", "NewCleanerRenderer", "PriorityCleaner"},
	{"flash", "NewFlashRenderer", "PriorityParticle"},
	{"fadeout", "NewFadeoutRenderer", "PriorityParticle"},
	{"marker", "NewMarkerRenderer", "PriorityMarker"},
	{"explosion", "NewExplosionRenderer", "PriorityParticle"},
	{"lightning", "NewLightningRenderer", "PriorityField"},
	{"missile", "NewMissileRenderer", "PriorityField"},
	{"pulse", "NewPulseRenderer", "PriorityField"},
	{"bullet", "NewBulletRenderer", "PriorityField"},
	{"spirit", "NewSpiritRenderer", "PriorityParticle"},
	{"materialize", "NewMaterializeRenderer", "PriorityMaterialize"},
	{"teleportline", "NewTeleportLineRenderer", "PriorityMaterialize"},
	{"drain", "NewDrainRenderer", "PrioritySpecies"},
	{"quasar", "NewQuasarRenderer", "PrioritySpecies"},
	{"swarm", "NewSwarmRenderer", "PrioritySpecies"},
	{"storm", "NewStormRenderer", "PrioritySpecies"},
	{"wall", "NewWallRenderer", "PriorityWall"},
	{"grayout", "NewGrayoutRenderer", "PriorityPostProcess"},
	{"dim", "NewDimRenderer", "PriorityPostProcess"},
	{"heat", "NewHeatRenderer", "PriorityUI"},
	{"indicator", "NewIndicatorRenderer", "PriorityUI"},
	{"statusbar", "NewStatusBarRenderer", "PriorityUI"},
	{"cursor", "NewCursorRenderer", "PriorityUI"},
	{"overlay", "NewOverlayRenderer", "PriorityOverlay"},

	{"flowfield", "NewFlowFieldDebugRenderer", "PriorityUI"},
}
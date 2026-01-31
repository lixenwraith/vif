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

	// Player state
	{"Energy", "EnergyComponent"},
	{"Heat", "HeatComponent"},
	{"Shield", "ShieldComponent"},
	{"Boost", "BoostComponent"},
	{"Buff", "BuffComponent"},
	{"Ping", "PingComponent"},

	// Entity behaviors
	{"Decay", "DecayComponent"},
	{"Blossom", "BlossomComponent"},
	{"Cleaner", "CleanerComponent"},
	{"Combat", "CombatComponent"},
	{"Drain", "DrainComponent"},
	{"Quasar", "QuasarComponent"},
	{"Swarm", "SwarmComponent"},
	{"Dust", "DustComponent"},
	{"Lightning", "LightningComponent"},
	{"Spirit", "SpiritComponent"},
	{"Materialize", "MaterializeComponent"},

	// Composite
	{"Header", "HeaderComponent"},
	{"Member", "MemberComponent"},

	// Effects
	{"Flash", "FlashComponent"},
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
	{"buff", "NewBuffSystem"},
	{"typing", "NewTypingSystem"},
	{"composite", "NewCompositeSystem"},
	{"glyph", "NewGlyphSystem"},
	{"nugget", "NewNuggetSystem"},
	{"decay", "NewDecaySystem"},
	{"blossom", "NewBlossomSystem"},
	{"gold", "NewGoldSystem"},
	{"materialize", "NewMaterializeSystem"},
	{"cleaner", "NewCleanerSystem"},
	{"vampire", "NewVampireSystem"},
	{"fuse", "NewFuseSystem"},
	{"spirit", "NewSpiritSystem"},
	{"lightning", "NewLightningSystem"},
	{"combat", "NewCombatSystem"},
	{"drain", "NewDrainSystem"},
	{"quasar", "NewQuasarSystem"},
	{"swarm", "NewSwarmSystem"},
	{"dust", "NewDustSystem"},
	{"flash", "NewFlashSystem"},
	{"marker", "NewMarkerSystem"},
	{"explosion", "NewExplosionSystem"},
	{"motion_marker", "NewMotionMarkerSystem"},
	{"splash", "NewSplashSystem"},
	{"environment", "NewEnvironmentSystem"},
	{"death", "NewDeathSystem"},
	{"timekeeper", "NewTimeKeeperSystem"},
	{"music", "NewMusicSystem"},
	{"diag", "NewDiagSystem"},
}

// Renderers is the authoritative renderer list
// Order determines ActiveRenderers() order (visual layering)
// Generator produces: RegisterRenderers(), ActiveRenderers()
var Renderers = []RendererDef{
	{"ping", "NewPingRenderer", "PriorityGrid"},
	{"splash", "NewSplashRenderer", "PrioritySplash"},
	{"glyph", "NewGlyphRenderer", "PriorityEntities"},
	{"sigil", "NewSigilRenderer", "PriorityEntities"},
	{"gold", "NewGoldRenderer", "PriorityEntities"},
	{"shield", "NewShieldRenderer", "PriorityField"},
	{"cleaner", "NewCleanerRenderer", "PriorityCleaner"},
	{"flash", "NewFlashRenderer", "PriorityParticle"},
	{"marker", "NewMarkerRenderer", "PriorityParticle"},
	{"explosion", "NewExplosionRenderer", "PriorityParticle"},
	{"lightning", "NewLightningRenderer", "PriorityField"},
	{"spirit", "NewSpiritRenderer", "PriorityParticle"},
	{"materialize", "NewMaterializeRenderer", "PriorityMaterialize"},
	{"quasar", "NewQuasarRenderer", "PriorityMulti"},
	{"swarm", "NewSwarmRenderer", "PriorityMulti"},
	{"grayout", "NewGrayoutRenderer", "PriorityPostProcess"},
	{"dim", "NewDimRenderer", "PriorityPostProcess"},
	{"heatmeter", "NewHeatMeterRenderer", "PriorityUI"},
	{"rowindicator", "NewRowIndicatorRenderer", "PriorityUI"},
	{"columnindicator", "NewColumnIndicatorRenderer", "PriorityUI"},
	{"statusbar", "NewStatusBarRenderer", "PriorityUI"},
	{"cursor", "NewCursorRenderer", "PriorityUI"},
	{"overlay", "NewOverlayRenderer", "PriorityOverlay"},
}
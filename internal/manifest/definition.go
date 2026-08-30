package manifest

// ComponentDef defines a component for registration, store generation, and the
// domain audit. Domain is "shared", "player", or "" when the bit attaches in either.
type ComponentDef struct {
	Field  string // ComponentStore field name (e.g., "Drain")
	Type   string // Type name without package (e.g., "DrainComponent")
	Domain string // Entity domain the bit may attach to; "" = either
}

// SystemDef declares a system: its registry key, constructor, domain profile, and
// the systems it depends on. Order in slice determines ActiveSystems() order.
type SystemDef struct {
	Name        string   // Registry key (e.g., "drain")
	Constructor string   // Constructor name without package (e.g., "NewDrainSystem")
	Domain      string   // Domain profile: "shared", "player" or "dual"
	Requires    []string // Systems this one cannot function without
	Optional    []string // Systems whose absence only degrades this one
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
	{"Nugget", "NuggetComponent", "player"},
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

// Systems is the authoritative system list: order, construction, domain profile, and dependencies
// Generator produces: RegisterSystems(), ActiveSystems(), systemProfiles
var Systems = []SystemDef{
	// --- Transport ---
	{Name: "network", Constructor: "NewNetworkSystem", Domain: "dual",
		Requires: []string{"cursor"}}, // replays a peer's crossings in the domain their producer stamped (D-7); sole writer of a remote cursor's owner-authored set (D-13)

	// --- Core / Frame Setup ---
	{Name: "cursor", Constructor: "NewCursorSystem", Domain: "shared"},                               // creates the shared cursor; replicated creation order (D-11); the roster is the dependency root
	{Name: "ping", Constructor: "NewPingSystem", Domain: "player", Requires: []string{"cursor"}},     // pure local view attached to the cursor (D-13)
	{Name: "transient", Constructor: "NewTransientSystem", Domain: "player"},                         // owns per-instance overlays and explosion presentation (D-6)
	{Name: "camera", Constructor: "NewCameraSystem", Domain: "player", Requires: []string{"cursor"}}, // per-instance view follows the local cursor

	// --- Player State: owner-authored cursor components (D-13) ---
	{Name: "energy", Constructor: "NewEnergySystem", Domain: "player", Requires: []string{"cursor"}},                               // owns cursor energy
	{Name: "shield", Constructor: "NewShieldSystem", Domain: "player", Requires: []string{"cursor"}, Optional: []string{"energy"}}, // owns cursor shield state; energy funds it
	{Name: "heat", Constructor: "NewHeatSystem", Domain: "player", Requires: []string{"cursor"}},                                   // owns cursor heat
	{Name: "boost", Constructor: "NewBoostSystem", Domain: "player", Requires: []string{"cursor"}, Optional: []string{"energy"}},   // owns cursor boost state; energy funds it
	{Name: "weapon", Constructor: "NewWeaponSystem", Domain: "player",
		Requires: []string{"cursor", "energy"}, Optional: []string{"combat", "cleaner"}}, // only the owner simulates a cursor's weapons (D-2, D-13)

	// --- Input Processing ---
	{Name: "typing", Constructor: "NewTypingSystem", Domain: "player", Requires: []string{"cursor"},
		Optional: []string{"glyph", "energy", "boost", "heat", "composite"}}, // consumes player glyphs and authors cursor state (D-13)

	// --- Composite / Structure ---
	{Name: "composite", Constructor: "NewCompositeSystem", Domain: "shared"},                                                                      // owns the shared header and member contract
	{Name: "wall", Constructor: "NewWallSystem", Domain: "shared", Requires: []string{"composite"}, Optional: []string{"navigation"}},             // shared walls push occupants from both domains (D-12)
	{Name: "tower", Constructor: "NewTowerSystem", Domain: "shared", Requires: []string{"composite"}, Optional: []string{"navigation", "combat"}}, // shared stream and composite species state
	{Name: "gateway", Constructor: "NewGatewaySystem", Domain: "shared", Requires: []string{"navigation"}, Optional: []string{"eye", "snake"}},    // shared route anchor; gated species are optional

	// --- Entity Behaviors ---
	{Name: "loot", Constructor: "NewLootSystem", Domain: "player",
		Optional: []string{"death", "energy", "weapon", "heat"}}, // rolled per participant against owner-authored inventory; reward handlers are optional (D-6)
	{Name: "glyph", Constructor: "NewGlyphSystem", Domain: "player"}, // player stream and entities; corpus and map are its only inputs
	{Name: "nugget", Constructor: "NewNuggetSystem", Domain: "player",
		Optional: []string{"cleaner", "energy", "heat"}}, // personal: each participant owns its spawn, collection and reward
	{Name: "decay", Constructor: "NewDecaySystem", Domain: "player", Optional: []string{"glyph", "death"}}, // player entities that idle without glyph and death events
	{Name: "blossom", Constructor: "NewBlossomSystem", Domain: "player", Optional: []string{"death"}},      // player entities requested on death and idle without it
	{Name: "gold", Constructor: "NewGoldSystem", Domain: "shared",
		Requires: []string{"composite"}, Optional: []string{"nugget", "energy", "splash"}}, // contested: the composite sequence is shared, the reward owner-authored

	// --- Spawning / Materialize ---
	{Name: "materialize", Constructor: "NewMaterializeSystem", Domain: "dual"}, // stamped from the requester; the spawn gate is a dependency root (D-7)
	{Name: "cleaner", Constructor: "NewCleanerSystem", Domain: "dual",
		Optional: []string{"combat", "decay"}}, // request-stamped construction; current producers are player-domain (D-7)
	{Name: "fuse", Constructor: "NewFuseSystem", Domain: "player", Requires: []string{"drain", "materialize", "spirit"},
		Optional: []string{"quasar", "swarm"}}, // player stream crosses through the spawn request (D-3)
	{Name: "spirit", Constructor: "NewSpiritSystem", Domain: "dual"}, // creates in the requesting domain, currently from the player-domain fuse (D-7)

	// --- Projectiles ---
	{Name: "lightning", Constructor: "NewLightningSystem", Domain: "player", Optional: []string{"combat"}}, // player stream and request-created entities; combat is optional (D-8)
	{Name: "missile", Constructor: "NewMissileSystem", Domain: "player", Requires: []string{"weapon"},
		Optional: []string{"explosion", "combat"}}, // player missile impact crosses through an explosion request (D-3)

	// --- Movement / Collision ---
	{Name: "navigation", Constructor: "NewNavigationSystem", Domain: "shared"},      // derives flow fields and route graphs from the map and shared species
	{Name: "soft_collision", Constructor: "NewSoftCollisionSystem", Domain: "dual"}, // one impulse stream per occupant domain (D-8)

	// --- Combat ---
	{Name: "combat", Constructor: "NewCombatSystem", Domain: "dual", Requires: []string{"death"}}, // one knockback stream per target domain; every kill routes through death (D-8)

	// --- Species ---
	{Name: "drain", Constructor: "NewDrainSystem", Domain: "player", Requires: []string{"materialize"},
		Optional: []string{"heat", "navigation", "combat"}}, // player stream and entities; materialize gates every spawn and heat sets population
	{Name: "quasar", Constructor: "NewQuasarSystem", Domain: "shared", Requires: []string{"composite"},
		Optional: []string{"navigation", "combat", "lightning"}}, // shared stream and composite species with a D-12 footprint sweep
	{Name: "swarm", Constructor: "NewSwarmSystem", Domain: "shared", Requires: []string{"composite"},
		Optional: []string{"navigation", "combat"}}, // shared stream and composite species with a D-12 footprint sweep
	{Name: "storm", Constructor: "NewStormSystem", Domain: "shared", Requires: []string{"composite"},
		Optional: []string{"navigation", "combat", "bullet", "dust", "wall"}}, // shared stream and composite species with a D-12 footprint sweep
	{Name: "pylon", Constructor: "NewPylonSystem", Domain: "shared", Requires: []string{"composite"},
		Optional: []string{"navigation", "combat"}}, // shared stream and composite species state
	{Name: "snake", Constructor: "NewSnakeSystem", Domain: "shared", Requires: []string{"composite"},
		Optional: []string{"navigation", "combat"}}, // shared stream and composite species with a D-12 footprint sweep
	{Name: "eye", Constructor: "NewEyeSystem", Domain: "shared", Requires: []string{"composite"},
		Optional: []string{"navigation", "combat"}}, // shared stream and composite species with a D-12 footprint sweep
	{Name: "bullet", Constructor: "NewBulletSystem", Domain: "player", Optional: []string{"combat"}}, // player bullets; combat optionally resolves their hits

	// --- Particles / Effects: player-domain by D-6 ---
	{Name: "dust", Constructor: "NewDustSystem", Domain: "player", Optional: []string{"explosion"}},               // player stream and entities; detonation optionally crosses through explosion
	{Name: "flash", Constructor: "NewFlashSystem", Domain: "player"},                                              // request-created player effect
	{Name: "fadeout", Constructor: "NewFadeoutSystem", Domain: "player"},                                          // request-created player effect
	{Name: "marker", Constructor: "NewMarkerSystem", Domain: "shared"},                                            // request-created shared marker
	{Name: "explosion", Constructor: "NewExplosionSystem", Domain: "shared", Requires: []string{"combat"}},        // combat-only consumer of crossed geometry; presentation is transient (D-3, D-6)
	{Name: "motion_marker", Constructor: "NewMotionMarkerSystem", Domain: "player", Requires: []string{"cursor"}}, // local-cursor marker (D-6)
	{Name: "splash", Constructor: "NewSplashSystem", Domain: "player", Requires: []string{"cursor"}},              // local-cursor viewport overlay (D-6)

	// --- Environment ---
	{Name: "environment", Constructor: "NewEnvironmentSystem", Domain: "shared"}, // shared stream and state derived from the map and clock

	// --- Lifecycle ---
	{Name: "death", Constructor: "NewDeathSystem", Domain: "dual"},                                                  // routes one death batch per domain; effect systems subscribe to its output
	{Name: "timer", Constructor: "NewTimerSystem", Domain: "dual", Requires: []string{"death"}},                     // expires entities of either domain through the death pipeline
	{Name: "adaptation", Constructor: "NewAdaptationSystem", Domain: "shared", Requires: []string{"navigation"}},    // shared stream and route state; scores navigation graphs
	{Name: "genetic", Constructor: "NewGeneticSystem", Domain: "shared", Optional: []string{"death", "adaptation"}}, // shared genotype state; observes lifecycle and route outcomes

	// --- Audio ---
	{Name: "audio", Constructor: "NewAudioSystem", Domain: "player"},                              // per-instance sound sink with no simulation writes
	{Name: "music", Constructor: "NewMusicSystem", Domain: "player", Optional: []string{"audio"}}, // player stream; tracks intensity silently without audio
}

// ContextSystems are context-scoped systems App registers directly: they take a
// GameContext rather than a World, so BuildSystems cannot construct them.
// TODO: fold into Systems once construction takes a capability set.
var ContextSystems = []SystemDef{
	{Name: "meta", Constructor: "NewMetaSystem", Domain: "shared"}, // world writes are replicated or the D-14 map writer; publishes context and kill counters
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

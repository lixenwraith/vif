package visual

import "github.com/lixenwraith/vi-fighter/terminal"

// Generic TrueColor palette — pure RGB definitions without game semantics
// Game systems and renderers reference these via aliases in their own parameter files
//
// Naming: standard color names where RGB closely matches (CSS, X11, Pantone-adjacent),
// descriptive compound names otherwise. Ordered dark-to-light within each hue group.

var (
	// --- Achromatic ---
	Black      = terminal.RGB{0, 0, 0}
	Charcoal   = terminal.RGB{5, 5, 5}
	Obsidian   = terminal.RGB{20, 20, 30} // Blue-black
	Gunmetal   = terminal.RGB{26, 27, 38} // Blue-tinted near-black
	DarkSlate  = terminal.RGB{35, 36, 48} // Blue-gray near-black
	DimGray    = terminal.RGB{55, 55, 55}
	DarkGray   = terminal.RGB{60, 60, 60}
	IronGray   = terminal.RGB{80, 80, 80}
	SlateGray  = terminal.RGB{80, 80, 90}  // Cool-tinted
	Taupe      = terminal.RGB{100, 95, 85} // Warm gray
	Gray       = terminal.RGB{120, 120, 120}
	MidGray    = terminal.RGB{128, 128, 128}
	CoolSilver = terminal.RGB{140, 145, 155} // Blue-tinted silver
	DimSilver  = terminal.RGB{155, 155, 155}
	Silver     = terminal.RGB{180, 180, 180}
	LightGray  = terminal.RGB{200, 200, 200}
	NearWhite  = terminal.RGB{250, 250, 250}
	White      = terminal.RGB{255, 255, 255}

	// --- Brown / Earth ---
	Chocolate    = terminal.RGB{90, 25, 15}
	SaddleBrown  = terminal.RGB{101, 67, 33}
	DarkRust     = terminal.RGB{140, 35, 25}
	Sienna       = terminal.RGB{140, 60, 0}
	DarkPlum     = terminal.RGB{60, 30, 40} // Warm dark purple-brown
	BlueCharcoal = terminal.RGB{40, 45, 60} // Cool dark blue-gray

	// --- Red ---
	BlackRed     = terminal.RGB{50, 15, 15}
	Oxblood      = terminal.RGB{100, 20, 20}
	DarkBurgundy = terminal.RGB{100, 25, 20}
	DarkCrimson  = terminal.RGB{139, 0, 0}
	Brick        = terminal.RGB{180, 40, 40}
	Cinnabar     = terminal.RGB{200, 60, 50}
	IndianRed    = terminal.RGB{180, 60, 60}
	BurntSienna  = terminal.RGB{200, 60, 25}
	Vermilion    = terminal.RGB{227, 66, 82}
	Red          = terminal.RGB{255, 0, 0}
	BrightRed    = terminal.RGB{255, 60, 60}
	Coral        = terminal.RGB{255, 80, 80}
	Salmon       = terminal.RGB{255, 100, 100}
	LightCoral   = terminal.RGB{255, 140, 140}
	LightRose    = terminal.RGB{255, 150, 150}
	MistyRose    = terminal.RGB{255, 200, 200}

	// --- Orange ---
	DarkAmber   = terminal.RGB{60, 40, 0}
	Rust        = terminal.RGB{180, 60, 20}
	Amber       = terminal.RGB{180, 120, 0}
	Bronze      = terminal.RGB{200, 100, 0}
	BurntOrange = terminal.RGB{200, 110, 0}
	Terracotta  = terminal.RGB{220, 100, 50}
	FlameOrange = terminal.RGB{240, 100, 30}
	OrangeRed   = terminal.RGB{255, 69, 0}
	RedOrange   = terminal.RGB{255, 80, 40}
	Mango       = terminal.RGB{255, 120, 50}
	TigerOrange = terminal.RGB{255, 140, 0}
	WarmOrange  = terminal.RGB{255, 140, 40}
	Apricot     = terminal.RGB{255, 160, 60}
	Orange      = terminal.RGB{255, 165, 0}

	// --- Yellow ---
	DarkGold    = terminal.RGB{200, 150, 0}
	OliveYellow = terminal.RGB{200, 180, 60}
	Gold        = terminal.RGB{255, 215, 0}
	LemonYellow = terminal.RGB{255, 240, 60}
	Yellow      = terminal.RGB{255, 255, 0}
	PaleGold    = terminal.RGB{255, 200, 100}
	Buttercream = terminal.RGB{255, 250, 150}
	PaleLemon   = terminal.RGB{255, 255, 100}
	Ivory       = terminal.RGB{255, 255, 220}
	Cream       = terminal.RGB{255, 255, 200}

	// --- Green ---
	BlackGreen   = terminal.RGB{0, 40, 0}
	DarkFern     = terminal.RGB{30, 80, 25}
	DeepForest   = terminal.RGB{25, 80, 35}
	HunterGreen  = terminal.RGB{35, 90, 30}
	DarkGreen    = terminal.RGB{15, 130, 15}
	ForestGreen  = terminal.RGB{34, 139, 34}
	MediumGreen  = terminal.RGB{40, 150, 40}
	FernGreen    = terminal.RGB{50, 140, 45}
	LeafGreen    = terminal.RGB{60, 160, 60}
	SeaGreen     = terminal.RGB{60, 180, 80}
	SageGreen    = terminal.RGB{70, 170, 100}
	GrassGreen   = terminal.RGB{70, 180, 55}
	EmeraldGreen = terminal.RGB{60, 220, 100}
	MintGreen    = terminal.RGB{100, 220, 130}
	BrightGreen  = terminal.RGB{20, 200, 20}
	YellowGreen  = terminal.RGB{100, 220, 80}
	LimeGreen    = terminal.RGB{50, 205, 50}
	NeonGreen    = terminal.RGB{50, 255, 50}
	BrightLime   = terminal.RGB{120, 255, 80}
	Lime         = terminal.RGB{0, 255, 0}
	LightGreen   = terminal.RGB{144, 238, 144}
	PaleGreen    = terminal.RGB{120, 255, 120}
	PastelGreen  = terminal.RGB{100, 220, 100}
	PaleMint     = terminal.RGB{150, 255, 180}
	Honeydew     = terminal.RGB{200, 255, 200}

	// --- Cyan / Teal ---
	Teal          = terminal.RGB{0, 139, 139}
	DimCyan       = terminal.RGB{0, 160, 160}
	VibrantCyan   = terminal.RGB{0, 200, 200}
	DarkTurquoise = terminal.RGB{0, 206, 209}
	BrightCyan    = terminal.RGB{0, 220, 220}
	Cyan          = terminal.RGB{0, 255, 255}
	SkyTeal       = terminal.RGB{80, 200, 220}
	PaleCyan      = terminal.RGB{200, 255, 255}
	AliceBlue     = terminal.RGB{230, 245, 255}
	IceCyan       = terminal.RGB{240, 255, 255}

	// --- Blue ---
	DeepNavy     = terminal.RGB{15, 25, 50}
	DeepIndigo   = terminal.RGB{40, 0, 180}
	NavyBlue     = terminal.RGB{30, 60, 120}
	CobaltBlue   = terminal.RGB{50, 80, 200}
	SteelBlue    = terminal.RGB{60, 100, 180}
	MediumBlue   = terminal.RGB{60, 120, 200}
	RoyalBlue    = terminal.RGB{65, 105, 225}
	CeruleanBlue = terminal.RGB{80, 140, 220}
	Cornflower   = terminal.RGB{80, 130, 255}
	DodgerBlue   = terminal.RGB{40, 180, 255}
	LightBlue    = terminal.RGB{120, 170, 255}
	LightSkyBlue = terminal.RGB{135, 206, 250}
	BabyBlue     = terminal.RGB{160, 210, 255}
	Blue         = terminal.RGB{0, 0, 255}

	// --- Purple / Violet ---
	DeepPurple     = terminal.RGB{60, 20, 80}
	DarkViolet     = terminal.RGB{120, 40, 180}
	MutedPurple    = terminal.RGB{160, 100, 160}
	MediumPurple   = terminal.RGB{170, 100, 210}
	ElectricViolet = terminal.RGB{180, 130, 255}
	LightOrchid    = terminal.RGB{200, 130, 210}
	Orchid         = terminal.RGB{200, 120, 220}
	PaleVioletRed  = terminal.RGB{219, 112, 147}
	SoftLavender   = terminal.RGB{220, 150, 230}
	PaleLavender   = terminal.RGB{220, 180, 255}

	// --- Pink / Rose ---
	RoseRed    = terminal.RGB{255, 60, 120}
	HotMagenta = terminal.RGB{255, 60, 200}
	HotPink    = terminal.RGB{255, 140, 200}
	PalePink   = terminal.RGB{255, 145, 220}
	LightPink  = terminal.RGB{255, 182, 193}
	Pink       = terminal.RGB{255, 192, 203}
	Magenta    = terminal.RGB{255, 0, 255}
)
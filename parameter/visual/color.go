package visual

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// terminal.RGB color definitions for all game systems
var (
	// General colors for various uses
	RgbBlack   = terminal.RGB{0, 0, 0}
	RgbWhite   = terminal.RGB{255, 255, 255}
	RgbRed     = terminal.RGB{255, 0, 0}
	RgbOrange  = terminal.RGB{180, 120, 0}
	RgbYellow  = terminal.RGB{255, 255, 0}
	RgbGreen   = terminal.RGB{0, 255, 0}
	RgbCyan    = terminal.RGB{0, 255, 255}
	RgbBlue    = terminal.RGB{0, 0, 255}
	RgbMagenta = terminal.RGB{255, 0, 255}

	// terminal.RGB color definitions for glyphs - all dark/normal/bright levels have minimum floor to prevent perceptual blackout at low alpha
	RgbGlyphBlueDark   = terminal.RGB{50, 80, 200} // Floor R/G
	RgbGlyphBlueNormal = terminal.RGB{80, 130, 255}
	RgbGlyphBlueBright = terminal.RGB{120, 170, 255}

	RgbGlyphGreenDark   = terminal.RGB{15, 130, 15} // Floor R/B to prevent blackout
	RgbGlyphGreenNormal = terminal.RGB{20, 200, 20}
	RgbGlyphGreenBright = terminal.RGB{50, 255, 50}

	RgbGlyphRedDark   = terminal.RGB{180, 40, 40} // Floor G/B
	RgbGlyphRedNormal = terminal.RGB{255, 60, 60}
	RgbGlyphRedBright = terminal.RGB{255, 100, 100}

	RgbGlyphGold = terminal.RGB{255, 255, 0} // Bright Yellow for gold sequence

	RgbGlyphWhite = terminal.RGB{255, 255, 255} // Pure white

	RgbDecay       = terminal.RGB{0, 139, 139}   // Dark Cyan for decay animation
	RgbBlossom     = terminal.RGB{255, 182, 193} // Light pink (cherry blossom)
	RgbDrain       = terminal.RGB{0, 200, 200}   // Vibrant Cyan for drain entity
	RgbMaterialize = terminal.RGB{0, 220, 220}   // Bright cyan for materialize head

	RgbDustDark   = terminal.RGB{R: 60, G: 60, B: 60}    // Dark gray dust
	RgbDustNormal = terminal.RGB{R: 128, G: 128, B: 128} // Mid-gray dust
	RgbDustBright = terminal.RGB{R: 200, G: 200, B: 200} // Light gray dust

	RgbRowIndicator    = terminal.RGB{180, 180, 180} // Brighter gray
	RgbColumnIndicator = terminal.RGB{180, 180, 180} // Brighter gray
	RgbStatusBar       = terminal.RGB{255, 255, 255} // White
	RgbBackground      = terminal.RGB{26, 27, 38}    // Tokyo Night background

	RgbPingHighlight  = terminal.RGB{55, 55, 55}    // Gray for INSERT mode ping
	RgbPingLineNormal = terminal.RGB{5, 5, 5}       // Almost Black for NORMAL and VISUAL modes ping lines
	RgbPingGridNormal = terminal.RGB{55, 55, 55}    // Gray for NORMAL mode ping grid
	RgbPingOrange     = terminal.RGB{60, 40, 0}     // Very dark orange for ping on whitespace
	RgbPingGreen      = terminal.RGB{0, 40, 0}      // Very dark green for ping on green char
	RgbPingRed        = terminal.RGB{50, 15, 15}    // Very dark red for ping on red char
	RgbPingBlue       = terminal.RGB{15, 25, 50}    // Very dark blue for ping on blue char
	RgbCursorNormal   = terminal.RGB{255, 165, 0}   // Orange for normal mode
	RgbCursorInsert   = terminal.RGB{255, 255, 255} // Bright white for insert mode

	// Boost glow effect
	RgbBoostGlow = terminal.RGB{255, 140, 200} // Vibrant pink for rotating shield glow

	// Splash colors
	RgbSplashWhite = terminal.RGB{155, 155, 155} // White for some actions
	RgbSplashCyan  = terminal.RGB{0, 200, 200}   // Cyan for quasar charge timer

	// Nugget colors
	RgbNuggetOrange = terminal.RGB{255, 165, 0}   // Same as insert cursor
	RgbNuggetDark   = terminal.RGB{101, 67, 33}   // Dark brown for contrast
	RgbCursorError  = terminal.RGB{255, 0, 0}     // Error Red
	RgbTrailGray    = terminal.RGB{200, 200, 200} // Light gray base

	// Status bar backgrounds
	RgbModeNormalBg  = terminal.RGB{135, 206, 250} // Light sky blue
	RgbModeVisualBg  = terminal.RGB{255, 200, 100} // Orange-yellow for visual mode
	RgbModeInsertBg  = terminal.RGB{144, 238, 144} // Light grass green
	RgbModeSearchBg  = terminal.RGB{255, 165, 0}   // Orange
	RgbModeCommandBg = terminal.RGB{160, 100, 160} // Light purple
	RgbEnergyBg      = terminal.RGB{255, 255, 255} // Bright white
	RgbBoostBg       = terminal.RGB{255, 192, 203} // Pink for boost timer
	RgbStatusText    = terminal.RGB{0, 0, 0}       // Dark text for status

	// Runtime Metrics Backgrounds
	RgbFpsBg = terminal.RGB{0, 255, 255}   // Cyan for FPS
	RgbGtBg  = terminal.RGB{255, 200, 100} // Light Orange for Game Ticks
	RgbApmBg = terminal.RGB{50, 205, 50}   // Lime Green for APM

	// Cleaner colors
	RgbCleanerBasePositive = terminal.RGB{255, 255, 0}   // Bright yellow for positive energy cleaners
	RgbCleanerBaseNegative = terminal.RGB{170, 100, 210} // Violet for negative energy cleaners

	// Flash colors
	RgbRemovalFlash = terminal.RGB{255, 255, 200} // Bright yellow-white flash

	// // Explosion gradient, Realistic
	// RgbExplosionCore = terminal.RGB{255, 255, 220} // Bright white-yellow flash
	// RgbExplosionMid  = terminal.RGB{255, 120, 20}  // Intense orange
	// RgbExplosionEdge = terminal.RGB{120, 20, 0}    // Dark red fade

	// Explosion gradient (Neon/Cyber theme)
	RgbExplosionCore = terminal.RGB{240, 255, 255} // Bright White-Cyan
	RgbExplosionMid  = terminal.RGB{0, 255, 255}   // Electric Cyan
	RgbExplosionEdge = terminal.RGB{40, 0, 180}    // Deep Indigo/Blue

	// CombatColors
	RgbCombatEnraged  = terminal.RGB{255, 60, 60} // Red tint during charge or zap phase
	RgbCombatHitFlash = terminal.RGB{255, 255, 0} // Bright yellow for hit flash

	// Quasar colors
	RgbQuasarShield = terminal.RGB{0, 200, 200} // Cyan for clean shield halo and quasar indication

	// Swarm charge line pulse color (light orchid/pink-violet)
	RgbSwarmChargeLine = terminal.RGB{R: 200, G: 130, B: 210}

	// Orb colors
	RgbOrbRod      = terminal.RGB{0, 220, 220}   // Cyan
	RgbOrbLauncher = terminal.RGB{255, 140, 0}   // Orange
	RgbOrbSpray    = terminal.RGB{105, 255, 180} //
	RgbOrbFlash    = terminal.RGB{255, 255, 255} // White flash

	// Parent Missile: Chrome/White
	RgbMissileParentBody       = terminal.RGB{255, 255, 255}
	RgbMissileParentTrailStart = terminal.RGB{250, 250, 250}
	RgbMissileParentTrailEnd   = terminal.RGB{80, 80, 90} // Steel gray

	// Child Missile: Deep Orange
	RgbMissileChildBody       = terminal.RGB{255, 140, 0}
	RgbMissileChildTrailStart = terminal.RGB{200, 100, 0}
	RgbMissileChildTrailEnd   = terminal.RGB{140, 60, 0}

	// Missile impact explosion (warm palette - distinct from cyan/neon main explosion)
	RgbMissileExplosionCore = terminal.RGB{R: 255, G: 255, B: 220} // Bright white-yellow
	RgbMissileExplosionMid  = terminal.RGB{R: 255, G: 140, B: 40}  // Warm orange
	RgbMissileExplosionEdge = terminal.RGB{R: 180, G: 60, B: 20}   // Dark orange-red

	// Audio indicator colors
	RgbAudioBothOff     = terminal.RGB{R: 180, G: 60, B: 60}  // Red: both off
	RgbAudioMusicOnly   = terminal.RGB{R: 200, G: 180, B: 60} // Yellow: effects off, music on
	RgbAudioEffectsOnly = terminal.RGB{R: 60, G: 160, B: 60}  // Green: effects on, music off
	RgbAudioBothOn      = terminal.RGB{R: 60, G: 120, B: 200} // Blue: both on

	// Energy meter blink colors
	RgbEnergyBlinkBlue  = terminal.RGB{160, 210, 255} // Blue blink
	RgbEnergyBlinkGreen = terminal.RGB{120, 255, 120} // Green blink
	RgbEnergyBlinkRed   = terminal.RGB{255, 140, 140} // Red blink
	RgbEnergyBlinkWhite = terminal.RGB{255, 255, 255} // White blink

	// Overlay colors
	RgbOverlayBorder    = terminal.RGB{0, 255, 255}            // Bright Cyan for border
	RgbOverlayBg        = terminal.RGB{20, 20, 30}             // Dark background
	RgbOverlayText      = terminal.RGB{255, 255, 255}          // White text for high contrast
	RgbOverlayTitle     = terminal.RGB{255, 255, 0}            // Yellow for title
	RgbOverlayHeader    = terminal.RGB{R: 255, G: 215, B: 0}   // Gold for headers
	RgbOverlayKey       = terminal.RGB{R: 180, G: 180, B: 180} // Light gray for keys
	RgbOverlayValue     = terminal.RGB{R: 100, G: 220, B: 100} // Green for values
	RgbOverlayHint      = terminal.RGB{R: 120, G: 120, B: 120} // Dim for hints
	RgbOverlaySeparator = terminal.RGB{R: 80, G: 80, B: 80}    // Dark for separators

	// Status bar auxiliary colors
	RgbColorModeIndicator = terminal.RGB{200, 200, 200} // Light gray for TC/256 indicator
	RgbGridTimerFg        = terminal.RGB{255, 255, 255} // White for grid timer text
	RgbLastCommandText    = terminal.RGB{255, 255, 0}   // Yellow for last command indicator
	RgbSearchInputText    = terminal.RGB{255, 255, 255} // White for search input
	RgbCommandInputText   = terminal.RGB{255, 255, 255} // White for command input
	RgbStatusMessageText  = terminal.RGB{200, 200, 200} // Light gray for status messages

	// Wall colors
	RgbWallDefault = terminal.RGB{R: 80, G: 80, B: 90}
	RgbWallStone   = terminal.RGB{R: 100, G: 95, B: 85}
	RgbWallMetal   = terminal.RGB{R: 140, G: 145, B: 155}
	RgbWallEnergy  = terminal.RGB{R: 60, G: 20, B: 80}
	RgbWallDanger  = terminal.RGB{R: 100, G: 20, B: 20}
	RgbWallGhost   = terminal.RGB{R: 35, G: 36, B: 48}

	// Loot shield colors (defined in renderer as gradient LUT)
	// Referenced here for consistency documentation only
	RgbLootShieldBorder = terminal.RGB{R: 255, G: 105, B: 180} // Hot pink
	RgbLootShieldInner  = terminal.RGB{R: 45, G: 12, B: 32}    // Dark
	RgbLootShieldCenter = terminal.RGB{R: 12, G: 4, B: 10}     // Near black

	// Loot glow colors
	RgbLootRodGlow      = terminal.RGB{R: 200, G: 255, B: 255} // Bright cyan-white
	RgbLootLauncherGlow = terminal.RGB{R: 255, G: 255, B: 100} // Bright yellow
)

// StormCircleColors - neon base colors (saturated 1.3x in renderer)
var StormCircleColors = []terminal.RGB{
	{R: 40, G: 180, B: 255}, // Cyan
	{R: 255, G: 60, B: 120}, // Magenta
	{R: 120, G: 255, B: 80}, // Lime
}

// LightningTrueColorLUT is TrueColor gradient endpoints per lightning color type
// Index by LightningColorType to get (core, hot) RGB pair
// Core = base color at end of life, Hot = bright color at full life
var LightningTrueColorLUT = [5][2]terminal.RGB{
	// Cyan: cool cyan core -> white hot center
	{RgbDrain, RgbEnergyBlinkWhite},
	// Red: dark red core -> bright red-white
	{{180, 40, 40}, {255, 200, 200}},
	// Gold: orange core -> bright yellow-white
	{{200, 150, 0}, {255, 255, 200}},
	// Green: dark green core -> bright green-white
	{{40, 150, 40}, {200, 255, 200}},
	// Purple: dark purple core -> bright purple-white
	{{120, 40, 180}, {220, 180, 255}},
}

// 256-colors palette indices
const (
	// Missile
	Missile256Trail  uint8 = 214 // Orange
	Missile256Body   uint8 = 220 // Gold
	Missile256Seeker uint8 = 208 // Dark orange

	// Swarm charge line
	SwarmChargeLine256Palette uint8 = 176 // Light orchid

	// Wall fallback
	Wall256PaletteDefault uint8 = 240

	// Loot shield
	Loot256Rim uint8 = 198

	// Storm rendering palette indices
	Storm256Bright = 87
	Storm256Normal = 44
	Storm256Dark   = 23
)

// GlyphColorLUT maps [GlyphType][GlyphLevel] to RGB
// Type indices: 0=Green, 1=Blue, 2=Red, 3=White, 4=Gold
// Level indices: 0=Dark, 1=Normal, 2=Bright
var GlyphColorLUT = [5][3]terminal.RGB{
	{RgbGlyphGreenDark, RgbGlyphGreenNormal, RgbGlyphGreenBright}, // Green
	{RgbGlyphBlueDark, RgbGlyphBlueNormal, RgbGlyphBlueBright},    // Blue
	{RgbGlyphRedDark, RgbGlyphRedNormal, RgbGlyphRedBright},       // Red
	{RgbGlyphWhite, RgbGlyphWhite, RgbGlyphWhite},                 // White
	{RgbGlyphGold, RgbGlyphGold, RgbGlyphGold},                    // Gold
}
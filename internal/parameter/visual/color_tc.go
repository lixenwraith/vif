package visual

import (
	"github.com/lixenwraith/color"
)

// color.RGB color definitions for all game systems
var (
	// General colors for various uses
	RgbBlack   = color.Black
	RgbWhite   = color.White
	RgbRed     = color.Red
	RgbOrange  = color.Amber
	RgbYellow  = color.Yellow
	RgbGreen   = color.Lime
	RgbCyan    = color.Cyan
	RgbBlue    = color.Blue
	RgbMagenta = color.Magenta

	RgbSoftGold       = color.PaleGold
	RgbElectricViolet = color.ElectricViolet

	RgbBackground = color.Gunmetal // Tokyo Night background

	// Glyph colors - all dark/normal/bright levels have minimum floor to prevent perceptual blackout at low alpha
	RgbGlyphBlueDark   = color.CobaltBlue
	RgbGlyphBlueNormal = color.Cornflower
	RgbGlyphBlueBright = color.LightBlue

	RgbGlyphGreenDark   = color.DarkGreen
	RgbGlyphGreenNormal = color.BrightGreen
	RgbGlyphGreenBright = color.NeonGreen

	RgbGlyphRedDark   = color.Brick
	RgbGlyphRedNormal = color.BrightRed
	RgbGlyphRedBright = color.Salmon

	RgbGlyphGold  = color.Yellow
	RgbGlyphWhite = color.White

	RgbDecay       = color.Teal
	RgbBlossom     = color.LightPink
	RgbDrain       = color.VibrantCyan
	RgbMaterialize = color.BrightCyan

	RgbDustDark   = color.DarkGray
	RgbDustNormal = color.MidGray
	RgbDustBright = color.LightGray

	RgbIndicator           = color.Silver
	RgbStatusBar           = color.White
	RgbTruncateIndicator   = color.Black
	RgbTruncateIndicatorBg = color.Orange
	RgbStatusCursor        = color.Orange
	RgbStatusCursorBg      = color.Black

	RgbPingHighlight  = color.DimGray
	RgbPingLineNormal = color.Charcoal
	RgbPingGridNormal = color.DimGray
	RgbPingOrange     = color.DarkAmber
	RgbPingGreen      = color.BlackGreen
	RgbPingRed        = color.BlackRed
	RgbPingBlue       = color.DeepNavy
	RgbCursorNormal   = color.Orange
	RgbCursorInsert   = color.White

	// Boost glow effect
	RgbBoostGlow = color.HotPink

	// Splash colors
	RgbSplashWhite = color.DimSilver
	RgbSplashCyan  = color.VibrantCyan

	// Nugget colors
	RgbNuggetOrange = color.Orange
	RgbNuggetDark   = color.SaddleBrown
	RgbCursorError  = color.Red
	RgbTrailGray    = color.LightGray

	// Status bar backgrounds
	RgbModeNormalBg  = color.LightSkyBlue
	RgbModeVisualBg  = color.PaleGold
	RgbModeInsertBg  = color.LightGreen
	RgbModeSearchBg  = color.Orange
	RgbModeCommandBg = color.MutedPurple
	RgbEnergyBg      = color.White
	RgbBoostBg       = color.Pink
	RgbStatusText    = color.Black

	// Runtime Metrics Backgrounds
	RgbFpsBg = color.Cyan
	RgbGtBg  = color.PaleGold
	RgbApmBg = color.LimeGreen

	// Cleaner colors
	RgbCleanerBasePositive = color.Yellow
	RgbCleanerBaseNegative = color.MediumPurple
	RgbCleanerBaseNugget   = color.Vermilion

	// Flash colors
	RgbRemovalFlash = color.Ivory

	// Explosion gradient (Neon/Cyber theme)
	RgbExplosionCore = color.IceCyan
	RgbExplosionMid  = color.Cyan
	RgbExplosionEdge = color.DeepIndigo

	// Combat
	RgbCombatEnraged  = color.BrightRed
	RgbCombatHitFlash = color.Yellow

	// Quasar
	RgbQuasarShield = color.VibrantCyan

	// Swarm
	RgbSwarmChargeLine = color.LightOrchid
	RgbSwarmTeleport   = color.SoftLavender

	// Orb sigil colors
	RgbOrbRod       = color.BrightCyan
	RgbOrbLauncher  = color.TigerOrange
	RgbOrbDisruptor = color.MintGreen
	RgbOrbFlash     = color.White

	// Orb corona colors (dimmer than sigil for glow effect)
	RgbOrbCoronaRod       = color.DimCyan
	RgbOrbCoronaLauncher  = color.BurntOrange
	RgbOrbCoronaDisruptor = color.SageGreen

	// Parent Missile: Chrome/White
	RgbMissileParentBody       = color.White
	RgbMissileParentTrailStart = color.NearWhite
	RgbMissileParentTrailEnd   = color.SlateGray

	// Child Missile: Deep Orange
	RgbMissileChildBody       = color.TigerOrange
	RgbMissileChildTrailStart = color.Bronze
	RgbMissileChildTrailEnd   = color.Sienna

	// Missile impact explosion (warm palette)
	RgbMissileExplosionCore = color.Ivory
	RgbMissileExplosionMid  = color.WarmOrange
	RgbMissileExplosionEdge = color.Rust

	// Pulse effect colors (polarity-based)
	RgbPulsePositive = color.Buttercream
	RgbPulseNegative = color.Orchid

	// Audio indicator colors
	RgbAudioBothOff     = color.IndianRed
	RgbAudioMusicOnly   = color.OliveYellow
	RgbAudioEffectsOnly = color.LeafGreen
	RgbAudioBothOn      = color.MediumBlue

	// Energy meter blink colors
	RgbEnergyBlinkBlue  = color.BabyBlue
	RgbEnergyBlinkGreen = color.PaleGreen
	RgbEnergyBlinkRed   = color.LightCoral
	RgbEnergyBlinkWhite = color.White

	// Overlay colors
	RgbOverlayBorder    = color.Cyan
	RgbOverlayBg        = color.Obsidian
	RgbOverlayText      = color.White
	RgbOverlayTitle     = color.Yellow
	RgbOverlayHeader    = color.Gold
	RgbOverlayKey       = color.Silver
	RgbOverlayValue     = color.PastelGreen
	RgbOverlayHint      = color.Gray
	RgbOverlaySeparator = color.IronGray

	// Status bar auxiliary colors
	RgbColorModeIndicator = color.LightGray
	RgbGridTimerFg        = color.White
	RgbLastCommandText    = color.Yellow
	RgbSearchInputText    = color.White
	RgbCommandInputText   = color.White
	RgbStatusMessageText  = color.LightGray

	// Wall colors
	RgbWallDefault = color.SlateGray
	RgbWallStone   = color.Taupe
	RgbWallMetal   = color.CoolSilver
	RgbWallEnergy  = color.DeepPurple
	RgbWallDanger  = color.Oxblood
	RgbWallGhost   = color.DarkSlate

	// Shield
	RgbLootShieldBorder = color.PalePink

	// Loot glow colors
	RgbLootRodGlow       = color.PaleCyan
	RgbLootLauncherGlow  = color.PaleLemon
	RgbLootDisruptorGlow = color.PaleMint
	RgbLootEnergyGlow    = color.HotMagenta
	RgbLootEnergySigil   = color.LemonYellow
	RgbLootHeatSigil     = color.Coral
	RgbLootHeatGlow      = color.LightRose

	// Storm attack effect colors
	RgbStormGreenPulse = color.EmeraldGreen
	RgbStormRedCone    = color.RedOrange

	// Bullet colors
	RgbBulletStormRed    = color.RoseRed
	RgbBulletStormRedDim = color.DarkRust

	// Muzzle flash colors
	RgbMuzzleFlashBase = color.Mango
	RgbMuzzleFlashTip  = color.Chocolate

	// Ember: Low heat (warm orange-red)
	RgbEmberCoreLow = color.Apricot
	RgbEmberMidLow  = color.FlameOrange
	RgbEmberEdgeLow = color.BurntSienna
	RgbEmberRingLow = color.DarkPlum

	// Ember: High heat (white-hot with blue tinge)
	RgbEmberCoreHigh = color.AliceBlue
	RgbEmberMidHigh  = color.White
	RgbEmberEdgeHigh = color.Terracotta
	RgbEmberRingHigh = color.BlueCharcoal
)

// StormCircleColors - neon base colors (saturated 1.3x in renderer)
var StormCircleColors = []color.RGB{
	color.BrightLime, // Lime
	color.RoseRed,    // Magenta
	color.DodgerBlue, // Cyan
}

// LightningTrueColorLUT is TrueColor gradient endpoints per lightning color type
// Index by LightningColorType to get (core, hot) RGB pair
// Core = base color at end of life, Hot = bright color at full life
var LightningTrueColorLUT = [5][2]color.RGB{
	{color.VibrantCyan, color.White},       // Cyan
	{color.Brick, color.MistyRose},         // Red
	{color.DarkGold, color.Cream},          // Gold
	{color.MediumGreen, color.Honeydew},    // Green
	{color.DarkViolet, color.PaleLavender}, // Purple
}

// GlyphColorLUT maps [GlyphType][GlyphLevel] to RGB
// Type indices: 0=Green, 1=Blue, 2=Red, 3=White, 4=Gold
// Level indices: 0=Dark, 1=Normal, 2=Bright
var GlyphColorLUT = [5][3]color.RGB{
	{RgbGlyphGreenDark, RgbGlyphGreenNormal, RgbGlyphGreenBright},
	{RgbGlyphBlueDark, RgbGlyphBlueNormal, RgbGlyphBlueBright},
	{RgbGlyphRedDark, RgbGlyphRedNormal, RgbGlyphRedBright},
	{RgbGlyphWhite, RgbGlyphWhite, RgbGlyphWhite},
	{RgbGlyphGold, RgbGlyphGold, RgbGlyphGold},
}


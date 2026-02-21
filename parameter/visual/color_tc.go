package visual

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// terminal.RGB color definitions for all game systems
var (
	// General colors for various uses
	RgbBlack   = terminal.Black
	RgbWhite   = terminal.White
	RgbRed     = terminal.Red
	RgbOrange  = terminal.Amber
	RgbYellow  = terminal.Yellow
	RgbGreen   = terminal.Lime
	RgbCyan    = terminal.Cyan
	RgbBlue    = terminal.Blue
	RgbMagenta = terminal.Magenta

	RgbSoftGold       = terminal.PaleGold
	RgbElectricViolet = terminal.ElectricViolet

	RgbBackground = terminal.Gunmetal // Tokyo Night background

	// Glyph colors - all dark/normal/bright levels have minimum floor to prevent perceptual blackout at low alpha
	RgbGlyphBlueDark   = terminal.CobaltBlue
	RgbGlyphBlueNormal = terminal.Cornflower
	RgbGlyphBlueBright = terminal.LightBlue

	RgbGlyphGreenDark   = terminal.DarkGreen
	RgbGlyphGreenNormal = terminal.BrightGreen
	RgbGlyphGreenBright = terminal.NeonGreen

	RgbGlyphRedDark   = terminal.Brick
	RgbGlyphRedNormal = terminal.BrightRed
	RgbGlyphRedBright = terminal.Salmon

	RgbGlyphGold  = terminal.Yellow
	RgbGlyphWhite = terminal.White

	RgbDecay       = terminal.Teal
	RgbBlossom     = terminal.LightPink
	RgbDrain       = terminal.VibrantCyan
	RgbMaterialize = terminal.BrightCyan

	RgbDustDark   = terminal.DarkGray
	RgbDustNormal = terminal.MidGray
	RgbDustBright = terminal.LightGray

	RgbIndicator           = terminal.Silver
	RgbStatusBar           = terminal.White
	RgbTruncateIndicator   = terminal.Black
	RgbTruncateIndicatorBg = terminal.Orange
	RgbStatusCursor        = terminal.Orange
	RgbStatusCursorBg      = terminal.Black

	RgbPingHighlight  = terminal.DimGray
	RgbPingLineNormal = terminal.Charcoal
	RgbPingGridNormal = terminal.DimGray
	RgbPingOrange     = terminal.DarkAmber
	RgbPingGreen      = terminal.BlackGreen
	RgbPingRed        = terminal.BlackRed
	RgbPingBlue       = terminal.DeepNavy
	RgbCursorNormal   = terminal.Orange
	RgbCursorInsert   = terminal.White

	// Boost glow effect
	RgbBoostGlow = terminal.HotPink

	// Splash colors
	RgbSplashWhite = terminal.DimSilver
	RgbSplashCyan  = terminal.VibrantCyan

	// Nugget colors
	RgbNuggetOrange = terminal.Orange
	RgbNuggetDark   = terminal.SaddleBrown
	RgbCursorError  = terminal.Red
	RgbTrailGray    = terminal.LightGray

	// Status bar backgrounds
	RgbModeNormalBg  = terminal.LightSkyBlue
	RgbModeVisualBg  = terminal.PaleGold
	RgbModeInsertBg  = terminal.LightGreen
	RgbModeSearchBg  = terminal.Orange
	RgbModeCommandBg = terminal.MutedPurple
	RgbEnergyBg      = terminal.White
	RgbBoostBg       = terminal.Pink
	RgbStatusText    = terminal.Black

	// Runtime Metrics Backgrounds
	RgbFpsBg = terminal.Cyan
	RgbGtBg  = terminal.PaleGold
	RgbApmBg = terminal.LimeGreen

	// Cleaner colors
	RgbCleanerBasePositive = terminal.Yellow
	RgbCleanerBaseNegative = terminal.MediumPurple
	RgbCleanerBaseNugget   = terminal.Vermilion

	// Flash colors
	RgbRemovalFlash = terminal.Ivory

	// Explosion gradient (Neon/Cyber theme)
	RgbExplosionCore = terminal.IceCyan
	RgbExplosionMid  = terminal.Cyan
	RgbExplosionEdge = terminal.DeepIndigo

	// Combat
	RgbCombatEnraged  = terminal.BrightRed
	RgbCombatHitFlash = terminal.Yellow

	// Quasar
	RgbQuasarShield = terminal.VibrantCyan

	// Swarm
	RgbSwarmChargeLine = terminal.LightOrchid
	RgbSwarmTeleport   = terminal.SoftLavender

	// Orb sigil colors
	RgbOrbRod       = terminal.BrightCyan
	RgbOrbLauncher  = terminal.TigerOrange
	RgbOrbDisruptor = terminal.MintGreen
	RgbOrbFlash     = terminal.White

	// Orb corona colors (dimmer than sigil for glow effect)
	RgbOrbCoronaRod       = terminal.DimCyan
	RgbOrbCoronaLauncher  = terminal.BurntOrange
	RgbOrbCoronaDisruptor = terminal.SageGreen

	// Parent Missile: Chrome/White
	RgbMissileParentBody       = terminal.White
	RgbMissileParentTrailStart = terminal.NearWhite
	RgbMissileParentTrailEnd   = terminal.SlateGray

	// Child Missile: Deep Orange
	RgbMissileChildBody       = terminal.TigerOrange
	RgbMissileChildTrailStart = terminal.Bronze
	RgbMissileChildTrailEnd   = terminal.Sienna

	// Missile impact explosion (warm palette)
	RgbMissileExplosionCore = terminal.Ivory
	RgbMissileExplosionMid  = terminal.WarmOrange
	RgbMissileExplosionEdge = terminal.Rust

	// Pulse effect colors (polarity-based)
	RgbPulsePositive = terminal.Buttercream
	RgbPulseNegative = terminal.Orchid

	// Audio indicator colors
	RgbAudioBothOff     = terminal.IndianRed
	RgbAudioMusicOnly   = terminal.OliveYellow
	RgbAudioEffectsOnly = terminal.LeafGreen
	RgbAudioBothOn      = terminal.MediumBlue

	// Energy meter blink colors
	RgbEnergyBlinkBlue  = terminal.BabyBlue
	RgbEnergyBlinkGreen = terminal.PaleGreen
	RgbEnergyBlinkRed   = terminal.LightCoral
	RgbEnergyBlinkWhite = terminal.White

	// Overlay colors
	RgbOverlayBorder    = terminal.Cyan
	RgbOverlayBg        = terminal.Obsidian
	RgbOverlayText      = terminal.White
	RgbOverlayTitle     = terminal.Yellow
	RgbOverlayHeader    = terminal.Gold
	RgbOverlayKey       = terminal.Silver
	RgbOverlayValue     = terminal.PastelGreen
	RgbOverlayHint      = terminal.Gray
	RgbOverlaySeparator = terminal.IronGray

	// Status bar auxiliary colors
	RgbColorModeIndicator = terminal.LightGray
	RgbGridTimerFg        = terminal.White
	RgbLastCommandText    = terminal.Yellow
	RgbSearchInputText    = terminal.White
	RgbCommandInputText   = terminal.White
	RgbStatusMessageText  = terminal.LightGray

	// Wall colors
	RgbWallDefault = terminal.SlateGray
	RgbWallStone   = terminal.Taupe
	RgbWallMetal   = terminal.CoolSilver
	RgbWallEnergy  = terminal.DeepPurple
	RgbWallDanger  = terminal.Oxblood
	RgbWallGhost   = terminal.DarkSlate

	// Shield
	RgbLootShieldBorder = terminal.PalePink

	// Loot glow colors
	RgbLootRodGlow       = terminal.PaleCyan
	RgbLootLauncherGlow  = terminal.PaleLemon
	RgbLootDisruptorGlow = terminal.PaleMint
	RgbLootEnergyGlow    = terminal.HotMagenta
	RgbLootEnergySigil   = terminal.LemonYellow
	RgbLootHeatSigil     = terminal.Coral
	RgbLootHeatGlow      = terminal.LightRose

	// Storm attack effect colors
	RgbStormGreenPulse = terminal.EmeraldGreen
	RgbStormRedCone    = terminal.RedOrange

	// Bullet colors
	RgbBulletStormRed    = terminal.RoseRed
	RgbBulletStormRedDim = terminal.DarkRust

	// Muzzle flash colors
	RgbMuzzleFlashBase = terminal.Mango
	RgbMuzzleFlashTip  = terminal.Chocolate

	// Ember: Low heat (warm orange-red)
	RgbEmberCoreLow = terminal.Apricot
	RgbEmberMidLow  = terminal.FlameOrange
	RgbEmberEdgeLow = terminal.BurntSienna
	RgbEmberRingLow = terminal.DarkPlum

	// Ember: High heat (white-hot with blue tinge)
	RgbEmberCoreHigh = terminal.AliceBlue
	RgbEmberMidHigh  = terminal.White
	RgbEmberEdgeHigh = terminal.Terracotta
	RgbEmberRingHigh = terminal.BlueCharcoal
)

// StormCircleColors - neon base colors (saturated 1.3x in renderer)
var StormCircleColors = []terminal.RGB{
	terminal.BrightLime, // Lime
	terminal.RoseRed,    // Magenta
	terminal.DodgerBlue, // Cyan
}

// LightningTrueColorLUT is TrueColor gradient endpoints per lightning color type
// Index by LightningColorType to get (core, hot) RGB pair
// Core = base color at end of life, Hot = bright color at full life
var LightningTrueColorLUT = [5][2]terminal.RGB{
	{terminal.VibrantCyan, terminal.White},       // Cyan
	{terminal.Brick, terminal.MistyRose},         // Red
	{terminal.DarkGold, terminal.Cream},          // Gold
	{terminal.MediumGreen, terminal.Honeydew},    // Green
	{terminal.DarkViolet, terminal.PaleLavender}, // Purple
}

// GlyphColorLUT maps [GlyphType][GlyphLevel] to RGB
// Type indices: 0=Green, 1=Blue, 2=Red, 3=White, 4=Gold
// Level indices: 0=Dark, 1=Normal, 2=Bright
var GlyphColorLUT = [5][3]terminal.RGB{
	{RgbGlyphGreenDark, RgbGlyphGreenNormal, RgbGlyphGreenBright},
	{RgbGlyphBlueDark, RgbGlyphBlueNormal, RgbGlyphBlueBright},
	{RgbGlyphRedDark, RgbGlyphRedNormal, RgbGlyphRedBright},
	{RgbGlyphWhite, RgbGlyphWhite, RgbGlyphWhite},
	{RgbGlyphGold, RgbGlyphGold, RgbGlyphGold},
}
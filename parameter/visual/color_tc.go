package visual

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// terminal.RGB color definitions for all game systems
var (
	// General colors for various uses
	RgbBlack   = Black
	RgbWhite   = White
	RgbRed     = Red
	RgbOrange  = Amber
	RgbYellow  = Yellow
	RgbGreen   = Lime
	RgbCyan    = Cyan
	RgbBlue    = Blue
	RgbMagenta = Magenta

	RgbSoftGold       = PaleGold
	RgbElectricViolet = ElectricViolet

	RgbBackground = Gunmetal // Tokyo Night background

	// Glyph colors - all dark/normal/bright levels have minimum floor to prevent perceptual blackout at low alpha
	RgbGlyphBlueDark   = CobaltBlue
	RgbGlyphBlueNormal = Cornflower
	RgbGlyphBlueBright = LightBlue

	RgbGlyphGreenDark   = DarkGreen
	RgbGlyphGreenNormal = BrightGreen
	RgbGlyphGreenBright = NeonGreen

	RgbGlyphRedDark   = Brick
	RgbGlyphRedNormal = BrightRed
	RgbGlyphRedBright = Salmon

	RgbGlyphGold  = Yellow
	RgbGlyphWhite = White

	RgbDecay       = Teal
	RgbBlossom     = LightPink
	RgbDrain       = VibrantCyan
	RgbMaterialize = BrightCyan

	RgbDustDark   = DarkGray
	RgbDustNormal = MidGray
	RgbDustBright = LightGray

	RgbIndicator           = Silver
	RgbStatusBar           = White
	RgbTruncateIndicator   = Black
	RgbTruncateIndicatorBg = Orange
	RgbStatusCursor        = Orange
	RgbStatusCursorBg      = Black

	RgbPingHighlight  = DimGray
	RgbPingLineNormal = Charcoal
	RgbPingGridNormal = DimGray
	RgbPingOrange     = DarkAmber
	RgbPingGreen      = BlackGreen
	RgbPingRed        = BlackRed
	RgbPingBlue       = DeepNavy
	RgbCursorNormal   = Orange
	RgbCursorInsert   = White

	// Boost glow effect
	RgbBoostGlow = HotPink

	// Splash colors
	RgbSplashWhite = DimSilver
	RgbSplashCyan  = VibrantCyan

	// Nugget colors
	RgbNuggetOrange = Orange
	RgbNuggetDark   = SaddleBrown
	RgbCursorError  = Red
	RgbTrailGray    = LightGray

	// Status bar backgrounds
	RgbModeNormalBg  = LightSkyBlue
	RgbModeVisualBg  = PaleGold
	RgbModeInsertBg  = LightGreen
	RgbModeSearchBg  = Orange
	RgbModeCommandBg = MutedPurple
	RgbEnergyBg      = White
	RgbBoostBg       = Pink
	RgbStatusText    = Black

	// Runtime Metrics Backgrounds
	RgbFpsBg = Cyan
	RgbGtBg  = PaleGold
	RgbApmBg = LimeGreen

	// Cleaner colors
	RgbCleanerBasePositive = Yellow
	RgbCleanerBaseNegative = MediumPurple
	RgbCleanerBaseNugget   = Vermilion

	// Flash colors
	RgbRemovalFlash = Ivory

	// Explosion gradient (Neon/Cyber theme)
	RgbExplosionCore = IceCyan
	RgbExplosionMid  = Cyan
	RgbExplosionEdge = DeepIndigo

	// Combat
	RgbCombatEnraged  = BrightRed
	RgbCombatHitFlash = Yellow

	// Quasar
	RgbQuasarShield = VibrantCyan

	// Swarm
	RgbSwarmChargeLine = LightOrchid
	RgbSwarmTeleport   = SoftLavender

	// Orb sigil colors
	RgbOrbRod       = BrightCyan
	RgbOrbLauncher  = TigerOrange
	RgbOrbDisruptor = MintGreen
	RgbOrbFlash     = White

	// Orb corona colors (dimmer than sigil for glow effect)
	RgbOrbCoronaRod       = DimCyan
	RgbOrbCoronaLauncher  = BurntOrange
	RgbOrbCoronaDisruptor = SageGreen

	// Parent Missile: Chrome/White
	RgbMissileParentBody       = White
	RgbMissileParentTrailStart = NearWhite
	RgbMissileParentTrailEnd   = SlateGray

	// Child Missile: Deep Orange
	RgbMissileChildBody       = TigerOrange
	RgbMissileChildTrailStart = Bronze
	RgbMissileChildTrailEnd   = Sienna

	// Missile impact explosion (warm palette)
	RgbMissileExplosionCore = Ivory
	RgbMissileExplosionMid  = WarmOrange
	RgbMissileExplosionEdge = Rust

	// Pulse effect colors (polarity-based)
	RgbPulsePositive = Buttercream
	RgbPulseNegative = Orchid

	// Audio indicator colors
	RgbAudioBothOff     = IndianRed
	RgbAudioMusicOnly   = OliveYellow
	RgbAudioEffectsOnly = LeafGreen
	RgbAudioBothOn      = MediumBlue

	// Energy meter blink colors
	RgbEnergyBlinkBlue  = BabyBlue
	RgbEnergyBlinkGreen = PaleGreen
	RgbEnergyBlinkRed   = LightCoral
	RgbEnergyBlinkWhite = White

	// Overlay colors
	RgbOverlayBorder    = Cyan
	RgbOverlayBg        = Obsidian
	RgbOverlayText      = White
	RgbOverlayTitle     = Yellow
	RgbOverlayHeader    = Gold
	RgbOverlayKey       = Silver
	RgbOverlayValue     = PastelGreen
	RgbOverlayHint      = Gray
	RgbOverlaySeparator = IronGray

	// Status bar auxiliary colors
	RgbColorModeIndicator = LightGray
	RgbGridTimerFg        = White
	RgbLastCommandText    = Yellow
	RgbSearchInputText    = White
	RgbCommandInputText   = White
	RgbStatusMessageText  = LightGray

	// Wall colors
	RgbWallDefault = SlateGray
	RgbWallStone   = Taupe
	RgbWallMetal   = CoolSilver
	RgbWallEnergy  = DeepPurple
	RgbWallDanger  = Oxblood
	RgbWallGhost   = DarkSlate

	// Shield
	RgbLootShieldBorder = PalePink

	// Loot glow colors
	RgbLootRodGlow       = PaleCyan
	RgbLootLauncherGlow  = PaleLemon
	RgbLootDisruptorGlow = PaleMint
	RgbLootEnergyGlow    = HotMagenta
	RgbLootEnergySigil   = LemonYellow
	RgbLootHeatSigil     = Coral
	RgbLootHeatGlow      = LightRose

	// Storm attack effect colors
	RgbStormGreenPulse = EmeraldGreen
	RgbStormRedCone    = RedOrange

	// Bullet colors
	RgbBulletStormRed    = RoseRed
	RgbBulletStormRedDim = DarkRust

	// Muzzle flash colors
	RgbMuzzleFlashBase = Mango
	RgbMuzzleFlashTip  = Chocolate

	// Ember: Low heat (warm orange-red)
	RgbEmberCoreLow = Apricot
	RgbEmberMidLow  = FlameOrange
	RgbEmberEdgeLow = BurntSienna
	RgbEmberRingLow = DarkPlum

	// Ember: High heat (white-hot with blue tinge)
	RgbEmberCoreHigh = AliceBlue
	RgbEmberMidHigh  = White
	RgbEmberEdgeHigh = Terracotta
	RgbEmberRingHigh = BlueCharcoal
)

// StormCircleColors - neon base colors (saturated 1.3x in renderer)
var StormCircleColors = []terminal.RGB{
	BrightLime, // Lime
	RoseRed,    // Magenta
	DodgerBlue, // Cyan
}

// LightningTrueColorLUT is TrueColor gradient endpoints per lightning color type
// Index by LightningColorType to get (core, hot) RGB pair
// Core = base color at end of life, Hot = bright color at full life
var LightningTrueColorLUT = [5][2]terminal.RGB{
	{VibrantCyan, White},       // Cyan
	{Brick, MistyRose},         // Red
	{DarkGold, Cream},          // Gold
	{MediumGreen, Honeydew},    // Green
	{DarkViolet, PaleLavender}, // Purple
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
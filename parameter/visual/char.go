package visual

// QuadrantChars provides 2x2 sub-cell resolution for TrueColor mode
// Bitmap encoding: bit0=UL, bit1=UR, bit2=LL, bit3=LR
// Layout: [UL][UR]
//
//	[LL][LR]
var QuadrantChars = [16]rune{
	' ', // 0000 - empty
	'▘', // 0001 - upper-left
	'▝', // 0010 - upper-right
	'▀', // 0011 - upper half
	'▖', // 0100 - lower-left
	'▌', // 0101 - left half
	'▞', // 0110 - anti-diagonal
	'▛', // 0111 - UL + UR + LL
	'▗', // 1000 - lower-right
	'▚', // 1001 - diagonal
	'▐', // 1010 - right half
	'▜', // 1011 - UL + UR + LR
	'▄', // 1100 - lower half
	'▙', // 1101 - UL + LL + LR
	'▟', // 1110 - UR + LL + LR
	'█', // 1111 - full block
}

// Half256Chars provides vertical half-cell resolution for 256-color mode
// CP437 block characters compatible with naked TTY
// Uses Unicode block characters (equivalent to CP437 visuals)
// Bitmap encoding: bit0=top, bit1=bottom
var Half256Chars = [4]rune{
	' ',      // 00 - empty
	'\u2580', // 01 - top half only (▀) - was 223
	'\u2584', // 10 - bottom half only (▄) - was 220
	'\u2588', // 11 - both halves (█) - was 219
}

// Horizontal256Chars provides horizontal half-cell characters
// Reserved for future horizontal sub-pixel support
var Horizontal256Chars = [2]rune{
	'\u258C', // ▌ - left half - was 221
	'\u258E', // ▐ - right half - was 222
}

// Density256Chars provides intensity variants for trail, glow effects, ordered from lowest to highest density
var Density256Chars = [4]rune{
	'\u2591', // ░ - light shade (25%) - was 176
	'\u2592', // ▒ - medium shade (50%) - was 177
	'\u2593', // ▓ - dark shade (75%) - was 178
	'\u2588', // █ - full block (100%) - was 219
}

// Missile characters
const (
	MissileTrailChar  = '▪' // U+25AA Black Small Square
	MissileParentChar = '◆' // U+25C6 Black Diamond
	MissileSeekerChar = '▸' // U+25B8 Black Right-Pointing Small Triangle
)

// Circle characters
const (
	// Standard Geometric Shapes (U+25CB to U+25CF)
	CircleWhite        = '○' // U+25CB White Circle
	CircleDotted       = '◌' // U+25CC Dotted Circle
	CircleWithVertical = '◍' // U+25CD Circle with Vertical Fill
	CircleBullsEye     = '◎' // U+25CE Bullseye
	CircleBlock        = '●' // U+25CF Black Circle

	// Halves and Quadrants (Filled) (U+25D0 to U+25D5)
	CircleLeftHalfBlack  = '◐' // U+25D0 Circle with Left Half Black
	CircleRightHalfBlack = '◑' // U+25D1 Circle with Right Half Black
	CircleLowerHalfBlack = '◒' // U+25D2 Circle with Lower Half Black
	CircleUpperHalfBlack = '◓' // U+25D3 Circle with Upper Half Black
	CircleQuadrantBlack  = '◔' // U+25D4 Circle with Upper Right Quadrant Black
	CircleThreeQuarters  = '◕' // U+25D5 Circle with All But Upper Left Quadrant Black

	// Semi-Circles (U+25D6 to U+25D7)
	CircleLeftHalfSolid  = '◖' // U+25D6 Left Half Black Circle
	CircleRightHalfSolid = '◗' // U+25D7 Right Half Black Circle

	// Large/Small Variants (U+25EF, U+25E6)
	CircleLargeWhite = '◯' // U+25EF Large Circle
	CircleSmallWhite = '◦' // U+25E6 White Bullet

	// Quadrants (Outline/White) (U+25F4 to U+25F7)
	CircleQuadrantULWhite = '◴' // U+25F4 White Circle with Upper Left Quadrant
	CircleQuadrantLLWhite = '◵' // U+25F5 White Circle with Lower Left Quadrant
	CircleQuadrantLRWhite = '◶' // U+25F6 White Circle with Lower Right Quadrant
	CircleQuadrantURWhite = '◷' // U+25F7 White Circle with Upper Right Quadrant

	// Extended & Symbols (Commonly used circle glyphs)
	CircleMediumWhite = '⚪' // U+26AA Medium White Circle
	CircleMediumBlack = '⚫' // U+26AB Medium Black Circle
	CircleLargeBlack  = '⬤' // U+2B24 Black Large Circle
	CircleHeavy       = '⭕' // U+2B55 Heavy Large Circle
	CircleShadowed    = '❍' // U+274D Shadowed White Circle
)

// Single-line box drawing characters
const (
	BorderSingleHorizontal     = '─' // U+2500
	BorderSingleVertical       = '│' // U+2502
	BorderSingleTopLeft        = '┌' // U+250C
	BorderSingleTopRight       = '┐' // U+2510
	BorderSingleBottomLeft     = '└' // U+2514
	BorderSingleBottomRight    = '┘' // U+2518
	BorderSingleVerticalRight  = '├' // U+251C
	BorderSingleVerticalLeft   = '┤' // U+2524
	BorderSingleHorizontalDown = '┬' // U+252C
	BorderSingleHorizontalUp   = '┴' // U+2534
	BorderSingleCross          = '┼' // U+253C
)

// Double-line box drawing characters
const (
	BorderDoubleHorizontal     = '═' // U+2550
	BorderDoubleVertical       = '║' // U+2551
	BorderDoubleTopLeft        = '╔' // U+2554
	BorderDoubleTopRight       = '╗' // U+2557
	BorderDoubleBottomLeft     = '╚' // U+255A
	BorderDoubleBottomRight    = '╝' // U+255D
	BorderDoubleVerticalRight  = '╠' // U+2560
	BorderDoubleVerticalLeft   = '╣' // U+2563
	BorderDoubleHorizontalDown = '╦' // U+2566
	BorderDoubleHorizontalUp   = '╩' // U+2569
	BorderDoubleCross          = '╬' // U+256C
)

// BoxDrawSingleLUT maps neighbor mask to single-line box character
var BoxDrawSingleLUT = [16]rune{
	0:  '┼', // Isolated
	1:  '│', // N
	2:  '─', // E
	3:  '└', // N+E
	4:  '│', // S
	5:  '│', // N+S
	6:  '┌', // E+S
	7:  '├', // N+E+S
	8:  '─', // W
	9:  '┘', // N+W
	10: '─', // E+W
	11: '┴', // N+E+W
	12: '┐', // S+W
	13: '┤', // N+S+W
	14: '┬', // E+S+W
	15: '┼', // All
}

// BoxDrawDoubleLUT maps neighbor mask to double-line box character
var BoxDrawDoubleLUT = [16]rune{
	0:  '╬', // Isolated
	1:  '║', // N
	2:  '═', // E
	3:  '╚', // N+E
	4:  '║', // S
	5:  '║', // N+S
	6:  '╔', // E+S
	7:  '╠', // N+E+S
	8:  '═', // W
	9:  '╝', // N+W
	10: '═', // E+W
	11: '╩', // N+E+W
	12: '╗', // S+W
	13: '╣', // N+S+W
	14: '╦', // E+S+W
	15: '╬', // All
}
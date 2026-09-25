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

// DensityChars are shade glyphs ordered from lowest to highest density
var DensityChars = [4]rune{'░', '▒', '▓', '█'}

// Density256Chars is DensityChars from glyphs every console font carries; they lack ▓
var Density256Chars = [4]rune{'·', '░', '▒', '█'}

// Missile characters
const (
	MissileTrailChar = '▪' // U+25AA Black Small Square
	// MissileTrailChar256 is in every console font; ▪ is not
	MissileTrailChar256 = '•'
)

// OrbFullChar256 is a full orb's glyph in 256 colors, where console fonts lack CircleBullsEye
const OrbFullChar256 = '@'

// Heading glyphs, indexed by heading: E W S N SE NE SW NW, then at rest. The 256 sets hold
// only glyphs every console font carries, and those fonts have no diagonal triangles.
var (
	MissileHeadChars    = [9]rune{'▸', '◂', '▾', '▴', '◢', '◥', '◣', '◤', '▸'}
	MissileHeadChars256 = [9]rune{'▶', '◀', '▼', '▲', '\\', '/', '/', '\\', '▶'}
	BulletHeadChars     = [9]rune{'▸', '◂', '▾', '▴', '◢', '◥', '◣', '◤', '•'}
	BulletHeadChars256  = [9]rune{'-', '-', '|', '|', '\\', '/', '/', '\\', '*'}
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

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
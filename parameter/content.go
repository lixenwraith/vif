package parameter

// AlphanumericRunes contains all alphanumeric characters as runes
var AlphanumericRunes = []rune{
	'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
	'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
	'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
	'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
}

// Content block shaping
const (
	// ContentMinBlockLines is the minimum lines for a block to be emitted
	ContentMinBlockLines = 2

	// ContentMaxBlockLines is the maximum lines in a single block
	ContentMaxBlockLines = 5

	// ContentIndentDelta is the indent shift in columns that starts a new block
	ContentIndentDelta = 2

	// TabWidth is the column expansion applied to a leading tab
	TabWidth = 4
)

// Content ingest limits
const (
	// ContentMaxLineRunes caps a stored line; glyph placement crops to map width
	ContentMaxLineRunes = 256

	// ContentMaxFileBytes skips any single content file larger than this
	ContentMaxFileBytes = 4 << 20

	// ContentMaxCorpusBytes stops ingest once the loaded corpus exceeds this
	ContentMaxCorpusBytes = 32 << 20

	// ContentMaxFiles caps the files admitted from one corpus directory
	ContentMaxFiles = 256
)

// ContentCommentPrefixes marks lines dropped during plain-text ingest
var ContentCommentPrefixes = []string{"//", "#", "/*"}

package visual

var (
	// DrainChar is the character used to render the drain entity
	DrainChar = BorderDoubleCross

	// ButtonLit256 fills a lit button on a console: eight backgrounds cannot show the
	// lighter entry of its hue, a foreground block can
	ButtonLit256 = DensityChars[3]
)

// ButtonUnlit is the share of its colour an unlit button keeps: dim beside a lit one,
// yet light enough for its black key to read on red and blue
const ButtonUnlit = 0.6

package parameter

// The gun's tracers: a streak's speed by axis and its trail
const (
	TracerHorizontalSpeed = 80.0
	TracerVerticalSpeed   = 40.0

	// TracerTrailLength is the number of previous positions tracked for the fade trail effect
	TracerTrailLength = 10.0

	// TracerTrailHeadIntensity and TracerTrailTailIntensity bound the
	// trail gradient: the head is bright, the tail short and soft
	TracerTrailHeadIntensity = 0.9
	TracerTrailTailIntensity = 0.12
)

// TracerLaneOffsets are the row or column offsets a volley covers, by charge count
var TracerLaneOffsets = [...][]int{{0}, {-1, 1}, {-1, 0, 1}}

package audio

// Intensity selects a registered arrangement tier. The engine carries no APM
// or gameplay concept: the embedder maps its own signal to a tier and
// registers the pattern set for each one at wiring time.
type Intensity int32

const (
	IntensityCalm Intensity = iota
	IntensityNormal
	IntensityElevated
	IntensityIntense
	IntensityPeak
	IntensityCount
)

var intensityNames = [...]string{"calm", "normal", "elevated", "intense", "peak"}

func (i Intensity) String() string {
	if i >= 0 && int(i) < len(intensityNames) {
		return intensityNames[i]
	}
	return "unknown"
}

// Arrangement is the pattern set for one tier; slot 2 stays free for the
// auto-fill bank and embedder use
type Arrangement struct {
	Rhythm PatternID
	Melody PatternID
}

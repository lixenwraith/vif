package audio

import "github.com/lixenwraith/vi-fighter/pkg/audio/model"

// Intensity selects a registered arrangement tier. The engine carries no APM
// or gameplay concept: the embedder maps its own signal to a tier and
// registers the pattern set for each one at wiring time.
type Intensity = model.Intensity

const (
	IntensityCalm     = model.IntensityCalm
	IntensityNormal   = model.IntensityNormal
	IntensityElevated = model.IntensityElevated
	IntensityIntense  = model.IntensityIntense
	IntensityPeak     = model.IntensityPeak
	IntensityCount    = model.IntensityCount
)

// Arrangement names one tier's rhythm and melody pools; slot 2 stays free for the
// auto-fill bank and embedder use. Start resolves the names, and the sequencer
// draws one member per slot whenever the tier is applied.
type Arrangement struct {
	Rhythm []string
	Melody []string
}

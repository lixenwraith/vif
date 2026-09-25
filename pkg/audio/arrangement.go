package audio

import "github.com/lixenwraith/vi-fighter/pkg/audio/model"

// Intensity selects an arrangement tier. The engine carries no APM or gameplay
// concept: the embedder maps its own signal to a tier, and its patterns name the
// tiers they are drawn at.
type Intensity = model.Intensity

const (
	IntensityCalm     = model.IntensityCalm
	IntensityNormal   = model.IntensityNormal
	IntensityElevated = model.IntensityElevated
	IntensityIntense  = model.IntensityIntense
	IntensityPeak     = model.IntensityPeak
	IntensityCount    = model.IntensityCount
)

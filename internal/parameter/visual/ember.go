package visual

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Precomputed inverse squared radii for ellipse containment
var (
	EmberRadiusX               = parameter.PlayerShieldRadiusX
	EmberRadiusY               = parameter.PlayerShieldRadiusY
	EmberInvRxSq, EmberInvRySq = vmath.EllipseInvRadiiSqF(EmberRadiusX, EmberRadiusY)

	// Precomputed constants
	ExpLUTDecayK      float64 = vmath.ExpLUTDecayK
	PulseFrequency    float64 = 1.8
	PulseAmplitude    float64 = 0.05
	BackFaceThreshold float64 = -0.1
	BackFaceDimming   float64 = 0.25

	// Ember-to-shield transition constants
	EmberTransitionDuration     = 1000 * time.Millisecond
	EmberTransitionRiseRatio    = 0.1 // 10% rise time
	EmberTransitionMaxIntensity = 0.7 // Peak overlay
)

// EmberParams holds interpolated visual parameters for a given heat level
// Colors excluded - interpolated at render time to avoid cyclic dependency
type EmberParams struct {
	// Jagged edge
	JaggedAmp     float64
	JaggedFreq    float64
	JaggedSpeed   float64
	Octave2       float64
	Octave3       float64
	EruptionPower float64

	// Glow layers
	CoreFalloff   float64
	CorePower     float64
	MidFalloff    float64
	MidPower      float64
	MidIntensity  float64
	EdgePower     float64
	EdgeIntensity float64

	// Rings
	RingAlpha   float64
	RingWidth   float64
	RingVisible float64
	RingSpeed   float64

	// Heat factor for color interpolation, from low heat (0) to high heat (1).
	HeatFactor float64
}

// Ember parameter bounds [low heat, high heat]
var (
	emberJaggedAmp     = [2]float64{0.0, 2.0}
	emberJaggedFreq    = [2]float64{4.0, 32.0}
	emberJaggedSpeed   = [2]float64{math.Pi, 6.0 * vmath.TwoPi}
	emberOctave2       = [2]float64{1.0, 1.0}
	emberOctave3       = [2]float64{1.0, 1.0}
	emberEruptionPower = [2]float64{1.5, 16.0}

	emberCoreFalloff   = [2]float64{1.6, 1.5}
	emberCorePower     = [2]float64{1.5, 1.5}
	emberMidFalloff    = [2]float64{1.0, 1.0}
	emberMidPower      = [2]float64{1.0, 1.0}
	emberMidIntensity  = [2]float64{1.0, 1.0}
	emberEdgePower     = [2]float64{0.2, 0.1}
	emberEdgeIntensity = [2]float64{0.2, 0.2}

	emberRingAlpha   = [2]float64{1.0, 0.0}
	emberRingWidth   = [2]float64{0.2, 0.02}
	emberRingVisible = [2]float64{70.0, 0.2}
	emberRingSpeed   = [2]float64{3.0 * vmath.TwoPi, vmath.TwoPi / 5.0}
)

// Ring orbital plane normals (3 rings with different tilts)
// Precomputed for Dyson-sphere effect
const EmberRingCount = 3

// Ring orbital plane normals - matches sandbox calculation exactly
// tilt = (i+0.5)*π/3.5, azimuth = i*2π/3, aspectRatio = 2.1
// Intentionally NOT normalized to match sandbox rz magnitude behavior
var EmberRingNormals = [EmberRingCount][3]float64{
	// i=0: tilt≈25.7°, azimuth=0 → (0.434, 0, 0.901)
	{0.43402786180377007, 0.0, 0.90087118139490485},
	// i=1: tilt≈77.1°, azimuth=2π/3 → (-0.487, 0.402, 0.222)
	{-0.48700084676966071, 0.40197692671790719, 0.22199999983422458},
	// i=2: tilt≈128.6°, azimuth=4π/3 → (-0.394, -0.324, -0.617)
	{-0.39398993481881917, -0.32402567262761295, -0.61702311504632235},
}

// EmberRingVelocities creates differential rotation for interference patterns.
var EmberRingVelocities = [EmberRingCount]float64{
	1.0,
	1.3,
	1.6,
}

// EmberRingPulsePhases - per-ring pulse phase offsets
var EmberRingPulsePhases = [EmberRingCount]float64{
	0.0, 0.7, 1.4,
}

// EmberRingPhaseOffsets staggers ring rotation start positions
var EmberRingPhaseOffsets = [EmberRingCount]float64{
	0.0,
	vmath.TwoPi / 3.0,
	2.0 * vmath.TwoPi / 3.0,
}

// InterpolateEmberParams returns parameters interpolated for given heat (0-100)
func InterpolateEmberParams(heat int) EmberParams {
	if heat < 0 {
		heat = 0
	}
	if heat > 100 {
		heat = 100
	}

	t := float64(heat) / 100.0

	return EmberParams{
		JaggedAmp:     vmath.LerpF(emberJaggedAmp[0], emberJaggedAmp[1], t),
		JaggedFreq:    vmath.LerpF(emberJaggedFreq[0], emberJaggedFreq[1], t),
		JaggedSpeed:   vmath.LerpF(emberJaggedSpeed[0], emberJaggedSpeed[1], t),
		Octave2:       vmath.LerpF(emberOctave2[0], emberOctave2[1], t),
		Octave3:       vmath.LerpF(emberOctave3[0], emberOctave3[1], t),
		EruptionPower: vmath.LerpF(emberEruptionPower[0], emberEruptionPower[1], t),

		CoreFalloff:   vmath.LerpF(emberCoreFalloff[0], emberCoreFalloff[1], t),
		CorePower:     vmath.LerpF(emberCorePower[0], emberCorePower[1], t),
		MidFalloff:    vmath.LerpF(emberMidFalloff[0], emberMidFalloff[1], t),
		MidPower:      vmath.LerpF(emberMidPower[0], emberMidPower[1], t),
		MidIntensity:  vmath.LerpF(emberMidIntensity[0], emberMidIntensity[1], t),
		EdgePower:     vmath.LerpF(emberEdgePower[0], emberEdgePower[1], t),
		EdgeIntensity: vmath.LerpF(emberEdgeIntensity[0], emberEdgeIntensity[1], t),

		RingAlpha:   vmath.LerpF(emberRingAlpha[0], emberRingAlpha[1], t),
		RingWidth:   vmath.LerpF(emberRingWidth[0], emberRingWidth[1], t),
		RingVisible: vmath.LerpF(emberRingVisible[0], emberRingVisible[1], t),
		RingSpeed:   vmath.LerpF(emberRingSpeed[0], emberRingSpeed[1], t),

		HeatFactor: t,
	}
}

// Ember256PaletteIndex returns xterm-256 palette index for given heat (0-100)
// Maps to Heat256LUT for consistent heat visualization
func Ember256PaletteIndex(heat int) uint8 {
	if heat < 0 {
		heat = 0
	}
	if heat > 100 {
		heat = 100
	}
	// Map 0-100 to 0-9 index
	idx := heat / 10
	if idx > 9 {
		idx = 9
	}
	return Heat256LUT[idx]
}

package visual

import (
	"time"

	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Precomputed inverse squared radii for ellipse containment
var (
	EmberRadiusX               = vmath.FromFloat(parameter.PlayerFieldRadiusX)
	EmberRadiusY               = vmath.FromFloat(parameter.PlayerFieldRadiusY)
	EmberInvRxSq, EmberInvRySq = vmath.EllipseInvRadiiSq(EmberRadiusX, EmberRadiusY)

	// Precomputed constants
	NanoPerSecond     int64   = 1_000_000_000
	NanoPerSecondF    float64 = 1_000_000_000.0
	ExpLUTDecayKF     float64 = float64(vmath.ExpLUTDecayK)
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
	JaggedAmp     int64
	JaggedFreq    int64
	JaggedSpeed   int64
	Octave2       int64
	Octave3       int64
	EruptionPower int64

	// Glow layers
	CoreFalloff   int64
	CorePower     int64
	MidFalloff    int64
	MidPower      int64
	MidIntensity  int64
	EdgePower     int64
	EdgeIntensity int64

	// Rings
	RingAlpha   int64
	RingWidth   int64
	RingVisible int64
	RingSpeed   int64

	// Heat factor for color interpolation (Q32.32, 0=low heat, Scale=high heat)
	HeatFactor int64
}

// Ember parameter bounds [low heat, high heat]
var (
	emberJaggedAmp     = [2]int64{0, 2 * vmath.Scale}
	emberJaggedFreq    = [2]int64{4 * vmath.Scale, 32 * vmath.Scale}
	emberJaggedSpeed   = [2]int64{vmath.Scale / 2, 6 * vmath.Scale}
	emberOctave2       = [2]int64{vmath.Scale, vmath.Scale}
	emberOctave3       = [2]int64{vmath.Scale, vmath.Scale}
	emberEruptionPower = [2]int64{3 * vmath.Scale / 2, 16 * vmath.Scale}

	emberCoreFalloff   = [2]int64{8 * vmath.Scale / 5, 3 * vmath.Scale / 2}
	emberCorePower     = [2]int64{3 * vmath.Scale / 2, 3 * vmath.Scale / 2}
	emberMidFalloff    = [2]int64{vmath.Scale, vmath.Scale}
	emberMidPower      = [2]int64{vmath.Scale, vmath.Scale}
	emberMidIntensity  = [2]int64{vmath.Scale, vmath.Scale}
	emberEdgePower     = [2]int64{vmath.Scale / 5, vmath.Scale / 10}
	emberEdgeIntensity = [2]int64{vmath.Scale / 5, vmath.Scale / 5}

	emberRingAlpha   = [2]int64{vmath.Scale, 0}
	emberRingWidth   = [2]int64{vmath.Scale / 5, vmath.Scale / 50}
	emberRingVisible = [2]int64{vmath.Scale * 70, vmath.Scale / 5}
	emberRingSpeed   = [2]int64{3 * vmath.Scale, vmath.Scale / 5}
)

// Ring orbital plane normals (3 rings with different tilts)
// Precomputed for Dyson-sphere effect
const EmberRingCount = 3

// Ring orbital plane normals - matches sandbox calculation exactly
// tilt = (i+0.5)*π/3.5, azimuth = i*2π/3, aspectRatio = 2.1
// Intentionally NOT normalized to match sandbox rz magnitude behavior
var EmberRingNormals = [EmberRingCount][3]int64{
	// i=0: tilt≈25.7°, azimuth=0 → (0.434, 0, 0.901)
	{1864135472, 0, 3869212262},
	// i=1: tilt≈77.1°, azimuth=2π/3 → (-0.487, 0.402, 0.222)
	{-2091652710, 1726477754, 953482739},
	// i=2: tilt≈128.6°, azimuth=4π/3 → (-0.394, -0.324, -0.617)
	{-1692173885, -1391679667, -2650094100},
}

// EmberRingNormalsF - precomputed float normals (avoids per-cell conversion)
var EmberRingNormalsF [EmberRingCount][3]float64

func init() {
	for i := 0; i < EmberRingCount; i++ {
		EmberRingNormalsF[i][0] = vmath.ToFloat(EmberRingNormals[i][0])
		EmberRingNormalsF[i][1] = vmath.ToFloat(EmberRingNormals[i][1])
		EmberRingNormalsF[i][2] = vmath.ToFloat(EmberRingNormals[i][2])
	}
}

// EmberRingVelocities - per-ring rotation speed multipliers (Q32.32)
// Creates differential rotation for interference patterns
var EmberRingVelocities = [EmberRingCount]int64{
	1 << 32,              // 1.0
	1<<32 + 3*(1<<32)/10, // 1.3
	1<<32 + 6*(1<<32)/10, // 1.6
}

// EmberRingPulsePhases - per-ring pulse phase offsets
var EmberRingPulsePhases = [EmberRingCount]float64{
	0.0, 0.7, 1.4,
}

// EmberRingPhaseOffsets staggers ring rotation start positions
var EmberRingPhaseOffsets = [EmberRingCount]int64{
	0,
	vmath.Scale / 3,
	2 * vmath.Scale / 3,
}

// InterpolateEmberParams returns parameters interpolated for given heat (0-100)
func InterpolateEmberParams(heat int) EmberParams {
	if heat < 0 {
		heat = 0
	}
	if heat > 100 {
		heat = 100
	}

	// t in Q32.32: 0 = low heat, Scale = high heat
	t := int64(heat) * vmath.Scale / 100

	return EmberParams{
		JaggedAmp:     vmath.Lerp(emberJaggedAmp[0], emberJaggedAmp[1], t),
		JaggedFreq:    vmath.Lerp(emberJaggedFreq[0], emberJaggedFreq[1], t),
		JaggedSpeed:   vmath.Lerp(emberJaggedSpeed[0], emberJaggedSpeed[1], t),
		Octave2:       vmath.Lerp(emberOctave2[0], emberOctave2[1], t),
		Octave3:       vmath.Lerp(emberOctave3[0], emberOctave3[1], t),
		EruptionPower: vmath.Lerp(emberEruptionPower[0], emberEruptionPower[1], t),

		CoreFalloff:   vmath.Lerp(emberCoreFalloff[0], emberCoreFalloff[1], t),
		CorePower:     vmath.Lerp(emberCorePower[0], emberCorePower[1], t),
		MidFalloff:    vmath.Lerp(emberMidFalloff[0], emberMidFalloff[1], t),
		MidPower:      vmath.Lerp(emberMidPower[0], emberMidPower[1], t),
		MidIntensity:  vmath.Lerp(emberMidIntensity[0], emberMidIntensity[1], t),
		EdgePower:     vmath.Lerp(emberEdgePower[0], emberEdgePower[1], t),
		EdgeIntensity: vmath.Lerp(emberEdgeIntensity[0], emberEdgeIntensity[1], t),

		RingAlpha:   vmath.Lerp(emberRingAlpha[0], emberRingAlpha[1], t),
		RingWidth:   vmath.Lerp(emberRingWidth[0], emberRingWidth[1], t),
		RingVisible: vmath.Lerp(emberRingVisible[0], emberRingVisible[1], t),
		RingSpeed:   vmath.Lerp(emberRingSpeed[0], emberRingSpeed[1], t),

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
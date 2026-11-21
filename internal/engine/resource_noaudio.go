//go:build vif_headless || vif_noaudio

package engine

import "github.com/lixenwraith/vif/internal/parameter"

type audioResources struct{}

func (*Resource) SfxVolume() float64   { return 0 }
func (*Resource) SetSfxVolume(float64) {}

// Binaural is off in a build with no audio
func (*Resource) Binaural() (parameter.BinauralMode, parameter.BinauralPreset) {
	return parameter.BinauralOff, 0
}

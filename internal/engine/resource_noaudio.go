//go:build vif_headless || vif_noaudio

package engine

type audioResources struct{}

func (*Resource) EffectsVolume() float64   { return 0 }
func (*Resource) SetEffectsVolume(float64) {}

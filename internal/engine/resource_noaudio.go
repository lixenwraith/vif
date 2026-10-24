//go:build vif_headless || vif_noaudio

package engine

type audioResources struct{}

func (*Resource) SfxVolume() float64   { return 0 }
func (*Resource) SetSfxVolume(float64) {}

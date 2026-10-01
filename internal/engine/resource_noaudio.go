//go:build vif_headless || vif_noaudio || wasm

package engine

type audioResources struct{}

func (*Resource) EffectsVolume() float64   { return 0 }
func (*Resource) SetEffectsVolume(float64) {}

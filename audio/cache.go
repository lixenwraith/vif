package audio

// soundCache stores pre-rendered unity-gain SFX variant sets
// preloadAll runs in AudioEngine.Start before the mixer goroutine exists;
// storage is immutable afterward — no synchronization required
type soundCache struct {
	store [SoundTypeCount][]floatBuffer
}

func newSoundCache() *soundCache { return &soundCache{} }

// preloadAll renders every SFX variant set before the mixer goroutine exists
// Shapes threaded from AudioConfig.EffectShapes; a nil map reads the
// zero SFXParams, which norm() maps to unity
func (c *soundCache) preloadAll(shapes map[SoundType]SFXParams) {
	for st := SoundType(0); st < SoundTypeCount; st++ {
		c.store[st] = RenderVariants(st, SFXVariants, shapes[st])
	}
}

// variants returns the pre-rendered set; nil for unknown types
func (c *soundCache) variants(st SoundType) []floatBuffer {
	if st < 0 || int(st) >= int(SoundTypeCount) {
		return nil
	}
	return c.store[st]
}

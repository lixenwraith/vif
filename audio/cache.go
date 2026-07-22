package audio

// soundCache stores pre-rendered unity-gain variant sets, indexed by SoundID.
// Sized at Start from the frozen registry; immutable afterward, so the mix
// goroutine reads it without synchronization.
type soundCache struct {
	store [][]floatBuffer
}

func newSoundCache() *soundCache { return &soundCache{} }

func (c *soundCache) preloadAll(shapes map[string]SFXParams) {
	defs := registeredSounds()
	c.store = make([][]floatBuffer, len(defs))
	for id := 1; id < len(defs); id++ {
		c.store[id] = RenderVariants(defs[id], shapes[defs[id].Name])
	}
}

// variants returns the pre-rendered set; nil for unknown IDs, which is the
// degrade-to-silence path.
func (c *soundCache) variants(id SoundID) []floatBuffer {
	if id <= 0 || int(id) >= len(c.store) {
		return nil
	}
	return c.store[id]
}

func (c *soundCache) count() int { return len(c.store) }

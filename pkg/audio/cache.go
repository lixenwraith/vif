package audio

// soundCache stores pre-rendered unity-gain variant sets, indexed by SoundID.
// Sized at Start from the frozen registry. Immutable afterward except through
// replace, which arrives as cmdReloadSound and runs on the mix goroutine — so
// the goroutine that reads store is the only one that writes it.
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

// replace swaps one sound's variant set, growing to cover a SoundID assigned
// after Start. Mix-goroutine confined. The grow allocates; that is a control
// -path cost paid once per newly defined sound, never per sample.
func (c *soundCache) replace(id SoundID, bufs []floatBuffer) {
	if id <= 0 {
		return
	}
	for int(id) >= len(c.store) {
		c.store = append(c.store, nil)
	}
	c.store[id] = bufs
}

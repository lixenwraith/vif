package audio

import "sync"

// soundCache stores pre-generated unity-gain float buffers
type soundCache struct {
	mu    sync.RWMutex
	store [soundTypeCount]floatBuffer
	ready [soundTypeCount]bool
}

func newSoundCache() *soundCache {
	return &soundCache{}
}

// get returns cached buffer or generates on demand
func (c *soundCache) get(st SoundType) floatBuffer {
	if st < 0 || int(st) >= int(soundTypeCount) {
		return nil
	}

	c.mu.RLock()
	if c.ready[st] {
		buf := c.store[st]
		c.mu.RUnlock()
		return buf
	}
	c.mu.RUnlock()

	// Generate and cache
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.ready[st] {
		return c.store[st]
	}

	buf := generateSound(st)
	c.store[st] = buf
	c.ready[st] = true
	return buf
}

// preload generates frequently used sounds at init
func (c *soundCache) preload() {
	c.get(SoundError) // Most frequent
}
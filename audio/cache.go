package audio

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/core"
)

// soundCache stores pre-generated unity-gain float buffers
type soundCache struct {
	mu    sync.RWMutex
	store [core.SoundTypeCount]floatBuffer
	ready [core.SoundTypeCount]bool
}

func newSoundCache() *soundCache {
	return &soundCache{}
}

// get returns cached buffer or generates on demand
func (c *soundCache) get(st core.SoundType) floatBuffer {
	if st < 0 || int(st) >= int(core.SoundTypeCount) {
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
	c.get(core.SoundError) // Most frequent
}

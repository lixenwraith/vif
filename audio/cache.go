package audio

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/core"
)

// soundCache stores pre-generated unity-gain float buffers
// After preloadAll (engine Start, pre-mixer), all reads hit immutable data;
// the mutex covers only the cold lazy path retained for safety
type soundCache struct {
	mu    sync.Mutex
	store [core.SoundTypeCount]floatBuffer
	ready [core.SoundTypeCount]bool
}

func newSoundCache() *soundCache { return &soundCache{} }

// preloadAll renders every SFX buffer before the mixer starts
func (c *soundCache) preloadAll() {
	for st := core.SoundType(0); st < core.SoundTypeCount; st++ {
		c.get(st)
	}
}

// get returns cached buffer, generating on the cold path
func (c *soundCache) get(st core.SoundType) floatBuffer {
	if st < 0 || int(st) >= int(core.SoundTypeCount) {
		return nil
	}
	if c.ready[st] { // safe: set-before-mixer-start or under mu below
		return c.store[st]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready[st] {
		return c.store[st]
	}
	c.store[st] = generateSound(st)
	c.ready[st] = true
	return c.store[st]
}

// preload generates frequently used sounds at init
func (c *soundCache) preload() {
	c.get(core.SoundError) // Most frequent
}

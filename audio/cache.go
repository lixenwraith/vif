package audio

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// soundCache stores pre-generated unity-gain float buffers
type soundCache struct {
	mu    sync.RWMutex
	store [core.SoundTypeCount]floatBuffer
	ready [core.SoundTypeCount]bool

	// Rapid-fire dampening
	lastPlay     [core.SoundTypeCount]time.Time
	rapidFireVol [core.SoundTypeCount]float64
}

func newSoundCache() *soundCache {
	c := &soundCache{}
	// Initialize rapid-fire volumes to 1.0
	for i := range c.rapidFireVol {
		c.rapidFireVol[i] = 1.0
	}
	return c
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

// getWithDampening returns buffer and dampened volume for rapid-fire protection
func (c *soundCache) getWithDampening(st core.SoundType) (floatBuffer, float64) {
	if st < 0 || int(st) >= int(core.SoundTypeCount) {
		return nil, 0
	}

	buf := c.get(st)
	if buf == nil {
		return nil, 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(c.lastPlay[st])

	var vol float64
	if elapsed < parameter.RapidFireCooldown {
		// Rapid fire: decay volume
		c.rapidFireVol[st] *= parameter.RapidFireDecay
		if c.rapidFireVol[st] < parameter.RapidFireMinVolume {
			c.rapidFireVol[st] = parameter.RapidFireMinVolume
		}
		vol = c.rapidFireVol[st]
	} else {
		// Reset volume after cooldown
		c.rapidFireVol[st] = 1.0
		vol = 1.0
	}

	c.lastPlay[st] = now
	return buf, vol
}

// preload generates frequently used sounds at init
func (c *soundCache) preload() {
	c.get(core.SoundError) // Most frequent
}
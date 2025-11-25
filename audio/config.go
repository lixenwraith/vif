package audio

import (
	"encoding/json"
	"os"
	"strconv"
)

// LoadAudioConfig loads audio configuration from environment variables
func LoadAudioConfig() *AudioConfig {
	cfg := DefaultAudioConfig()

	// Check if audio is enabled
	if enabled := os.Getenv("VI_FIGHTER_AUDIO_ENABLED"); enabled != "" {
		if val, err := strconv.ParseBool(enabled); err == nil {
			cfg.Enabled = val
		}
	}

	// Load master volume (0-100 converted to 0.0-1.0)
	if volume := os.Getenv("VI_FIGHTER_MASTER_VOLUME"); volume != "" {
		if val, err := strconv.Atoi(volume); err == nil {
			cfg.MasterVolume = float64(val) / 100.0
			if cfg.MasterVolume < 0 {
				cfg.MasterVolume = 0
			}
			if cfg.MasterVolume > 1 {
				cfg.MasterVolume = 1
			}
		}
	}

	// Load effect volumes from JSON
	if effectVols := os.Getenv("VI_FIGHTER_SFX_VOLUMES"); effectVols != "" {
		var volumes map[string]float64
		if err := json.Unmarshal([]byte(effectVols), &volumes); err == nil {
			// Map string keys to SoundType
			if v, ok := volumes["error"]; ok {
				cfg.EffectVolumes[SoundError] = v
			}
			if v, ok := volumes["bell"]; ok {
				cfg.EffectVolumes[SoundBell] = v
			}
			if v, ok := volumes["whoosh"]; ok {
				cfg.EffectVolumes[SoundWhoosh] = v
			}
			if v, ok := volumes["coin"]; ok {
				cfg.EffectVolumes[SoundCoin] = v
			}
		}
	}

	// Load sample rate
	if sampleRate := os.Getenv("VI_FIGHTER_SAMPLE_RATE"); sampleRate != "" {
		if val, err := strconv.Atoi(sampleRate); err == nil && val > 0 {
			cfg.SampleRate = val
		}
	}

	return cfg
}
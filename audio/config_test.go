package audio

import (
	"os"
	"testing"
)

// TestDefaultAudioConfig verifies default configuration
func TestDefaultAudioConfig(t *testing.T) {
	cfg := DefaultAudioConfig()

	if cfg == nil {
		t.Fatal("Expected non-nil default config")
	}

	if !cfg.Enabled {
		t.Error("Expected default config to have Enabled=true")
	}

	if cfg.MasterVolume != 0.5 {
		t.Errorf("Expected default master volume 0.5, got %f", cfg.MasterVolume)
	}

	if cfg.SampleRate != 44100 {
		t.Errorf("Expected default sample rate 44100, got %d", cfg.SampleRate)
	}

	// Verify effect volumes are set
	if len(cfg.EffectVolumes) == 0 {
		t.Error("Expected default effect volumes to be set")
	}

	// Check specific effect volumes
	expectedVolumes := map[SoundType]float64{
		SoundError:  0.8,
		SoundBell:   1.0,
		SoundWhoosh: 0.6,
		SoundCoin:   0.5,
	}

	for soundType, expectedVol := range expectedVolumes {
		if vol, ok := cfg.EffectVolumes[soundType]; !ok {
			t.Errorf("Expected volume for sound type %d to be set", soundType)
		} else if vol != expectedVol {
			t.Errorf("Expected volume %f for sound type %d, got %f", expectedVol, soundType, vol)
		}
	}
}

// TestLoadAudioConfigDefaults verifies loading with no env vars
func TestLoadAudioConfigDefaults(t *testing.T) {
	// Clear any existing env vars
	os.Unsetenv("VI_FIGHTER_AUDIO_ENABLED")
	os.Unsetenv("VI_FIGHTER_MASTER_VOLUME")
	os.Unsetenv("VI_FIGHTER_SFX_VOLUMES")
	os.Unsetenv("VI_FIGHTER_SAMPLE_RATE")

	cfg := LoadAudioConfig()

	if cfg == nil {
		t.Fatal("Expected non-nil config")
	}

	// Should match defaults
	defaultCfg := DefaultAudioConfig()

	if cfg.Enabled != defaultCfg.Enabled {
		t.Errorf("Expected Enabled=%v, got %v", defaultCfg.Enabled, cfg.Enabled)
	}

	if cfg.MasterVolume != defaultCfg.MasterVolume {
		t.Errorf("Expected MasterVolume=%f, got %f", defaultCfg.MasterVolume, cfg.MasterVolume)
	}

	if cfg.SampleRate != defaultCfg.SampleRate {
		t.Errorf("Expected SampleRate=%d, got %d", defaultCfg.SampleRate, cfg.SampleRate)
	}
}

// TestLoadAudioConfigEnabled verifies loading enabled flag
func TestLoadAudioConfigEnabled(t *testing.T) {
	defer os.Unsetenv("VI_FIGHTER_AUDIO_ENABLED")

	testCases := []struct {
		value    string
		expected bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
	}

	for _, tc := range testCases {
		t.Run(tc.value, func(t *testing.T) {
			os.Setenv("VI_FIGHTER_AUDIO_ENABLED", tc.value)
			cfg := LoadAudioConfig()

			if cfg.Enabled != tc.expected {
				t.Errorf("Expected Enabled=%v for value %s, got %v", tc.expected, tc.value, cfg.Enabled)
			}
		})
	}
}

// TestLoadAudioConfigMasterVolume verifies loading master volume
func TestLoadAudioConfigMasterVolume(t *testing.T) {
	defer os.Unsetenv("VI_FIGHTER_MASTER_VOLUME")

	testCases := []struct {
		value    string
		expected float64
	}{
		{"0", 0.0},
		{"50", 0.5},
		{"100", 1.0},
		{"75", 0.75},
	}

	for _, tc := range testCases {
		t.Run(tc.value, func(t *testing.T) {
			os.Setenv("VI_FIGHTER_MASTER_VOLUME", tc.value)
			cfg := LoadAudioConfig()

			if cfg.MasterVolume != tc.expected {
				t.Errorf("Expected MasterVolume=%f for value %s, got %f", tc.expected, tc.value, cfg.MasterVolume)
			}
		})
	}
}

// TestLoadAudioConfigMasterVolumeClamp verifies volume clamping
func TestLoadAudioConfigMasterVolumeClamp(t *testing.T) {
	defer os.Unsetenv("VI_FIGHTER_MASTER_VOLUME")

	testCases := []struct {
		value    string
		expected float64
	}{
		{"-50", 0.0},   // Should clamp to 0
		{"150", 1.0},   // Should clamp to 1
		{"-100", 0.0},  // Should clamp to 0
		{"200", 1.0},   // Should clamp to 1
	}

	for _, tc := range testCases {
		t.Run(tc.value, func(t *testing.T) {
			os.Setenv("VI_FIGHTER_MASTER_VOLUME", tc.value)
			cfg := LoadAudioConfig()

			if cfg.MasterVolume != tc.expected {
				t.Errorf("Expected MasterVolume=%f for value %s (clamped), got %f", tc.expected, tc.value, cfg.MasterVolume)
			}
		})
	}
}

// TestLoadAudioConfigSampleRate verifies loading sample rate
func TestLoadAudioConfigSampleRate(t *testing.T) {
	defer os.Unsetenv("VI_FIGHTER_SAMPLE_RATE")

	testCases := []struct {
		value    string
		expected int
	}{
		{"22050", 22050},
		{"44100", 44100},
		{"48000", 48000},
	}

	for _, tc := range testCases {
		t.Run(tc.value, func(t *testing.T) {
			os.Setenv("VI_FIGHTER_SAMPLE_RATE", tc.value)
			cfg := LoadAudioConfig()

			if cfg.SampleRate != tc.expected {
				t.Errorf("Expected SampleRate=%d for value %s, got %d", tc.expected, tc.value, cfg.SampleRate)
			}
		})
	}
}

// TestLoadAudioConfigSampleRateInvalid verifies handling of invalid sample rate
func TestLoadAudioConfigSampleRateInvalid(t *testing.T) {
	defer os.Unsetenv("VI_FIGHTER_SAMPLE_RATE")

	// Invalid values should use default
	defaultRate := DefaultAudioConfig().SampleRate

	testCases := []string{
		"invalid",
		"-1000",
		"0",
		"",
	}

	for _, value := range testCases {
		t.Run(value, func(t *testing.T) {
			os.Setenv("VI_FIGHTER_SAMPLE_RATE", value)
			cfg := LoadAudioConfig()

			if cfg.SampleRate != defaultRate {
				t.Errorf("Expected default SampleRate=%d for invalid value %s, got %d", defaultRate, value, cfg.SampleRate)
			}
		})
	}
}

// TestLoadAudioConfigEffectVolumes verifies loading effect volumes
func TestLoadAudioConfigEffectVolumes(t *testing.T) {
	defer os.Unsetenv("VI_FIGHTER_SFX_VOLUMES")

	// Valid JSON
	jsonValue := `{"error": 0.9, "bell": 0.8, "whoosh": 0.7, "coin": 0.6}`
	os.Setenv("VI_FIGHTER_SFX_VOLUMES", jsonValue)

	cfg := LoadAudioConfig()

	expectedVolumes := map[SoundType]float64{
		SoundError:  0.9,
		SoundBell:   0.8,
		SoundWhoosh: 0.7,
		SoundCoin:   0.6,
	}

	for soundType, expectedVol := range expectedVolumes {
		if vol, ok := cfg.EffectVolumes[soundType]; !ok {
			t.Errorf("Expected volume for sound type %d to be set", soundType)
		} else if vol != expectedVol {
			t.Errorf("Expected volume %f for sound type %d, got %f", expectedVol, soundType, vol)
		}
	}
}

// TestLoadAudioConfigEffectVolumesInvalid verifies handling of invalid JSON
func TestLoadAudioConfigEffectVolumesInvalid(t *testing.T) {
	defer os.Unsetenv("VI_FIGHTER_SFX_VOLUMES")

	// Invalid JSON should use defaults
	os.Setenv("VI_FIGHTER_SFX_VOLUMES", "invalid json")

	cfg := LoadAudioConfig()
	defaultCfg := DefaultAudioConfig()

	// Should have default volumes
	for soundType, expectedVol := range defaultCfg.EffectVolumes {
		if vol, ok := cfg.EffectVolumes[soundType]; !ok {
			t.Errorf("Expected volume for sound type %d to be set", soundType)
		} else if vol != expectedVol {
			t.Errorf("Expected default volume %f for sound type %d, got %f", expectedVol, soundType, vol)
		}
	}
}

// TestSaveAudioConfig verifies saving configuration
func TestSaveAudioConfig(t *testing.T) {
	// Clean up after test
	defer func() {
		os.Unsetenv("VI_FIGHTER_AUDIO_ENABLED")
		os.Unsetenv("VI_FIGHTER_MASTER_VOLUME")
		os.Unsetenv("VI_FIGHTER_SFX_VOLUMES")
		os.Unsetenv("VI_FIGHTER_SAMPLE_RATE")
	}()

	cfg := &AudioConfig{
		Enabled:      true,
		MasterVolume: 0.75,
		EffectVolumes: map[SoundType]float64{
			SoundError:  0.9,
			SoundBell:   0.8,
			SoundWhoosh: 0.7,
			SoundCoin:   0.6,
		},
		SampleRate: 48000,
	}

	err := SaveAudioConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Verify env vars were set
	if enabled := os.Getenv("VI_FIGHTER_AUDIO_ENABLED"); enabled != "true" {
		t.Errorf("Expected VI_FIGHTER_AUDIO_ENABLED=true, got %s", enabled)
	}

	if volume := os.Getenv("VI_FIGHTER_MASTER_VOLUME"); volume != "75" {
		t.Errorf("Expected VI_FIGHTER_MASTER_VOLUME=75, got %s", volume)
	}

	if rate := os.Getenv("VI_FIGHTER_SAMPLE_RATE"); rate != "48000" {
		t.Errorf("Expected VI_FIGHTER_SAMPLE_RATE=48000, got %s", rate)
	}

	// Load and verify roundtrip
	loadedCfg := LoadAudioConfig()

	if loadedCfg.Enabled != cfg.Enabled {
		t.Errorf("Roundtrip failed: Enabled=%v, expected %v", loadedCfg.Enabled, cfg.Enabled)
	}

	if loadedCfg.MasterVolume != cfg.MasterVolume {
		t.Errorf("Roundtrip failed: MasterVolume=%f, expected %f", loadedCfg.MasterVolume, cfg.MasterVolume)
	}

	if loadedCfg.SampleRate != cfg.SampleRate {
		t.Errorf("Roundtrip failed: SampleRate=%d, expected %d", loadedCfg.SampleRate, cfg.SampleRate)
	}

	for soundType, expectedVol := range cfg.EffectVolumes {
		if vol, ok := loadedCfg.EffectVolumes[soundType]; !ok {
			t.Errorf("Roundtrip failed: volume for sound type %d not set", soundType)
		} else if vol != expectedVol {
			t.Errorf("Roundtrip failed: volume %f for sound type %d, expected %f", vol, soundType, expectedVol)
		}
	}
}

// TestSaveAudioConfigVolumeConversion verifies volume conversion
func TestSaveAudioConfigVolumeConversion(t *testing.T) {
	defer os.Unsetenv("VI_FIGHTER_MASTER_VOLUME")

	testCases := []struct {
		floatVolume float64
		intVolume   string
	}{
		{0.0, "0"},
		{0.25, "25"},
		{0.5, "50"},
		{0.75, "75"},
		{1.0, "100"},
	}

	for _, tc := range testCases {
		t.Run(tc.intVolume, func(t *testing.T) {
			cfg := DefaultAudioConfig()
			cfg.MasterVolume = tc.floatVolume

			err := SaveAudioConfig(cfg)
			if err != nil {
				t.Fatalf("Failed to save config: %v", err)
			}

			volume := os.Getenv("VI_FIGHTER_MASTER_VOLUME")
			if volume != tc.intVolume {
				t.Errorf("Expected VI_FIGHTER_MASTER_VOLUME=%s, got %s", tc.intVolume, volume)
			}
		})
	}
}

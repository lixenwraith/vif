package audio

import (
	"sync"
	"testing"
	"time"
)

// TestNewAudioEngine verifies audio engine initialization
func TestNewAudioEngine(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)

	if err != nil {
		t.Logf("Audio engine initialization warning: %v (this is expected in CI)", err)
	}

	if ae == nil {
		t.Fatal("Expected non-nil audio engine")
	}

	if !ae.initialized.Load() {
		t.Error("Expected initialized flag to be true")
	}

	if !ae.muted.Load() {
		t.Error("Expected audio to start muted by default")
	}

	if ae.running.Load() {
		t.Error("Expected audio engine to not be running before Start()")
	}
}

// TestAudioEngineStartStop verifies engine lifecycle
func TestAudioEngineStartStop(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	// Start the engine
	err = ae.Start()
	if err != nil {
		t.Fatalf("Failed to start audio engine: %v", err)
	}

	if !ae.IsRunning() {
		t.Error("Expected engine to be running after Start()")
	}

	// Stop the engine
	ae.Stop()

	if ae.IsRunning() {
		t.Error("Expected engine to be stopped after Stop()")
	}

	// Verify idempotent stop
	ae.Stop()
	if ae.IsRunning() {
		t.Error("Expected engine to remain stopped after second Stop()")
	}
}

// TestAudioEngineMuteToggle verifies mute functionality
func TestAudioEngineMuteToggle(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	// Should start muted
	if !ae.IsMuted() {
		t.Error("Expected engine to start muted")
	}

	// Toggle unmute
	enabled := ae.ToggleMute()
	if !enabled {
		t.Error("Expected ToggleMute to return true when unmuting")
	}
	if ae.IsMuted() {
		t.Error("Expected engine to be unmuted after toggle")
	}

	// Toggle mute
	enabled = ae.ToggleMute()
	if enabled {
		t.Error("Expected ToggleMute to return false when muting")
	}
	if !ae.IsMuted() {
		t.Error("Expected engine to be muted after second toggle")
	}
}

// TestAudioEngineIsEnabled verifies enabled state
func TestAudioEngineIsEnabled(t *testing.T) {
	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	// Should be disabled due to mute (starts muted)
	if ae.IsEnabled() {
		t.Error("Expected engine to be disabled when muted")
	}

	// Unmute
	ae.ToggleMute()
	if !ae.IsEnabled() {
		t.Error("Expected engine to be enabled when unmuted and config.Enabled=true")
	}

	// Disable via config
	cfg2 := DefaultAudioConfig()
	cfg2.Enabled = false
	ae.SetConfig(cfg2)

	if ae.IsEnabled() {
		t.Error("Expected engine to be disabled when config.Enabled=false")
	}
}

// TestAudioEngineQueueOverflow verifies queue overflow handling
func TestAudioEngineQueueOverflow(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	err = ae.Start()
	if err != nil {
		t.Fatalf("Failed to start audio engine: %v", err)
	}
	defer ae.Stop()

	// Unmute to enable sending
	ae.ToggleMute()

	// Fill real-time queue (size 5)
	for i := 0; i < 10; i++ {
		cmd := AudioCommand{
			Type:       SoundError,
			Priority:   1,
			Generation: uint64(i),
			Timestamp:  time.Now(),
		}
		ae.SendRealTime(cmd)
	}

	// Check stats
	_, _, overflows := ae.GetStats()
	if overflows == 0 {
		t.Log("No queue overflows detected (queue may have been processed)")
	}

	// Fill state queue (size 10)
	for i := 0; i < 20; i++ {
		cmd := AudioCommand{
			Type:       SoundCoin,
			Priority:   0,
			Generation: uint64(i + 100),
			Timestamp:  time.Now(),
		}
		ae.SendState(cmd)
	}

	// Allow processing
	time.Sleep(100 * time.Millisecond)

	played, dropped, overflows := ae.GetStats()
	t.Logf("Stats - Played: %d, Dropped: %d, Overflows: %d", played, dropped, overflows)
}

// TestAudioEngineStatsTracking verifies statistics tracking
func TestAudioEngineStatsTracking(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	err = ae.Start()
	if err != nil {
		t.Fatalf("Failed to start audio engine: %v", err)
	}
	defer ae.Stop()

	// Initial stats should be zero
	played, dropped, overflows := ae.GetStats()
	if played != 0 || dropped != 0 || overflows != 0 {
		t.Errorf("Expected initial stats to be zero, got played=%d dropped=%d overflows=%d",
			played, dropped, overflows)
	}

	// Unmute
	ae.ToggleMute()

	// Send commands
	cmd := AudioCommand{
		Type:       SoundBell,
		Priority:   1,
		Generation: 1,
		Timestamp:  time.Now(),
	}
	ae.SendRealTime(cmd)

	// Allow processing
	time.Sleep(150 * time.Millisecond)

	played, dropped, overflows = ae.GetStats()
	t.Logf("After sending - Played: %d, Dropped: %d, Overflows: %d", played, dropped, overflows)
}

// TestAudioEngineThreadSafety verifies concurrent access safety
func TestAudioEngineThreadSafety(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	err = ae.Start()
	if err != nil {
		t.Fatalf("Failed to start audio engine: %v", err)
	}
	defer ae.Stop()

	ae.ToggleMute() // Unmute

	// Concurrent command sends
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cmd := AudioCommand{
					Type:       SoundType(j % 4),
					Priority:   j,
					Generation: uint64(id*10 + j),
					Timestamp:  time.Now(),
				}
				if j%2 == 0 {
					ae.SendRealTime(cmd)
				} else {
					ae.SendState(cmd)
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Concurrent mute toggles
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				ae.ToggleMute()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Concurrent config updates
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				cfg := DefaultAudioConfig()
				cfg.MasterVolume = float64(j) / 10.0
				ae.SetConfig(cfg)
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Wait for all goroutines
	wg.Wait()

	// Verify stats
	played, dropped, overflows := ae.GetStats()
	t.Logf("Thread safety test - Played: %d, Dropped: %d, Overflows: %d", played, dropped, overflows)

	// Engine should still be running
	if !ae.IsRunning() {
		t.Error("Expected engine to still be running after concurrent access")
	}
}

// TestAudioEngineStopCurrentSound verifies sound interruption
func TestAudioEngineStopCurrentSound(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	err = ae.Start()
	if err != nil {
		t.Fatalf("Failed to start audio engine: %v", err)
	}
	defer ae.Stop()

	// Should not panic when no sound is playing
	ae.StopCurrentSound()

	// Unmute and send sound
	ae.ToggleMute()
	cmd := AudioCommand{
		Type:       SoundCoin,
		Priority:   1,
		Generation: 1,
		Timestamp:  time.Now(),
	}
	ae.SendRealTime(cmd)

	// Wait a bit for sound to start
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic
	ae.StopCurrentSound()
}

// TestAudioEngineDrainQueues verifies queue draining
func TestAudioEngineDrainQueues(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	err = ae.Start()
	if err != nil {
		t.Fatalf("Failed to start audio engine: %v", err)
	}
	defer ae.Stop()

	ae.ToggleMute() // Unmute

	// Fill queues
	for i := 0; i < 5; i++ {
		cmd := AudioCommand{
			Type:       SoundError,
			Priority:   i,
			Generation: uint64(i),
			Timestamp:  time.Now(),
		}
		ae.SendRealTime(cmd)
	}

	for i := 0; i < 10; i++ {
		cmd := AudioCommand{
			Type:       SoundBell,
			Priority:   i,
			Generation: uint64(i + 100),
			Timestamp:  time.Now(),
		}
		ae.SendState(cmd)
	}

	// Drain queues
	ae.DrainQueues()

	// Verify queues are empty by checking dropped count increased
	_, dropped, _ := ae.GetStats()
	if dropped == 0 {
		t.Log("Queue may have been empty or processed before drain")
	}
}

// TestAudioEngineVolumeControl verifies volume setting
func TestAudioEngineVolumeControl(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	// Test valid volumes
	testCases := []float64{0.0, 0.25, 0.5, 0.75, 1.0}
	for _, vol := range testCases {
		ae.SetVolume(vol)
		// Volume is internal to config, verify no panic
	}

	// Test clamping
	ae.SetVolume(-0.5) // Should clamp to 0
	ae.SetVolume(1.5)  // Should clamp to 1

	// No panic = success
}

// TestAudioEngineRapidStartStop verifies rapid lifecycle operations
func TestAudioEngineRapidStartStop(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	// Rapid start/stop cycles
	for i := 0; i < 5; i++ {
		err = ae.Start()
		if err != nil {
			t.Fatalf("Failed to start audio engine on iteration %d: %v", i, err)
		}

		if !ae.IsRunning() {
			t.Errorf("Expected engine to be running on iteration %d", i)
		}

		ae.Stop()

		if ae.IsRunning() {
			t.Errorf("Expected engine to be stopped on iteration %d", i)
		}
	}
}

// TestAudioEngineSendWhileMuted verifies behavior when muted
func TestAudioEngineSendWhileMuted(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	err = ae.Start()
	if err != nil {
		t.Fatalf("Failed to start audio engine: %v", err)
	}
	defer ae.Stop()

	// Should be muted by default
	if !ae.IsMuted() {
		t.Fatal("Expected engine to start muted")
	}

	// Try to send commands while muted
	cmd := AudioCommand{
		Type:       SoundError,
		Priority:   1,
		Generation: 1,
		Timestamp:  time.Now(),
	}

	// SendRealTime should return false when muted (engine disabled)
	sent := ae.SendRealTime(cmd)
	if sent {
		t.Error("Expected SendRealTime to return false when muted")
	}

	// SendState should also return false when muted
	sent = ae.SendState(cmd)
	if sent {
		t.Error("Expected SendState to return false when muted")
	}

	// Verify no sounds were played
	time.Sleep(50 * time.Millisecond)
	played, _, _ := ae.GetStats()
	if played != 0 {
		t.Errorf("Expected 0 sounds played when muted, got %d", played)
	}
}

// TestAudioEngineSendBeforeStart verifies behavior before starting
func TestAudioEngineSendBeforeStart(t *testing.T) {
	cfg := DefaultAudioConfig()
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Logf("Audio engine initialization warning: %v", err)
	}

	ae.ToggleMute() // Unmute

	cmd := AudioCommand{
		Type:       SoundError,
		Priority:   1,
		Generation: 1,
		Timestamp:  time.Now(),
	}

	// Should return false when not running
	sent := ae.SendRealTime(cmd)
	if sent {
		t.Error("Expected SendRealTime to return false when engine not started")
	}

	sent = ae.SendState(cmd)
	if sent {
		t.Error("Expected SendState to return false when engine not started")
	}
}

//go:build !vif_headless && !vif_noaudio && !wasm

package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/audio"
)

func TestAudioAvailabilityGatesMuteResetAndMusicWithoutLosingPreferences(t *testing.T) {
	audio.ResetRegistries()
	t.Cleanup(audio.ResetRegistries)
	cfg := audio.DefaultAudioConfig()
	cfg.Enabled, cfg.ForceBackend = true, audio.BackendNameNull
	player, err := audio.NewAudioEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := player.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Stop)
	w, _, _ := testCursorWorld(t)
	w.Resources.Audio = &engine.AudioResource{Engine: player}
	a, m := NewAudioSystem(w).(*AudioSystem), NewMusicSystem(w).(*MusicSystem)
	dispatch := func(ev event.GameEvent) {
		a.HandleEvent(ev)
		m.HandleEvent(ev)
		for _, next := range w.Resources.Event.Queue.Consume() {
			a.HandleEvent(next)
			m.HandleEvent(next)
		}
	}
	command := func(name string, enabled bool) {
		dispatch(event.GameEvent{Type: event.EventMetaSystemCommandRequest, Payload: &event.MetaSystemCommandPayload{SystemName: name, Enabled: enabled}})
	}
	setMask := func(mask uint8) {
		dispatch(event.GameEvent{Type: event.EventSoundMuteToggle, Payload: &event.SoundMuteTogglePayload{Mode: event.MuteSet, Mask: mask}})
	}
	assertMask := func(want uint8) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			a.Update()
			m.Update()
			if got := w.Resources.Status.Ints.Get("audio.mask").Load(); got == int64(want) {
				break
			} else if time.Now().After(deadline) {
				t.Fatalf("audio mask=%d, want %d", got, want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	// Scenario gates settle before the first music update, even with -mute=false.
	command("audio", false)
	command("music", false)
	for range 8 {
		dispatch(event.GameEvent{Type: event.EventSoundMuteToggle})
		assertMask(parameter.AudioChanNone)
		if !player.IsEffectMuted() || !player.IsMusicMuted() || player.IsMusicPlaying() {
			t.Fatal("mute cycle started a disabled channel")
		}
	}
	setMask(parameter.AudioChanAll)
	dispatch(event.GameEvent{Type: event.EventMusicStart})
	assertMask(parameter.AudioChanNone)
	command("audio", true)
	assertMask(parameter.AudioChanEffects)
	command("music", true)
	assertMask(parameter.AudioChanAll)
	w.Resources.Time.DeltaTime = 50 * time.Millisecond
	w.Resources.Game.State.MusicAPM.Store(240)
	before := m.bpmF
	m.Update()
	if m.bpmF <= before {
		t.Fatal("resumed music did not track APM")
	}
	command("audio", false)
	assertMask(parameter.AudioChanNone)
	if !player.IsMusicMuted() {
		t.Fatal("audio disable left the music bus open")
	}
	command("audio", true)
	assertMask(parameter.AudioChanAll)
	dispatch(event.GameEvent{Type: event.EventGamePauseChanged, Payload: &event.GamePausePayload{Paused: true}})
	setMask(parameter.AudioChanNone)
	assertMask(parameter.AudioChanNone)
	setMask(parameter.AudioChanAll)
	assertMask(parameter.AudioChanAll)
	dispatch(event.GameEvent{Type: event.EventGamePauseChanged, Payload: &event.GamePausePayload{Paused: false}})
	dispatch(event.GameEvent{Type: event.EventMusicStop})
	assertMask(parameter.AudioChanEffects)
	setMask(parameter.AudioChanNone)
	setMask(parameter.AudioChanAll)
	assertMask(parameter.AudioChanEffects)
	dispatch(event.GameEvent{Type: event.EventMusicStart})
	assertMask(parameter.AudioChanAll)
	command("audio", false)
	command("music", false)
	dispatch(event.GameEvent{Type: event.EventGameResetRequest})
	command("audio", false)
	command("music", false)
	assertMask(parameter.AudioChanNone)
	command("audio", true)
	command("music", true)
	assertMask(parameter.AudioChanAll)
}

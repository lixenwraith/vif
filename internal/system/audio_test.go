//go:build !vif_headless && !vif_noaudio

package system

import (
	"math"
	"testing"
	"time"

	"github.com/lixenwraith/audio"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
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
	a, m := ready(NewAudioSystem(w)).(*AudioSystem), ready(NewMusicSystem(w)).(*MusicSystem)
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
		dispatch(event.GameEvent{Type: event.EventAudioMuteToggle, Payload: &event.AudioMuteTogglePayload{Mode: event.MuteSet, Mask: mask}})
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
		dispatch(event.GameEvent{Type: event.EventAudioMuteToggle})
		assertMask(parameter.AudioChanNone)
		if !player.IsSfxMuted() || !player.IsMusicMuted() || player.IsMusicPlaying() {
			t.Fatal("mute cycle started a disabled channel")
		}
	}
	setMask(parameter.AudioChanAll)
	dispatch(event.GameEvent{Type: event.EventMusicStart})
	assertMask(parameter.AudioChanNone)
	command("audio", true)
	assertMask(parameter.AudioChanSfx)
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
	assertMask(parameter.AudioChanSfx)
	setMask(parameter.AudioChanNone)
	setMask(parameter.AudioChanAll)
	assertMask(parameter.AudioChanSfx)
	dispatch(event.GameEvent{Type: event.EventMusicStart})
	assertMask(parameter.AudioChanAll)
	command("audio", false)
	command("music", false)
	a.Init()
	m.Init()
	command("audio", false)
	command("music", false)
	assertMask(parameter.AudioChanNone)
	command("audio", true)
	command("music", true)
	assertMask(parameter.AudioChanAll)
}

// A copy rebuilt to replace a presented world replayed without speakers; on taking
// them it sounds what its own replayed gates allow, and the replaced world goes quiet.
func TestARebuiltCopyTakesOverTheSpeakers(t *testing.T) {
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
	world := func(r *engine.AudioResource) (*engine.World, *MusicSystem, func(event.GameEvent)) {
		w := engine.NewWorld()
		engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
		w.Resources.Audio = r
		a, m := ready(NewAudioSystem(w)).(*AudioSystem), ready(NewMusicSystem(w)).(*MusicSystem)
		w.AddSystem(a, engine.SystemProfile{})
		w.AddSystem(m, engine.SystemProfile{})
		return w, m, func(ev event.GameEvent) {
			for evs := []event.GameEvent{ev}; len(evs) > 0; evs = w.Resources.Event.Queue.Consume() {
				for _, e := range evs {
					a.HandleEvent(e)
					m.HandleEvent(e)
				}
			}
		}
	}
	mask := func(w *engine.World) int64 { return w.Resources.Status.Ints.Get("audio.mask").Load() }
	settle := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(time.Second); !ok(); time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
		}
	}
	gate := func(enabled bool) event.GameEvent {
		return event.GameEvent{Type: event.EventMetaSystemCommandRequest, Payload: &event.MetaSystemCommandPayload{SystemName: "music", Enabled: enabled}}
	}
	from, _, play := world(&engine.AudioResource{Engine: player})
	to, music, dispatch := world(nil)
	play(event.GameEvent{Type: event.EventAudioMuteToggle, Payload: &event.AudioMuteTogglePayload{Mode: event.MuteSet, Mask: parameter.AudioChanAll}})
	play(event.GameEvent{Type: event.EventMusicStart})
	play(event.GameEvent{Type: event.EventMusicBinauralRequest, Payload: &event.MusicBinauralPayload{Mode: parameter.BinauralFixed, Preset: 1}})
	settle("presented world's music did not start", player.IsMusicPlaying)
	dispatch(gate(false)) // the copy stands before a region opened the music gate

	HandOverAudio(to, from)
	if to.Resources.Audio == nil || from.Resources.Audio != nil || !player.IsMusicMuted() || mask(to) != int64(parameter.AudioChanSfx) {
		t.Fatalf("handed over: copy mask %d, music muted %v", mask(to), player.IsMusicMuted())
	}
	// the viewer's binaural beat goes with the speakers, already sent
	if mode, preset := to.Resources.Binaural(); mode != parameter.BinauralFixed || preset != 1 || music.bed.Beat != parameter.BinauralPresets[1].Beat {
		t.Fatalf("handed over: the copy's bed is mode %d preset %d, sent as %+v", mode, preset, music.bed)
	}
	dispatch(gate(true))
	settle("the copy's gate did not resume the music", func() bool {
		music.Update()
		return mask(to) == int64(parameter.AudioChanAll)
	})
}

// TestTheConductorSendsTheBedThePlayersSettingAsks: the music system keeps the
// engine's bed at what the setting asks, none while off, a preset's beat on the
// key's root while fixed, and while dynamic the tempo's, which a bed already
// sounding glides to; the music card says the mode and the beat.
func TestTheConductorSendsTheBedThePlayersSettingAsks(t *testing.T) {
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
	w.Resources.Time.DeltaTime = 50 * time.Millisecond
	ready(NewAudioSystem(w))
	m := ready(NewMusicSystem(w)).(*MusicSystem)
	card := w.Resources.Status.Strings.Get("music.binaural")
	set := func(mode parameter.BinauralMode) {
		m.HandleEvent(event.GameEvent{Type: event.EventMusicBinauralRequest, Payload: &event.MusicBinauralPayload{Mode: mode, Preset: 1}})
	}
	m.HandleEvent(event.GameEvent{Type: event.EventMusicStart})
	for deadline := time.Now().Add(5 * time.Second); !player.IsMusicPlaying(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the music did not start")
		}
	}
	m.Update()
	if m.bed != (audio.Binaural{}) || card.Load() != "off" {
		t.Fatalf("with the setting off the conductor sent %+v and the card reads %q", m.bed, card.Load())
	}

	focus := parameter.BinauralPresets[1].Beat
	set(parameter.BinauralFixed)
	if want := (audio.Binaural{Carrier: parameter.BinauralCarrier(parameter.DefaultRootNote), Beat: focus, Level: parameter.BinauralLevel}); m.bed != want || card.Load() != "fixed 15.0 Hz" {
		t.Fatalf("fixed on the second preset: sent %+v, want %+v; the card reads %q", m.bed, want, card.Load())
	}
	m.HandleEvent(event.GameEvent{Type: event.EventMelodyPatternRequest, Payload: &event.MelodyPatternRequestPayload{RootNote: 45}})
	if m.Update(); m.bed.Carrier != parameter.BinauralCarrier(45) || m.bed.Carrier != 220 {
		t.Fatalf("under a key of A the bed is carried at %g Hz, want 220", m.bed.Carrier)
	}

	// from a bed sounding at 15 Hz the tempo's beat is glided to, a step an update
	tempo, step := parameter.BinauralBeat(m.bpmF), parameter.BinauralGlide*0.05
	if set(parameter.BinauralDynamic); math.Abs(m.bed.Beat-(focus-step)) > 1e-9 || tempo >= focus-step {
		t.Fatalf("dynamic after fixed: the beat stands at %g, want a step of %g from %g toward the tempo's %g", m.bed.Beat, step, focus, tempo)
	}
	for range int((focus-tempo)/step) + 2 {
		m.Update()
	}
	if m.bed.Beat != tempo || card.Load() != "dynamic 13.3 Hz" {
		t.Fatalf("the glide ended at %g Hz, the card reading %q: want the tempo's %g", m.bed.Beat, card.Load(), tempo)
	}
	// from no bed it starts on the tempo's
	set(parameter.BinauralOff)
	if set(parameter.BinauralDynamic); m.bed.Beat != tempo {
		t.Fatalf("dynamic from off starts at %g Hz, want the tempo's %g", m.bed.Beat, tempo)
	}
}

// TestAPlacedSoundIsHeardFromTheCursor: a sound placed left of the cursor pans left,
// one below it is muffled and one above it open, the further quieter, its pitch kept.
func TestAPlacedSoundIsHeardFromTheCursor(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	s := NewAudioSystem(w).(*AudioSystem)
	cursor, ok := w.LocalCursor()
	if !ok {
		t.Fatal("no local cursor")
	}
	at := func(dx, dy int) audio.Placement {
		return s.placement(&event.SoundRequestPayload{X: cursor.X + dx, Y: cursor.Y + dy, Placed: true, Pitch: 3})
	}
	near, leftBelow, rightAbove := at(1, 0), at(-10, 4), at(10, -4)
	if leftBelow.Pan >= 0 || rightAbove.Pan <= 0 {
		t.Errorf("pans %.2f left and %.2f right, want them either side of the middle", leftBelow.Pan, rightAbove.Pan)
	}
	if leftBelow.Cutoff == 0 || rightAbove.Cutoff != 0 {
		t.Errorf("cutoffs %.0f below and %.0f above, want a low-pass below alone", leftBelow.Cutoff, rightAbove.Cutoff)
	}
	if leftBelow.Gain >= near.Gain || near.Pitch != 3 {
		t.Errorf("far gain %.1f against near %.1f, pitch %.1f: want the far quieter and the pitch kept", leftBelow.Gain, near.Gain, near.Pitch)
	}
}

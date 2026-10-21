package input

import (
	"testing"

	"github.com/lixenwraith/terminal"
)

// TestCapturedKeyOutranksPlayGrammar asserts a captured key reaches the scenario
// from Normal mid-count and from Insert, while text entry still types it
func TestCapturedKeyOutranksPlayGrammar(t *testing.T) {
	var capture KeyCapture
	if refused := capture.Set("15é", false); refused != "é" {
		t.Fatalf("refused %q, want the non-ASCII rune", refused)
	}
	m := NewMachine()
	m.SetCapture(&capture)
	key := func(r rune) *Intent {
		return m.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r})
	}

	if in := key('2'); in != nil {
		t.Fatalf("an uncaptured digit is still a count, got %v", in.Type)
	}
	if in := key('1'); in == nil || in.Type != IntentKeyPress || in.Char != '1' {
		t.Fatalf("captured key in Normal mid-count = %+v, want IntentKeyPress '1'", in)
	}
	if in := key('l'); in == nil || in.Count != 1 {
		t.Fatalf("the count the capture interrupted leaked into the next motion: %+v", in)
	}

	m.SetMode(ModeInsert)
	if in := key('5'); in == nil || in.Type != IntentKeyPress {
		t.Fatalf("captured key in Insert = %+v, want IntentKeyPress", in)
	}
	m.SetMode(ModeCommand)
	if in := key('5'); in == nil || in.Type != IntentTextChar {
		t.Fatalf("captured key in Command = %+v, want IntentTextChar", in)
	}

	capture.Set("", false)
	m.SetMode(ModeInsert)
	if in := key('1'); in == nil || in.Type != IntentTextChar {
		t.Fatalf("released key in Insert = %+v, want IntentTextChar", in)
	}
}

// TestOnlyACapturedKeyReleases: a captured key's release reaches the router in any
// mode, which ends holds as play stops, and its repeat is nothing while the hold is
// down; any other key's release is nothing and its repeat presses again.
func TestOnlyACapturedKeyReleases(t *testing.T) {
	var capture KeyCapture
	capture.Set("1", false)
	m := NewMachine()
	m.SetCapture(&capture)
	key := func(r rune, action terminal.KeyAction, lost bool) *Intent {
		return m.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r, Action: action, Lost: lost})
	}

	if in := key('1', terminal.KeyRepeat, false); in != nil {
		t.Fatalf("a captured key's repeat = %+v, want nothing", in)
	}
	m.SetMode(ModeCommand)
	if in := key('1', terminal.KeyRelease, true); in == nil || in.Type != IntentKeyRelease || in.Char != '1' || !in.Lost {
		t.Fatalf("a captured key's lost release in Command = %+v, want a lost IntentKeyRelease", in)
	}
	m.SetMode(ModeNormal)
	if in := key('l', terminal.KeyRelease, false); in != nil {
		t.Fatalf("an uncaptured key's release = %+v, want nothing", in)
	}
	if in := key('l', terminal.KeyRepeat, false); in == nil || in.Type != IntentMotion {
		t.Fatalf("an uncaptured key's repeat = %+v, want its motion again", in)
	}
}

// TestControlKeysReachAnOverlay: quit, the mute cycle and the macro stop are
// global, so an open overlay must not swallow them.
func TestControlKeysReachAnOverlay(t *testing.T) {
	m := NewMachine()
	m.SetMode(ModeOverlay)
	for key, want := range map[terminal.Key]IntentType{
		terminal.KeyCtrlQ:     IntentQuit,
		terminal.KeyCtrlC:     IntentQuit,
		terminal.KeyCtrlS:     IntentToggleAudioCycle,
		terminal.KeyCtrlSpace: IntentMacroStopAll,
	} {
		in := m.Process(terminal.Event{Type: terminal.EventKey, Key: key})
		if in == nil || in.Type != want {
			t.Errorf("key %v in an overlay = %+v, want %v", key, in, want)
		}
	}
}

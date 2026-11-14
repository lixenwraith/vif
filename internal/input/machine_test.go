package input

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
)

// TestCapturedKeyOutranksPlayGrammar asserts a captured key reaches the scenario
// from Normal mid-count and from Insert, while text entry still types it
func TestCapturedKeyOutranksPlayGrammar(t *testing.T) {
	var capture KeyCapture
	if refused := capture.Set("15é", 0, false, false); refused != "é" {
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

	capture.Set("", 0, false, false)
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
	capture.Set("1", 0, false, false)
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

// TestAToggleActsOncePerPress: a held toggle's repeats are nothing, in the mode its
// press opened too, while the same key's repeat that types still types.
func TestAToggleActsOncePerPress(t *testing.T) {
	m := NewMachine()
	key := func(k terminal.Key, r rune, action terminal.KeyAction) *Intent {
		return m.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: r, Action: action})
	}

	if in := key(terminal.KeyCtrlS, 0, terminal.KeyPress); in == nil || in.Type != IntentToggleAudioCycle {
		t.Fatalf("Ctrl+S press = %+v, want the mute cycle", in)
	}
	if in := key(terminal.KeyCtrlS, 0, terminal.KeyRepeat); in != nil {
		t.Fatalf("Ctrl+S repeat = %+v, want nothing", in)
	}
	if in := key(terminal.KeyRune, 'a', terminal.KeyRepeat); in != nil {
		t.Fatalf("a repeat = %+v, want nothing", in)
	}
	m.SetMode(ModeOverlay)
	if in := key(terminal.KeyCtrlG, 0, terminal.KeyRepeat); in != nil {
		t.Fatalf("Ctrl+G repeat over the menu it opened = %+v, want nothing", in)
	}
	m.SetMode(ModeInsert)
	if in := key(terminal.KeyRune, 'a', terminal.KeyRepeat); in == nil || in.Type != IntentTextChar {
		t.Fatalf("a repeat in Insert = %+v, want the typed character", in)
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

// A capture scoped to Normal and Visual leaves its key to Insert, and an idle one
// to a sequence already pending, so a find still takes the letter it captures.
func TestAScopedCaptureYieldsToInsertAndToPendingGrammar(t *testing.T) {
	var capture KeyCapture
	capture.Set("a", core.ModeMaskNormal|core.ModeMaskVisual, true, false)
	m := NewMachine()
	m.SetCapture(&capture)
	key := func(r rune) *Intent {
		return m.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r})
	}

	if in := key('a'); in == nil || in.Type != IntentKeyPress || in.Char != 'a' {
		t.Fatalf("idle Normal: a = %+v, want IntentKeyPress", in)
	}
	key('f')
	if in := key('a'); in == nil || in.Type != IntentCharMotion || in.Char != 'a' {
		t.Fatalf("after f: a = %+v, want the find's target, not a press", in)
	}
	m.SetMode(ModeInsert)
	if in := key('a'); in == nil || in.Type != IntentTextChar {
		t.Fatalf("Insert: a = %+v, want the typed character", in)
	}
}

// TestDotCompletesTheLastChangeAgain: '.' completes the last change the parser
// completed, an operator's, x's or r's, and not a motion; a count before it
// replaces the change's own, and the next '.' keeps that count.
func TestDotCompletesTheLastChangeAgain(t *testing.T) {
	m := NewMachine()
	keys := func(s string) (last *Intent) {
		for _, r := range s {
			if i := m.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r}); i != nil {
				last = i
			}
		}
		return last
	}
	if got := keys("."); got != nil {
		t.Fatalf("'.' before any change made %+v", got)
	}
	change := Intent{Type: IntentOperatorMotion, Operator: OperatorDelete, Motion: MotionWordForward, ModeTarget: ModeTargetInsert}
	keys("c2wl")
	for _, c := range []struct {
		keys  string
		count int
	}{{".", 2}, {"3.", 3}, {".", 3}} {
		want := change
		want.Count, want.Command = c.count, c.keys
		if got := keys(c.keys); got == nil || *got != want {
			t.Fatalf("%q made %+v, want %+v", c.keys, got, want)
		}
	}
	keys("2rz")
	want := Intent{Type: IntentSpecial, Special: SpecialReplaceChar, Char: 'z', Count: 2, Command: "."}
	if got := keys("."); got == nil || *got != want {
		t.Fatalf("'.' after 2rz made %+v, want %+v", got, want)
	}
}

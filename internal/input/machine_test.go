package input

import (
	"testing"

	"github.com/lixenwraith/terminal"
)

// TestCapturedKeyOutranksPlayGrammar asserts a captured key reaches the scenario
// from Normal mid-count and from Insert, while text entry still types it
func TestCapturedKeyOutranksPlayGrammar(t *testing.T) {
	var capture KeyCapture
	if refused := capture.Set("15é"); refused != "é" {
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

	capture.Set("")
	m.SetMode(ModeInsert)
	if in := key('1'); in == nil || in.Type != IntentTextChar {
		t.Fatalf("released key in Insert = %+v, want IntentTextChar", in)
	}
}

package hostkey

import (
	"slices"
	"testing"

	"github.com/lixenwraith/terminal"
)

// TestAHostKeyHoldsUntilItComesUpOnceAsked: keys are presses, repeats too, until a
// game asks a host that can for releases; then a press holds until its key comes up,
// the release carrying what the press did, a tap lets go as it presses, and focus
// leaving or an ask for presses ends every hold as lost
func TestAHostKeyHoldsUntilItComesUpOnceAsked(t *testing.T) {
	var got []terminal.Event
	r := Report{Post: func(ev terminal.Event) { got = append(got, ev) }}
	key := func(c rune, action terminal.KeyAction, lost bool) terminal.Event {
		return terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: c, Action: action, Lost: lost}
	}
	report := func(releases bool) terminal.Event {
		if releases {
			return terminal.Event{Type: terminal.EventKeyReport, Report: terminal.KeyReportReleases, Source: terminal.KeySourceHost}
		}
		return terminal.Event{Type: terminal.EventKeyReport, Report: terminal.KeyReportPresses}
	}
	expect := func(step string, want ...terminal.Event) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s: posted %+v, want %+v", step, got, want)
		}
		got = nil
	}
	press := key('a', terminal.KeyPress, false)

	r.Down(38, press, false)
	r.Down(38, press, true)
	r.Up(38)
	expect("before an ask", press, press)

	r.Ask(terminal.KeyReportReleases, terminal.KeySourceProtocol, true)
	r.Ask(terminal.KeyReportReleases, terminal.KeySourceHost, false)
	expect("asks a host can't take", report(false), report(false))

	r.Ask(terminal.KeyReportReleases, terminal.KeySourceProtocol|terminal.KeySourceHost, true)
	r.Down(38, key('A', terminal.KeyPress, false), false)
	r.Down(38, key('A', terminal.KeyPress, false), true)
	r.Up(38)
	r.Up(38)
	expect("a hold", report(true), key('A', terminal.KeyPress, false), key('A', terminal.KeyRepeat, false),
		key('A', terminal.KeyRelease, false))

	r.Tap(press)
	expect("a tap", press, key('a', terminal.KeyRelease, true))

	r.Down(38, press, false)
	r.Down(39, key('s', terminal.KeyPress, false), false)
	r.Lost()
	expect("focus leaving", press, key('s', terminal.KeyPress, false), key('a', terminal.KeyRelease, true),
		key('s', terminal.KeyRelease, true))

	r.Down(38, press, false)
	r.Ask(terminal.KeyReportPresses, terminal.KeySourceHost, true)
	r.Up(38)
	expect("an ask for presses", press, key('a', terminal.KeyRelease, true), report(false))
}

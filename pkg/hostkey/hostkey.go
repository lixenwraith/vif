// Package hostkey is the key report of a host that reads its platform's own key
// events, a window, a page or a phone, and posts them as a terminal would: presses
// until a game asks for releases, then each key's press, repeats and release, paired
// by the host's code for the physical key.
package hostkey

import (
	"sync"

	"github.com/lixenwraith/terminal"
)

// Report is a host's key report. Post takes the events it makes and is set before
// use; any goroutine may call any method.
type Report struct {
	Post func(terminal.Event)

	mu       sync.Mutex
	releases bool
	holds    terminal.KeyHolds
}

// Ask answers a game's SetKeyReport with an EventKeyReport: releases when it asks a
// host for them and this one can tell a release from a repeat, presses otherwise,
// which ends every hold as lost
func (r *Report) Ask(report terminal.KeyReport, allow terminal.KeySource, can bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releases = can && report == terminal.KeyReportReleases && allow&terminal.KeySourceHost != 0
	answer := terminal.Event{Type: terminal.EventKeyReport}
	if r.releases {
		answer.Report, answer.Source = terminal.KeyReportReleases, terminal.KeySourceHost
	} else {
		r.holds.ReleaseAll(r.Post)
	}
	r.Post(answer)
}

// Down is the key code names going down as ev, or repeating while held; with
// releases reported its Up ends the hold
func (r *Report) Down(code uint32, ev terminal.Event, repeat bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.releases {
		r.Post(ev)
		return
	}
	if repeat {
		ev.Action = terminal.KeyRepeat
	}
	r.holds.Report(code, ev, r.Post)
}

// Up is the key code names coming up
func (r *Report) Up(code uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.releases {
		r.holds.Report(code, terminal.Event{Type: terminal.EventKey, Action: terminal.KeyRelease}, r.Post)
	}
}

// Tap is a key with no release of its own to come, a soft keyboard's text or a key
// the host made: with releases reported it lets go as it presses, lost
func (r *Report) Tap(ev terminal.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Post(ev)
	if r.releases {
		ev.Action, ev.Lost = terminal.KeyRelease, true
		r.Post(ev)
	}
}

// Lost ends every hold as lost: the host's keys went to something else
func (r *Report) Lost() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.holds.ReleaseAll(r.Post)
}

// Held reports whether the key code names is down, for a host whose keyboard
// repeats a held key as more presses
func (r *Report) Held(code uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.holds.Held(code)
}

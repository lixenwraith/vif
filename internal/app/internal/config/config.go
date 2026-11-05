// Package config holds what configures a run without needing the run: its runtime
// shape, Mode, and the predicates assembly reads off it. internal/app aliases Mode
// and its constants, so its callers name them there. See doc/runtime.md §1.
package config

// Mode selects the runtime shape: which I/O services exist, which clock drives
// time, and whether a scheduler goroutine or the caller advances it.
type Mode uint8

const (
	// ModePlay is the interactive game: presentation, available host services,
	// and the scheduler and event goroutines.
	ModePlay Mode = iota
	// ModeHeadless has no presentation or audio and runs on a manual clock the
	// caller ticks. An authored script may still attach the network service.
	ModeHeadless
	// ModeReplay presents a recorded run: terminal and renderer, but a manual clock
	// the caller ticks and geometry taken from the journal rather than the terminal
	ModeReplay
	// ModeScript presents an authored script the same way ModeReplay presents a
	// journal: terminal and renderer over a manual clock the script driver ticks.
	// Geometry is the script's, so a presented run and a headless one simulate the
	// same world and only the presentation differs.
	ModeScript
	// ModeServer is a dedicated host: the interactive runtime with no terminal, no
	// renderer, no audio and no local cursor. It runs the real clock and the
	// scheduler goroutine like ModePlay, because a session's simulation has to
	// advance whether or not anybody is watching it here.
	ModeServer
)

// modeNames indexes Mode for diagnostics
var modeNames = [...]string{"play", "headless", "replay", "script", "server"}

// String returns the diagnostic name
func (m Mode) String() string {
	if int(m) >= len(modeNames) {
		return "invalid"
	}
	return modeNames[m]
}

// Presents reports whether the mode builds a terminal and a render pipeline
func (m Mode) Presents() bool { return m == ModePlay || m == ModeReplay || m == ModeScript }

// Driven reports whether the caller advances the clock; a driven run spawns no
// scheduler goroutine, so nothing races the world lock
func (m Mode) Driven() bool { return m != ModePlay && m != ModeServer }

// Serves reports whether the mode is a dedicated host: it authors a session and
// puts no cursor of its own on the map.
func (m Mode) Serves() bool { return m == ModeServer }

// OwnsGeometry reports whether the terminal is the authority on world dimensions.
// A replay's geometry is recorded, so its terminal drives presentation only.
func (m Mode) OwnsGeometry() bool { return m == ModePlay }

// OwnsInput reports whether terminal input drives the simulation. A replay's input
// drives playback controls instead, so the mode router never sees it.
func (m Mode) OwnsInput() bool { return m == ModePlay }

// Audio reports whether this mode and build register an audio service.
func (m Mode) Audio() bool {
	return buildHasAudio && (m == ModePlay || m == ModeReplay || m == ModeScript)
}

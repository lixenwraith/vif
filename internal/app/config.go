package app

import (
	"errors"
	"fmt"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/resource"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// Terminal-equivalent dimensions applied when a caller-driven run leaves them unset
const (
	DefaultWidth  = 80
	DefaultHeight = 24
)

// Mode selects the runtime shape: which I/O services exist, which clock drives
// time, and whether a scheduler goroutine or the caller advances it.
type Mode uint8

const (
	// ModePlay is the interactive game: terminal, audio, network, renderer, and
	// the scheduler and event goroutines
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

// Audio reports whether an audio service is registered. A driven mode advances the
// simulation only through its driver, so AudioSystem must push no event a system
// reads.
func (m Mode) Audio() bool { return m == ModePlay || m == ModeReplay || m == ModeScript }

// Config is the resolved startup configuration
// Built from CLI flags by cmd/vif, or programmatically by embedders
// (map editor, wasm entry, headless harness) that have no flag set
type Config struct {
	// Mode selects the runtime shape; the zero value is the interactive game
	Mode Mode

	// Resources names every external file this run may load: the config root and
	// the individual game, content, keymap and audio overrides.
	Resources resource.Options

	// ColorMode overrides terminal detection when ColorModeSet is true
	ColorMode    terminal.ColorMode
	ColorModeSet bool

	// AudioBackend forces a named backend; "" = auto-detect priority chain
	AudioBackend string

	// AudioMuted is the initial effect mute state
	AudioMuted bool

	// LogScope is the initial scope spec; "" = all
	LogScope string

	// StatTicks overrides the status snapshot period in game ticks;
	// 0 = parameter default, negative = disabled
	StatTicks int

	// RecTicks overrides the flight-recorder depth in game ticks;
	// 0 = parameter default, negative = disabled
	RecTicks int

	// TimeScaleSpec is the initial simulation rate ladder token; "" = real time.
	//
	// An authored script reads it as the rate its ticks are paced against the wall,
	// where "1" is real time and ScriptPaceMax is as fast as the driver can go. A
	// solo script defaults to ScriptPaceMax and one in a session to real time,
	// because a participant that outran its peer would not be in the session it is
	// simulating.
	TimeScaleSpec string

	// System seed for RNG
	Seed uint64

	// Journal enables the replay journal, written to its own file
	Journal bool

	// JournalSink overrides the journal destination; nil opens the vlog journal
	// file. Harnesses set it to capture records in memory.
	JournalSink event.JournalSink

	// HostAddress binds a startup-only two-participant session. The scheduler
	// remains at tick zero until the joining participant passes the start gate.
	HostAddress string

	// JoinAddress connects to a startup-only session. The host anchor supplies
	// simulation identity before the App is constructed.
	JoinAddress string

	// Participants is the lobby size a host waits for, itself included. A startup
	// host treats zero as two; a solo run opened later with :host treats zero as
	// parameter.MaxPlayers. The ceiling is also the roster width.
	//
	// ModeServer is the exception, twice over: the server is not one of them, so
	// the value counts guests rather than participants — and it is a ceiling only.
	// A dedicated host starts on its first guest and admits the rest through the
	// mid-run gate as they arrive, so zero means the full roster rather than one.
	// Waiting for a named number would make a fleet host's readiness a function of
	// how many people happened to want to play.
	Participants int

	// Width and Height are the terminal-equivalent dimensions a caller-driven run
	// assumes; margins apply as usual, so the viewport is smaller than these.
	// Ignored when the terminal owns geometry; zero selects the defaults.
	Width, Height int

	// MapWidth, MapHeight and CropOnResize are the D-14 map latch a joining run
	// adopts instead of deriving from its own terminal. They are applied to the
	// world before the FSM boots, because the boot script spawns cursor slot zero
	// centred on the map: a joiner that adopted the latch after construction would
	// already hold that cursor on a different cell than the host, which is a shared
	// position and diverges permanently. Zero width or height means no latch and the
	// terminal decides, which is what a solo or hosting run does.
	MapWidth, MapHeight int
	CropOnResize        bool

	// ProbeAddress binds the liveness, readiness and metrics endpoint. Empty
	// leaves it unbound, which is what an interactive run wants: a person watching
	// the screen is the probe.
	ProbeAddress string

	// LockMap latches the world as shared before the FSM boots, so this run's
	// terminal never rewrites shared map bounds and its crossings take the session's
	// playout lead. A hosting run sets it, because its bounds are what every joiner
	// adopts from the anchor and a crop between the offer a participant dialled and
	// the gate that starts it would move bounds that participant has already built
	// its world on. A run reproducing a session sets it from the anchor's
	// SessionShared, which is how a replay and a catch-up reach the same bounds and
	// the same apply ticks as the run they reproduce.
	LockMap bool

	// networkConfig is prepared by Run after host/join negotiation. Keeping the
	// transport detail private leaves Config's public session surface role-neutral.
	networkConfig *network.Config

	// scriptedSession admits headless network I/O only through RunScript, which
	// performs the startup gate and owns wall pacing.
	scriptedSession bool

	// geometryDefaulted records that Normalize supplied Width or Height because
	// nobody named one. It is what lets a dedicated host tell "size me from the
	// session" apart from "serve exactly this": a server with no -size has no
	// terminal to derive a map from and would otherwise serve the fallback one.
	//
	// Normalize runs more than once on the way to a session — once resolving the
	// handshake and once inside New — so it is set only on the pass that actually
	// fills a zero, and a later pass finds the value the earlier one wrote.
	geometryDefaulted bool
}

// ConfigForJoin applies the host-authored simulation identity to local operator options.
func ConfigForJoin(local Config, o network.SessionOffer) (Config, error) {
	fromAnchor, err := ConfigFromAnchor(o.Anchor.Anchor)
	if err != nil {
		return Config{}, err
	}
	local.Seed = fromAnchor.Seed
	local.Resources.Embedded = fromAnchor.Resources.Embedded
	local.Resources.Game = fromAnchor.Resources.Game
	local.Resources.Content = fromAnchor.Resources.Content
	// The map latch travels with identity rather than being adopted afterwards: the
	// FSM boots inside New and spawns cursor slot zero at the centre of whatever map
	// it finds, so a latch applied later leaves that shared cursor on this
	// terminal's centre instead of the session's (D-14, D-11).
	local.MapWidth = fromAnchor.MapWidth
	local.MapHeight = fromAnchor.MapHeight
	local.CropOnResize = fromAnchor.CropOnResize
	local.LockMap = true
	if !local.Mode.Driven() {
		local.TimeScaleSpec = o.Anchor.Anchor.Speed
	}
	return local, local.Validate()
}

// Normalize fills unset fields that carry a defined default
func (c *Config) Normalize() {
	if c.Mode.OwnsGeometry() {
		return
	}
	if c.Width == 0 {
		c.Width, c.geometryDefaulted = DefaultWidth, true
	}
	if c.Height == 0 {
		c.Height, c.geometryDefaulted = DefaultHeight, true
	}
}

// Validate reports configuration conflicts
func (c Config) Validate() error {
	if err := c.Resources.Validate(); err != nil {
		return err
	}
	if c.HostAddress != "" && c.JoinAddress != "" {
		return errors.New("-host and -join are mutually exclusive")
	}
	if c.HostAddress != "" || c.JoinAddress != "" {
		switch {
		case c.Mode == ModeReplay:
			return fmt.Errorf("%s: journal playback cannot join a network session", c.Mode)
		case c.Mode != ModePlay && !c.Mode.Serves() && !c.scriptedSession:
			return fmt.Errorf("%s: network sessions require app.RunScript", c.Mode)
		}
	}
	minParticipants := 2
	if c.Mode.Serves() {
		minParticipants = 1 // a dedicated host is not one of them
	}
	if c.Participants != 0 && (c.Participants < minParticipants || c.Participants > parameter.MaxPlayers) {
		return fmt.Errorf("-players %d is outside the supported range %d..%d",
			c.Participants, minParticipants, parameter.MaxPlayers)
	}
	if c.Mode.Serves() && c.HostAddress == "" {
		return errors.New("server: a dedicated host needs a bind address")
	}
	if c.ProbeAddress != "" && !c.Mode.Serves() {
		// Refused rather than ignored, for the reason validateDriven refuses a
		// colour mode with no terminal: a flag that silently does nothing is worse
		// than one that says it cannot.
		return fmt.Errorf("%s: -probe serves a dedicated host; this mode has no supervisor to answer", c.Mode)
	}
	if c.Participants != 0 && c.JoinAddress != "" {
		return errors.New("-players configures a host, not a joining guest")
	}
	if c.LogScope != "" {
		if _, err := vlog.ParseScopes(c.LogScope, vlog.ScopeAll); err != nil {
			return err
		}
	}
	if c.TimeScaleSpec != "" && !c.scriptedSession {
		if _, ok := engine.ParseScale(c.TimeScaleSpec); !ok {
			return fmt.Errorf("-speed %q is not a ladder rate (1/8 1/4 1/2 1 2 4 8)", c.TimeScaleSpec)
		}
	}
	if c.scriptedSession && c.TimeScaleSpec == ScriptPaceMax &&
		(c.HostAddress != "" || c.JoinAddress != "") {
		return errors.New("-speed max cannot pace a session: a script that outruns its peer " +
			"is not simulating the session it is in")
	}
	if c.Mode.OwnsGeometry() {
		return nil
	}
	return c.validateDriven()
}

// validateDriven rejects settings a run without its own terminal cannot honour,
// rather than accepting a flag that would silently do nothing
func (c Config) validateDriven() error {
	if c.ColorModeSet && !c.Mode.Presents() {
		return fmt.Errorf("%s: color mode is unused, no terminal is created", c.Mode)
	}
	if c.AudioBackend != "" && !c.Mode.Audio() {
		return fmt.Errorf("%s: audio backend is unused, no audio service is created", c.Mode)
	}
	if (c.Resources.Music != "" || c.Resources.Sounds != "") && !c.Mode.Audio() {
		return fmt.Errorf("%s: audio config is unused, no audio service is created", c.Mode)
	}
	// A manual clock records a simulation rate and never applies it. An authored
	// script reads the same field as its wall pace, which is a property of the run
	// rather than of the clock; every other driven mode has nothing to do with it.
	if c.TimeScaleSpec != "" {
		switch {
		case c.scriptedSession:
			if _, _, err := scriptPacing(c); err != nil {
				return err
			}
		case c.Mode.Serves():
			return fmt.Errorf("%s: a dedicated host runs at the session's rate", c.Mode)
		default:
			return fmt.Errorf("%s: the manual clock is driven by Tick, not by a rate", c.Mode)
		}
	}
	// Matches what MetaSystem admits, so a live journal always rebuilds a valid config
	if !engine.ViewportFits(c.Width, c.Height) {
		return fmt.Errorf("%s: %dx%d leaves no game area", c.Mode, c.Width, c.Height)
	}
	return nil
}

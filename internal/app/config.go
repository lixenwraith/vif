package app

import (
	"errors"
	"fmt"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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
)

// modeNames indexes Mode for diagnostics
var modeNames = [...]string{"play", "headless", "replay"}

// String returns the diagnostic name
func (m Mode) String() string {
	if int(m) >= len(modeNames) {
		return "invalid"
	}
	return modeNames[m]
}

// Presents reports whether the mode builds a terminal and a render pipeline
func (m Mode) Presents() bool { return m == ModePlay || m == ModeReplay }

// Driven reports whether the caller advances the clock; a driven run spawns no
// scheduler goroutine, so nothing races the world lock
func (m Mode) Driven() bool { return m != ModePlay }

// OwnsGeometry reports whether the terminal is the authority on world dimensions.
// A replay's geometry is recorded, so its terminal drives presentation only.
func (m Mode) OwnsGeometry() bool { return m == ModePlay }

// OwnsInput reports whether terminal input drives the simulation. A replay's input
// drives playback controls instead, so the mode router never sees it.
func (m Mode) OwnsInput() bool { return m == ModePlay }

// Audio reports whether an audio service is registered. The replay drives the
// simulation only through ReplayDriver, so AudioSystem must push no event a system reads.
func (m Mode) Audio() bool { return m == ModePlay || m == ModeReplay }

// Config is the resolved startup configuration
// Built from CLI flags by cmd/vi-fighter, or programmatically by embedders
// (map editor, wasm entry, headless harness) that have no flag set
type Config struct {
	// Mode selects the runtime shape; the zero value is the interactive game
	Mode Mode

	// ColorMode overrides terminal detection when ColorModeSet is true
	ColorMode    terminal.ColorMode
	ColorModeSet bool

	// AudioBackend forces a named backend; "" = auto-detect priority chain
	AudioBackend string

	// AudioMuted is the initial effect mute state
	AudioMuted bool

	// ContentPath is a corpus directory or a single content file;
	// "" = discovery, falling back to the embedded corpus
	ContentPath string

	// GameScript is a game.toml path or a map directory; "" = config discovery
	GameScript string

	// ForceDefault selects the embedded FSM config and corpus, ignoring GameScript and ContentPath
	ForceDefault bool

	// KeymapPath is a keymap TOML path; "" = keymap discovery
	KeymapPath string

	// LogScope is the initial scope spec; "" = all
	LogScope string

	// StatTicks overrides the status snapshot period in game ticks;
	// 0 = parameter default, negative = disabled
	StatTicks int

	// RecTicks overrides the flight-recorder depth in game ticks;
	// 0 = parameter default, negative = disabled
	RecTicks int

	// TimeScaleSpec is the initial simulation rate ladder token; "" = real time
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

	// Participants is the lobby size a host waits for, itself included. Zero means
	// two. The ceiling is parameter.MaxPlayers, which is also the roster width.
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
}

// ConfigForJoin applies the host-authored simulation identity to local operator options.
func ConfigForJoin(local Config, o network.SessionOffer) (Config, error) {
	fromAnchor, err := ConfigFromAnchor(o.Anchor.Anchor)
	if err != nil {
		return Config{}, err
	}
	local.Seed = fromAnchor.Seed
	local.ForceDefault = fromAnchor.ForceDefault
	local.GameScript = fromAnchor.GameScript
	local.ContentPath = fromAnchor.ContentPath
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
		c.Width = DefaultWidth
	}
	if c.Height == 0 {
		c.Height = DefaultHeight
	}
}

// Validate reports configuration conflicts
func (c Config) Validate() error {
	if c.HostAddress != "" && c.JoinAddress != "" {
		return errors.New("-host and -join are mutually exclusive")
	}
	if c.HostAddress != "" || c.JoinAddress != "" {
		switch {
		case c.Mode == ModeReplay:
			return fmt.Errorf("%s: journal playback cannot join a network session", c.Mode)
		case c.Mode == ModeHeadless && !c.scriptedSession:
			return fmt.Errorf("%s: network sessions require app.RunScript", c.Mode)
		}
	}
	if c.Participants != 0 && (c.Participants < 2 || c.Participants > parameter.MaxPlayers) {
		return fmt.Errorf("-players %d is outside the supported range 2..%d",
			c.Participants, parameter.MaxPlayers)
	}
	if c.Participants != 0 && c.HostAddress == "" {
		return errors.New("-players applies to -host")
	}
	if c.ForceDefault && (c.GameScript != "" || c.ContentPath != "") {
		return errors.New("-d is mutually exclusive with -g and -f")
	}
	if c.LogScope != "" {
		if _, err := vlog.ParseScopes(c.LogScope, vlog.ScopeAll); err != nil {
			return err
		}
	}
	if c.TimeScaleSpec != "" {
		if _, ok := engine.ParseScale(c.TimeScaleSpec); !ok {
			return fmt.Errorf("-speed %q is not a ladder rate (1/8 1/4 1/2 1 2 4 8)", c.TimeScaleSpec)
		}
	}
	if c.Mode.OwnsGeometry() {
		return nil
	}
	return c.validateDriven()
}

// validateDriven rejects settings a caller-driven run cannot honour, rather than
// accepting a flag that would silently do nothing
func (c Config) validateDriven() error {
	if c.ColorModeSet && !c.Mode.Presents() {
		return fmt.Errorf("%s: color mode is unused, no terminal is created", c.Mode)
	}
	if c.AudioBackend != "" && !c.Mode.Audio() {
		return fmt.Errorf("%s: audio backend is unused, no audio service is created", c.Mode)
	}
	// TimeScaleSpec sets the simulation rate, which a manual clock records but never
	// applies; playback pacing is a presentation knob and gets its own field
	if c.TimeScaleSpec != "" {
		return fmt.Errorf("%s: the manual clock is driven by Tick, not by a rate", c.Mode)
	}
	// Matches what MetaSystem admits, so a live journal always rebuilds a valid config
	if !engine.ViewportFits(c.Width, c.Height) {
		return fmt.Errorf("%s: %dx%d leaves no game area", c.Mode, c.Width, c.Height)
	}
	return nil
}

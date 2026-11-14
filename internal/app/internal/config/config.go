// Package config holds what a run is configured with: its runtime shape, Mode, and
// the predicates assembly reads off it; Config, its session half Membership, and the
// rules Validate holds them to in this build. internal/app aliases them, so its
// callers name them there. See doc/runtime.md §1.
package config

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
	"github.com/lixenwraith/vif/pkg/vmath"
)

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

// Terminal-equivalent dimensions applied when a caller-driven run leaves them unset
const (
	DefaultWidth  = 80
	DefaultHeight = 24

	// BotWidth and BotHeight are a bot's screen where nothing names another: a
	// headless bot given no -size, a seat whose holder names none, and so the map a
	// server's own bots start alone.
	BotWidth  = 120
	BotHeight = 40
)

// SocketNetwork reports whether this build binds and dials sockets, which a
// browser's cannot: it reaches a session over its site's WebSocket route alone.
const SocketNetwork = runtime.GOARCH != "wasm"

// ErrBrowserHosting is why a browser never hosts: a page opens connections but
// cannot accept one, so no player could reach a session it opened
var ErrBrowserHosting = errors.New("hosting unavailable in a browser, which cannot accept connections; host from the installed game")

// Config is the resolved startup configuration, built from CLI flags by cmd/vif, or
// by the Android host and the headless harness, which have no flag set
type Config struct {
	// Mode selects the runtime shape; the zero value is the interactive game
	Mode Mode

	// Resources names every external file this run may load: the config root and
	// the individual scenario, content, keymap and audio overrides.
	Resources resource.Options

	// ColorMode overrides terminal detection when ColorModeSet is true
	ColorMode    terminal.ColorMode
	ColorModeSet bool
	// NoTmuxFrameCap draws the terminal at the run's rate inside tmux or screen,
	// vif.toml's [presentation] tmux_frame_cap = false
	NoTmuxFrameCap bool

	// AudioBackend forces a named backend; "" = auto-detect priority chain
	AudioBackend string

	// Play is how the run starts: the audio channels, the sfx volume, auto-fire,
	// the mouse and the HUD, vif.toml's [play] as a flag amended it; one naming no
	// auto-fire word takes the embedded table. Settings is where a change the player
	// makes is written back; nil keeps it to this run.
	Play     paths.Play
	Settings paths.SettingsStore

	// Notice is what the run's status line opens with: what the host's start found,
	// such as settings it dropped, or the failure of the run this one replaces.
	Notice string

	// MusicWAV is the directory the music is recorded to as a WAV file; "" records none.
	MusicWAV string

	// AudioBuffer is the mixer period; zero is the audio package's default
	AudioBuffer time.Duration

	// LogScope is the initial scope spec; "" = all
	LogScope string

	// StatTicks overrides the status snapshot period in game ticks;
	// 0 = parameter default, negative = disabled
	StatTicks int

	// RecTicks overrides the flight-recorder depth in game ticks;
	// 0 = parameter default, negative = disabled
	RecTicks int

	// TimeScaleSpec is the initial simulation rate ladder token; "" = real time. An
	// authored script reads it as the wall rate its ticks are paced at, defaulting
	// to internal/app's ScriptPaceMax solo and to real time in a session.
	TimeScaleSpec string

	// TickInterval is the game time one tick advances, chosen by a session's host
	// and adopted by its joiners; zero is parameter.GameUpdateInterval.
	TickInterval time.Duration

	// System seed for RNG
	Seed uint64

	// Session is the RNG session a run reproduces rather than counts to: the seed
	// names the family of streams, this names the game in it. Zero counts from one;
	// a join and a replay adopt the anchor's number, because a restarted session is
	// several games in and every stream it draws is a function of that number.
	Session uint64

	// Journal enables the replay journal, written to its own file
	Journal bool

	// JournalSink overrides the journal destination; nil opens the vlog journal
	// file. Harnesses set it to capture records in memory.
	JournalSink event.JournalSink

	// Membership is the session half: what the membership holds of this run's
	// configuration, and writes back for the run that replaces this one.
	Membership Membership

	// Width and Height are the terminal-equivalent dimensions a caller-driven run
	// assumes; margins apply as usual, so the viewport is smaller than these.
	// Ignored when the terminal owns geometry; zero selects the defaults.
	Width, Height int

	// MapWidth, MapHeight and CropOnResize are the D-14 map latch a joining run
	// adopts instead of deriving from its terminal. Applied before the FSM boots:
	// the boot script spawns cursor slot zero centred on the map, so a latch adopted
	// later leaves that shared cursor on the wrong cell. Zero means no latch.
	MapWidth, MapHeight int
	CropOnResize        bool
	// CellAspect is the cell's width over its height the geometry runs at; zero
	// takes the presentation's, which for a terminal is the reference
	CellAspect vmath.Aspect

	// ProbeAddress binds the liveness, readiness and metrics endpoint. Empty
	// leaves it unbound, which is what an interactive run wants: a person watching
	// the screen is the probe.
	ProbeAddress string

	// Lifetime bounds an allocated session: the first-guest window, the empty-roster
	// grace, and the drain a termination request opens. The zero policy times out
	// nothing, which is what an interactively started host wants. See
	// internal/lifecycle.
	Lifetime lifecycle.Policy

	// LockMap latches the world as shared before the FSM boots, so this run's
	// terminal never rewrites shared map bounds and its crossings take the session's
	// playout lead. A hosting run sets it because its bounds are what joiners adopt;
	// a run reproducing a session sets it from the anchor's SessionShared.
	LockMap bool
}

// Normalize fills unset fields that carry a defined default
func (c *Config) Normalize() {
	if c.Play.AutoFire == "" {
		c.Play = paths.DefaultPlay()
	}
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

// Validate reports configuration conflicts: the rules a Config is held to in this
// build, whichever run it is. A run's own, which its wiring relaxes or adds, are
// internal/app's.
func (c Config) Validate() error {
	if err := c.Resources.Validate(); err != nil {
		return err
	}
	if c.TickInterval != 0 && (c.TickInterval < parameter.TickIntervalMin || c.TickInterval > parameter.TickIntervalMax) {
		return fmt.Errorf("-tick %s is outside %s to %s", c.TickInterval, parameter.TickIntervalMin, parameter.TickIntervalMax)
	}
	if err := validateBuildConfig(c); err != nil {
		return err
	}
	if err := validateAudioBuildConfig(c); err != nil {
		return err
	}
	if c.Membership.JoinAddress != "" {
		if _, err := link.ParseEndpoint(c.Membership.JoinAddress); err != nil {
			return err
		}
	}
	if c.Membership.HostAddress != "" {
		if _, err := link.Doors(c.Membership.HostAddress); err != nil {
			return err
		}
	}
	if c.Membership.RelayAddress != "" {
		relay, err := link.ParseEndpoint(c.Membership.RelayAddress)
		_, router := RelayRouter(c.Membership.RelayAddress)
		switch {
		case err != nil:
		case router && relay.Name != "":
			err = errors.New("-relay vif://<router> names no session: the router names the door")
		case !router:
			err = relay.Listenable()
		}
		if err == nil && c.Membership.JoinAddress == "" {
			err = errors.New("-relay admits participants to a session this run joined")
		}
		if err != nil {
			return err
		}
	}
	// A browser has no listener to host or serve from. What it may join is the run's
	// rule, since its bots dial nothing.
	if !SocketNetwork {
		switch {
		case c.Membership.HostAddress != "":
			return ErrBrowserHosting
		case c.Membership.RelayAddress != "":
			return errors.New("a browser relays only its own bots")
		}
	}
	if c.Membership.HostAddress != "" && c.Membership.JoinAddress != "" {
		return errors.New("-host and -join are mutually exclusive")
	}
	if (c.Membership.HostAddress != "" || c.Membership.JoinAddress != "") && c.Mode == ModeReplay {
		return fmt.Errorf("%s: journal playback cannot join a network session", c.Mode)
	}
	minParticipants := 2
	if c.Mode.Serves() {
		minParticipants = 1 // a dedicated host is not one of them
	}
	if n := c.Membership.Participants; n != 0 && (n < minParticipants || n > parameter.MaxPlayers) {
		return fmt.Errorf("-players %d is outside the supported range %d..%d",
			n, minParticipants, parameter.MaxPlayers)
	}
	if c.Mode.Serves() && c.Membership.HostAddress == "" {
		return errors.New("server: a dedicated host needs a bind address")
	}
	if err := c.Lifetime.Validate(); err != nil {
		return err
	}
	if c.Lifetime.Bounded() && !c.Mode.Serves() {
		// Refused rather than ignored, for the same reason -probe is: the bounds
		// end a process, and one that silently kept a flag that was meant to end it
		// is the failure this would be hiding.
		return fmt.Errorf("%s: session lifetime bounds an allocated -serve session; this mode is ended by its operator", c.Mode)
	}
	if c.ProbeAddress != "" && !c.Mode.Serves() {
		// Refused rather than ignored, for the reason validateDriven refuses a
		// colour mode with no terminal: a flag that silently does nothing is worse
		// than one that says it cannot.
		return fmt.Errorf("%s: -probe serves a dedicated host; this mode has no supervisor to answer", c.Mode)
	}
	if c.Membership.Participants != 0 && c.Membership.JoinAddress != "" {
		return errors.New("-players configures a host, not a joining guest")
	}
	if c.Membership.Watch {
		// A viewer holds no place: nothing of its run is in the session it watches.
		switch {
		case c.Membership.JoinAddress == "":
			return errors.New("-spectate names the session to watch")
		case len(c.Membership.Bots) != 0:
			return errors.New("-spectate holds no place a bot could play from")
		case c.Membership.RelayAddress != "" || c.Membership.ListenAddress != "":
			return errors.New("-spectate opens no door: -relay and -listen are a participant's")
		case c.Journal:
			return errors.New("-spectate keeps no journal")
		}
	}
	if len(c.Membership.Bots) > parameter.MaxPlayers {
		return fmt.Errorf("-bot %d is more than the %d a session holds", len(c.Membership.Bots), parameter.MaxPlayers)
	}
	if c.LogScope != "" {
		if _, err := vlog.ParseScopes(c.LogScope, vlog.ScopeAll); err != nil {
			return err
		}
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
	// Matches what MetaSystem admits, so a live journal always rebuilds a valid config
	if !engine.ViewportFits(c.Width, c.Height) {
		return fmt.Errorf("%s: %dx%d leaves no game area", c.Mode, c.Width, c.Height)
	}
	return nil
}

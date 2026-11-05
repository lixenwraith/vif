package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/vif/pkg/vmath"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
)

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

// Config is the resolved startup configuration
// Built from CLI flags by cmd/vif, or programmatically by embedders
// (map editor, wasm entry, headless harness) that have no flag set
type Config struct {
	// Mode selects the runtime shape; the zero value is the interactive game
	Mode Mode

	// Resources names every external file this run may load: the config root and
	// the individual scenario, content, keymap and audio overrides.
	Resources resource.Options

	// Host is the platform's adapters; the zero value is the build's own
	Host Host

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
	// to ScriptPaceMax solo and to real time in a session.
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

	// resumeHost is Run's own: the address this run opens once its clock runs, the
	// -host a player gave or the door a rebuilt coordinator reopens. It is not
	// HostAddress because nothing waits in a startup lobby: guests use the mid-run gate.
	resumeHost string

	// networkConfig is prepared by Run after host/join negotiation. Keeping the
	// transport detail private leaves Config's public session surface role-neutral.
	networkConfig *link.Config

	// dialled is a :join its session already answered, dialled while the run it
	// replaces played on.
	dialled *joinDial

	// startScreen says this run is the launch on a host without a start screen of its
	// own, which waits behind the game's (App.startScreen)
	startScreen bool

	// journalCarried says Journal came from the run this one replaces, which was
	// journaling or asked to start: one that fails to open leaves this run
	// unjournaled, not unstarted.
	journalCarried bool

	// log is a secondary runtime's handle: a seat's or a staging world's is tagged,
	// a replay copy's muted until presented. Nil takes the process's or, driven, its own.
	log *vlog.Log

	// borrow is another replay's presentation, whose terminal a replay rebuilt
	// behind its viewer draws on; it has no speakers, since two mixers would open
	// one device.
	borrow *presentationState

	// scriptedSession admits headless network I/O only through RunScript, which
	// performs the startup gate and owns wall pacing.
	scriptedSession bool

	// terminalGeometry sizes a presented run from its terminal, as play does; a
	// presented bot takes it when -size names none.
	terminalGeometry bool

	// geometryDefaulted records that Normalize supplied Width or Height, which is
	// how a dedicated host tells "size me from the session" apart from "serve
	// exactly this". Normalize runs more than once on the way to a session, so only
	// the pass that fills a zero sets it.
	geometryDefaulted bool
}

// ConfigForJoin applies the host-authored simulation identity to local operator options.
func ConfigForJoin(local Config, o network.SessionOffer) (Config, error) {
	fromAnchor, err := ConfigFromAnchor(o.Anchor.Anchor)
	if err != nil {
		return Config{}, err
	}
	local.Seed = fromAnchor.Seed
	local.Session = fromAnchor.Session
	// The scenario's name is only a hint for the root lookup, which -d skips:
	// resolveJoinScenario compares digests and fetches the bytes no root here holds.
	// The corpus is not taken at all: what a player types is resolved here, from
	// this machine's roots or -d, and never reconciled with the coordinator.
	if !local.Resources.Embedded {
		local.Resources.Scenario = fromAnchor.Resources.Scenario
	}
	// The map latch travels with identity rather than being adopted afterwards: the
	// FSM boots inside New and spawns cursor slot zero at the centre of whatever map
	// it finds, so a latch applied later leaves that shared cursor on this
	// terminal's centre instead of the session's (D-14, D-11).
	local.MapWidth = fromAnchor.MapWidth
	local.MapHeight = fromAnchor.MapHeight
	local.CropOnResize = fromAnchor.CropOnResize
	local.CellAspect = fromAnchor.CellAspect
	local.LockMap = true
	local.TickInterval = fromAnchor.TickInterval
	local.Membership.sessionPlayers = o.Players
	// The session's rate is its host's, a script's or bot's pace included; a
	// harness's manual clock is driven by Tick and takes none.
	if !local.Mode.Driven() || local.scriptedSession {
		local.TimeScaleSpec = o.Anchor.Anchor.Speed
	}
	return local, local.Validate()
}

// validateWith checks this configuration with s as its session half: a join's,
// before anything is dialled.
func (c Config) validateWith(s Membership) error {
	c.Membership = s
	return c.Validate()
}

// prepare is the resources a dial of s's session builds its world on, settled on
// its preview: this configuration as the session makes it, checked, with the
// session's scenario resolved.
func (c Config) prepare(s Membership, pending *network.PendingJoin, preview network.SessionOffer) (resource.Options, error) {
	c.Membership = s
	var err error
	if c, err = ConfigForJoin(c, preview); err == nil {
		err = resolveJoinScenario(&c, pending, preview)
	}
	return c.Resources, err
}

// joinNotice adds one line to what the next run's status line opens with.
func joinNotice(notice, line string) string {
	if notice == "" {
		return line
	}
	return notice + "; " + line
}

// restarted is the run next asks for, built on this one's command line, and the
// solo run a :join that fails after its dial plays on as. Hosting resumes after the
// clock rather than before it: the next run is not waiting for a lobby, it is
// reopening a door its guests are already at.
func (c Config) restarted(next *restartRequest) (Config, *Config) {
	c.Notice, c.dialled, c.Membership.Bots, c.Play = next.Notice, nil, next.Bots, next.Play
	c.Journal, c.journalCarried = next.Journal, next.Journal
	c.Membership.joinPress, c.Resources.Provided = next.press, next.provided
	if next.Scenario != "" {
		c.Resources.Scenario, c.Resources.Embedded = next.Scenario, false
	}
	c.Membership.HostAddress, c.resumeHost = "", next.Host
	switch {
	case next.Rejoin:
		if next.Join != "" {
			c.Membership.JoinAddress = next.Join
		}
	case next.Join == "" && next.dialled != nil:
		solo := c
		solo.Membership.JoinAddress, solo.Membership.SessionName, solo.Membership.sessionPlayers = "", "", 0
		c.dialled = next.dialled
		return c, &solo
	case next.Join != "":
		solo := c
		solo.Membership.JoinAddress, solo.Membership.SessionName, solo.Membership.sessionPlayers = "", "", 0
		joined, err := c.Membership.joining(next.Join)
		if err != nil { // parsed when latched, so a solo run is what is left
			next.dialled.abandon(err)
			solo.Notice = "Join failed: " + err.Error()
			return solo, nil
		}
		c.Membership, c.dialled = joined, next.dialled
		return c, &solo
	default:
		// Not following anyone: a run that led its session, and one that
		// inherited it and has nobody left, both start over on their own.
		c.Membership.JoinAddress = ""
	}
	return c, nil
}

// Normalize fills unset fields that carry a defined default
func (c *Config) Normalize() {
	if c.Membership.claims == nil {
		c.Membership.claims = newClaimKeeper(c.Membership.ClaimStore)
	}
	if c.Play.AutoFire == "" {
		c.Play = paths.DefaultPlay()
	}
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
	if c.TickInterval != 0 && (c.TickInterval < parameter.TickIntervalMin || c.TickInterval > parameter.TickIntervalMax) {
		return fmt.Errorf("-tick %s is outside %s to %s", c.TickInterval, parameter.TickIntervalMin, parameter.TickIntervalMax)
	}
	if err := validateBuildConfig(c); err != nil {
		return err
	}
	if err := validateAudioBuildConfig(c); err != nil {
		return err
	}
	// A browser joins over a session's WebSocket route and nothing else: it has no
	// socket to dial a host:port with, so a link naming a session there is looked up
	// on its site, and no listener to host or serve from. Its bots dial nothing,
	// joining it in-process.
	var join link.Endpoint
	if c.Membership.JoinAddress != "" {
		var err error
		if join, err = link.ParseEndpoint(c.Membership.JoinAddress); err != nil {
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
		_, router := c.Membership.relayRouter()
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
	if !buildHasSocketNetwork {
		switch {
		case c.Membership.HostAddress != "":
			return errBrowserHosting
		case c.Membership.RelayAddress != "":
			return errors.New("a browser relays only its own bots")
		case c.Membership.JoinAddress != "" && c.Membership.relay == nil &&
			join.Scheme != link.SchemeWebSocket && c.Membership.SessionName == "":
			return errors.New("a browser cannot dial host:port; join with a session's link")
		}
	}
	if c.Membership.HostAddress != "" && c.Membership.JoinAddress != "" {
		return errors.New("-host and -join are mutually exclusive")
	}
	if c.Membership.HostAddress != "" || c.Membership.JoinAddress != "" {
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
	if len(c.Membership.Bots) > parameter.MaxPlayers {
		return fmt.Errorf("-bot %d is more than the %d a session holds", len(c.Membership.Bots), parameter.MaxPlayers)
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
		(c.Membership.HostAddress != "" || c.Membership.JoinAddress != "" || len(c.Membership.Bots) > 0) {
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
			if _, err := scriptPacing(c); err != nil {
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

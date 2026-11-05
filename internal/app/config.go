package app

import (
	"errors"
	"fmt"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/app/internal/membership"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
)

// Terminal-equivalent dimensions applied when a caller-driven run leaves them
// unset, and a bot's screen where nothing names another (config.BotWidth)
const (
	DefaultWidth  = config.DefaultWidth
	DefaultHeight = config.DefaultHeight
	BotWidth      = config.BotWidth
	BotHeight     = config.BotHeight
)

// Mode is config's runtime shape, aliased with its constants for the callers that
// build a Config.
type Mode = config.Mode

const (
	ModePlay     = config.ModePlay
	ModeHeadless = config.ModeHeadless
	ModeReplay   = config.ModeReplay
	ModeScript   = config.ModeScript
	ModeServer   = config.ModeServer
)

// Config, its session half Membership and the ClaimStore that keeps a process's
// claims are config's, aliased for the callers that build a Config: cmd/vif from its
// flags, and the Android host and the headless harness with no flag set.
type (
	Config     = config.Config
	Membership = config.Membership
	ClaimStore = config.ClaimStore
)

// runConfig is a run's Config with what the run is built with beside it: host, the
// platform's adapters its caller hands over, and the wiring, which this package
// sets and no caller can. The App keeps it as cfg; its Validate adds the wiring's
// rules to Config's, and its Normalize what the wiring settles.
type runConfig struct {
	Config
	host   Host
	wiring wiring
}

// wiring is what a run is built with beside its Config, none of it a caller's to
// set: what one run hands the next, what a script's, a bot's or a seat's run, a
// replay's copy or a staging world is made with, and what construction settles.
type wiring struct {
	// resumeHost is Run's own: the address this run opens once its clock runs, the
	// -host a player gave or the door a rebuilt coordinator reopens. It is not
	// HostAddress because nothing waits in a startup lobby: guests use the mid-run gate.
	resumeHost string

	// networkConfig is the transport the host or join negotiation prepared.
	networkConfig *link.Config

	// dialled is a :join its session already answered, dialled while the run it
	// replaces played on.
	dialled *membership.JoinDial

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

	// membership is the wiring's session half, which the membership holds.
	membership membership.Wiring
}

// configForJoin applies the host-authored simulation identity to local operator
// options, and the session's -players to the run's wiring.
func configForJoin(local runConfig, o network.SessionOffer) (runConfig, error) {
	fromAnchor, err := ConfigFromAnchor(o.Anchor.Anchor)
	if err != nil {
		return runConfig{}, err
	}
	local.Seed = fromAnchor.Seed
	local.Session = fromAnchor.Session
	// The scenario's name is only a hint for the root lookup, which -d skips:
	// ResolveJoinScenario compares digests and fetches the bytes no root here holds.
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
	local.wiring.membership.SessionPlayers = o.Players
	// The session's rate is its host's, a script's or bot's pace included; a
	// harness's manual clock is driven by Tick and takes none.
	if !local.Mode.Driven() || local.wiring.scriptedSession {
		local.TimeScaleSpec = o.Anchor.Anchor.Speed
	}
	return local, local.Validate()
}

// ValidateWith checks this configuration with s as its session half: a join's,
// before anything is dialled.
func (c runConfig) ValidateWith(s Membership) error {
	c.Membership = s
	return c.Validate()
}

// Prepare is the resources a dial of s's session builds its world on, settled on
// its preview: this configuration as the session makes it, checked, with the
// session's scenario resolved.
func (c runConfig) Prepare(s Membership, pending *network.PendingJoin, preview network.SessionOffer) (resource.Options, error) {
	c.Membership = s
	var err error
	if c, err = configForJoin(c, preview); err == nil {
		err = membership.ResolveJoinScenario(&c.Config, pending, preview)
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
func (c runConfig) restarted(next *restartRequest) (runConfig, *runConfig) {
	c.Notice, c.wiring.dialled, c.Membership.Bots, c.Play = next.Notice, nil, next.Bots, next.Play
	c.Journal, c.wiring.journalCarried = next.Journal, next.Journal
	c.wiring.membership.JoinPress, c.Resources.Provided = next.Press, next.provided
	if next.Scenario != "" {
		c.Resources.Scenario, c.Resources.Embedded = next.Scenario, false
	}
	c.Membership.HostAddress, c.wiring.resumeHost = "", next.Host
	switch {
	case next.Rejoin:
		if next.Join != "" {
			c.Membership.JoinAddress = next.Join
		}
	case next.Join == "" && next.Dialled != nil:
		solo := c
		solo.Membership.JoinAddress, solo.Membership.SessionName, solo.wiring.membership.SessionPlayers = "", "", 0
		c.wiring.dialled = next.Dialled
		return c, &solo
	case next.Join != "":
		solo := c
		solo.Membership.JoinAddress, solo.Membership.SessionName, solo.wiring.membership.SessionPlayers = "", "", 0
		joined, err := membership.Joining(c.Membership, next.Join)
		if err != nil { // parsed when latched, so a solo run is what is left
			next.Dialled.Abandon(err)
			solo.Notice = "Join failed: " + err.Error()
			return solo, nil
		}
		c.Membership, c.wiring.dialled = joined, next.Dialled
		return c, &solo
	default:
		// Not following anyone: a run that led its session, and one that
		// inherited it and has nobody left, both start over on their own.
		c.Membership.JoinAddress = ""
	}
	return c, nil
}

// Normalize is Config's, noting a size it supplied (geometryDefaulted), and gives a
// run with no claim keeper one of its own.
func (c *runConfig) Normalize() {
	if c.wiring.membership.Claims == nil {
		c.wiring.membership.Claims = membership.NewClaimKeeper(c.Membership.ClaimStore)
	}
	width, height := c.Width, c.Height
	if c.Config.Normalize(); c.Width != width || c.Height != height {
		c.wiring.geometryDefaulted = true
	}
}

// Validate is Config's with the wiring's rules: what a script's or bot's run may do
// (scriptedSession), and how a browser's bot joins (relay).
func (c runConfig) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return err
	}
	return c.validateWiring()
}

// validateWiring holds the run to the rules its wiring relaxes or adds.
func (c runConfig) validateWiring() error {
	scripted := c.wiring.scriptedSession
	// A browser joins over a session's WebSocket route and nothing else: it has no
	// socket to dial a host:port with, so a link naming a session there is looked up
	// on its site. Its bots dial nothing, joining it in-process.
	if !config.SocketNetwork && c.Membership.JoinAddress != "" && c.wiring.membership.Relay == nil &&
		c.Membership.SessionName == "" {
		if join, _ := link.ParseEndpoint(c.Membership.JoinAddress); join.Scheme != link.SchemeWebSocket {
			return errors.New("a browser cannot dial host:port; join with a session's link")
		}
	}
	if (c.Membership.HostAddress != "" || c.Membership.JoinAddress != "") &&
		c.Mode != ModePlay && !c.Mode.Serves() && !scripted {
		return fmt.Errorf("%s: network sessions require app.RunScript", c.Mode)
	}
	if c.TimeScaleSpec == "" {
		return nil
	}
	if !scripted {
		if _, ok := engine.ParseScale(c.TimeScaleSpec); !ok {
			return fmt.Errorf("-speed %q is not a ladder rate (1/8 1/4 1/2 1 2 4 8)", c.TimeScaleSpec)
		}
	}
	if scripted && c.TimeScaleSpec == ScriptPaceMax &&
		(c.Membership.HostAddress != "" || c.Membership.JoinAddress != "" || len(c.Membership.Bots) > 0) {
		return errors.New("-speed max cannot pace a session: a script that outruns its peer " +
			"is not simulating the session it is in")
	}
	// A manual clock records a simulation rate and never applies it. An authored
	// script reads the same field as its wall pace, which is a property of the run
	// rather than of the clock; every other driven mode has nothing to do with it.
	switch {
	case c.Mode.OwnsGeometry():
		return nil
	case scripted:
		_, err := scriptPacing(c.Config)
		return err
	case c.Mode.Serves():
		return fmt.Errorf("%s: a dedicated host runs at the session's rate", c.Mode)
	default:
		return fmt.Errorf("%s: the manual clock is driven by Tick, not by a rate", c.Mode)
	}
}

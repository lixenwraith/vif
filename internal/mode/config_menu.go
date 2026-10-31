package mode

import (
	"cmp"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/vlog"
)

// nodeKind is what a menu row is to a host that draws the menu with its own
// widgets; the tui menu draws every kind as a row
type nodeKind uint8

const (
	kindGroup  nodeKind = iota // a page or a section, holding nodes
	kindToggle                 // two states, a switch
	kindChoice                 // one of several named values
	kindStep                   // moved by a direction, - and +
	kindPick                   // one of a pick list, which a press marks at once
	kindAction                 // runs when activated and leaves the menu
	kindForm                   // text fields: one typed in on its row, more in a window
	kindInfo                   // read only
)

// configNode is a menu row, or a group of them: a page or a section
type configNode struct {
	kind                    nodeKind
	key, label, description string
	name                    func(*engine.GameContext) string // A label that follows the state, where the row's action does
	command                 string
	choices                 []string
	steps                   []uint64
	read                    func(*engine.GameContext) string
	change                  func(*engine.GameContext, int)
	activate                func(*Router) // A run-time row's own action, which closes the menu itself
	disabled                func(*engine.GameContext) string
	confirm                 bool                                   // A native host asks before it acts
	marked                  bool                                   // The pick its list marks
	form                    string                                 // The form a kindForm row opens, in configForms
	nodes                   []configNode                           // A group's rows
	extra                   func(*engine.GameContext) []configNode // A group's rows built at run time, after nodes
}

// configPages is the menu's pages, each a group of its sections and rows
var configPages = []configNode{
	{kind: kindGroup, key: "game", label: "Game", description: "The game running now: pause it, restart it from its scenario's first tick, or change its speed.", nodes: []configNode{
		{kind: kindAction, key: "pause", label: "Pause", name: pauseName, command: "pause", read: enterValue, disabled: pauseRefused,
			description: "Stop the game until Resume, as the command :pause or the key p in Normal mode does; the menu closes and leaves it stopped. In a session only the host can, and every participant stops at the same tick."},
		{kind: kindAction, key: "restart", label: "Restart game", command: "new", read: enterValue, confirm: true, disabled: hostOnly("Only the host can restart the game in a session"),
			description: "Start this game over from its scenario's first tick. In a session only the host can, and every participant restarts with it."},
		{kind: kindStep, key: "speed", label: "Game speed", description: "1x is real time; range 1/8x to 8x. In a session only the host can, and every participant changes with it.",
			command: "speed", read: func(ctx *engine.GameContext) string { return ctx.TimeCtl.Scale().String() + "x" },
			change: func(ctx *engine.GameContext, d int) {
				handleSpeedCommand(ctx, []string{engine.ScaleStep(ctx.TimeCtl.Scale(), d).String()})
			}, disabled: speedRefused},
	}, extra: replayConfigOptions},
	{kind: kindGroup, key: "scenarios", label: "Scenarios", description: "Choose a scenario and start a game of it, or name one that is not listed.", extra: scenarioConfigOptions},
	{kind: kindGroup, key: "multiplayer", label: "Multiplayer", description: "Host this game, join or leave a session, drop players, and add or remove your bots.", extra: multiplayerConfigOptions},
	{kind: kindGroup, key: "settings", label: "Settings", description: "Sound, controls, display and start: your preferences, each saved as it changes for the next run.", nodes: []configNode{
		section("Sound", []configNode{
			{kind: kindToggle, key: "sfx", label: "Sound effects", description: "Game sound effects; independent of music.",
				read: audioChannelValue(parameter.AudioChanSfx), change: toggleAudioChannel(parameter.AudioChanSfx), disabled: audioUnavailable},
			{kind: kindStep, key: "sfx_volume", label: "Effects volume", description: "Sound effect volume, in steps of 5%; music is not affected. Muting keeps this level.",
				read: func(ctx *engine.GameContext) string {
					return fmt.Sprintf("%.0f%%", ctx.World.Resources.SfxVolume()*100)
				},
				change: func(ctx *engine.GameContext, d int) {
					r := ctx.World.Resources
					r.SetSfxVolume((math.Round(r.SfxVolume()*100) + float64(d*5)) / 100)
				}, disabled: audioUnavailable},
			{kind: kindToggle, key: "music", label: "Music", description: "Procedural music; independent of sound effects.",
				read: audioChannelValue(parameter.AudioChanMusic), change: toggleAudioChannel(parameter.AudioChanMusic), disabled: audioUnavailable},
		}),
		section("Controls", []configNode{
			{kind: kindChoice, key: "auto", label: "Auto-fire", description: "off: manual fire; main: main weapon only; on: both weapons.",
				command: "auto", choices: []string{"off", "main", "on"},
				read: func(ctx *engine.GameContext) string { return engine.AutoFireWords[ctx.AutoFire.Load()] }},
			{kind: kindToggle, key: "mouse", label: "Mouse input", description: "Enable or ignore mouse movement and buttons.",
				command: "mouse", choices: []string{"disable", "enable"},
				read: func(ctx *engine.GameContext) string {
					if ctx.MouseDisabled.Load() {
						return "disable"
					}
					return "enable"
				}},
			{kind: kindToggle, key: "free", label: "Follow mouse", description: "Move the cursor with the pointer, without holding a button.",
				command: "free", choices: []string{"off", "on"},
				read: func(ctx *engine.GameContext) string { return toggleWord(ctx.MouseFreeMode.Load()) }},
		}),
		section("Display", []configNode{
			{kind: kindStep, key: "fps", label: "Max FPS", description: "The most frames drawn a second, never under one a tick. Auto is three a tick (60 at the default tick), or 24 inside tmux or screen, which drop output that comes faster.",
				command: "fps", read: func(ctx *engine.GameContext) string { return fpsWord(ctx.MaxFPS.Load()) },
				change: func(ctx *engine.GameContext, d int) {
					handleFPSCommand(ctx, []string{strconv.Itoa(fpsStep(int(ctx.MaxFPS.Load()), d))})
				}},
			{kind: kindToggle, key: "hud", label: "Telemetry HUD", description: "Show pinned telemetry cards over the game. The command :t opens the telemetry view, where Enter or a press pins a card.",
				command: "hud", choices: []string{"off", "on"},
				read: func(ctx *engine.GameContext) string { return toggleWord(ctx.OverlayHUD.Load()) }},
		}),
		section("Pause", []configNode{
			{kind: kindToggle, key: "pause_menu", label: "Pause in menu", description: "Pause the game while this menu, help or telemetry is open. Applies where the game can pause: solo, or hosting only your own bots; a session with other players plays on.",
				command: "pause menu", choices: []string{"off", "on"},
				read: func(ctx *engine.GameContext) string { return toggleWord(ctx.PauseMenu.Load()) }},
			{kind: kindToggle, key: "pause_command", label: "Pause in command line", description: "Pause the game while the command line is open. Applies where the game can pause: solo, or hosting only your own bots. With it off, the command :pause and the key p still pause.",
				command: "pause command", choices: []string{"off", "on"},
				read: func(ctx *engine.GameContext) string { return toggleWord(ctx.PauseCommand.Load()) }},
		}),
		section("Start", []configNode{
			{kind: kindToggle, key: "wait", label: "Wait to start", description: "Hold each game you start or restart at its first tick until a key, click or tap starts it; in a session, the host's. A scenario's own restart never waits.",
				command: "wait", choices: []string{"off", "on"},
				read: func(ctx *engine.GameContext) string { return toggleWord(ctx.WaitStart.Load()) }},
		}),
	}},
	{kind: kindGroup, key: "developer", label: "Developer", description: "Logging, flight recorder, profiling, telemetry, navigation overlays and the replay journal.", nodes: []configNode{
		section("Logging", []configNode{
			{kind: kindToggle, key: "logging", label: "File logging", description: "Start or stop writing diagnostic logs to the configured log directory.",
				command: "log", choices: []string{"off", "on"}, read: func(*engine.GameContext) string { return toggleWord(vlog.Enabled()) }, disabled: loggingUnavailable},
			{kind: kindChoice, key: "level", label: "Log level", description: "Minimum severity to record. Trace and debug can produce large log files.",
				command: "log level", choices: []string{"trace", "debug", "info", "warn", "error"},
				read: func(*engine.GameContext) string { return strings.ToLower(vlog.LevelName()) }, disabled: loggingUnavailable},
			{kind: kindChoice, key: "scope", label: "Log scope preset", description: "Choose all, app+fsm+stat, or none. The command :log scope <categories> allows individual categories.",
				command: "log scope", choices: []string{"all", "app+fsm+stat", "none"},
				read: func(*engine.GameContext) string { return vlog.ScopeString(vlog.Scopes()) }, disabled: loggingUnavailable},
			{kind: kindStep, key: "stat", label: "Snapshot ticks", description: "Ticks between log snapshots; 0 disables them. The command :log stat <ticks> accepts any interval.",
				command: "log stat", steps: []uint64{0, 60, 200, 600, 3600},
				read: func(ctx *engine.GameContext) string {
					return strconv.FormatUint(ctx.World.Resources.Status.SnapshotInterval(), 10)
				}, disabled: loggingUnavailable},
		}),
		section("Flight recorder", []configNode{
			{kind: kindStep, key: "rec", label: "Recorder ticks", description: "Flight recorder history depth; 0 disables it. Changing depth discards existing history.",
				command: "log rec", steps: []uint64{0, 120, 600, 1800, 3600},
				read: func(ctx *engine.GameContext) string { return strconv.Itoa(ctx.World.Resources.Status.RecorderDepth()) }, disabled: loggingUnavailable},
			{kind: kindToggle, key: "fsm", label: "Record FSM transitions", description: "Flush flight recorder history when an FSM transition occurs.",
				command: "log rec fsm", choices: []string{"off", "on"},
				read: func(ctx *engine.GameContext) string {
					rc := ctx.World.Resources.Status.Recorder()
					return toggleWord(rc != nil && rc.FSMTrigger())
				},
				disabled: recorderUnavailable},
			withDisabled(commandAction("flush", "Flush flight recorder", "Write the recorder window on the next tick; needs rec in the log scope.", "log rec flush"), recorderUnavailable),
		}),
		section("Profiling", []configNode{
			{kind: kindToggle, key: "prof", label: "Runtime profiler", description: "Collect module timings, logged each second while file logging is on. Enabling also pins profiler cards to the telemetry HUD.",
				command: "debug prof", choices: []string{"off", "on"},
				read: func(ctx *engine.GameContext) string { return toggleWord(ctx.World.Resources.Prof.Profiling()) }},
			withDisabled(commandAction("cpu", "CPU profile", captureDescription("a CPU profile"), "debug cpu"), lacking(paths.ExternalFiles(), "CPU profile")),
			withDisabled(commandAction("heap", "Heap profile", "Write a heap profile; the status bar names the file.", "debug heap"), lacking(paths.ExternalFiles(), "Heap profile")),
			withDisabled(commandAction("mutex", "Mutex profile", captureDescription("a mutex profile"), "debug mutex"), lacking(paths.ExternalFiles(), "Mutex profile")),
			withDisabled(commandAction("trace", "Execution trace", captureDescription("an execution trace"), "debug trace"), lacking(paths.ExternalFiles(), "Execution trace")),
		}),
		section("Telemetry", []configNode{
			withDisabled(commandAction("snap", "Telemetry snapshot", "Write every metric with the view and session state to a snapshot file in the log directory.", "t save"), lacking(vlog.Linked, "Telemetry snapshot")),
			commandAction("unpin", "Clear HUD pins", "Unpin every telemetry card from the HUD.", "t unpin"),
		}),
		section("Navigation", []configNode{
			{kind: kindToggle, key: "flow", label: "Flow field", description: "Show the navigation flow field for the selected target group; the command :flow <group> selects one.",
				command: "flow", read: func(ctx *engine.GameContext) string { return toggleWord(ctx.NavigationDebug.ShowFlow) }},
			{kind: kindToggle, key: "graph", label: "Route graph", description: "Show composite navigation routes; the command :graph <group> selects a target group.",
				command: "graph", read: func(ctx *engine.GameContext) string { return toggleWord(ctx.NavigationDebug.ShowComposite) }},
		}),
		section("Recording", []configNode{
			{kind: kindToggle, key: "journal", label: "Replay journal", description: "Record a replay journal from here, or close the one recording. The game plays on as it was. The command :journal start <slot> records a bot of yours.",
				command: "journal", choices: []string{"off", "on"}, read: journalValue, disabled: journalUnavailable},
		}),
	}},
	{kind: kindGroup, key: "launch", label: "Launch options", description: cmp.Or(startupNotes[runtime.GOOS], "What is set when the game launches, and where to set it: these rows only explain."), nodes: []configNode{
		{kind: kindInfo, key: "files", label: "Paths and scenario", description: "Set by vif.toml's [paths] table or the launch flags -config-dir, -s, -f and -k. The Scenarios page starts another scenario now.", read: startupValue},
		{kind: kindInfo, key: "backend", label: "Audio device / buffer", description: "Set by the launch flag -audio-backend and vif.toml's [audio] buffer_ms. The audio device opens at launch.", read: startupValue},
		{kind: kindInfo, key: "color", label: "Colour depth", description: "Set by the launch flag -color (auto, 256 or true). Rendering resources are selected at launch.", read: startupValue},
		{kind: kindInfo, key: "keymap", label: "Key bindings", description: "Edit input/keymap.toml in your configuration root, or name a file with the launch flag -k. A key names one action, and an action can have several keys: config_menu has Ctrl+G and F10, help has F1.", read: startupValue},
		{kind: kindInfo, key: "session", label: "Identity", description: "Session identity and seed are launch flags. The Multiplayer page holds the live session actions, Developer the replay journal.", read: startupValue},
	}},
}

func startupValue(*engine.GameContext) string { return "info" }

// startupNotes is how a platform without a command line sets what the Startup
// rows name, by GOOS; a terminal or desktop run takes the flags and vif.toml as named
var startupNotes = map[string]string{
	"js":      "Settings fixed when the page loaded. A browser reads no files: launch arguments (?arg=) carry the flags these rows name, and the embedded scenario, keymap and content stand.",
	"android": "Settings fixed when the app started. Android has no command line: vif.toml in the app's files directory sets what these rows name.",
}

// commandAction is a row that runs command when activated, leaving the menu
func commandAction(key, label, description, command string) configNode {
	return configNode{kind: kindAction, key: key, label: label, description: description, command: command, read: enterValue}
}

// enterValue is an action row's value in the tui menu, where Enter runs it; a native
// host draws a button instead
func enterValue(*engine.GameContext) string { return "Enter" }

// section titles the rows it holds
func section(label string, nodes []configNode) configNode {
	return configNode{kind: kindGroup, key: "#" + label, label: label, nodes: nodes}
}

// pauseName is what the Pause row does now: a game this instance holds resumes,
// and in a session with other people the host's pause stops everyone
func pauseName(ctx *engine.GameContext) string {
	name := "Pause"
	if ctx.TimeCtl.Holds(engine.HoldPause) {
		name = "Resume"
	}
	if !ctx.World.Session().Pausable() {
		name += " everyone"
	}
	return name
}

func pauseRefused(ctx *engine.GameContext) string {
	return pauseRefusedFor(ctx, !ctx.TimeCtl.Holds(engine.HoldPause))
}

// fpsStep is the max_fps a menu step d from fps, around parameter.FrameRateSteps;
// one between steps moves to the step on that side of it
func fpsStep(fps, d int) int {
	steps := parameter.FrameRateSteps
	i, found := slices.BinarySearch(steps, fps)
	switch {
	case found:
		i += d
	case d < 0:
		i-- // i is the first step above fps
	}
	return steps[(i%len(steps)+len(steps))%len(steps)]
}

func withDisabled(n configNode, disabled func(*engine.GameContext) string) configNode {
	n.disabled = disabled
	return n
}

// hostOnly refuses what only a live session's host may do, as its command does
func hostOnly(why string) func(*engine.GameContext) string {
	return func(ctx *engine.GameContext) string {
		if ctx.World.LiveSession() && !ctx.World.IsSessionCoordinator() {
			return why
		}
		return ""
	}
}

func replayConfigOptions(ctx *engine.GameContext) []configNode {
	if ctx.ReplaySeek == nil {
		return nil
	}
	return []configNode{commandAction("replay", "Restart replay", "Play the recording again from its first tick. The commands :r tick and :r time go to any other tick or time.", "replay restart")}
}

// scenarioConfigOptions lists the scenarios the menu opened with, one marked, then
// the button that starts the marked one as :n <name> does and a box for any other.
// The mark starts on the current scenario; a phone has no name to type.
func scenarioConfigOptions(ctx *engine.GameContext) []configNode {
	if ctx.SessionCtl == nil {
		return nil
	}
	names, current := menuScenarios(ctx)
	picked := current
	if m := configMenu(ctx); m != nil && slices.Contains(names, m.Choice) {
		picked = m.Choice
	}
	picks := make([]configNode, 0, len(names))
	for _, name := range names {
		value := ""
		if name == current {
			value = "current"
		}
		picks = append(picks, configNode{kind: kindPick, key: "pick/" + name, label: name, marked: name == picked,
			description: "Mark " + name + " to start; Start below starts the scenario marked.",
			read:        func(*engine.GameContext) string { return value },
			activate:    func(r *Router) { r.pickConfigRow(name) }})
	}
	description := "Start a game of " + picked + " from its first tick. In a session only the host can, and its participants rejoin it."
	if picked == current {
		description = "Start " + picked + ", the current scenario, over from its first tick. In a session only the host can, and every participant restarts with it."
	}
	return []configNode{
		section("Installed", picks),
		{kind: kindAction, key: "start", label: "Start " + picked, command: "new " + picked, read: enterValue, confirm: true,
			description: description, disabled: hostOnly("Only the host can start a scenario in a session")},
		section("Other", []configNode{formRow("scenario")}),
	}
}

// menuScenarios is the scenario list the menu opened with and the scenario running,
// scanned once per open: a host drawing its own menu rebuilds the whole tree every frame
func menuScenarios(ctx *engine.GameContext) ([]string, string) {
	if m := configMenu(ctx); m != nil && m.Scenarios != nil {
		return m.Scenarios, m.Running
	}
	return ctx.SessionCtl.Scenarios()
}

func captureDescription(what string) string {
	return fmt.Sprintf("Capture %s for %v to the diagnostics directory; the status bar names the file.", what, parameter.ProfCaptureDefault)
}

func recorderUnavailable(ctx *engine.GameContext) string {
	if why := loggingUnavailable(ctx); why != "" {
		return why
	}
	if ctx.World.Resources.Status.Recorder() == nil {
		return "Enable the flight recorder first"
	}
	return ""
}

func journalValue(ctx *engine.GameContext) string {
	return toggleWord(ctx.SessionCtl != nil && ctx.SessionCtl.Journaling())
}

// A journal recording can always be closed; starting one has the run's conditions.
func journalUnavailable(ctx *engine.GameContext) string {
	switch {
	case ctx.SessionCtl == nil:
		return sessionsUnavailable
	case ctx.SessionCtl.Journaling():
		return ""
	case !vlog.Linked:
		return unavailable("Journal")
	}
	if err := ctx.SessionCtl.JournalError(); err != nil {
		return sentence(err)
	}
	return ""
}

// Audio is left out of a build, or silent in a run whose device did not open
func audioUnavailable(ctx *engine.GameContext) string {
	reg := ctx.World.Resources.Status
	switch {
	case reg.Ints.Get("audio.mask").Load() < 0:
		return unavailable("Audio")
	case reg.Bools.Get("audio.silent").Load():
		return "Audio unavailable in this run"
	}
	return ""
}

func loggingUnavailable(*engine.GameContext) string {
	if !vlog.Linked {
		return unavailable("Logging")
	}
	return ""
}

// lacking disables a row whose command this platform cannot serve, with its reason
func lacking(have bool, what string) func(*engine.GameContext) string {
	return func(*engine.GameContext) string {
		if have {
			return ""
		}
		return unavailable(what)
	}
}

func audioChannelValue(mask uint8) func(*engine.GameContext) string {
	return func(ctx *engine.GameContext) string {
		current := ctx.World.Resources.Status.Ints.Get("audio.mask").Load()
		return toggleWord(current >= 0 && current&int64(mask) != 0)
	}
}

func toggleAudioChannel(mask uint8) func(*engine.GameContext, int) {
	return func(ctx *engine.GameContext, _ int) {
		ctx.PushLocalOrigin(event.EventAudioMuteToggle, &event.AudioMuteTogglePayload{Mode: event.MuteToggle, Mask: mask}, event.OriginDevice)
	}
}

// unavailable is why the row cannot act now, the sentence its command gives
func (n configNode) unavailable(ctx *engine.GameContext) string {
	if n.command != "" {
		if why := commandUnavailable(ctx, strings.Fields(n.command)[0]); why != "" {
			return why
		}
	}
	if n.disabled != nil {
		return n.disabled(ctx)
	}
	if n.kind == kindInfo {
		return "Read only"
	}
	return ""
}

// setting reports a row a direction changes: a toggle, a choice or a step
func (n configNode) setting() bool {
	return n.kind == kindToggle || n.kind == kindChoice || n.kind == kindStep
}

// inline reports a form typed in on its row rather than in a window: one field
func (n configNode) inline() bool {
	return n.kind == kindForm && len(configForms[n.form].fields) == 1
}

// children is a group's rows: its own, then those built at run time
func (n configNode) children(ctx *engine.GameContext) []configNode {
	if n.extra == nil {
		return n.nodes
	}
	return append(slices.Clone(n.nodes), n.extra(ctx)...)
}

// nodeID is a node's id, its keys from the root joined by /; a host echoes it back
func nodeID(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "/" + key
}

// configNodeFor resolves an id to its node, building only the groups on its path
func configNodeFor(ctx *engine.GameContext, id string) (configNode, bool) {
	nodes, prefix := configPages, ""
	for {
		i := slices.IndexFunc(nodes, func(n configNode) bool {
			nid := nodeID(prefix, n.key)
			return nid == id || n.kind == kindGroup && strings.HasPrefix(id, nid+"/")
		})
		if i < 0 {
			return configNode{}, false
		}
		n := nodes[i]
		if nodeID(prefix, n.key) == id {
			return n, true
		}
		prefix, nodes = nodeID(prefix, n.key), n.children(ctx)
	}
}

func handleConfigCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 0 {
		setCommandError(ctx, "Usage: :menu, :g or :config")
		return CommandResult{Continue: true}
	}
	ctx.RequestMode(core.ModeOverlay)
	ctx.PauseForMenu()
	ctx.SetOverlayContent(nil)
	showConfigMenu(ctx, "")
	return CommandResult{Continue: true}
}

func configMenu(ctx *engine.GameContext) *core.OverlayMenu {
	if c := ctx.GetOverlayContent(); c != nil && c.Layout == core.OverlayLayoutMenu {
		return c.Menu
	}
	return nil
}

// configMenuTitle names the page and says what the game does while the menu is
// open: a solo game stops, a session with other people plays on
func configMenuTitle(ctx *engine.GameContext, page string) string {
	title := "Menu"
	if page != "" {
		title += " / " + page
	}
	switch {
	case ctx.TimeCtl.Holds(engine.HoldStart):
		return title + " · game waiting to start"
	case ctx.TimeCtl.IsPaused():
		return title + " · game paused"
	}
	return title + " · game running"
}

// newOverlayMenu is a menu on page carrying what the one up holds for the run: the
// pick marked and the scenarios scanned when it opened
func newOverlayMenu(ctx *engine.GameContext, page string) *core.OverlayMenu {
	menu := &core.OverlayMenu{Page: page}
	if old := configMenu(ctx); old != nil {
		menu.Choice, menu.Scenarios, menu.Running = old.Choice, old.Scenarios, old.Running
	}
	if menu.Scenarios == nil && ctx.SessionCtl != nil {
		menu.Scenarios, menu.Running = ctx.SessionCtl.Scenarios()
	}
	return menu
}

// configMenuView is the menu as it stands: the pages, one page's rows, or with a
// query the rows of every page that match all its words, each naming its page
func configMenuView(ctx *engine.GameContext, page string) *core.OverlayContent {
	menu := newOverlayMenu(ctx, page)
	if terms := strings.Fields(strings.ToLower(ctx.OverlayFilter())); len(terms) > 0 {
		menu.Rows = menuMatches(ctx, terms)
		title := fmt.Sprintf("Menu / search: %d found", len(menu.Rows))
		if len(menu.Rows) == 0 {
			menu.Rows = append(menu.Rows, core.OverlayMenuRow{Key: "#none", Label: "Nothing matches", Header: true})
		}
		return &core.OverlayContent{Title: title, Layout: core.OverlayLayoutMenu, Menu: menu}
	}
	if page == "" {
		for _, p := range configPages {
			menu.Rows = append(menu.Rows, core.OverlayMenuRow{Key: p.key, Label: p.label, Value: ">", Description: p.description})
		}
		return &core.OverlayContent{Title: configMenuTitle(ctx, ""), Layout: core.OverlayLayoutMenu, Menu: menu}
	}
	title := configMenuTitle(ctx, "")
	menu.Rows = append(menu.Rows, core.OverlayMenuRow{Key: "..", Label: "Back", Value: "<", Description: "Return to the menu."})
	if n, ok := configNodeFor(ctx, page); ok && n.kind == kindGroup {
		title = configMenuTitle(ctx, n.label)
		menu.Rows = pageRows(ctx, page, n.children(ctx), menu.Rows)
	}
	return &core.OverlayContent{Title: title, Layout: core.OverlayLayoutMenu, Menu: menu}
}

// pageRows lists a group's rows for the tui menu: a group inside it is a section
// title over its own rows, at any depth
func pageRows(ctx *engine.GameContext, prefix string, nodes []configNode, rows []core.OverlayMenuRow) []core.OverlayMenuRow {
	for _, n := range nodes {
		id := nodeID(prefix, n.key)
		rows = append(rows, n.row(ctx, id))
		if n.kind == kindGroup {
			rows = pageRows(ctx, id, n.children(ctx), rows)
		}
	}
	return rows
}

// menuMatches is the rows of every page matching all of terms in the page, its
// sections, the label and the description, each labelled Page / Row
func menuMatches(ctx *engine.GameContext, terms []string) []core.OverlayMenuRow {
	var rows []core.OverlayMenuRow
	for _, p := range configPages {
		var walk func(prefix, heading string, nodes []configNode)
		walk = func(prefix, heading string, nodes []configNode) {
			for _, n := range nodes {
				id := nodeID(prefix, n.key)
				if n.kind == kindGroup {
					walk(id, heading+" "+n.label, n.children(ctx))
					continue
				}
				text := strings.ToLower(strings.Join([]string{p.label, heading, n.label, n.description}, " "))
				if !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(text, t) }) {
					row := n.row(ctx, id)
					row.Label = p.label + " / " + row.Label
					rows = append(rows, row)
				}
			}
		}
		walk(p.key, "", p.children(ctx))
	}
	return rows
}

// row is the node as the tui menu draws it, keyed by its id
func (n configNode) row(ctx *engine.GameContext, id string) core.OverlayMenuRow {
	if n.kind == kindGroup {
		return core.OverlayMenuRow{Key: id, Label: n.label, Header: true}
	}
	label := n.label
	switch {
	case n.name != nil:
		label = n.name(ctx)
	case n.kind == kindPick && n.marked:
		label = "(*) " + label
	case n.kind == kindPick:
		label = "( ) " + label
	}
	return core.OverlayMenuRow{Key: id, Label: label, Value: n.read(ctx), Description: n.description, Disabled: n.unavailable(ctx), Text: n.inline()}
}

func showConfigMenu(ctx *engine.GameContext, page string) {
	publishConfigMenu(ctx, configMenuView(ctx, page))
}

// publishConfigMenu replaces the menu when it changed, keeping its scroll on one page
func publishConfigMenu(ctx *engine.GameContext, content *core.OverlayContent) {
	old, menu := configMenu(ctx), content.Menu
	if old != nil && old.Form == nil && old.Page == menu.Page && old.Choice == menu.Choice &&
		ctx.GetOverlayTitle() == content.Title && slices.Equal(old.Rows, menu.Rows) {
		return
	}
	scroll := ctx.GetOverlayScroll()
	ctx.SetOverlayContent(content)
	if old != nil && old.Page == menu.Page {
		ctx.SetOverlayScroll(scroll)
	}
}

// RefreshConfigMenu snapshots settled settings under the world lock before rendering.
func (r *Router) RefreshConfigMenu() {
	switch m := configMenu(r.ctx); {
	case m == nil:
		r.configForm = nil
	case r.configForm == nil:
		showConfigMenu(r.ctx, m.Page)
	case r.configForm.inline:
		r.publishConfigForm()
	}
}

func (r *Router) handleConfigMenu() bool {
	if configMenu(r.ctx) != nil {
		return r.closeOverlay()
	}
	r.enterOverlay()
	handleConfigCommand(r.ctx, nil)
	return true
}

// configMenuBack steps out of what the menu shows: a form, then a search, then a
// page, then the menu itself
func (r *Router) configMenuBack() bool {
	m := configMenu(r.ctx)
	switch {
	case r.configForm != nil:
		key := r.configForm.row
		r.configForm = nil
		showConfigMenu(r.ctx, m.Page)
		r.ctx.SetOverlaySelection(key)
		r.machine.SetMode(input.ModeOverlay)
		return true
	case r.ctx.OverlayFilter() != "" || r.ctx.IsOverlayFilterEditing():
		r.ctx.SetOverlayFilter("")
		r.endOverlayFilter()
		showConfigMenu(r.ctx, m.Page)
		return true
	case m.Page == "":
		return r.closeOverlay()
	}
	page := m.Page
	showConfigMenu(r.ctx, "")
	r.ctx.SetOverlaySelection(page)
	return true
}

// pickConfigRow marks one row of a page's pick list
func (r *Router) pickConfigRow(choice string) {
	m := *configMenu(r.ctx)
	m.Choice = choice
	c := *r.ctx.GetOverlayContent()
	c.Menu = &m
	scroll := r.ctx.GetOverlayScroll()
	r.ctx.SetOverlayContent(&c)
	r.ctx.SetOverlayScroll(scroll)
}

// selectableRow is the row nearest i in direction d (+1 or -1) that is not a
// section title, or i itself when none is
func selectableRow(rows []core.OverlayMenuRow, i, d int) int {
	for j := i; j >= 0 && j < len(rows); j += d {
		if !rows[j].Header {
			return j
		}
	}
	for j := i - d; j >= 0 && j < len(rows); j -= d {
		if !rows[j].Header {
			return j
		}
	}
	return i
}

func (r *Router) moveConfigMenu(motion input.MotionOp, pageDelta int) bool {
	m := configMenu(r.ctx)
	i := slices.IndexFunc(m.Rows, func(row core.OverlayMenuRow) bool { return row.Key == r.ctx.GetOverlaySelection() })
	i = max(i, 0)
	d := 1
	switch motion {
	case input.MotionLeft:
		return r.changeConfigMenu(-1, false)
	case input.MotionRight:
		return r.changeConfigMenu(1, false)
	case input.MotionUp:
		i, d = i-1, -1
	case input.MotionDown:
		i++
	case input.MotionScreenTop:
		i = 0
	case input.MotionScreenBottom:
		i, d = len(m.Rows)-1, -1
	}
	if pageDelta < 0 {
		d = -1
	}
	i += pageDelta * r.ctx.OverlayGeometry().ListH()
	i = tui.ClampCursor(i, len(m.Rows))
	if i >= 0 {
		r.ctx.SetOverlaySelection(m.Rows[selectableRow(m.Rows, i, d)].Key)
	}
	return true
}

// changeConfigMenu acts on the row selected: Back, a page to enter, or the node
// its key names
func (r *Router) changeConfigMenu(direction int, activate bool) bool {
	m := configMenu(r.ctx)
	key := r.ctx.GetOverlaySelection()
	if key == ".." {
		return r.configMenuBack()
	}
	if m.Page == "" && r.ctx.OverlayFilter() == "" {
		if direction < 0 {
			return true
		}
		showConfigMenu(r.ctx, key)
		if menu := configMenu(r.ctx); len(menu.Rows) > 1 {
			r.ctx.SetOverlaySelection(menu.Rows[selectableRow(menu.Rows, 1, 1)].Key)
		}
		return true
	}
	n, ok := configNodeFor(r.ctx, key)
	if !ok {
		return true
	}
	return r.applyConfigNode(key, n, direction, activate)
}

// applyConfigNode acts on the node keyed id as a key or a host's op does: a direction
// steps or cycles a setting and marks a pick, activate runs an action or opens a
// form; a refusal is answered on the status line, as the command's would be
func (r *Router) applyConfigNode(id string, n configNode, direction int, activate bool) bool {
	switch n.kind {
	case kindGroup:
		return true
	case kindAction, kindForm:
		if !activate {
			return true
		}
	}
	if why := n.unavailable(r.ctx); why != "" {
		setCommandError(r.ctx, why)
		return true
	}
	if n.kind == kindForm {
		r.openConfigForm(id, n)
		return true
	}
	r.ctx.WithOrigin(event.OriginCommand, func() {
		if n.activate != nil {
			n.activate(r)
			return
		}
		if n.change != nil {
			n.change(r.ctx, direction)
			return
		}
		command := n.command
		if len(n.steps) > 0 {
			current, _ := strconv.ParseUint(n.read(r.ctx), 10, 64)
			next := current
			for _, v := range n.steps {
				if direction > 0 && v > current {
					next = v
					break
				}
				if direction < 0 && v < current {
					next = v
				}
			}
			// A key at the limit must not discard recorder history.
			if next == current {
				return
			}
			command += " " + strconv.FormatUint(next, 10)
		} else if len(n.choices) > 0 {
			i := slices.Index(n.choices, n.read(r.ctx))
			if i < 0 && direction < 0 {
				i = 0
			}
			i = (i + direction + len(n.choices)) % len(n.choices)
			command += " " + n.choices[i]
		}
		ExecuteCommand(r.ctx, command)
	})
	if n.kind == kindAction && n.activate == nil {
		return r.closeOverlay()
	}
	r.RefreshConfigMenu()
	return true
}

// pointConfigMenu is a left press on a body cell of the menu: on a form field it
// focuses it; on a row it selects it, and on the row already selected, or a row of
// a pick list, it acts as Enter does, or as Left on a setting's left half.
func (r *Router) pointConfigMenu(x, y int) bool {
	m := configMenu(r.ctx)
	g := r.ctx.OverlayGeometry()
	if m == nil || x < 0 || x >= g.ContentW || y < 0 || y >= g.ListH() {
		return true
	}
	if f := r.configForm; f != nil && !f.inline {
		scroll := tui.NewScrollState(len(f.state.Fields), min(len(f.state.Fields), g.ListH()))
		scroll.Select(f.state.Focus)
		if i := scroll.Offset + y; i < len(f.state.Fields) {
			f.state.Focus = i
			r.publishConfigForm()
		}
		return true
	}
	i := r.ctx.GetOverlayScroll() + y
	if i >= len(m.Rows) || m.Rows[i].Header || r.configForm != nil && m.Rows[i].Key == r.configForm.row {
		return true
	}
	if r.configForm != nil {
		r.configMenuBack() // A press elsewhere leaves the text box as Escape does
		m = configMenu(r.ctx)
	}
	selected := max(slices.IndexFunc(m.Rows, func(row core.OverlayMenuRow) bool { return row.Key == r.ctx.GetOverlaySelection() }), 0)
	r.ctx.SetOverlaySelection(m.Rows[i].Key)
	n, ok := configNodeFor(r.ctx, m.Rows[i].Key)
	if i != selected && n.kind != kindPick {
		return true
	}
	if ok && n.setting() && x < g.ContentW/2 {
		return r.changeConfigMenu(-1, false)
	}
	return r.changeConfigMenu(1, true)
}

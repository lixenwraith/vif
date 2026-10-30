package mode

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/prof"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/internal/vlog"
)

// CommandResult represents the outcome of command execution
type CommandResult struct {
	Continue bool // false = exit game
}

// commandNames lists every case label below, aliases included. Kept adjacent to
// the switch so the two move together; TestCommandsDocumented cross-checks it
// against internal/help.
var commandNames = []string{
	"flow", "graph", "l", "log", "q", "quit", "p", "pause", "n", "new", "new!", "n!",
	"f", "free", "a", "auto", "s", "system", "m", "mouse", "e", "emit", "event",
	"t", "telemetry", "hud", "d", "debug", "h", "help", "?", "about", "content", "energy", "heat",
	"boost", "god", "demon", "w", "weapon", "blossom", "decay", "dust",
	"sp", "speed", "st", "step",
	"r", "region", "replay",
	"host", "join", "leave", "relay", "session", "bot", "player", "g", "config", "journal", "wait",
}

// CommandNames returns the recognised command names and aliases
func CommandNames() []string { return commandNames }

// ExecuteCommand parses and executes a command string
// Returns CommandResult indicating whether game should continue and pause state
func ExecuteCommand(ctx *engine.GameContext, command string) CommandResult {
	command = strings.TrimSpace(command)
	if command == "" {
		return CommandResult{Continue: true}
	}
	// The answer a command that says nothing else leaves; one that answers replaces it
	ctx.SetStatusMessage(":"+command, parameter.StatusNotice)

	// Parse command into parts (space-separated)
	parts := strings.Fields(command)
	cmd := parts[0]
	args := parts[1:]
	if cmd == "r" && ctx.ReplaySeek != nil {
		cmd = "replay" // a replay's regions are the recording's, so :r is the replay's own
	}

	if reason := commandUnavailable(ctx, cmd); reason != "" {
		setCommandError(ctx, reason+": :"+cmd)
		return CommandResult{Continue: true}
	}

	// Execute based on command
	switch cmd {
	case "g", "config":
		return handleConfigCommand(ctx, args)
	case "flow":
		return handleFlowCommand(ctx, args)
	case "graph":
		return handleGraphCommand(ctx, args)
	case "l", "log":
		return handleLogCommand(ctx, args)
	case "q", "quit":
		return handleQuitCommand(ctx)
	case "p", "pause":
		return handlePauseCommand(ctx, args)
	case "new", "n":
		return handleNewCommand(ctx, args, false)
	case "new!", "n!":
		return handleNewCommand(ctx, args, true)
	case "f", "free":
		return handleFreeCommand(ctx, args)
	case "a", "auto":
		return handleAutoCommand(ctx, args)
	case "sp", "speed":
		return handleSpeedCommand(ctx, args)
	case "st", "step":
		return handleStepCommand(ctx, args)
	case "s", "system":
		return handleSystemCommand(ctx, args)
	case "m", "mouse":
		return handleMouseCommand(ctx, args)
	case "e", "emit", "event":
		return handleEmitCommand(ctx, args)
	case "t", "telemetry":
		return handleTelemetryCommand(ctx, args)
	case "hud":
		return applyToggle(ctx, &ctx.OverlayHUD, args, "hud", "Telemetry HUD")
	case "wait":
		return applyToggle(ctx, &ctx.WaitStart, args, "wait", "Wait to start")
	case "d", "debug":
		return handleDebugCommand(ctx, args)
	case "h", "help", "?":
		return handleHelpCommand(ctx)
	case "r", "region":
		return handleRegionCommand(ctx, args)
	case "replay":
		return handleReplayCommand(ctx, args)
	case "about":
		return handleAboutCommand(ctx)
	case "content":
		return handleContentCommand(ctx)
	case "energy":
		return handleEnergyCommand(ctx, args)
	case "heat":
		return handleHeatCommand(ctx, args)
	case "boost":
		return handleBoostCommand(ctx)
	case "god":
		return handleGodCommand(ctx, 1)
	case "demon":
		return handleGodCommand(ctx, -1)
	case "w", "weapon":
		return handleWeaponCommand(ctx, args)
	case "blossom":
		return handleBlossomCommand(ctx)
	case "decay":
		return handleDecayCommand(ctx)
	case "dust":
		return handleDustCommand(ctx)
	case "host":
		return handleHostCommand(ctx, args)
	case "join":
		return handleJoinCommand(ctx, args)
	case "leave":
		return handleLeaveCommand(ctx, args)
	case "relay":
		return handleRelayCommand(ctx, args)
	case "session":
		return handleSessionCommand(ctx)
	case "journal":
		return handleJournalCommand(ctx, args)
	case "bot":
		return handleBotCommand(ctx, args)
	case "player":
		return handlePlayerCommand(ctx, args)
	default:
		setCommandError(ctx, fmt.Sprintf("Unknown command: %s", cmd))
		return CommandResult{Continue: true}
	}
}

// The menu and command line share the same session and replay policy.
func commandUnavailable(ctx *engine.GameContext, cmd string) string {
	if cmd == "replay" && ctx.ReplaySeek == nil {
		return "Only in a replay"
	}
	// A replay's viewer inspects a world the recording authors
	if ctx.Viewer.Load() {
		switch cmd {
		case "g", "config", "h", "help", "?", "about", "t", "telemetry", "hud", "d", "debug", "content", "flow", "graph", "l", "log", "q", "quit", "replay":
		default:
			return "Unavailable in a replay"
		}
	}

	// A live operator may inspect the instance and author its own player state,
	// but may not mutate shared scheduling, systems or FSM configuration locally.
	if ctx.World.LiveSession() {
		switch cmd {
		case "st", "step", "s", "system", "e", "emit", "event", "r", "region":
			return "Unavailable in a live session"
		}
	}

	return ""
}

// handleLogCommand controls the session logger
// Usage: :log | :log on|off | :log <level> | :log scope [spec] | :log stat [ticks]
func handleLogCommand(ctx *engine.GameContext, args []string) CommandResult {
	if !vlog.Linked {
		setCommandError(ctx, unavailable("Logging"))
		return CommandResult{Continue: true}
	}
	if len(args) == 0 {
		reportLogState(ctx)
		return CommandResult{Continue: true}
	}

	switch strings.ToLower(args[0]) {
	case "on", "start":
		if vlog.Enabled() {
			ctx.SetStatusMessage("Logging already active", parameter.StatusNotice)
			break
		}
		// Opens a file under the world lock; a deliberate operator cost
		path, err := vlog.Start()
		if err != nil {
			setCommandError(ctx, "Logging failed: "+err.Error())
			return CommandResult{Continue: true}
		}
		ctx.Log.Info("app", "msg", "logging started", "path", path, "level", vlog.LevelName())
		ctx.SetStatusMessage("Logging to "+path, parameter.StatusNotice)

	case "off", "stop":
		if !vlog.Enabled() {
			ctx.SetStatusMessage("Logging already stopped", parameter.StatusNotice)
			break
		}
		ctx.Log.Info("app", "msg", "logging stopped by command")
		vlog.Stop() // drains asynchronously; never blocks the world lock
		ctx.SetStatusMessage("Logging stopped", parameter.StatusNotice)

	case "scope", "sc":
		if len(args) < 2 {
			reportLogState(ctx)
			break
		}
		s, err := vlog.ParseScopes(strings.Join(args[1:], "+"), vlog.Scopes())
		if err != nil {
			setCommandError(ctx, "Usage: :log scope [+|-]<scope>+<scope> | <letters> | all | none; scopes in :help logging")
			return CommandResult{Continue: true}
		}
		vlog.SetScopes(s)
		ctx.Log.Info("app", "msg", "log scope changed", "scope", vlog.ScopeString(s))
		reportLogState(ctx)

	case "level", "lvl":
		if len(args) < 2 || vlog.SetLevelName(args[1]) != nil {
			setCommandError(ctx, "Usage: :log level trace|debug|info|warn|error")
			return CommandResult{Continue: true}
		}
		ctx.Log.Info("app", "msg", "log level changed", "level", vlog.LevelName())
		reportLogState(ctx)

	case "stat", "snap":
		if len(args) < 2 {
			reportLogState(ctx)
			break
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 0 {
			setCommandError(ctx, "Usage: :log stat <ticks> (0 disables)")
			return CommandResult{Continue: true}
		}
		ctx.World.Resources.Status.SetSnapshotInterval(uint64(n))
		ctx.Log.Info("app", "msg", "stat interval changed", "ticks", n)
		reportLogState(ctx)

	case "rec", "recorder":
		return handleLogRec(ctx, args[1:])

	default:
		if err := vlog.SetLevelName(args[0]); err != nil {
			setCommandError(ctx, "Usage: :log [on|off|trace|debug|info|warn|error|scope|level|stat]")
			return CommandResult{Continue: true}
		}
		ctx.Log.Info("app", "msg", "log level changed", "level", vlog.LevelName())
		reportLogState(ctx)
	}

	return CommandResult{Continue: true}
}

// handleLogRec controls the flight recorder
// Usage: :log rec | :log rec <ticks> | :log rec flush | :log rec fsm [on|off]
func handleLogRec(ctx *engine.GameContext, args []string) CommandResult {
	reg := ctx.World.Resources.Status
	if len(args) == 0 {
		reportLogState(ctx)
		return CommandResult{Continue: true}
	}

	switch strings.ToLower(args[0]) {
	case "flush", "f":
		rc := reg.Recorder()
		if rc == nil {
			setCommandError(ctx, "Recorder disabled")
			return CommandResult{Continue: true}
		}
		// Command mode holds the world lock; the ring belongs to the tick
		// goroutine, which owns sample. Request, do not flush here.
		rc.Trigger(status.TrigManual)
		ctx.SetStatusMessage("Recorder flush requested", parameter.StatusNotice)

	case "fsm":
		rc := reg.Recorder()
		if rc == nil {
			setCommandError(ctx, "Recorder disabled")
			return CommandResult{Continue: true}
		}
		desired, explicit, ok := parseToggleArg(args[1:])
		if !ok {
			setCommandError(ctx, "Usage: :log rec fsm [on|off]")
			return CommandResult{Continue: true}
		}
		if !explicit {
			desired = !rc.FSMTrigger()
		}
		rc.SetFSMTrigger(desired)
		ctx.SetStatusMessage("Recorder FSM trigger "+toggleWord(desired), parameter.StatusNotice)

	default:
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 {
			setCommandError(ctx, "Usage: :log rec [<ticks>|flush|fsm]")
			return CommandResult{Continue: true}
		}
		// Relayout discards history; the metric set is already frozen
		reg.EnableRecorder(n)
		ctx.Log.Info("app", "msg", "recorder depth changed", "ticks", n)
		reportLogState(ctx)
	}

	return CommandResult{Continue: true}
}

// reportLogState shows session state in the status bar
func reportLogState(ctx *engine.GameContext) {
	target := "off"
	if vlog.Enabled() {
		target = vlog.Path()
	}
	ctx.SetStatusMessage(fmt.Sprintf("log %s | level %s | scope %s | stat %d | rec %d",
		target,
		vlog.LevelName(),
		vlog.ScopeString(vlog.Scopes()),
		ctx.World.Resources.Status.SnapshotInterval(),
		ctx.World.Resources.Status.RecorderDepth()), parameter.StatusNotice)
}

func setCommandError(ctx *engine.GameContext, message string) {
	ctx.SetStatusMessage(message, parameter.StatusNotice)
}

// sessionsUnavailable refuses a session, scenario or journal command in a run
// built without the controller, as a test harness is
const sessionsUnavailable = "Session controls unavailable in this run"

// unavailable is why a feature this platform or build lacks is refused, the one
// phrase its menu row and its command both show: a browser has no files to write,
// and a build may leave a part out
func unavailable(what string) string {
	if !paths.ExternalFiles() {
		return what + " unavailable in a browser"
	}
	return what + " unavailable in this build"
}

// sentence is an error as the status bar shows it, its first letter raised
func sentence(err error) string {
	s := err.Error()
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// handleQuitCommand exits the game
func handleQuitCommand(ctx *engine.GameContext) CommandResult {
	return CommandResult{Continue: false}
}

// handleNewCommand starts a new game. Bare, it resets this run's state through the
// event; purge also clears operator session state. Named, it restarts the run on
// that scenario, because a scenario's regions are what register the metric set the
// run froze. Purge says nothing on that path: what comes back is a fresh run.
func handleNewCommand(ctx *engine.GameContext, args []string, purge bool) CommandResult {
	if len(args) > 1 {
		setCommandError(ctx, "Usage: :n [scenario]")
		return CommandResult{Continue: true}
	}
	if len(args) == 1 {
		return changeScenario(ctx, args[0])
	}
	if !resetGame(ctx, purge) {
		return CommandResult{Continue: true}
	}
	return CommandResult{Continue: true}
}

// resetGame requests a new game on the run as it stands, crossing it in a live
// session so every participant resets from one artifact. False means the request
// was refused and the caller has already been told why.
func resetGame(ctx *engine.GameContext, purge bool) bool {
	if ctx.World.LiveSession() && !ctx.World.IsSessionCoordinator() {
		setCommandError(ctx, "Only the host can reset a live session")
		return false
	}
	// A reset crossed in a held session would wait for a tick nobody reaches, so the
	// hold ends first; a new game that waits holds again as it starts
	if ctx.TimeCtl.Held() {
		ctx.PushLocal(event.EventGameHoldRequest, &event.GamePausePayload{Paused: false})
	}
	reset := &event.GameResetPayload{Purge: purge, Wait: ctx.WaitStart.Load()}
	if ctx.World.LiveSession() {
		ctx.PushCrossing(event.EventGameResetRequest, reset)
	} else {
		ctx.PushEvent(event.EventGameResetRequest, reset)
	}
	ctx.MacroClearFlag.Store(true) // Signal macro reset
	return true
}

// handlePauseCommand starts, ends or toggles a hold: the pause only another :pause
// ends. A live session's hold is its host's, and stops every participant at once.
func handlePauseCommand(ctx *engine.GameContext, args []string) CommandResult {
	held, explicit, ok := parseToggleArg(args)
	if !ok {
		setCommandError(ctx, "Usage: :pause [on|off]")
		return CommandResult{Continue: true}
	}
	if !explicit {
		held = !ctx.TimeCtl.Held()
	}
	if why := holdRefused(ctx, held); why != "" {
		setCommandError(ctx, why)
		return CommandResult{Continue: true}
	}
	ctx.PushLocal(event.EventGameHoldRequest, &event.GamePausePayload{Paused: held})
	return CommandResult{Continue: true}
}

// holdRefused is why this instance may not start, or end, a hold: in a live session
// that is the host's alone, since what it starts stops everyone.
func holdRefused(ctx *engine.GameContext, held bool) string {
	switch {
	case !ctx.World.LiveSession() || ctx.World.IsSessionCoordinator():
		return ""
	case held:
		return "Only the host can pause a live session"
	}
	return "Only the host can resume a live session"
}

// changeScenario asks the runtime to rebuild this run on another scenario. The
// controller validates before it latches, so a name that does not resolve is
// reported here with the game still running.
func changeScenario(ctx *engine.GameContext, name string) CommandResult {
	if ctx.SessionCtl == nil {
		setCommandError(ctx, sessionsUnavailable)
		return CommandResult{Continue: true}
	}
	changed, err := ctx.SessionCtl.ChangeScenario(name)
	if err != nil {
		setCommandError(ctx, sentence(err))
		return CommandResult{Continue: true}
	}
	if !changed {
		// Already running these bytes, so a reset is the same new game without a
		// restart's cost — and in a session it crosses, as a bare :n does
		if !resetGame(ctx, false) {
			return CommandResult{Continue: true}
		}
		return CommandResult{Continue: true}
	}
	ctx.MacroClearFlag.Store(true)
	// Nothing to report on the restart path: the loop returns before the next
	// frame, and what the operator sees is the new scenario.
	return CommandResult{Continue: true}
}

// handleFreeCommand toggles or sets mouse motion cursor tracking
func handleFreeCommand(ctx *engine.GameContext, args []string) CommandResult {
	return applyToggle(ctx, &ctx.MouseFreeMode, args, "free", "Mouse free mode")
}

func handleAutoCommand(ctx *engine.GameContext, args []string) CommandResult {
	next := (ctx.AutoFire.Load() + 1) % 3
	valid := len(args) <= 1
	if len(args) > 0 {
		switch args[0] {
		case "on", "both":
			next = engine.AutoFireBoth
		case "off":
			next = engine.AutoFireOff
		case "main", "gun":
			next = engine.AutoFireMain
		default:
			valid = false
		}
	}
	if !valid {
		setCommandError(ctx, "Usage: :auto [on|off|main]")
		return CommandResult{Continue: true}
	}
	ctx.AutoFire.Store(next)
	word := engine.AutoFireWords[next]
	ctx.SetStatusMessage("Auto: "+word, parameter.StatusNotice)
	return CommandResult{Continue: true}
}

// Explicit on/off keeps scripted input preferences idempotent.
func applyToggle(ctx *engine.GameContext, flag *atomic.Bool, args []string, cmd, label string) CommandResult {
	desired, explicit, ok := parseToggleArg(args)
	if !ok {
		setCommandError(ctx, fmt.Sprintf("Usage: :%s [on|off]", cmd))
		return CommandResult{Continue: true}
	}
	if !explicit {
		desired = !flag.Load()
	}
	flag.Store(desired)

	state := "disabled"
	if desired {
		state = "enabled"
	}
	ctx.SetStatusMessage(label+" "+state, parameter.StatusNotice)
	return CommandResult{Continue: true}
}

// parseToggleArg accepts an empty argument list (toggle) or a single on|off
// token. Returns the requested value, whether it was explicit, and validity.
func parseToggleArg(args []string) (value, explicit, ok bool) {
	if len(args) == 0 {
		return false, false, true
	}
	if len(args) > 1 {
		return false, false, false
	}
	switch strings.ToLower(args[0]) {
	case "on", "e", "enable", "enabled", "1", "true", "y", "yes":
		return true, true, true
	case "off", "d", "disable", "disabled", "0", "false", "n", "no":
		return false, true, true
	}
	return false, false, false
}

func toggleWord(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// handleSystemCommand sets the energy to a specified value
func handleSystemCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 2 {
		setCommandError(ctx, "Usage: :system <name> enable|disable")
		return CommandResult{Continue: true}
	}

	if !ctx.World.HasSystem(args[0]) {
		setCommandError(ctx, fmt.Sprintf("Invalid system: %s", args[0]))
		return CommandResult{Continue: true}
	}

	enabledFlag := false
	switch args[1] {
	case "e", "enable", "enabled":
		enabledFlag = true
	case "d", "disable", "disabled":
		enabledFlag = false
	default:
		setCommandError(ctx, fmt.Sprintf("Invalid state: %s", args[1]))
		return CommandResult{Continue: true}
	}

	if !enabledFlag && !ctx.World.AllowSystemDisable(args[0]) {
		setCommandError(ctx, fmt.Sprintf("%s is required by %s", args[0],
			strings.Join(ctx.World.SystemsRequiring(args[0], engine.DepRequired), ", ")))
		return CommandResult{Continue: true}
	}

	ctx.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
		SystemName: args[0],
		Enabled:    enabledFlag,
	})

	return CommandResult{Continue: true}
}

// handleMouseCommand controls mouse input
func handleMouseCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) == 0 {
		setCommandError(ctx, "Usage: :mouse enable|disable|free")
		return CommandResult{Continue: true}
	}

	switch args[0] {
	case "free":
		res := handleFreeCommand(ctx, args[1:])
		return res

	case "enable":
		msg := "Mouse already enabled"
		if ctx.MouseDisabled.Load() {
			ctx.MouseDisabled.Store(false)
			msg = "Mouse enabled"
		}
		ctx.SetStatusMessage(msg, parameter.StatusNotice)

	case "disable":
		msg := "Mouse already disabled"
		if !ctx.MouseDisabled.Load() {
			ctx.MouseDisabled.Store(true)
			msg = "Mouse disabled"
		}
		ctx.SetStatusMessage(msg, parameter.StatusNotice)

	default:
		setCommandError(ctx, "Usage: :mouse enable|disable|free")
		return CommandResult{Continue: true}
	}

	return CommandResult{Continue: true}
}

// handleEmitCommand emits an event by name with optional TOML payload (debug/testing)
// Usage: :emit EventName
// Usage: :emit EventName { field = value, nested = { x = 1 } }
func handleEmitCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) < 1 {
		setCommandError(ctx, "Usage: :emit <EventName> [{ payload }]")
		return CommandResult{Continue: true}
	}

	name := args[0]

	// Normalize: add "Event" prefix if missing
	if !strings.HasPrefix(name, "Event") {
		name = "Event" + name
	}

	eventType, ok := event.GetEventType(name)
	if !ok {
		setCommandError(ctx, fmt.Sprintf("Unknown event: %s", name))
		return CommandResult{Continue: true}
	}

	// Parse payload if provided
	var payload any
	if len(args) > 1 {
		var err error
		payload, err = parseEventPayload(ctx, eventType, strings.Join(args[1:], " "))
		if err != nil {
			setCommandError(ctx, sentence(err))
			return CommandResult{Continue: true}
		}
	}

	ctx.PushEvent(eventType, payload)

	return CommandResult{Continue: true}
}

// parseEventPayload reads an inline TOML table, "{ field = value, ... }", into the
// event's payload as a scenario's EmitEvent would, the running programs among its
// species and the scenario's loot kinds among its kinds; empty text is no payload
func parseEventPayload(ctx *engine.GameContext, et event.EventType, raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	// Wrap inline table as TOML key-value for parser compatibility
	parsed, err := toml.NewParser([]byte("_p = " + raw)).Parse()
	if err != nil {
		return nil, fmt.Errorf("payload: %w", err)
	}
	declared := component.Declared{Loot: ctx.World.Resources.Loot.Names()}
	for _, p := range ctx.World.Resources.Behaviors {
		declared.Programs = append(declared.Programs, p.Name)
	}
	return event.AuthoredPayload(event.PayloadNames(declared), et, parsed["_p"])
}

// handleTelemetryCommand opens the telemetry overlay, or runs a telemetry subcommand
func handleTelemetryCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "s", "save", "snap":
			return handleTelemetrySaveCommand(ctx)
		case "hud":
			return applyToggle(ctx, &ctx.OverlayHUD, args[1:], "hud", "Telemetry HUD")
		case "unpin", "clear":
			ctx.ClearOverlayPins()
			ctx.SetStatusMessage("Pins cleared", parameter.StatusNotice)
			return CommandResult{Continue: true}
		default:
			setCommandError(ctx, "Usage: :telemetry [save|hud|unpin]")
			return CommandResult{Continue: true}
		}
	}

	ctx.RequestMode(core.ModeOverlay)
	ctx.PauseForInput()
	ctx.PushLocalOrigin(event.EventMetaTelemetryRequest, nil, event.OriginDevice)
	return CommandResult{Continue: true}
}

// snapRecord is one captured snapshot record, written after the lock is released
type snapRecord struct {
	sub  string
	args []any
}

// handleTelemetrySaveCommand captures the registry and context under the world
// lock, one coherent tick, and writes the file off it so a live tick never waits
// on disk. The status bar reports the path once the write drains.
func handleTelemetrySaveCommand(ctx *engine.GameContext) CommandResult {
	if !vlog.Linked {
		setCommandError(ctx, unavailable("Telemetry snapshot"))
		return CommandResult{Continue: true}
	}
	var records []snapRecord
	capture := func(sub string, args ...any) { records = append(records, snapRecord{sub, args}) }
	ctx.SnapshotContext(capture)
	ctx.World.Resources.Status.Snapshot(capture)
	run, tick := ctx.Log.Stamp()

	core.Go(func() {
		path, err := vlog.Dump(run, tick, func(emit func(sub string, args ...any)) {
			for _, r := range records {
				emit(r.sub, r.args...)
			}
		})
		if err != nil {
			setCommandError(ctx, "Snapshot failed: "+err.Error())
			return
		}
		ctx.Log.Info("app", "msg", "snapshot saved", "path", path)
		ctx.SetStatusMessage("Snapshot saved to "+path, parameter.StatusNotice)
	})

	return CommandResult{Continue: true}
}

// handleDebugCommand opens the profiler report, or runs a profiling subcommand
func handleDebugCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) == 0 {
		ctx.RequestMode(core.ModeOverlay)
		ctx.PauseForInput()
		ctx.PushLocalOrigin(event.EventMetaDebugRequest, nil, event.OriginDevice)
		return CommandResult{Continue: true}
	}

	kind := strings.ToLower(args[0])
	if _, ok := timedCaptures[kind]; ok {
		handleCaptureCommand(ctx, kind, args[1:])
		return CommandResult{Continue: true}
	}
	switch kind {
	case "p", "prof":
		handleProfCommand(ctx, args[1:])
	case "b", "break":
		handleBreakCommand(ctx, args[1:])
	case "heap":
		if !paths.ExternalFiles() {
			setCommandError(ctx, unavailable("Heap profile"))
			break
		}
		dir := diagnosticsDir()
		core.Go(func() {
			path, err := prof.WriteHeap(dir)
			reportCapture(ctx, "Heap profile", path, err)
		})
		ctx.SetStatusMessage("Writing heap profile", parameter.StatusNotice)
	default:
		setCommandError(ctx, "Usage: :debug [prof [on|off]|break ...|cpu [s]|heap|mutex [s]|trace [s]]")
	}
	return CommandResult{Continue: true}
}

// handleProfCommand toggles or sets the profiler; starting it pins its cards
func handleProfCommand(ctx *engine.GameContext, args []string) {
	p := ctx.World.Resources.Prof
	on, explicit, ok := parseToggleArg(args)
	if !ok {
		setCommandError(ctx, "Usage: :d prof [on|off]")
		return
	}
	if !explicit {
		on = !p.Profiling()
	}
	p.SetOn(on)
	msg := "Profiler off"
	if on {
		for _, key := range [...]string{"prof", "prof.top"} {
			if !slices.Contains(ctx.OverlayPins(), key) {
				ctx.ToggleOverlayPin(key)
			}
		}
		msg = "Profiler on: its cards are pinned to the HUD, :d shows every module"
	}
	ctx.Log.Info("app", "msg", "profiler toggled", "on", on)
	ctx.SetStatusMessage(msg, parameter.StatusNotice)
}

const breakUsage = "Usage: :d break [event <Event>|region <name>|state <name>|stat <key> [op] [value]|clear [...]]"

// handleBreakCommand lists, sets or clears the standing breakpoints. A hit pauses
// the run on the command line, so leaving the line resumes it.
func handleBreakCommand(ctx *engine.GameContext, args []string) {
	tc := ctx.TimeCtl
	if len(args) == 0 {
		var labels []string
		if bs := tc.Breaks(); bs != nil {
			for _, bp := range bs.List {
				labels = append(labels, bp.Label)
			}
		}
		if len(labels) == 0 {
			ctx.SetStatusMessage("No breakpoints; "+breakUsage[len("Usage: "):], parameter.StatusNotice)
			return
		}
		ctx.SetStatusMessage("Break on "+strings.Join(labels, "; "), parameter.StatusNotice)
		return
	}
	if strings.EqualFold(args[0], "clear") {
		var match func(*engine.Breakpoint) bool
		if len(args) > 1 {
			bp, err := parseBreakpoint(ctx, args[1:])
			if err != nil {
				setCommandError(ctx, err.Error())
				return
			}
			// A stat named without a comparison clears every one on that key
			whole := bp.Kind != engine.BreakOnStat || len(args) > 2
			match = func(o *engine.Breakpoint) bool {
				return o.Kind == bp.Kind && strings.EqualFold(o.Name, bp.Name) && (!whole || o.Label == bp.Label)
			}
		}
		n := tc.ClearBreaks(match)
		ctx.Log.Info("app", "msg", "breakpoints cleared", "count", n)
		ctx.SetStatusMessage(fmt.Sprintf("Cleared %d breakpoint(s)", n), parameter.StatusNotice)
		return
	}
	if ctx.World.Session().Live {
		setCommandError(ctx, "Breakpoints are unavailable in a live session")
		return
	}
	bp, err := parseBreakpoint(ctx, args)
	if err != nil {
		setCommandError(ctx, err.Error())
		return
	}
	tc.SetBreak(bp)
	ctx.Log.Info("app", "msg", "breakpoint set", "on", bp.Label)
	ctx.SetStatusMessage("Break on "+bp.Label, parameter.StatusNotice)
}

// parseBreakpoint reads `<kind> <name> [op] [value]` into a breakpoint
func parseBreakpoint(ctx *engine.GameContext, args []string) (*engine.Breakpoint, error) {
	if len(args) < 2 {
		return nil, errors.New(breakUsage)
	}
	word := strings.ToLower(args[0])
	if word == "ev" {
		word = "event"
	}
	idx := slices.Index(engine.BreakKindNames[:], word)
	if idx < 0 {
		return nil, errors.New(breakUsage)
	}
	kind := engine.BreakKind(idx)
	bp := &engine.Breakpoint{Kind: kind, Name: args[1]}
	reg := ctx.World.Resources.Status
	switch kind {
	case engine.BreakOnEvent:
		et, ok := event.GetEventType(args[1])
		if !ok {
			et, ok = event.GetEventType("Event" + args[1])
		}
		// A hit pushes these two itself, so breaking on them would never end
		if !ok || et == event.EventNone || et == event.EventGamePauseRequest || et == event.EventMetaStatusMessageRequest {
			return nil, fmt.Errorf("No breakable event %q", args[1])
		}
		bp.Event, bp.Name = et, event.GetEventName(et)
	case engine.BreakOnRegion:
		region := ""
		for _, k := range reg.Strings.Keys() {
			if name, ok := strings.CutPrefix(k, "fsm."); ok && strings.HasSuffix(name, ".state") &&
				strings.EqualFold(strings.TrimSuffix(name, ".state"), args[1]) {
				region = strings.TrimSuffix(name, ".state")
			}
		}
		if region == "" {
			return nil, fmt.Errorf("No region %q in this scenario", args[1])
		}
		bp.Name = region
	case engine.BreakOnStat:
		test, cond, err := statCondition(reg, args[1], args[2:])
		if err != nil {
			return nil, err
		}
		bp.Test, bp.Label = test, "stat "+args[1]+cond
		return bp, nil
	}
	if len(args) > 2 {
		return nil, errors.New(breakUsage)
	}
	bp.Label = word + " " + bp.Name
	return bp, nil
}

// breakOps maps each comparison word to the sign it tests: what the comparison
// of cell against value must be, as cmp.Compare reports it
var breakOps = map[string][]int{
	"eq": {0}, "equal": {0}, "e": {0}, "=": {0}, "==": {0},
	"ne": {-1, 1}, "!=": {-1, 1},
	"gt": {1}, "g": {1}, ">": {1},
	"ge": {0, 1}, ">=": {0, 1},
	"lt": {-1}, "l": {-1}, "<": {-1},
	"le": {-1, 0}, "<=": {-1, 0},
}

// statCondition builds the comparison over one status cell. With no value it
// holds while the cell is set: non-zero, true, or a string neither empty nor "-".
func statCondition(reg *status.Registry, key string, rest []string) (func() bool, string, error) {
	op, value := "eq", ""
	switch len(rest) {
	case 0:
	case 1:
		value = rest[0]
	case 2:
		op, value = strings.ToLower(rest[0]), rest[1]
	default:
		return nil, "", errors.New(breakUsage)
	}
	signs, ok := breakOps[op]
	if !ok {
		return nil, "", fmt.Errorf("Unknown comparison %q: eq ne gt ge lt le", op)
	}
	holds := func(c int) bool { return slices.Contains(signs, c) }
	cond := ""
	if value != "" {
		cond = " " + op + " " + value
	}
	switch {
	case reg.Ints.Has(key):
		cell := reg.Ints.Get(key)
		if value == "" {
			return func() bool { return cell.Load() != 0 }, cond, nil
		}
		want, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("%s is an integer", key)
		}
		return func() bool { return holds(cmp.Compare(cell.Load(), want)) }, cond, nil
	case reg.Floats.Has(key):
		cell := reg.Floats.Get(key)
		if value == "" {
			return func() bool { return cell.Get() != 0 }, cond, nil
		}
		want, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, "", fmt.Errorf("%s is a number", key)
		}
		return func() bool { return holds(cmp.Compare(cell.Get(), want)) }, cond, nil
	case reg.Bools.Has(key):
		cell := reg.Bools.Get(key)
		want := true
		if value != "" {
			b, err := strconv.ParseBool(value)
			if err != nil {
				return nil, "", fmt.Errorf("%s is true or false", key)
			}
			want = b
		}
		return func() bool { return holds(boolCompare(cell.Load(), want)) }, cond, nil
	case reg.Strings.Has(key):
		cell := reg.Strings.Get(key)
		if value == "" {
			return func() bool { v := cell.Load(); return v != "" && v != "-" }, cond, nil
		}
		return func() bool { return holds(strings.Compare(cell.Load(), value)) }, cond, nil
	}
	return nil, "", fmt.Errorf("No status key %q; :t lists them", key)
}

func boolCompare(a, b bool) int {
	if a == b {
		return 0
	}
	if a {
		return 1
	}
	return -1
}

// timedCaptures are the :d captures that run for a duration, by subcommand
var timedCaptures = map[string]struct {
	start func(dir string, d time.Duration, done func(string, error)) (string, error)
	label string
}{
	"cpu":   {prof.StartCPU, "CPU profile"},
	"trace": {prof.StartTrace, "Execution trace"},
	"mutex": {prof.StartMutex, "Mutex profile"},
}

// handleCaptureCommand starts a timed capture
func handleCaptureCommand(ctx *engine.GameContext, kind string, args []string) {
	d := parameter.ProfCaptureDefault
	if len(args) > 0 {
		secs, err := strconv.Atoi(args[0])
		if err != nil || secs < 1 || time.Duration(secs)*time.Second > parameter.ProfCaptureMax {
			setCommandError(ctx, fmt.Sprintf("Usage: :d %s [seconds], at most %v", kind, parameter.ProfCaptureMax))
			return
		}
		d = time.Duration(secs) * time.Second
	}

	capture := timedCaptures[kind]
	label := capture.label
	if !paths.ExternalFiles() {
		setCommandError(ctx, unavailable(label))
		return
	}
	path, err := capture.start(diagnosticsDir(), d, func(path string, err error) { reportCapture(ctx, label, path, err) })
	if err != nil {
		setCommandError(ctx, label+" failed: "+err.Error())
		return
	}
	ctx.SetStatusMessage(fmt.Sprintf("%s for %v to %s", label, d, path), parameter.StatusNotice)
}

// reportCapture announces a finished capture from whichever goroutine wrote it
func reportCapture(ctx *engine.GameContext, label, path string, err error) {
	if err != nil {
		setCommandError(ctx, label+" failed: "+err.Error())
		return
	}
	ctx.Log.Info("app", "msg", "capture saved", "kind", label, "path", path)
	ctx.SetStatusMessage(label+" saved to "+path, parameter.StatusNotice)
}

// diagnosticsDir is where captures land: the log directory, or the platform
// default in a build without logging
func diagnosticsDir() string {
	if dir := vlog.Dir(); dir != "" {
		return dir
	}
	return paths.DefaultLogDir()
}

// handleHelpCommand asks for the help overlay in place of any overlay shown
func handleHelpCommand(ctx *engine.GameContext) CommandResult {
	ctx.RequestMode(core.ModeOverlay)
	ctx.PauseForInput()
	ctx.SetOverlayContent(nil)
	ctx.PushLocalOrigin(event.EventMetaHelpRequest, nil, event.OriginDevice)
	return CommandResult{Continue: true}
}

// Opening a session uses its own admission check, before the live-session guard applies.
func handleHostCommand(ctx *engine.GameContext, args []string) CommandResult {
	if ctx.SessionCtl == nil {
		setCommandError(ctx, sessionsUnavailable)
		return CommandResult{Continue: true}
	}
	if len(args) != 1 && len(args) != 2 {
		setCommandError(ctx, "Usage: :host <doors> [host|migrate]  (e.g. :host :7777 migrate, :host :7777,wss://:7778)")
		return CommandResult{Continue: true}
	}
	authority := ""
	if len(args) == 2 {
		authority = args[1]
	}
	if err := ctx.SessionCtl.BeginHosting(args[0], authority); err != nil {
		setCommandError(ctx, sentence(err))
		return CommandResult{Continue: true}
	}
	return CommandResult{Continue: true}
}

// handleRelayCommand turns on or off carrying the game for players who reach this
// run and not their host, as after a host loss; one that joined through this run
// is carried either way. Usage: :relay [on|off]
func handleRelayCommand(ctx *engine.GameContext, args []string) CommandResult {
	if reason := relayUnavailable(ctx); reason != "" {
		setCommandError(ctx, reason)
		return CommandResult{Continue: true}
	}
	return applyToggle(ctx, &ctx.Relaying, args, "relay", "Relaying for others")
}

// relayUnavailable refuses relaying where nobody can reach this run: a browser
// is never dialled
var relayUnavailable = lacking(paths.ExternalFiles(), "Relaying")

// handleJoinCommand replaces this solo run with one joined to a session.
// Usage: :join <target>   the same forms -join takes
func handleJoinCommand(ctx *engine.GameContext, args []string) CommandResult {
	if ctx.SessionCtl == nil {
		setCommandError(ctx, sessionsUnavailable)
		return CommandResult{Continue: true}
	}
	if len(args) != 1 {
		setCommandError(ctx, "Usage: :join <target>  (e.g. :join host:7777, a ws(s):// URL, or an https:// site for a new session)")
		return CommandResult{Continue: true}
	}
	if err := ctx.SessionCtl.Join(args[0]); err != nil {
		setCommandError(ctx, "Join: "+err.Error())
		return CommandResult{Continue: true}
	}
	ctx.MacroClearFlag.Store(true)
	return CommandResult{Continue: true}
}

// handleLeaveCommand takes this run out of its session into one of its own, the
// world as it stands. The run's loop serves it; the status line says how it went.
func handleLeaveCommand(ctx *engine.GameContext, args []string) CommandResult {
	if ctx.SessionCtl == nil {
		setCommandError(ctx, sessionsUnavailable)
		return CommandResult{Continue: true}
	}
	if len(args) != 0 {
		setCommandError(ctx, "Usage: :leave")
		return CommandResult{Continue: true}
	}
	if err := ctx.SessionCtl.Leave(); err != nil {
		setCommandError(ctx, "Leave: "+err.Error())
	}
	return CommandResult{Continue: true}
}

// handleJournalCommand reports the journals, starts one where the run stands, or
// closes one: this instance's, or with a slot the one of the bot seat this run
// holds there. The run's loop serves either later; the status line says how it went.
// Usage: :journal [start|stop] [slot]
func handleJournalCommand(ctx *engine.GameContext, args []string) CommandResult {
	const usage = "Usage: :journal [start|stop] [slot]"
	if ctx.SessionCtl == nil {
		setCommandError(ctx, sessionsUnavailable)
		return CommandResult{Continue: true}
	}
	if len(args) == 0 {
		ctx.SetStatusMessage(ctx.SessionCtl.JournalSummary(), parameter.StatusNotice)
		return CommandResult{Continue: true}
	}
	slot := -1
	switch {
	case len(args) > 2:
		setCommandError(ctx, usage)
		return CommandResult{Continue: true}
	case len(args) == 2:
		n, err := parsePlayerSlot(args[1])
		if err != nil {
			setCommandError(ctx, sentence(err))
			return CommandResult{Continue: true}
		}
		slot = n
	}
	var err error
	switch args[0] {
	case "start", "on":
		if !vlog.Linked {
			setCommandError(ctx, unavailable("Journal"))
			return CommandResult{Continue: true}
		}
		err = ctx.SessionCtl.StartJournal(slot)
	case "stop", "off":
		err = ctx.SessionCtl.StopJournal(slot)
	default:
		setCommandError(ctx, usage)
		return CommandResult{Continue: true}
	}
	if err != nil {
		setCommandError(ctx, sentence(err))
	}
	return CommandResult{Continue: true}
}

// handleSessionCommand reports what this run is part of.
func handleSessionCommand(ctx *engine.GameContext) CommandResult {
	if ctx.SessionCtl == nil {
		setCommandError(ctx, sessionsUnavailable)
		return CommandResult{Continue: true}
	}
	ctx.SetStatusMessage(ctx.SessionCtl.SessionSummary(), parameter.StatusNotice)
	return CommandResult{Continue: true}
}

// handleBotCommand lists, seats or drops the bots this run holds. Outside the
// live-session guard, like :host: a bot joins the session as any participant does.
// Usage: :bot | :bot add [N[:graph]|graph] | :bot drop <slot>
func handleBotCommand(ctx *engine.GameContext, args []string) CommandResult {
	if ctx.SessionCtl == nil {
		setCommandError(ctx, sessionsUnavailable)
		return CommandResult{Continue: true}
	}
	var err error
	switch {
	case len(args) == 0:
		ctx.SetStatusMessage(ctx.SessionCtl.BotSummary(), parameter.StatusNotice)
		return CommandResult{Continue: true}
	case args[0] == "add" && len(args) <= 2:
		graph := ""
		if len(args) == 2 {
			graph = args[1]
		}
		if err = ctx.SessionCtl.AddBot(graph); err == nil {
			ctx.SetStatusMessage("Bot joining; :bot lists this run's bots", parameter.StatusNotice)
		}
	case args[0] == "drop" && len(args) == 2:
		slot, perr := parsePlayerSlot(args[1])
		if err = perr; err == nil {
			err = ctx.SessionCtl.DropBot(slot)
			if err == nil {
				ctx.SetStatusMessage(fmt.Sprintf("Bot %X leaving", slot), parameter.StatusNotice)
			}
		}
	default:
		setCommandError(ctx, "Usage: :bot [add [N[:graph]|graph] | drop <slot>]")
		return CommandResult{Continue: true}
	}
	if err != nil {
		setCommandError(ctx, sentence(err))
	}
	return CommandResult{Continue: true}
}

func parsePlayerSlot(value string) (int, error) {
	base := 10
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		base, value = 16, value[2:]
	} else if strings.ContainsAny(value, "abcdefABCDEF") {
		base = 16
	}
	n, err := strconv.ParseUint(value, base, 8)
	if err != nil || n >= parameter.MaxPlayers {
		return 0, fmt.Errorf("slot must be 0..%d or hexadecimal 0..%X", parameter.MaxPlayers-1, parameter.MaxPlayers-1)
	}
	return int(n), nil
}

func handlePlayerCommand(ctx *engine.GameContext, args []string) CommandResult {
	result := CommandResult{Continue: true}
	if ctx.SessionCtl == nil {
		setCommandError(ctx, sessionsUnavailable)
		return result
	}
	if len(args) == 0 {
		participants := ctx.SessionCtl.Participants()
		slots := make(map[uint32]uint8, len(participants)) // participant to slot, to name a bot's holder
		for _, p := range participants {
			if c, ok := ctx.World.Components.Cursor.GetComponent(p.Entity); ok {
				slots[c.PeerID] = p.Slot
			}
		}
		var names []string
		for _, p := range participants {
			name := fmt.Sprintf("%X player", p.Slot)
			if p.Holder != 0 {
				name = fmt.Sprintf("%X bot", p.Slot)
				if holder, ok := slots[p.Holder]; ok {
					name += fmt.Sprintf(" of %X", holder)
				}
			}
			if p.Away {
				name += " (away)"
			}
			names = append(names, name)
		}
		ctx.SetStatusMessage("Players: "+strings.Join(names, ", ")+"; :player drop <slot>", parameter.StatusNotice)
		return result
	}
	if len(args) != 2 || args[0] != "drop" {
		setCommandError(ctx, "Usage: :player [drop <slot>]")
		return result
	}
	slot, err := parsePlayerSlot(args[1])
	if err == nil {
		err = ctx.SessionCtl.DropPlayer(slot)
	}
	if err != nil {
		setCommandError(ctx, sentence(err))
	} else {
		ctx.SetStatusMessage(fmt.Sprintf("Slot %X and its bots leaving", slot), parameter.StatusNotice)
	}
	return result
}

// handleAboutCommand triggers about overlay event
func handleAboutCommand(ctx *engine.GameContext) CommandResult {
	ctx.RequestMode(core.ModeOverlay)
	ctx.PauseForInput()
	ctx.PushLocalOrigin(event.EventMetaAboutRequest, nil, event.OriginDevice)
	return CommandResult{Continue: true}
}

// handleContentCommand reports corpus telemetry in the status bar
func handleContentCommand(ctx *engine.GameContext) CommandResult {
	reg := ctx.World.Resources.Status
	msg := fmt.Sprintf("content %s | files %d | blocks %d | served %d | now %s",
		reg.Strings.Get("content.source").Load(),
		reg.Ints.Get("content.files").Load(),
		reg.Ints.Get("content.blocks").Load(),
		reg.Ints.Get("content.served").Load(),
		reg.Strings.Get("content.file").Load(),
	)
	ctx.SetStatusMessage(msg, parameter.StatusNotice)
	return CommandResult{Continue: true}
}

// handleEnergyCommand sets the energy to a specified value
func handleEnergyCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for energy")
		return CommandResult{Continue: true}
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid arguments for energy")
		return CommandResult{Continue: true}
	}

	ctx.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{
		Entity: ctx.World.Resources.Player.Entity,
		Value:  value,
	})

	return CommandResult{Continue: true}
}

// handleHeatCommand sets the heat to a specified value
func handleHeatCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 1 {
		setCommandError(ctx, "Usage: :heat <0-100>")
		return CommandResult{Continue: true}
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid number format")
		return CommandResult{Continue: true}
	}

	if value < 0 {
		value = 0
	}
	if value > parameter.HeatMax {
		value = parameter.HeatMax
	}

	ctx.PushLocal(event.EventHeatSetRequest, &event.HeatSetRequestPayload{
		Entity: ctx.World.Resources.Player.Entity,
		Value:  value,
	})

	return CommandResult{Continue: true}
}

// handleBoostCommand triggers boost request event
func handleBoostCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushLocal(event.EventHeatSetRequest, &event.HeatSetRequestPayload{
		Entity: ctx.World.Resources.Player.Entity,
		Value:  parameter.HeatMax,
	})

	ctx.PushLocal(event.EventBoostActivate, &event.BoostActivatePayload{
		Entity:   ctx.World.Resources.Player.Entity,
		Duration: parameter.BoostBaseDuration,
	})

	return CommandResult{Continue: true}
}

// handleGodCommand sets max heat, high energy of the given sign and every weapon;
// :demon is the negative sign
func handleGodCommand(ctx *engine.GameContext, sign int) CommandResult {
	player := ctx.World.Resources.Player.Entity
	ctx.PushLocal(event.EventHeatSetRequest, &event.HeatSetRequestPayload{Entity: player, Value: parameter.HeatMax})
	ctx.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{
		Entity: player, Value: sign * parameter.GodEnergyAmount, Weapons: true,
	})
	return CommandResult{Continue: true}
}

// handleWeaponCommand sets one weapon's charges, by its name or its attack's: past
// the kind's maximum it maxes out, 0 takes it, the gun included, and a negative
// count flips the energy's sign, which drops every other loot weapon, before the set
func handleWeaponCommand(ctx *engine.GameContext, args []string) CommandResult {
	const usage = "Usage: :w <weapon> <charges>"
	if len(args) != 2 {
		setCommandError(ctx, usage)
		return CommandResult{Continue: true}
	}
	weapon := slices.IndexFunc(component.WeaponSpecs[:], func(spec component.WeaponSpec) bool {
		return strings.EqualFold(args[0], spec.Name) || strings.EqualFold(args[0], component.CombatAttackNames[spec.Attack])
	})
	if weapon < 0 {
		setCommandError(ctx, "Unknown weapon: "+args[0])
		return CommandResult{Continue: true}
	}
	charges, err := strconv.Atoi(args[1])
	if err != nil {
		setCommandError(ctx, usage)
		return CommandResult{Continue: true}
	}

	player, kind := ctx.World.Resources.Player.Entity, component.WeaponType(weapon)
	if charges < 0 {
		ctx.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{
			Entity: player, Flip: true, Weapon: kind, Charges: -charges,
		})
		return CommandResult{Continue: true}
	}
	ctx.PushLocal(event.EventWeaponSetRequest, &event.WeaponSetRequestPayload{Entity: player, Weapon: kind, Charges: charges})
	return CommandResult{Continue: true}
}

// handleBlossomCommand triggers a blossom wave
func handleBlossomCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushLocal(event.EventParticleWave, &event.ParticleWavePayload{Behavior: component.ParticleBlossom})
	return CommandResult{Continue: true}
}

// handleDecayCommand triggers a decay wave
func handleDecayCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushLocal(event.EventParticleWave, &event.ParticleWavePayload{Behavior: component.ParticleDecay})
	return CommandResult{Continue: true}
}

// handleDustCommand triggers glyph to dust transform
func handleDustCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushLocal(event.EventDustAllRequest, nil)
	return CommandResult{Continue: true}
}

// === DEBUG ===

func handleFlowCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) == 0 {
		ctx.PushLocal(event.EventDebugFlowToggle, nil)
	} else {
		groupID, err := strconv.Atoi(args[0])
		if err != nil || groupID < 0 || groupID >= component.MaxTargetGroups {
			setCommandError(ctx, fmt.Sprintf("Invalid group ID: %s (0-%d)", args[0], component.MaxTargetGroups-1))
			return CommandResult{Continue: true}
		}
		ctx.PushLocal(event.EventDebugFlowToggle, &event.DebugFlowGroupPayload{
			GroupID: uint8(groupID),
		})
	}
	return CommandResult{Continue: true}
}

func handleGraphCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) == 0 {
		ctx.PushLocal(event.EventDebugGraphToggle, nil)
	} else {
		groupID, err := strconv.Atoi(args[0])
		if err != nil || groupID < 0 || groupID >= component.MaxTargetGroups {
			setCommandError(ctx, fmt.Sprintf("Invalid group ID: %s (0-%d)", args[0], component.MaxTargetGroups-1))
			return CommandResult{Continue: true}
		}
		ctx.PushLocal(event.EventDebugGraphToggle, &event.DebugFlowGroupPayload{
			GroupID: uint8(groupID),
		})
	}
	return CommandResult{Continue: true}
}

// handleSpeedCommand reports or sets the simulation time scale
// Usage: :speed | :speed <1/8|1/4|1/2|1|2|4|8> | :speed +|- | :speed reset
func handleSpeedCommand(ctx *engine.GameContext, args []string) CommandResult {
	cur := ctx.TimeCtl.Scale()
	if len(args) == 0 {
		ctx.SetStatusMessage("Speed "+cur.String()+"x", parameter.StatusNotice)
		return CommandResult{Continue: true}
	}
	if why := speedRefused(ctx); why != "" {
		setCommandError(ctx, why)
		return CommandResult{Continue: true}
	}

	var next engine.TimeScale
	switch strings.ToLower(args[0]) {
	case "+", "up", "faster":
		next = engine.ScaleStep(cur, 1)
	case "-", "down", "slower":
		next = engine.ScaleStep(cur, -1)
	case "reset", "normal":
		next = engine.ScaleNormal
	default:
		s, ok := engine.ParseScale(args[0])
		if !ok {
			setCommandError(ctx, "Usage: :speed [1/8|1/4|1/2|1|2|4|8|+|-|reset]")
			return CommandResult{Continue: true}
		}
		next = s
	}

	ctx.PushLocal(event.EventGameSpeedRequest, &event.GameSpeedPayload{Num: next.Num, Den: next.Den})
	ctx.SetStatusMessage("Speed "+next.String()+"x", parameter.StatusNotice)
	return CommandResult{Continue: true}
}

// speedRefused is why this instance may not set the rate: in a live session that is
// the host's alone, since every participant runs at it.
func speedRefused(ctx *engine.GameContext) string {
	if ctx.World.LiveSession() && !ctx.World.IsSessionCoordinator() {
		return "Only the host can change the speed in a live session"
	}
	return ""
}

// handleStepCommand advances the paused simulation or arms a run-until breakpoint
// Usage: :step [n] | :step [rate] fsm [region] [pause] | :step [rate] ev <Event> [pause] | :step off
func handleStepCommand(ctx *engine.GameContext, args []string) CommandResult {
	p := &event.GameStepPayload{}

	switch {
	case len(args) == 0:
		p.Ticks = 1

	case strings.EqualFold(args[0], "off"), strings.EqualFold(args[0], "clear"):
		p.Off = true

	default:
		rest := args
		if s, ok := engine.ParseScale(args[0]); ok && len(args) > 1 {
			p.Num, p.Den = s.Num, s.Den
			rest = args[1:]
		}
		if p.Num == 0 && len(rest) == 1 {
			if n, err := strconv.Atoi(rest[0]); err == nil {
				if n < 1 {
					return stepUsage(ctx)
				}
				p.Ticks = int64(n)
				break
			}
		}
		if !parseStepCond(p, rest) {
			return stepUsage(ctx)
		}
	}

	ctx.PushLocal(event.EventGameStepRequest, p)
	return CommandResult{Continue: true}
}

// parseStepCond fills the run-until fields from "fsm [region]" or "ev <Event>",
// with an optional trailing "pause"
func parseStepCond(p *event.GameStepPayload, args []string) bool {
	if len(args) > 0 && strings.EqualFold(args[len(args)-1], "pause") {
		p.Pause = true
		args = args[:len(args)-1]
	}
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(args[0]) {
	case "fsm":
		p.Mode = "fsm"
		if len(args) > 1 {
			p.Region = args[1]
		}
		return len(args) <= 2
	case "ev", "event":
		if len(args) != 2 {
			return false
		}
		p.Mode = "event"
		p.Event = args[1]
		if !strings.HasPrefix(p.Event, "Event") {
			p.Event = "Event" + p.Event
		}
		return true
	}
	return false
}

func stepUsage(ctx *engine.GameContext) CommandResult {
	setCommandError(ctx, "Usage: :step [n] | :step [rate] fsm [region] [pause] | :step [rate] ev <Event> [pause] | :step off")
	return CommandResult{Continue: true}
}

// handleReplayCommand moves a replay to its start, or to a tick of the run it
// shows, named directly or as game time at 1x.
func handleReplayCommand(ctx *engine.GameContext, args []string) CommandResult {
	const usage = "Usage: :replay restart | tick <n> | time <[[h:]m:]s>"
	switch {
	case len(args) == 1 && args[0] == "restart":
		ctx.ReplaySeek(0, true)
	case len(args) == 2 && args[0] == "tick":
		n, err := strconv.ParseUint(args[1], 10, 64)
		if err != nil {
			setCommandError(ctx, usage)
			break
		}
		ctx.ReplaySeek(n, false)
	case len(args) == 2 && args[0] == "time":
		at, ok := parseClock(args[1])
		if !ok {
			setCommandError(ctx, usage)
			break
		}
		tick := ctx.World.TickInterval()
		ctx.ReplaySeek(uint64((at+tick/2)/tick), false)
	default:
		setCommandError(ctx, usage)
	}
	return CommandResult{Continue: true}
}

// parseClock reads seconds, m:s or h:m:s; only the seconds take a fraction, and
// a field after the first stays under 60.
func parseClock(s string) (time.Duration, bool) {
	fields := strings.Split(s, ":")
	if len(fields) > 3 {
		return 0, false
	}
	var secs float64
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		last := i == len(fields)-1
		if err != nil || !(v >= 0 && v < 1e9) || !last && v != float64(int64(v)) || i > 0 && v >= 60 {
			return 0, false
		}
		secs = secs*60 + v
	}
	return time.Duration(secs * float64(time.Second)), true
}

// handleRegionCommand controls FSM regions for debugging.
// Each invocation performs one primitive operation; entering a region that the
// escalation chain would reach is pause-then-spawn, issued as two commands.
func handleRegionCommand(ctx *engine.GameContext, args []string) CommandResult {
	const usage = "Usage: :region list | spawn <name> <state> | pause|resume|terminate <name>"

	res := CommandResult{Continue: true}
	if len(args) == 0 {
		setCommandError(ctx, usage)
		return res
	}

	p := &event.FSMRegionPayload{Op: strings.ToLower(args[0])}
	switch p.Op {
	case event.RegionList:
		if len(args) != 1 {
			setCommandError(ctx, usage)
			return res
		}
	case event.RegionSpawn:
		if len(args) != 3 {
			setCommandError(ctx, usage)
			return res
		}
		p.Region, p.State = args[1], args[2]
	case event.RegionPause, event.RegionResume, event.RegionTerminate:
		if len(args) != 2 {
			setCommandError(ctx, usage)
			return res
		}
		p.Region = args[1]
	default:
		setCommandError(ctx, usage)
		return res
	}

	ctx.PushEvent(event.EventFSMRegionRequest, p)
	return res
}

package mode

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// CommandResult represents the outcome of command execution
type CommandResult struct {
	Continue   bool // false = exit game
	KeepPaused bool // true = caller should not unpause
}

// commandNames lists every case label below, aliases included. Kept adjacent to
// the switch so the two move together; TestCommandsDocumented cross-checks it
// against internal/help.
var commandNames = []string{
	"flow", "graph", "l", "log", "q", "quit", "n", "new", "new!", "n!",
	"f", "free", "a", "auto", "s", "system", "m", "mouse", "e", "emit", "event",
	"d", "debug", "h", "help", "?", "about", "content", "energy", "heat",
	"boost", "god", "demon", "blossom", "decay", "cleaner", "dust",
	"sp", "speed", "st", "step",
	"r", "region",
}

// CommandNames returns the recognised command names and aliases
func CommandNames() []string { return commandNames }

// ExecuteCommand parses and executes a command string
// Returns CommandResult indicating whether game should continue and pause state
func ExecuteCommand(ctx *engine.GameContext, command string) CommandResult {
	command = strings.TrimSpace(command)
	if command == "" {
		return CommandResult{Continue: true, KeepPaused: false}
	}

	// Parse command into parts (space-separated)
	parts := strings.Fields(command)
	cmd := parts[0]
	args := parts[1:]

	// Execute based on command
	switch cmd {
	case "flow":
		return handleFlowCommand(ctx, args)
	case "graph":
		return handleGraphCommand(ctx, args)
	case "l", "log":
		return handleLogCommand(ctx, args)
	case "q", "quit":
		return handleQuitCommand(ctx)
	case "new", "n":
		return handleNewCommand(ctx, false)
	case "new!", "n!":
		return handleNewCommand(ctx, true)
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
	case "d", "debug":
		return handleDebugCommand(ctx, args)
	case "h", "help", "?":
		return handleHelpCommand(ctx)
	case "r", "region":
		return handleRegionCommand(ctx, args)
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
		return handleGodCommand(ctx)
	case "demon":
		return handleDemonCommand(ctx)
	case "blossom":
		return handleBlossomCommand(ctx)
	case "decay":
		return handleDecayCommand(ctx)
	case "cleaner":
		return handleCleanerCommand(ctx)
	case "dust":
		return handleDustCommand(ctx)
	default:
		setCommandError(ctx, fmt.Sprintf("Unknown command: %s", cmd))
		return CommandResult{Continue: true, KeepPaused: false}
	}
}

// handleLogCommand controls the session logger
// Usage: :log | :log on|off | :log <level> | :log scope [spec] | :log stat [ticks]
func handleLogCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) == 0 {
		reportLogState(ctx)
		return CommandResult{Continue: true, KeepPaused: false}
	}

	switch strings.ToLower(args[0]) {
	case "on", "start":
		if vlog.Enabled() {
			ctx.SetStatusMessage("Logging already active", parameter.StatusMessageDefaultTimeout, true)
			break
		}
		// Opens a file under the world lock; a deliberate operator cost
		path, err := vlog.Start()
		if err != nil {
			setCommandError(ctx, "Logging failed: "+err.Error())
			return CommandResult{Continue: true, KeepPaused: false}
		}
		vlog.Info("app", "msg", "logging started", "path", path, "level", vlog.LevelName())
		ctx.SetStatusMessage("Logging to "+path, parameter.StatusMessageDefaultTimeout, true)

	case "off", "stop":
		if !vlog.Enabled() {
			ctx.SetStatusMessage("Logging already stopped", parameter.StatusMessageDefaultTimeout, true)
			break
		}
		vlog.Info("app", "msg", "logging stopped by command")
		vlog.Stop() // drains asynchronously; never blocks the world lock
		ctx.SetStatusMessage("Logging stopped", parameter.StatusMessageDefaultTimeout, true)

	case "scope", "sc":
		if len(args) < 2 {
			reportLogState(ctx)
			break
		}
		s, err := vlog.ParseScopes(strings.Join(args[1:], "+"), vlog.Scopes())
		if err != nil {
			setCommandError(ctx, "Usage: :log scope [+|-]app+fsm+event+dispatch+push+input+stat+rec+lock+tap | afs | all | none")
			return CommandResult{Continue: true, KeepPaused: false}
		}
		vlog.SetScopes(s)
		vlog.Info("app", "msg", "log scope changed", "scope", vlog.ScopeString(s))
		reportLogState(ctx)

	case "level", "lvl":
		if len(args) < 2 || vlog.SetLevelName(args[1]) != nil {
			setCommandError(ctx, "Usage: :log level trace|debug|info|warn|error")
			return CommandResult{Continue: true, KeepPaused: false}
		}
		vlog.Info("app", "msg", "log level changed", "level", vlog.LevelName())
		reportLogState(ctx)

	case "stat", "snap":
		if len(args) < 2 {
			reportLogState(ctx)
			break
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 0 {
			setCommandError(ctx, "Usage: :log stat <ticks> (0 disables)")
			return CommandResult{Continue: true, KeepPaused: false}
		}
		ctx.World.Resources.Status.SetSnapshotInterval(uint64(n))
		vlog.Info("app", "msg", "stat interval changed", "ticks", n)
		reportLogState(ctx)

	case "rec", "recorder":
		return handleLogRec(ctx, args[1:])

	default:
		if err := vlog.SetLevelName(args[0]); err != nil {
			setCommandError(ctx, "Usage: :log [on|off|trace|debug|info|warn|error|scope|level|stat]")
			return CommandResult{Continue: true, KeepPaused: false}
		}
		vlog.Info("app", "msg", "log level changed", "level", vlog.LevelName())
		reportLogState(ctx)
	}

	ctx.SetLastCommand(":log " + strings.Join(args, " "))
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleLogRec controls the flight recorder
// Usage: :log rec | :log rec <ticks> | :log rec flush | :log rec fsm [on|off]
func handleLogRec(ctx *engine.GameContext, args []string) CommandResult {
	reg := ctx.World.Resources.Status
	if len(args) == 0 {
		reportLogState(ctx)
		return CommandResult{Continue: true, KeepPaused: false}
	}

	switch strings.ToLower(args[0]) {
	case "flush", "f":
		rc := reg.Recorder()
		if rc == nil {
			setCommandError(ctx, "Recorder disabled")
			return CommandResult{Continue: true, KeepPaused: false}
		}
		// Command mode holds the world lock; the ring belongs to the tick
		// goroutine, which owns sample. Request, do not flush here.
		rc.Trigger(status.TrigManual)
		ctx.SetStatusMessage("Recorder flush requested", parameter.StatusMessageDefaultTimeout, true)

	case "fsm":
		rc := reg.Recorder()
		if rc == nil {
			setCommandError(ctx, "Recorder disabled")
			return CommandResult{Continue: true, KeepPaused: false}
		}
		desired, explicit, ok := parseToggleArg(args[1:])
		if !ok {
			setCommandError(ctx, "Usage: :log rec fsm [on|off]")
			return CommandResult{Continue: true, KeepPaused: false}
		}
		if !explicit {
			desired = !rc.FSMTrigger()
		}
		rc.SetFSMTrigger(desired)
		ctx.SetStatusMessage("Recorder FSM trigger "+toggleWord(desired),
			parameter.StatusMessageDefaultTimeout, true)

	default:
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 {
			setCommandError(ctx, "Usage: :log rec [<ticks>|flush|fsm]")
			return CommandResult{Continue: true, KeepPaused: false}
		}
		// Relayout discards history; the metric set is already frozen
		reg.EnableRecorder(n)
		vlog.Info("app", "msg", "recorder depth changed", "ticks", n)
		reportLogState(ctx)
	}

	ctx.SetLastCommand(":log rec " + strings.Join(args, " "))
	return CommandResult{Continue: true, KeepPaused: false}
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
		ctx.World.Resources.Status.RecorderDepth()),
		parameter.StatusMessageDefaultTimeout, true)
}

// setCommandError sets an error message in the status message
// This string will be cleared by InputHandler on the next keystroke
func setCommandError(ctx *engine.GameContext, message string) {
	ctx.SetStatusMessage(message, 0, false)
}

// handleQuitCommand exits the game
func handleQuitCommand(ctx *engine.GameContext) CommandResult {
	return CommandResult{Continue: false, KeepPaused: true}
}

// handleNewCommand resets the game state via event; purge also clears operator session state
func handleNewCommand(ctx *engine.GameContext, purge bool) CommandResult {
	ctx.PushEvent(event.EventGameResetRequest, &event.GameResetPayload{Purge: purge})
	cmd := ":new"
	if purge {
		cmd = ":new!"
	}
	ctx.SetLastCommand(cmd)
	ctx.MacroClearFlag.Store(true) // Signal macro reset
	return CommandResult{Continue: true, KeepPaused: true}
}

// handleFreeCommand toggles or sets mouse motion cursor tracking
func handleFreeCommand(ctx *engine.GameContext, args []string) CommandResult {
	return applyToggle(ctx, &ctx.MouseFreeMode, args, "free", "Mouse free mode")
}

// handleAutoCommand toggles or sets auto-fire (main + special)
func handleAutoCommand(ctx *engine.GameContext, args []string) CommandResult {
	return applyToggle(ctx, &ctx.AutoFire, args, "auto", "Auto-fire")
}

// applyToggle resolves a bare toggle or an explicit on|off argument against a
// context flag. The explicit form exists so macros and scripts are idempotent
// now that both flags default to on.
func applyToggle(ctx *engine.GameContext, flag *atomic.Bool, args []string, cmd, label string) CommandResult {
	desired, explicit, ok := parseToggleArg(args)
	if !ok {
		setCommandError(ctx, fmt.Sprintf("Usage: :%s [on|off]", cmd))
		return CommandResult{Continue: true, KeepPaused: false}
	}
	if !explicit {
		desired = !flag.Load()
	}
	flag.Store(desired)

	state := "disabled"
	if desired {
		state = "enabled"
	}
	ctx.SetStatusMessage(label+" "+state, parameter.StatusMessageDefaultTimeout, false)
	ctx.SetLastCommand(fmt.Sprintf(":%s %s", cmd, toggleWord(desired)))
	return CommandResult{Continue: true, KeepPaused: false}
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
		return CommandResult{Continue: true, KeepPaused: false}
	}

	if !ctx.World.HasSystem(args[0]) {
		setCommandError(ctx, fmt.Sprintf("Invalid system: %s", args[0]))
		return CommandResult{Continue: true, KeepPaused: false}
	}

	enabledFlag := false
	switch args[1] {
	case "e", "enable", "enabled":
		enabledFlag = true
	case "d", "disable", "disabled":
		enabledFlag = false
	default:
		setCommandError(ctx, fmt.Sprintf("Invalid state: %s", args[1]))
		return CommandResult{Continue: true, KeepPaused: false}
	}

	if !enabledFlag && !ctx.World.AllowSystemDisable(args[0]) {
		setCommandError(ctx, fmt.Sprintf("%s is required by %s", args[0],
			strings.Join(ctx.World.SystemsRequiring(args[0], engine.DepRequired), ", ")))
		return CommandResult{Continue: true, KeepPaused: false}
	}

	ctx.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
		SystemName: args[0],
		Enabled:    enabledFlag,
	})

	ctx.SetLastCommand(fmt.Sprintf(":system %s %v", args[0], enabledFlag))
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleMouseCommand controls mouse input
func handleMouseCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) == 0 {
		setCommandError(ctx, "Usage: :mouse enable|disable|free")
		return CommandResult{Continue: true, KeepPaused: false}
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
		ctx.SetStatusMessage(msg, parameter.StatusMessageDefaultTimeout, false)

	case "disable":
		msg := "Mouse already disabled"
		if !ctx.MouseDisabled.Load() {
			ctx.MouseDisabled.Store(true)
			msg = "Mouse disabled"
		}
		ctx.SetStatusMessage(msg, parameter.StatusMessageDefaultTimeout, false)

	default:
		setCommandError(ctx, "Usage: :mouse enable|disable|free")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	ctx.SetLastCommand(":mouse " + args[0])
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleEmitCommand emits an event by name with optional TOML payload (debug/testing)
// Usage: :emit EventName
// Usage: :emit EventName { field = value, nested = { x = 1 } }
func handleEmitCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) < 1 {
		setCommandError(ctx, "Usage: :emit <EventName> [{ payload }]")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	name := args[0]

	// Normalize: add "Event" prefix if missing
	if !strings.HasPrefix(name, "Event") {
		name = "Event" + name
	}

	eventType, ok := event.GetEventType(name)
	if !ok {
		setCommandError(ctx, fmt.Sprintf("Unknown event: %s", name))
		return CommandResult{Continue: true, KeepPaused: false}
	}

	// Parse payload if provided
	var payload any
	if len(args) > 1 {
		payloadStr := strings.Join(args[1:], " ")
		var err error
		payload, err = parseEventPayload(eventType, payloadStr)
		if err != nil {
			setCommandError(ctx, fmt.Sprintf("Payload error: %v", err))
			return CommandResult{Continue: true, KeepPaused: false}
		}
	}

	ctx.PushEvent(eventType, payload)
	ctx.SetLastCommand(fmt.Sprintf(":emit %s", strings.Join(args, " ")))

	return CommandResult{Continue: true, KeepPaused: false}
}

// parseEventPayload parses an inline TOML table string into the typed payload struct
// Input: "{ field = value, ... }" or empty string
// Returns: typed payload pointer or nil
func parseEventPayload(et event.EventType, raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	// Get typed payload struct for this event
	payload := event.NewPayloadStruct(et)
	if payload == nil {
		return nil, fmt.Errorf("event does not accept payload")
	}

	// Wrap inline table as TOML key-value for parser compatibility
	wrapped := "_p = " + raw

	p := toml.NewParser([]byte(wrapped))
	parsed, err := p.Parse()
	if err != nil {
		return nil, err
	}

	payloadMap, ok := parsed["_p"]
	if !ok {
		return nil, fmt.Errorf("malformed payload")
	}

	if err := toml.Decode(payloadMap, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

// handleDebugCommand opens the debug overlay, or runs a debug subcommand
func handleDebugCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "s", "save", "snap":
			return handleDebugSaveCommand(ctx)
		case "hud":
			return applyToggle(ctx, &ctx.OverlayHUD, args[1:], "d hud", "Debug HUD")
		case "unpin", "clear":
			ctx.ClearOverlayPins()
			ctx.SetStatusMessage("Pins cleared", parameter.StatusMessageDefaultTimeout, false)
			ctx.SetLastCommand(":d unpin")
			return CommandResult{Continue: true, KeepPaused: false}
		default:
			setCommandError(ctx, "Usage: :debug [save|hud|unpin]")
			return CommandResult{Continue: true, KeepPaused: false}
		}
	}

	ctx.RequestMode(core.ModeOverlay)
	ctx.SetPaused(true)
	ctx.PushLocal(event.EventMetaDebugRequest, nil)
	return CommandResult{Continue: true, KeepPaused: true}
}

// handleDebugSaveCommand writes a standalone status snapshot to its own file
// Command mode still holds the world lock and the pause, so the values are a
// single coherent tick. Opens a second logger: a deliberate operator cost
func handleDebugSaveCommand(ctx *engine.GameContext) CommandResult {
	path, err := vlog.Dump(func(emit func(sub string, args ...any)) {
		ctx.SnapshotContext(emit)
		ctx.World.Resources.Status.Snapshot(emit)
	})
	if err != nil {
		setCommandError(ctx, "Snapshot failed: "+err.Error())
		return CommandResult{Continue: true, KeepPaused: false}
	}

	vlog.Info("app", "msg", "snapshot saved", "path", path)
	ctx.SetStatusMessage("Snapshot saved to "+path, parameter.StatusMessageDefaultTimeout, true)
	ctx.SetLastCommand(":debug save")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleHelpCommand triggers help overlay event
func handleHelpCommand(ctx *engine.GameContext) CommandResult {
	ctx.RequestMode(core.ModeOverlay)
	ctx.SetPaused(true)
	ctx.PushLocal(event.EventMetaHelpRequest, nil)
	return CommandResult{Continue: true, KeepPaused: true}
}

// handleAboutCommand triggers about overlay event
func handleAboutCommand(ctx *engine.GameContext) CommandResult {
	ctx.RequestMode(core.ModeOverlay)
	ctx.SetPaused(true)
	ctx.PushLocal(event.EventMetaAboutRequest, nil)
	return CommandResult{Continue: true, KeepPaused: true}
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
	ctx.SetStatusMessage(msg, parameter.StatusMessageDefaultTimeout, true)
	ctx.SetLastCommand(":content")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleEnergyCommand sets the energy to a specified value
func handleEnergyCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for energy")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid arguments for energy")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	ctx.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{
		Entity: ctx.World.Resources.Player.Entity,
		Value:  value,
	})

	ctx.SetLastCommand(fmt.Sprintf(":energy %d", value))
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleHeatCommand sets the heat to a specified value
func handleHeatCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 1 {
		setCommandError(ctx, "Usage: :heat <0-100>")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid number format")
		return CommandResult{Continue: true, KeepPaused: false}
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
	ctx.SetLastCommand(fmt.Sprintf(":heat %d", value))

	return CommandResult{Continue: true, KeepPaused: false}
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

	ctx.SetLastCommand(":boost")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleGodCommand sets heat to max and energy to high value
func handleGodCommand(ctx *engine.GameContext) CommandResult {
	player := ctx.World.Resources.Player.Entity
	ctx.PushLocal(event.EventHeatSetRequest, &event.HeatSetRequestPayload{Entity: player, Value: parameter.HeatMax})
	ctx.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: player, Value: parameter.GodEnergyAmount})
	ctx.PushLocal(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Entity: player, Weapon: component.WeaponRod})
	ctx.PushLocal(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Entity: player, Weapon: component.WeaponLauncher})
	ctx.PushLocal(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Entity: player, Weapon: component.WeaponDisruptor})
	ctx.SetLastCommand(":god")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDemonCommand sets heat to max and energy to high value
func handleDemonCommand(ctx *engine.GameContext) CommandResult {
	player := ctx.World.Resources.Player.Entity
	ctx.PushLocal(event.EventHeatSetRequest, &event.HeatSetRequestPayload{Entity: player, Value: parameter.HeatMax})
	ctx.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: player, Value: -parameter.GodEnergyAmount})
	ctx.PushLocal(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Entity: player, Weapon: component.WeaponRod})
	ctx.PushLocal(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Entity: player, Weapon: component.WeaponLauncher})
	ctx.PushLocal(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Entity: player, Weapon: component.WeaponDisruptor})
	ctx.SetLastCommand(":demon")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleBlossomCommand triggers a blossom wave
func handleBlossomCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushLocal(event.EventBlossomWave, nil)
	ctx.SetLastCommand(":blossom")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDecayCommand triggers a decay wave
func handleDecayCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushLocal(event.EventDecayWave, nil)
	ctx.SetLastCommand(":decay")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleCleanerCommand triggers sweeping cleaners
func handleCleanerCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEventDomain(event.EventCleanerSweepingRequest, &event.CleanerSweepingRequestPayload{
		Entity: ctx.World.Resources.Player.Entity,
	}, core.DomainPlayer)
	ctx.SetLastCommand(":cleaner")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDustCommand triggers glyph to dust transform
func handleDustCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushLocal(event.EventDustAllRequest, nil)
	ctx.SetLastCommand(":dust")
	return CommandResult{Continue: true, KeepPaused: false}
}

// === DEBUG ===

func handleFlowCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) == 0 {
		ctx.PushLocal(event.EventDebugFlowToggle, nil)
	} else {
		groupID, err := strconv.Atoi(args[0])
		if err != nil || groupID < 0 || groupID >= component.MaxTargetGroups {
			setCommandError(ctx, fmt.Sprintf("Invalid group ID: %s (0-%d)", args[0], component.MaxTargetGroups-1))
			return CommandResult{Continue: true, KeepPaused: false}
		}
		ctx.PushLocal(event.EventDebugFlowToggle, &event.DebugFlowGroupPayload{
			GroupID: uint8(groupID),
		})
	}
	return CommandResult{Continue: true, KeepPaused: false}
}

func handleGraphCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) == 0 {
		ctx.PushLocal(event.EventDebugGraphToggle, nil)
	} else {
		groupID, err := strconv.Atoi(args[0])
		if err != nil || groupID < 0 || groupID >= component.MaxTargetGroups {
			setCommandError(ctx, fmt.Sprintf("Invalid group ID: %s (0-%d)", args[0], component.MaxTargetGroups-1))
			return CommandResult{Continue: true, KeepPaused: false}
		}
		ctx.PushLocal(event.EventDebugGraphToggle, &event.DebugFlowGroupPayload{
			GroupID: uint8(groupID),
		})
	}
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleSpeedCommand reports or sets the simulation time scale
// Usage: :speed | :speed <1/8|1/4|1/2|1|2|4|8> | :speed +|- | :speed reset
func handleSpeedCommand(ctx *engine.GameContext, args []string) CommandResult {
	cur := ctx.TimeCtl.Scale()
	if len(args) == 0 {
		ctx.SetStatusMessage("Speed "+cur.String()+"x", parameter.StatusMessageDefaultTimeout, true)
		return CommandResult{Continue: true, KeepPaused: false}
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
			return CommandResult{Continue: true, KeepPaused: false}
		}
		next = s
	}

	ctx.PushLocal(event.EventGameSpeedRequest, &event.GameSpeedPayload{Num: next.Num, Den: next.Den})
	ctx.SetStatusMessage("Speed "+next.String()+"x", parameter.StatusMessageDefaultTimeout, true)
	ctx.SetLastCommand(":speed " + next.String())
	return CommandResult{Continue: true, KeepPaused: false}
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
	ctx.SetLastCommand(":step " + strings.Join(args, " "))
	return CommandResult{Continue: true, KeepPaused: false}
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
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleRegionCommand controls FSM regions for debugging.
// Each invocation performs one primitive operation; entering a region that the
// escalation chain would reach is pause-then-spawn, issued as two commands.
func handleRegionCommand(ctx *engine.GameContext, args []string) CommandResult {
	const usage = "Usage: :region list | spawn <name> <state> | pause|resume|terminate <name>"

	res := CommandResult{Continue: true, KeepPaused: false}
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
	ctx.SetLastCommand(":region " + strings.Join(args, " "))
	return res
}

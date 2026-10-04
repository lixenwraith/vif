package mode

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/vlog"
)

type configOption struct {
	key, label, description string
	command                 string
	choices                 []string
	steps                   []uint64
	read                    func(*engine.GameContext) string
	change                  func(*engine.GameContext, int)
	activate                func(*Router)
	disabled                func(*engine.GameContext) string
}

type configPage struct {
	key, label, description string
	options                 []configOption
	extra                   func(*engine.GameContext) []configOption
}

var configPages = []configPage{
	{key: "bots", label: "Bots", description: "Add bots or remove your bots. Hosts can also remove other players and bots on Multiplayer.",
		options: []configOption{configFormOption("add")}, extra: botConfigOptions},
	{key: "multiplayer", label: "Multiplayer", description: "Host this run, join a session or request one from an allocator.", options: []configOption{
		configFormOption("host"), configFormOption("join"), configFormOption("request"),
	}, extra: sessionConfigOptions},
	{key: "audio", label: "Audio", description: "Music, sound effects and effects volume.", options: []configOption{
		{key: "effects", label: "Sound effects", description: "Game sound effects; independent of music.",
			read: audioChannelValue(parameter.AudioChanEffects), change: toggleAudioChannel(parameter.AudioChanEffects), disabled: audioUnavailable},
		{key: "music", label: "Music", description: "Procedural music; independent of sound effects.",
			read: audioChannelValue(parameter.AudioChanMusic), change: toggleAudioChannel(parameter.AudioChanMusic), disabled: audioUnavailable},
		{key: "volume", label: "Effects volume", description: "Sound effect volume, in steps of 5%. Muting keeps this level.",
			read: func(ctx *engine.GameContext) string {
				return fmt.Sprintf("%.0f%%", ctx.World.Resources.EffectsVolume()*100)
			},
			change: func(ctx *engine.GameContext, d int) {
				r := ctx.World.Resources
				r.SetEffectsVolume((math.Round(r.EffectsVolume()*100) + float64(d*5)) / 100)
			}, disabled: audioUnavailable},
	}},
	{key: "controls", label: "Controls", description: "Auto-fire and mouse behavior.", options: []configOption{
		{key: "auto", label: "Auto-fire", description: "off: manual fire; main: main weapon only; on: both weapons.",
			command: "auto", choices: []string{"off", "main", "on"},
			read: func(ctx *engine.GameContext) string { return [...]string{"off", "main", "on"}[ctx.AutoFire.Load()] }},
		{key: "mouse", label: "Mouse input", description: "Enable or ignore mouse movement and buttons.",
			command: "mouse", choices: []string{"disable", "enable"},
			read: func(ctx *engine.GameContext) string {
				if ctx.MouseDisabled.Load() {
					return "disable"
				}
				return "enable"
			}},
		{key: "free", label: "Follow mouse", description: "Move the cursor with the pointer, without holding a button.",
			command: "free", choices: []string{"off", "on"},
			read: func(ctx *engine.GameContext) string { return toggleWord(ctx.MouseFreeMode.Load()) }},
	}},
	{key: "display", label: "Display", description: "Telemetry and navigation overlays.", options: []configOption{
		{key: "hud", label: "Telemetry HUD", description: "Show pinned telemetry cards over the game. Pin cards in :t with Enter.",
			command: "hud", choices: []string{"off", "on"},
			read: func(ctx *engine.GameContext) string { return toggleWord(ctx.OverlayHUD.Load()) }},
		{key: "flow", label: "Flow field", description: "Show the navigation flow field for the selected target group (:flow <group>).",
			command: "flow", read: func(ctx *engine.GameContext) string { return toggleWord(ctx.NavigationDebug.ShowFlow) }},
		{key: "graph", label: "Route graph", description: "Show composite navigation routes (:graph <group> selects a target group).",
			command: "graph", read: func(ctx *engine.GameContext) string { return toggleWord(ctx.NavigationDebug.ShowComposite) }},
	}},
	{key: "simulation", label: "Simulation", description: "Solo simulation pacing; network sessions keep their shared rate.", options: []configOption{
		{key: "speed", label: "Game speed", description: "Left slows down; Right or Enter speeds up. 1x is real time; range 1/8x to 8x.",
			command: "speed", read: func(ctx *engine.GameContext) string { return ctx.TimeCtl.Scale().String() + "x" },
			change: func(ctx *engine.GameContext, d int) {
				handleSpeedCommand(ctx, []string{engine.ScaleStep(ctx.TimeCtl.Scale(), d).String()})
			}},
	}},
	{key: "diagnostics", label: "Diagnostics", description: "Logging, snapshots, flight recorder and profiling.", options: []configOption{
		{key: "logging", label: "File logging", description: "Start or stop writing diagnostic logs to the configured log directory.",
			command: "log", choices: []string{"off", "on"}, read: func(*engine.GameContext) string { return toggleWord(vlog.Enabled()) }, disabled: loggingUnavailable},
		{key: "level", label: "Log level", description: "Minimum severity to record. Trace and debug can produce large log files.",
			command: "log level", choices: []string{"trace", "debug", "info", "warn", "error"},
			read: func(*engine.GameContext) string { return strings.ToLower(vlog.LevelName()) }, disabled: loggingUnavailable},
		{key: "scope", label: "Log scope preset", description: "Choose all, app+fsm+stat, or none. :log scope allows individual categories.",
			command: "log scope", choices: []string{"all", "app+fsm+stat", "none"},
			read: func(*engine.GameContext) string { return vlog.ScopeString(vlog.Scopes()) }, disabled: loggingUnavailable},
		{key: "stat", label: "Snapshot ticks", description: "Ticks between log snapshots; 0 disables them. :log stat accepts any interval.",
			command: "log stat", steps: []uint64{0, 60, 200, 600, 3600},
			read: func(ctx *engine.GameContext) string {
				return strconv.FormatUint(ctx.World.Resources.Status.SnapshotInterval(), 10)
			}, disabled: loggingUnavailable},
		{key: "rec", label: "Recorder ticks", description: "Flight recorder history depth; 0 disables it. Changing depth discards existing history.",
			command: "log rec", steps: []uint64{0, 120, 600, 1800, 3600},
			read: func(ctx *engine.GameContext) string { return strconv.Itoa(ctx.World.Resources.Status.RecorderDepth()) }, disabled: loggingUnavailable},
		{key: "fsm", label: "Record FSM transitions", description: "Flush flight recorder history when an FSM transition occurs.",
			command: "log rec fsm", choices: []string{"off", "on"},
			read: func(ctx *engine.GameContext) string {
				rc := ctx.World.Resources.Status.Recorder()
				return toggleWord(rc != nil && rc.FSMTrigger())
			},
			disabled: func(ctx *engine.GameContext) string {
				if why := loggingUnavailable(ctx); why != "" {
					return why
				}
				if ctx.World.Resources.Status.Recorder() == nil {
					return "Enable the flight recorder first"
				}
				return ""
			}},
		{key: "prof", label: "Runtime profiler", description: "Collect module timings. Enabling also pins profiler cards to the telemetry HUD.",
			command: "debug prof", choices: []string{"off", "on"},
			read: func(ctx *engine.GameContext) string { return toggleWord(ctx.World.Resources.Prof.Profiling()) }},
		{key: "journal", label: "Replay journal", description: "Enter records a replay journal from here. A solo run restarts on its own world and a guest rejoins, so local glyphs and effects start fresh, as on a join.",
			command: "journal", read: journalValue, disabled: journalUnavailable,
			activate: func(r *Router) {
				if err := r.ctx.SessionCtl.StartJournal(); err != nil {
					setCommandError(r.ctx, "Journal: "+err.Error())
					return
				}
				r.closeOverlay()
			}},
	}},
	{key: "startup", label: "Startup settings", description: "Settings that require restarting, and where to configure them.", options: []configOption{
		{key: "files", label: "Paths and scenario", description: "Use vif.toml [paths] or -config-dir, -s, -f, -k. :new <scenario> starts another scenario.", read: startupValue},
		{key: "backend", label: "Audio device / buffer", description: "Use -audio-backend and vif.toml [audio].buffer_ms. The audio device opens at startup.", read: startupValue},
		{key: "color", label: "Colour depth", description: "Use -color auto|256|true. Rendering resources are selected at startup.", read: startupValue},
		{key: "keymap", label: "Key bindings", description: "Edit input/keymap.toml in your config root, or use -k. config_menu is the action bound to Ctrl+G.", read: startupValue},
		{key: "session", label: "Identity", description: "Session identity and seed are startup options. Use the Multiplayer and Bots pages for live session actions, Diagnostics to start a replay journal.", read: startupValue},
	}},
}

func startupValue(*engine.GameContext) string { return "info" }

func journalValue(ctx *engine.GameContext) string {
	if ctx.SessionCtl != nil && ctx.SessionCtl.JournalPath() != "" {
		return "on"
	}
	return "Enter"
}

func journalUnavailable(ctx *engine.GameContext) string {
	if ctx.SessionCtl == nil {
		return "This runtime cannot start a journal"
	}
	if err := ctx.SessionCtl.JournalError(); err != nil {
		return "Unavailable: " + err.Error()
	}
	return ""
}

func audioUnavailable(ctx *engine.GameContext) string {
	reg := ctx.World.Resources.Status
	if reg.Ints.Get("audio.mask").Load() < 0 || reg.Bools.Get("audio.silent").Load() {
		return "Audio unavailable in this run"
	}
	return ""
}

func loggingUnavailable(*engine.GameContext) string {
	if vlog.LevelName() == "OFF" {
		return "Logging unavailable in this build"
	}
	return ""
}

func audioChannelValue(mask uint8) func(*engine.GameContext) string {
	return func(ctx *engine.GameContext) string {
		current := ctx.World.Resources.Status.Ints.Get("audio.mask").Load()
		return toggleWord(current >= 0 && current&int64(mask) != 0)
	}
}

func toggleAudioChannel(mask uint8) func(*engine.GameContext, int) {
	return func(ctx *engine.GameContext, _ int) {
		ctx.PushLocalOrigin(event.EventSoundMuteToggle, &event.SoundMuteTogglePayload{Mode: event.MuteToggle, Mask: mask}, event.OriginDevice)
	}
}

func (o configOption) unavailable(ctx *engine.GameContext) string {
	if o.command != "" {
		if why := commandUnavailable(ctx, strings.Fields(o.command)[0]); why != "" {
			return why
		}
	}
	if o.disabled != nil {
		return o.disabled(ctx)
	}
	if o.command == "" && o.change == nil && o.activate == nil {
		return "Read only"
	}
	return ""
}

func (p configPage) entries(ctx *engine.GameContext) []configOption {
	options := slices.Clone(p.options)
	if p.extra != nil {
		options = append(options, p.extra(ctx)...)
	}
	return options
}

func handleConfigCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 0 {
		setCommandError(ctx, "Usage: :g or :config")
		return CommandResult{Continue: true}
	}
	ctx.RequestMode(core.ModeOverlay)
	ctx.SetPaused(true)
	ctx.SetOverlayContent(nil)
	showConfigMenu(ctx, "")
	return CommandResult{Continue: true, KeepPaused: true}
}

func configMenu(ctx *engine.GameContext) *core.OverlayMenu {
	if c := ctx.GetOverlayContent(); c != nil && c.Layout == core.OverlayLayoutMenu {
		return c.Menu
	}
	return nil
}

func showConfigMenu(ctx *engine.GameContext, page string) {
	menu := &core.OverlayMenu{Page: page}
	title := "Configuration (this run)"
	if page == "" {
		for _, p := range configPages {
			menu.Rows = append(menu.Rows, core.OverlayMenuRow{Key: p.key, Label: p.label, Value: ">", Description: p.description})
		}
	} else {
		menu.Rows = append(menu.Rows, core.OverlayMenuRow{Key: "..", Label: "Back", Value: "<", Description: "Return to configuration categories."})
		for _, p := range configPages {
			if p.key != page {
				continue
			}
			title = "Configuration / " + p.label
			for _, o := range p.entries(ctx) {
				menu.Rows = append(menu.Rows, core.OverlayMenuRow{Key: o.key, Label: o.label, Value: o.read(ctx), Description: o.description, Disabled: o.unavailable(ctx)})
			}
			break
		}
	}
	old := configMenu(ctx)
	if old != nil && old.Form == nil && old.Page == page && slices.Equal(old.Rows, menu.Rows) {
		return
	}
	scroll := ctx.GetOverlayScroll()
	ctx.SetOverlayContent(&core.OverlayContent{Title: title, Layout: core.OverlayLayoutMenu, Menu: menu})
	if old != nil && old.Page == page {
		ctx.SetOverlayScroll(scroll)
	}
}

// RefreshConfigMenu snapshots settled settings under the world lock before rendering.
func (r *Router) RefreshConfigMenu() {
	if m := configMenu(r.ctx); m != nil {
		if m.Form == nil {
			showConfigMenu(r.ctx, m.Page)
		}
	} else {
		r.configForm = nil
	}
}

func (r *Router) handleConfigMenu() bool {
	if configMenu(r.ctx) != nil {
		return r.closeOverlay()
	}
	r.mouseLeftHeld, r.mouseRightHeld = false, false
	r.ctx.SetCommandText("")
	r.ctx.SetSearchText("")
	r.ctx.SetCommandCursorPos(0)
	r.resetCommandHistoryBrowse()
	handleConfigCommand(r.ctx, nil)
	r.machine.SetMode(input.ModeOverlay)
	return true
}

func (r *Router) configMenuBack() bool {
	m := configMenu(r.ctx)
	if r.configForm != nil {
		key := r.configForm.option.key
		r.configForm = nil
		showConfigMenu(r.ctx, m.Page)
		r.ctx.SetOverlaySelection(key)
		r.machine.SetMode(input.ModeOverlay)
		return true
	}
	if m.Page == "" {
		return r.closeOverlay()
	}
	page := m.Page
	showConfigMenu(r.ctx, "")
	r.ctx.SetOverlaySelection(page)
	return true
}

func (r *Router) moveConfigMenu(motion input.MotionOp, pageDelta int) bool {
	m := configMenu(r.ctx)
	i := slices.IndexFunc(m.Rows, func(row core.OverlayMenuRow) bool { return row.Key == r.ctx.GetOverlaySelection() })
	i = max(i, 0)
	switch motion {
	case input.MotionLeft:
		return r.changeConfigMenu(-1, false)
	case input.MotionRight:
		return r.changeConfigMenu(1, false)
	case input.MotionUp:
		i--
	case input.MotionDown:
		i++
	case input.MotionScreenTop:
		i = 0
	case input.MotionScreenBottom:
		i = len(m.Rows) - 1
	}
	i += pageDelta * max(r.ctx.OverlayGeometry().ContentH-parameter.OverlayMenuDetailRows, 1)
	i = tui.ClampCursor(i, len(m.Rows))
	if i >= 0 {
		r.ctx.SetOverlaySelection(m.Rows[i].Key)
	}
	return true
}

func (r *Router) changeConfigMenu(direction int, activate bool) bool {
	m := configMenu(r.ctx)
	key := r.ctx.GetOverlaySelection()
	if key == ".." {
		return r.configMenuBack()
	}
	if m.Page == "" {
		if direction < 0 {
			return true
		}
		showConfigMenu(r.ctx, key)
		if menu := configMenu(r.ctx); len(menu.Rows) > 1 {
			r.ctx.SetOverlaySelection(menu.Rows[1].Key)
		}
		return true
	}
	for _, p := range configPages {
		if p.key != m.Page {
			continue
		}
		for _, o := range p.entries(r.ctx) {
			if o.key != key {
				continue
			}
			if o.activate != nil && !activate {
				return true
			}
			if why := o.unavailable(r.ctx); why != "" {
				setCommandError(r.ctx, why)
				return true
			}
			r.ctx.WithOrigin(event.OriginCommand, func() {
				if o.activate != nil {
					o.activate(r)
					return
				}
				if o.change != nil {
					o.change(r.ctx, direction)
					return
				}
				command := o.command
				if len(o.steps) > 0 {
					current, _ := strconv.ParseUint(o.read(r.ctx), 10, 64)
					next := current
					for _, n := range o.steps {
						if direction > 0 && n > current {
							next = n
							break
						}
						if direction < 0 && n < current {
							next = n
						}
					}
					// A key at the limit must not discard recorder history.
					if next == current {
						return
					}
					command += " " + strconv.FormatUint(next, 10)
				} else if len(o.choices) > 0 {
					i := slices.Index(o.choices, o.read(r.ctx))
					if i < 0 && direction < 0 {
						i = 0
					}
					i = (i + direction + len(o.choices)) % len(o.choices)
					command += " " + o.choices[i]
				}
				ExecuteCommand(r.ctx, command)
			})
			r.RefreshConfigMenu()
			return true
		}
	}
	return true
}

package mode

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

type configFormField struct{ label, value, help string }

type configFormSpec struct {
	label, description, command string
	fields                      []configFormField
	unavailable                 func(engine.SessionController) error
	submit                      func(*engine.GameContext, *tui.FormState) error
}

type configForm struct {
	option configOption
	spec   configFormSpec
	state  *tui.FormState
	page   string
	err    string
}

var configForms = map[string]configFormSpec{
	"add": {
		label: "Add bots", description: "Seat local bots in this session. A solo run opens a loopback host; host publicly first to invite remote players.", command: "bot",
		fields: []configFormField{
			{"Count", "1", "Number of bots to add. Each bot occupies a player slot."},
			{"Policy", "default", "Policy graph name or path; blank uses default. Bots belong to this run and leave with it."},
		},
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			n, err := strconv.Atoi(strings.TrimSpace(f.Value(0)))
			if err != nil || n < 1 || n > parameter.MaxPlayers {
				return fmt.Errorf("bot count must be 1..%d", parameter.MaxPlayers)
			}
			if err := ctx.SessionCtl.AddBot(fmt.Sprintf("%d:%s", n, strings.TrimSpace(f.Value(1)))); err != nil {
				return err
			}
			ctx.SetStatusMessage("Bots joining; open Configuration / Bots to remove them", parameter.StatusMessageDefaultTimeout, false)
			return nil
		},
	},
	"host": {
		label: "Host this run", description: "Open this running game to other players at a listening address.", command: "host",
		fields: []configFormField{
			{"Address", ":4242", "Listen address, e.g. :4242, 127.0.0.1:4242 or ws://:4242. Share a reachable address with guests."},
			{"Authority", "", "Blank keeps the startup policy. host ends the session when you leave; migrate allows a successor."},
		},
		unavailable: engine.SessionController.HostError,
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			return ctx.SessionCtl.BeginHosting(strings.TrimSpace(f.Value(0)), strings.TrimSpace(f.Value(1)))
		},
	},
	"join": {
		label: "Join a session", description: "Join an existing session; a failed connection keeps this run intact.", command: "join",
		fields: []configFormField{
			{"Target", "", "Session address or link: host:port[/name], vif://, tcp:// or ws(s)://. An http(s):// site requests a session with server defaults."},
		},
		unavailable: engine.SessionController.JoinError,
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			if err := ctx.SessionCtl.Join(strings.TrimSpace(f.Value(0))); err != nil {
				return err
			}
			ctx.MacroClearFlag.Store(true)
			return nil
		},
	},
	"scenario": {
		label: "Change scenario", description: "Start another scenario. In a session only the host can, and its participants rejoin it.", command: "new",
		fields: []configFormField{
			{"Scenario", "", "Scenario name from the config roots, as -s takes it. The current one's name starts a new game."},
		},
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			name := strings.TrimSpace(f.Value(0))
			if name == "" {
				return errors.New("name a scenario")
			}
			changeScenario(ctx, name)
			return nil
		},
	},
	"request": {
		label: "Request a session", description: "Ask a vif-allocator site to create a session, then join it. Availability and quotas are set by the server.", command: "join",
		fields: []configFormField{
			{"Site", "https://lixen.com", "Allocator site's http(s):// origin. Enter creates a session and joins the returned link."},
			{"Players", "0", "Total player slots, including you. 0 uses the server default; the server may impose a lower limit."},
			{"Scenario", "", "Scenario name on the server; blank uses its default. Local scenario files are not uploaded."},
		},
		unavailable: engine.SessionController.JoinError,
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			n, err := strconv.Atoi(strings.TrimSpace(f.Value(1)))
			if err != nil {
				return fmt.Errorf("players must be a number (0 uses the server default)")
			}
			if err := ctx.SessionCtl.RequestSession(strings.TrimSpace(f.Value(0)), n, strings.TrimSpace(f.Value(2))); err != nil {
				return err
			}
			ctx.MacroClearFlag.Store(true)
			return nil
		},
	},
}

func configFormOption(key string) configOption {
	spec := configForms[key]
	o := configOption{key: key, label: spec.label, description: spec.description, command: spec.command,
		read: func(*engine.GameContext) string { return "Enter" },
		disabled: func(ctx *engine.GameContext) string {
			if ctx.SessionCtl == nil {
				return "Session controls unavailable in this run"
			}
			if spec.unavailable != nil {
				if err := spec.unavailable(ctx.SessionCtl); err != nil {
					return err.Error()
				}
			}
			return ""
		}}
	o.activate = func(r *Router) {
		state := tui.NewFormState()
		for _, f := range spec.fields {
			state.Fields = append(state.Fields, tui.FormField{Label: f.label, State: tui.NewTextFieldState(f.value)})
		}
		r.configForm = &configForm{option: o, spec: spec, state: state, page: configMenu(r.ctx).Page}
		r.machine.SetMode(input.ModeSearch)
		r.publishConfigForm()
	}
	return o
}

func botConfigOptions(ctx *engine.GameContext) []configOption {
	if ctx.SessionCtl == nil {
		return nil
	}
	bots := ctx.SessionCtl.Bots()
	if len(bots) == 0 {
		return []configOption{{key: "empty", label: "No bots held by this run", description: "Add bots above. Bots held by another player cannot be removed here.", read: startupValue}}
	}
	options := make([]configOption, 0, len(bots))
	for _, b := range bots {
		key, label, value, reason := fmt.Sprintf("drop/%d", b.ID), fmt.Sprintf("Remove %X: %s", b.Slot, b.Graph), "Enter", ""
		description := "Enter removes this run's bot from the session. The hexadecimal slot matches its cursor label."
		if b.Joining {
			label = "Cancel joining: " + b.Graph
			description = "This bot is joining. Enter cancels it; leaving completes when its pending admission finishes."
		}
		if b.Stopping {
			value, reason = "leaving", "This bot is leaving"
		}
		options = append(options, configOption{key: key, label: label, command: "bot", description: description,
			read: func(*engine.GameContext) string { return value }, disabled: func(*engine.GameContext) string { return reason },
			activate: func(r *Router) {
				if err := r.ctx.SessionCtl.RemoveBot(b.ID); err != nil {
					setCommandError(r.ctx, err.Error())
					return
				}
				r.closeOverlay()
				r.ctx.SetStatusMessage("Bot "+b.Graph+" leaving", parameter.StatusMessageDefaultTimeout, false)
			}})
	}
	return options
}

func sessionConfigOptions(ctx *engine.GameContext) []configOption {
	if ctx.SessionCtl == nil {
		return nil
	}
	return append([]configOption{{key: "status", label: "Current session", description: ctx.SessionCtl.SessionSummary(), read: startupValue}}, participantConfigOptions(ctx)...)
}

func participantConfigOptions(ctx *engine.GameContext) []configOption {
	if ctx.SessionCtl == nil {
		return nil
	}
	var out []configOption
	for _, p := range ctx.SessionCtl.Participants() {
		if p.Local {
			continue
		}
		kind, description := "player", "Enter removes this player and all bots it brought."
		if p.Holder != 0 {
			kind, description = "bot", "Enter removes this bot only; its holder and other bots stay."
		}
		out = append(out, configOption{key: fmt.Sprintf("player/%d", p.Entity),
			label: fmt.Sprintf("Drop %X: %s", p.Slot, kind), description: description,
			command: "player", read: func(*engine.GameContext) string { return "Enter" },
			disabled: func(ctx *engine.GameContext) string {
				if !ctx.World.IsSessionCoordinator() {
					return "Only the host can remove other participants"
				}
				return ""
			}, activate: func(r *Router) {
				if err := r.ctx.SessionCtl.RemovePlayer(p.Entity); err != nil {
					setCommandError(r.ctx, err.Error())
					return
				}
				r.closeOverlay()
				r.ctx.SetStatusMessage(fmt.Sprintf("Slot %X leaving", p.Slot), parameter.StatusMessageDefaultTimeout, true)
			}})
	}
	return out
}

func (r *Router) publishConfigForm() {
	f := r.configForm
	view := &core.OverlayForm{Focus: f.state.Focus, Help: f.spec.fields[f.state.Focus].help, Error: f.err}
	for _, field := range f.state.Fields {
		view.Fields = append(view.Fields, core.OverlayFormField{Label: field.Label, Value: field.State.Value(), Cursor: field.State.Cursor})
	}
	r.ctx.SetOverlayContent(&core.OverlayContent{Title: "Configuration / " + f.spec.label, Layout: core.OverlayLayoutMenu,
		Menu: &core.OverlayMenu{Page: f.page, Form: view}})
}

func (r *Router) handleConfigForm(intent *input.Intent) bool {
	f := r.configForm
	field := f.state.CurrentField()
	switch intent.Type {
	case input.IntentTextChar:
		field.Insert(intent.Char)
	case input.IntentTextBackspace:
		field.DeleteBackward()
	case input.IntentInsertDeleteCurrent:
		field.DeleteForward()
	case input.IntentTextNav:
		switch intent.Motion {
		case input.MotionUp, input.MotionHalfPageUp:
			f.state.FocusPrev()
		case input.MotionDown, input.MotionHalfPageDown:
			f.state.FocusNext()
		case input.MotionLeft:
			field.MoveLeft()
		case input.MotionRight:
			field.MoveRight()
		case input.MotionLineStart:
			field.MoveToStart()
		case input.MotionLineEnd:
			field.MoveToEnd()
		}
	case input.IntentTextConfirm:
		if why := f.option.unavailable(r.ctx); why != "" {
			f.err = why
		} else {
			var err error
			r.ctx.WithOrigin(event.OriginCommand, func() { err = f.spec.submit(r.ctx, f.state) })
			if err != nil {
				f.err = err.Error()
			} else {
				r.closeOverlay()
				return true
			}
		}
	default:
		return false
	}
	r.publishConfigForm()
	return true
}

package mode

import (
	"cmp"
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

// A field's input picks the keyboard a native host raises for it
const (
	inputText   = "text"
	inputNumber = "number"
	inputURI    = "uri"
)

type configFormField struct{ label, value, help, input string }

type configFormSpec struct {
	label, description, command string
	fields                      []configFormField
	unavailable                 func(engine.SessionController) error
	submit                      func(*engine.GameContext, *tui.FormState) error
}

type configForm struct {
	node   configNode
	spec   configFormSpec
	state  *tui.FormState
	page   string
	row    string // The node's id, which keys the menu row it opened from, a search's included
	inline bool   // One field, typed in on its row rather than in a window
	err    string
}

var configForms = map[string]configFormSpec{
	"add": {
		label: "Add bots", description: "Seat local bots in this session. A solo run opens a session only they can join; host first to invite remote players.", command: "bot",
		fields: []configFormField{
			{"Count", "1", "Number of bots to add. Each bot occupies a player slot.", inputNumber},
			{"Policy", "default", "Policy graph name or path; blank uses default. Bots belong to this run and leave with it.", inputText},
		},
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			n, err := strconv.Atoi(strings.TrimSpace(f.Value(0)))
			if err != nil || n < 1 || n > parameter.MaxPlayers {
				return fmt.Errorf("bot count must be 1..%d", parameter.MaxPlayers)
			}
			if err := ctx.SessionCtl.AddBot(fmt.Sprintf("%d:%s", n, strings.TrimSpace(f.Value(1)))); err != nil {
				return err
			}
			ctx.SetStatusMessage("Bots joining; Menu / Multiplayer removes them", parameter.StatusNotice)
			return nil
		},
	},
	"host": {
		label: "Host this game", description: "Open this running game to other players at a listening address.", command: "host",
		fields: []configFormField{
			{"Address", ":4242", "Listen address, e.g. :4242, 127.0.0.1:4242 or ws://:4242. Share a reachable address with guests.", inputURI},
			{"Authority", "", "Blank keeps this run's policy, host unless it asked for migrate. host ends the session when you leave; migrate moves it to a guest that turned Take over as host on, whose address the session shares.", inputText},
		},
		unavailable: engine.SessionController.HostError,
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			return ctx.SessionCtl.BeginHosting(strings.TrimSpace(f.Value(0)), strings.TrimSpace(f.Value(1)))
		},
	},
	"join": {
		label: "Join a session", description: "Type a session address or link here: host:port[/name], vif://, tcp:// or ws(s)://; an http(s):// site requests a session with server defaults. A failed connection keeps this game intact.", command: "join",
		fields:      []configFormField{{"Target", "", "", inputURI}},
		unavailable: engine.SessionController.JoinError,
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			if err := ctx.SessionCtl.Join(strings.TrimSpace(f.Value(0))); err != nil {
				return err
			}
			ctx.MacroClearFlag.Store(true)
			return nil
		},
	},
	"spectate": {
		label: "Spectate a session", description: "Type a session address or link, as Join takes one. This run only watches the session: it takes no place there, and its own game, bots and journal end. A failed connection keeps this game intact.", command: "spectate",
		fields:      []configFormField{{"Target", "", "", inputURI}},
		unavailable: engine.SessionController.JoinError,
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			if err := ctx.SessionCtl.Spectate(strings.TrimSpace(f.Value(0)), 0); err != nil {
				return err
			}
			ctx.MacroClearFlag.Store(true)
			return nil
		},
	},
	"scenario": {
		label: "Other scenario", description: "Type a scenario the list does not hold, by name or path as the launch flag -s takes it: a scenario.toml or a scenario directory. In a session only the host can, and its participants rejoin it.", command: "new",
		fields: []configFormField{{"Scenario", "", "", inputText}},
		submit: func(ctx *engine.GameContext, f *tui.FormState) error {
			name := strings.TrimSpace(f.Value(0))
			if name == "" {
				return errors.New("name a scenario")
			}
			return startScenario(ctx, name)
		},
	},
	"request": {
		label: "Request a session", description: "Ask a vif-router site to create a session, then join it. Availability and quotas are set by the server.", command: "join",
		fields: []configFormField{
			{"Site", "https://lixen.com", "Allocator site's http(s):// origin. Submitting creates a session and joins the returned link.", inputURI},
			{"Players", "0", "Total player slots, including you. 0 uses the server default; the server may impose a lower limit.", inputNumber},
			{"Scenario", "", "Scenario name on the server; blank uses its default. Local scenario files are not uploaded.", inputText},
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

// sessionRefusal is why a session row is disabled: a run without session controls,
// or what check says the run refuses now, as its command would.
func sessionRefusal(check func(engine.SessionController) error) func(*engine.GameContext) string {
	return func(ctx *engine.GameContext) string {
		if ctx.SessionCtl == nil {
			return sessionsUnavailable
		}
		if check != nil {
			if err := check(ctx.SessionCtl); err != nil {
				return sentence(err)
			}
		}
		return ""
	}
}

// formRow is the row a form opens from: a form of one field is a text box typed in
// on the row, whose value is the field's default, and a longer one opens its own window
func formRow(key string) configNode {
	spec := configForms[key]
	n := configNode{kind: kindForm, key: key, label: spec.label, description: spec.description, command: spec.command, form: key,
		read: enterValue, disabled: sessionRefusal(spec.unavailable)}
	if len(spec.fields) == 1 {
		n.read = func(*engine.GameContext) string { return spec.fields[0].value }
	}
	return n
}

// openConfigForm opens node n's form from the row keyed id, its fields at their
// defaults, and gives it the keys
func (r *Router) openConfigForm(id string, n configNode) {
	spec := configForms[n.form]
	state := tui.NewFormState()
	for _, f := range spec.fields {
		state.Fields = append(state.Fields, tui.FormField{Label: f.label, State: tui.NewTextFieldState(f.value)})
	}
	r.configForm = &configForm{node: n, spec: spec, state: state, page: configMenu(r.ctx).Page, row: id, inline: len(spec.fields) == 1}
	r.machine.SetMode(input.ModeSearch)
	r.publishConfigForm()
}

// submitConfigForm submits the form's fields as its command would: a refusal or an
// error stays on the form, as a sentence, and success closes the menu
func (r *Router) submitConfigForm(f *configForm) bool {
	if why := f.node.unavailable(r.ctx); why != "" {
		f.err = why
		return false
	}
	var err error
	r.ctx.WithOrigin(event.OriginCommand, func() { err = f.spec.submit(r.ctx, f.state) })
	if err != nil {
		f.err = sentence(err)
		return false
	}
	r.closeOverlay()
	return true
}

// multiplayerConfigOptions is the session's rows, then the other participants a
// host may drop, then this run's bots
func multiplayerConfigOptions(ctx *engine.GameContext) []configNode {
	var session []configNode
	if ctx.SessionCtl != nil {
		session = append(session, configNode{kind: kindInfo, key: "status", label: "Current session", description: ctx.SessionCtl.SessionSummary(), read: startupValue})
	}
	session = append(session,
		formRow("host"), formRow("join"), formRow("spectate"), formRow("request"),
		configNode{kind: kindAction, key: "leave", label: "Leave the session", command: "leave", read: enterValue, confirm: true,
			description: "Play on in a session of your own from where this one stands; your bots come along, and the others play on without you.",
			disabled:    sessionRefusal(engine.SessionController.LeaveError)},
		configNode{kind: kindToggle, key: "relay", label: "Relay for others", description: "Carry the game for players who reach this run and not their host, as after a host loss. Off by default; one that joined through this run is carried either way. Saved for the next run.",
			command: "relay", choices: []string{"off", "on"},
			read: func(ctx *engine.GameContext) string { return toggleWord(ctx.Relaying.Load()) }, disabled: relayUnavailable},
		configNode{kind: kindToggle, key: "takeover", label: "Take over as host", description: "Let a session whose host chose migrate move to this run when that host leaves. This run then listens on a port in each session it joins from now, and the session shares its address with the other players. Off by default; off, this run also passes its turn in the session it is in. Saved for the next run.",
			command: "takeover", choices: []string{"off", "on"},
			read: func(ctx *engine.GameContext) string { return toggleWord(ctx.Takeover.Load()) }, disabled: takeoverUnavailable},
		configNode{kind: kindToggle, key: "lan", label: "Visible on this network", description: "Announce the games this run hosts to players on the same network, who find them under Nearby games with no address to type. Off by default; it applies whenever this run hosts. Saved for the next run.",
			command: "lan", choices: []string{"off", "on"},
			read: func(ctx *engine.GameContext) string { return toggleWord(ctx.Lan.Load()) }, disabled: lanUnavailable})
	options := []configNode{section("Session", session)}
	if ctx.SessionCtl == nil {
		return options
	}
	options = append(options, section("Nearby games", nearbyConfigOptions(ctx)))
	if players := participantConfigOptions(ctx); len(players) > 0 {
		options = append(options, section("Players", players))
	}
	return append(options, section("Bots", append([]configNode{formRow("add")}, botConfigOptions(ctx)...)))
}

// nearbyConfigOptions lists the games found on this network, each joined as :join
// joins its address. Building the list is what keeps the run looking for them.
func nearbyConfigOptions(ctx *engine.GameContext) []configNode {
	if why := lanUnavailable(ctx); why != "" {
		return []configNode{{kind: kindInfo, key: "none", label: why, read: startupValue,
			description: "This run cannot look for games on a network. Join one by its link, or request a session."}}
	}
	games := ctx.SessionCtl.Nearby()
	if len(games) == 0 {
		return []configNode{{kind: kindInfo, key: "none", label: "No games found yet", read: startupValue,
			description: "Games hosted on this network with Visible on this network turned on are listed here as they are found."}}
	}
	options := make([]configNode, 0, len(games))
	for _, g := range games {
		state := ""
		if g.Waiting {
			state = ", waiting to start"
		}
		options = append(options, configNode{kind: kindAction, key: "near/" + g.ID, command: "join", read: enterValue,
			label: fmt.Sprintf("Join %s (%s, %d/%d)", cmp.Or(g.Name, g.Address), g.Scenario, g.Players, g.Ceiling),
			description: fmt.Sprintf("Join the game at %s, as :join does: %s, %d of %d players%s. A failed connection keeps this game intact.",
				g.Address, g.Scenario, g.Players, g.Ceiling, state),
			disabled: func(ctx *engine.GameContext) string {
				if g.Refusal != "" {
					return g.Refusal
				}
				return sessionRefusal(engine.SessionController.JoinError)(ctx)
			},
			activate: func(r *Router) {
				if err := r.ctx.SessionCtl.Join(g.Target); err != nil {
					setCommandError(r.ctx, "Join: "+err.Error())
					return
				}
				r.ctx.MacroClearFlag.Store(true)
				r.closeOverlay()
			}})
	}
	return options
}

func botConfigOptions(ctx *engine.GameContext) []configNode {
	bots := ctx.SessionCtl.Bots()
	if len(bots) == 0 {
		return []configNode{{kind: kindInfo, key: "empty", label: "No bots held by this run", description: "Add bots above. Bots held by another player cannot be removed here.", read: startupValue}}
	}
	options := make([]configNode, 0, len(bots))
	for _, b := range bots {
		key, label, value, reason := fmt.Sprintf("drop/%d", b.ID), fmt.Sprintf("Remove %X: %s", b.Slot, b.Graph), "Enter", ""
		description := "Removes this run's bot from the session. The hexadecimal slot matches its cursor label."
		if b.Joining {
			label = "Cancel joining: " + b.Graph
			description = "This bot is joining: cancel it here. Leaving completes when its pending admission finishes."
		}
		if b.Stopping {
			value, reason = "leaving", "This bot is leaving"
		}
		options = append(options, configNode{kind: kindAction, key: key, label: label, command: "bot", description: description, confirm: true,
			read: func(*engine.GameContext) string { return value }, disabled: func(*engine.GameContext) string { return reason },
			activate: func(r *Router) {
				if err := r.ctx.SessionCtl.RemoveBot(b.ID); err != nil {
					setCommandError(r.ctx, sentence(err))
					return
				}
				r.closeOverlay()
				r.ctx.SetStatusMessage("Bot "+b.Graph+" leaving", parameter.StatusNotice)
			}})
	}
	return options
}

// participantConfigOptions lists the session's other participants: each a row its
// host drops it from, or on a spectator's run the row that follows it.
func participantConfigOptions(ctx *engine.GameContext) []configNode {
	var out []configNode
	for _, p := range ctx.SessionCtl.Participants() {
		if p.Local {
			continue
		}
		kind, description := "player", "Removes this player and every bot it brought."
		if p.Holder != 0 {
			kind, description = "bot", "Removes this bot alone; its holder and other bots stay."
		}
		if p.Away {
			kind += ", away"
		}
		if ctx.World.Watches() {
			out = append(out, configNode{kind: kindAction, key: fmt.Sprintf("follow/%d", p.Slot),
				label: fmt.Sprintf("Follow %X: %s", p.Slot, kind), command: "spectate",
				description: "Look from this player's cursor, as :spectate <slot> does; left and right in the game step through the players.",
				read: func(ctx *engine.GameContext) string {
					if ctx.World.Resources.Player.ViewSlot() == p.Slot {
						return "following"
					}
					return "Enter"
				},
				activate: func(r *Router) {
					reportSpectated(r.ctx, p.Slot, 0)
					r.closeOverlay()
				}})
			continue
		}
		out = append(out, configNode{kind: kindAction, key: fmt.Sprintf("player/%d", p.Entity),
			label: fmt.Sprintf("Drop %X: %s", p.Slot, kind), description: description, confirm: true,
			command: "player", read: enterValue,
			disabled: func(ctx *engine.GameContext) string {
				if !ctx.World.IsSessionCoordinator() {
					return "Only the host can remove other participants"
				}
				return ""
			}, activate: func(r *Router) {
				if err := r.ctx.SessionCtl.RemovePlayer(p.Entity); err != nil {
					setCommandError(r.ctx, sentence(err))
					return
				}
				r.closeOverlay()
				r.ctx.SetStatusMessage(fmt.Sprintf("Slot %X leaving", p.Slot), parameter.StatusNotice)
			}})
	}
	return out
}

func (r *Router) publishConfigForm() {
	f := r.configForm
	if f.inline {
		content := configMenuView(r.ctx, f.page)
		for i := range content.Menu.Rows {
			if row := &content.Menu.Rows[i]; row.Key == f.row {
				field := f.state.Fields[0].State
				row.Value, row.Typing, row.Cursor = field.Value(), true, field.Cursor
				if f.err != "" {
					row.Description = "Error: " + f.err + ". " + row.Description
				}
			}
		}
		publishConfigMenu(r.ctx, content)
		return
	}
	view := &core.OverlayForm{Focus: f.state.Focus, Help: f.spec.fields[f.state.Focus].help, Error: f.err}
	for _, field := range f.state.Fields {
		view.Fields = append(view.Fields, core.OverlayFormField{Label: field.Label, Value: field.State.Value(), Cursor: field.State.Cursor})
	}
	menu := newOverlayMenu(r.ctx, f.page)
	menu.Form = view
	r.ctx.SetOverlayContent(&core.OverlayContent{Title: "Menu / " + f.spec.label, Layout: core.OverlayLayoutMenu, Menu: menu})
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
		case input.MotionUp, input.MotionDown:
			if f.inline {
				r.configMenuBack() // Up and Down leave a text box for the rows beside it
				return r.moveConfigMenu(intent.Motion, 0)
			}
		}
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
		if r.submitConfigForm(f) {
			return true
		}
	default:
		return false
	}
	r.publishConfigForm()
	return true
}

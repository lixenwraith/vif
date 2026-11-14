package mode

import (
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
)

// handleConsole opens the console over the play modes or another overlay, and
// closes it where it is shown
func (r *Router) handleConsole() bool {
	if r.ctx.IsConsoleShown() {
		return r.closeOverlay()
	}
	r.showConsole()
	return true
}

// showConsole leaves the mode or overlay the console opens over for it, as the
// backtick and the menu's Console row do
func (r *Router) showConsole() {
	r.enterOverlay()
	openConsole(r.ctx)
}

// openConsole shows the console at its newest row, which it follows as messages come
// in until it is scrolled off its foot, and pauses a solo run as the menu does
func openConsole(ctx *engine.GameContext) {
	ctx.RequestMode(core.ModeOverlay)
	ctx.PauseForMenu()
	ctx.SetOverlayContent(&core.OverlayContent{Title: parameter.ConsoleTitle, Layout: core.OverlayLayoutConsole})
	ctx.SetOverlayScroll(parameter.ConsoleFollow)
}

// handleConsoleCommand opens the console, or empties it
// Usage: :console | :console clear
func handleConsoleCommand(ctx *engine.GameContext, args []string) CommandResult {
	switch {
	case len(args) == 0:
		openConsole(ctx)
	case len(args) == 1 && args[0] == "clear":
		ctx.ClearConsole()
	default:
		setCommandError(ctx, "Usage: :console [clear]")
	}
	return CommandResult{Continue: true}
}

// returnToConsole gives the keys back to the console the command line was opened
// over, at its foot, where the command and its answer are
func (r *Router) returnToConsole() {
	r.transitionMode(core.ModeOverlay)
	r.ctx.PauseForMenu()
	r.ctx.SetOverlayScroll(parameter.ConsoleFollow)
}

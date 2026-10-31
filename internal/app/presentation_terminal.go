//go:build !vif_headless

package app

import (
	"github.com/lixenwraith/vif/pkg/vmath"
	"os"
	"strings"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/manifest"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/service"
)

type presentationState struct {
	pres         Presentation
	orchestrator *render.RenderOrchestrator

	// termBlocked is when the terminal stopped taking output, zero while it takes it
	termBlocked time.Time
	// awayPaused is a pause made because the player left the screen, undone on return
	awayPaused bool
	// unseen is a run whose player was away when its clock started, behind Android's
	// start screen, until they come
	unseen bool
	// touched says the last pointer press was a finger's, which a waiting game asks for
	touched bool
	// releases says the presentation reports key releases, as its last answer did
	releases bool
	// startScreen says the launch's first game waits behind the start screen, which
	// lifts with its hold
	startScreen bool
	// cell is the zoom the presentation opened at or the run last wrote back
	cell float64
	// menuHost is the presentation when it draws the menu itself, menu the snapshot
	// it was last handed, and menuSent whether the first frame handed it one
	menuHost menuPresentation
	menu     []byte
	menuSent bool
}

func validateBuildConfig(Config) error { return nil }

func (a *App) initPresentationService() error {
	if !a.cfg.Mode.Presents() {
		return nil
	}
	if b := a.cfg.borrow; b != nil {
		a.pres = b.pres // the lender's hub starts and stops it
		return nil
	}
	if a.pres = a.cfg.Host.Presentation; a.pres == nil {
		a.pres = newTerminal(a.cfg)
	}
	return a.hub.Register(a.pres)
}

// newTerminal is the build's own presentation, in the colour mode cfg asks for,
// held to a multiplexer's frame floor unless cfg turns that off.
func newTerminal(cfg Config) terminalHost {
	colorMode := terminal.DetectColorMode()
	if cfg.ColorModeSet {
		colorMode = cfg.ColorMode
	}
	t := terminalHost{TerminalService: service.NewTerminalService(colorMode)}
	if !cfg.NoTmuxFrameCap {
		t.floor = frameFloor(os.Getenv)
	}
	return t
}

// The environment that names a terminal multiplexer: tmux sets multiplexerEnv for
// the programs in it, and TERM names tmux or screen there and over an ssh hop out
const (
	multiplexerEnv = "TMUX"
	termEnv        = "TERM"
)

var multiplexerTerms = []string{"tmux", "screen"}

// frameFloor is the frame interval a terminal keeps up with: inside a multiplexer,
// parameter.MultiplexerFrameInterval, anywhere else any
func frameFloor(getenv func(string) string) time.Duration {
	if getenv(multiplexerEnv) != "" {
		return parameter.MultiplexerFrameInterval
	}
	term := getenv(termEnv)
	for _, prefix := range multiplexerTerms {
		if strings.HasPrefix(term, prefix) {
			return parameter.MultiplexerFrameInterval
		}
	}
	return 0
}

func (a *App) presentationGeometry(width, height int) (int, int, terminal.ColorMode, vmath.Aspect) {
	colorMode, aspect := fallbackColorMode, a.cfg.CellAspect
	if aspect == 0 {
		aspect = vmath.RefAspect
	}
	if !a.cfg.Mode.Presents() {
		return width, height, colorMode, aspect
	}
	if a.cfg.CellAspect == 0 {
		aspect = a.pres.CellAspect()
	}
	// A presentation that has to be restored after a crash says so; a copy built
	// off the frame loop shares the lender's
	if f, ok := a.pres.(interface{ Fini() }); ok && a.cfg.borrow == nil {
		core.SetCrashTerminal(f)
	}
	// The terminal reports a text console as 16 colors, the console the 256-color tier draws for
	if colorMode = a.pres.Surface().ColorMode(); colorMode == terminal.ColorMode16 {
		colorMode = terminal.ColorMode256
	}
	if a.cfg.Mode.OwnsGeometry() || a.cfg.terminalGeometry {
		width, height = a.pres.Size()
	}
	return width, height, colorMode, aspect
}

func (a *App) initPresentation() {
	if z, ok := a.pres.(zoomPresentation); ok {
		a.cell = z.Cell()
	}
	a.menuHost, _ = a.pres.(menuPresentation)
	w, h := a.pres.Size()
	a.orchestrator = render.NewRenderOrchestrator(a.pres.Surface(), w, h)
	// 256 colors is for a text console, whose palette renderers choose their colors from
	if cfg := a.world.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		cfg.ConsolePalette = render.DetectConsolePalette()
		cfg.TextConsole = a.pres.Surface().ColorMode() == terminal.ColorMode16
		a.orchestrator.SetConsole(render.ConsoleFor(cfg.ConsolePalette))
	}
	for _, reg := range manifest.BuildRenderers(a.ctx) {
		a.orchestrator.Register(reg)
	}
}

// lobbyEvent is what a lobby waits on beside the handshake: the terminal's events.
type lobbyEvent = terminal.Event

func (a *App) lobbyEvents() <-chan terminal.Event {
	if a.pres == nil {
		return nil
	}
	return a.pres.Events()
}

func (a *App) lobbyEventCancels(ev terminal.Event) bool {
	switch ev.Type {
	case terminal.EventClosed, terminal.EventError:
		return true
	case terminal.EventResize:
		a.handleResize(ev.Width, ev.Height)
	case terminal.EventKey:
		return ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyCtrlQ
	}
	return false
}

func (a *App) pollTerminalEarly() error {
	if a.pres == nil {
		return nil
	}
	return a.pres.Start()
}

func (a *App) showStartupStatus(message string) {
	a.ctx.SetStatusMessage(message, parameter.StatusNotice)
	if a.orchestrator != nil {
		a.frame()
	}
}

// showJoinAsk asks for the press that seats a waiting join: on the start screen's
// button at the launch, where the host has no start screen of its own, else on the
// status line, drawing the command line or overlay the operator opened over the wait.
func (a *App) showJoinAsk() {
	if !a.cfg.startScreen {
		a.showJoinStatus(a.joinPrompt())
		return
	}
	if a.orchestrator != nil && !pageHidden() && a.terminalTaking() {
		ctx := a.renderContext()
		ctx.StartScreen, ctx.StartHelp, ctx.Prompt = true, "", a.joinPrompt()
		a.orchestrator.RenderStatus(ctx, a.world)
	}
}

// startHelp says ev is a click on the start screen's help button, which opens help
// rather than the game; an overlay drawn over the button keeps the presses on it
func (a *App) startHelp(ev terminal.Event) bool {
	if !a.startScreen || ev.Type != terminal.EventMouse || ev.MouseAction != terminal.MouseActionPress ||
		ev.MouseBtn != terminal.MouseBtnLeft || a.ctx.MouseDisabled.Load() || a.orchestrator == nil {
		return false
	}
	if g := a.ctx.OverlayGeometry(); a.ctx.IsOverlayMode() && render.RectWH(g.X, g.Y, g.W, g.H).Contains(ev.MouseX, ev.MouseY) {
		return false
	}
	width, _ := a.orchestrator.Size()
	return render.StartHelpRect(width, a.startHelpLabel()).Contains(ev.MouseX, ev.MouseY)
}

// startHelpLabel is the start screen's help button, named by the first key help is
// bound to, so a keymap that moves help moves the label; a click opens it either way
func (a *App) startHelpLabel() string {
	if keys := a.ctx.KeyTable.Bindings(input.SectionNormal, "help"); len(keys) > 0 {
		return keys[0] + " " + parameter.StartHelpWord
	}
	return parameter.StartHelpWord
}

// showJoinStatus is showStartupStatus before the gate hands a joiner its session's
// world. The one it holds is a stand-in on the map its dial was offered, which a
// host sizing its session from its first person changes at the gate, so only the
// status line is drawn.
func (a *App) showJoinStatus(message string) {
	a.ctx.SetStatusMessage(message, parameter.StatusNotice)
	if a.orchestrator != nil && !pageHidden() && a.terminalTaking() {
		a.orchestrator.RenderStatus(a.renderContext(), a.world)
	}
}

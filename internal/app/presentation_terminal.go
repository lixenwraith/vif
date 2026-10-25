//go:build !vif_headless

package app

import (
	"github.com/lixenwraith/vif/pkg/vmath"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
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
	// cell is the zoom the presentation opened at or the run last wrote back
	cell float64
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

// newTerminal is the build's own presentation, in the colour mode cfg asks for.
func newTerminal(cfg Config) terminalHost {
	colorMode := terminal.DetectColorMode()
	if cfg.ColorModeSet {
		colorMode = cfg.ColorMode
	}
	return terminalHost{service.NewTerminalService(colorMode)}
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
	w, h := a.pres.Size()
	a.orchestrator = render.NewRenderOrchestrator(a.pres.Surface(), w, h)
	// 256 colors is for a text console, whose palette renderers choose their colors from
	if cfg := a.world.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		cfg.ConsolePalette = render.DetectConsolePalette()
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

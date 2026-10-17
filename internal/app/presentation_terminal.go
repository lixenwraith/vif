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
		colorMode := terminal.DetectColorMode()
		if a.cfg.ColorModeSet {
			colorMode = a.cfg.ColorMode
		}
		a.pres = terminalHost{service.NewTerminalService(colorMode)}
	}
	return a.hub.Register(a.pres)
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

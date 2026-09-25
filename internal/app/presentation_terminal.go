//go:build !vif_headless

package app

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/service"
)

type presentationState struct {
	termSvc      *service.TerminalService
	term         terminal.Terminal
	orchestrator *render.RenderOrchestrator
}

func validateBuildConfig(Config) error { return nil }

func (a *App) initPresentationService() error {
	if !a.cfg.Mode.Presents() {
		return nil
	}
	colorMode := terminal.DetectColorMode()
	if a.cfg.ColorModeSet {
		colorMode = a.cfg.ColorMode
	}
	a.termSvc = service.NewTerminalService(colorMode)
	return a.hub.Register(a.termSvc)
}

func (a *App) presentationGeometry(width, height int) (int, int, terminal.ColorMode) {
	colorMode := fallbackColorMode
	if !a.cfg.Mode.Presents() {
		return width, height, colorMode
	}
	a.term = a.termSvc.Terminal()
	core.SetCrashTerminal(a.term)
	colorMode = a.term.ColorMode()
	if a.cfg.Mode.OwnsGeometry() {
		width, height = a.term.Size()
	}
	return width, height, colorMode
}

func (a *App) initPresentation() {
	w, h := a.term.Size()
	a.orchestrator = render.NewRenderOrchestrator(a.term, w, h)
	// 256 colors is for a text console, whose palette renderers choose their colors from
	if cfg := a.world.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		cfg.ConsolePalette = render.DetectConsolePalette()
		a.orchestrator.SetConsole(render.ConsoleFor(cfg.ConsolePalette))
	}
	for _, reg := range manifest.BuildRenderers(a.ctx) {
		a.orchestrator.Register(reg.Renderer, reg.Priority)
	}
}

func (a *App) lobbyEvents() <-chan terminal.Event {
	if a.termSvc == nil {
		return nil
	}
	return a.termSvc.Events()
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
	if a.termSvc == nil {
		return nil
	}
	return a.termSvc.Start()
}

func (a *App) showStartupStatus(message string) {
	a.ctx.SetStatusMessage(message, 0, false)
	if a.orchestrator != nil {
		a.frame()
	}
}

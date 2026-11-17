//go:build vif_headless

package app

import (
	"errors"
	"github.com/lixenwraith/vif/pkg/vmath"
	"os"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/parameter"
)

type presentationState struct{}

// Host is empty: a headless build has no screen and no speakers to supply
type Host struct{}

func (a *App) initPresentationService() error { return nil }

func (a *App) presentationGeometry(width, height int) (int, int, terminal.ColorMode, vmath.Aspect) {
	aspect := a.cfg.CellAspect
	if aspect == 0 {
		aspect = vmath.RefAspect
	}
	width, height = a.fitTerminal(width, height)
	return width, height, fallbackColorMode, aspect
}

func (a *App) initPresentation() {}

func (a *App) applyMouseMode(bool, bool) {}

func (a *App) keyReportApplier() mode.KeyReportApplier { return nil }

func (a *App) lobbyEvents() <-chan terminal.Event { return nil }

func (a *App) lobbyEventCancels(terminal.Event) bool { return false }

func (a *App) pollTerminalEarly() error { return nil }

func (a *App) showStartupStatus(message string) {
	a.ctx.SetStatusMessage(message, parameter.StatusNotice)
}

func (a *App) showJoinStatus(message string) { a.showStartupStatus(message) }

func Run(Config, Host) error {
	return errors.New("interactive play unavailable in this build; use -serve")
}

func PlayJournal(Config, ...string) error {
	return errors.New("journal presentation unavailable in this build")
}

func runPresented(*App, pacedSource, string, string, bool, <-chan os.Signal) error {
	return errors.New("presentation unavailable in this build")
}

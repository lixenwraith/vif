//go:build vif_headless

package app

import (
	"fmt"
	"github.com/lixenwraith/vif/pkg/vmath"
	"os"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/parameter"
)

type presentationState struct{}

// Host is empty: a headless build has no screen and no speakers to supply
type Host struct{}

func validateBuildConfig(cfg Config) error {
	if cfg.Mode.Presents() {
		return fmt.Errorf("%s: presentation is unavailable in a vif_headless build", cfg.Mode)
	}
	return nil
}

func (a *App) initPresentationService() error { return nil }

func (a *App) presentationGeometry(width, height int) (int, int, terminal.ColorMode, vmath.Aspect) {
	aspect := a.cfg.CellAspect
	if aspect == 0 {
		aspect = vmath.RefAspect
	}
	return width, height, fallbackColorMode, aspect
}

func (a *App) initPresentation() {}

func (a *App) applyMouseMode(bool, bool) {}

type lobbyEvent struct{}

func (a *App) lobbyEvents() <-chan lobbyEvent { return nil }

func (a *App) lobbyEventCancels(lobbyEvent) bool { return false }

func (a *App) pollTerminalEarly() error { return nil }

func (a *App) showStartupStatus(message string) {
	a.ctx.SetStatusMessage(message, parameter.StatusNotice)
}

func Run(Config) error {
	return fmt.Errorf("interactive play is unavailable in a vif_headless build; use -serve")
}

func PlayJournal(Config, ...string) error {
	return fmt.Errorf("journal presentation is unavailable in a vif_headless build")
}

func runPresented(*App, pacedSource, string, string, time.Duration, bool, <-chan os.Signal) error {
	return fmt.Errorf("presentation is unavailable in a vif_headless build")
}

//go:build vif_headless

package app

import (
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/journal"
)

type presentationState struct{}

func validateBuildConfig(cfg Config) error {
	if cfg.Mode.Presents() {
		return fmt.Errorf("%s: presentation is unavailable in a vif_headless build", cfg.Mode)
	}
	return nil
}

func (a *App) initPresentationService() error { return nil }

func (a *App) presentationGeometry(width, height int) (int, int, terminal.ColorMode) {
	return width, height, fallbackColorMode
}

func (a *App) initPresentation() {}

func (a *App) applyMouseMode(bool, bool) {}

type lobbyEvent struct{}

func (a *App) lobbyEvents() <-chan lobbyEvent { return nil }

func (a *App) lobbyEventCancels(lobbyEvent) bool { return false }

func (a *App) pollTerminalEarly() error { return nil }

func (a *App) showStartupStatus(message string) {
	a.ctx.SetStatusMessage(message, 0, false)
}

func Run(Config) error {
	return fmt.Errorf("interactive play is unavailable in a vif_headless build; use -serve")
}

func PlayJournal(...string) error {
	return fmt.Errorf("journal presentation is unavailable in a vif_headless build")
}

func runPresentedScript(*App, *journal.ScriptDriver, string, time.Duration, bool,
	<-chan os.Signal) (journal.ScriptStats, error) {
	return journal.ScriptStats{}, fmt.Errorf("script presentation is unavailable in a vif_headless build")
}

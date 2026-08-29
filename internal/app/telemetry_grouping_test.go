package app

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

func TestTelemetryGroupsFitDebugCards(t *testing.T) {
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()
	a.Settle()

	seen := make(map[string]bool)
	for _, view := range a.World().Resources.Status.Views() {
		seen[view.Name()] = true
		if view.Len() > parameter.OverlayCardMaxEntries {
			t.Errorf("telemetry group %q has %d entries, max %d",
				view.Name(), view.Len(), parameter.OverlayCardMaxEntries)
		}
	}
	for _, want := range []string{
		"adapt.buffers", "combat.absorbed.attacker", "combat.damage.defender",
		"combat.rejects", "death.batch", "event.settle", "eye.ga", "fsm.main",
		"network.session", "player.0", "player.0.weapon", "storm.protection",
	} {
		if !seen[want] {
			t.Errorf("semantic telemetry group %q is missing", want)
		}
	}
	visiblePlayers := make(map[string]bool)
	for _, view := range a.World().Resources.Status.VisibleViews() {
		if strings.HasPrefix(view.Name(), "player.") &&
			view.Name() != "player.0" && view.Name() != "player.0.weapon" {
			t.Errorf("inactive roster group %q is visible", view.Name())
		}
		if strings.HasPrefix(view.Name(), "player.0") {
			visiblePlayers[view.Name()] = true
		}
	}
	for _, want := range []string{"player.0", "player.0.weapon"} {
		if !visiblePlayers[want] {
			t.Errorf("active roster group %q is hidden", want)
		}
	}
}

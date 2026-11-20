package membership

import (
	"errors"
	"os"
	"testing"

	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
)

func TestBotRemovalDoesNotFollowReusedRosterIdentities(t *testing.T) {
	var m Membership
	old := &seat{number: 1, graph: &bot.Graph{Name: "default"}, stop: make(chan os.Signal)}
	old.id.Store(2)
	old.slot.Store(1)
	m.seats = []*seat{old}
	selected := m.botSeats()[0]
	replacement := &seat{number: 2, graph: old.graph, stop: make(chan os.Signal)}
	replacement.id.Store(2)
	replacement.slot.Store(1)
	m.seats = []*seat{replacement}
	if err := m.removeSeat(selected.ID); err == nil || m.botSeats()[0].Stopping {
		t.Fatal("stale selection removed a replacement with the same participant and slot")
	}
	if err := m.removeSeat(replacement.number); err != nil || !m.botSeats()[0].Stopping {
		t.Fatal("current bot could not be removed")
	}
	pending := &seat{number: 3, graph: old.graph, stop: make(chan os.Signal)}
	m.seats = []*seat{pending}
	if !m.botSeats()[0].Joining {
		t.Fatal("pending bot appears admitted")
	}
	if err := m.removeSeat(pending.number); err != nil || !m.botSeats()[0].Stopping {
		t.Fatal("pending bot could not be cancelled")
	}
}

// TestABotSeatRefusalIsTheRunsOwnFailure: a bot a scenario seating one player is
// asked to seat is refused as this run's own failure, which no session answered, so
// a run that dials nothing exits as one that failed, never as one a session refused.
func TestABotSeatRefusalIsTheRunsOwnFailure(t *testing.T) {
	t.Parallel()
	sc, err := resource.LoadScenario(resource.Options{Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	sc.MaxPlayers = 1
	if err := botRefused(sc); err == nil || errors.As(err, new(*network.Refusal)) {
		t.Fatalf("a bot asked of a scenario seating one player is refused %v; want the run's own failure, no session's refusal", err)
	}
}

package membership

import (
	"os"
	"testing"

	"github.com/lixenwraith/vif/internal/bot"
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

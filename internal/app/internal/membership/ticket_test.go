package membership

import (
	"errors"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/network"
)

// A door that takes tickets charges the join budget to the player the ticket
// names, since every dial reaches it from the router's address: two tickets for
// one subject spend that subject's budget, and the address spends none.
func TestATicketedDoorBudgetsTheTicketsSubject(t *testing.T) {
	router, dialer := network.NewKey(), network.NewKey()
	m := &Membership{
		cfg:        &config.Membership{SessionName: "s", TicketKeys: [][]byte{router.Public()}},
		admissions: network.NewAdmissionLimiterOf(time.Minute, 1),
	}
	if m.joinBudget() != nil {
		t.Fatal("a ticketed door charges the address")
	}
	hook := m.ticketHook(network.AdmitPlay)
	mint := func(subject string) *network.Signed {
		t.Helper()
		ticket, err := network.SignTicket(router, network.TicketBody{Session: "s", Kind: network.AdmitPlay,
			Subject: subject, Key: dialer.Public()}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	keys := []string{dialer.ID()}
	if body, err := hook(mint("acct:7"), keys); err != nil || body.Subject != "acct:7" {
		t.Fatalf("the first ticket: %+v, %v", body, err)
	}
	if _, err := hook(mint("addr:198.51.100.7"), keys); err != nil {
		t.Fatalf("another subject's ticket: %v", err)
	}
	_, err := hook(mint("acct:7"), keys)
	var refused *network.Refusal
	if !errors.As(err, &refused) || refused.Code != network.RefusalBudget {
		t.Fatalf("the subject's second ticket in the window: %v", err)
	}
	m.cfg.TicketKeys = nil
	if m.joinBudget() == nil || m.ticketHook(network.AdmitPlay) != nil {
		t.Fatal("an open door's budget or gate")
	}
}

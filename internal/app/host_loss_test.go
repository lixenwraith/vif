package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/network"
)

func TestGuestContinuesLocallyAfterHostLoss(t *testing.T) {
	host, guest, _ := shapedPair(t, 0x10571057, network.LinkShape{})
	runSession(host, guest, 80)

	beforeTick := guest.Position().Tick
	beforeCell, ok := localCell(guest)
	if !ok {
		t.Fatal("guest has no local cursor")
	}
	if err := transportOf(t, host).Close(); err != nil {
		t.Fatalf("close host link: %v", err)
	}
	guest.Tick(1) // drain the disconnect through the ordinary poll boundary
	if !statBoolOf(guest, "network.host_lost") {
		t.Fatal("guest did not enter explicit local continuation")
	}

	// The fork is still a playable game: local input settles immediately and the
	// scheduler keeps advancing without an authority or a replacement election.
	inject(t, guest, intentMotion(input.MotionRight, 1))
	afterCell, _ := localCell(guest)
	if afterCell.X != beforeCell.X+1 || afterCell.Y != beforeCell.Y {
		t.Fatalf("local continuation moved to %#v, want one cell right of %#v", afterCell, beforeCell)
	}
	guest.Tick(8)
	if got := guest.Position().Tick; got <= beforeTick+1 {
		t.Fatalf("guest stopped at tick %d after losing the host; started at %d", got, beforeTick)
	}
}

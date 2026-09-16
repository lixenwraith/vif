package app

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// chooseBarrierDelay picks the lead one roster and its measurements ask for, and
// names the participants whose links ask for more than the ceiling can absorb.
// Nothing on the far end takes no lead, nothing measured keeps the default, and a
// measured link asks for what it measured. The ceiling reads the *smallest* round
// trip, not the smoothed one: see multi-player.md §3.5 for why.
func chooseBarrierDelay(link engine.LinkMeasuringPort, roster []network.RosterEntry, local network.PeerID) (uint64, []network.PeerID) {
	if len(roster) == 0 {
		return parameter.NetworkBarrierDelayTicks, nil
	}
	var (
		worst    uint64
		remote   int
		measured bool
		overrun  []network.PeerID
	)
	hops := sessionHops(roster)
	ticks := func(d time.Duration) uint64 {
		return hops * uint64((d+parameter.GameUpdateInterval-1)/parameter.GameUpdateInterval)
	}
	for _, p := range roster {
		if p.ID == local {
			continue
		}
		remote++
		if link == nil {
			continue
		}
		m := link.LinkMetric(uint32(p.ID))
		if !m.Ready || m.RTT <= 0 {
			continue
		}
		if m.MinRTT > 0 && ticks(m.MinRTT/2) > parameter.NetworkBarrierMaxDelayTicks {
			overrun = append(overrun, p.ID)
		}
		worst = max(worst, ticks(m.RTT/2+parameter.NetworkBarrierJitterMargin*m.Jitter))
		measured = true
	}
	switch {
	case remote == 0:
		return 0, nil
	case !measured:
		return parameter.NetworkBarrierDelayTicks, nil
	}
	return min(max(worst, parameter.NetworkBarrierMinDelayTicks), parameter.NetworkBarrierMaxDelayTicks), overrun
}

// sessionHops is the diameter of the topology the CLI builds: one guest is one hop
// to the coordinator, two or more reach each other through it.
func sessionHops(roster []network.RosterEntry) uint64 {
	if len(roster) > 2 {
		return 2
	}
	return 1
}

// playoutLead measures the roster the protocol hands it against this instance's own
// links, beside the lead the barrier is actually deferring by. The roster comes from
// the caller because the session holds two and they do not always agree: the cursors
// carry the live one, and the lobby's own is what a run whose cursors are not up yet
// has.
func (a *App) playoutLead(roster []network.RosterEntry) (current, target uint64, overrun []network.PeerID) {
	var link engine.LinkMeasuringPort
	a.world.RunSafe(func() {
		if r := a.world.Resources.Network; r != nil {
			current = r.BarrierDelayTicks
			link, _ = r.Port.(engine.LinkMeasuringPort)
		}
	})
	target, overrun = chooseBarrierDelay(link, roster, a.authorityID())
	return current, target, overrun
}

// crossPlayoutLead publishes the session's lead as the barrier-bound crossing it
// is: nobody's input waits on it, and every instance has to start deferring by it
// at one agreed tick. OriginSession for the reason a roster change is — no other
// record in the stream implies a measurement, so a reproduction that did not carry
// it would defer by a lead the run it reproduces never had.
func (a *App) crossPlayoutLead(ticks uint64) {
	a.world.RunSafe(func() {
		a.world.PushEventFull(event.EventPlayoutLead,
			&event.PlayoutLeadPayload{Ticks: ticks}, event.OriginSession, core.DomainPlayer)
	})
	a.sessionMu.Lock()
	a.barrierDelay = ticks
	a.sessionMu.Unlock()
}

// dropParticipant closes one participant's link. What follows is the ordinary
// departure path: a direct neighbour observes the loss and the authority turns it
// into one crossing at one agreed tick.
func (a *App) dropParticipant(id uint32) bool {
	p, ok := a.sessionTransport().(engine.PeerDroppingPort)
	return ok && p.Disconnect(id)
}

// adoptBarrierDelayLocked records the lead every later offer carries.
// Caller MUST hold sessionMu.
func (a *App) adoptBarrierDelayLocked(link engine.LinkMeasuringPort) {
	chosen, _ := chooseBarrierDelay(link, a.sessionRoster, a.authorityID())
	if chosen == a.barrierDelay {
		return
	}
	a.barrierDelay = chosen
	vlog.Info("app", "msg", "playout lead chosen",
		"ticks", chosen, "roster", len(a.sessionRoster),
		"hops", sessionHops(a.sessionRoster))
}

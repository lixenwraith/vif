package app

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// chooseBarrierDelay picks the lead from the worst measured link: one way plus a
// reordering allowance, times the hop count, inside [floor, ceiling]. An unready
// link contributes nothing rather than zero, so a lobby that closes before a probe
// completes keeps the default.
func chooseBarrierDelay(link engine.LinkMeasuringPort, roster []network.SessionParticipant, local network.PeerID) uint64 {
	if link == nil || len(roster) == 0 {
		return parameter.NetworkBarrierDelayTicks
	}
	var worst uint64
	for _, p := range roster {
		if p.ID == local {
			continue
		}
		m := link.LinkMetric(uint32(p.ID))
		if !m.Ready || m.RTT <= 0 {
			continue
		}
		budget := m.RTT/2 + parameter.NetworkBarrierJitterMargin*m.Jitter
		ticks := uint64((budget + parameter.GameUpdateInterval - 1) / parameter.GameUpdateInterval)
		worst = max(worst, ticks)
	}
	if worst == 0 {
		return parameter.NetworkBarrierDelayTicks
	}
	if hops := sessionHops(roster); hops > 1 {
		worst *= hops
	}
	return min(max(worst, parameter.NetworkBarrierDelayTicks), parameter.NetworkBarrierMaxDelayTicks)
}

// sessionHops is the diameter of the topology the CLI builds: one guest is one hop
// to the coordinator, two or more reach each other through it.
func sessionHops(roster []network.SessionParticipant) uint64 {
	if len(roster) > 2 {
		return 2
	}
	return 1
}

// adoptBarrierDelayLocked records the lead every later offer carries.
// Caller MUST hold sessionMu.
func (a *App) adoptBarrierDelayLocked(link engine.LinkMeasuringPort) {
	chosen := chooseBarrierDelay(link, a.sessionRoster, a.authorityID())
	if chosen == a.barrierDelay {
		return
	}
	a.barrierDelay = chosen
	vlog.Info("app", "msg", "playout lead chosen",
		"ticks", chosen, "participants", len(a.sessionRoster),
		"hops", sessionHops(a.sessionRoster))
}

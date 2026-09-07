// Choosing the session's playout lead.
//
// Every wire frame carries an absolute ApplyTick, and a remote copy waits for it.
// The lead between production and that tick is the interpolation buffer: long
// enough and a peer's artifacts have arrived when their tick comes up, short
// enough and a remote actor is not lagging visibly behind the world it is in.
//
// The plumbing for a negotiated lead was complete long before anything chose one.
// BarrierDelayTicks travels in SessionOffer, survives a handoff in HandoffRecord,
// reaches NetworkResource and sets NetworkSystem.delayTicks — and every writer put
// the same constant in it, so a 150 ms budget was what every deployment got
// whether its links were loopback or intercontinental. This is the missing input.
//
// Two properties bound the design. The value is session-wide, because a run
// reproducing a session by replay has to defer its re-derived crossings by the
// same lead the run it reproduces did. And it is chosen once, when the coordinator
// closes its roster, because there is no authoritative artifact between offers and
// handoffs that could tell participants it had changed — a coordinator that
// re-chose per joiner would leave two participants applying the same producer's
// artifacts at different ticks.

package app

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// chooseBarrierDelay picks the lead from what the links have actually measured,
// bounded below by the constant and above by NetworkBarrierMaxDelayTicks.
//
// The estimate per link is one way plus a reordering allowance — half the round
// trip, plus a multiple of the measured variation — and the session takes the
// worst of them, because one lead has to serve every participant.
//
// Then the hop count. The protocol relays over arbitrary graphs and an artifact
// crossing a relay pays each link in turn, so a lead measured on direct links
// under-counts the path a relayed peer's artifacts actually take. The CLI builds a
// star, whose diameter is two links the moment a second guest arrives, so that is
// what a roster of more than two is charged; a two-participant session is one hop
// and pays for one.
//
// An unmeasured or not-yet-ready link contributes nothing rather than zero: a
// session that closes its lobby before a probe has completed a round trip keeps
// the default, which is the same answer it had before any of this existed.
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

// sessionHops is the number of links an artifact crosses between the two furthest
// participants of the topology the CLI builds. One guest is one hop to the
// coordinator; two or more reach each other through it.
func sessionHops(roster []network.SessionParticipant) uint64 {
	if len(roster) > 2 {
		return 2
	}
	return 1
}

// adoptBarrierDelay chooses the session's lead and records it for every offer this
// coordinator builds afterwards. Caller MUST hold sessionMu.
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

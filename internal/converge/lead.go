package converge

import (
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// driveLead re-derives the session's playout lead from what its links measure and
// publishes it as the barrier-bound crossing every instance applies at one agreed
// tick. Only the authority authors one: the lead is session identity, so a
// participant deriving its own from a measurement nobody else took would produce
// artifacts for ticks the rest of the session is not on. See multi-player.md §3.5.
func (u *Authority) driveLead() {
	if u.inst.LocalParticipant() == 0 || !u.isAuthority() {
		return
	}
	current, target, overrun := u.inst.PlayoutLead(u.currentRoster())
	for _, id := range overrun {
		// The report follows the transport rather than the decision: the roster
		// keeps the entry until the departure crossing applies, so the same
		// verdict is reached for a lead's worth of ticks after the link is gone.
		if u.inst.DropParticipant(uint32(id)) {
			vlog.Warn("app", "msg", "participant dropped past the playout ceiling",
				"participant", id, "ceiling_ticks", parameter.NetworkBarrierMaxDelayTicks)
		}
	}

	tick := u.inst.Position().Tick
	u.mu.Lock()
	next, lowSince, announce := chooseLead(current, target, tick, u.leadPublished, u.leadLowSince)
	changed := next != current
	// Written back so a handoff record carries the lead the session is on, which is
	// the other half of not remembering it above.
	u.delay, u.leadLowSince = next, lowSince
	if announce {
		u.leadPublished = tick
	}
	u.mu.Unlock()

	if !announce {
		return
	}
	u.inst.SetPlayoutLead(next)
	if changed {
		vlog.Info("app", "msg", "playout lead renegotiated", "ticks", next, "tick", tick)
	}
}

// chooseLead is the whole of the policy, kept apart from the session it reads so it
// can be exercised against a table. It raises at once and lowers on a window,
// because an artifact that misses the lead costs a correction and one that clears
// it costs nothing; the same window re-announces an unchanged lead, so a change a
// peer never received costs a window rather than the rest of the session.
func chooseLead(current, target, tick, published, lowSince uint64) (next, nextLowSince uint64, announce bool) {
	// A window whose start is ahead of the clock is one an install moved the clock
	// out from under, which is a window to restart rather than a huge one.
	elapsed := func(since uint64) uint64 {
		if since == 0 || tick < since {
			return 0
		}
		return tick - since
	}
	stale := published == 0 || tick < published ||
		elapsed(published) >= parameter.NetworkBarrierRenegotiateTicks
	switch {
	case target > current:
		return target, 0, true
	case target == current:
		return current, 0, stale
	case lowSince == 0 || tick < lowSince:
		return current, tick, stale
	case elapsed(lowSince) >= parameter.NetworkBarrierRenegotiateTicks:
		return target, 0, true
	default:
		return current, lowSince, stale
	}
}

package converge

import (
	"cmp"
	"slices"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
)

// PeerCadence is one link's operating point and the measurements it came from.
type PeerCadence struct {
	Participant      uint32
	CadenceTicks     uint64
	KeyframeInterval int
	Constrained      bool
	FloorBreached    bool
	RTT              time.Duration
	Jitter           time.Duration
	ThroughputBps    float64
	Saturated        bool

	// Drift is the rise in this participant's own correction magnitude, in
	// percent; Relevance how far its share of the last correction stood above the
	// session's mean; Near the raw count behind that share.
	Drift     int
	Relevance int
	Near      int
}

// CadenceReport is the session's operating point: the timeline the host publishes
// on, and the per-link decisions it was composed from. The status bar shows the
// worst link; this names which one it is.
type CadenceReport struct {
	CadenceTicks        uint64
	KeyframePeriodTicks uint64
	KeyframeInterval    int
	Constrained         bool
	FloorBreached       bool
	KeyframeBytes       int64
	DeltaBytes          int64
	Peers               []PeerCadence
}

// Cadence describes what the correction cadence is doing, per link. The status bar
// reads the published cells instead — under the world lock, where taking this one
// would invert the lock order — so this is the structured form, which is the only
// one that can name which link is the worst. A run that is not publishing returns
// the zero value, which reads as "nominal, no links".
func (c *Corrections) Cadence() CadenceReport {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	out := CadenceReport{
		CadenceTicks:        c.base,
		KeyframePeriodTicks: c.keyPeriod,
		FloorBreached:       c.breached,
		KeyframeBytes:       c.sizes.Keyframe,
		DeltaBytes:          c.sizes.Delta,
	}
	if c.base > 0 {
		out.KeyframeInterval = int(c.keyPeriod / c.base)
	}
	for id, p := range c.peers {
		out.Constrained = out.Constrained || p.plan.Constrained
		out.Peers = append(out.Peers, PeerCadence{
			Participant:      id,
			CadenceTicks:     p.plan.CadenceTicks,
			KeyframeInterval: p.plan.KeyframeInterval,
			Constrained:      p.plan.Constrained,
			FloorBreached:    p.plan.FloorBreached,
			RTT:              p.metrics.RTT,
			Jitter:           p.metrics.Jitter,
			ThroughputBps:    p.metrics.Throughput,
			Saturated:        p.metrics.Saturated,
			Drift:            p.demand.Drift,
			Relevance:        p.demand.Relevance,
			Near:             p.near,
		})
	}
	slices.SortFunc(out.Peers, func(x, y PeerCadence) int {
		return cmp.Compare(x.Participant, y.Participant)
	})
	return out
}

// PeerSelective is one peer's standing in the selective protocol, from the host's
// side: the last manifest it was sent, the last it answered, whether that answer
// said it had converged, and how many manifests have gone unanswered.
type PeerSelective struct {
	ManifestTick uint64
	AnsweredTick uint64
	Converged    bool
	Silence      int
}

// SelectiveReport is what the selective protocol is currently doing, for the
// diagnostics surface and for the criteria that assert the protocol rather than its
// effect. WholeBodies is the session-wide fallback: a participant nobody can answer
// selectively keeps every peer on whole correction bodies.
type SelectiveReport struct {
	Retained    []uint64
	Awaiting    uint64
	Requests    int
	Keyframe    bool
	WholeBodies bool
	PeerState   map[uint32]PeerSelective
}

// Selective describes the selective exchange, for `:session` and for the criteria.
func (c *Corrections) Selective() SelectiveReport {
	out := SelectiveReport{PeerState: map[uint32]PeerSelective{}}
	c.publishMu.Lock()
	for _, r := range c.selective.retained {
		out.Retained = append(out.Retained, r.tick)
	}
	for id, p := range c.peers {
		out.PeerState[id] = PeerSelective{
			ManifestTick: p.manifestTick,
			AnsweredTick: p.answeredTick,
			Converged:    p.converged,
			Silence:      p.silence,
		}
	}
	out.WholeBodies = c.saidUnrelayed
	c.publishMu.Unlock()
	c.selectiveMu.Lock()
	if n := len(c.selective.awaiting); n > 0 {
		out.Awaiting = c.selective.awaiting[n-1].tick
	}
	out.Keyframe = c.selective.wantKeyframe
	out.Requests = len(c.selective.requests)
	c.selectiveMu.Unlock()
	slices.Sort(out.Retained)
	return out
}

// AuthorityReport is the structured answer to who is authoring, beside the one line
// Summary gives the status bar.
// Roster, Anchor and Delay are the membership a handoff carries unchanged: a record
// is refused unless all three are the ones the session closed on, so what they say
// before and after one is what says a handoff moved authorship and nothing else.
type AuthorityReport struct {
	Term       network.AuthorityTerm
	Authority  network.PeerID
	Local      network.PeerID
	Migrations int64
	Migrating  bool
	Fork       bool
	Retained   int
	RetainedAt uint64
	Roster     []network.RosterEntry
	Anchor     event.JoinAnchor
	Delay      uint64
}

// Authoring reports whether this instance is the one publishing the world, which
// is Authority and Local being the same participant under a term that exists.
func (r AuthorityReport) Authoring() bool {
	return r.Term > 0 && r.Local != 0 && r.Local == r.Authority
}

// State describes this instance's place in the session's authority.
func (u *Authority) State() AuthorityReport {
	u.mu.Lock()
	out := AuthorityReport{
		Term: u.term, Authority: u.holder, Local: u.local,
		Migrations: u.statMigrations.Load(),
		Migrating:  u.contested != 0, Fork: u.fork,
		Roster: slices.Clone(u.roster), Anchor: u.anchor, Delay: u.delay,
	}
	u.mu.Unlock()
	out.RetainedAt, out.Retained = u.corrections.retentionEvidence()
	return out
}

// canAnswer reports whether every participant can be repaired selectively right
// now, for a caller outside the publication schedule.
func (c *Corrections) canAnswer(ids []uint32) bool {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	return c.canAnswerEveryParticipantLocked(ids)
}

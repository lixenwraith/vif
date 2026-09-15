package converge

import (
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// authorityFrame is one queued succession message.
type authorityFrame struct {
	kind uint8
	from uint32
	body []byte
}

// ReceiveAuthorityFrame queues one succession message. Caller holds the world
// lock, so it takes the bytes and nothing else.
func (c *Corrections) ReceiveAuthorityFrame(kind uint8, from uint32, body []byte) {
	c.authorityMu.Lock()
	if len(c.authorityIn) < maxQueuedAuthorityFrames {
		c.authorityIn = append(c.authorityIn, authorityFrame{kind: kind, from: from, body: body})
	}
	c.authorityMu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// QueuePeerLost records a departure for the succession to read between two ticks.
// Caller holds the world lock.
func (c *Corrections) QueuePeerLost(id uint32) {
	c.authorityMu.Lock()
	if !slices.Contains(c.peersLost, id) && len(c.peersLost) < maxQueuedAuthorityFrames {
		c.peersLost = append(c.peersLost, id)
	}
	c.authorityMu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// maxQueuedAuthorityFrames bounds what one succession may buffer. A report is
// idempotent and a handoff adopted once, so the ceiling is far
// above what a roster can produce; reaching it means a peer that is not running a
// succession.
const maxQueuedAuthorityFrames = 256

// driveAuthority drains the succession queue and advances the election. It runs
// between two ticks, from whichever loop this instance has.
func (c *Corrections) driveAuthority() {
	u := c.authority
	c.authorityMu.Lock()
	frames, lost := c.authorityIn, c.peersLost
	c.authorityIn, c.peersLost = nil, nil
	c.authorityMu.Unlock()

	for _, id := range lost {
		u.peerLost(id)
	}
	for _, f := range frames {
		c.tel.HandoffBytes.Add(int64(len(f.body)))
		u.Receive(f.kind, f.from, f.body)
	}
	u.drive()
}

// BecomeAuthority turns a receiver into the publisher for a new term, seeded from
// the last whole capture it installed. That capture is the one every other survivor
// installed too, so it is already the baseline a delta may name — without it the
// successor would publish a keyframe to everyone at once. The per-peer schedule is
// not inherited: a plan is a statement about this instance's links.
func (c *Corrections) BecomeAuthority(rec network.HandoffRecord) {
	c.publishMu.Lock()
	c.installedMu.Lock()
	if c.haveBase {
		c.baseline, c.haveKey, c.lastKeyTick = c.installed, true, c.installed.Header.Tick
	}
	c.installedMu.Unlock()
	clear(c.peers)
	c.nextBroadcast = 0
	seeded, seedTick := c.haveKey, c.lastKeyTick
	c.publishMu.Unlock()

	c.selectiveMu.Lock()
	c.selective.awaiting = nil
	c.selective.manifests, c.selective.shardSets = nil, nil
	c.selective.wantKeyframe = false
	c.selective.source = 0
	c.selectiveMu.Unlock()

	// A publisher installs nothing, so a correction still waiting on this
	// instance's clock describes a world it is about to supersede.
	c.dropHeld()

	vlog.Warn("app", "msg", "authoring under a new term",
		"term", uint64(rec.Term), "seeded_from_tick", seedTick, "have_baseline", seeded)

	// A driven run's caller paces its own corrections and calls PublishCorrection;
	// starting a pump beside it would give the session two things deciding when a
	// correction leaves, which is the ambiguity the driven flag exists to prevent.
	if !c.inst.Driven() {
		c.StartPump()
	}
}

// FollowAuthority resets what a receiver holds about the authority it was talking
// to, without touching the world. Outstanding baselines name manifests nobody can
// answer any more and the keyframe wait is answered by the successor's first
// manifest; the installed capture stays, because that index is compared against it.
func (c *Corrections) FollowAuthority(rec network.HandoffRecord) {
	c.selectiveMu.Lock()
	c.selective.awaiting = nil
	c.selective.wantKeyframe = false
	c.selective.source = 0
	c.selectiveMu.Unlock()

	c.publishMu.Lock()
	clear(c.peers)
	c.publishMu.Unlock()

	// Whatever is waiting on the clock was authored under the term this instance
	// has just left, and the successor's first correction supersedes it anyway.
	c.dropHeld()

	vlog.Info("app", "msg", "following a new authority",
		"term", uint64(rec.Term), "authority", uint64(rec.Authority))
}

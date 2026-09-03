// The correction protocol across a change of authority. Three things change and
// nothing else:
//
//   - Which role this run plays. A guest that is elected keeps its apply loop and
//     gains a publication cadence. It does not restart the protocol: the capture it
//     last installed is the keyframe every other survivor also holds, so it becomes
//     the baseline the successor's first delta names.
//
//   - What a term admits. The gate is applied where the artifact is decoded rather
//     than where it is queued: an inbound frame arrives under the world lock and the
//     queue may only take bytes.
//
//   - What the successor inherits. The retained ring, which is what lets it answer a
//     request naming a manifest the previous authority published; the per-peer
//     schedule starts again, because the links are this instance's rather than its
//     predecessor's.

package app

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

// receiveAuthorityFrame queues one succession message. Caller holds the world
// lock, so it takes the bytes and nothing else.
func (c *corrections) receiveAuthorityFrame(kind uint8, from uint32, body []byte) {
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

// queuePeerLost records a departure for the succession to read between two ticks.
// Caller holds the world lock.
func (c *corrections) queuePeerLost(id uint32) {
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
// idempotent, a vote immutable and a handoff adopted once, so the ceiling is far
// above what a roster can produce; reaching it means a peer that is not running a
// succession.
const maxQueuedAuthorityFrames = 256

// driveAuthority drains the succession queue and advances the election. It runs
// between two ticks, from whichever loop this instance has.
func (c *corrections) driveAuthority() {
	u := c.a.authority
	if u == nil {
		return
	}
	c.authorityMu.Lock()
	frames, lost := c.authorityIn, c.peersLost
	c.authorityIn, c.peersLost = nil, nil
	c.authorityMu.Unlock()

	for _, id := range lost {
		u.peerLost(id)
	}
	for _, f := range frames {
		c.a.snapshotTelemetry.handoffBytes.Add(int64(len(f.body)))
		u.receive(f.kind, f.from, f.body)
	}
	u.drive()
}

// becomeAuthority turns a receiver into the publisher for a new term.
//
// The seeding is the whole of requirement 5. A successor that started from no
// baseline would publish a keyframe to every survivor at once — a keyframe storm
// exactly where the session can least afford one — and it does not have to: the
// last whole capture this instance installed is the capture every other survivor
// installed too, so it is already the baseline a delta may name and the state a
// manifest may be compared against. What the successor adds is the new term, and
// a receiver whose world agrees answers the first manifest with a hash and no
// state at all.
//
// The per-peer schedule is deliberately not inherited. A plan is a statement about
// a link, and these are this instance's links rather than its predecessor's.
func (c *corrections) becomeAuthority(rec network.HandoffRecord) {
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

	vlog.Warn("app", "msg", "authoring under a new term",
		"term", uint64(rec.Term), "seeded_from_tick", seedTick, "have_baseline", seeded)

	// A driven run's caller paces its own corrections and calls PublishCorrection;
	// starting a pump beside it would give the session two things deciding when a
	// correction leaves, which is the ambiguity the driven flag exists to prevent.
	if !c.a.cfg.Mode.Driven() {
		c.startPump()
	}
}

// followAuthority resets what a receiver holds about the authority it was talking
// to, without touching the world.
//
// The outstanding baselines go because they name manifests the previous authority
// published and no one can answer any more; the keyframe wait goes because the
// successor's first manifest is the answer to it. The installed capture stays: it
// is the state this instance holds, and the successor's first index is compared
// against exactly that.
func (c *corrections) followAuthority(rec network.HandoffRecord) {
	c.selectiveMu.Lock()
	c.selective.awaiting = nil
	c.selective.wantKeyframe = false
	c.selective.source = 0
	c.selectiveMu.Unlock()

	c.publishMu.Lock()
	clear(c.peers)
	c.publishMu.Unlock()

	vlog.Info("app", "msg", "following a new authority",
		"term", uint64(rec.Term), "authority", uint64(rec.Authority))
}

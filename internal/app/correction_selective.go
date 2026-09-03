// Package app: the correction as an exchange rather than a broadcast.
//
// Phase 5's correction is one-directional and self-sufficient: the host reads its
// world and sends a body, and a receiver either applies it or waits for the next.
// Phase 6 keeps that as the floor and puts a cheaper exchange in front of it.
//
//	host                                  guest
//	 |  manifest (root + section hashes)    |
//	 |------------------------------------->|  index own world, compare roots
//	 |                                      |
//	 |  request: converged, or page hashes  |
//	 |<-------------------------------------|
//	 |                                      |
//	 |  shard set: only mismatching pages   |
//	 |------------------------------------->|  splice, verify root, install
//
// Three things about the shape are load-bearing.
//
// **The descent happens where the content is.** The guest sends its own page
// hashes for the sections that disagreed, so the host compares against content it
// already holds and answers in one round trip. Asking the host for its page hashes
// first would cost two.
//
// **Silence falls back rather than stalls.** Every manifest is answered, so a peer
// that stops answering is a peer whose uplink cannot reach the authority — a relay
// participant, or a broken return path. After SnapshotManifestSilenceCorrections
// the host stops assuming it can repair that peer selectively and publishes the
// Phase 5 body again, which reaches it by the same flood the artifacts use. No
// peer is ever left with an index it cannot act on.
//
// **The keyframe schedule is untouched.** A whole compressed capture still goes
// out every keyframe period, so the convergence floor, the maximum repair age and
// the recovery from any refusal are exactly what they were. Everything here is an
// optimisation of the interval between keyframes, and every failure in it ends at
// the keyframe that was going to be sent anyway.
package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// retainedCapture is one published capture and its index, kept so the host can
// answer a request that names it.
//
// Retention is what makes the round trip sound: the guest compares against the
// manifest for tick T and the host must answer from the capture at tick T, not
// from whatever it holds when the request arrives. A request naming a tick that
// has fallen out of the ring is answered with a keyframe.
type retainedCapture struct {
	tick  uint64
	term  network.AuthorityTerm
	root  uint64
	index *snapshot.Manifest

	// authored marks a record this instance produced rather than adopted. An
	// authority's retention is authored; a receiver's — and therefore a relay's —
	// is not, and the difference is what the relay role rests on: an adopted record
	// may be served only because its root is provably the authority's, so a
	// substituted page fails the same check that catches a corrupt wire.
	authored bool
}

// pendingRequest is one receiver's answer to a manifest, waiting to be served.
//
// Requests are queued rather than served on arrival because they arrive under the
// world lock, inside the tick that drained the frame, and building a repair means
// hashing and compressing. Both belong outside the lock, so this holds bytes and
// nothing else.
type pendingRequest struct {
	from uint32
	body []byte
}

// awaitingRepair is the state a receiver keeps between sending a request and the
// repair arriving: the capture it compared, the index over it, and the manifest it
// is answering.
//
// A bounded few are outstanding at once, because a cadence can elapse between the
// request and the repair and the receiver has answered a newer manifest by then.
// Holding only the newest cost a repair for every round trip longer than one
// cadence — measured at about a fifth of them over a real socket — and each of
// those is a cadence of freshness spent on nothing.
//
// What the several entries do *not* do is combine. Each is a whole baseline with
// its own capture, index and root, a repair is matched to exactly one of them by
// tick, and applying one drops it and everything older — so a repair is spliced
// into the state its manifest described and no other, and an older repair that
// arrives after a newer one has landed matches nothing and is refused. That is
// "a newer repair may replace an older incomplete one, never combine with it
// accidentally", with the replacement made explicit rather than implied by there
// being room for one.
type awaitingRepair struct {
	tick     uint64
	capture  snapshot.SharedCapture
	index    *snapshot.Manifest
	manifest snapshot.CorrectionManifest
	from     uint32
}

// selectiveState is the Phase 6 half of the correction protocol, on whichever side
// this run turns out to be.
//
// The whole of it is covered by selectiveMu rather than by publishMu, and that is
// a lock-order rule rather than a preference. The established order in this file
// is publishMu, then the world lock: readWorld and takeKeyframe are called with
// the publication schedule held and acquire the world lock themselves. Inbound
// frames arrive the other way round — under the world lock, from the tick that
// drained them — so a queue they append to may not sit behind publishMu, or a pump
// reading the world and a tick taking a request would each hold what the other is
// waiting for. selectiveMu is taken either with no world lock or with the world
// lock already held, and never while acquiring one.
type selectiveState struct {
	// Host: the retention ring and the request queue.
	retained []retainedCapture
	requests []pendingRequest

	// Guest: the newest manifest that has arrived and not been answered, the
	// baselines whose repairs are outstanding, the repairs that have arrived, and
	// the cannot-serve answers a relaying neighbour returned instead of one.
	manifests [][]byte
	shardSets [][]byte
	unserved  [][]byte
	awaiting  []*awaitingRepair

	// forward is the newest manifest this instance has answered and not yet passed
	// on, held until it can actually serve the tick the manifest names.
	//
	// Forwarding earlier would be forwarding a question this instance cannot
	// answer: a relay that has itself asked for a repair does not hold that tick
	// yet, so a request coming back for it would be refused and the participant
	// behind would degrade for no reason. Waiting one exchange costs the relayed
	// participant a cadence of freshness and buys it the whole selective path.
	forward     []byte
	forwardTick uint64
	forwardFrom uint32

	// wantKeyframe records that this receiver has asked for a whole world and is
	// waiting for one, so a manifest arriving in the meantime is answered with the
	// same request rather than starting a repair that cannot help.
	wantKeyframe bool

	// source is the participant this receiver's manifests arrive from, which is
	// where its answers go. It is the authority on a direct link and the relaying
	// neighbour otherwise — a receiver answers whoever asked it, and the protocol
	// stays honest about not knowing the topology.
	source uint32
}

// === host: publishing the index ===

// publishManifest sends the index for one capture to the peers that are in the
// selective protocol, and reports whether every due peer was covered.
//
// A false return is what makes the caller send a whole body as well: it means at
// least one due peer has not answered a manifest recently enough to be repaired
// selectively, and that peer is owed the Phase 5 correction it would otherwise
// have received.
//
// Caller MUST hold publishMu, and MUST NOT hold the world lock.
func (c *corrections) publishManifest(port engine.NetworkPort, index *snapshot.Manifest, due []uint32) (bool, error) {
	started := time.Now() // [wall] telemetry only; outside the world lock
	body, err := snapshot.EncodeManifest(index.Summary())
	if err != nil {
		return false, fmt.Errorf("correction manifest encode: %w", err)
	}
	if len(body) > network.MaxPayloadSize {
		// A manifest is section summaries and a header; outgrowing a frame would
		// mean the world had grown a section vector no descent could be cheap
		// over. Say so and let the caller fall back rather than chunk it.
		return false, fmt.Errorf("correction manifest is %d bytes, past one frame", len(body))
	}
	tick := index.Summary().Header.Tick

	m := c.a.snapshotTelemetry
	m.hashUS.Store(time.Since(started).Microseconds())

	covered, sent := true, 0
	for _, id := range due {
		p := c.peers[id]
		if p == nil {
			continue
		}
		if p.wide > 0 {
			// Diverged too widely to repair last time; the caller owes it a body.
			p.wide--
			covered = false
			continue
		}
		if p.silence >= parameter.SnapshotManifestSilenceCorrections {
			covered = false
			continue
		}
		if !port.Send(id, uint8(network.MsgStateManifest), body) {
			p.refused++
			covered = false
			continue
		}
		p.manifestTick = tick
		p.silence++
		sent++
	}
	if sent > 0 {
		m.manifestSent.Add(1)
		m.manifestBytesSent.Add(int64(len(body) * sent))
		c.recordSelectiveSizeLocked(len(body))
	}
	return covered, nil
}

// retainLocked adds one capture and its index to the bounded ring.
// Caller MUST hold publishMu.
func (c *corrections) retainLocked(cap snapshot.SharedCapture, index *snapshot.Manifest, authored bool) {
	c.selective.retained = append(c.selective.retained, retainedCapture{
		tick: cap.Header.Tick, term: cap.Header.Term, root: index.Root(),
		index: index, authored: authored,
	})
	if n := len(c.selective.retained); n > parameter.SnapshotManifestRetention {
		c.selective.retained = append(c.selective.retained[:0], c.selective.retained[n-parameter.SnapshotManifestRetention:]...)
	}
	c.a.snapshotTelemetry.relayRetained.Store(int64(len(c.selective.retained)))
}

// retain is retainLocked for a caller that holds no lock, which is every receiver
// path: a receiver retains what it has just proved it holds, and it does so
// outside the publication schedule because it is not publishing.
func (c *corrections) retain(cap snapshot.SharedCapture, index *snapshot.Manifest, authored bool) {
	c.publishMu.Lock()
	c.retainLocked(cap, index, authored)
	c.publishMu.Unlock()
}

// retentionEvidence is what this instance can prove it holds: the newest
// authoritative tick it has an index over, and how many such records it has.
//
// It is the succession's requirement (b), and the reason it reads the retained
// ring rather than taking a capture is that a fresh capture proves only what the
// candidate believes. A record is in this ring only because its root was the
// authority's — the capture arrived whole and passed its integrity hash, or the
// receiver's own index reproduced the authority's root — so the ring is evidence
// about the session rather than about the instance.
func (c *corrections) retentionEvidence() (uint64, int) {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	newest := uint64(0)
	for _, r := range c.selective.retained {
		newest = max(newest, r.tick)
	}
	return newest, len(c.selective.retained)
}

// retainInstalled records the index over a capture this instance has just
// installed whole, so it can answer for the authority afterwards.
//
// This is the primitive both Phase 7 deliverables rest on. A successor needs it to
// prove its world is at least as new as the last artifact the old authority
// published; a relay needs it to answer a request for a participant behind it. In
// both cases what makes the record usable is that the capture *is* the
// authority's, byte for byte — a whole correction re-checks its own integrity hash
// before it installs — so an index built over it carries the authority's root.
func (c *corrections) retainInstalled(cap snapshot.SharedCapture) {
	if cap.Header.Term == 0 {
		return // not an authoritative artifact: nothing to answer for
	}
	index, err := snapshot.BuildManifest(cap, cap.Header.Authority)
	if err != nil {
		return
	}
	c.retain(cap, index, false)
}

// retainedAtLocked finds the capture a request names. Caller MUST hold publishMu.
func (c *corrections) retainedAtLocked(tick uint64) (retainedCapture, bool) {
	for i := len(c.selective.retained) - 1; i >= 0; i-- {
		if c.selective.retained[i].tick == tick {
			return c.selective.retained[i], true
		}
	}
	return retainedCapture{}, false
}

// === host: serving a repair ===

// serveRequests answers every queued request.
//
// It runs between two ticks, on whatever drives this instance: a driven host
// reaches it from Tick and an interactive one from the pump. Nothing here holds
// the world lock — the captures being compared were read on the cadence and the
// hashing was done then.
func (c *corrections) serveRequests() {
	c.selectiveMu.Lock()
	pending := c.selective.requests
	c.selective.requests = nil
	c.selectiveMu.Unlock()
	if len(pending) == 0 {
		return
	}
	port := c.a.sessionTransport()
	for _, req := range pending {
		c.serveOne(port, req)
	}
}

// serveOne answers one request, falling back to a keyframe whenever a repair
// cannot be built or would not be worth building.
func (c *corrections) serveOne(port engine.NetworkPort, pending pendingRequest) {
	m := c.a.snapshotTelemetry
	req, err := snapshot.DecodeCorrectionRequest(pending.body)
	if err != nil {
		m.shardsRefused.Add(1)
		vlog.Debug("app", "msg", "correction request refused",
			"peer", pending.from, "error", err.Error())
		return
	}
	m.requestBytes.Add(int64(len(pending.body)))

	if !c.a.admitArtifactTerm(req.Term, pending.from) {
		m.shardsRefused.Add(1)
		return
	}
	c.publishMu.Lock()
	p := c.peers[pending.from]
	if p != nil {
		p.silence = 0
		p.answeredTick = req.Tick
		p.converged = req.Converged()
		p.relayed = slices.Clone(req.Relayed)
	}
	if req.Version != snapshot.ManifestVersion || req.Schema != snapshot.Schema {
		c.publishMu.Unlock()
		m.shardsRefused.Add(1)
		vlog.Debug("app", "msg", "correction request refused",
			"peer", pending.from, "version", req.Version, "schema", req.Schema)
		return
	}
	if req.Converged() {
		c.publishMu.Unlock()
		return // the cheapest correction there is: the receiver already agrees
	}

	held, ok := c.retainedAtLocked(req.Tick)
	authored := ok && held.authored
	c.publishMu.Unlock()

	// A request this instance did not author the answer to is a relayed one. It is
	// served from retention if that retention holds the tick, and refused in words
	// if it does not — never with a body from a different baseline, and never by
	// forwarding the request onward, which would be the routing layer this protocol
	// deliberately does not have.
	if !authored {
		if req.Keyframe {
			c.sendUnserved(port, pending.from, req, "a relay cannot author a whole world")
			return
		}
		if c.serveRelayed(port, pending, req) {
			return
		}
		c.sendUnserved(port, pending.from, req, "this participant retains no index for that tick")
		return
	}

	c.publishMu.Lock()
	held, ok = c.retainedAtLocked(req.Tick)
	if !ok || req.Keyframe {
		c.publishMu.Unlock()
		c.sendKeyframeTo(port, pending.from, req.Tick)
		return
	}
	m.shardsRequested.Add(int64(countRequestedPages(req)))
	set, pages, err := snapshot.BuildShardSet(held.index, req)
	c.publishMu.Unlock()
	if err != nil {
		m.shardsRefused.Add(1)
		vlog.Debug("app", "msg", "repair not built",
			"peer", pending.from, "tick", req.Tick, "error", err.Error())
		c.sendKeyframeTo(port, pending.from, req.Tick)
		return
	}

	body, err := snapshot.EncodeShardSet(set)
	if err != nil || !c.repairIsWorthSending(len(body)) {
		// A repair this wide is not repairing anything: past the frame bound it
		// does not fit, and past the measured keyframe size the whole world is
		// smaller and needs no round trip to have been asked for. What that says
		// about the peer is that its prediction is not tracking — a storm moves
		// the entire shared population every cadence, and no index makes that
		// cheap — so the peer is dropped out of the exchange for a few
		// publications and served the Phase 5 body instead, which is the cheapest
		// thing anyone has for a receiver that has diverged wholesale. This
		// correction is still answered — with the whole world, which is what the
		// peer needs and what it would otherwise wait a cadence for — and the
		// index is tried again afterwards, because the condition is the world's
		// rather than the peer's and it ends when the storm does.
		m.keyframeFallback.Add(1)
		c.widenLocked(pending.from)
		c.sendKeyframeTo(port, pending.from, req.Tick)
		return
	}
	if port == nil || !port.Send(pending.from, uint8(network.MsgStateShard), body) {
		m.shardsRefused.Add(1)
		return
	}
	m.shardsSent.Add(int64(pages))
	m.shardBytesSent.Add(int64(len(body)))
	c.publishMu.Lock()
	c.recordSelectiveSizeLocked(len(body))
	c.publishMu.Unlock()
	vlog.Debug("app", "msg", "repair sent",
		"peer", pending.from, "tick", req.Tick, "pages", pages, "bytes", len(body))
}

// widenLocked drops one peer out of the selective exchange for the next few
// publications, so it is served the ordinary correction body instead.
func (c *corrections) widenLocked(id uint32) {
	c.publishMu.Lock()
	if p := c.peers[id]; p != nil {
		p.wide = parameter.SnapshotManifestSilenceCorrections
	}
	c.publishMu.Unlock()
}

// repairIsWorthSending reports whether a repair of this size is still cheaper than
// the whole world it stands in for.
//
// Two bounds, and they answer different questions. SnapshotShardBytesMax is the
// protocol's: past it a repair no longer fits one transport frame. The measured
// keyframe size is the session's: a repair wider than the capture it is repairing
// toward has stopped being an optimisation, and the fallback is what keeps the
// selective path from ever costing more than the Phase 5 stream it replaced.
func (c *corrections) repairIsWorthSending(bytes int) bool {
	if bytes > parameter.SnapshotShardBytesMax || bytes > network.MaxPayloadSize {
		return false
	}
	c.publishMu.Lock()
	keyframe := c.sizes.Keyframe
	c.publishMu.Unlock()
	return keyframe == 0 || int64(bytes) < keyframe
}

// countRequestedPages is how many pages a request put in play, which is the unit
// the shard counters are reported in.
func countRequestedPages(req snapshot.CorrectionRequest) int {
	n := 0
	for _, s := range req.Sections {
		n += len(s.Hash)
	}
	return n
}

// sendKeyframeTo pushes a whole compressed capture at one peer. It is the bounded
// fallback every refusal in this protocol reaches.
//
// minTick is the tick the receiver was asking about, and the keyframe has to be at
// least that fresh. The retained baseline usually is not: it is the last *whole*
// world this host published, which on a healthy link is up to a keyframe period
// old, and a receiver refuses an authority it has already moved past. So a stale
// baseline is refreshed by reading the world — the one place in this protocol that
// pays a capture outside the cadence, and only on a path that has already given up
// on repairing selectively.
func (c *corrections) sendKeyframeTo(port engine.NetworkPort, id uint32, minTick uint64) {
	if port == nil {
		return
	}
	c.publishMu.Lock()
	if !c.haveKey || c.baseline.Header.Tick < minTick {
		if _, _, err := c.takeKeyframe(); err != nil {
			c.publishMu.Unlock()
			vlog.Warn("app", "msg", "keyframe fallback capture", "error", err.Error())
			return
		}
	}
	cap := c.baseline
	// The peer is being served a whole world, so its standing in the selective
	// exchange starts again from the state it is about to hold.
	if p := c.peers[id]; p != nil {
		p.silence = 0
	}
	c.publishMu.Unlock()

	body, err := snapshot.EncodeCorrection(cap)
	if err != nil {
		vlog.Warn("app", "msg", "keyframe fallback encode", "error", err.Error())
		return
	}
	chunks, err := network.EncodeSnapshotChunks(cap.Header.Tick, body)
	if err != nil {
		vlog.Warn("app", "msg", "keyframe fallback chunk", "error", err.Error())
		return
	}
	if !c.sendTo(port, id, chunks) {
		return
	}
	// Counted where every other correction body is: a fallback is not free, and a
	// wire total that omitted it would flatter the protocol that provoked it.
	c.a.snapshotTelemetry.sentBytes.Add(int64(len(body)))
	c.a.snapshotTelemetry.keyframeFallback.Add(1)
	vlog.Debug("app", "msg", "keyframe fallback sent",
		"peer", id, "tick", cap.Header.Tick, "bytes", len(body))
}

// === guest: answering the index ===

// applySelective drains whatever selective traffic has arrived and returns the
// capture a repair produced, if one did.
//
// The order is deliberate: a repair that has arrived is applied before a newer
// manifest is answered, because the repair is state and the manifest is only a
// question. A manifest newer than the repair being awaited then supersedes it, so
// nothing older is ever installed after something newer.
func (c *corrections) applySelective() {
	c.selectiveMu.Lock()
	manifests := c.selective.manifests
	shardSets := c.selective.shardSets
	unserved := c.selective.unserved
	from := c.selective.source
	c.selective.manifests, c.selective.shardSets, c.selective.unserved = nil, nil, nil
	c.selectiveMu.Unlock()

	for _, body := range shardSets {
		c.applyRepair(body)
	}
	for _, body := range unserved {
		c.applyUnserved(body)
	}
	if len(manifests) == 0 {
		return
	}
	// Only the newest manifest is answered. An older one describes a state the
	// authority has already moved past, and answering it would spend a round trip
	// repairing this instance onto a world nobody holds any more.
	newest := manifests[len(manifests)-1]
	tick := c.answerManifest(newest, int64(len(manifests)))
	if tick == 0 {
		return // refused; there is nothing worth passing on
	}
	c.holdForward(newest, from, tick)
	c.flushForward()
}

// holdForward records the newest manifest this instance has answered, replacing
// whatever it was holding: an older one describes a state the authority has moved
// past, and passing it on would send the participants behind this one to repair
// themselves onto a world nobody holds.
func (c *corrections) holdForward(body []byte, from uint32, tick uint64) {
	c.selectiveMu.Lock()
	c.selective.forward, c.selective.forwardTick, c.selective.forwardFrom = body, tick, from
	c.selectiveMu.Unlock()
}

// flushForward passes the held manifest on once this instance can answer for the
// tick it names.
func (c *corrections) flushForward() {
	c.selectiveMu.Lock()
	body, tick, from := c.selective.forward, c.selective.forwardTick, c.selective.forwardFrom
	c.selectiveMu.Unlock()
	if body == nil || !c.holdsRetention(tick) {
		return
	}
	c.selectiveMu.Lock()
	c.selective.forward, c.selective.forwardTick = nil, 0
	c.selectiveMu.Unlock()
	c.forwardManifest(body, from, tick)
}

// holdsRetention reports whether this instance can answer a request naming tick.
func (c *corrections) holdsRetention(tick uint64) bool {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	_, ok := c.retainedAtLocked(tick)
	return ok
}

// answerManifest indexes this instance's own world against one manifest and
// answers it.
func (c *corrections) answerManifest(body []byte, arrived int64) uint64 {
	m := c.a.snapshotTelemetry
	m.manifestRecv.Add(arrived)
	m.manifestBytesRecv.Add(int64(len(body)))

	want, err := snapshot.DecodeManifest(body)
	if err != nil {
		m.baselineRefusals.Add(1)
		vlog.Debug("app", "msg", "manifest refused", "error", err.Error())
		return 0
	}
	from := c.selectiveSource()
	if !c.a.admitArtifactTerm(want.Header.Term, from) {
		m.baselineRefusals.Add(1)
		return 0
	}
	if want.Version != snapshot.ManifestVersion || want.Header.Schema != snapshot.Schema {
		m.baselineRefusals.Add(1)
		c.requestKeyframe(from, want)
		return 0
	}
	if err := c.a.verifyCaptureIdentity(want.Header); err != nil {
		m.baselineRefusals.Add(1)
		vlog.Debug("app", "msg", "manifest describes another session", "error", err.Error())
		return 0
	}

	mine, err := c.a.CaptureShared()
	if err != nil {
		vlog.Warn("app", "msg", "manifest comparison capture", "error", err.Error())
		return 0
	}
	// Compared under the authority's term rather than this instance's. The two are
	// the same in the ordinary case; across a handoff a receiver that has adopted
	// the record may still be holding a capture stamped a moment earlier, and
	// comparing under two generations would make every root differ for a reason
	// that is not a disagreement about the world.
	mine.Header.Term = want.Header.Term
	started := time.Now() // [wall] telemetry only; outside the world lock
	index, err := snapshot.BuildManifest(mine, want.Authority)
	if err != nil {
		vlog.Warn("app", "msg", "manifest comparison index", "error", err.Error())
		return 0
	}
	req, sections, pages := snapshot.CompareRequest(index, want)
	req.Term = want.Header.Term
	m.hashUS.Store(time.Since(started).Microseconds())
	m.sectionsCompared.Add(int64(sections))
	m.pagesCompared.Add(int64(pages))

	if c.selective.wantKeyframe {
		req.Keyframe = true
		req.Sections = nil
	}
	// What this instance can answer for, stated where the authority will read it.
	req.Relayed = c.relayedParticipants()
	c.sendRequest(from, req)

	if req.Converged() {
		// The whole point of the protocol: the roots agree, so no state travels.
		// The header still does — the authority's tick, run and map bounds are what
		// an install adopts, and a guest that kept its own would keep predicting
		// from a clock the session has moved past.
		m.hashOnly.Add(1)
		mine.Header = want.Header
		if integrity, err := snapshot.Integrity(mine); err == nil {
			mine.Header.Integrity = integrity
			if err := c.install(mine); err != nil {
				vlog.Debug("app", "msg", "hash-only correction not applied", "error", err.Error())
			}
		}
		c.selectiveMu.Lock()
		c.selective.awaiting = nil
		c.selectiveMu.Unlock()
		return want.Header.Tick
	}

	c.selectiveMu.Lock()
	c.selective.awaiting = append(c.selective.awaiting, &awaitingRepair{
		tick: want.Header.Tick, capture: mine, index: index,
		manifest: want, from: from,
	})
	if n := len(c.selective.awaiting); n > parameter.SnapshotCorrectionQueue {
		c.selective.awaiting = append(c.selective.awaiting[:0],
			c.selective.awaiting[n-parameter.SnapshotCorrectionQueue:]...)
	}
	c.selectiveMu.Unlock()
	return want.Header.Tick
}

// takeAwaiting claims the outstanding baseline a repair answers, dropping it and
// every older one. A repair matching nothing outstanding is one this instance has
// already moved past.
func (c *corrections) takeAwaiting(tick uint64) *awaitingRepair {
	c.selectiveMu.Lock()
	defer c.selectiveMu.Unlock()
	for i, a := range c.selective.awaiting {
		if a.tick != tick {
			continue
		}
		c.selective.awaiting = append(c.selective.awaiting[:0], c.selective.awaiting[i+1:]...)
		return a
	}
	return nil
}

// applyRepair validates and installs one shard set.
//
// Nothing is written anywhere until the whole set has passed validation and the
// repaired capture has reproduced the set's root. A failure at either point leaves
// the awaited state untouched and asks for a keyframe, which is the one answer
// that cannot itself fail for the same reason.
func (c *corrections) applyRepair(body []byte) {
	m := c.a.snapshotTelemetry
	m.shardBytesRecv.Add(int64(len(body)))

	set, err := snapshot.DecodeShardSet(body)
	if err != nil {
		m.shardsRefused.Add(1)
		vlog.Debug("app", "msg", "repair refused", "error", err.Error())
		return
	}
	m.shardsRecv.Add(int64(len(set.Shards)))

	if !c.a.admitArtifactTerm(set.Header.Term, set.Served) {
		m.shardsRefused.Add(1)
		return
	}
	awaiting := c.takeAwaiting(set.Header.Tick)
	if awaiting == nil {
		m.baselineRefusals.Add(1)
		m.shardsRefused.Add(1)
		vlog.Debug("app", "msg", "repair names a baseline this instance has moved past",
			"repair_tick", set.Header.Tick)
		return
	}
	if err := snapshot.ValidateShardSet(set, awaiting.tick, awaiting.manifest.Authority, awaiting.manifest.Root, awaiting.manifest.Header); err != nil {
		m.proofFailures.Add(1)
		m.shardsRefused.Add(1)
		vlog.Warn("app", "msg", "repair failed its proof", "error", err.Error())
		c.requestKeyframe(awaiting.from, awaiting.manifest)
		return
	}

	// The splice runs on a copy, so a failure past this point leaves the awaited
	// capture exactly as it was rather than half-repaired.
	repaired := awaiting.capture
	index := awaiting.index
	rep, err := snapshot.ApplyShardSet(&repaired, index, set)
	if err != nil {
		m.proofFailures.Add(1)
		m.shardsRefused.Add(1)
		vlog.Warn("app", "msg", "repair did not verify", "error", err.Error())
		c.requestKeyframe(awaiting.from, awaiting.manifest)
		return
	}

	if err := c.install(repaired); err != nil {
		vlog.Warn("app", "msg", "repair not installed",
			"tick", repaired.Header.Tick, "error", err.Error())
		c.requestKeyframe(awaiting.from, awaiting.manifest)
		return
	}
	// Installing may be what finally lets this instance answer for the tick it is
	// holding a manifest to forward for.
	c.flushForward()
	m.shardsApplied.Add(int64(rep.Pages))
	m.pagesRepaired.Add(int64(rep.Pages))
	m.entitiesRepaired.Add(int64(rep.Entities))
	m.cellsRepaired.Add(int64(rep.Rows))
	vlog.Debug("app", "msg", "repair applied",
		"tick", repaired.Header.Tick, "pages", rep.Pages,
		"sections", rep.Sections, "cells", rep.Rows)
}

// requestKeyframe asks the authority for a whole world and remembers that it is
// waiting for one, so the next manifest does not start a repair instead.
func (c *corrections) requestKeyframe(from uint32, want snapshot.CorrectionManifest) {
	c.selectiveMu.Lock()
	c.selective.wantKeyframe = true
	c.selective.awaiting = nil
	c.selectiveMu.Unlock()
	c.a.snapshotTelemetry.keyframeFallback.Add(1)
	c.sendRequest(from, snapshot.CorrectionRequest{
		Version:  snapshot.ManifestVersion,
		Schema:   snapshot.Schema,
		Tick:     want.Header.Tick,
		Run:      want.Header.Run,
		Session:  want.Header.Session,
		Term:     want.Header.Term,
		Keyframe: true,
	})
}

// sendRequest returns one answer to the peer the manifest came from.
func (c *corrections) sendRequest(from uint32, req snapshot.CorrectionRequest) {
	port := c.a.sessionTransport()
	if port == nil || from == 0 {
		return
	}
	body, err := snapshot.EncodeCorrectionRequest(req)
	if err != nil {
		vlog.Warn("app", "msg", "correction request encode", "error", err.Error())
		return
	}
	if len(body) > network.MaxPayloadSize {
		// A page vector this wide means the disagreement is not page-shaped. Ask
		// for the whole world instead of describing the difference.
		c.sendRequest(from, snapshot.CorrectionRequest{
			Version: snapshot.ManifestVersion, Schema: snapshot.Schema,
			Tick: req.Tick, Run: req.Run, Session: req.Session, Term: req.Term,
			Keyframe: true,
		})
		return
	}
	if !port.Send(from, uint8(network.MsgStateRequest), body) {
		return
	}
	c.a.snapshotTelemetry.requestBytes.Add(int64(len(body)))
}

// selectiveSource is the participant a receiver answers: the peer its manifests
// arrive from, which is the authority or the neighbour relaying for it.
func (c *corrections) selectiveSource() uint32 {
	c.selectiveMu.Lock()
	defer c.selectiveMu.Unlock()
	return c.selective.source
}

// receiveSelective queues one selective frame. Caller holds the world lock, so it
// takes the bytes and nothing else.
func (c *corrections) receiveSelective(kind uint8, from uint32, body []byte) {
	c.selectiveMu.Lock()
	switch network.MessageType(kind) {
	case network.MsgStateManifest:
		c.selective.source = from
		c.selective.manifests = append(c.selective.manifests, body)
		if n := len(c.selective.manifests); n > parameter.SnapshotCorrectionQueue {
			c.selective.manifests = append(c.selective.manifests[:0], c.selective.manifests[n-parameter.SnapshotCorrectionQueue:]...)
		}
	case network.MsgStateShard:
		c.selective.shardSets = append(c.selective.shardSets, body)
		if n := len(c.selective.shardSets); n > parameter.SnapshotCorrectionQueue {
			c.selective.shardSets = append(c.selective.shardSets[:0], c.selective.shardSets[n-parameter.SnapshotCorrectionQueue:]...)
		}
	case network.MsgStateUnserved:
		if len(c.selective.unserved) < parameter.SnapshotCorrectionQueue {
			c.selective.unserved = append(c.selective.unserved, body)
		}
	case network.MsgStateRequest:
		if len(c.selective.requests) < parameter.MaxPlayers*parameter.SnapshotCorrectionQueue {
			c.selective.requests = append(c.selective.requests, pendingRequest{from: from, body: body})
		}
	}
	c.selectiveMu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// clearKeyframeWait is called when a whole capture is installed, which is the
// answer a keyframe request was waiting for.
func (c *corrections) clearKeyframeWait() {
	c.selectiveMu.Lock()
	c.selective.wantKeyframe = false
	c.selective.awaiting = nil
	c.selectiveMu.Unlock()
}

// recordSelectiveSizeLocked prices the schedule from what the selective protocol
// actually puts on the wire.
//
// The controller's Delta figure is what a non-keyframe correction costs, and a
// non-keyframe correction is now a manifest plus whatever repair it provoked. It
// is deliberately blended into the same field rather than added beside it: the
// controller's cost model is "keyframe, and the other thing", and giving it a
// third number would let a schedule be priced from a shape it does not send.
//
// Caller MUST hold publishMu.
func (c *corrections) recordSelectiveSizeLocked(bytes int) {
	const smoothing = 0.25
	if bytes <= 0 {
		return
	}
	if c.sizes.Delta == 0 {
		c.sizes.Delta = int64(bytes)
	} else {
		c.sizes.Delta += int64(smoothing * float64(int64(bytes)-c.sizes.Delta))
	}
	if c.sizes.Keyframe > 0 {
		c.haveSizes = true
	}
	c.a.snapshotTelemetry.selectiveBytes.Store(c.sizes.Delta)
}

// localParticipant is this instance's session identity, zero outside a session.
func (a *App) localParticipant() uint32 {
	var id uint32
	a.world.RunSafe(func() { id = a.localParticipantLocked() })
	return id
}

// verifyCaptureIdentity answers "is this header describing my session" without
// requiring the body a full verification hashes.
func (a *App) verifyCaptureIdentity(h snapshot.CaptureHeader) error {
	if h.Schema != snapshot.Schema {
		return fmt.Errorf("capture schema %d, this build reads %d", h.Schema, snapshot.Schema)
	}
	return firstAnchorMismatch("manifest", a.anchorIdentity(snapshot.Anchor(h)))
}

// selectiveSummary is what the selective protocol is currently doing, for the
// diagnostics surface and for the tests that assert the protocol rather than its
// effect.
type selectiveSummary struct {
	Retained  []uint64
	Awaiting  uint64
	Requests  int
	Keyframe  bool
	PeerState map[uint32]PeerSelective
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

// SelectiveReport describes the selective exchange, for `:session` and for tests.
func (a *App) SelectiveReport() selectiveSummary {
	out := selectiveSummary{PeerState: map[uint32]PeerSelective{}}
	c := a.corrections
	if c == nil {
		return out
	}
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

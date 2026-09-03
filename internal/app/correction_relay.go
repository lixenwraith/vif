// Package app: answering for the participants behind you.
//
// The selective exchange runs between an authority and a receiver that can answer
// it, and a relayed receiver's request goes to the neighbour that forwarded the
// manifest. That neighbour therefore has to hold something.
//
// Every instance retains an index over each authoritative capture it can prove it
// holds, bounded by the same SnapshotManifestRetention the authority uses, and a
// participant with more than one link forwards the manifest onward and answers
// from that retention. Four properties make it a role rather than a routing layer:
//
//   - One hop. A relay that does not hold the manifest a request names does
//     not forward the request onward. It says so, and the receiver degrades to the
//     whole body the authority's keyframe cadence is already flooding.
//
//   - A relay cannot forge. It serves pages it did not author, so what binds
//     the answer is the authority's own root: the set must declare the root the
//     receiver was sent in the manifest, and the repaired capture must reproduce
//     it. A substituted, truncated or corrupted page fails one of the two, by the
//     same check that catches a corrupt wire.
//
//   - Retention is why it may answer at all. An index enters the ring only
//     when the capture under it is provably the authority's — a whole correction
//     re-checked its own integrity hash, or a comparison reproduced the
//     authority's root — so a relay never holds a baseline of its own to serve
//     from, and mixed-baseline assembly stays unreachable.
//
//   - The edge that carries it pays for it. A relayed repair is priced into
//     the relaying participant's own link plan, never the authority's.
package app

import (
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// sessionRole is what this instance is doing in the protocol right now.
func (c *corrections) sessionRole() network.Role {
	links := 0
	if link, ok := c.a.sessionTransport().(engine.LinkMeasuringPort); ok {
		links = len(link.Peers())
	}
	return network.SessionRole(c.a.authority != nil && c.a.authority.IsAuthority(), links)
}

// canRelay reports whether this instance can answer for a participant behind it:
// it is not the authority, it has more than one link, and it holds retention it
// could serve from.
//
// The retention test is what makes the claim honest rather than optimistic. A
// participant that has never held an authoritative capture cannot answer anything,
// and saying otherwise upstream would leave the participants behind it receiving
// an index nobody can act on — the failure the answerability gate exists to
// prevent and which this role has to keep preventing.
func (c *corrections) canRelay() bool {
	if c.sessionRole() != network.RoleRelay {
		return false
	}
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	return len(c.selective.retained) > 0
}

// forwardManifest passes one manifest to the participants behind this one and
// records which they were, so the answer this instance sends upstream can say who
// it is answering for.
//
// It runs after this instance has answered the manifest itself, which is what puts
// the tick in its retention before a request naming it can arrive.
func (c *corrections) forwardManifest(body []byte, from uint32, tick uint64) {
	if !c.canRelay() {
		return
	}
	link, ok := c.a.sessionTransport().(engine.LinkMeasuringPort)
	if !ok {
		return
	}
	behind := behindLinks(link.Peers(), from)
	if len(behind) == 0 {
		return
	}
	port := c.a.sessionTransport()
	sent := 0
	for _, id := range behind {
		if port.Send(id, uint8(network.MsgStateManifest), body) {
			sent++
		}
	}
	if sent == 0 {
		return
	}
	c.a.snapshotTelemetry.relayBytesSent.Add(int64(len(body) * sent))
	vlog.Debug("app", "msg", "manifest relayed", "tick", tick, "from", from, "to", sent)
}

// behindLinks is this instance's links other than the one an index arrives on,
// which is the set it forwards to and answers for.
func behindLinks(peers []uint32, from uint32) []uint32 {
	out := make([]uint32, 0, len(peers))
	for _, id := range peers {
		if id != from {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// relayedParticipants is who this instance can answer for, which is what its own
// answer carries upstream.
//
// It is a statement of capability rather than a record of what was forwarded, and
// the difference is what breaks the deadlock the gate would otherwise have: the
// authority withholds the index while a participant is unanswerable, and a relay
// that only ever reported what it had already forwarded could never forward
// anything to report.
func (c *corrections) relayedParticipants() []uint32 {
	if !c.canRelay() {
		return nil
	}
	link, ok := c.a.sessionTransport().(engine.LinkMeasuringPort)
	if !ok {
		return nil
	}
	return behindLinks(link.Peers(), c.selectiveSource())
}

// canAnswerEveryParticipant reports whether every participant can be answered — a
// relayed one can when the neighbour forwarding to it holds retention, which that
// neighbour states in its own answer to the manifest since it is the only instance
// that knows.
//
// A session whose relays hold retention keeps the selective stream. A session with
// a relay that cannot answer keeps the whole-body flood, and the reason is
// reported rather than silent.
func (c *corrections) canAnswerEveryParticipant(ids []uint32) bool {
	roster := 0
	c.a.world.RunSafe(func() { roster = c.a.world.Resources.Player.Count() })
	if roster == 0 {
		return true // no roster yet: nobody is behind a relay
	}
	reachable := len(ids) + 1
	if roster <= reachable {
		return true
	}
	covered := make(map[uint32]struct{}, roster)
	for _, id := range ids {
		covered[id] = struct{}{}
	}
	for _, id := range ids {
		p := c.peers[id]
		if p == nil {
			continue
		}
		for _, behind := range p.relayed {
			covered[behind] = struct{}{}
		}
	}
	if len(covered)+1 >= roster {
		return true
	}
	if !c.saidUnrelayed {
		c.saidUnrelayed = true
		vlog.Info("app", "msg", "selective correction withheld; a participant cannot be answered",
			"roster", roster, "direct", len(ids), "answerable", len(covered))
	}
	return false
}

// serveRelayed answers one request from this instance's retention rather than from
// a world it authored.
//
// The answer carries the authority's header, the authority's root and the
// authority's section summaries, because that is what the receiver validates
// against — and this instance holds them only because it once proved it held that
// exact state. Served names this instance, so the bytes are priced against the
// edge that carried them.
func (c *corrections) serveRelayed(port engine.NetworkPort, pending pendingRequest, req snapshot.CorrectionRequest) bool {
	c.publishMu.Lock()
	held, ok := c.retainedAtLocked(req.Tick)
	c.publishMu.Unlock()
	if !ok || held.authored {
		return false
	}
	set, pages, err := snapshot.BuildShardSet(held.index, req)
	if err != nil {
		c.sendUnserved(port, pending.from, req, "the retained index cannot answer this page vector")
		return true
	}
	set.Served = c.a.localParticipant()
	body, err := snapshot.EncodeShardSet(set)
	if err != nil || len(body) > parameter.SnapshotShardBytesMax || len(body) > network.MaxPayloadSize {
		c.sendUnserved(port, pending.from, req, "the repair is wider than a relayed answer may carry")
		return true
	}
	if port == nil || !port.Send(pending.from, uint8(network.MsgStateShard), body) {
		c.a.snapshotTelemetry.shardsRefused.Add(1)
		return true
	}
	m := c.a.snapshotTelemetry
	m.shardsSent.Add(int64(pages))
	m.shardBytesSent.Add(int64(len(body)))
	m.relayServed.Add(1)
	m.relayBytesSent.Add(int64(len(body)))
	// Priced here rather than at the authority: these bytes left this instance's
	// uplink, so they belong to this instance's plan.
	c.publishMu.Lock()
	c.recordSelectiveSizeLocked(len(body))
	c.publishMu.Unlock()
	vlog.Debug("app", "msg", "relayed repair served",
		"peer", pending.from, "tick", req.Tick, "pages", pages, "bytes", len(body))
	return true
}

// sendUnserved tells a receiver this instance cannot produce what it asked for.
//
// It is a message rather than a silence because silence costs the receiver a whole
// cadence waiting for a repair that is not coming, and it is not a body because a
// body from a different baseline is exactly what the supersession rules make
// unreachable. What the receiver does with it is degrade: it stops waiting for the
// repair and takes the next whole authoritative world, which the keyframe cadence
// is flooding anyway.
func (c *corrections) sendUnserved(port engine.NetworkPort, to uint32, req snapshot.CorrectionRequest, why string) {
	c.a.snapshotTelemetry.relayUnserved.Add(1)
	if port == nil {
		return
	}
	body, err := snapshot.EncodeUnserved(snapshot.CorrectionUnserved{
		Version: snapshot.ManifestVersion, Tick: req.Tick, Term: req.Term,
		From: c.a.localParticipant(), Reason: why,
	})
	if err != nil {
		return
	}
	port.Send(to, uint8(network.MsgStateUnserved), body)
	vlog.Debug("app", "msg", "request cannot be served from retention",
		"peer", to, "tick", req.Tick, "reason", why)
}

// applyUnserved is the receiver's half: stop waiting for a repair that is not
// coming and take the next whole world instead.
func (c *corrections) applyUnserved(body []byte) {
	u, err := snapshot.DecodeUnserved(body)
	if err != nil {
		return
	}
	m := c.a.snapshotTelemetry
	m.relayBytesRecv.Add(int64(len(body)))
	m.relayUnserved.Add(1)
	if awaiting := c.takeAwaiting(u.Tick); awaiting == nil {
		return // already superseded; nothing was waiting on this
	}
	c.selectiveMu.Lock()
	c.selective.wantKeyframe = true
	c.selectiveMu.Unlock()
	m.keyframeFallback.Add(1)
	vlog.Debug("app", "msg", "repair unavailable from the relaying neighbour",
		"peer", u.From, "tick", u.Tick, "reason", u.Reason)
}

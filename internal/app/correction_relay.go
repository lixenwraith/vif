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

// canRelay reports whether this instance can answer for a participant behind it: not
// the authority, more than one link, and retention to serve from. The retention test
// is what keeps the claim honest — saying otherwise upstream would leave the
// participants behind it holding an index nobody can act on.
func (c *corrections) canRelay() bool {
	if c.sessionRole() != network.RoleRelay {
		return false
	}
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	return len(c.selective.retained) > 0
}

// forwardManifest passes one manifest to the participants behind this one and
// records who they were, so this instance's own answer can say who it answers for.
// It runs after this instance answered the manifest, which is what puts the tick in
// retention before a request naming it can arrive.
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
	c.a.telemetry.RelayBytesSent.Add(int64(len(body) * sent))
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

// relayedParticipants is who this instance can answer for, carried upstream in its
// own answer. A statement of capability rather than a record of what was forwarded:
// the authority withholds the index while a participant is unanswerable, so a relay
// reporting only past forwards could never forward anything to report.
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

// canAnswerEveryParticipant reports whether every participant can be answered. A
// relayed one can when its forwarding neighbour holds retention, which that
// neighbour states in its own answer since it is the only instance that knows.
// Otherwise the session keeps the whole-body flood, and says why.
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

// serveRelayed answers one request from this instance's retention rather than from a
// world it authored. The answer carries the authority's header, root and section
// summaries, which is what the receiver validates against; Served names this
// instance, so the bytes are priced against the edge that carried them.
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
		c.a.telemetry.ShardsRefused.Add(1)
		return true
	}
	m := c.a.telemetry
	m.ShardsSent.Add(int64(pages))
	m.ShardBytesSent.Add(int64(len(body)))
	m.RelayServed.Add(1)
	m.RelayBytesSent.Add(int64(len(body)))
	// Priced here rather than at the authority: these bytes left this instance's
	// uplink, so they belong to this instance's plan.
	c.publishMu.Lock()
	c.recordSelectiveSizeLocked(len(body))
	c.publishMu.Unlock()
	vlog.Debug("app", "msg", "relayed repair served",
		"peer", pending.from, "tick", req.Tick, "pages", pages, "bytes", len(body))
	return true
}

// sendUnserved tells a receiver this instance cannot produce what it asked for. A
// message rather than a silence, which would cost it a whole cadence; not a body,
// because one from a different baseline is what the supersession rules make
// unreachable. The receiver degrades to the next whole authoritative world.
func (c *corrections) sendUnserved(port engine.NetworkPort, to uint32, req snapshot.CorrectionRequest, why string) {
	c.a.telemetry.RelayUnserved.Add(1)
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
	m := c.a.telemetry
	m.RelayBytesRecv.Add(int64(len(body)))
	m.RelayUnserved.Add(1)
	if awaiting := c.takeAwaiting(u.Tick); awaiting == nil {
		return // already superseded; nothing was waiting on this
	}
	c.selectiveMu.Lock()
	c.selective.wantKeyframe = true
	c.selectiveMu.Unlock()
	m.KeyframeFallback.Add(1)
	vlog.Debug("app", "msg", "repair unavailable from the relaying neighbour",
		"peer", u.From, "tick", u.Tick, "reason", u.Reason)
}

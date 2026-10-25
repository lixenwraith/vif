package converge

import (
	"fmt"
	"slices"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	link "github.com/lixenwraith/vif/pkg/network"
)

// links is the transport and the participants behind this instance, read together
// because both take the world lock and every decision below needs both.
func (c *Corrections) links() (engine.NetworkPort, []uint32) {
	return c.inst.Transport(), c.inst.Downstream()
}

// canRelay reports whether this instance can answer for a participant behind it: not
// the authority, somebody behind it, and retention to serve from. The retention test
// is what keeps the claim honest — saying otherwise upstream would leave the
// participants behind it holding an index nobody can act on.
func (c *Corrections) canRelay(behind []uint32) bool {
	if network.SessionRole(c.authority.isAuthority(), len(behind)) != link.RoleRelay {
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
func (c *Corrections) forwardManifest(body []byte, from uint32, tick uint64) {
	port, peers := c.links()
	if !c.canRelay(peers) {
		return
	}
	behind := behindLinks(peers, from)
	if len(behind) == 0 {
		return
	}
	sent := 0
	for _, id := range behind {
		if port.Send(id, uint8(network.MsgStateManifest), body) {
			sent++
		}
	}
	if sent == 0 {
		return
	}
	c.tel.RelayBytesSent.Add(int64(len(body) * sent))
	c.log.Debug("converge", "msg", "manifest relayed", "tick", tick, "from", from, "to", sent)
}

// behindLinks is the participants behind this instance other than the link an
// index arrives on, which is the set it forwards to and answers for.
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
// own answer with each one's proof, and what they relay in turn. A statement of
// capability rather than a record of what was forwarded: the authority withholds the
// index while a participant is unanswerable, so a relay reporting only past forwards
// could never forward anything to report.
func (c *Corrections) relayedParticipants() []snapshot.RelayedLeaf {
	_, peers := c.links()
	if !c.canRelay(peers) {
		return nil
	}
	behind := behindLinks(peers, c.selectiveSource())
	out := make([]snapshot.RelayedLeaf, 0, len(behind))
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	for _, id := range behind {
		leaf := snapshot.RelayedLeaf{ID: id}
		if p := c.peers[id]; p != nil {
			leaf.Proved = p.provedTick
			out = append(out, p.relayed...)
		}
		out = append(out, leaf)
	}
	return out
}

// provedLocked maps every participant this instance can answer, on a link of its
// own or through a relay that reported it, to the tick its newest matching answer
// proved, zero before any matched. Caller MUST hold publishMu.
func (c *Corrections) provedLocked(ids []uint32) map[uint32]uint64 {
	out := make(map[uint32]uint64, len(ids))
	for _, id := range ids {
		var at uint64
		if p := c.peers[id]; p != nil {
			at = p.provedTick
			for _, leaf := range p.relayed {
				out[leaf.ID] = max(out[leaf.ID], leaf.Proved)
			}
		}
		out[id] = max(out[id], at)
	}
	return out
}

// owedLocked is everyone corrections are owed to: the direct links, and every
// participant the world's roster holds but this one and any away, which nothing
// reaches until it returns. Caller MUST hold publishMu.
func (c *Corrections) owedLocked(ids []uint32) []uint32 {
	out, local := slices.Clone(ids), c.inst.LocalParticipant()
	for _, e := range c.inst.WorldRoster() {
		if id := uint32(e.ID); id != local && !e.Away && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// canAnswerEveryParticipantLocked reports whether every participant can be
// answered. A relayed one can when its forwarding neighbour holds retention, which
// that neighbour states in its own answer since it is the only instance that knows.
// Otherwise the session keeps the whole-body flood, and says why.
// Caller MUST hold publishMu.
func (c *Corrections) canAnswerEveryParticipantLocked(ids []uint32) bool {
	proved := c.provedLocked(ids)
	unanswerable := slices.DeleteFunc(c.owedLocked(ids), func(id uint32) bool {
		_, ok := proved[id]
		return ok
	})
	if len(unanswerable) == 0 {
		return true
	}
	if !c.saidUnrelayed {
		c.saidUnrelayed = true
		c.log.Info("converge", "msg", "selective correction withheld; a participant cannot be answered",
			"direct", len(ids), "unanswerable", fmt.Sprint(unanswerable))
	}
	return false
}

// serveRelayed answers one request from this instance's retention rather than from a
// world it authored. The answer carries the authority's header, root and section
// summaries, which is what the receiver validates against; Served names this
// instance, so the bytes are priced against the edge that carried them.
func (c *Corrections) serveRelayed(port engine.NetworkPort, pending pendingRequest, req snapshot.CorrectionRequest) bool {
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
	set.Served = c.inst.LocalParticipant()
	body, err := snapshot.EncodeShardSet(set)
	var chunks [][]byte
	// Bounded by the transfer ceiling, not by a keyframe this instance installed
	// long ago: it cannot send a whole world instead, so a repair that fits the
	// link is the only answer that ends the leaf's wait.
	if err == nil && len(body) <= link.MaxSnapshotBytes {
		chunks, err = link.EncodeSnapshotChunksOf(set.Header.Run, req.Tick, body, parameter.SnapshotCorrectionChunkBytes)
	}
	if err != nil || chunks == nil {
		c.sendUnserved(port, pending.from, req, "the repair is wider than a relayed answer may carry")
		return true
	}
	c.sendOwed(port, pending.from, owedSend{typ: network.MsgStateShard, frames: chunks, tick: req.Tick})
	m := c.tel
	m.ShardsSent.Add(int64(pages))
	m.ShardBytesSent.Add(int64(len(body)))
	m.RelayServed.Add(1)
	m.RelayBytesSent.Add(int64(len(body)))
	// Priced here rather than at the authority: these bytes left this instance's
	// uplink, so they belong to this instance's plan.
	c.publishMu.Lock()
	c.recordSelectiveSizeLocked(len(body))
	c.publishMu.Unlock()
	c.log.Debug("converge", "msg", "relayed repair served",
		"peer", pending.from, "tick", req.Tick, "pages", pages, "bytes", len(body),
		"sections", requestedSections(req))
	return true
}

// sendUnserved tells a receiver this instance cannot produce what it asked for. A
// message rather than a silence, which would cost it a whole cadence; not a body,
// because one from a different baseline is what the supersession rules make
// unreachable. The receiver degrades to the next whole authoritative world.
func (c *Corrections) sendUnserved(port engine.NetworkPort, to uint32, req snapshot.CorrectionRequest, why string) {
	c.tel.RelayUnserved.Add(1)
	if port == nil {
		return
	}
	body, err := snapshot.EncodeUnserved(snapshot.CorrectionUnserved{
		Version: snapshot.ManifestVersion, Tick: req.Tick, Term: req.Term,
		From: c.inst.LocalParticipant(), Reason: why,
	})
	if err != nil {
		return
	}
	port.Send(to, uint8(network.MsgStateUnserved), body)
	c.log.Debug("converge", "msg", "request cannot be served from retention",
		"peer", to, "tick", req.Tick, "reason", why)
}

// applyUnserved is the receiver's half: stop waiting for a repair that is not
// coming and take the next whole world instead.
func (c *Corrections) applyUnserved(body []byte) {
	u, err := snapshot.DecodeUnserved(body)
	if err != nil {
		return
	}
	m := c.tel
	m.RelayBytesRecv.Add(int64(len(body)))
	m.RelayUnserved.Add(1)
	if awaiting := c.takeAwaiting(u.Tick); awaiting == nil {
		return // already superseded; nothing was waiting on this
	}
	c.selectiveMu.Lock()
	c.selective.wantKeyframe = true
	c.selectiveMu.Unlock()
	m.KeyframeFallback.Add(1)
	c.log.Debug("converge", "msg", "repair unavailable from the relaying neighbour",
		"peer", u.From, "tick", u.Tick, "reason", u.Reason)
}

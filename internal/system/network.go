package system

import (
	"cmp"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// NetworkSystem is the only crossing between this instance and its peers, in both
// directions. Outbound it is the event queue's wire sink (D-3 artifacts, D-10
// classes) plus a periodic owner-authored state sync (D-13). Inbound it replays a
// peer's crossings in the domain their producer stamped, and it is the sole writer
// of a remote cursor's owner-authored components — which is what makes a
// transported value the single authority for that cell.
type NetworkSystem struct {
	world *engine.World

	// Barrier state is written from the lock-free push path and drained under the tick.
	mu              sync.Mutex
	crossings       []event.ScheduledWireFrame
	scheduled       []barrierArtifact
	lastPeerTick    [parameter.MaxPlayers + 1]uint64
	productionEpoch uint64
	crossSeq        uint64
	localSource     uint32
	delayTicks      uint64
	encodeErr       int64
	barrierActive   atomic.Bool

	buf [parameter.NetworkDrainWindow]network.Inbound // per-tick drain window

	syncSeq   uint64
	lastSync  [parameter.MaxPlayers]uint64 // last applied sync per slot, for reordering
	ticks     uint64
	statSent  *atomic.Int64
	statRecv  *atomic.Int64
	statState *atomic.Int64
	statDrop  *atomic.Int64

	statDeferred      *atomic.Int64
	statAppliedLocal  *atomic.Int64
	statAppliedPeer   *atomic.Int64
	statLate          *atomic.Int64
	statRanWithout    *atomic.Int64
	statPeerLag       *atomic.Int64
	statPeerArtifacts *atomic.Int64
	statPeerApplied   *atomic.Bool
	statPeers         *atomic.Int64
	statConnected     *atomic.Bool
	statConnection    *status.AtomicString
	statMapLatched    *atomic.Bool
	statLostIn        *atomic.Int64
	statLostOut       *atomic.Int64

	// Last reported transport loss, so a new one is logged once rather than per tick.
	lastLostIn  uint64
	lastLostOut uint64

	enabled bool
}

// barrierArtifact is one encoded local or peer crossing waiting for its apply tick.
type barrierArtifact struct {
	frame     event.WireFrame
	applyTick uint64
	source    uint32
	origin    event.Origin
}

func NewNetworkSystem(world *engine.World) engine.System {
	s := &NetworkSystem{world: world}

	reg := world.Resources.Status
	s.statSent = reg.Ints.Get("network.crossings_sent")
	s.statRecv = reg.Ints.Get("network.crossings_received")
	s.statState = reg.Ints.Get("network.state_applied")
	s.statDrop = reg.Ints.Get("network.frames_dropped")
	s.statDeferred = reg.Ints.Get("network.barrier_deferred")
	s.statAppliedLocal = reg.Ints.Get("network.barrier_applied_local")
	s.statAppliedPeer = reg.Ints.Get("network.barrier_applied_peer")
	s.statLate = reg.Ints.Get("network.barrier_late")
	s.statRanWithout = reg.Ints.Get("network.barrier_ran_without_peer")
	s.statPeerLag = reg.Ints.Get("network.barrier_peer_lag_ticks")
	s.statPeerArtifacts = reg.Ints.Get("network.barrier_peer_artifacts")
	s.statPeerApplied = reg.Bools.Get("network.barrier_peer_applied")
	s.statPeers = reg.Ints.Get("network.peers")
	s.statConnected = reg.Bools.Get("network.connected")
	s.statConnection = reg.Strings.Get("network.state")
	s.statMapLatched = reg.Bools.Get("network.map_latched")
	s.statLostIn = reg.Ints.Get("network.transport_lost_in")
	s.statLostOut = reg.Ints.Get("network.transport_lost_out")

	s.Init()
	return s
}

// Init resets the barrier and leaves its sink installed as a no-op without peers.
func (s *NetworkSystem) Init() {
	s.enabled = true
	s.ticks = 0
	s.syncSeq = 0
	s.lastSync = [parameter.MaxPlayers]uint64{}

	s.statSent.Store(0)
	s.statRecv.Store(0)
	s.statState.Store(0)
	s.statDrop.Store(0)
	s.statDeferred.Store(0)
	s.statAppliedLocal.Store(0)
	s.statAppliedPeer.Store(0)
	s.statLate.Store(0)
	s.statRanWithout.Store(0)
	s.statPeerLag.Store(0)
	s.statPeerArtifacts.Store(0)
	s.statPeerApplied.Store(false)
	s.statPeers.Store(0)
	s.statConnected.Store(false)
	s.statConnection.Store("off")
	s.statMapLatched.Store(false)
	s.statLostIn.Store(0)
	s.statLostOut.Store(0)
	s.lastLostIn, s.lastLostOut = 0, 0
	s.barrierActive.Store(false)

	s.mu.Lock()
	s.crossings = s.crossings[:0]
	s.scheduled = s.scheduled[:0]
	s.lastPeerTick = [parameter.MaxPlayers + 1]uint64{}
	s.productionEpoch = s.world.Resources.Game.State.GetGameTicks() + 1
	s.crossSeq = 0
	s.localSource = 1
	s.delayTicks = parameter.NetworkBarrierDelayTicks
	if r := s.world.Resources.Network; r != nil {
		s.localSource = r.ParticipantID
		s.delayTicks = r.BarrierDelayTicks
	}
	s.encodeErr = 0
	s.mu.Unlock()

	s.world.Resources.Event.Queue.SetWireSink(s)
}

// port returns the live endpoint, nil when no transport is attached. Read per tick
// rather than cached at construction: an embedder or a harness may attach one after
// the system set is sealed.
func (s *NetworkSystem) port() engine.NetworkPort {
	if s.world.Resources.Network == nil {
		return nil
	}
	return s.world.Resources.Network.Port
}

// Name returns system's name
func (s *NetworkSystem) Name() string { return "network" }

// Priority: inbound translation runs before every consumer, so a peer's crossing
// is queued in time for the settle of the tick that drained it
func (s *NetworkSystem) Priority() int { return parameter.PriorityNetwork }

func (s *NetworkSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *NetworkSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameResetRequest:
		s.Init()
	case event.EventMetaSystemCommandRequest:
		if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && p.SystemName == s.Name() {
			s.enabled = p.Enabled
		}
	}
}

// Cross encodes and schedules a crossing when a peer is live. Returning true
// transfers queue ownership; pooled originals are released after encoding.
func (s *NetworkSystem) Cross(ev event.GameEvent) bool {
	if !s.barrierActive.Load() {
		return false
	}
	frame, encErr := event.NewWireFrame(ev)
	s.mu.Lock()
	if encErr != "" {
		s.encodeErr++
		s.mu.Unlock()
		event.ReleaseDeferredPayload(ev.Payload)
		return true
	}
	s.crossSeq++
	frame.Seq = s.crossSeq
	applyTick := s.productionEpoch + s.delayTicks
	s.crossings = append(s.crossings, event.ScheduledWireFrame{Frame: frame, ApplyTick: applyTick})
	s.scheduled = append(s.scheduled, barrierArtifact{
		frame: frame, applyTick: applyTick, source: s.localSource, origin: ev.Origin,
	})
	s.mu.Unlock()
	event.ReleaseDeferredPayload(ev.Payload)
	s.statDeferred.Add(1)
	return true
}

// refreshLink re-reads the negotiated session identity and reports whether the
// barrier owns crossings this tick. Every entry point the system has — the startup
// gate, the update pass, tick open and tick close — needs the same answer, and the
// endpoint may be attached or lost between any two of them.
func (s *NetworkSystem) refreshLink(p engine.NetworkPort) bool {
	active := s.enabled && p != nil && p.IsRunning() && p.PeerCount() > 0
	if r := s.world.Resources.Network; r != nil {
		s.mu.Lock()
		s.localSource = r.ParticipantID
		s.delayTicks = r.BarrierDelayTicks
		s.mu.Unlock()
	}
	s.barrierActive.Store(active)
	return active
}

// ActivateSession closes the pre-first-tick input window after startup gates.
// The regular tick path keeps the same state current after disconnects.
func (s *NetworkSystem) ActivateSession() {
	p := s.port()
	s.refreshLink(p)
	s.publishConnectionTelemetry(p)
}

func (s *NetworkSystem) Update() {
	p := s.port()
	s.refreshLink(p)
	s.publishConnectionTelemetry(p)
	if s.enabled && p != nil && p.IsRunning() {
		s.ticks++
	}
}

// Receive drains transport state and publishes every artifact due before nextTick.
// Caller holds the world lock.
func (s *NetworkSystem) Receive(nextTick uint64) int {
	queued := 0
	p := s.port()
	s.refreshLink(p)
	s.publishConnectionTelemetry(p)
	if s.enabled && p != nil {
		queued += s.drain(p)
	}
	queued += s.applyDue(nextTick)
	s.publishBarrierTelemetry(nextTick, p)
	return queued
}

// Flush closes completedTick's production epoch, including an empty marker.
// Caller holds the world lock.
func (s *NetworkSystem) Flush(completedTick uint64) {
	p := s.port()
	active := s.refreshLink(p)
	s.flushCrossings(p, completedTick, active)
	if s.enabled && p != nil && p.IsRunning() && s.ticks%parameter.NetworkSyncTicks == 0 {
		s.sendCursorState(p)
	}
	s.publishTransportLoss(p)
}

// drain translates one tick's transport notifications into events
func (s *NetworkSystem) drain(p engine.NetworkPort) int {
	n := p.Drain(s.buf[:])
	queued := 0
	for i := range n {
		in := &s.buf[i]
		switch in.Kind {
		case network.InboundConnect:
			s.world.PushLocal(event.EventNetworkConnect, &event.NetworkConnectPayload{PeerID: uint32(in.Peer)})
			queued++
		case network.InboundDisconnect:
			s.world.PushLocal(event.EventNetworkDisconnect, &event.NetworkDisconnectPayload{PeerID: uint32(in.Peer)})
			s.despawnPeer(uint32(in.Peer))
			queued++
		case network.InboundMessage:
			queued += s.dispatchMessage(in.Peer, in.Msg)
		}
	}
	return queued
}

// despawnPeer leaves the local simulation alive with only its connected roster.
// The slot's sync sequence is released with it: sequences are per-sender and restart
// at one, so a slot refilled by a later participant must not be gated by the numbers
// its predecessor already used.
func (s *NetworkSystem) despawnPeer(peerID uint32) {
	s.world.Components.Cursor.Each(func(_ core.Entity, c *component.CursorComponent) bool {
		if c.Control == component.ControlRemote && c.PeerID == peerID {
			s.world.PushEvent(event.EventCursorDespawnRequest, &event.CursorDespawnRequestPayload{Slot: c.Slot})
			if int(c.Slot) < parameter.MaxPlayers {
				s.lastSync[c.Slot] = 0
			}
		}
		return true
	})
}

// publishConnectionTelemetry exposes the poll endpoint and D-14 latch state.
func (s *NetworkSystem) publishConnectionTelemetry(p engine.NetworkPort) {
	peers := 0
	state := "off"
	if p != nil {
		peers = p.PeerCount()
		if statePort, ok := p.(interface{ ConnectionState() network.ConnState }); ok {
			switch statePort.ConnectionState() {
			case network.StateConnected:
				state = "connected"
			case network.StateConnecting:
				state = "waiting"
			default:
				state = "down"
			}
		} else {
			switch {
			case !p.IsRunning():
				state = "down"
			case peers == 0:
				state = "waiting"
			default:
				state = "connected"
			}
		}
	}
	latched := !s.world.MapSizeLocal()
	s.statPeers.Store(int64(peers))
	s.statConnected.Store(peers > 0)
	s.statConnection.StoreIfChanged(state)
	s.statMapLatched.Store(latched)
	s.world.Resources.Status.Bools.Get("context.map_locked").Store(
		s.world.Resources.Config.CropOnResize && latched)
}

// dispatchMessage admits the two message kinds the domain model defines: the D-3
// artifact epoch and the D-13 owner-authored sync. Raw participant input is not one
// of them — a peer sends the resolved artifact, never the keystroke that produced
// it — so an unrecognised type is counted and discarded rather than translated.
func (s *NetworkSystem) dispatchMessage(_ network.PeerID, msg *network.Message) int {
	if msg == nil {
		return 0
	}
	switch msg.Type {
	case network.MsgEvent:
		s.scheduleCrossings(msg.Payload)
	case network.MsgStateSync:
		s.applyCursorState(msg.Payload)
	default:
		s.statDrop.Add(1)
	}
	return 0
}

// publishTransportLoss exposes frames the link lost outside the barrier: inbound
// notifications a full poll buffer discarded, and outbound frames a peer's send
// queue refused. Either one silently desynchronises two instances, so a new loss
// is logged as well as counted.
func (s *NetworkSystem) publishTransportLoss(p engine.NetworkPort) {
	if p == nil {
		return
	}
	loss, ok := p.(interface {
		Dropped() uint64
		Refused() uint64
	})
	if !ok {
		return
	}
	in, out := loss.Dropped(), loss.Refused()
	s.statLostIn.Store(int64(in))
	s.statLostOut.Store(int64(out))
	if in == s.lastLostIn && out == s.lastLostOut {
		return
	}
	vlog.Warn("app", "msg", "network transport loss",
		"inbound_dropped", in, "outbound_refused", out)
	s.lastLostIn, s.lastLostOut = in, out
}

// flushCrossings advances the epoch under the producer lock, then sends its marker.
func (s *NetworkSystem) flushCrossings(p engine.NetworkPort, completedTick uint64, active bool) {
	s.mu.Lock()
	pending := s.crossings
	s.crossings = nil
	dropped := s.encodeErr
	s.encodeErr = 0
	s.productionEpoch = completedTick + 1
	source := s.localSource
	s.mu.Unlock()

	if dropped != 0 {
		s.statDrop.Add(dropped)
	}
	if !active {
		if len(pending) != 0 {
			s.statDrop.Add(int64(len(pending)))
		}
		return
	}
	body, err := event.EncodeWireBatch(event.WireBatch{
		Source: source, ProducedTick: completedTick, Frames: pending,
	})
	if err != nil {
		s.statDrop.Add(int64(len(pending)))
		vlog.Warn("app", "msg", "network encode", "frames", len(pending), "error", err.Error())
		return
	}
	p.Broadcast(uint8(network.MsgEvent), body)
	s.statSent.Add(int64(len(pending)))
}

// scheduleCrossings buffers one peer epoch; application waits for its target tick.
func (s *NetworkSystem) scheduleCrossings(body []byte) {
	batch, err := event.DecodeWireBatch(body)
	if err != nil {
		s.statDrop.Add(1)
		vlog.Warn("app", "msg", "network decode", "error", err.Error())
		return
	}
	if batch.Source == 0 || int(batch.Source) >= len(s.lastPeerTick) {
		s.statDrop.Add(int64(max(1, len(batch.Frames))))
		return
	}
	s.mu.Lock()
	if batch.ProducedTick <= s.lastPeerTick[batch.Source] {
		s.mu.Unlock()
		s.statDrop.Add(int64(len(batch.Frames)))
		return
	}
	s.lastPeerTick[batch.Source] = batch.ProducedTick
	for _, f := range batch.Frames {
		s.scheduled = append(s.scheduled, barrierArtifact{
			frame: f.Frame, applyTick: f.ApplyTick, source: batch.Source, origin: event.OriginNetwork,
		})
	}
	s.mu.Unlock()
}

// applyDue publishes due artifacts in the same source/sequence order everywhere.
func (s *NetworkSystem) applyDue(nextTick uint64) int {
	s.mu.Lock()
	due := make([]barrierArtifact, 0, len(s.scheduled))
	keep := s.scheduled[:0]
	for _, a := range s.scheduled {
		if a.applyTick <= nextTick {
			due = append(due, a)
		} else {
			keep = append(keep, a)
		}
	}
	s.scheduled = keep
	localSource := s.localSource
	s.mu.Unlock()

	slices.SortFunc(due, func(a, b barrierArtifact) int {
		if n := cmp.Compare(a.applyTick, b.applyTick); n != 0 {
			return n
		}
		if n := cmp.Compare(a.source, b.source); n != 0 {
			return n
		}
		return cmp.Compare(a.frame.Seq, b.frame.Seq)
	})

	local, peer := 0, 0
	for _, a := range due {
		et, payload, domain, err := a.frame.Decode()
		if err != nil {
			s.statDrop.Add(1)
			continue
		}
		if a.applyTick < nextTick {
			s.statLate.Add(1)
		}
		s.world.Resources.Event.Queue.PushReady(event.GameEvent{
			Type: et, Payload: payload, Origin: a.origin, Domain: domain,
		})
		if a.source == localSource {
			local++
		} else {
			peer++
		}
	}
	s.statAppliedLocal.Add(int64(local))
	s.statAppliedPeer.Add(int64(peer))
	s.statPeerArtifacts.Store(int64(peer))
	s.statPeerApplied.Store(peer != 0)
	s.statRecv.Add(int64(peer))
	return len(due)
}

// publishBarrierTelemetry reports playout lead and epochs that ran before a marker.
func (s *NetworkSystem) publishBarrierTelemetry(nextTick uint64, p engine.NetworkPort) {
	if p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		s.statPeerLag.Store(0)
		return
	}
	s.mu.Lock()
	delay := s.delayTicks
	local := s.localSource
	minPeer := uint64(0)
	seen := 0
	for source := 1; source < len(s.lastPeerTick); source++ {
		if uint32(source) == local || s.lastPeerTick[source] == 0 {
			continue
		}
		tick := s.lastPeerTick[source]
		if seen == 0 || tick < minPeer {
			minPeer = tick
		}
		seen++
	}
	s.mu.Unlock()

	required := uint64(0)
	if nextTick > delay {
		required = nextTick - delay
	}
	lag := uint64(0)
	if required > minPeer {
		lag = required - minPeer
	}
	s.statPeerLag.Store(int64(lag))
	// The guard above already established a running port with at least one peer.
	if required != 0 && (seen < p.PeerCount() || lag != 0) {
		s.statRanWithout.Add(1)
	}
}

// sendCursorState broadcasts the owner-authored state of every cursor this
// instance simulates (D-13). One message per cursor keeps a late arrival from
// holding up the others.
func (s *NetworkSystem) sendCursorState(p engine.NetworkPort) {
	s.syncSeq++
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if cursor == 0 || !s.world.SimulatesLocally(cursor) {
			continue
		}
		body, err := event.EncodeFrames([]event.WireFrame{stateFrame(s.readCursorState(cursor, uint8(i)))})
		if err != nil {
			s.statDrop.Add(1)
			continue
		}
		p.Broadcast(uint8(network.MsgStateSync), body)
	}
}

// readCursorState reads one cursor's D-13 set into a payload.
// Caller MUST hold updateMutex — Update runs inside the tick.
func (s *NetworkSystem) readCursorState(cursor core.Entity, slot uint8) *event.CursorStatePayload {
	p := &event.CursorStatePayload{Entity: cursor, Slot: slot, Seq: s.syncSeq}

	if c, ok := s.world.Components.Energy.GetComponent(cursor); ok {
		p.Energy = c.Current
	}
	if c, ok := s.world.Components.Heat.GetComponent(cursor); ok {
		p.Heat, p.Overheat, p.EmberActive = c.Current, c.Overheat, c.EmberActive
	}
	if c, ok := s.world.Components.Shield.GetComponent(cursor); ok {
		p.ShieldActive = c.Active
		p.ShieldRadiusX, p.ShieldRadiusY = c.RadiusX, c.RadiusY
		p.ShieldInvRxSq, p.ShieldInvRySq = c.InvRxSq, c.InvRySq
	}
	if c, ok := s.world.Components.Boost.GetComponent(cursor); ok {
		p.BoostActive = c.Active
		p.BoostRemaining, p.BoostTotal = int64(c.Remaining), int64(c.TotalDuration)
	}
	if c, ok := s.world.Components.Weapon.GetComponent(cursor); ok {
		p.WeaponCharges = make([]int, component.WeaponCount)
		p.WeaponCooldown = make([]int64, component.WeaponCount)
		for wt := range component.WeaponCount {
			p.WeaponCharges[wt] = c.Charges[wt]
			p.WeaponCooldown[wt] = int64(c.Cooldown[wt])
		}
		p.MainFireCooldown = int64(c.MainFireCooldown)
	}
	if c, ok := s.world.Components.Combat.GetComponent(cursor); ok {
		p.HitPoints, p.DamageImmunity = c.HitPoints, int64(c.RemainingDamageImmunity)
	}
	if c, ok := s.world.Components.CursorView.GetComponent(cursor); ok {
		p.ErrorFlash, p.BurstFlash = int64(c.ErrorFlashRemaining), int64(c.BurstFlashRemaining)
		p.BlinkActive, p.BlinkType, p.BlinkLevel = c.BlinkActive, c.BlinkType, c.BlinkLevel
		p.BlinkRemaining = int64(c.BlinkRemaining)
	}
	if c, ok := s.world.Components.Pulse.GetComponent(cursor); ok {
		p.PulseActive = true
		p.PulseOriginX, p.PulseOriginY = c.OriginX, c.OriginY
		p.PulseDuration, p.PulseRemaining = int64(c.Duration), int64(c.Remaining)
	}
	return p
}

// applyCursorState writes a peer's cursor state. The write is admitted only for a
// cursor this instance does not simulate, so the transported value and a local
// writer can never both author one cell (D-2, D-13).
func (s *NetworkSystem) applyCursorState(body []byte) {
	frames, err := event.DecodeFrames(body)
	if err != nil {
		s.statDrop.Add(1)
		return
	}
	for _, f := range frames {
		et, payload, _, err := f.Decode()
		if err != nil || et != event.EventCursorStateSync {
			s.statDrop.Add(1)
			continue
		}
		p, ok := payload.(*event.CursorStatePayload)
		if !ok {
			s.statDrop.Add(1)
			continue
		}
		s.writeCursorState(p)
	}
}

// writeCursorState applies one sync, dropping a stale or misaddressed one. The
// payload names both an entity and a slot; they must agree, because the entity
// selects the cells written and the slot keys the sequence that decides whether to
// write them at all. A disagreement would age one participant's state under
// another's sequence, so it is dropped rather than reconciled.
func (s *NetworkSystem) writeCursorState(p *event.CursorStatePayload) {
	cursor := s.world.ResolveCursor(p.Entity)
	if cursor == 0 || s.world.SimulatesLocally(cursor) || int(p.Slot) >= parameter.MaxPlayers {
		s.statDrop.Add(1)
		return
	}
	if slot, ok := s.world.CursorSlot(cursor); !ok || slot != p.Slot {
		s.statDrop.Add(1)
		return
	}
	if p.Seq <= s.lastSync[p.Slot] {
		s.statDrop.Add(1) // reordered or replayed; the newer value already landed
		return
	}
	s.lastSync[p.Slot] = p.Seq

	if c, ok := s.world.Components.Energy.GetPtr(cursor); ok {
		c.Current = p.Energy
	}
	if c, ok := s.world.Components.Heat.GetPtr(cursor); ok {
		c.Current, c.Overheat, c.EmberActive = p.Heat, p.Overheat, p.EmberActive
	}
	if c, ok := s.world.Components.Shield.GetPtr(cursor); ok {
		c.Active = p.ShieldActive
		c.RadiusX, c.RadiusY = p.ShieldRadiusX, p.ShieldRadiusY
		c.InvRxSq, c.InvRySq = p.ShieldInvRxSq, p.ShieldInvRySq
	}
	if c, ok := s.world.Components.Boost.GetPtr(cursor); ok {
		c.Active = p.BoostActive
		c.Remaining, c.TotalDuration = time.Duration(p.BoostRemaining), time.Duration(p.BoostTotal)
	}
	if c, ok := s.world.Components.Weapon.GetPtr(cursor); ok {
		for wt := range min(component.WeaponCount, component.WeaponType(len(p.WeaponCharges))) {
			c.Charges[wt] = p.WeaponCharges[wt]
		}
		for wt := range min(component.WeaponCount, component.WeaponType(len(p.WeaponCooldown))) {
			c.Cooldown[wt] = time.Duration(p.WeaponCooldown[wt])
		}
		c.MainFireCooldown = time.Duration(p.MainFireCooldown)
	}
	if c, ok := s.world.Components.Combat.GetPtr(cursor); ok {
		c.HitPoints = p.HitPoints
		c.RemainingDamageImmunity = time.Duration(p.DamageImmunity)
	}
	if c, ok := s.world.Components.CursorView.GetPtr(cursor); ok {
		c.ErrorFlashRemaining = time.Duration(p.ErrorFlash)
		c.BurstFlashRemaining = time.Duration(p.BurstFlash)
		c.BlinkActive, c.BlinkType, c.BlinkLevel = p.BlinkActive, p.BlinkType, p.BlinkLevel
		c.BlinkRemaining = time.Duration(p.BlinkRemaining)
	}
	switch {
	case p.PulseActive:
		s.world.Components.Pulse.SetComponent(cursor, component.PulseComponent{
			OriginX: p.PulseOriginX, OriginY: p.PulseOriginY,
			Duration:  time.Duration(p.PulseDuration),
			Remaining: time.Duration(p.PulseRemaining),
		})
	case s.world.Components.Pulse.HasEntity(cursor):
		s.world.Components.Pulse.RemoveEntity(cursor, false)
	}
	s.statState.Add(1)
}

// stateFrame wraps one cursor sync in the frame the codec already carries, so
// state and crossings share one encoder
func stateFrame(p *event.CursorStatePayload) event.WireFrame {
	f, _ := event.NewWireFrame(event.GameEvent{
		Type: event.EventCursorStateSync, Payload: p, Domain: core.DomainShared,
	})
	return f
}

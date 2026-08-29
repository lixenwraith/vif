package system

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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

	// crossings accumulates one tick's outbound artifacts. Written from the
	// lock-free push path on any producer goroutine, drained under the tick.
	mu        sync.Mutex
	crossings []event.WireFrame
	encodeErr int64

	buf [parameter.NetworkDrainWindow]network.Inbound // per-tick drain window

	syncSeq   uint64
	lastSync  [parameter.MaxPlayers]uint64 // last applied sync per slot, for reordering
	ticks     uint64
	statSent  *atomic.Int64
	statRecv  *atomic.Int64
	statState *atomic.Int64
	statDrop  *atomic.Int64

	attached bool // the wire sink is installed
	enabled  bool
}

func NewNetworkSystem(world *engine.World) engine.System {
	s := &NetworkSystem{world: world}

	reg := world.Resources.Status
	s.statSent = reg.Ints.Get("network.crossings_sent")
	s.statRecv = reg.Ints.Get("network.crossings_received")
	s.statState = reg.Ints.Get("network.state_applied")
	s.statDrop = reg.Ints.Get("network.frames_dropped")

	s.Init()
	return s
}

// Init resets per-session state. The outbound sink is installed by Update once a
// live port appears, so a run with no transport leaves the push path untouched.
func (s *NetworkSystem) Init() {
	s.enabled = true
	s.ticks = 0
	s.syncSeq = 0
	s.lastSync = [parameter.MaxPlayers]uint64{}

	s.statSent.Store(0)
	s.statRecv.Store(0)
	s.statState.Store(0)
	s.statDrop.Store(0)

	s.mu.Lock()
	s.crossings = s.crossings[:0]
	s.encodeErr = 0
	s.mu.Unlock()

	s.attached = false
	s.world.Resources.Event.Queue.SetWireSink(nil)
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

// Cross implements event.WireSink: it encodes a crossing on the producer's
// goroutine and buffers it. Encoding here, not at flush, is what lets the payload
// be recycled the moment the queue slot publishes.
func (s *NetworkSystem) Cross(ev event.GameEvent) {
	frame, encErr := event.NewWireFrame(ev)
	s.mu.Lock()
	if encErr != "" {
		s.encodeErr++
	} else {
		s.crossings = append(s.crossings, frame)
	}
	s.mu.Unlock()
}

func (s *NetworkSystem) Update() {
	p := s.port()
	if !s.enabled || p == nil || !p.IsRunning() {
		if s.attached {
			s.world.Resources.Event.Queue.SetWireSink(nil)
			s.attached = false
		}
		return
	}
	if !s.attached {
		s.world.Resources.Event.Queue.SetWireSink(s)
		s.attached = true
	}

	s.ticks++
}

// Receive implements event.WireSink: inbound translation opens the tick.
// Caller holds the world lock.
func (s *NetworkSystem) Receive() {
	p := s.port()
	if !s.enabled || p == nil || !p.IsRunning() {
		return
	}
	s.drain(p)
}

// Flush implements event.WireSink: outbound work runs at the tick boundary, after
// the tick has settled, so a crossing produced anywhere in the tick leaves with it.
// Caller holds the world lock.
func (s *NetworkSystem) Flush() {
	p := s.port()
	if !s.enabled || p == nil || !p.IsRunning() {
		return
	}
	s.flushCrossings(p)
	if s.ticks%parameter.NetworkSyncTicks == 0 {
		s.sendCursorState(p)
	}
}

// drain translates one tick's transport notifications into events
func (s *NetworkSystem) drain(p engine.NetworkPort) {
	n := p.Drain(s.buf[:])
	for i := range n {
		in := &s.buf[i]
		switch in.Kind {
		case network.InboundConnect:
			s.world.PushLocal(event.EventNetworkConnect, &event.NetworkConnectPayload{PeerID: uint32(in.Peer)})
		case network.InboundDisconnect:
			s.world.PushLocal(event.EventNetworkDisconnect, &event.NetworkDisconnectPayload{PeerID: uint32(in.Peer)})
		case network.InboundMessage:
			s.dispatchMessage(in.Peer, in.Msg)
		}
	}
}

func (s *NetworkSystem) dispatchMessage(id network.PeerID, msg *network.Message) {
	if msg == nil {
		return
	}
	switch msg.Type {
	case network.MsgEvent:
		s.applyCrossings(msg.Payload)
	case network.MsgStateSync:
		s.applyCursorState(msg.Payload)
	case network.MsgInput:
		s.world.PushLocal(event.EventRemoteInput, &event.RemoteInputPayload{PeerID: uint32(id), Payload: msg.Payload})
	}
}

// flushCrossings sends this tick's artifacts as one message; an empty tick sends
// nothing, so an idle link is silent
func (s *NetworkSystem) flushCrossings(p engine.NetworkPort) {
	s.mu.Lock()
	pending := s.crossings
	s.crossings = nil
	dropped := s.encodeErr
	s.encodeErr = 0
	s.mu.Unlock()

	if dropped != 0 {
		s.statDrop.Add(dropped)
	}
	if len(pending) == 0 {
		return
	}
	body, err := event.EncodeFrames(pending)
	if err != nil {
		s.statDrop.Add(int64(len(pending)))
		vlog.Warn("app", "msg", "network encode", "frames", len(pending), "error", err.Error())
		return
	}
	p.Broadcast(uint8(network.MsgEvent), body)
	s.statSent.Add(int64(len(pending)))
}

// applyCrossings replays a peer's artifacts with the domain their producer
// resolved (D-7), tagged OriginNetwork so the wire gate does not echo them back
func (s *NetworkSystem) applyCrossings(body []byte) {
	frames, err := event.DecodeFrames(body)
	if err != nil {
		s.statDrop.Add(1)
		vlog.Warn("app", "msg", "network decode", "error", err.Error())
		return
	}
	for _, f := range frames {
		et, payload, domain, err := f.Decode()
		if err != nil {
			s.statDrop.Add(1)
			vlog.Warn("app", "msg", "network frame", "ev", f.Event, "error", err.Error())
			continue
		}
		s.world.PushEventFull(et, payload, event.OriginNetwork, domain)
		s.statRecv.Add(1)
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

// writeCursorState applies one sync, dropping a stale or misaddressed one
func (s *NetworkSystem) writeCursorState(p *event.CursorStatePayload) {
	cursor := s.world.ResolveCursor(p.Entity)
	if cursor == 0 || s.world.SimulatesLocally(cursor) || int(p.Slot) >= parameter.MaxPlayers {
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

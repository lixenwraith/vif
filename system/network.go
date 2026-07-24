package system

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/network"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// NetworkSystem drains transport notifications into game events and will own
// outbound send policy once protocol events exist. Stub: inbound path only.
type NetworkSystem struct {
	world *engine.World
	port  engine.NetworkPort

	buf     [64]network.Inbound // per-tick drain window
	enabled bool
}

func NewNetworkSystem(world *engine.World) engine.System {
	s := &NetworkSystem{world: world}
	if world.Resources.Network != nil {
		s.port = world.Resources.Network.Port
	}
	s.Init()
	return s
}

func (s *NetworkSystem) Init() { s.enabled = true }

func (s *NetworkSystem) Name() string { return "network" }

// Priority: inbound translation before gameplay logic consumes events
// Stub value; finalize with protocol work
func (s *NetworkSystem) Priority() int { return parameter.PriorityUI }

func (s *NetworkSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
		// Outbound request events registered here when protocol lands
	}
}

func (s *NetworkSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameReset:
		s.Init()
	case event.EventMetaSystemCommandRequest:
		if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && p.SystemName == s.Name() {
			s.enabled = p.Enabled
		}
	}
}

func (s *NetworkSystem) Update() {
	if !s.enabled || s.port == nil || !s.port.IsRunning() {
		return
	}
	n := s.port.Drain(s.buf[:])
	for i := range n {
		in := &s.buf[i]
		switch in.Kind {
		case network.InboundConnect:
			s.world.PushEvent(event.EventNetworkConnect, &event.NetworkConnectPayload{PeerID: uint32(in.Peer)})
		case network.InboundDisconnect:
			s.world.PushEvent(event.EventNetworkDisconnect, &event.NetworkDisconnectPayload{PeerID: uint32(in.Peer)})
		case network.InboundMessage:
			s.dispatchMessage(in.Peer, in.Msg)
		}
	}
}

func (s *NetworkSystem) dispatchMessage(id network.PeerID, msg *network.Message) {
	switch msg.Type {
	case network.MsgInput:
		s.world.PushEvent(event.EventRemoteInput, &event.RemoteInputPayload{PeerID: uint32(id), Payload: msg.Payload})
	case network.MsgStateSync:
		s.world.PushEvent(event.EventStateSync, &event.StateSyncPayload{PeerID: uint32(id), Seq: msg.Seq, Payload: msg.Payload})
	case network.MsgEvent:
		s.world.PushEvent(event.EventNetworkEvent, &event.NetworkEventPayload{PeerID: uint32(id), Payload: msg.Payload})
	}
}

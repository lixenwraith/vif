package network

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/service"
)

// Service wraps Transport as a hub-managed service
type Service struct {
	config    *Config
	transport *Transport

	// Event queue for publishing network events
	eventQueue *event.EventQueue

	disabled atomic.Bool
}

// NewService creates a network service (disabled by default)
func NewService() *Service {
	return &Service{
		config: DefaultConfig(),
	}
}

// Name implements service.Service
func (s *Service) Name() string {
	return "network"
}

// Dependencies implements service.Service
func (s *Service) Dependencies() []string {
	return nil
}

// Init implements service.Service
// args[0]: *Config (optional, overrides default)
func (s *Service) Init(args ...any) error {
	if len(args) > 0 {
		if cfg, ok := args[0].(*Config); ok && cfg != nil {
			s.config = cfg
		}
	}

	if s.config.Role == RoleNone {
		s.disabled.Store(true)
		return nil
	}

	s.transport = NewTransport(s.config)
	s.transport.SetHandlers(s.onConnect, s.onDisconnect, s.onMessage)

	return nil
}

// Start implements service.Service
func (s *Service) Start() error {
	if s.disabled.Load() || s.transport == nil {
		return nil
	}
	return s.transport.Start()
}

// Stop implements service.Service
func (s *Service) Stop() error {
	if s.transport != nil {
		return s.transport.Stop()
	}
	return nil
}

// Contribute implements service.ResourceContributor
func (s *Service) Contribute(publish service.ResourcePublisher) {
	if s.disabled.Load() {
		return
	}
	publish(&engine.NetworkResource{Transport: s})
}

// SetEventQueue wires the service to the ECS event system
// Called after resources are published
func (s *Service) SetEventQueue(eq *event.EventQueue) {
	s.eventQueue = eq
}

// onConnect handles new peer connections
func (s *Service) onConnect(id PeerID) {
	if s.eventQueue == nil {
		return
	}
	s.eventQueue.Push(event.GameEvent{
		Type:    event.EventNetworkConnect,
		Payload: &event.NetworkConnectPayload{PeerID: uint32(id)},
	})
}

// onDisconnect handles peer disconnections
func (s *Service) onDisconnect(id PeerID) {
	if s.eventQueue == nil {
		return
	}
	s.eventQueue.Push(event.GameEvent{
		Type:    event.EventNetworkDisconnect,
		Payload: &event.NetworkDisconnectPayload{PeerID: uint32(id)},
	})
}

// onMessage handles incoming messages
func (s *Service) onMessage(id PeerID, msg *Message) {
	if s.eventQueue == nil {
		return
	}

	switch msg.Type {
	case MsgInput:
		s.eventQueue.Push(event.GameEvent{
			Type: event.EventRemoteInput,
			Payload: &event.RemoteInputPayload{
				PeerID:  uint32(id),
				Payload: msg.Payload,
			},
		})

	case MsgStateSync:
		s.eventQueue.Push(event.GameEvent{
			Type: event.EventStateSync,
			Payload: &event.StateSyncPayload{
				PeerID:  uint32(id),
				Seq:     msg.Seq,
				Payload: msg.Payload,
			},
		})

	case MsgEvent:
		s.eventQueue.Push(event.GameEvent{
			Type: event.EventNetworkEvent,
			Payload: &event.NetworkEventPayload{
				PeerID:  uint32(id),
				Payload: msg.Payload,
			},
		})
	}
}

// --- NetworkProvider interface for ECS access ---

// Send transmits a message to a specific peer
func (s *Service) Send(peerID uint32, msgType uint8, payload []byte) bool {
	if s.transport == nil {
		return false
	}
	return s.transport.Send(PeerID(peerID), NewMessage(MessageType(msgType), payload))
}

// Broadcast sends a message to all peers
func (s *Service) Broadcast(msgType uint8, payload []byte) {
	if s.transport == nil {
		return
	}
	s.transport.Broadcast(NewMessage(MessageType(msgType), payload))
}

// PeerCount returns connected peer count
func (s *Service) PeerCount() int {
	if s.transport == nil {
		return 0
	}
	return s.transport.PeerCount()
}

// IsRunning returns true if network is active
func (s *Service) IsRunning() bool {
	return s.transport != nil && s.transport.IsRunning()
}
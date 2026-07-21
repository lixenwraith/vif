package service

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/network"
)

const inboundBufferSize = 1024

// NetworkService bridges the transport to the ECS via a drained inbound
// buffer; it holds no event queue reference
type NetworkService struct {
	config    *network.Config
	transport *network.Transport
	disabled  atomic.Bool

	inbound chan network.Inbound
	dropped atomic.Uint64 // inbound overflow count; surfaced via telemetry later
}

// Pass nil config to disable or initialize with defaults
func NewNetworkService(cfg *network.Config) *NetworkService {
	if cfg == nil {
		cfg = network.DefaultConfig()
	}
	return &NetworkService{
		config:  cfg,
		inbound: make(chan network.Inbound, inboundBufferSize),
	}
}

func (s *NetworkService) Name() string           { return "network" }
func (s *NetworkService) Dependencies() []string { return nil }

func (s *NetworkService) Init() error {
	if s.config.Role == network.RoleNone {
		s.disabled.Store(true)
		return nil
	}
	s.transport = network.NewTransport(s.config)
	s.transport.SetHandlers(s.onConnect, s.onDisconnect, s.onMessage)
	return nil
}

func (s *NetworkService) Start() error {
	if s.disabled.Load() || s.transport == nil {
		return nil
	}
	return s.transport.Start()
}

func (s *NetworkService) Stop() error {
	if s.transport != nil {
		return s.transport.Stop()
	}
	return nil
}

func (s *NetworkService) Contribute(r *engine.Resource) {
	if s.disabled.Load() {
		return
	}
	r.Network = &engine.NetworkResource{Port: s}
}

// push enqueues without blocking transport goroutines; drops on full buffer
func (s *NetworkService) push(in network.Inbound) {
	select {
	case s.inbound <- in:
	default:
		s.dropped.Add(1)
	}
}

func (s *NetworkService) onConnect(id network.PeerID) {
	s.push(network.Inbound{Kind: network.InboundConnect, Peer: id})
}

func (s *NetworkService) onDisconnect(id network.PeerID) {
	s.push(network.Inbound{Kind: network.InboundDisconnect, Peer: id})
}

func (s *NetworkService) onMessage(id network.PeerID, msg *network.Message) {
	s.push(network.Inbound{Kind: network.InboundMessage, Peer: id, Msg: msg})
}

// Drain implements engine.NetworkPort; non-blocking, called on game tick
func (s *NetworkService) Drain(dst []network.Inbound) int {
	n := 0
	for n < len(dst) {
		select {
		case in := <-s.inbound:
			dst[n] = in
			n++
		default:
			return n
		}
	}
	return n
}

func (s *NetworkService) Send(peerID uint32, msgType uint8, payload []byte) bool {
	if s.transport == nil {
		return false
	}
	return s.transport.Send(network.PeerID(peerID), network.NewMessage(network.MessageType(msgType), payload))
}

func (s *NetworkService) Broadcast(msgType uint8, payload []byte) {
	if s.transport != nil {
		s.transport.Broadcast(network.NewMessage(network.MessageType(msgType), payload))
	}
}

func (s *NetworkService) PeerCount() int {
	if s.transport == nil {
		return 0
	}
	return s.transport.PeerCount()
}

func (s *NetworkService) IsRunning() bool {
	return s.transport != nil && s.transport.IsRunning()
}

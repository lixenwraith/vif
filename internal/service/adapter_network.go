package service

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
)

// NetworkService bridges the transport to the ECS via a drained inbound
// buffer; it holds no event queue reference
type NetworkService struct {
	config   *network.Config
	port     *network.SocketPort
	disabled atomic.Bool
}

// Pass nil config to disable or initialize with defaults
func NewNetworkService(cfg *network.Config) *NetworkService {
	if cfg == nil {
		cfg = network.DefaultConfig()
	}
	return &NetworkService{
		config: cfg,
	}
}

func (s *NetworkService) Name() string           { return "network" }
func (s *NetworkService) Dependencies() []string { return nil }

func (s *NetworkService) Init() error {
	if s.config.Role == network.RoleNone {
		s.disabled.Store(true)
		return nil
	}
	s.port = network.NewSocketPort(s.config)
	return nil
}

func (s *NetworkService) Start() error {
	if s.disabled.Load() || s.port == nil {
		return nil
	}
	return s.port.Start()
}

func (s *NetworkService) Stop() error {
	if s.port != nil {
		return s.port.Close()
	}
	return nil
}

func (s *NetworkService) Contribute(r *engine.Resource) {
	if s.disabled.Load() {
		return
	}
	r.Network = engine.NewNetworkResource(s.port)
}

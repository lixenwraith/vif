package service

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/engine"
	link "github.com/lixenwraith/vif/pkg/network"
)

// NetworkService bridges the transport to the ECS via a drained inbound
// buffer; it holds no event queue reference
type NetworkService struct {
	config   *link.Config
	port     *link.SocketPort
	disabled atomic.Bool
}

// Pass nil config to disable or initialize with defaults
func NewNetworkService(cfg *link.Config) *NetworkService {
	if cfg == nil {
		cfg = link.DefaultConfig()
	}
	return &NetworkService{
		config: cfg,
	}
}

func (s *NetworkService) Name() string           { return "network" }
func (s *NetworkService) Dependencies() []string { return nil }

func (s *NetworkService) Init() error {
	if s.config.Role == link.RoleNone {
		s.disabled.Store(true)
		return nil
	}
	s.port = link.NewSocketPort(s.config)
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

// Port returns the poll endpoint after Init, nil for a disabled role.
func (s *NetworkService) Port() *link.SocketPort { return s.port }

func (s *NetworkService) Contribute(r *engine.Resource) {
	if s.disabled.Load() {
		return
	}
	r.Network = engine.NewNetworkResource(s.port)
}

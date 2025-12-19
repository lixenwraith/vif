package status

// StatusService wraps Registry as a Service
// Registry is stateless at init; service wrapper provides lifecycle conformance
type StatusService struct {
	registry *Registry
}

// NewService creates a new status service with initialized registry
func NewService() *StatusService {
	return &StatusService{
		registry: NewRegistry(),
	}
}

// Name implements Service
func (s *StatusService) Name() string {
	return "status"
}

// Dependencies implements Service
func (s *StatusService) Dependencies() []string {
	return nil
}

// Init implements Service
func (s *StatusService) Init(world any) error {
	return nil
}

// Start implements Service
func (s *StatusService) Start() error {
	return nil
}

// Stop implements Service
func (s *StatusService) Stop() error {
	return nil
}

// Registry returns the underlying metrics registry
func (s *StatusService) Registry() *Registry {
	return s.registry
}
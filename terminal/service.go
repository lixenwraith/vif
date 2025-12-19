package terminal

// TerminalService wraps an already-initialized Terminal as a Service
// Terminal is a "bootstrap" service - Init/Start are no-ops since it must
// be initialized before the service hub exists (to provide dimensions)
//
// Stop is also a no-op; terminal restoration is handled by main.go defer
// to ensure cleanup even on panic
type TerminalService struct {
	term Terminal
}

// NewService creates a service wrapper for an initialized terminal
func NewService(term Terminal) *TerminalService {
	return &TerminalService{term: term}
}

// Name implements Service
func (s *TerminalService) Name() string {
	return "terminal"
}

// Dependencies implements Service
func (s *TerminalService) Dependencies() []string {
	return nil
}

// Init implements Service (no-op for bootstrap service)
func (s *TerminalService) Init(world any) error {
	return nil
}

// Start implements Service (no-op for bootstrap service)
func (s *TerminalService) Start() error {
	return nil
}

// Stop implements Service (no-op - terminal cleanup via main.go defer)
func (s *TerminalService) Stop() error {
	return nil
}

// Terminal returns the wrapped terminal instance
func (s *TerminalService) Terminal() Terminal {
	return s.term
}
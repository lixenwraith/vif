package services

// Service defines the lifecycle interface for non-ECS game subsystems
// Services handle meta-game concerns: audio, networking, debug tools
type Service interface {
	// Name returns the unique identifier for this service
	Name() string

	// Init receives the GameContext for dependency injection
	// Called after World and Resources are initialized
	Init(ctx any) error

	// Start begins service operation
	// Called after all services are initialized
	Start() error

	// Stop halts service operation and releases resources
	Stop() error
}
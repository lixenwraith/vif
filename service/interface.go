package service

// Service defines the lifecycle interface for infrastructure subsystems
// Services manage long-lived resources: audio backends, content pipelines, terminals
//
// Lifecycle:
//  1. Construction (via factory)
//  2. Init(args...) - implicit configuration (e.g. from parsed flags/env)
//  3. Start() - launch background goroutines
//  4. [runtime operation]
//  5. Stop() - halt goroutines, release resources
type Service interface {
	// Name returns the unique identifier for this service
	Name() string

	// Dependencies returns names of services that must Init before this one
	// Return nil or empty slice if no dependencies
	Dependencies() []string

	// Init configures the service from optional args
	// Args are service-specific (color mode, mute state, file patterns)
	Init(args ...any) error

	// Start begins service operation (launches goroutines if any)
	// Called after all services have initialized
	Start() error

	// Stop halts service operation and releases resources
	// Must be idempotent - safe to call multiple times
	Stop() error
}

// ResourcePublisher is a callback for services to contribute ECS resources
// Services call this with wrapped resources; receiver handles type routing
type ResourcePublisher func(resource any)

// ResourceContributor is implemented by services that expose APIs to the ECS layer
// Optional interface - services not implementing it are skipped during contribution
type ResourceContributor interface {
	Contribute(publish ResourcePublisher)
}
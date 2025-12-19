package services

// Service defines the lifecycle interface for infrastructure subsystems
// Services manage long-lived resources: audio backends, content pipelines, telemetry
//
// Lifecycle:
//  1. Construction (via factory)
//  2. Init(world) - resolve dependencies from World.Resources
//  3. Start() - launch background goroutines
//  4. [runtime operation]
//  5. Stop() - halt goroutines, release resources
type Service interface {
	// Name returns the unique identifier for this service
	Name() string

	// Dependencies returns names of services that must Init before this one
	// Return nil or empty slice if no dependencies
	Dependencies() []string

	// Init receives the World for dependency resolution via Resources
	// Called after all dependencies have initialized
	Init(world any) error

	// Start begins service operation (launches goroutines if any)
	// Called after all services have initialized
	Start() error

	// Stop halts service operation and releases resources
	// Must be idempotent - safe to call multiple times
	Stop() error
}
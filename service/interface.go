package service

import (
	"github.com/lixenwraith/vi-fighter/engine"
)

// Service is a lifecycle-managed external resource (I/O boundary).
// Hub drives Init → Start → Stop in dependency order with rollback.
type Service interface {
	Name() string
	// Dependencies lists service names required before this one
	Dependencies() []string
	// Init acquires resources; no background goroutines
	Init() error
	// Start begins background work
	Start() error
	// Stop releases resources; idempotent
	Stop() error
}

// ResourceContributor attaches service capabilities to the ECS resource set.
// Called by Hub.BindResources after InitAll, in dependency order.
type ResourceContributor interface {
	Contribute(r *engine.Resource)
}

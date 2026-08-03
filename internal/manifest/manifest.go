package manifest

//go:generate go run ../gen-manifest

// Package manifest contains the authoritative game component, system, and renderer definitions
//
// Code generation:
//   - internal/engine/component_store_gen.go: Component struct, entity lifecycle methods
//   - internal/manifest/build_gen.go: typed system and renderer builders, ActiveSystems
//   - internal/event/registry_gen.go: Event registry, derived from event/type.go
//   - internal/input/strings_gen.go: Reverse lookup strings for input constants
//
// Run 'go generate ./internal/manifest' to regenerate.

package manifest

//go:generate go run ../cmd/gen-manifest

// Package manifest contains the authoritative game component, system, and renderer definitions
//
// Code generation:
//   - engine/component_store_gen.go: Component struct, entity lifecycle methods
//   - manifest/build_gen.go: typed system and renderer builders, ActiveSystems
//   - event/registry_gen.go: Event registry, derived from event/type.go
//
// Run 'go generate ./manifest' to regenerate.

package manifest

//go:generate go run ../cmd/gen-manifest

// Package manifest contains the authoritative game component, system, and renderer definitions
//
// Code generation:
//   - engine/component_store_gen.go: Component struct, entity lifecycle methods
//   - manifest/register_gen.go: Registry wiring, Active* lists
//
// Run 'go generate ./manifest' to regenerate.
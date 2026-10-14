// Package manifest declares the authoritative component, system and renderer lists
// (definition.go), the simulation fingerprint two participants share, the FSM bridge
// and the FSM schema -schema prints. `go generate ./internal/manifest` derives the
// engine stores, domain table, snapshot code and builders from definition.go, the
// event registry from event/type.go, and the input enum strings from internal/input.
package manifest

//go:generate go run ../gen-manifest

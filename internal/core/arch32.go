//go:build 386 || arm || mips || mipsle

package core

// vif supports 64-bit targets only; see doc/multi-platform.md §4
var _ = vif_supports_64_bit_targets_only

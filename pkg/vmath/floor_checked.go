//go:build amd64 && !amd64.v2

package vmath

// floorChecked is whether math.Floor compiles to a CPU feature test with a call
// for its fallback, as amd64's does below its v2 level, where elsewhere it is one
// instruction. Only a build constraint knows the level, hence the two files.
const floorChecked = true

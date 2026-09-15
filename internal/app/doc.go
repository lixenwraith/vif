// Package app is the composition root: it wires the ECS world, the services around
// it and the input pipeline into one object, and owns hosting, joining, capturing
// and installing the shared world, and being driven by a journal, a script or a
// terminal. The authority protocol that world is corrected under is internal/converge.
// See doc/runtime.md and doc/desync.md.
package app

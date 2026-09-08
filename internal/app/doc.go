// Package app is the composition root: it wires the ECS world, the services around
// it and the input pipeline into one object, and owns everything a run does that is
// not a system — hosting, joining, authoring the shared world under an authority
// term, publishing the correction cadence a guest converges on, and being driven by
// a journal, a script or a terminal. See doc/runtime.md and doc/desync.md.
package app

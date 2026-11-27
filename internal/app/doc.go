// Package app is the composition root: it wires the ECS world, the services around
// it and the input pipeline into one object, driven by a presentation, a journal, a
// script or a bot graph, or serving its session as a dedicated host. Its nested
// packages hold a run's config, correction glue, session place and journal, and the
// authority protocol is internal/converge. See doc/runtime.md, doc/package-map.md.
package app

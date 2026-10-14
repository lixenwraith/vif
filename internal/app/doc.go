// Package app is the composition root: it wires the ECS world, the services around
// it and the input pipeline into one object; owns hosting, serving, joining, bot
// seats, and capturing and installing the shared world; and is driven by a terminal,
// a journal, a script or a bot graph. The authority protocol that world is corrected
// under is internal/converge. See doc/runtime.md and doc/multi-player.md.
package app

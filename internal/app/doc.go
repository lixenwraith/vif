// Package app is the runtime: it wires the ECS world, the services around it and
// the input pipeline into one object, and it owns everything a run does that is not
// a system — hosting, joining, authoring the shared world, and being driven by a
// journal, a script or a terminal.
//
// # The runtime
//
// App holds a world, a service hub, a scheduler and — for a mode that presents —
// a terminal, a renderer and an input machine. Config decides which of those exist;
// a driven run (headless, replay, script) advances on a manual clock and owns no
// goroutine the caller did not ask for, which is what makes a run a pure function of
// its seed, its config and its injected events.
//
// # The session
//
// Exactly one instance authors the Shared world. Everything else — every other
// participant's cursor included — is a prediction that the authority corrects. The
// unit of authorship is the authority term: every authoritative artifact carries the
// term it was produced under, an artifact from an older term is ignored, and one
// from a term this instance was never handed is the split-brain case and is refused
// loudly. Losing the authority elects a successor by report, vote and handoff, one
// vote per participant per term, so two authorities in one term are impossible
// rather than unlikely.
//
// A participant joins by receiving the world rather than reproducing it, and it is
// admitted as a peer before the world is read for it: the crossings produced during
// the transfer then reach it instead of falling into the gap between the capture and
// the admission.
//
// # The correction
//
// The host publishes on a cadence and a guest consumes; neither negotiates and
// neither re-derives the other's answer. That is weakened D-11 — identical on the
// host, on a guest equal to the host as of the last applied correction, and
// converging. Four properties hold it up:
//
//   - Nothing is acknowledged and nothing fails. A keyframe supersedes everything
//     before it, so loss costs freshness for at most one keyframe interval and never
//     correctness; late arrival, a delta against a keyframe this instance does not
//     hold, and supersession before apply are counters rather than errors.
//   - One timeline, one baseline. A correction is computed once and is exact: every
//     guest holds the same keyframe and a delta names it. Only which corrections a
//     peer is sent — its cadence — is per peer, and only ever its send *time*, never
//     its content.
//   - The floor is not adaptive. Cadence and keyframe interval are preferences
//     traded away under pressure; the guarantee that a participant sees a whole
//     authoritative world within SnapshotFloorKeyframeTicks is not. A link that
//     cannot carry it is refused at admission and reported mid-session.
//   - Timing paces the transport and enters nothing else. No round trip, delivery
//     rate or jitter estimate reaches a component store, an RNG stream, a replay or a
//     game decision.
//
// In front of that floor sits the selective exchange: a manifest of section hashes,
// a request naming the pages that disagreed, and a shard set carrying only those
// pages — which on a converged link moves no state at all. Every failure in it ends
// at the keyframe that was going to be sent anyway.
//
// # Reading the rest
//
// Each file opens with a comment covering the part of this it implements:
// app.go wires the runtime, session.go and host.go open and join sessions,
// authority.go owns the term and the succession, correction*.go the publication
// cadence and the selective exchange, capture.go and snapshot_*.go what a capture
// carries and what an install re-derives, and headless.go, replay*.go, script.go,
// play.go and serve.go the ways a run can be driven.
package app

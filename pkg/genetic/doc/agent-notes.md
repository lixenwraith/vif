# Agent Notes

What agents working on this repository learned that no rule in
[AGENTS.md](../AGENTS.md) and nothing in the [README](../README.md) states: facts
that cost a session an hour to rediscover, and how to check them. Read it before
starting. When a task teaches you one, add it in the same PR, one bullet under its
topic; delete a note that proves wrong or that the README now states, and link the
README instead.

## Genetic

- `StreamingStateVersion` names the layout `Checkpoint` writes and `Restore`
  reads. A change to `StreamingState`'s layout bumps it, and a host that carries
  the checkpoint in a capture of its own bumps that capture's version too: vif's
  capture holds `registry.SpeciesState`, which embeds it.
- A program over the library is tuned by its `-headless` table, several seeds
  at once in the background with a `sed` variant per constant, not by watching
  it: `examples/lander`'s crash rate at one setting varies by half between
  seeds, and its archive statistics read well long before its streams do.
- An elitist archive under a noisy objective selects for luck. Score a
  genotype by the mean of several evaluations and re-evaluate archived members
  (`examples/lander`'s trials and refreshes) before touching the operators.
- `Engine.Checkpoint`'s members alias the engine's buffers, which the next
  generation writes over: copy them before running the source engine on, as its
  test does. `Restore` into an engine of another configuration or pool size is
  refused rather than reshaped.
- An assignment genome under `CategoricalPerturbator` wants a rate of one over
  its length and a `MixProbability` near one, so most children differ from a
  parent by one move.

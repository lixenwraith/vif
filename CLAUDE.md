# Working rules

It is the contract, not a suggestion.

## Comments

- A comment block is **at most 5 lines**. If the explanation needs more, the code
  is wrong or the explanation belongs in `doc/`.
- One line is the default. Comment *why*, never *what* — the code says what.
- No narrative, no history ("this used to…", "the plan proposed…"), no restating
  the diff, no essays on design philosophy. Git log and `doc/` hold those.
- No comment on a self-evident function. `// Lookup returns the address` above
  `func Lookup` is noise.
- Package doc blocks: 5 lines. Files do not get their own prologue.

## Files

- Do not create a file for one or a few helpers. Put it next to what uses it or if shared, there is usually a main or type file in the package that can hold them.
- A new file needs a new concept, not a new function.
- Prefer editing an existing file over adding one.
- Refactor comments as you review and edit the files, there are pre-existing files that do not follow the guidelines.

## Data

- Embedded files live in `internal/asset/`. External files live in `wad/`, laid
  out exactly as they install. Nowhere else, and never beside the code that
  reads them.
- `pkg/` never imports `internal/`. A leaf package that needs game data takes it
  from its caller.
- Resolution is flag, `-config-dir`, user root, XDG system roots, embedded.
  Never add a working-directory probe.

## Scope

- Implement what was asked. Do not add tables, indirection, telemetry, tests or
  abstraction that nothing asked for.
- One mechanism per problem. Two mechanisms doing one job is a bug.
- Delete before adding. If a change is net-positive lines for a fix, justify it.
- Reverting an existing API to "improve" it is not a fix. Leave working code alone.

## Tests

- One test per rule, named for the rule. No test that restates another.
- Test comments follow the 5-line limit.
- Do not pin lists that a human has to hand-maintain unless the pin prevents a
  real regression.
- Collapse the tests: if a complex test covers a simple test scope, delete existing simple test or do not add the simple test. Do not add tests for obvious and simple functionalities that are unlikely to fail.

## Docs and commits

- `doc/` is already long. Condense when you touch it; do not append unless new concept or scope is being added.
- A gap you are deferring goes in `doc/todo.md` as one line, not a comment.
- PR bodies: what changed, why, how it was verified.

## Gates

`go generate ./internal/event ./internal/manifest`, `go build ./...`,
`go test ./...`, `go vet ./...`,
`gofmt -l` on changed files, `test/scenario.sh all`.
- Do not run `-race` test, it takes a long time and user verifies it.

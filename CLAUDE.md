# Working rules

Edit this file. It is the contract, not a suggestion.

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

- Do not create a file for one helper. Put it next to what uses it.
- A new file needs a new concept, not a new function.
- Prefer editing an existing file over adding one.

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

## Docs and commits

- Commit subject ≤ 72 chars; body ≤ 10 lines, plain, no rhetoric.
- `doc/` is already long. Condense when you touch it; do not append.
- PR bodies: what changed, why, how it was verified. Nothing else.

## Gates

`go generate ./internal/event ./internal/manifest`, `go build ./...`,
`go test ./...`, `go test -race ./internal/app/...`, `go vet ./...`,
`gofmt -l` on changed files, `test/scenario.sh all`.

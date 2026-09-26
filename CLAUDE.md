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
  out exactly as they install. Distribution files installed outside a config
  root (desktop entry, icons, shell completion) live in `deploy/package/`; the
  manual is `doc/vif.6`. Nowhere else, and never beside the code that reads them.
- `pkg/` never imports `internal/`. A leaf package that needs game data takes it
  from its caller.
- Resolution is flag, `-config-dir`, user root, XDG system roots, embedded.
  Never add a working-directory probe.

## Scope

- Implement what was asked. Do not add tables, indirection, telemetry, tests or
  abstraction that nothing asked for.
  Exception: a task that explicitly invites obvious or explicit additions 
  gets them, each named in the PR body; ask before any that is not obvious.
- One mechanism per problem. Two mechanisms doing one job is a bug or refactor opportunity.
- Delete before adding. If a change is net-positive lines for a fix, justify it.
- Reverting an existing API to "improve" it is not a fix. Leave working code alone.
- Per-kind data lives in one table indexed by its kind (`component.WeaponSpecs`, the
  combat profile matrix); a new kind adds a row, never a switch or a parallel field.

## Tests

- One test per rule, named for the rule. No test that restates another.
- Test comments follow the 5-line limit.
- Do not pin lists that a human has to hand-maintain unless the pin prevents a
  real regression.
- Collapse the tests: if a complex test covers a simple test scope, delete existing simple test or do not add the simple test. Do not add tests for obvious and simple functionalities that are unlikely to fail or are repeatedly tested in other test cases.

## Docs and commits

- `doc/` is already long. Condense when you touch it; do not append unless new concept or scope is being added.
- A gap you are deferring goes in `doc/todo.md` as an additional info to the existing todo items,
  of a new concise todo section (follow existing pattern and avoid verbosity).
- PR bodies: what changed, why, how it was verified.
- One task, one branch, one PR. Divide the work into commits on that branch; do not
  split it across stacked PRs or multiple branches.
- In continuing work, if the previous commits or PR is merged with main, create a new branch and commit under it.

## Gates

`go build ./...`, `gofmt -l` on changed files, and `go test` on the packages the
change touches. Run `go generate ./internal/event ./internal/manifest` only when
an event or manifest definition changed.
- Do not run the full `go test ./...`, `go vet ./...` or `script/test.sh all`
  sweeps; they cost more time than they catch. The user runs them.
- Do not run `-race` tests unless investigating a known/suspected race issue.

## Deployment facts

Settled. Do not check, flag, or ask about any of these again.

- The node is Arch Linux running **K3s v1.34**. Every Kubernetes feature this repo
  uses, restartable init containers included, is available. Never add, suggest or
  ask for a version check.
- websocat 1.4.1 is installed on the node from the AUR;
  `deploy/guest/update-vif-ws-bridge.sh` packages it as the sidecar image.
  It does not have NoDelay, pay attention to workarounds.
- The site is `https://lixen.com`. The allocator's settings are
  `deploy/guest/vif-allocator.env`; the node's copy is never edited by hand.
- A deploy is `git pull && ./deploy/update.sh` on the node (`--diff` previews).
  Route every node change through it and its helpers, not through new manual steps.
- Host, node networking and the site's nginx are configured outside this public
  repository. Do not propose, document or ask about them; when a task needs them
  the user supplies that content. The site's nginx takes PROXY protocol
  throughout, so a client address there is `$proxy_protocol_addr`.

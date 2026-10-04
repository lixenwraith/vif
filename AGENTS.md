# Working rules

It is the contract, not a suggestion.

## Comments

- A comment block is **at most 5 lines**. If the explanation needs more, the code
  is wrong or the explanation belongs in `doc/`.
- One line is the default. Comment *why*, never *what* — the code says what.
- No narrative, no history ("this used to…", "the plan proposed…"), no restating
  the diff, no essays on design philosophy. Git log and `doc/` hold those.
- No dates, commit or PR references, or plan/phase labels in comments. Multi-step
  work is tracked in a working doc under `doc/` (e.g. `doc/machine-design.md`) or
  an item in `doc/todo.md`, never in code.
- No comment on a self-evident function. `// Lookup returns the address` above
  `func Lookup` is noise.
- Package doc blocks: 5 lines. Files do not get their own prologue.

## Files

- Do not create a file for one or a few helpers. Put them next to what uses them,
  or, if shared, in the package's main or type file.
- A new file needs a new concept, not a new function.
- Prefer editing an existing file over adding one.
- When you edit a file, bring its comments up to these rules; older files predate
  them.

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
  Exception: a task that explicitly invites additions gets the obvious ones, each
  named in the PR body; ask before any that is not obvious.
- One mechanism per problem. Two mechanisms doing one job is a bug or a refactor
  opportunity.
- Delete before adding. If a change is net-positive lines for a fix, justify it.
- Reverting an existing API to "improve" it is not a fix. Leave working code alone.
- Per-kind data lives in one table indexed by its kind (`component.WeaponSpecs`, the
  combat profile matrix); a new kind adds a row, never a switch or a parallel field.

## Tests

- One test per rule, named for the rule. No test that restates another.
- Test comments follow the 5-line limit.
- Do not pin lists that a human has to hand-maintain unless the pin prevents a
  real regression.
- Collapse tests: when a broader test covers a narrower one, delete the narrower
  one or do not add it. No test for simple behaviour that is unlikely to fail or
  is already exercised elsewhere.

## Docs and commits

- `doc/` is already long. Condense when you touch it; append only for a new concept
  or scope.
- A gap you are deferring goes in `doc/todo.md`: extend the item it belongs to, or
  add a concise one in the file's pattern.
- PR bodies: what changed, why, how it was verified.
- One task, one branch, one PR. Divide the work into commits on that branch; do not
  split it across stacked PRs or multiple branches.
- Work that continues after its PR merged starts again from the latest main; never
  stack new commits on merged history.

## Gates

`go build ./...`, `gofmt -l` on changed files, and `go test` on the packages the
change touches. Run `go generate ./internal/event ./internal/manifest` only when
an event or manifest definition changed.
- A change to code behind a build constraint also builds that target:
  `GOOS=js GOARCH=wasm go build ./...` for `js/wasm`, `go build -tags vif_headless
  ./...` for the headless profile.
- Do not run the full `go test ./...`, `go vet ./...` or `script/test.sh all`
  sweeps; they cost more time than they catch. The user runs them.
- Do not run `-race` tests unless investigating a known/suspected race issue.

## Deployment facts

Settled. Do not check, flag, or ask about any of these again.

- The node is Arch Linux running K3s, at the release the deployment procedure pins.
  Every Kubernetes feature this repo uses, restartable init containers included, is
  available. Never add, suggest or ask for a version check.
- The browser route's WebSocket is `pkg/websocket`, terminated in the allocator and
  spliced to the pod's game port. There is no WebSocket sidecar or bridge image.
- The site is `https://lixen.com`. The allocator's settings are
  `deploy/guest/vif-allocator.env`; the node's copy is never edited by hand.
- A deploy is `git pull && ./deploy/update.sh` on the node (`--diff` previews).
  Route every node change through it and its helpers, not through new manual steps.
- Host, node networking and the site's nginx are configured outside this public
  repository. Do not propose or document their configuration. When a task depends
  on them, ask the user for the specific output or content instead of guessing. The
  site's nginx takes PROXY protocol throughout, so a client address there is
  `$proxy_protocol_addr`.
- The repository describes the architecture, never a machine: docs and commands
  use placeholders for addresses, node names and credentials; only the public
  site's URL is named.

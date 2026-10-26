# Working rules

It is the contract, not a suggestion.

## Comments

- A comment block is **at most 5 lines**. If the explanation needs more, the code
  is wrong or the explanation belongs in `doc/`.
- One line is the default. Comment *why*, never *what* — the code says what.
- No narrative, no history ("this used to…", "the plan proposed…"), no restating
  the diff, no essays on design philosophy. Git log and `doc/` hold those.
- No dates, commit or PR references, or plan/phase labels in comments. Multi-step
  work is tracked in a working doc under `doc/` (e.g. `doc/generic-kinetic.md`) or
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
- Resolution is flag, `-config-dir`, user root, the checkout `wad/` beside a
  `bin/` binary, system roots (`/usr/local/etc/vif`, `/etc/vif`), embedded.
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
- The game is in development: a change may break the wire protocol, the journal,
  the log, captures or settings. Bump the version the artifact carries and move on;
  never hold or narrow a fix for backward compatibility.

## Platforms

- One game on every host: terminal, desktop window, Android and the browser.
  A command, key, gesture, layout or setting works the same on each wherever the
  host allows; a difference the host forces is documented in
  `doc/multi-platform.md`, and a touch gesture one touch host has the other gets.
- What a player reads (status bar, menu, errors, manual) uses one vocabulary on
  every host. Name the platform as the player knows it ("a browser", "Android",
  "this build", "this run"), never a package, build tag, GOOS or system call.
- A feature a host lacks reads `<Thing> unavailable in a browser`, `in this build`
  or `in this run` (`internal/mode` `unavailable`); a menu row's reason and its
  command's refusal are the same sentence. An error shown starts with a capital.
- A setting the player changes is saved by one rule on every host
  (`paths.SettingsFor`, `paths.SettingsStore`).
- Fix an inconsistent message, comment or doc you come across.

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
- Docs are read in terminals, where a Markdown table renders as its source. Lay
  tabular content out as a nested list; a matrix of figures goes in a fenced code
  block with its columns aligned by spaces, which the terminal keeps. A table that
  neither fits, or that converting would only lengthen, stays a table.
- `doc/term-index.md` cites identifiers and section anchors: a rename of either, or
  a new term of art, updates its entry.
- A gap you are deferring goes in `doc/todo.md`: extend the item it belongs to, or
  add a concise one in the file's pattern.
- PR bodies: what changed, why, how it was verified.
- A change made for performance reports its measurement, before and after, in the
  PR body and in the design doc it serves: the benchmark or the reading, the
  shape it was taken in, and the benefit in the unit the user feels (bytes, CPU,
  lock time, latency). A projection is not a result; a change whose measured
  benefit is nil says so.
- One task, one branch, one PR. Divide the work into commits on that branch; do not
  split it across stacked PRs or multiple branches.
- Work that continues after its PR merged starts again from the latest main; never
  stack new commits on merged history.
- Finish and push one commit before starting the next.
- Once a PR is merged or closed unmerged, delete its branch on GitHub and locally
  if this session can (`gh pr merge --delete-branch` does both; a branch a
  worktree holds goes with that worktree, which the app that made it removes).
  A session that cannot, because it has no remote access or works in a copy,
  says so once in its report and leaves the branch; it never retries.

## Agents

- Spin off few agents, one at a time rather than a fan-out, and only for work one
  context cannot hold. The account quota may run out and, which stops every agent
  in flight at once.
- An agent writes what it finds to a file as it goes, one section per step, under
  the scratchpad or `doc/`, so an interrupt costs a step and not the task; the
  parent reads those files rather than waiting for a final report, and resumes
  from them instead of running the agent again.

## Gates

`go build ./...`, `gofmt -l` on changed files, and `go test` on the packages the
change touches. Run `go generate ./internal/manifest` (`make generate`), which
also writes the event registry, only when an event or manifest definition changed.
- A mechanical edit across files is checked by a count before and after, with a
  search that reads its pattern as the file holds it (`grep -P` for a tab, or a
  literal tab); a search that matches nothing proves nothing, and the PR body
  states the count.
- A change to code behind a build constraint also builds that target:
  `GOOS=js GOARCH=wasm go build ./...` for `js/wasm`, `go build -tags vif_headless
  ./...` for the headless profile.
- Do not run the full `go test ./...`, `go vet ./...` or `script/test.sh all`
  sweeps; they cost more time than they catch. The user runs them.
- Do not run `-race` tests unless investigating a known/suspected race issue.
- A change to pacing, the lead, the compared surface or a shared system's state
  runs the divergence harness over its four shapes before and after (netcode
  design §3.5) and reads the late counts, which are robust, over pages repaired,
  which are chaotic.

## Deployment facts

Settled. Do not check, flag, or ask about any of these again.

- The node is Arch Linux running K3s, at the release the deployment procedure pins.
  Every Kubernetes feature this repo uses, restartable init containers included, is
  available. Never add, suggest or ask for a version check.
- The browser route's WebSocket is `pkg/websocket`, terminated in the router and
  spliced to the pod's game port. There is no WebSocket sidecar or bridge image.
- The site is `https://lixen.com`. The router's settings are
  `deploy/guest/vif-router.env`; the node's copy is never edited by hand.
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

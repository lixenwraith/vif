# Working rules

It is the contract, not a suggestion.

Read [doc/agent-notes.md](doc/agent-notes.md) before starting: it holds what earlier
agents learned that neither these rules nor the [README](README.md),
[doc/stapes.md](doc/stapes.md) and [doc/rip-design.md](doc/rip-design.md) state.
When a task teaches you something the next agent would otherwise rediscover, add
it there in the same PR.

## Comments

- A comment block is **at most 5 lines**. If the explanation needs more, the code
  is wrong or the explanation belongs in the README or `doc/`.
- One line is the default. Comment *why*, never *what* — the code says what.
- No narrative, no history ("this used to…", "the plan proposed…"), no restating
  the diff, no essays on design philosophy. Git log and `doc/` hold those.
- No dates, commit or PR references, or plan/phase labels in comments. Multi-step
  work is tracked in a working doc under `doc/` (e.g. `doc/rip-design.md`) or an
  item in `doc/todo.md`, never in code.
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

## Platforms

- One vocabulary on every host: a verb, key, message or path reads and works the
  same in the REPL, the TUI, a script and a host that runs the package
  in-process; a difference a host forces is documented in `doc/stapes.md`.
- Terminal hosts: over ssh and on a raw tty, on Ubuntu, Arch and FreeBSD, with
  cgo off. Windows is not a target.
- Android is to come, run in-process by vif's app with its own screen and
  speakers: the package takes its sink and its terminal from the host, and only
  `cmd/stapes` reads flags, signals and the process's terminal.

## Data

- No embedded files of its own: the shipped banks are the audio module's `bank`
  (`lixenwraith/audio/bank`).
- Files resolve through the root and the search path (`ResolveFiles`), or a path
  a verb or a flag names. Never add a working-directory probe.
- A script's relative paths are read from the script's own folder, a nested
  `run`'s from its own.

## Scope

- Implement what was asked. Do not add tables, indirection, telemetry, tests or
  abstraction that nothing asked for.
  Exception: a task that explicitly invites additions gets the obvious ones, each
  named in the PR body; ask before any that is not obvious.
- One mechanism per problem. Two mechanisms doing one job is a bug or a refactor
  opportunity.
- Delete before adding. If a change is net-positive lines for a fix, justify it.
- Reverting an existing API to "improve" it is not a fix. Leave working code alone.
- Per-kind data lives in one table indexed by its kind (`commands`, a row a verb);
  a new kind adds a row, never a switch or a parallel field.
- The module imports nothing of vif: it stands on `lixenwraith/audio` (the
  engine, `bank` and `ripmodel`), `lixenwraith/genetic`, `lixenwraith/terminal`,
  `lixenwraith/color` and `lixenwraith/toml`. A change the engine or the searches
  need lands in that module, under its own rules, and stapes takes its tag. No
  file carries a build tag.
- The module is at v0: a change may break the API, a verb or a document's keys,
  and takes the next minor version; never hold or narrow a fix for backward
  compatibility.

## Tests

- One test per rule, named for the rule. No test that restates another.
- Test comments follow the 5-line limit.
- Do not pin lists that a human has to hand-maintain unless the pin prevents a
  real regression.
- Collapse tests: when a broader test covers a narrower one, delete the narrower
  one or do not add it. No test for simple behaviour that is unlikely to fail or
  is already exercised elsewhere.
- A test session opens with no files (`Options.Files` zero), so it reads no
  root and no search path; a test of resolution points `XDG_CONFIG_HOME` at a
  temporary directory.

## Training

- A round runs from its own copy of the tree: `tool/rip-train/pipeline.sh` runs
  `go run ./cmd/stapes` at every stage, so an edit to the checkout under a round
  changes its corpus or its bench.
- A change to the corpus or the bench reports the `bench` lines before and after,
  on the same weights, and says whether the corpus bytes change (`corpus <dir> 24
  4 aug gen nowav` before and after, `diff -r`), and so whether a new round is
  needed.

## Docs and commits

- Docs are short. Condense when you touch them; append only for a new concept or
  scope.
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
  PR body: the benchmark or the reading, the shape it was taken in, and the
  benefit in the unit the user feels (CPU, bytes, allocations, latency). A
  projection is not a result; a change whose measured benefit is nil says so.
- Commits are versions: each commit on main starts with the version it makes
  (`v0.1.1 rip: ...`), the next minor for a break or a new feature and the next
  patch otherwise, and Lixen tags it. A PR's title is its squashed commit's
  message, so it starts with the version too.
- One task, one branch, one PR. Divide the work into commits on that branch; do not
  split it across stacked PRs or multiple branches.
- Work that continues after its PR merged starts again from the latest main; never
  stack new commits on merged history.
- Finish and push one commit before starting the next.
- Once a PR is merged or closed unmerged, delete its branch on GitHub and locally
  if this session can. A session that cannot, because it has no remote access or
  works in a copy, says so once in its report and leaves the branch; it never
  retries.

## Agents

- Spin off few agents, and only for work one context cannot hold. The account
  quota may run out, which stops every agent in flight at once.
- Agents run in parallel only on finely defined scopes that cannot conflict in
  commits: each brief names the files its agent owns, disjoint from every other
  agent's, and the agent touches nothing else. Shared docs (the README,
  `doc/stapes.md`, `doc/term-index.md`, `doc/todo.md`) stay with the parent,
  which commits each agent's work in turn on the task's one branch.
- An agent writes what it finds to a file as it goes, one section per step, under
  the scratchpad or `doc/`, so an interrupt costs a step and not the task; the
  parent reads those files rather than waiting for a final report, and resumes
  from them instead of running the agent again.

## Gates

`go build ./...`, `gofmt -l` on changed files by the toolchain's own gofmt,
`go test` on the packages the change touches (the root package's suite takes
minutes), and the FreeBSD and Android builds: `CGO_ENABLED=0` with `GOOS=freebsd
GOARCH=amd64` and `GOOS=android GOARCH=arm64` `go build ./...`. `make verify` is
the sweep CI runs.
- A mechanical edit across files is checked by a count before and after, with a
  search that reads its pattern as the file holds it (`grep -P` for a tab, or a
  literal tab); a search that matches nothing proves nothing, and the PR body
  states the count.
- Do not run `-race` tests unless investigating a known/suspected race issue; CI
  runs them, with the cross builds.

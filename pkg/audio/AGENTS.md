# Working rules

It is the contract, not a suggestion.

Read [doc/agent-notes.md](doc/agent-notes.md) before starting: it holds what earlier
agents learned that neither these rules nor the [README](README.md) and
[doc/audio.md](doc/audio.md) state. When a task teaches you something the next
agent would otherwise rediscover, add it there in the same PR.

## Comments

- A comment block is **at most 5 lines**. If the explanation needs more, the code
  is wrong or the explanation belongs in the README or `doc/`.
- One line is the default. Comment *why*, never *what* — the code says what.
- No narrative, no history ("this used to…", "the plan proposed…"), no restating
  the diff, no essays on design philosophy. Git log and `doc/` hold those.
- No dates, commit or PR references, or plan/phase labels in comments. Multi-step
  work is tracked in a working doc under `doc/` or an item in `doc/todo.md`, never
  in code.
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

- Embedded files live in `bank/` only: the sounds, the drum kit, the voices and
  the music vif and stapes ship.
- The root package ships no content: every sound, voice and pattern it plays is
  data its embedder hands it, from `bank` or from documents of its own.

## Scope

- Implement what was asked. Do not add tables, indirection, telemetry, tests or
  abstraction that nothing asked for.
  Exception: a task that explicitly invites additions gets the obvious ones, each
  named in the PR body; ask before any that is not obvious.
- One mechanism per problem. Two mechanisms doing one job is a bug or a refactor
  opportunity.
- Delete before adding. If a change is net-positive lines for a fix, justify it.
- Reverting an existing API to "improve" it is not a fix. Leave working code alone.
- Per-kind data lives in one table indexed by its kind (`midiInstruments`, a row
  an instrument); a new kind adds a row, never a switch or a parallel field.
- The root package imports nothing beyond the standard library and
  `lixenwraith/toml` (its `macro` expander included); `ripmodel` adds
  `lixenwraith/transformer`. No file carries a build tag: a backend a host lacks
  is passed over at run time, so every package builds for every target.
- The module is at v0: a change may break the API or a document's keys, and
  takes the next minor version; never hold or narrow a fix for backward
  compatibility.

## Tests

- One test per rule, named for the rule. No test that restates another.
- Test comments follow the 5-line limit.
- Do not pin lists that a human has to hand-maintain unless the pin prevents a
  real regression.
- Collapse tests: when a broader test covers a narrower one, delete the narrower
  one or do not add it. No test for simple behaviour that is unlikely to fail or
  is already exercised elsewhere.
- A test registers stand-in voices, never the bank, unless it tests the bank
  (`bank`'s own tests), and clears the registries after (`ResetRegistries`).

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
- A change to a render or a voice reports its measurement, before and after, in
  the PR body: levels re peak and the first half second's RMS over a few notes
  and velocities (`RenderNote`), against the recordings it was fitted to where
  there are some. It names the vif and stapes bumps it needs, since a moved
  render changes what vif plays and what stapes' corpus and bench read.
- Commits are versions: each commit on main starts with the version it makes
  (`v0.1.1 bank: ...`), the next minor for a break or a new feature and the next
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
  `doc/audio.md`, `doc/term-index.md`, `doc/todo.md`) stay with the parent, which
  commits each agent's work in turn on the task's one branch.
- An agent writes what it finds to a file as it goes, one section per step, under
  the scratchpad or `doc/`, so an interrupt costs a step and not the task; the
  parent reads those files rather than waiting for a final report, and resumes
  from them instead of running the agent again.

## Gates

`go build ./...`, `gofmt -l` on changed files, `go test` on the packages the
change touches, and the cross builds: `GOOS=js GOARCH=wasm`, `GOOS=freebsd
GOARCH=amd64`, `GOOS=android GOARCH=arm64` and `GOARCH=386` `go build ./...`.
- A mechanical edit across files is checked by a count before and after, with a
  search that reads its pattern as the file holds it (`grep -P` for a tab, or a
  literal tab); a search that matches nothing proves nothing, and the PR body
  states the count.
- A change that must not move what plays (a refactor, a speed-up) leaves the
  renders bit-identical before and after, on one platform: every bank sound
  (`RenderPreview`) and a song of the bank's patterns (`RenderTimeline`), written
  with `WriteWAV` and compared with `cmp`; the PR body says what it rendered.
- Do not run `-race` tests unless investigating a known/suspected race issue; CI
  runs them, with the cross builds.

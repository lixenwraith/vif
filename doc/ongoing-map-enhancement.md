# Live scenario change, over-the-air scenarios, and an external fleet wad

Working plan. Delete this file when the last phase lands; the durable parts move
into [HFSM and configuration](fsm-and-configuration.md),
[Multiplayer](multi-player.md), [External filesystem layout](filesystem-layout.md)
and [Deploying the session fleet](kube-docker-deploy.md) as each phase completes.

## 0. What a scenario is

A scenario is what a player calls a map: a `scenario.toml` entry plus the region
files it includes, rooted at one directory — `wad/scenario/main`,
`wad/scenario/td`, `wad/scenario/blank`, or the embedded `internal/asset/scenario`.
It is selected by `-s <name|path>` or by `-d`.

It used to be called a config, which named three different things at once. It is
now a scenario, and *configuration* is left to mean what settles how the process
runs — the config roots, the keymap and audio overrides, and the `vif.toml` that
`filesystem-layout.md` §3 reserves for the settings that flags and environment
variables carry today.

Three objectives, in the order the work has to happen:

1. **`:n <scenario>`** changes the scenario during play, on a solo run and on a
   live host.
2. A guest **receives the host's scenario in memory** when it does not have it,
   matched by content hash.
3. A **fleet pod serves an external `wad/`** that an operator can update live, and
   takes deployment-supplied environment.

## 1. What exists today

| Fact | Where |
|---|---|
| A run reads its scenario whole and loads the FSM from that, not from a directory. | `App.readScenario`, `internal/app/app.go` |
| A scenario is identified by its name and the SHA-256 of one canonical form, which is also the transfer body. | `resource.Scenario`, `internal/resource/scenario.go` |
| `Machine.LoadScenarioFromMap` clears the graph before building, so a second load into a live machine is mechanically possible. | `internal/fsm/loader.go` |
| A reset re-enters the loaded graph. It cannot load a different one. | `Scheduler.executeReset`, `internal/engine/scheduler.go` |
| `fsm.<region>.*` status keys are registered per declared region at load time. | `Scheduler.bindFSMTelemetry`, `internal/engine/scheduler.go` |
| The status registry is **frozen** before the first tick and never reopens. The flight recorder's ring is laid out from the frozen index. | `Registry.Freeze`, `internal/status/registry.go` |
| The system set is **sealed** at the same moment. | `World.Seal`, `internal/engine/world.go` |
| A joining App is constructed **after** the offer arrives, from `ConfigForJoin`. | `newJoiningApp`, `internal/app/session.go` |
| A whole second `App` is already built at runtime and re-used, for correction staging. | `App.newStagingApp`, `internal/app/snapshot_stage.go` |
| A chunked, bounded bulk transfer already exists for captures. | `network.EncodeSnapshotChunks`, `internal/network/snapshot.go` |
| The fleet pod runs `-d`: embedded scenario, embedded corpus, nothing mounted. | `deploy/k3s/30-session.yaml`, `tool/vif-allocator/config.go` |
| The allocator already has a "what a caller may select" mechanism. | `sessionRequest`/`fleetLimits`/`resolve`, `tool/vif-allocator/allocator.go` |

One consequence shapes everything below.

**Region names belong to the scenario.** `main` declares `main, quasar, storm,
monitor, tower`; `td` declares `td_main, td_counter, td_esc, td_storm, td_monitor,
td_player`. A different scenario needs status keys that do not exist, and the
registry that would hold them is frozen with the recorder bound to its index.
**Loading a different scenario into a running App is therefore not available**, and
re-opening the registry would have to invalidate the recorder ring, the cached
metric pointers 212 call sites hold, and the snapshot surface the session compares
on. A scenario change is an App restart.

## 2. Design

**M-1 — A scenario is content-addressed.** *(landed)* One canonical byte
serialization is both the digest input and the transfer body, so a copy that
verifies is by construction the copy that was hashed.

**M-2 — Identity is `name` plus `digest`.** *(landed)* The anchor carries the name
so a reproduction resolves it through the ordinary roots, and the digest so the
match is exact. The name is what a person reads; the digest is what a join is
refused on.

**M-3 — A scenario change is an App restart.** `:n <scenario>` validates, then
tears the App down and builds a new one from the same `Config` with a new
`Resources.Scenario`. `Run` owns the restart loop. Everything a restart already
gets right — a fresh registry, a fresh seal, a fresh journal run, a fresh D-14
latch — is what makes this the cheap answer rather than the expensive one.

**M-4 — A guest that lacks the scenario asks for it during the handshake.** The
transfer sits between the offer and the reply, on the stream the join already owns,
bounded by the handshake deadline and by the `resource.Scenario` size caps. It is
held in memory and never written to disk. The body is the canonical form
**compressed with `compress/flate`**; the digest stays over the uncompressed bytes,
so compression is a transport detail and nothing downstream has to know about it.

**M-5 — A live scenario change is a session restart, not a new mechanism.** The
host broadcasts the new name and digest and restarts; each guest restarts and
re-dials the address it joined on. The new join is an ordinary mid-run join, so it
already carries the scenario transfer, the roster, and the world install. `:n`
resets the run anyway — there is no continuity to preserve that a redial loses.

**M-6 — The pod's scenario comes from a read-only node volume, not the image.**
The image stays scenario-free and one layer. An operator replaces the node
directory atomically; running matches keep the scenario they started on, and the
next pod gets the new one.

## 3. Phases

Each phase builds, passes `gofmt -l` on its changed files, passes `go test` on the
packages it touches, and leaves the game playable. Manual checks are the acceptance
gate; agent tests are added only where a rule has no other witness.

### Phase 0 — `ClockScheduler` → `Scheduler` (done)

`internal/engine/clock_scheduler.go` is `scheduler.go`; the type, its constructor
and its receiver are renamed; the four comments where the substitution read badly
were rewritten. No conflict existed: nothing else in the tree declared `Scheduler`,
and `App.scheduler` was already the field name. Seven documents follow the rename.

### Phase 1 — Scenario vocabulary and identity (done)

Two commits.

**`Name the loaded map a scenario, not a config`.** `wad/game/` → `wad/scenario/`,
`game.toml` → `scenario.toml`, `internal/asset/config/` →
`internal/asset/scenario/`. `-g`/`-config-game` → `-s`/`-config-scenario`, removed
rather than aliased. `resource.Options.Scenario`, `resource.ScenarioPath`,
`fsm.LoadScenario*`, `fsm.ScenarioDoc`, `Scheduler.LoadScenarioFromFS`,
`paths.ScenarioDirName`/`ScenarioFile`, `asset.DefaultScenario`, and `ScenarioID` on
the anchor, the capture header and the peer identity.
`Scheduler.LoadFSMFromPath` went with its last caller.

**`Identify a scenario by its content, not by its path`.** `resource.Scenario`
reads the entry and its region files whole and hashes the canonical form;
`fsm.ScenarioFiles` reports the set from the loader's own include walk. Bounds: 64
files, 256 KiB each, 1 MiB total, `.toml` only, no escaping name. The anchor gained
`ScenarioDigest`, the identity compares it, journal schema 12 → 13 and capture
schema 6 → 7. Reading moved ahead of the services, so a bad `-s` fails before the
terminal is taken and before the journal anchor is written.

Behaviour change worth remembering: a journal names its scenario instead of
pointing at it, so replaying one recorded against a scenario that is not installed
needs the same `-config-dir` the run used. `PlayJournal` takes the operator's roots
for exactly that. `-check` prints name, file count and digest prefix.

### Phase 2 — `:n <scenario>` on a solo run (done)

`Run` owns a loop that builds one App per scenario: `runScenario` constructs, runs
and tears one down, and what `Loop` returns is the scenario to build next — empty
means the player quit. The next run is the operator's original command line with a
different `-s`, so `-seed` still pins what it pinned and nothing else drifts.

`SessionController.ChangeScenario(name) (bool, error)` validates with
`resource.ValidateScenario` and latches `App.restartScenario`, which `Loop` reads
between two waits on its own goroutine. Validation is before the teardown because
the operator typed the name: a scenario that does not resolve, or that names a
system this build lacks, is an error read with the game still running. `false`
means the name resolved to the bytes already loaded, and the command resets in
place instead — `:n main` on `main` costs nothing.

Refused on a driven mode, which has no `Run` loop, and on a run that has opened a
session, which Phase 4 takes up. `:n! <scenario>` is accepted and means the same
thing: what comes back is a fresh run, operator state included.

`resource.ValidateScenario` is the export `-check` and the command now share, so
neither can drift from the other.

**Verified.** `script/test.sh scenario` drives a real terminal through `:n td` and
back, asserting the run rebuilt each time and returned to the scenario it started
on; it skips where no `script(1)` can give it a pty.
`TestScenarioChangeNeedsARestartLoop` pins the driven-mode guard through the
command path. By hand: `:n nosuch` reports and the game continues, `:n` alone still
resets in place, `:q` still exits 0.

**Accepted cost.** The terminal is torn down and re-created with the App, so a
restart flashes the shell for one frame.

### Phase 3 — The scenario over the wire (done)

`MsgScenarioRequest` (0x16) and `MsgScenarioBody` (0x17) sit inside the join
handshake, between the offer and the reply. A joiner whose roots hold no scenario
with the offer's digest asks for it there, so it is running the session's scenario
before it reports an identity the coordinator would refuse it on. One request fits
by construction: what follows the answer has to be the reply, so a second is a
protocol error on both sides without a counter to keep.

`Coordinator.Scenario` serves only the digest being played — the transfer makes a
participant match this run, it is not a file service. The body is the canonical
form deflated, chunked with the capture's own framing, which is why `readSnapshot`
generalised into `readChunked`: a capture and a scenario are the two messages whose
size is a function of their content. A `MsgJoinReply` arriving mid-transfer is the
coordinator refusing, and its reason is what the dialer is given.

`resource.Options.Provided` is a scenario already in hand. It wins over every other
selector, is never written to disk, and `newStagingApp` now hands the staging world
this instance's loaded scenario rather than re-resolving one — which also covers a
correction on a run whose scenario exists nowhere on this host.

**Compression is over the whole container, not per file.** The files are
near-identical TOML and a shared window is most of the saving: `td` goes from about
60 KiB to 5.3 KiB, `main` from 44 KiB to 5.3 KiB. That is safe because the
container is length-prefixed rather than delimited — a reader takes exactly the
bytes each file declares and never scans for a terminator, so a file that is
truncated, malformed or deliberately unterminated cannot run into the next one.
Compression changes the bytes on the wire and nothing about the framing. The
inflate is bounded with an `io.LimitedReader` at the same ceiling a scenario read
from disk is held to, so a few kilobytes cannot be made to allocate a megabyte, and
the digest is taken over what deflate was given, so a receiver verifies exactly
what the sender hashed.

`ProtocolVersion` stays at 1, as decided: one node updated with the code, every
client in step with it. The reason is recorded at the constant, and the next wire
change reaching a fleet that is not in step has to bump it.

**Verified.** `script/test.sh transfer` starts a host on an installed `main` and a
guest whose configuration root is empty: the host serves, the guest receives,
stages and installs the session world on a scenario it could not have resolved.
`TestATransferCarriesBytesByLengthNotByScanning` round-trips a region file holding
the container's own magic, a NUL and an unbalanced quote, and refuses a deflate
bomb that inflates past the ceiling.

**Found on the way, not fixed here.** A `wad/scenario/td` world does not fit the
capture ceiling: `-serve -s td` answers a join with `snapshot encode: 10112276
plain bytes is outside 1..4194304`, so a `td` session cannot be joined mid-run at
all. It is pre-existing and independent of the transfer — a guest that already has
`td` installed fails identically — and `MaxSnapshotBytes` is documented for a world
whose captures are "single-digit kilobytes". `td`'s 500x250 map with its maze and
towers is three orders of magnitude past that. Phase 5 cannot serve `td` from the
fleet until this is decided. See `doc/todo.md`.

### Phase 4 — Live scenario change in a session

0. **The refusal to lift.** Phase 2 answers `Scenario: a run that has opened a
   session cannot change scenario` to `:n <name>` and `:n! <name>` on a host that
   has a guest — `bin/vif -host :7777 -s wad/scenario/main/` then `:n
   wad/scenario/td`. That message is this phase's entry point: when it is gone,
   the same two commands rebuild the host and carry every guest with them.
1. The coordinator's `:n <scenario>` validates as in Phase 2, then broadcasts a
   session-restart notice carrying the new name, digest and the address to redial,
   and latches its own restart. A guest's `:n <scenario>` stays refused, as `:n` is.
2. A guest receiving the notice latches a restart whose next `Config` is
   `-join <addr>` against the same address, with a bounded redial window
   (`NetworkJoinReadyTimeout` scale, a handful of attempts) covering the host's own
   rebuild. Failure to redial ends the guest's run with the reason on the way out,
   rather than leaving it in a session that no longer exists.
3. The host's restart re-binds the same address. Guests arrive through the ordinary
   mid-run gate: identity, scenario transfer if needed, capture install, roster
   slot. No new admission path.
4. A dedicated `-serve` host is out of scope: it has no operator, and its scenario
   is the pod's. Phase 5 is how that changes.

**Gates.** `go test ./internal/app`.
**Manual.** Two local instances, `:n td` on the host: both reach TD, the guest's
cursor is on the session's map, a correction lands. Guest that cannot obtain the
scenario exits with the reason. Host `:n` with no argument still resets both in
place through the existing crossing. Three participants: all three follow.
**New test.** A notice for the digest already running is a no-op, not a restart.

### Phase 5 — The fleet serves an external wad

**5a — The node directory.** `/var/db/vif/wad/`, root-owned, `0755`/`0644`,
holding the categorized layout. `deploy/guest/update-vif-wad.sh` follows the shape
of the other updaters — build or stage, verify, keep one `.previous` rollback set,
restore on failure. It stages the repository's `wad/` into `/var/db/vif/wad.new`,
validates it with the session image's own `-check -config-dir /wad -s <name>` for
every scenario it contains, then swaps by rename. A running pod keeps the inode it
mounted; the next pod gets the new tree. Unlike `update-vif-image.sh` it does
**not** require an empty fleet — updating without ending a match is the point.

**5b — The volume.** `deploy/k3s/07-wad-volume.yaml`: a no-provisioner
`StorageClass vif-node-wad`, a node-affine `local` PV `vif-fleet-wad` at
`/var/db/vif/wad` (8 Mi, Retain, `WaitForFirstConsumer`), and one PVC.
`10-quota.yaml` goes to `persistentvolumeclaims: "2"` and `requests.storage: 264Mi`.
`06-log-volume-check.yaml` gains a sibling, or grows a second mount, so a fresh
node binds both claims before a real player does.

**5c — The pod.** `30-session.yaml` and `tool/vif-allocator/manifest.go` mount the
claim read-only with `subPath: scenario` at `/wad/scenario` and `subPath: image` at
`/wad/image`, and replace `-d` with `-config-dir /wad -s ${SCENARIO}`. `content`,
`input` and `audio` are deliberately **not** mounted, so the corpus and keymap stay
embedded and the session's `ContentID` does not move — a native guest running `-d`
must still be able to join. The init container runs `-check -config-dir /wad -s
${SCENARIO}`, which finally proves the scenario the session will actually serve
rather than the embedded one; the Dockerfile comment that names this gap is
updated. `.dockerignore` gains `wad/` so the context stops carrying what the image
never uses. `-config-dir` rather than a mount at `/etc/xdg/vi-fighter`: the
explicit root does not depend on XDG defaults inside a `scratch` container. Verify
on the node that a `scratch` image with `readOnlyRootFilesystem: true` gets its
mount points created — it is the one assumption here that the cluster, not the
code, has to honour.

**5d — The allocator.** `-scenarios` is the allowlist a caller may pick from and
`-scenario` the default; `sessionRequest.Scenario` and `fleetLimits.Scenarios`
follow the shape `Players`/`LogLevel` already have, and `resolve` refuses an
unlisted name rather than clamping it. `VIF_ALLOCATOR_SCENARIOS` and
`VIF_ALLOCATOR_SCENARIO` join `vif-allocator.env.example`. The allocator does not
read the wad; the allowlist is the operator's statement about what is installed
there, and a mismatch fails the init container, which is where a configuration
error belongs.

**5e — Environment.** One optional `envFrom` a namespace `ConfigMap
vif-session-env`, `optional: true` so an absent map is not a failed pod. Deployment
state, never request state: a caller cannot set an environment variable, only
choose from `limits`. `GOMEMLIMIT` stays where it is, as a property of the
manifest's own resource envelope.

**5f — Documentation.** `kube-docker-deploy.md` gains the wad volume beside §7's
log tmpfs and §9's fleet objects, and a wad step in §15's operating notes;
`kubernetes-fleet.md` §1, §2 and §6 record the new mount and its ceiling;
`deploy/README.md`, `deploy/guest/README.md` and `deploy/runbook.md` gain the new
file and the new helper; `filesystem-layout.md` §2 notes the fleet's partial root.

**5g — P2, after the rest.** The site's fleet page builds its controls from
`limits`, so a scenario picker is a page change once `limits.scenarios` exists.

**Gates.** `go test ./tool/vif-allocator`; `./deploy/k3s/render-session.sh` diffed
against the allocator's rendered Job; `make image-check`.
**Manual.** A real allocate with `{"scenario":"td"}` reaches a playable TD session;
an unlisted scenario is refused with the list; `update-vif-wad.sh` during a live
match leaves that match alone and changes the next one; a deliberately broken
scenario is refused by the helper and, if forced past it, by the init container.

## 4. `doc/todo.md` items this work closes or touches

| Item | Disposition |
|---|---|
| *Add verified downloadable content bundles* (P1) | Phase 1 decided the format and Phase 3 delivers the scenario half: content-addressed, verified, in memory, not on the simulation channel. Rewrite the item at the end of Phase 3 to name only the corpus half, and drop the "decide the bundle format" prerequisite. |
| *Render the session log level from the template* (P3) | Phase 5c rewrites the same argument list in both the template and `manifest.go`. Add `${LOG_LEVEL}` there and delete the item. |
| *Say why a session reads as unavailable* (P2) | Adjacent — Phase 5d is in `allocator.go` — but it is a logging decision in `listSessions`, not a scenario change. Not folded in. |
| *Rename the participant identity type* (P3) | 230 sites. Folding it into a phase that also changes the capture-adjacent identity struct would make both unreviewable. Not folded in. |
| *Expand vif-allocator into the browser session adapter* (P0) | Phase 5d touches `sessionRequest`; keep the shape it establishes compatible with the session-adapter split that item anticipates. No work here. |

New items to add when the phases land: caching a received scenario to the user root
behind an explicit opt-in; an HTTP-backed scenario provider for browser builds,
which `multi-platform.md` already anticipates; and `script/test.sh`, whose name
now collides with the word for a playable scenario.

## 5. Settled

1. **Terminal across a restart** — the one-frame flash is accepted. A
   `TerminalService` that survives an App restart is a later, optional refinement.
2. **Node wad path** — `/var/db/vif/wad/`.
3. **Who owns the fleet's wad** — `update-vif-wad.sh`, shaped like the other
   updaters, from a repository checkout.
4. **`ProtocolVersion`** — not bumped. One deployed node, updated with the code,
   and dev clients in step with it. Documented at the constant instead.

## 6. Out of scope

Persisting a received scenario; a scenario browser or in-game list; editing a
scenario from inside the game; transferring the corpus, keymap or audio overrides;
changing the scenario of a running `-serve` pod; browser-build scenario transfer.

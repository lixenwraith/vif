# Map handling, over-the-air maps, and live map change

Working plan. Delete this file when the last phase lands; the durable parts move
into [HFSM and configuration](fsm-and-configuration.md),
[Multiplayer](multi-player.md), [External filesystem layout](filesystem-layout.md)
and [Deploying the session fleet](kube-docker-deploy.md) as each phase completes.

## 0. What "map" means here

A map is a **game bundle**: a `game.toml` entry plus the region files it includes,
rooted at one directory — `wad/game/main`, `wad/game/td`, `wad/game/blank`, or the
embedded `internal/asset/config`. It is selected by `-g <name|path>` or by `-d`.
Nothing else in the tree is part of it: the corpus, keymap and audio overrides are
separate resources with their own identity, and they stay that way.

Three objectives, in the order the work has to happen:

1. **`:n <map>`** changes the map during play, on a solo run and on a live host.
2. A guest **downloads the host's bundle in memory** when it does not have it,
   matched by content hash.
3. A **fleet pod serves an external `wad/`** that an operator can update live, and
   takes deployment-supplied environment.

## 1. What exists today

| Fact | Where |
|---|---|
| The FSM bundle is loaded once, during construction, from a path or the embedded FS. | `app.loadFSM`, `internal/app/app.go:549` |
| `Machine.LoadConfigFromMap` already clears the graph before building, so a second load into a live machine is mechanically possible. | `internal/fsm/loader.go:27` |
| A reset re-enters the loaded graph. It cannot load a different one. | `Scheduler.executeReset`, `internal/engine/scheduler.go:1039` |
| `fsm.<region>.*` status keys are registered per declared region at load time. | `Scheduler.bindFSMTelemetry`, `internal/engine/scheduler.go:404` |
| The status registry is **frozen** before the first tick and never reopens. The flight recorder's ring is laid out from the frozen index. | `Registry.Freeze`, `internal/status/registry.go:114` |
| The system set is **sealed** at the same moment. | `World.Seal`, `internal/engine/world.go:303` |
| The session identity a coordinator refuses a join on carries `ConfigID`, which is *the resolved filesystem path*. | `resolveConfigID`, `internal/app/app.go:441`; `network.PeerIdentity`, `internal/network/identity.go:50` |
| A joining App is constructed **after** the offer arrives, from `ConfigForJoin`. | `newJoiningApp`, `internal/app/session.go:251` |
| A whole second `App` is already built at runtime and re-used, for correction staging. | `App.newStagingApp`, `internal/app/snapshot_stage.go` |
| A chunked, bounded, digest-free bulk transfer already exists for captures. | `network.EncodeSnapshotChunks`, `internal/network/snapshot.go` |
| The fleet pod runs `-d`: embedded bundle, embedded corpus, nothing mounted. | `deploy/k3s/30-session.yaml`, `tool/vif-allocator/config.go` |
| The allocator already has a "what a caller may select" mechanism. | `sessionRequest`/`fleetLimits`/`resolve`, `tool/vif-allocator/allocator.go:38` |

Two consequences shape everything below.

**Region names are per-map.** `main` declares `main, quasar, storm, monitor, tower`;
`td` declares `td_main, td_counter, td_esc, td_storm, td_monitor, td_player`. A
different map needs status keys that do not exist, and the registry that would hold
them is frozen with the recorder bound to its index. **Loading a different bundle
into a running App is therefore not available**, and re-opening the registry would
have to invalidate the recorder ring, the cached metric pointers 212 call sites hold,
and the snapshot surface the session compares on. A map change is an App restart.

**Config identity is a path.** Two machines that both have `wad/game/td` installed
at different absolute paths cannot join each other today, because `ConfigID` differs.
That is the same defect objective 2 has to fix, so it is fixed once, first, and both
objectives build on it.

## 2. Design

**M-1 — A bundle is content-addressed.** `Bundle` is the ordered set of files
rooted at a `game.toml`, one canonical byte serialization, and the SHA-256 of that
serialization. The serialization is both the digest input and the wire body: one
format, not two.

**M-2 — Identity is `name` plus `digest`.** The anchor carries the *name*
(`main`, `td`, `embedded`) so a reproduction resolves it through the ordinary roots,
and the *digest* so the match is exact. The name is what a person reads; the digest
is what a join is refused on.

**M-3 — A map change is an App restart.** `:n <map>` validates the bundle, then
tears the App down and builds a new one from the same `Config` with a new
`Resources.Game`. `Run` owns the restart loop. Everything a restart already gets
right — a fresh registry, a fresh seal, a fresh journal run, a fresh D-14 latch — is
what makes this the cheap answer rather than the expensive one.

**M-4 — A guest that lacks the bundle asks for it during the handshake.** The
transfer sits between the offer and the reply, on the stream the join already owns,
bounded by the handshake deadline and by an explicit size cap. It is held in memory
and never written to disk.

**M-5 — A live map change is a session restart, not a new mechanism.** The host
broadcasts the new name and digest and restarts; each guest restarts and re-dials
the address it joined on. The new join is an ordinary mid-run join, so it already
carries the bundle download, the roster, and the world install. `:n` resets the run
anyway — there is no continuity to preserve that a redial loses.

**M-6 — The pod's map comes from a read-only node volume, not the image.** The
image stays wad-free and one layer. An operator replaces the node directory
atomically; running matches keep the bundle they started on, and the next pod gets
the new one.

## 3. Phases

Each phase builds, passes `gofmt -l` on its changed files, passes `go test` on the
packages it touches, and leaves the game playable. Manual checks are the acceptance
gate; agent tests are added only where a rule has no other witness.

### Phase 0 — `ClockScheduler` → `Scheduler` (done)

`internal/engine/clock_scheduler.go` is `scheduler.go`; the type, its constructor and
its receiver are renamed; the four comments where the substitution read badly were
rewritten. No conflict existed: nothing else in the tree declared `Scheduler`, and
`App.scheduler` was already the field name. Seven documents follow the same rename.
Every label below is the post-rename one.

### Phase 1 — Bundle identity

**New concept, so a new file:** `internal/resource/bundle.go`.

```go
type Bundle struct { Name string; Files []BundleFile; digest string }
func LoadBundle(fsys fs.FS, entry string) (Bundle, error)  // walks game.toml's includes
func (b Bundle) Marshal() []byte                           // canonical; the digest input
func UnmarshalBundle(name string, body []byte) (Bundle, error)
func (b Bundle) Digest() string                            // sha256, hex
func (b Bundle) FS() fs.FS                                 // what LoadConfigFromFS reads
```

Caps, refused at load and at unmarshal: 64 files, 256 KiB per file, 1 MiB total,
`.toml` only, no `..`, no absolute names. `main` is 44 KiB and `td` is 60 KiB today.

1. `resource.GameBundle(Options) (Bundle, error)` resolves exactly as `GameConfig`
   does — explicit path, installed name, embedded default — and reads the result into
   a `Bundle`. `GameConfig` stays for `-check`'s "which file" reporting.
2. `app.loadFSM` loads from `bundle.FS()` in every case. The FS/path fork disappears;
   `Scheduler.LoadFSMFromPath` loses its only caller and goes with it.
3. `event.JournalAnchor`: `ConfigID` becomes the bundle **name**, and `ConfigDigest`
   is added. `JournalSchema` 12 → 13, with the reason recorded beside the others.
   Journals written before this are refused rather than misread.
4. `network.PeerIdentity` gains `ConfigDigest`; `sessionFields` compares it. The
   refusal message then names a hash, which is the exact answer objective 2.a wants.
5. `ConfigFromAnchor` sets `Resources.Game` from the *name*, so a replay or a join
   resolves the bundle through its own roots. `VerifyAnchor` compares the digest of
   what actually loaded.
6. `resolveConfigID` is replaced by the bundle's own name and digest, read from the
   App rather than re-resolved from the flags — the anchor then describes what
   loaded, matching how `ContentID` already works.

**Gates.** `go build ./...`; `go test ./internal/resource ./internal/fsm
./internal/app ./internal/engine ./internal/network`.
**Manual.** `vif -g td`, `vif -g wad/game/td`, `vif -d`, `vif -config-dir wad`, each
reaching its first encounter; `vif -check -g td`; a recorded journal replays;
`test/scenario.sh` host/join pair on one box.
**One new test.** Two bundles differing in one byte produce different digests and the
same bundle read through a path and through a name produces the same one.
**Risk.** Behaviour is unchanged except for identity strings and the schema bump.
This is the phase that makes a cross-machine join with an installed `td` work at all.

### Phase 2 — `:n <map>` on a solo run

1. `App.Loop` returns `(*Config, error)`. A non-nil `Config` is the run to build
   next. `Run` becomes a loop: construct, run, close, repeat. It is the only caller.
2. `engine.SessionController` gains `ChangeMap(name string) error`. Like the rest of
   that interface it is the locked form: it resolves and loads the named bundle into a
   throwaway `fsm.Machine` and runs the same system-name and dependency checks
   `resource.checkSystems` runs, then latches the request. A bad name fails here, in
   front of the operator, with the game still running. The bounded file I/O under the
   world lock is the cost `:log on` already pays.
3. `App.restartConfig()` builds the next `Config`: this run's flags with
   `Resources.Game` replaced, `Resources.Embedded` cleared, and a fresh seed and
   session unless `-seed` pinned them. Map bounds are not carried — the new map
   derives or declares its own.
4. `mode`: `:n <map>` and `:n! <map>`. No argument keeps today's in-place reset
   exactly. An argument naming the running map is an in-place reset too, so `:n td`
   twice does not restart twice. Update `commandNames` and
   `internal/help/topics.go:156`, which `TestCommandsDocumented` cross-checks.
5. Refuse on a driven mode (`ModeReplay`, `ModeScript`, `ModeHeadless`) and, until
   Phase 4, in any live session.

**Gates.** `go test ./internal/app ./internal/mode ./internal/help`.
**Manual.** `:n td` from `main` reaches the TD setup chain; `:n main` returns;
`:n blank`; `:n nosuch` prints an error and the game continues; `:n` alone still
resets in place; `:n` with `-j` open starts a new journal run; `:q` still exits.
**Known cost.** The terminal is torn down and re-created with the App, so a restart
flashes the shell for one frame. See §5.

### Phase 3 — The bundle over the wire

1. `network`: `MsgConfigRequest = 0x16`, `MsgConfigBundle = 0x17`. Extend the
   numbering rather than reusing a retired code; `ProtocolVersion` 1 → 2.
   `MsgConfigBundle` re-uses `SnapshotChunkHeader` framing with the tick field zero,
   or a two-field header of its own if that reads better at the call site — decide in
   the diff, not here.
2. `Coordinator` gains `Bundle func(digest string) ([]byte, error)`. `HostAcceptor`
   accepts at most one `MsgConfigRequest` between the offer and the reply, answers it
   with chunks, and refuses a second. `network` never learns the format.
3. `PendingJoin.RequestConfig(digest string) ([]byte, error)`, bounded by the same
   handshake deadline with a per-chunk extension.
4. `resource.Options` gains `Bundle *Bundle`: an in-memory bundle that wins over
   discovery. `GameBundle` returns it directly. `newStagingApp` carries it, or the
   first correction after a downloaded-map join resolves against the wrong world.
5. `newJoiningApp`, after the offer and before `New`: if the local roots hold a
   bundle whose digest matches, use it; otherwise request, verify the digest,
   `UnmarshalBundle`, and put it in `Options.Bundle`. A digest that does not match
   what arrived is a refused join, not a retry.

**Bounds and posture.** The bundle is configuration, not code: every system, guard,
action and event name resolves against a registry that refuses an unknown one, and
map dimensions pass `ClampMapSize`. It is size-capped, `.toml`-only, held in memory
and never written to disk. The game port is unauthenticated by decision
(`doc/kubernetes-fleet.md` §4), so this adds one more bounded thing a stranger can
make a peer allocate, at 1 MiB, once per handshake, inside the existing admission
budget.

**Gates.** `go test ./internal/network ./internal/resource ./internal/app`.
**Manual.** Host `-g td`; guest with `-config-dir` pointing at an empty directory
joins, downloads and plays. Guest with its own edited `td` is refused on the digest
and says so. Guest with the identical `td` installed joins without a transfer — check
the log for the absence. Kill the host mid-transfer: the guest fails the join rather
than starting on a prefix.
**New tests.** A bundle that arrives with the wrong digest is refused. A second
`MsgConfigRequest` in one handshake is refused.

### Phase 4 — Live map change in a session

1. The coordinator's `:n <map>` validates as in Phase 2, then broadcasts a
   session-restart notice carrying the new name, digest and the address to redial,
   and latches its own restart. A guest's `:n <map>` stays refused, as `:n` is.
2. A guest receiving the notice latches a restart whose next `Config` is
   `-join <addr>` against the same address, with a bounded redial window
   (`NetworkJoinReadyTimeout` scale, a handful of attempts) covering the host's own
   rebuild. Failure to redial ends the guest's run with the reason on the way out,
   rather than leaving it in a session that no longer exists.
3. The host's restart re-binds the same address. Guests arrive through the ordinary
   mid-run gate: identity, bundle download if needed, capture install, roster slot.
   No new admission path.
4. A dedicated `-serve` host is out of scope: it has no operator, and its map is the
   pod's. Phase 5 is how that map changes.

**Gates.** `go test ./internal/app`.
**Manual.** Two local instances, `:n td` on the host: both reach TD, the guest's
cursor is on the session's map, a correction lands. Guest that cannot obtain the
bundle exits with the reason. Host `:n` with no argument still resets both in place
through the existing crossing. Three participants: all three follow.
**New test.** A notice for the digest already running is a no-op, not a restart.

### Phase 5 — The fleet serves an external wad

**5a — The node directory.** `/srv/vif-wad`, root-owned, `0755`/`0644`, holding the
categorized layout. `deploy/guest/update-vif-wad.sh` stages the repository's `wad/`
(or a release tarball) into `/srv/vif-wad.new`, validates it with the session image's
own `-check -config-dir /wad -g <name>` for every game it contains, then swaps by
rename. A running pod keeps the inode it mounted; the next pod gets the new tree.
Unlike `update-vif-image.sh` it does **not** require an empty fleet — updating
without ending a match is the point. It refuses a tree that fails validation.

**5b — The volume.** `deploy/k3s/07-wad-volume.yaml`: a no-provisioner
`StorageClass vif-node-wad`, a node-affine `local` PV `vif-fleet-wad` at
`/srv/vif-wad` (8 Mi, Retain, `WaitForFirstConsumer`), and one PVC. `10-quota.yaml`
goes to `persistentvolumeclaims: "2"` and `requests.storage: 264Mi`.
`06-log-volume-check.yaml` gains a sibling, or grows a second mount, so a fresh node
binds both claims before a real player does.

**5c — The pod.** `30-session.yaml` and `tool/vif-allocator/manifest.go` mount the
claim read-only with `subPath: game` at `/wad/game` and `subPath: image` at
`/wad/image`, and replace `-d` with `-config-dir /wad -g ${GAME}`. `content`,
`input` and `audio` are deliberately **not** mounted, so the corpus and keymap stay
embedded and the session's `ContentID` does not move — a native guest running `-d`
must still be able to join. The init container runs `-check -config-dir /wad -g
${GAME}`, which finally proves the configuration the session will actually serve
rather than the embedded one; the Dockerfile comment that names this gap is updated.
`.dockerignore` gains `wad/` so the context stops carrying what the image never uses.
Verify on the node that a `scratch` image with `readOnlyRootFilesystem: true` gets
its mount points created — it is the one assumption here that the cluster, not the
code, has to honour.

**5d — The allocator.** `-games` is the allowlist a caller may pick from and `-game`
the default; `sessionRequest.Game` and `fleetLimits.Games` follow the shape
`Players`/`LogLevel` already have, and `resolve` refuses an unlisted name rather than
clamping it. `VIF_ALLOCATOR_GAMES` and `VIF_ALLOCATOR_GAME` join
`vif-allocator.env.example`. The allocator does not read the wad; the allowlist is
the operator's statement about what is installed there, and a mismatch fails the init
container, which is where a configuration error belongs.

**5e — Environment.** One optional `envFrom` a namespace `ConfigMap
vif-session-env`, `optional: true` so an absent map is not a failed pod. Deployment
state, never request state: a caller cannot set an environment variable, only choose
from `limits`. `GOMEMLIMIT` stays where it is, as a property of the manifest's own
resource envelope.

**5f — Documentation.** `kube-docker-deploy.md` gains the wad volume beside §7's log
tmpfs and §9's fleet objects, and a wad step in §15's operating notes;
`kubernetes-fleet.md` §1, §2 and §6 record the new mount and its ceiling;
`deploy/README.md`, `deploy/guest/README.md` and `deploy/runbook.md` gain the new
file and the new helper; `filesystem-layout.md` §2 notes the fleet's partial root.

**5g — P2, after the rest.** The site's fleet page builds its controls from
`limits`, so a map picker is a page change once `limits.games` exists.

**Gates.** `go test ./tool/vif-allocator`; `./deploy/k3s/render-session.sh` diffed
against the allocator's rendered Job; `make image-check`.
**Manual.** A real allocate with `{"game":"td"}` reaches a playable TD session; an
unlisted game is refused with the list; `update-vif-wad.sh` during a live match
leaves that match alone and changes the next one; a deliberately broken bundle is
refused by the helper and, if forced past it, by the init container.

## 4. `doc/todo.md` items this work closes or touches

| Item | Disposition |
|---|---|
| *Add verified downloadable content bundles* (P1) | Phases 1 and 3 deliver the game half: content-addressed, verified, in memory, not on the simulation channel. Rewrite the item at the end of Phase 3 to name only the corpus half, and drop the "decide the bundle format" prerequisite, which Phase 1 decides. |
| *Render the session log level from the template* (P3) | Phase 5c rewrites the same argument list in both the template and `manifest.go`. Add `${LOG_LEVEL}` there and delete the item. |
| *Say why a session reads as unavailable* (P2) | Adjacent — Phase 5d is in `allocator.go` — but it is a logging decision in `listSessions`, not a map change. Not folded in. |
| *Rename the participant identity type* (P3) | 230 sites. Folding it into a phase that also changes the capture-adjacent identity struct would make both unreviewable. Not folded in. |
| *Expand vif-allocator into the browser session adapter* (P0) | Phase 5d touches `sessionRequest`; keep the shape it establishes compatible with the session-adapter split that item anticipates. No work here. |

New items to add when the phases land: caching a downloaded bundle to the user root
behind an explicit opt-in, and an HTTP-backed bundle provider for browser builds,
which `multi-platform.md` already anticipates.

## 5. Open questions

1. **Terminal across a restart.** Phase 2 as written closes and re-opens the
   terminal, which flashes the shell for one frame on every `:n <map>`. Keeping one
   terminal for the process means making `TerminalService.Init`/`Start`/`Stop`
   re-entrant and handing the instance to the next App's hub. Recommended, as a
   separate step after Phase 2 proves the restart, because a visible artifact on a
   deliberate operator action is the kind of thing that gets lived with forever.
2. **Node wad path.** `/srv/vif-wad` is proposed to sit beside `/var/log/vif-fleet`
   without implying it is log state. Confirm the path and whether the Arch guest has a
   convention already.
3. **Who owns the fleet's wad.** `update-vif-wad.sh` from a repository checkout
   matches how the image is updated today. A release tarball would decouple the two.
   Confirm before writing the helper.
4. **`ProtocolVersion` bump.** Phase 3 bumps it, so every participant must be on the
   same build for the duration. Confirm there is no deployed fleet that has to
   interoperate across the change.

## 6. Out of scope

Persisting a downloaded bundle; a map browser or in-game map list; editing a map from
inside the game; transferring the corpus, keymap or audio overrides; changing the map
of a running `-serve` pod; browser-build map download.

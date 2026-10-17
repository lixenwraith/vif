# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release or leads the feature development push, P1 is
wanted next, P2 is convenient follow-up, and P3 is an idea.

## Suspected bugs to review

The documentation sweep traced each of these to the code without fixing it; those
marked reproduced also failed in a scratch test. Review an item before other work,
then fix it, refile it at its real priority under its area, or delete it. Two more
suspected bugs already have items at their own priority:
[Drop the kinetic window's dead joiner path](#drop-the-kinetic-windows-dead-joiner-path)
and [Drop the cursors a linked instance cannot reach](#drop-the-cursors-a-linked-instance-cannot-reach).

### Refuse a parent cycle at load

- Priority: P0, to review
- Affected files: `internal/fsm/builder.go`, `internal/fsm/loader.go`

`CompilePaths` walks each state's parents without a bound (`builder.go:39-51`), and
the loader checks only that a parent name exists (`loader.go:113`). With
`A.parent = "B"` and `B.parent = "A"`, or a self parent, the walk never ends and its
path grows until memory runs out (reproduced), so `-check`, startup and `:bot add`
(under the world lock) hang. Stop the walk once it passes the node count and return
an error naming the state.

### Fail a document without `[states]` instead of panicking

- Priority: P0, to review
- Affected files: `internal/fsm/loader.go`

`LoadScenarioFromMap` writes `config.States["Root"]` (`loader.go:65-67`), and
`toml.Decode` leaves that map nil when the document has no `[states]`. The file
loader always adds one (`file_loader.go:103-105`), but `Machine.LoadScenario` and
`bot.ParseGraph` (through `Graph.machine`, `internal/bot/bot.go:108`) do not, so a
bot graph with only `[regions]` panics `-check`, `-bot` and `:bot add` (reproduced).
Make `config.States` when it is nil.

### Let an on_exit terminate its own region, or forbid it

- Priority: P0, to review
- Affected files: `internal/fsm/machine.go`, `doc/fsm-reference.md`

`TerminateRegion` runs the region's on_exit actions (`machine.go:623-628`) before
deleting it (`:630`), so an on_exit that terminates its own region reruns itself
until the stack overflows, whether the exit comes from a transition,
`TerminateRegion` or `Reset` (`:471-484`; all three reproduced). The Notes of the
[FSM reference](fsm-reference.md) call it supported; shipped scenarios terminate only
from on_enter. Options: honour it (a nested call only marks the exiting region, and
the outer exit deletes it and skips any enter phase); or forbid it
(`TerminateRegion` returns an error for an exiting region, and the reference says so).

### Decide which control keys work in an overlay

- Priority: P0, to review
- Affected files: `internal/input/machine.go`, `internal/asset/input/keymap.toml`,
  `internal/help/topics.go`, `doc/input-and-modes.md`

`processOverlay` (`internal/input/machine.go:708-723`) reads only the overlay tables:
it lacks the Ctrl-Space stop the other modes handle (`:220`, `:627`, `:666`, `:689`),
and `[overlay_keys]` (`keymap.toml:123-134`) binds no `ctrl_q`, `ctrl_c` or
`ctrl_s`. In help, telemetry or the configuration menu these keys do nothing, yet
while the telemetry filter is typed, which edits in Search mode
(`internal/mode/overlay.go:98-105`), they quit, mute and stop, and the help says
Ctrl+Space works "from any mode" (`topics.go:125`). Options: bind the three keys,
add the Ctrl-Space branch and update [Input and modes](input-and-modes.md) §4 and
§6, which say overlay navigation ignores these keys; or keep overlays navigation-only
and correct the help line.

### Decide whether loads refuse unknown keys and `op` values

- Priority: P0, to review
- Affected files: `internal/fsm/loader.go`, `internal/fsm/std/guard.go`,
  `internal/fsm/std/payload.go`, `internal/resource/check.go`, `doc/fsm-reference.md`

The scenario and bot-graph decode (`loader.go:33`) drops keys outside
`ScenarioDoc`'s tags, so a misspelt `on_entr` loads as absent and `-check` is silent.
`CompareInt` reads an omitted or unknown `op` as `eq` (`guard.go:232-250`), unchecked
by the guard factories, while `:d break stat` spells `ne`, `ge`, `le`
(`internal/mode/commands.go:843-850`), so `op = "ge"` tests equality (reproduced).
Options: keep this and have `-check` report both; or refuse at load, with the strict
`Op` of [Behavior machines](machine-design.md) §7. Shipped scenarios and the default
bot graph use only known keys and ops.

### Decide what the cornering threshold gates

- Priority: P0, to review
- Affected files: `internal/parameter/navigation.go`, `pkg/vmath/physics/steering.go`

`NavCorneringThreshold` is 3.0 (`internal/parameter/navigation.go:21`), but
`TurnSeverity` compares it with a cosine (`pkg/vmath/physics/steering.go:44-48`), so
it never gates: above 1 cell/s every drain, swarm, quasar, eye and loot that steers
(`drain.go:1061`, `swarm.go:699`, `quasar.go:475`, `eye.go:419`, `loot.go:235`, all in
`internal/system`) brakes at `NavCorneringBrake` × (3 − cos θ), a drain at twice that,
even heading straight at its target. Options: keep that motion and name it, a constant
drag plus a turn term; or make the threshold a real cosine and retune. At 0.7
`internal/system` passes, but eleven `internal/app` tests whose scripted runs depend
on today's motion fail.

### Put the route bandit's floor and scout rate in parameter

- Priority: P0, to review
- Affected files: `internal/parameter/adaptation.go`, `internal/system/adaptation.go`

`parameter.RouteMinWeight` (0.05, `internal/parameter/adaptation.go:16`) is
referenced nowhere; `applyEXP3` floors at an inline `minWeight := 0.005`
(`internal/system/adaptation.go:487`), the 0.5% that
[AI, physics and evolution](ai-physics-and-evolution.md) §6 documents. The 10% scout
rate (`:537`) and the corridor recovery radius 5 (`:350`) are inline too, while the
genetic scout rate is `parameter.GAScoutRate`. Set `RouteMinWeight` to 0.005 and use
it, and add the other two beside it; adopting 0.05 instead would retune route
exploration.

### Move the inline simulation and render tunables into parameter

- Priority: P0, to review
- Affected files: `internal/engine/position.go`, `internal/profile/homing.go`,
  `internal/system/genetic.go`, `internal/component/kraken.go`,
  `internal/system/weapon.go`, `internal/render/renderer/flow_debug.go`,
  `internal/parameter`, `internal/parameter/visual`, `pkg/vmath`

[Development](development.md) §11 puts tunables in `internal/parameter` or
`internal/parameter/visual`, never inline. Still inline: the spiral search's default
radius 20 (`internal/engine/position.go:522`, eight callers); the quasar, snake and
missile arrival and dead-zone values (`internal/profile/homing.go:27-29,37-39,58-60`;
loot's are in `parameter`); the type-fitness EMA rate
(`internal/system/genetic.go:412`); the tentacle factors that shape the Kraken's
hitbox (`internal/component/kraken.go:41-66`); the gun's lane table
(`internal/system/weapon.go:1346`); and the flow-field debug colours
(`internal/render/renderer/flow_debug.go:83-205`). The 7/10 at `position.go:1085`
approximates 1/√2, a `pkg/vmath` constant if named. Each move keeps its value.

### Move the runtime cadences and bounds into parameter

- Priority: P0, to review
- Affected files: `internal/engine/sync_std.go`, `internal/app/play.go`,
  `internal/app/serve.go`, `internal/event/journal.go`, `internal/engine/resource.go`,
  `internal/resource/scenario.go`, `internal/resource/scenario_test.go`,
  `internal/system/navigation.go`, `internal/engine/scheduler.go`,
  `internal/app/play_test.go`, `internal/parameter`, `doc/logging-and-diagnostics.md`,
  `doc/term-index.md`

Wall-clock, cadence and capacity tunables sit beside their code: `LockHoldWarn`
(`internal/engine/sync_std.go:15-24`), the replay pan step
(`internal/app/play.go:22`), the server's report and lifecycle intervals
(`internal/app/serve.go:16,23`), the journal anchor and digest cadences
(`internal/event/journal.go:166,173`), the per-group target cap
(`internal/engine/resource.go:670`) and the scenario size bounds
(`internal/resource/scenario.go:34-36`). Their peers (`ProfWindow`, the camera
margins, `NetworkDigestTicks`, `ContentMaxFiles`) live in `internal/parameter`.
Moving them keeps every value; decide first whether a format bound such as the
scenario limits counts as a tunable.

### Name the FNV-1a constants in vmath and settle the golden seeds

- Priority: P0, to review
- Affected files: `pkg/vmath/rand.go`, `internal/manifest/fingerprint.go`,
  `internal/snapshot/digest.go`, `pkg/audio/sound_render.go`,
  `pkg/audio/sequencer.go`, `pkg/maze/generator.go`, `pkg/genetic/engine.go`,
  `doc/package-map.md`

The FNV-1a 64 offset and prime are written out in
`internal/manifest/fingerprint.go:29-30`, `internal/snapshot/digest.go:16-17`,
`pkg/audio/sound_render.go:439` and `pkg/vmath/rand.go:107-110`, and `pkg/vmath`
names neither: export them there and use them in all four. `0x9E3779B97F4A7C15` is
restated beside `vmath.Golden64` as seeds in `pkg/maze/generator.go:9` and
`pkg/genetic/engine.go:11`, and as a stream in `pkg/audio/sequencer.go:74`.
`pkg/audio` can import `vmath`; `pkg/maze` and core `pkg/genetic` are
standard-library only ([Package map](package-map.md) §5). Options: those two keep a
local seed as a stated exception, an arbitrary seed not being the SplitMix
increment; or they import `pkg/vmath` and that coupling rule changes.

### Decide whether a gateway's population ID selects anything

- Priority: P0, to review
- Affected files: `internal/engine/resource.go`, `internal/system/gateway.go`,
  `internal/component/gateway.go`, `internal/event/payload.go`,
  `internal/snapshot/capture.go`, `internal/engine/snapshot_world_gen.go`,
  `pkg/genetic/registry`

A gateway's `population_id` (`internal/event/payload.go:1318`) is stored in
`GatewayComponent.PopulationID` (`internal/component/gateway.go:19`), carried in the
capture and passed to `GeneticResource.Sample` and `SampleScout`
(`internal/system/gateway.go:264-266`), which ignore it
(`internal/engine/resource.go:867`); the registry keeps one population per species
and no scenario sets the key. Options: drop the parameter, field and key (a capture
schema bump); or give the registry per-island populations for it to select.

### Give walls a write mask so pause dims them

- Priority: P0, to review
- Affected files: `internal/render/renderer/wall.go`,
  `internal/parameter/visual/mask.go`, `doc/rendering.md`, `doc/term-index.md`

`WallRenderer.Render` never calls `SetWriteMask`
(`internal/render/renderer/wall.go:35-61`), so walls take the previous layer's mask:
`MaskPing` when the crosshair or grid drew this frame (`ping.go:46`), else
`MaskNone` from `Clear` (`internal/render/buffer.go:97`). Pause dim (`DimMask`,
`internal/parameter/visual/post_process.go:24`) therefore skips walls unless ping is
showing (reproduced). Add a wall mask bit (`0x80` is free) and set it in `Render`, or
set `MaskNone` explicitly if walls are meant to stay bright.

### Read `_duration` keys as nanoseconds in the overlay

- Priority: P0, to review
- Affected files: `internal/status/format.go`

`status.IntUnit` matches `.duration` as a suffix (`internal/status/format.go:17`,
commented "also matches .max_duration"), but `fsm.max_duration` and
`fsm.<region>.max_duration` end in `_duration`, so the telemetry overlay and pinned
cards show their nanoseconds as plain counts, against the units table in
[Logging and diagnostics](logging-and-diagnostics.md) §6. The status bar converts
the summary itself, and vif-log's `DurationUnit` already matches the `_duration`
segment. Match the trailing `.` or `_` segment as vif-log does, or add `_duration`
to `intUnits`; either way correct the comment at `format.go:17`.

### Share the scope parser with the novlog build

- Priority: P0, to review
- Affected files: `internal/vlog/stub.go`, `internal/vlog/scope.go`

In a `wasm` or `novlog` build `ParseScopes` returns `cur, nil` for any spec
(`internal/vlog/stub.go:110-111`), under a comment saying CLI parsing behaves
identically, so a malformed `-ls` or `:log scope` passes there and fails in a
logging build (`internal/vlog/scope.go:130-163`). `Scope`, its constants and the
parser need only `fmt` and `strings`: move them to an untagged file that both builds
share, or keep the stub and correct its comment.

### Delete the dead diagnostics entry points

- Priority: P0, to review
- Affected files: `internal/status/recorder.go`, `internal/vlog/stream.go`,
  `doc/term-index.md`

The package-level `status.TriggerFSM` (`internal/status/recorder.go:48-56`) has no
caller (the scheduler uses the `Registry` method,
`internal/engine/scheduler.go:468`), and neither have `vlog.OpenStream` and
`vlog.Stream` (`internal/vlog/stream.go`). Delete them, and `vlog.Stream` from the
term index's **stream** entry. The package-level `status.RecorderActive`
(`recorder.go:68`) serves only `internal/app/play_test.go:451`, which checks the
crash path's process-wide recorder; it stays while that assertion does.

### Drop `swarm.player_kills`

- Priority: P0, to review
- Affected files: `internal/system/swarm.go`, `doc/telemetry-reference.md`

`SwarmSystem` adds to `swarm.player_kills` (`internal/system/swarm.go:107-110`) on
the same `ResolveCursor` test with which `lifecycle.RecordKill`, one line earlier,
adds to `swarm.killed_by_player` (`internal/system/telemetry.go:102-108`), so the two
always agree, and the duplicate is one of the swarm card's 12 entries, of the 15 a
card may hold (`parameter.OverlayCardMaxEntries`). Remove the counter, its field,
its key note in the [Telemetry reference](telemetry-reference.md) and its key in
that page's Swarm row.

### Drop navigation's two unhandled cursor subscriptions

- Priority: P0, to review
- Affected files: `internal/system/navigation.go`

`NavigationSystem.EventTypes` subscribes `EventCursorMoved` and
`EventCursorDespawned` (`internal/system/navigation.go:133-134`), but `HandleEvent`
(`:145-214`) has no case for either, and nothing is missed: `Update` rebuilds group
0 from the roster every tick (`resolveGroupTargets`, `:323`). Every cursor move is
still dispatched to a handler that drops it. Remove the two entries.

### Check float telemetry in the reset test

- Priority: P0, to review
- Affected files: `internal/app/telemetry_test.go`

`persistentTelemetryKey` lists `glyph.density` and `glyph.rate_mult` under `string`
(`internal/app/telemetry_test.go:109`), but they are floats
(`internal/system/glyph.go:99-100`), and `TestTelemetrySessionCountersReset` never
ranges floats, so the entries match nothing and no float is checked across a reset.
A float pass against the post-reset baseline passes today with the `proc.` prefix
exemption alone (tried in a scratch test), since the glyph pair resets. Add the float
pass and delete the two entries.

### Confine a scenario's wall image path to the roots

- Priority: P0, to review
- Affected files: `internal/service/adapter_file.go`, `pkg/ascimage/dual.go`

`FileService.Open` opens a wall image path as given, cwd-relative or absolute,
before its escape check and the roots (`internal/service/adapter_file.go:51-58`),
and refuses only directories (`:86`). So `./test.vifimg` shadows the roots' copy,
against [Packaging](packaging.md) §1, and a scenario a coordinator serves a joiner
(`internal/app/session.go:450`) can open `../` paths or `/dev/zero`, which
`pkg/ascimage/dual.go:421` reads without bound (all three reproduced). Resolve only
through the roots and require a regular file; decide whether absolute paths stay,
or stay only for a scenario this run resolved itself.

### Read vif.toml in soundlab

- Priority: P0, to review
- Affected files: `cmd/soundlab/main.go`, `cmd/soundlab/session.go`

soundlab resolves its audio overrides from its flags alone
(`cmd/soundlab/main.go:38`, `cmd/soundlab/session.go:45-61`) and never reads
`vif.toml`, while vif fills `-config-dir` from `[paths] root`
(`cmd/vif/main.go:112-116,387`). With `root` set, vif reads an override under it
first while soundlab opens and saves the user root's copy, so a save is not what the
next run loads, though `cmd/soundlab/README.md:33` says it is; `[audio] buffer_ms` is
ignored too. Load the settings in soundlab and fill both as vif does.

### Decide how sound includes reach the game

- Priority: P0, to review
- Affected files: `cmd/soundlab/session.go`, `internal/service/adapter_audio.go`,
  `internal/resource/check_audio.go`, `pkg/audio/sound_load.go`

soundlab loads a sounds document through `LoadSoundsFS`, keeps its `include` list,
adopts the user override as clean and writes the list back on save
(`cmd/soundlab/session.go:128,160-175,254`). The game hands that file to
`LoadSoundsTOML` (`internal/service/adapter_audio.go:69`, `pkg/audio/engine.go:141`),
which refuses any include (`pkg/audio/sound_load.go:23-25`), so play ignores the whole
override and `-check` fails it (`internal/resource/check_audio.go:37`); soundlab
prints that refusal only at start. Options: the audio service and `-check` resolve
includes inside the file's directory as soundlab does; or soundlab flattens or
refuses includes in a file the game reads.

### Build the Makefile's release binaries without CGO

- Priority: P0, to review
- Affected files: `Makefile`, `doc/packaging.md`

`release`, `headless` and `nolog` run `go build` without `CGO_ENABLED=0`
(`Makefile:131-141`), so on a host with a C compiler `net` links cgo: a scratch build
with those flags is dynamically linked against glibc. [Packaging](packaging.md) §1
promises no CGO, and §4 `depends=()` assumes a static binary; the release workflow,
the Dockerfile and `make windows` already set the variable. Set `CGO_ENABLED=0` on
the three recipes and in §4's `go build` line.

### Keep code generation out of the build targets

- Priority: P0, to review
- Affected files: `Makefile`, `.github/workflows/release.yml`, `doc/development.md`

The binary targets `release`, `headless`, `nolog`, `wasm` and `windows` depend on
`generate` (`Makefile:121-151`), so a packager's `make release` reruns
`gen-manifest`, which rewrites ten committed files
(`internal/gen-manifest/main.go:169-182`), against [Packaging](packaging.md) §1: no
`go generate` at package time. `.github/workflows/release.yml:45` also names
`./internal/event`, which has no directive. Keep `generate` on `dev`, `test` and
`verify` only, and drop `./internal/event` from the workflow.

### Leave no empty stderr capture where redirection fails

- Priority: P0, to review
- Affected files: `internal/core/dev.go`

`CaptureStderr` creates `vif-stderr-<stamp>.log` (`internal/core/dev.go:59-69`)
before `dupFD` (`:71`) reports that the build cannot redirect, as every non-unix
build does (`internal/core/redirect_other.go:9-10`); the error path closes both
handles and keeps the file. Each `vif -dev` on Windows leaves an empty capture in the
log directory beside "runtime capture disabled" (`cmd/vif/main.go:259-263`). Call
`dupFD` before creating the file, or remove the file on that path.

### Pass the image scenario's name and engine to make

- Priority: P0, to review
- Affected files: `script/test.sh`, `script/README.md`

The `image` scenario passes only `IMAGE_TAG="${IMAGE#*:}"`
(`script/test.sh:865-866`). An `IMAGE` set in the environment reaches make whole, so
`IMAGE=reg/vif:x` builds `reg/vif:x:x` (`make -n image` shows it), and
`reg:5000/vif:x` cuts at the wrong colon. Its check also passes a podman-only machine
(`:863-864`), where `make image` runs `docker` (`Makefile:10`) unless the caller sets
`CONTAINER_ENGINE=podman`, as `script/README.md:95` says. Pass `IMAGE` and `IMAGE_TAG`
split at the last colon, and `CONTAINER_ENGINE`, when unset, set to the engine found.

### Run maxmap under `all`, or list it apart

- Priority: P0, to review
- Affected files: `script/test.sh`, `script/README.md`

`./script/test.sh list` files `maxmap` under "Automated (assert, and used by `all`)"
and says `all` runs every automated scenario (`script/test.sh:155-170`), but the
`all` loop leaves it out (`:871`), as `script/README.md:7,65` state. Options: add it
to the loop, which holds a 500x250 `td` session for over 40 s, since its guest's
6000-tick script outlasts `timeout 40` (`script/test.sh:672-674`); or list it outside
the `all` set as `image` is.

### Drop the fleet grants nothing uses

- Priority: P0, to review
- Affected files: `deploy/k3s/40-allocator-rbac.yaml`,
  `deploy/k3s/20-networkpolicy.yaml`, `doc/kube-docker-deploy.md`

The allocator Role grants `get` and `watch` on jobs, services, pods and
endpointslices, and reads events (`deploy/k3s/40-allocator-rbac.yaml:24-52`), but the
allocator only lists, creates and deletes (`tool/vif-allocator/kube.go:23-32`),
against the header's "exactly the verbs it needs". `allow-operator-ports` admits a
`logstream` port (`deploy/k3s/20-networkpolicy.yaml:62-63`) no container declares
(`tool/vif-allocator/manifest.go:135-136`). Drop the unused verbs (the `can-i` check
in [Fleet deploy](kube-docker-deploy.md) §16 then asks `list pods`); decide whether
the allocator should log why a session ended from events or that rule goes, and
whether `logstream` goes or a pod declares it.

### Let update.sh check the objects on a fresh cluster

- Priority: P0, to review
- Affected files: `deploy/update.sh`

`check_objects` runs `kubectl diff` on each object file before anything is applied
and dies on an exit above 1 (`deploy/update.sh:66-76`). On a cluster without the
`vif` namespace, where [Fleet deploy](kube-docker-deploy.md) §9 runs
`./deploy/update.sh objects wad`, the server dry run of every namespaced object
should be refused as namespace not found, so the first install stops before
`apply_objects` (inferred, not tried on a cluster). When `kubectl get namespace vif`
fails, report the objects pending, undiffed.

### Decide fleet log retention against the Job deadline

- Priority: P0, to review
- Affected files: `deploy/guest/vif-fleet-log-cleanup.py`, `doc/kubernetes-fleet.md`,
  `doc/kube-docker-deploy.md`, `deploy/guest/var-log-vif\x2dfleet.mount`,
  `deploy/k3s/05-log-volume.yaml`

The cleanup keeps every session file touched in the last five hours
(`deploy/guest/vif-fleet-log-cleanup.py:14`), while a session Job ends within one
(`deploy/k3s/30-session.yaml:25`), so the 256 MiB tmpfs mostly holds ended sessions.
Ten sessions at the measured 3 KiB/s fill it in about 2.4 h, which
[Kubernetes fleet](kubernetes-fleet.md) §6 says the cleanup carries, yet nothing is
stale by then and the lines lost are live sessions'. Options: a stale age just over
the deadline; five hours under a byte budget that removes ended sessions oldest
first; or a tmpfs sized for five hours of a full fleet.

### Ship the third-party licence notices

- Priority: P0, to review
- Affected files: `web/vendor/xterm-5.5`, `wad/content`, `Makefile`,
  `.github/workflows/release.yml`

None of the six files in `web/vendor/xterm-5.5/` carries xterm.js's copyright or MIT
notice, which MIT requires in every copy, and the release's web archive copies `web/`
with no licence file (`.github/workflows/release.yml:74-77`). The corpus files keep
the Go Authors' header, which points to a LICENSE no install or archive carries
(`Makefile:217,237` ship only vif's). Add xterm's LICENSE beside it and Go's in
`wad/content/` (the loader reads only `.txt` and `.toml`,
`internal/content/load.go:32-35`), and ship both, with vif's, in every archive.

## Feature development push

Bots come before the Android host. They need no presentation refactor, build on
the scripted and headless paths that exist, give the session stack and the fleet
continuous real play, and make the site playable alone. A bot is also the first
non-terminal source of semantic input, the contract the Android extraction has to
define first; doing Android first would define it without a second consumer.

### Add bots that play as participants

- Priority: P1, leading the push
- Affected files: `internal/bot`, `internal/app`, `internal/network`,
  `tool/vif-allocator`
- Guide: [Bots](bots.md)

Bots play beside a person, in place of one, or alone on a dedicated host, and a
guest's bots and the people at its `-relay` address join and play through its own
link. What remains is under [Bots](#bots) and
[Reach a relay without an address in hand](#reach-a-relay-without-an-address-in-hand).

### Extract the renderer-neutral Android host model

- Priority: P1, after bots
- Affected files: terminal-shaped input/cell/color values, `internal/app`, host
  entry points
- Guide: fleet item H4, [Engine decoupling](engine-decoupling.md) §7 for the
  decisions taken and the order

Target a minimally polished Android build within one month of development time.
`vif_headless` already leaves out the renderer and audio packages, the frame
leaves through `render.Surface`, and the terminal module's value types are the
renderer-neutral vocabulary (D1). Done: `app.Presentation` and `Config.Host`
(the terminal as the default presentation, the host's speakers as the audio
sink), the font atlas under `internal/asset/font/` with `pkg/glyph` and its
software rasterizer as the reference drawer, and the cell aspect as one parameter
(`vmath.Aspect`, carried by the anchor). Next, in order: the Android host on
`SurfaceView` behind a Java-side drawer interface a GLES drawer can replace, with
its asset provider; then touch mapped
onto the semantic input bots already drive, and a radial menu summoned by
holding a tap in place of the Ctrl-G menu, whose gameplay implication is
unresolved.

### Take the position store out of the engine

- Priority: P3
- Affected files: `internal/engine/position.go`, `spatial_grid.go`

`engine.Position` answers 2D cell queries over integer cells and couples to the
world only for the component mask. As a `pkg/` package generic over its value
type and taking its blocking predicate from the caller, it could add a third axis
without the engine knowing. A design direction for the host model to stay
compatible with, not a change on its own.

## Bots

### Open fleet sessions to bots

- Priority: P2
- Affected files: the Hugo site's `vif-lobby.js`, `internal/network/endpoint.go`,
  `internal/mode/config_form.go`, `tool/vif-allocator`, `deploy/guest/vif-allocator.env`,
  `deploy/k3s/10-quota.yaml`
- Guide: [Session fleet](kubernetes-fleet.md) §6

The allocator takes `bots`, but `bots_max` stays 0 until these hold:
- The site's `vif-lobby.js` sends `bots` and reads `bots_max`; the native client
  (`network.RequestSession`, the menu's request form) sends `bots`.
- The quota's 1000m of CPU fills at three sessions of three bots: raise it, or
  accept fewer sessions. A create the quota cannot hold waits 75 s for a pod that
  never schedules and answers 504 `session_not_ready`; sum the live sessions'
  requests against the quota and refuse it as `fleet_full` instead.
- The CPU limit stays 500m while each bot adds 64m of request, and six bots with
  one person use about 0.5 core: grow the limit with bots (the `LimitRange` allows
  1 CPU), or bound `bots_max` by CPU as well as memory.
- td: refuse bots on ceiling-sized maps by map cells, or leave td off the offered
  scenarios, until the map-limit trim below lands.
- K=3 measured on the node with `./script/test.sh load` or the deploy guide,
  replacing the sandbox figures (2 sessions of 2 bots for 30 s: about 190–200 ms
  of CPU a second and 90–95 MiB peak per host, no evictions).
- Digest mismatches watched as networked bot load grows; that run counted 0–5 per
  host.

### Beat the default graph

- Priority: P2
- Affected files: `internal/bot`, `internal/asset/bot/default.toml`,
  `internal/system/nugget.go`, `script/test.sh`
- Guide: [Bots](bots.md#fitness-and-comparing-graphs)

Exit: a graph beats `default` under `./script/test.sh botfitness` over seeds 1–8
on the embedded scenario, td and kraken. Baseline, `default` over 4 seeds of 60 s:
embedded 3493, td 1479, kraken −4800 (kraken has no text, gold or nuggets, so
`default` is weighed only on its defeats there). Candidates:
- a `Best` utility guard, added once a graph uses it: a weight per target class,
  utility `weight*1024/(1+isqrt(dist²))` in integers, ties to the lower class;
- threat evasion in `default` (`HasTarget threat`, `PointAt away`);
- kill engagement on kraken;
- a motor layer: a goal the view does not show reached by clicks along a
  `pkg/navigation` route rather than the clamp.

Decide whether an improved graph replaces `default`, keeping a pinned copy as the
baseline, or ships beside it. A collected nugget weighs nothing: nothing pushes
`EventNuggetCollected`; push it in `NuggetSystem.collectNugget` and drop the
duplicate `statCollected` increment there. Threats are unweighted; weighting them
by `profile.Contact` needs a `CombatEntityType`-to-`SpeciesType` table.

### Let a bot graph use operators and marks

- Priority: P3
- Affected files: `internal/input/actionnames.go`, `internal/bot/mind.go`

`input.IntentFor` refuses the actions that only open a key sequence
(`operator_delete`, `prefix_g`, `prefix_macro_play`, `marker_glyph_*`), so a graph
cannot delete a word or jump to a coloured glyph. Options: a `motion` operand
on `Intent` that applies an operator to a named motion, which keeps graphs
semantic; or a `Keys` action parsed through the keymap, which reaches everything a
keymap binds but depends on the bindings. A right press carries no position, so
special fire stays `Intent fire_special` where the cursor stands.

### Limit the commands a bot graph may issue

- Priority: P3
- Affected files: `internal/bot/mind.go`

`Command` accepts any ex-command, `:bot add`, `:host`, `:q` and `:d cpu` included,
so a graph from elsewhere can seat bots, open a listener or profile its holder's
process. Decide an allowlist (the commands play needs, such as `:n`) or a refusal
set (session, bot, journal and diagnostic commands), checked at load as the
world-writing actions are.

### Expose a bot difficulty knob

- Priority: P3
- Affected files: `internal/app/seat.go`, `cmd/vif`, `internal/mode/config_form.go`,
  `tool/vif-allocator`

`actions_per_second` is the only knob, and nothing caps it. Options: a rate suffix
such as `-bot 2:default@6` that `BotSpecs` parses, so `-bots`, `:bot add` and the
menu share it; graph variants per level; or a ceiling the driver clamps every
graph to. The fleet request carries it once the site exposes one.

### Decide a seat's geometry

- Priority: P3
- Affected files: `internal/app/seat.go`, `internal/app/config.go`

Seats take 120x40, so a `-serve` with no `-size` whose first joiner is its own bot
serves that map. A person's seats could take the holder's terminal instead, with
120x40 left for a `-serve` with defaulted geometry: perception parity with the
person beside them, against bot behaviour that reproduces across terminals.

### Journal a seat's play

- Priority: P3
- Affected files: `internal/app/seat.go`, `internal/vlog`

Under `-j` only the front instance is journaled, since the vlog journal is one per
process, so a seat's play cannot be replayed. Option: named journal sessions, one
per seat.

### Judge a relayed bot's lateness

- Priority: P3
- Affected files: `internal/converge/authority.go`, `internal/app/converge.go`

A guest's bot is never evicted as slow on its own: it has no link to the
authority, so `LinkMetric` reads zero throughput for it, and `DropParticipant` is
a disconnect, which would take its holder. Options: charge its lateness to the
holder's link; evict the bot alone through a dismissal crossing; or keep this
and document it.

### Seat a host's bots without a listener

- Priority: P3
- Affected files: `internal/app/seat.go`, `internal/app/relay.go`, `internal/network`

A host's bots dial a loopback listener, which a run in no session opens first.
Serving them on the in-process stream a guest's bots use would remove that
listener and the loopback exemption from the join budget (which also exempts local
people), let `:host` add a listener to a session opened only for bots instead of
refusing, and let a browser seat bots solo.

### Let a watched holder pause with only its own bots

- Priority: P3
- Affected files: `internal/app/play.go`, `internal/app/seat.go`

A watched bot with seats is in a session, so the viewer forces it unpaused and
ignores pause keys, and a driven holder's seats get no hold. A person's run pauses
with only its own bots; let the viewer pause while `SessionState.SeatsOnly` holds,
and give a driven holder's seats the same hold.

### Name participants beyond their slot

- Priority: P3
- Affected files: `internal/network/session.go`, `internal/snapshot`,
  `internal/render/renderer`

A hexadecimal slot identifies a cursor for one session, and participants have no
names. A label needs a roster and join-reply field, a capture schema change to
reach late joiners, and a trust decision, since each peer would choose its own.

### Train bot graphs offline

- Priority: P3
- Affected files: `internal/bot`, `pkg/genetic`
- Prerequisite: [Beat the default graph](#beat-the-default-graph)'s harness

First decide how a graph declares bounded tunable numbers: a `[bot.params]` table,
or bounded macro parameters shared with the editor's macro metadata. A genome is
those numbers as a bounded vector; `pkg/genetic`'s `Engine` evaluates whole
headless runs over a matrix of scenarios and seeds, and `pkg/genetic/fitness`
scalarises the result. A solo run depends only on its seed, so evaluation needs no
mesh. Decide where trained values ship. Bot genomes never enter the in-game
genetic registry, which captures carry.

### Adapt bots to the session's players

- Priority: P3
- Prerequisite: [Train bot graphs offline](#train-bot-graphs-offline)

A small model of each other participant from Shared state, keyed by participant
for the session and dropped at its end, and a bandit over trained genomes. Decide
what is modelled and whether players see it; nothing is persisted.

## Authoring

### Widen the input hook past order-only play

- Priority: P2
- Affected files: `internal/event/payload.go`, `internal/mode/router.go`,
  `internal/input/machine.go`, `internal/bot`, `internal/system/button.go`

Presses of captured keys and clicks on buttons cross as `EventInputPressed` and
`EventInputReleased` ([Input and modes](input-and-modes.md) §1). Simon judges
order only, so what an order-only game never needed is open:

- Timing: a press applies a playout lead after it was pressed (D-3). A rhythm or
  reaction game judges by the press's production tick: carry the tick in
  `InputPayload`, or widen each window by the lead?
- Sources: right button, wheel, touch, and key release (kitty keyboard protocol)
  have room in `InputSource`. Does a held key mean anything before terminals
  report releases?
- Cursorless play: a press names its cursor. Should a scenario without one still
  take presses, and whose would they be?
- Bots press by key; a `button` `PointAt` target matters only if a bot should
  click.

`only_systems` is root-only, so a puzzle that is one phase of a larger scenario
cannot narrow the set for that phase alone. A region-level form needs an answer for
the regions active beside it, and a system a region enables outside the baseline
does not yet hold its required dependencies on against a later disable.

### Build the map and scenario editor

- Priority: P2
- Affected files: `pkg/tomlmacro`, `internal/fsm`, `internal/manifest/schema.go`,
  `internal/mode`, `internal/render/renderer`, `internal/resource`
- Prerequisite: the argument catalog of the dialect kernel
  ([Behavior machines](machine-design.md) §7)
- Design: [TOML macros](toml-macros.md)

The editor writes ordinary scenarios: a directory under the user root's `scenario/`
that loads, digests, transfers and lists like a hand-written one, so nothing at run
time knows an editor made it.

- **Documents, not a format.** It edits the parsed trees of the entry, region and
  macro files and saves them with `toml.Marshal`, which drops comments, so it writes
  only files it owns (a blocks file, a region file per edited region) and opens
  hand-written ones read-only.
- **Blocks are macros.** The palette is the scenario's macro set and a block's form
  its parameters, defaults and `doc`. A placed block is an invocation, and one
  placed as a state (`Name = { macro = ... }`) is a node the graph view draws. A
  block from a shared library is copied into the scenario's macro files on first
  use, since expansion reads nothing outside the scenario.
- **Macro metadata still needed**, each additive to `pkg/tomlmacro`: reference kinds
  (`state`, `region`, `var`, `event`, `status`) the expander checks as strings, so
  the editor offers pickers and draws an edge per `state` argument; a `point` kind
  the map canvas shows as a draggable marker for pylons, towers and spawns;
  parameter order and per-parameter docs, since a TOML table has no order; root
  `[[expand]]` for blocks spanning several states or a whole region; and provenance,
  so a load error such as an unknown target lands on the block that produced it.
- **Vocabulary.** Action, guard and event pickers read `-schema`, which lists actions
  and guards by name only, without arguments, until the argument catalog lands.
- **Map layer.** Geometry is generated at run time (`EventLevelSetup`, maze rooms,
  `.vifimg` walls). A painted map needs a static form the digest covers, and
  `.vifimg` assets come from the `image/` category, which it does not and a joiner
  is not sent. Decide between a TOML wall encoding (rectangles or run-length rows)
  and admitting `.vifimg` to the scenario container under its bounds.
- **In the game.** An editor mode beside command and visual in `internal/mode` acts on
  a paused world and draws through an overlay like `:graph`. Applying an edit
  validates with `resource.ValidateScenario` and restarts on it as `:n` does, since
  a run plays one scenario. Editing inside a multiplayer session is out of scope.

In order: the argument catalog; reference kinds, order and provenance; a read-only
graph of a scenario's regions, states and blocks; placing and editing blocks into
editor-owned files; the map layer and point markers; root `[[expand]]` blocks and
library copies.

## Browser sessions

### Coarsen navigation fields if busy maps still stall a browser frame

- Priority: P2
- Affected files: `pkg/navigation/flowfield.go`, `internal/system/navigation.go`

Navigation computes a field only for a reader and, while a valid field covers the
same number of targets, at most once per 5-tick cooldown; a 199x97 field is 3.0 ms
under WASM, and in solo play with nothing chasing, navigation costs 0.1 ms a tick.
A busy map pays those 3 ms for every field read when it falls due, and several
groups' fields can fall due in one tick. Next, in order: compute a field over 2x2
blocks, about a quarter of the cells, where its directions mostly agree with the
full field's away from walls; then a second Go instance in another worker computing
fields at a fixed lead, at the cost of a second runtime and of copying the grid in
and the field out.

### Export a backend constructor in the terminal module

- Priority: P3
- Affected files: `lixenwraith/terminal`

`terminal.New` takes only a color mode; a constructor taking a `Backend` would let
a pipe or an in-process emulator stand in for the tty, for tests and tooling.
Measured at 80x the cell path per frame, it is not the renderer route
([Engine decoupling](engine-decoupling.md) §4.3); add it when a consumer exists.

### Serve wss from vif's own listener

- Priority: P3
- Affected files: `cmd/vif`, `internal/network/websocket_other.go`

`-host`/`-serve ws://` is plain: a page served over `https` cannot open it. Taking a
certificate and key would wrap the listener's TCP port in `tls.NewListener` and let
`wss://host:port` bind; decide where the key lives before adding the flags.

### Tighten the per-address budgets

- Priority: P2
- Affected files: `deploy/guest/vif-allocator.env`, `tool/vif-allocator`

The allocator budgets each player address 6 joins a minute over both proxied
routes and 4 creations per `-first-join` window, loose while one address carries
several test clients; the target is one of each. One NAT is one address, so loosen
joins before creations if households report refusals, and key IPv6 on its /64
first.

### Add browser admission authentication

- Priority: P2
- Affected files: allocator/session adapter, website, future authentication
  dependency
- Prerequisite: every route carries real play without transport faults, the
  built-in WebSocket included; no auth layer goes in while that is being settled

Issue a short-lived, session-scoped admission credential after authentication and
consume it during the WebSocket handshake without putting it in page history or
logs. Creation takes one too: `network.RequestSession` is the terminal's side of
`POST /vif/api/sessions`, and already shows a 401 or 403 as the allocator's
message. Decide whether the expanded allocator remains the credential boundary or
is renamed/split before adding the planned `github.com/lixenwraith/auth`
Argon2-SCRAM dependency.

### Fetch a content-addressed bundle over HTTP

- Priority: P1
- Affected files: `internal/resource`, browser resource provider
- Prerequisite: decide publisher trust, allowed origins, and cache policy; the
  format and the limits are `resource.Scenario`'s and are already decided

Two of the three routes to content exist. A native player downloads the release
wad and extracts it as a config root; a guest joining a session is served the
coordinator's scenario and verifies its digest before constructing its `App`. The
corpus needs neither: it is player domain, resolved per instance and never
reconciled, so a peer holding a different one is not a disagreement to settle.

The third route is a client with no config root and no peer — a WASM build before
it has joined anything, which has no roots at all. An HTTP-backed provider reading
the same content-addressed container is what [Multi-platform](multi-platform.md)
anticipates, and it is one provider for both the browser case and a native player
who would rather fetch a scenario than unpack one. What it needs before it is
written is whose signature makes bytes trustworthy, which origins may serve them,
and how long a fetched container is kept.

### Let a browser tab leave and come back

- Priority: P2
- Affected files: `web/terminal.js`, `web/worker.js`, `internal/app/loop.go`,
  `internal/lifecycle`
- Guide: [Build profiles](multi-platform.md#a-hidden-tab), fleet item W1

A hidden tab draws nothing and repaints once on return. What remains is a tab that
leaves: one hidden or suspended for five minutes must cost the other players
nothing and resume or rejoin on return, and a session whose last guest was a
browser must end on its own grace.

## Packaging

Distribution-specific detail lives in [Packaging](packaging.md).

### Promote a nightly build to the first stable release

- Priority: P0
- Affected files: `doc/packaging.md`, `README.md`
- Prerequisite: choose a nightly commit after the complete verification gate
  passes

Tag the selected commit `v0.1.0` and publish the draft release the tag creates,
which already carries the source archive and its checksum; then drop the "no final
release" lines in `README.md` and [Packaging](packaging.md) §2.

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vif-git` alongside the release-based AUR package.

### Provision audio to the fleet when a host needs it

- Priority: P3
- Affected files: `deploy/guest/update-vif-wad.sh`, `tool/vif-allocator/manifest.go`,
  `deploy/k3s/30-session.yaml`

What a fleet session reads from the node volume is one list in three places —
`categories` in the installer, `wadCategories` in the allocator, and the mounts in
the template. `scenario` and `image` are on it. `audio` goes on it the day a
dedicated host renders anything, and `content` never does. Nothing else changes;
the volume already carries whatever the installer puts there.

### Cache a received scenario to the user root

- Priority: P3
- Affected files: `internal/resource`, `internal/app/session.go`

A scenario received from a coordinator lives in memory and is dropped at exit, so
rejoining the same session downloads it again. Writing it under the user root
behind an explicit opt-in would keep it, and needs a trust decision first: the
bytes came from a peer, and nothing about a plaintext link says they are the
operator's.

### Promote verified images to the node

- Priority: P2
- Affected files: `deploy/guest/update-vif-image.sh`, `.github/workflows/release.yml`,
  `deploy/k3s/30-session.yaml`, `tool/vif-allocator`
- Guide: [Session fleet](kubernetes-fleet.md#3-work-list) §3, fleet item H16

Nightly CI publishes the headless image to GHCR under moving and commit-addressed
tags, and the node builds and imports its own. What remains is choosing the
node's registry and promotion policy, authenticating pulls without a long-lived
off-node deployment credential, and moving new sessions to a verified digest while
existing ones finish on theirs.

## Multiplayer

Diagnoses and what each item follows from are in
[Troubleshooting](troubleshooting.md).

### Trim what remains of a tick and a checkpoint at the map limit

- Priority: P2
- Affected files: `pkg/navigation/flowfield.go`, `internal/system/interaction.go`,
  `internal/engine/world_copy.go`

Measure a td tick with 16 players at `GOMAXPROCS=1` and trim the larger of field
derivation and `checkCursorOverlap`'s position lookups; with eager, heap-searched
fields on a 3-tick cooldown they were about 45% and a quarter of a 2.6 ms tick. A
checkpoint scans the whole grid for its occupied cells, most of its 10 ms. Each must
stay exact; per-store write counters stay rejected, since a cache compares values it
owns.

A td participant costs about 300 MB, of which the 32 MB spatial grid is a minority;
whole-world captures and the staging world dominate. In a sandbox a `-serve` host
with one seat held 672 MB resident and 1255 MB with two more, its ticks slipped from
two seats on, and networked joiners stalled ("no authoritative world inside the
convergence floor") or lost their links within seconds.

### Reach a relay without an address in hand

- Priority: P3
- Affected files: `internal/app/relay.go`, `internal/converge/reach.go`, `internal/network`

A person who cannot reach the host joins at a guest's `-relay` address and plays
through it; the session is a tree, raw artifacts go up and the authority's down,
and a relayed person whose relay leaves walks the chain to another participant
(multi-player.md §5). What is left is finding that address: a relay's address is
handed out of band like the host's, nothing publishes which participants relay,
and a NAT between a person and every relay is not crossed. A browser relays only
its own bots. Verify a relayed topology under `LinkShape`s, asserting hash-only
convergence and the authority's per-link fan-out.

### Decide the authority's fan-out and publication cadence

- Priority: P2
- Affected files: `internal/system/network.go`, `internal/converge/correction.go`

A star's authority sends every participant's epochs and owner syncs down to every
other, the producer's own copy included: at 16 players 4,444 epochs and 736 syncs a
second, 1.4 MB/s framed on the tower map and 2.8 MB/s on td before deflate. Rounds
follow the union of peer deadlines, 7.2 a second at td, and whole bodies add
2.2 MB/s there. On one guest with two seats over a 110 ms link
([Troubleshooting](troubleshooting.md) §19) the command stream was nine tenths of
about 64 kB/s both ways before deflate: owner syncs, a 595-byte TOML of 27 fields
of which 17 were zero, every six ticks per cursor and twice over, a fifth; the
explosion batch crossings a quarter; the producer's own committed echo, which a
guest with nothing behind it reads only the marker of, a quarter of the downlink.
Candidates, each a message-set or cadence change that cuts constants where relays
(`-relay`) move the quadratic term off the host: one bundle per link per tick
carrying every source's committed frames with their epoch markers, which pacing
and fences read, as `MsgBundle` already does upstream; owner syncs as deltas, or
first `omitempty` on `CursorStatePayload` as other payloads carry; a frames-less
committed copy to a producer with nothing behind it; shard sets without the
section summary the receiver already holds; and rounds aligned across peers.

### Project a correction outside the live lock

- Priority: P2
- Affected files: `internal/app/snapshot_stage.go`

At td an install that projects nothing already holds the live lock 40–70 ms for the
capture pair, comparison and write, and each projected tick adds up to a simulation
tick, so a repair 16 ticks behind holds it past 100 ms. Reduce the O(E) part first.
Any out-of-lock or incremental design must keep [Multi-player](multi-player.md) §3.3:
fence membership, pending barrier crossings not fed, the ledger settled before
projection, the journal's place and mark, and no live tick between the final
projection and the write. WASM gains scheduling time, not parallel CPU.

### Tell both ends why a join failed

- Priority: P3
- Affected files: `internal/app/session.go`, `internal/network/session.go`,
  `internal/network/endpoint.go`, `internal/app/loop.go`

A bind or dial names the player's next step; these gaps remain:
- A playing host never learns why a joiner was refused: accept and handshake
  failures only reach the log, and `port.Errors()` is drained only by a lobby.
- A refusal written after the joiner's reply goes out raw while the joiner reads
  deflate, so it reads a stream error instead of the reason (from reading; unrun).
- A site request can run 2 minutes, past the 5 s its "Joining" notice lasts.
- A rejoin shows nothing for up to `SessionRejoinWindow`.
- Windows' WSAE codes miss `plainNetError` and keep the system's words.

### Pace repairs as bodies are paced

- Priority: P3
- Affected files: `internal/converge/selective.go`, `internal/converge/correction.go`,
  `internal/snapshot/shard.go`

A repair set waits for a busy link in the per-peer queue a body uses, but leaves as
one frame of up to `SnapshotShardBytesMax` (48 kB), so on a link at the tower's floor
it can hold the epochs behind it for two seconds, where an epoch waits behind at most
one 4 KiB body chunk ([Troubleshooting](troubleshooting.md) §16). Sending it in
`SnapshotCorrectionChunkBytes` chunks, as a body is, bounds that too. Repairs at
20 kB/s have so far been a few kilobytes.

### Find why a storm spawn diverges on a guest

- Priority: P1
- Affected files: `internal/system/storm.go`, `internal/converge/correction.go`

A guest's only large corrections followed its storm spawns in [Troubleshooting](troubleshooting.md)
§18 and none did in §19, where both storms spawned at one tick on the guest and its
two seats, the blue attack never fired in either round and the §18 corrections came
before one could have, so the divergent input is at the spawn itself: the FSM
tick on a predicted quasar kill (`machine.go` dispatches a `PhasePredicted`
`EventSpeciesKilled` like any other), the sweep's victims, or the three z-jitter
draws. The repair logs now name the sections a repair carries; the next §18-shaped
recording says which. The seats' recurring one-page, eight-cell repairs are a
section of exactly eight rows, `streams` or `systems`, and not the storm. If it is
inherent, a forced keyframe at the spawn tick lands the snap once and early
instead of as a repair.

## Session membership

A suggestion for the words, not a decision: a *session* is one world timeline under
one authority — a fleet pod, a terminal host, any instance — and its participants,
and solo play is a session of one. Commands then move participants between sessions:
`:open`/`:close` in place of `:host`, since every instance already is its own
session's authority; `:join`/`:leave` for a participant; `:drop` for an authority
removing one; `:merge`/`:split` for whole sessions.

### Leave a session and play on alone

- Priority: P2
- Affected files: `internal/mode/commands.go`, `internal/app/loop.go`,
  `internal/help/topics.go`

`:leave` ends this instance's part in a session on purpose and keeps its world: it
closes its link, so the authority crosses the departure now rather than on a
timeout, and continues as its own authority. A guest takes the local-fork path it
takes on losing its host ([Multi-player](multi-player.md) §5.1) without the
`Host lost` badge; an authority closes its listener first, and its guests succeed
or fork as its `-authority` policy says.

### Start a host's journal mid-session

- Priority: P3
- Affected files: `internal/app/app.go`, `internal/app/loop.go`

`:journal start` replaces the run, and a host's world holds its guests' cursors, so a
resumed host would carry them into a run they are not in. A host would resume with
those participants departed and reopen its door, as a scenario change does, for its
guests to rejoin.

### Let an authority drop a participant

- Priority: P2
- Affected files: `internal/network`, `internal/app`, `internal/mode/commands.go`,
  `tool/vif-allocator`

`:player drop <slot>` lets a host drop a player with its bots, or one bot, and the
dropped person starts a new local game. What remains: a reason the dropped client
prints, playing on alone as after `:leave` instead of starting over, and an
operator route on the allocator, since a dedicated host has no console. Keeping a
dropped player out is a separate decision: the session name is a routing key, not
an identity.

### Drop the cursors a linked instance cannot reach

- Priority: P3
- Affected files: `internal/app/session.go`, `internal/converge/authority.go`

`dropAbandonedCursors` acts only on an instance left with no link, so a successor
some survivors could not reach, or a fork still linked to its own bots, keeps every
roster entry, and a participant it has no path to stays a cursor nothing moves
([Multi-player](multi-player.md#52-the-roster-after-a-loss) §5.2). Decide who
produces those departures, and at which tick, on an instance that still has peers
to agree one with.

### Merge two sessions

- Priority: P3
- Prerequisite: a redirect message, "rejoin at this link", which the split shares

Two sessions, each under its own authority, become one by keeping one world: B's
authority redirects its participants to A's link, they rejoin A as fresh players, and
B's world ends — `:leave` and the drop above are the same move for one participant.
Merging two worlds is not planned: two timelines share no state to reconcile, which
is why a higher term from a fork is refused today.

### Split a session under a second authority

- Priority: P3
- Prerequisite: the redirect message, and an authority seeded from a live world

For a session that outgrows its authority — a large roster or a heavy scenario
lagging — twelve participants under A become seven under A and five under B. B is a
participant at first, ideally a fresh fleet pod. A hands B its current world under a
new session identity rather than a new term, redirects the five, and the two worlds
diverge from there. Late joiners already install a mid-run world; what is missing is
the redirect and an authority, for a pod an allocator request, that starts from a
transferred world instead of a scenario.

## Combat

### Let stronger knockback override the active immunity window

- Priority: P2
- Affected files: `internal/component/combat.go`, `internal/system/combat.go`,
  `internal/profile/combat.go`

Record the strength of the knockback that opened immunity. A stronger incoming
knockback should reset the immunity duration and apply its impulse; equal or weaker
hits remain blocked across all weapons and players. Carry the recorded strength
through snapshots and synchronization. Stun immunity remains target-wide.

### Drop the kinetic window's dead joiner path

- Priority: P2
- Affected files: `internal/system/combat.go`, `internal/component/combat.go`,
  `internal/snapshot/digest.go`, `doc/generic-kinetic.md`

Both kinetic hit sites (`combat.go:557`, `:733`) land only while
`RemainingKineticImmunity` is zero, so every accepted knockback opens the window:
`spendKineticImmunity` always reports opened, `joining` never composes an impulse,
and `KineticImmunitySpent` is written but read only by the digest. Generic kinetic
§5 still describes a per-attacker kinetic budget whose joiners add. The target-wide
gate is the intended rule (see the item above), so delete the joiner path, the field
and its digest line, and correct that text; the field's removal is a capture schema
bump.

### Let mounted weapons strike more than cursors

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/profile/combat.go`

A mounted ray destroying walls and glyphs, mounted weapons striking other species, and each instance's own drains; Shared outcomes must resolve from Shared geometry, not a Player-domain shot.

### Let cursor weapons strike other cursors

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/system/bullet.go`, `internal/profile/combat.go`

PvP: a hit on a remote cursor crosses as its impact and the victim's owner applies it, as `strikeCursor` does for mounts.

### Tune the charge-cap effects after play tests

- Priority: P1
- Affected files: `internal/parameter/{combat,player,effect}.go`,
  `internal/system/weapon.go`

Arc reward (an arc pays the lightning's full energy reward; halve it if the cap doubles the rod's income too far), the sustained ray's scale (three damage per covered member per immunity window, continuously), the burn figures and whether the charged pulse's stun should shorten to offset them, `LightningArcRange`, `WeaponStreamInterval` and `TurretStream`, the gun's lane offsets at two charges, `LightningZapDuration` for an arc, and a burn tint on species renderers, which show only the hit flash per tick now.

### Drop the gun's charges as loot

- Priority: P2
- Affected files: `internal/component/loot.go`, `internal/profile/loot.go`

Only a scenario's `EventWeaponAddRequest` or god mode raises the gun past one charge; a loot row with a drop tier would let play do it.

### Direct the gun by scenario

- Priority: P3
- Affected files: `internal/component/weapon.go`, `internal/system/weapon.go`, `internal/fsm`

`WeaponSpec.Directions` is a cardinal mask the spawn already reads; a side-scroller needs a level to set one direction, which waits on scenario authoring.

### Fan a fully charged ray into spokes

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/component/ray.go`

Charges beyond the ray could add rays 360°/n apart, one through the sustained orb ray; a sweep already passes each target once a turn, so spokes trade coverage for crossings.

### Move storm's green circle onto a disruptor mount

- Priority: P3
- Affected files: `internal/system/storm.go`

Its area pulse is the hosted disruptor's shape; the red circle's turret mount is the pattern.

## Environment

### Carry the cursor with the text it is on

- Priority: P2
- Affected files: `internal/system/environment.go`, `internal/system/cursor.go`

Glyph drift moves the line under a cursor but not the cursor, so a player chases
moving text. Carrying it would move each local cursor with the glyph it stands on,
as a Player-domain cursor write per layer step; a peer's cursor would follow
through its owner's move, never through the drift.

### Decide what a delete removes under a drift stack

- Priority: P2
- Affected files: `internal/system/typing.go`

`handleDeleteRequest` removes every player glyph in its range, including farther
layers hidden under a cell's top glyph, while typing, rendering and motions act
on the top glyph only. Deleting only each cell's `TopGlyphAt` would make a
delete remove exactly what is shown.

### Refresh motion markers as glyphs drift

- Priority: P2
- Affected files: `internal/system/motion_marker.go`

Markers are recomputed on cursor movement and mode changes, so under a drift they
sit on cells the glyphs have left. Recomputing after a drift step, which the
environment could announce as a local event, would keep them on the text.

### Widen what drifts

- Priority: P3
- Affected files: `internal/system/environment.go`, `internal/parameter/environment.go`

Composite members (gold), particles, nuggets and species stay put; a side-scroller
may want some of them carried with the nearest layer. Decay and blossom change a
glyph's level, so a glyph they touch changes layer and speed mid-flight. In 256
colours normal and bright glyphs share one console entry, so only the dark layer
reads as farther; a distinct attribute for bright would separate the three.

## Rendering

### Configuration menu follow-up

- Priority: P2
- Affected files: `internal/mode/config_menu.go`, `internal/paths/settings.go`
- Persist operator preferences through an explicit save action and startup
  loading; the live menu currently changes only this run, and what `:new` keeps
  (rate, HUD, pins, standing breakpoints) a restart or scenario change drops. Music
  gain needs an authoritative readback before joining effects volume in the menu.
  Keymap, backend, paths and colour depth remain startup settings; `:d break`
  stays on the command line, since a row cannot take its arguments.

### Use sixteen console backgrounds on Linux

- Priority: P3
- Affected files: `internal/render/console.go`

The framebuffer console shows a bright background under SGR 5, which vgacon blinks instead; telling them apart would give Linux FreeBSD's sixteen backgrounds.

### Draw the flow-field debug view from console glyphs

- Priority: P3
- Affected files: `internal/render/renderer/flow_debug.go`

Its diagonal arrows and `◆●` are missing from console fonts, so in 256 colours the view shows their fallbacks.

## Genetic library

### Spin off `pkg/genetic` as its own module

- Priority: P2
- Affected files: `pkg/genetic`, `pkg/genetic/registry`, `pkg/genetic/fitness`,
  `pkg/genetic/tracking`, `pkg/genetic/persistence`, `sandbox/genetic`

The audit before the spin-off left these for the new module, none of which the
game needs. The pending table is direct-mapped, so a long evaluation is evicted
by issue distance even when the table is nearly empty; bounded linear probing
that evicts the oldest id in its window would make `PendingCapacity` a live-count
bound, at the cost of a checkpoint-format version. The streaming engine breeds as
soon as two outcomes exist; breeding only once the archive is full was measured
on the lander example and changed nothing, so it stays unimplemented. The batch
`Engine` spawns one goroutine per candidate under a semaphore and reallocates
offspring every generation; a fixed worker set over an atomic index and a free
list would remove both. `RouletteSelector` rebuilds its cumulative table on every
`Select`; cache it by archive version. `registry.Stats` restates
`genetic.PoolStats` field for field and `SpeciesConfig.PerturbationStdDev`
restates `EngineConfig.PerturbationStrength`; collapse each pair. The `fitness`
context keys and most `tracking` metric keys are conventions of this game, and
the `persistence` default codec is the one external dependency: make JSON the
default and move the TOML codec to the caller, or drop the subpackage.

## Engine

### Initialize systems in dependency order from the engine

- Priority: P3
- Affected files: `internal/engine/world.go` (`CheckSystemDependencies`),
  `internal/app/app.go` (`initScheduler`), each system's constructor and
  `EventGameResetRequest` handler in `internal/system`

Half of this exists: the manifest declares each system's `Requires` and startup
checks the graph for unknown names and cycles. The other half does not: each
system still calls its own `Init` in its constructor (manifest order) and again
on reset (handler order). The engine would instead call `Init` once per system
in the graph's `core.TopoSort` order, at startup and on reset, and the per-system
calls would go.

### Give the composite lifecycle methods a user

- Priority: P3
- Affected files: `internal/system/composite.go`

`CompositeSystem` carries `handleEmptyComposite`, `CreateHeader`, `AddMember`,
`DestroyComposite` and `GetAnchorForMember`, designed for building and tearing
down composites at run time, but no system or scenario uses them. A species or
scenario that assembles composites would; it reaches them through events, since
another system may not call them directly.

## Tests

### Cut the app package's test time

- Priority: P2
- Affected files: the socket tests in `internal/app` (`bot_test.go`, `host_test.go`,
  `join_test.go`, `migration_test.go`, `script_test.go`)

Without `-race` the package's wall time is set by its 47 serial tests, about 23 s
of 33 s on 4 cores: each drives a real socket against the shipped wall-clock
deadlines (heartbeat loss, succession, join readiness), so none can share the
machine. Test-only deadlines a fraction of the shipped ones, or the in-process mesh
where a socket is not the rule under test, would cut them.

### Fuzz the join handshake

- Priority: P1
- Affected files: `internal/network` (join and peer-link handshakes, admission)
- Guide: [Session fleet](kubernetes-fleet.md#4-security-posture) §4, fleet item H1

The game port is unauthenticated by decision and reachable from the Internet, and
§4 lists what bounds a stranger on it. What it lacks is a fuzz target in
`internal/network` for malformed, oversized, replayed and half-open handshakes,
beside the frame and upgrade targets `pkg/websocket` already has.

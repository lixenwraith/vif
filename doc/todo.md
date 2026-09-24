# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release, P1 is wanted next, P2 is convenient follow-up,
and P3 is an idea.

## Browser sessions and mobile

### Exercise the browser path's edges

- Priority: P1
- Affected files: `internal/network/websocket_wasm.go`, `deploy/k3s/30-session.yaml`

A browser guest has joined and played on the deployed node. Not yet run: a dropped
socket rejoining, a suspended tab, and expiry with a browser in the session. The
sidecar's CPU and memory are still the estimate in `30-session.yaml`. The hop costs
about 40 µs a round trip, except that websocat 1.x never sets `TCP_NODELAY`: about
0.5% of browser-to-game frames wait 40–80 ms on a delayed ACK. A bridge that sets it
removes that.

### Restore a per-player bound for browser participants

- Priority: P1
- Affected files: `tool/vif-allocator`, `deploy/website/vif.nginx.example`
- Prerequisite: the browser path is carrying real players

Every browser participant reaches the session pod from `127.0.0.1`, so
`network.AdmissionLimiter` gives the whole browser population one budget instead of
one each. The edge's `limit_conn`/`limit_req` and the allocator's per-session
ceiling stand in for it today. Decide whether that is the answer or whether the
allocator should carry a per-address bound of its own, which means deciding whether
it may trust a forwarded address at all.

### Decide what `page_url` names

- Priority: P1
- Affected files: `tool/vif-allocator/allocator.go`, the site

`page_url` is `-page-base` with the session's NodePort appended, and no deployment
has built the page it names: the reference site has no `session/` tree, so the link
the fleet page renders falls through that server's `try_files` and answers the
homepage with 200. Either build one page that reads its own identifier from the
URL, or drop the field and let `join_target` and `ws_url` be the whole of what a
session publishes.

### Add browser admission authentication

- Priority: P1
- Affected files: allocator/session adapter, website, future authentication
  dependency
- Prerequisite: the unauthenticated browser path is bounded and measured

Issue a short-lived, session-scoped admission credential after authentication and
consume it during the WebSocket handshake without putting it in page history or
logs. Decide whether the expanded allocator remains the credential boundary or is
renamed/split before adding the planned `github.com/lixenwraith/auth`
Argon2-SCRAM dependency.

### Fetch a content-addressed bundle over HTTP

- Priority: P1
- Affected files: `internal/resource`, browser resource provider
- Prerequisite: decide publisher trust, allowed origins, and cache policy; the
  format and the limits are `resource.Scenario`'s and are already decided

Two of the three routes to content exist. A native player downloads the release
wad and extracts it over a config root; a guest joining a session is served the
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

### Extract the renderer-neutral Android host model

- Priority: P1
- Affected files: terminal-shaped input/cell/color values, `internal/app`, host
  entry points
- Prerequisite: define the minimum visual and semantic-input contract for the app

Target a minimally polished Android build within one month of development time.
Move terminal-specific visual and input values behind positive renderer/host
adapters, add a library entry point, and keep lifecycle, simulation, networking,
and resource providers common.

## Packaging

Distribution-specific detail lives in [Packaging](packaging.md).

### Promote a nightly build to the first stable release

- Priority: P0
- Affected files: `.github/workflows/nightly.yml`, release artifacts and
  `doc/packaging.md`
- Prerequisite: choose a nightly commit after the complete verification gate
  passes

Promote the selected commit to `v0.1.0` and publish a byte-stable source tarball
with its checksum. Keep the moving nightly prerelease and headless GHCR image as
development artifacts rather than treating them as the stable source archive.

### Install a manual page

- Priority: P1
- Affected files: `cmd/vif/usage.go`, `doc/vif.1`, `Makefile`
- Prerequisite: keep the flag table as the source of truth

Generate `vif.1` from the flag table and install it through `make install`.

### Add a desktop launcher

- Priority: P2
- Affected files: packaging assets and `Makefile`
- Prerequisite: select an installable icon

Add a `.desktop` entry with `Terminal=true` and install its icon.

### Add shell completion

- Priority: P2
- Affected files: `cmd/vif/usage.go`, completion assets, `Makefile`
- Prerequisite: choose generated or maintained completion definitions

Provide shell completion for `vif` and install it in the appropriate data path.

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vi-fighter-git` alongside the release-based AUR package.

### Let the whole tree build headless

- Priority: P3
- Affected files: `cmd/soundlab`

`go build -tags=vif_headless ./...` fails: `cmd/soundlab` is fourteen untagged
files over `parameter.BuiltinSounds`, which the tag removes. The gates and
`script/test.sh deploy` therefore build `./cmd/vif` alone, as the image does, so a
break confined to soundlab is invisible. Tag the package out, or give it a stub.

### Let a request name the map size

- Priority: P2
- Affected files: `tool/vif-allocator/config.go`, `tool/vif-allocator/allocator.go`,
  the Hugo site's `vif-fleet.js`

`-map-size` is the deployment's for every session, and the session page cannot
select it. A scenario that fixes its own dimensions ignores it — `td` emits
`EventLevelSetup` at 500x250 — so the flag only decides the scenarios that do not,
and those all run at one size. Offer it the way `scenarios` is offered, bounded by
`parameter.MaxMapCells` rather than by a list: `limits` names the ceiling, a
request names a size under it, and a scenario that sets its own still wins.

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

## Audio

### Surface malformed user sound configuration during play

- Priority: P1
- Affected files: `internal/service/adapter_audio.go`
- Prerequisite: choose the in-game error surface

Fallback currently succeeds silently during play while `-check` reports the
error. Expose the latched `AudioEngine.SpecError()` without making fallback
fatal.

### Move built-in music patterns into an editable asset

- Priority: P2
- Affected files: `cmd/soundlab/session.go`, `pkg/audio`,
  `internal/asset/audio/music.toml`
- Prerequisite: define and validate the pattern document

Replace the built-in Go registry literals with `internal/asset/audio/music.toml`
so `soundlab` edits the same data the game loads.

### Make the mixer buffer configurable

- Priority: P3
- Affected files: `pkg/audio/params.go`
- Prerequisite: define a construction-time option and recompute dependent buffer
  sizes before the mixer or backend starts

Replace the compile-time 50 ms mixer buffer with a construction-time setting.

### Decide reveal behavior for manual intensity decreases

- Priority: P3
- Affected files: `internal/system/music.go`
- Prerequisite: deterministic rising and falling transition coverage around
  `applyArrangement`

Manual intensity changes always use per-bar track reveal, while automatic
changes reveal only when intensity rises. Decide whether manual decreases should
retain that distinction.

## Multiplayer

Diagnoses and what each item follows from are in
[Troubleshooting](troubleshooting.md).

### Find what diverges a guest in the tower region

- Priority: P1
- Affected files: `internal/system/network.go`, `internal/engine/snapshot_roster.go`,
  `internal/system/interaction.go`, `internal/snapshot/manifest.go`
- Prerequisite: a delayed-link harness that counts, per section, what a guest in the
  tower region answers wrong

A guest present when the region starts answers 77% of manifests hash-only, against
95% on the main map; the differing sections are eye, genotype, combat and fsm. The
lead suspect is owner-authored cursor state: `writeCursorState` skips the owner, so
shield, energy and heat change at once there and a lead later elsewhere, while eye
and snake contact reads them from shared systems — the shape crossings had before
they applied at one tick. If confirmed, shared readers take the committed copy on
every instance and the live value stays with presentation. The browser cost is the
other half: every answer captures and hashes the whole world, maze walls included;
per-store write counters would let an unwritten section keep its hash.

### Decide who owns a contested shared hit

- Priority: P2
- Affected files: `internal/system/combat.go`, `internal/component/combat.go`,
  `internal/engine/prediction.go`
- Prerequisite: none; it is a rule to choose

`LastDamagedBy` and the knockback override `SpendKineticImmunity` reports as
`opened` both follow arrival order, and a hit that missed the lead is applied late
by the authority, so for that lateness two instances disagree on the kill's credit
and on which hit overrides. The correction repairs the world, but the ledger pays
the credit its own prediction recorded, and the authority's world no longer holds
the dead entity to read it from. One rule for both, made where the hit is produced
and independent of arrival order, closes both; the per-attacker window was that
answer for the damage budget.

### Predict a typed gold member instead of publishing it

- Priority: P2
- Affected files: `internal/system/network.go`, `internal/system/typing.go`,
  `internal/render/renderer`
- Prerequisite: none

`EventCompositeMemberDestroyed` is the one crossing its producer applies at once
(`producerImmediate`), because `isLeftmostMember` validates the next keystroke
against the live run; a correction inside the lead shows the member again for a
tick. A player-domain tombstone the check skips and the glyph renderer hides would
let the member cross at the agreed tick like everything else.

### Retire a splash whose shared anchor an install re-issues

- Priority: P3
- Affected files: `internal/app/capture.go`, `internal/system/splash.go`
- Prerequisite: none

An install restores the authority's shared allocator, so the ids from its
`NextEntity` up to this instance's are issued again. The prediction ledger drops
entries in that range; a timer splash anchored in it keeps counting on whatever
composite takes the id until it expires. The install knows the range and can hand
it to the one other holder of a bare shared id.

### Keep the correction magnitude to the shared surface

- Priority: P3
- Affected files: `internal/gen-manifest/main.go`, `internal/app/capture.go`
- Prerequisite: none

`SharedWorldDifference` counts the owner-authored cells of a cursor this instance
authors, which `RebindCursorRoster` then restores, so a projected install reports a
phantom entity per owned cursor. The generated difference can skip the nine stores
`snapshot_roster.go` restores for the entities `CaptureCursorControl` held.

### Find what makes a networkless soak load-sensitive

- Priority: P2
- Affected files: `internal/app/soak_test.go`, `internal/event/pool.go`,
  `internal/event/batch_pool.go`
- Prerequisite: a reproduction; 36 runs under parallel load, and 12 more beside six
  spinning cores, stayed clean

`TestSoakAppsAreIndependent` failed twice in nine loaded suite runs with two worlds
of one seed differing in their position digest. A headless App ticks on its caller
with no scheduler goroutine, the streaming GA has no workers, and the domain audit
only records, so what parallel Apps still share is the payload pools
(`CharacterTypedPayloadPool`, the batch pools): a payload read after its release is
the candidate, and moving the pools onto the World the smallest step.

## Fleet logging

### Repin LogWisp past the fleet stream fixes

- Priority: P1
- Affected files: `deploy/logwisp/REVISION`
- Prerequisite: the quiet-stream keepalive and the rotated-file resume reaching
  LogWisp `main`, which the installer requires the pin to descend from

`REVISION` names v0.18.1, which predates both. Until it moves, a vacant node still
idle-expires a connected viewer and evicts it on the next session's first record,
and a session crossing the 8 MiB file cap still replays its rotated log whole,
spending the rate limit on duplicates while live records drop.
[Deploying the session fleet](kube-docker-deploy.md) §10 states what the pin must
carry.

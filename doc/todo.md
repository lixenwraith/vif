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

## Multiplayer

Diagnoses and what each item follows from are in
[Troubleshooting](troubleshooting.md).

### Write only what a correction changed

- Priority: P1
- Affected files: `internal/app/snapshot_stage.go`, `internal/converge/selective.go`

Every correction a guest takes, a two-page repair included, stages the whole
capture and projects and writes the whole world under the live lock: about 35 ms
and 30 ms at 239x64 in the tower region, 50 ms and 60-104 ms in a 9,300-entity
session. Most move nothing: 27 of 31 there, 38 of 55 in that session. A repair
could splice its pages into the capture the live world already matches, and a body
whose index root equals this instance's at that tick could be adopted as a
hash-only answer is.

### Keep route graphs across an install that moved no wall

- Priority: P2
- Affected files: `internal/system/navigation.go`

`NavigationSystem.LoadShared` rebuilds passability, every flow field and every
gateway route graph on each install, in the staging world and again in the live
commit: 44% of install CPU in the tower region. A route graph is a function of its
endpoints and the passability grid; keeping one needs the grid generation it was
built at, because `refreshRouteGraphs` also rebuilds between installs.

## Combat

### Let mounted weapons strike more than cursors

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/profile/combat.go`

A mounted beam destroying walls and glyphs, mounted weapons striking other species, and each instance's own drains; Shared outcomes must resolve from Shared geometry, not a Player-domain shot.

### Let cursor weapons strike other cursors

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/system/bullet.go`, `internal/profile/combat.go`

PvP: a hit on a remote cursor crosses as its impact and the victim's owner applies it, as `strikeCursor` does for mounts.

### Fan a fully charged beam into spokes

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/component/beam.go`

Charges beyond the ray could add rays 360°/n apart, one through the orb; a sweep already passes each target once a turn, so spokes trade coverage for crossings.

### Move storm's green circle onto a disruptor mount

- Priority: P3
- Affected files: `internal/system/storm.go`

Its area pulse is the hosted disruptor's shape; the red circle's turret mount is the pattern.

### Route species contact damage through strikeCursor

- Priority: P3
- Affected files: `internal/system/{quasar,swarm,storm,eye,pylon,snake,drain}.go`

Each still writes the shield-drain-or-heat pair by hand; `TestSharedCursorOverlapOutcomesStayOwnerResolved` counts must move with it.

## Rendering

### Use sixteen console backgrounds on Linux

- Priority: P3
- Affected files: `internal/render/console.go`

The framebuffer console shows a bright background under SGR 5, which vgacon blinks instead; telling them apart would give Linux FreeBSD's sixteen backgrounds.

### Draw the flow-field debug view from console glyphs

- Priority: P3
- Affected files: `internal/render/renderer/flow_debug.go`

Its diagonal arrows and `◆●` are missing from console fonts, so in 256 colours the view shows their fallbacks.

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

# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release or leads the feature development push, P1 is
wanted next, P2 is convenient follow-up, and P3 is an idea.

## Code issues to review

The documentation sweep met these in the code and left the code as it is, its
docs describing what the code does. A second agent confirmed each against the
code and reproduced all but two in a scratch run. Review an item, then fix it,
refile it at its real priority under its area, or delete it.

### Let -relay take vifs:// on the command line

- Priority: P1, to review
- Affected files: `cmd/vif/main.go`, `cmd/vif/main_test.go`, `doc/development.md`,
  `doc/runtime.md`, `doc/services-and-networking.md`

`validateInvocation` (`cmd/vif/main.go:875`) takes only `vif://` as a router, so
`-join … -relay vifs://<router>` exits 1 ("vifs:// is dialed, never bound"), and
`relayTarget` (`:962`) would cut it to host:port; `config.RelayRouter` takes it, and
the `-relay` hint, `vif-router.env`, the router README, multi-player §5.6,
auth-session §2 and §3.5 and term-index's tunnel name it. Fix: test either scheme at
both sites, a `cmd/vif` case, `vifs://` in the three docs above.

### Look up and dial a vifs:// link's listed routes

- Priority: P1, to review
- Affected files: `pkg/network/endpoint.go` (`linkSites`),
  `internal/network/session.go` (`dialListed`)

`linkSites` (`endpoint.go:550`) splits the join address with `net.SplitHostPort`,
which refuses `vifs://host:port`, so the router's `vifs://` links are never looked
up: no WebSocket fallback, no browser join. `dialListed` (`session.go:1608-1613`)
dials and stores a listed `vifs://` door's bare `e.Addr`, in the clear. Fix: split
and compare the failed `ParseEndpoint(addr).Addr`; dial and store `e.Dialable()`.

### Bind a passphrase exchange the same way at both ends

- Priority: P1, to review
- Affected files: `internal/network/session.go`, `internal/network/admission.go`,
  `internal/network/session_test.go`, `internal/app/door_test.go`,
  `doc/auth-session-design.md`

A door binds SCRAM to its leaf over TLS (`session.go:946-949`), a dial to the leaf
it sees or none (`session.go:1026`); bound never verifies unbound, so a `vifs://`
dial at a relay door the router publishes (`relayAcceptor`, no `Binding`) and a page
at a gated `wss://` door (`wsConn`, no TLS) have the right phrase refused. Fix:
`AuthRequest` says whether the dial bound; the door binds likewise, a tunnel's to
`bindingOf(conn)`.

### Present a replay on the platform's host, as -watch does

- Priority: P2, to review
- Affected files: `cmd/vif/main.go`, `internal/app/play.go`,
  `internal/app/presentation_headless.go`

`-replay` calls `app.PlayJournal` with no host and no `presenting()`
(`cmd/vif/main.go:258`), and its lender is `runConfig{Config: cfg}`
(`internal/app/play.go:49`), so it presents on `newTerminal`: `-g` or a `vif_gui`
build replays in the terminal, the Windows window build has none, and the build's
own speakers go untried (multi-platform §8). Fix: both builds' `PlayJournal` take a
`Host` after `viewer`, the lender's `runConfig` carries it (its copies borrow its
presentation), and `-replay` passes `presenting()`.

### Name the blend tester's additive row Add

- Priority: P2, to review
- Affected files: `tool/blend-tester/main.go`, `tool/blend-tester/blend.go`

`blendOps` (`main.go:117`) labels `render.BlendAdd` (formula `min(Dst + Src, 255)`)
as "Set". Every other row is named for its mode, and Rendering §4 calls this
operation Add. The blend screen shows `OP: Set`, and the result switch repeats the
name in `case 2: // Set` above `color.Add` (`blend.go:119`). Smallest fix: rename
the label to `"Add"` and the comment to `// Add`.

### Group `network.shed` with the authority card

- Priority: P2, to review
- Affected files: `internal/status/key.go`, `doc/services-and-networking.md`

`splitKey` (`internal/status/key.go:109-112`) puts `network.evicted` in the
`network.authority` card but leaves its sibling `network.shed`
(`Authority.driveShedding`) in the catch-all `network` card, though the reset test's
"whole authority card" (`internal/app/telemetry_test.go:82-90`) counts it there. Add
`name == "shed"` to that case, and name shedding beside eviction in
`doc/services-and-networking.md`'s card list.

### Group `snapshot.repair_sections` with the repair card

- Priority: P2, to review
- Affected files: `internal/status/key.go`, `doc/telemetry-reference.md`

`snapshotSelectiveGroup` (`internal/status/key.go:158`) routes the repair card's
keys but not `repair_sections` (`internal/snapshot/telemetry.go:133`), so the
sections a repair moved land on the plain `snapshot` card among the capture costs,
against the card comment at `key.go:127`. Fix: add `"repair_sections"` to the case
at `key.go:163` and drop "`repair_sections` in `snapshot`" from
`doc/telemetry-reference.md:136`.

### Count the quota read in a create's HTTP deadline

- Priority: P2, to review
- Affected files: `tool/vif-router/main.go`

`main.go:120-122` sets `WriteTimeout` to `ReadyTimeout + 3*RequestTimeout + 10s` for
the "three Kubernetes requests" a create makes before readiness, but
`createTransaction` (`allocator.go:446`) makes four: `listJobs`, `listQuotas`
(`fits`), `createJob`, `createService`. The 10 s slack covers the fourth only up to
the default `-kube-timeout`; past it a ready session's 201 is lost (at 16 s: EOF at
66 s, deadline 63 s). Fix: `4*cfg.RequestTimeout` and "four" in the comment.

### Word the -join-host help for a TLS front door

- Priority: P2, to review
- Affected files: `tool/vif-router/config.go`

`config.go:137` helps `-join-host` as "public raw-TCP host returned to players", but
under `-route-secure` (this deployment), `-route-cert` or `-route-selfsign` every
`join_target` and door link it names reads `vifs://` (`allocator.go:88`,
`main.go:107`). Fix: "public host every join_target and door link names (required)",
the README's wording with doors added.

### Hold magic's rounds past the fifth at the fifth to eighth ranks

- Priority: P2, to review
- Affected files: `wad/scenario/base/magic/scenario.toml` (`LevelUp`),
  `doc/gameplay.md`

`LevelUp` (`scenario.toml:513-515`) sets the stray to
`min(level % 4 + level / 4, 7)`: the sixth round runs ranks 6, 7, 8, 8, the seventh
7, 8, 8, 8 and each round from the eighth rank 8 alone, but its comment and Gameplay
§10 say the fifth round and every one after it run the fifth rank to the eighth. Add
`max = 4` to the round's `DivideVar` and drop the stray's `max = 7`, which no longer
takes effect; or, if the climb is meant, say so in both.

### Stop a phase once its region terminates itself

- Priority: P2, to review
- Affected files: `internal/fsm/machine.go`, `doc/fsm-reference.md`

`TerminateRegion` (`machine.go:987`) defers only from `on_exit`; elsewhere the phase
runs on over the removed region (reproduced): a transition's target or a parent's
children exit, then enter (`:781-789`, `:171-175`), and an update's Tick exits again
(`:207-237`). Fix: mark `terminated` while any phase runs; after the issuing list,
the phase exits what it entered (`:741-744` too, which skips the LCA's path) and
removes the region.

### Report a bad region initial state in name order

- Priority: P2, to review
- Affected files: `internal/fsm/loader.go`, `internal/system/maprange_test.go`

`LoadScenarioFromMap` step 8 (`loader.go:187`) ranges over the `config.Regions` map
and returns at the first region whose `initial` names no state, so with two such
regions the error names either, run to run, where the region check at `loader.go:50`
goes in name order. Range over `slices.Sorted(maps.Keys(config.Regions))` as it
does, and drop `regionInitials` from the loop's reason at `maprange_test.go:55`.

### Delete the route weight floor that never binds

- Priority: P2, to review
- Affected files: `pkg/navigation/routegraph.go`

`computeRouteWeights` (`pkg/navigation/routegraph.go:338`) floors each raw weight
`maxDist / d` at `RouteGraphMinWeightFloor` (0.05). No `d` exceeds `maxDist`, so
`w >= 1` never reaches the floor. After normalizing, up to 8 routes within the 50%
tolerance still keep at least 2/23 (0.087) each. Delete the constant and its `if`,
and the floor named in the comments of `computeRouteWeights` and `Route.Weight`.

### Keep a bot's `help` and Normal-mode `escape` out of the overlays

- Priority: P2, to review
- Affected files: `internal/bot/mind.go`, `internal/mode/router.go`, `doc/bots.md`

`intentFor` (`mind.go:584`) admits `help` and `escape`, which in Normal opens the
menu (`router.go:498`), against `doc/bots.md#actions`. Either one leaves the bot in
an overlay that pauses a solo run and drops all its pointer play (`router.go:247`).
Refuse `IntentHelp` there and add it to the doc's list. Spare a bot's Escape in
Normal the way macro playback's is spared (`router.go:302`, keyed on
`World.Session().Bot`): refusing `escape` would strand a bot in Insert.

### Color a refusal on the native menus' status line

- Priority: P2, to review
- Affected files: `android/app/src/main/java/com/lixen/vif/MenuDialog.java`,
  `web/terminal.css`

The snapshot sends every refusal (`setCommandError`, or an op on a disabled row)
with `status.kind` `error`. `MenuDialog.java:184` and `terminal.css:409-410` color
only `alert` (danger) and `progress` (accent), so a refusal shows white like a
notice. The console draws it salmon, and the same sentence refusing a form's submit
shows in danger under the form. Smallest fix: give `error` the danger color `alert`
already has, on both hosts. Traced, not run.

### Name vifs:// in the menu's join row

- Priority: P2, to review
- Affected files: `internal/mode/config_form.go`

The Join a session row's description (`config_form.go:84`) lists `host:port[/name]`,
`vif://`, `tcp://` or `ws(s)://` but not `vifs://`. Yet `-join`, `:join` and
`network.ParseEndpoint` take `vifs://`, a TLS front door's links use it, and
`doc/config-menu.md` names it for this box. A menu search for `vifs` finds no row.
Add `vifs://` after `vif://` in that string; the Spectate row says "as Join takes
one" and follows.

### Count -doors-per-address by the admission key, not the whole host

- Priority: P2, to review
- Affected files: `tool/vif-router/tcp_proxy.go`

`tcpRouter.tunnel` passes `TunnelHub.Publish` the dialer's whole host
(`addressHost(from)`, `tool/vif-router/tcp_proxy.go:382`). The join budget charged
for the same publication, and the router's spectator and create counts, use the /64
(`network.AdmissionKey`). So one IPv6 /64 that rotates hosts holds
`-doors-per-address` doors per host, up to `-tunnels`. Fix: pass
`network.AdmissionKey(from)` and delete `addressHost`.

### Lift the top diagonal exclusion slots `padding` rows clear of the exclusion

- Priority: P2, to review
- Affected files: `pkg/position/search.go`

`exclusionSlots` (`pkg/position/search.go:399-400`) stands the top-right and
top-left slots at `-exclusion.Top - objectH + padding`, where the top slot (:394)
takes `- padding` and the bottom diagonals (:397-398) stand `padding` rows below the
exclusion. So a top diagonal's lower `padding` rows lie beside the exclusion's top
rows, and a countdown there inks level with a one-row header, not a row above it.
The fix is `- padding` on both lines.

### Name RunBot beside RunScript in the network-session refusal

- Priority: P3, to review
- Affected files: `internal/app/config.go`, `internal/app/internal/config/config.go`

`runConfig.validateWiring` (`internal/app/config.go:291`) refuses a headless run
given a host or join address outside `drive` as "network sessions require
app.RunScript", yet `drive` admits `RunBot`'s run as it does a script's
(`internal/app/script.go:426`); only an embedder's `NewHeadless` reaches it.
Smallest fix: "...require app.RunScript or app.RunBot", naming a bot's run beside
the script in `ModeHeadless`'s comment too
(`internal/app/internal/config/config.go:35`).

### Count a self-declared bot as no person when a dedicated host sizes its map

- Priority: P3, to review
- Affected files: `internal/app/internal/membership/session.go`

`noteJoinerReport` (`session.go:293-299`) counts every joiner with no holder a
person, one reporting `Bot` among them, so a bot's own run
(`-join <addr> -bot N -headless`) sizes a `-serve` lobby waiting for two at its
120x40 over the person after it, and one alone at its `-size`. Fix: test
`!m.sessionRoster[i].Bot` there in place of `report.Holder == 0`; it closes the
`firstPerson` bullet of [Settle what a bot's defeat leaves
standing](#settle-what-a-bots-defeat-leaves-standing).

### Drop the proof binding no dial passes

- Priority: P3, to review
- Affected files: `internal/network/admission.go`, `internal/network/session.go`,
  `internal/app/internal/membership/relay.go`, `tool/vif-router/tcp_proxy.go`,
  `internal/network/admission_test.go`

`Key.Prove`, `SignProof`, `VerifyProof` and `VerifyProofs` (`admission.go:118-248`)
take a `binding` every caller leaves nil (`session.go:921`, `:982`, `relay.go:687`,
`tcp_proxy.go:199`), as every dial proves unbound (auth design §3.2); only tests
bind. Drop it, sign `signable("proof", []byte(nonce))` and bump `ProtocolVersion`;
drop the two bound test cases, and the binding from §3.1 and the term index's
*Admission* entry.

### Hold the generated snapshot code's comments to five lines

- Priority: P3, to review
- Affected files: `internal/gen-manifest/main.go`, `internal/engine/snapshot_world_gen.go`,
  `internal/engine/snapshot_pages_gen.go`

The generator's templates write comment blocks of six to 23 lines into the snapshot
code (`ReconcileSharedWorld`, `gen-manifest/main.go:1272`; `SharedWorldDelta`, :1201;
`CaptureSharedWorld`, :1066; `DiffSharedWorld`, :1220; `clearSharedEntities`, :1348;
`applyStoreRows`, :1470), past AGENTS.md's five. Condense each template comment to
five lines and run `go generate ./internal/manifest`.

## Cave

The side-scroller's refinements after its first play test (2026-10-27) lead,
grouped by area so an agent can take a group. What is left is the machine
follow-ups and what the position package ([Position](position-package.md)) leaves
open. A cave's shape stays in `pkg/cave` and its scenario, the rock behind
`Positions.Rock()` and the wall system and renderers that read it.

### Machine follow-ups

What `cave` met in the machine and is still open.

#### Give a program's entrance an entering state

- Priority: P2
- Affected files: `internal/behavior/program.go`, `internal/system/behavior.go`
  (`enter`), `internal/component/behavior.go` (`BehaviorEntrance`)
- Guide: [Behavior machines §4.4](machine-design.md#44-spawns-that-ride-their-materializes-and-spirits)

A prototype's `entrance = "materialize"` plays its own materialize style over the
footprint for its `entrance_ms`, the style's whole when that is 0, and lands the body
as it ends, beside any rock that came over it, and nothing of it reaches the machine,
which starts in `initial` the tick the body stands. Lixen expects entrances to grow for machines (2026-10-27).
What a program would declare next:

- Whether a countdown shows over the materialize.
- An entering state the machine runs while the materialize holds, so a program can
  telegraph, re-aim or cancel its arrival; while it runs the body neither strikes
  nor takes damage, as no body stands yet.
- A landing its probe chooses (`FindLanding` or its own) as the materialize ends,
  in place of the nearest clear footprint.

The materialize is Shared, so the entering state's clock and probe must run alike
on every instance and a capture mid-materialize must restore them
(`snapshot.Schema`).

### Position, blocking and navigation

What remains stands on its own.

#### Play a volume: the third axis's remaining foundation

- Priority: P3, not now: it needs other foundation changes first
- Affected files: `internal/engine/position.go`, `internal/app/internal/config`,
  `internal/snapshot`, `internal/system/navigation.go`, `pkg/navigation`,
  `internal/render/renderer`, `pkg/vmath`, `pkg/position/surface.go`
- Guide: [Position §3](position-package.md#3-the-volume),
  [Rendering §2](rendering.md#2-render-context-and-coordinates)

What a game in depth still needs:

- The binding and the records: a depth in the config the binding resizes to
  (`Position.Resize` passes one layer), and a row that carries its layer, which
  moves the journal, the captures and the digest, a row being a flat `Point`.
- Navigation: a footprint with depth and a passability per layer, where one
  flat grid is derived from `BlockBits`.
- Rendering: a flat renderer reading the layer it looks at, and a render in
  depth ordering overlap by a depth buffer over every layer projected
  (`sandbox/3d`).
- The forms left flat for want of a use: the cell strip `Ray` and `Octants`,
  the arcs on a sphere, a depth in `vmath.Aspect`; the searches that cross
  layers are [their own item](#placement-searches-that-cross-a-volumes-layers).
- A heightfield along the third axis: a height for each column and row, solid
  through the layers under or over it. A `Surface` a layer is a field over
  columns and layers only.
- The ceiling: `MaxCells` bounds a volume as it does a map (`ClampVolume`), 2 MB
  of cells, 100×50×25 or 200×60 ten layers deep. A game in depth raises the one
  constant, and past what stays dense needs cells chunked on demand.

#### An index typed by its coordinate, `Index[E, P]`

- Priority: P3, to evaluate when flat and volume games are to run on the engine together
- Affected files: `pkg/position`, `internal/engine/position.go`,
  `internal/engine/world.go`, every call site of a query
- Guide: [Position §3](position-package.md#3-the-volume)

The choice made: one `Index[E]` whose depth is set at `Resize`, each query a
generic method taking a `Point`, its cell on the first layer, or a `Point3`, the
two told apart by size at compile time, at no cost to a probe.

What the other form does: the point is the index's second type parameter, a flat
map an `Index[E, Point]` and a volume an `Index[E, Point3]`. Its methods are plain
ones taking `P`, the block log is in `P`, and the compiler refuses a `Point3` on
a flat map.

What it entails: the engine's store embeds the index and every game shares one
`World`, so `P` is one type for the whole binary. A volume game beside flat ones
then needs a `World` generic over `P`, or a second store, or `Point3`
throughout: every game's rows, log and captures carrying a Z, and every flat
call site, the 155 the conversion rewrote among them, passing a `Point3`. Go
derives no area or footprint type from `P`, so those queries stay split by name
or stay generic methods.

Why it was not chosen: with one `World` for every game, a flat system passes the
`Point` its rows hold and a volume's a `Point3` to the same index, a flat row
stays two ints, and a volume comes up a system at a time. The form taken pays
for that with generic methods, new in Go 1.27, which no interface can hold and
tools may trail, a type parameter on every signature, and nothing at compile
time between a `Point3` and a flat map. A probe costs the same under both.

Evaluate it when a flat game and a volume game run on the engine in one build:
if each then has a world of its own type, the point belongs on the index.

#### Placement searches that cross a volume's layers

- Priority: P3, when a game in a volume needs a place found off its origin's layer
- Affected files: `pkg/position/search.go`, `pkg/vmath` (`Aspect`, the diagonal scale)
- Guide: [Position §3](position-package.md#3-the-volume)

The spiral, the pattern search and the exclusion search each work the layer
their origin names, a body with depth tested through the layers it spans, and so
does the directional scan. What a layer means, a story or a slice of depth, is
the game's, so none was guessed. A form of all three that crosses layers
searches 26 directions where a layer has 8, and has to settle: the order the
layers are tried in, a depth in `vmath.Aspect` beside the cell's width and
height, the scale of a corner diagonal (1/√3, beside the 1/√2 of
`DiagonalNum`/`DiagonalDen`), what clockwise means off the plane, whether the
pattern's area gains a depth, and the slots before and behind an exclusion that
has one. It goes beside the layer forms and changes no caller of them.

#### Trim td's storm ticks

- Priority: P3
- Affected files: `internal/system/navigation.go`, `internal/system/storm.go`,
  `pkg/navigation/flowfield.go`
- Guide: [Navigation §10](navigation-design.md#10-costs)

With eight cursors walking and a storm raised, a td storm tick averages 1.9 ms
and its worst is 12–15 ms of the 50 ms tick (Navigation §10), the snake lock's
reaches stopping at their choice. Walls going are no peak: 235 at once cost the
wall and death events 0.3–2 ms, and a storm left alone takes 24 a tick at most.
What the slowest hundredth of the storm's ticks holds, each measured there:

- Fields, 43%: three fifths point fields, the private fields of the cursors the
  snake heads lock and the drops follow, recomputed at the cooldown as each
  cursor walks; two fifths the eyes' composite fields. Settling a field only out
  to its farthest reader reopens a decision taken on the `cave` scenario's numbers
  (Navigation §12).
- The eyes' own update, 24%, and the reaches, 21%.
- A storm circle's X and Y checks each ask for the walls they meet, so a wall in
  both footprints is asked for twice: twice the requests where 235 go, whose wall
  event costs 0.1–1 ms.

A td reset is the one place the wall system costs tens of milliseconds, laying
the maze again (Trim what a tick spends outside the position index).

## Armada

Armada is a vertical scroller with no terrain: the drift carries its stars down
past the ship, its ranks stand overhead and its squadrons fly routes into circles
([Gameplay §10](gameplay.md#10-embedded-campaign-progression)). A terrain under
that scroll is what is left, likely the first scenario built on a painted map
([Authoring](#authoring)); its items follow the editor's foundation. The bots'
armada figures ([Bots](bots.md#the-shipped-bots)) want reading again on it.

### Scroll a terrain past armada

- Priority: P2
- Affected files: `wad/scenario/base/armada/`, `internal/system/wall.go` (the cave
  step), `pkg/position/surface.go`, `internal/engine/rock.go` (`RockField`),
  `internal/system/particle.go`, `internal/render/renderer/rock.go`,
  `internal/event/payload.go` (`CaveStartPayload`)
- Prerequisite: [Lay a painted map a scenario carries](#lay-a-painted-map-a-scenario-carries),
  for the first form
- Guide: [Gameplay §9](gameplay.md#9-walls-maps-navigation-and-environment),
  [Position §1](position-package.md#1-the-boundary)

The map comes down past the ship as its stars do, with the ranks and squadrons in
it. The game has one scroll, the cave's: a profile of columns shifted left a column
at a time (`ShiftRock`), which pushes what it meets left, crushes a cursor against
the left edge and carries particles left. Given a direction and a source, that one
scroll is each form a terrain may take:

- **A painted map, likely the first.** The map's rows enter at the top as the
  scroll steps. Its tiles are a third kind of surface, blocks laid from the map's
  record as rock is from the cave's (Position §1 leaves a *shape* of that kind
  unbuilt), where walls as entities would move every one in view each step. What
  a row carries beside walls, a program's spawn or a loot, is raised as it enters.
- **Procedural rock.** The cave on its side, endless: `pkg/cave`'s profile is a
  function of an index and serves rows as it serves columns.

What the direction needs, whatever the source: a surface of widths a row with a
solid side, where `pkg/position` holds heights a column (`Above`, `Below`) and
shifts a column; the cave step's push, its crush edge (`EventCursorCrushed`, here
the bottom) and the particle carry along rows; the rock renderer; and names, the
events and `CaveResource` being the cave's.

To settle with the design:

- The painted map's other route, a view scrolling over a map laid whole. It is a
  second scroll beside the cave's, and every reader of the map's edges (a
  cursor's bounds, a spawn search, the glyph spawner, a bounce, the camera) then
  needs a window beside the map.
- The pace: the rock steps whole cells, and a row is twice a column's height on
  screen, so rock at the drift's pace in rows reads twice as fast, and a 40-row map
  shows a third of what a 120-column side-scroller does ahead (`[map] min_height`).
- Whether the ranks keep their place on the screen, as now, or pass with the map,
  and how a squadron's routes, fractions of the map, meet the rock.

[Gameplay §10](gameplay.md#10-embedded-campaign-progression) and the soak test
follow the terrain.

## Magic

Magic holds a magic square under its cursor's shield against the gateways on its
map's corners ([Gameplay §10](gameplay.md#10-embedded-campaign-progression)). Its
numbers are what a play test settles.

### Tune magic after its sixth play test

- Priority: P1
- Affected files: `wad/scenario/base/magic/scenario.toml`,
  `internal/parameter/square.go`, `internal/component/square.go` (`SquareKinds`)

To settle: the shield's count and what a touch costs the blocks, the eight ranks'
hit points and speeds, the paces, how a rank and a round quicken them and the waves
a round adds, each kind's duration and cooldown, the tempest's carry and the gust's
throw, the carrier's weapons, what charges the dust meter and the burst meter,
what the dust's blast does, and the spell's wait, hold and boost and the confuse's
kick. The headless survival table Gameplay §10 carried went
with strikes by turn across the map: a probe's figures under strikes each cooldown
within reach, and the middle lines firing, replace it.

The options Lixen did not take (2026-11-25), each with what would reopen it:

- An attack striking once per line formed, struck again only by breaking and
  forming the line, two turns a strike: the hardest of the three. Reopen if strikes
  each cooldown within reach prove too easy.
- The attack's repeat with a longer cooldown, about 5 s: no key keeps it striking,
  so the late game's pressure returns only in part. Reopen with the one above.
- A burst fired at any charge, its death wave reaching that share of the map:
  ends sitting on a full meter, at the cost of a reach on the death wave (its event
  and the pulse). Reopen if a full meter still waits unused in play.
- A burst fired by the next defense an edge fires, Enter dropped: one input on every
  host, but not on demand, and the dust the defense sheds goes with nothing left to
  blast. Reopen if Enter or its meter press reads badly on a phone.

### Hold magic's length whatever the map's size

- Priority: P2
- Affected files: `wad/scenario/base/magic/scenario.toml` (`Gate`, the ranks)

A body's walk from a corner grows with the map, and the weapons and strikes have
that much longer over it: a probe turning once a second, under strikes by turn
across the map, stood 728 s at 80 by 40 and past 30 minutes untouched at 200 by 60.
Gateways a fixed walk from the square, or a pace by the map's size, closes it.

### Leave magic's dust to the player's key

- Priority: P2
- Affected files: `internal/mode/router.go` (`ProcessInputTick`),
  `internal/system/particle.go` (`detonateDust`)

Under auto-fire `on`, the default, special fire detonates the dust a defense
sheds as it is shed, so only a player on `:auto main` times the blast. A scenario's
say over auto-fire's special, or dust the automatic special passes over, closes it.

## Devourer

### Let a bot see the Devourer

- Priority: P2
- Affected files: `internal/bot/catalog.go`

A bot's catalog holds no kind for the Devourer, its guts or its omens, so a bot
flies into its walls, which `default` meets about four minutes into every campaign
game. Kinds `devourer` and `gut` by their nearest covered cell, and
`landing` reading an omen's ray as it reads a Kraken's charge, would close it.

### Check a composite's members only when one went

- Priority: P3
- Affected files: `internal/engine/store.go`, `internal/system/composite.go`

`CompositeSystem.syncMembers` asks the member store about every member of every
composite each tick, 35 µs a tick while a Devourer's 8,000 stand at 200x60. A
count of removals on the store, read before the pass, would skip it on a tick
none went.

## Bots

What ships, and what was tried and not built, is in [Bots](bots.md). Decisions
of Lixen's that bind, as they stood on 2026-11-20:

- No trainer for the embedded scenario or `armada`: a typing bot cannot lose
  there, and armada's design is not final.
- `cave`'s trainer is stopped. It found one model, which ships.
- The embedded scenario carries no bot of its own: `default` plays it.
- The special attack is a network's output in every bot of a scenario that has
  it, and main fire too where auto-fire is off.
- A bot plays the game a person plays: no mechanic, event handling or engine
  wire is made for it.
- Beside a person, one who is away included, a bot's defeat ends no game.
  Where every player is a bot it does.
- The learner's rules are frozen as built until a measured read says otherwise.
- Nothing learned so far is precious: every population may start over for a
  setup that scales to new scenarios.

### Settle what the learner leaves open

- Priority: P1
- Affected files: `internal/bot` (`bot.go`, `learner.go`, `catalog.go`), the
  shipped bot documents
- Guide: [Bots: Learning](bots.md#learning),
  [what was tried](bots.md#tried-and-not-built)

- **A member's stale mean.** A proved member that is neither fielded nor its
  rival is never flown again, so its mean stays that of the partners it once
  flew beside: one of `raid`'s held 83 rounds at 38,437 where its title held
  53,245, and the two held out level
  ([the read](bots.md#learning)). Owed: a repeat that reaches every proved
  member in turn, or a mean over a member's latest rounds, read as an arm.
- **Firing.** `default`'s one written weight asks for the special attack
  always, an action each, and no run has learned to ask less. No shipped bot
  plays with auto-fire off, so nothing has learned the output `fire`.
- **From nothing.** With no weight written `cave`'s archive stalls in its
  caves: after nine million ticks it fields 7,517 held out where `seed`'s hand
  weights field 27,614, its members near-copies that 24 rounds cannot tell
  apart. A rule that averages 32 rounds a step fields no more, and a lean
  `raid` under twice as much ([tried](bots.md#tried-and-not-built),
  [inputs](bots.md#networks-and-learning)). Still to try: that lean start
  widened by `import` once it reaches the Kraken, and a candidate judged on
  the 50 to 75 rounds that do tell them apart.
- **Scenarios.** `td` and `armada` carry no bot of their own, so `-bot` there
  seats `default`. `armada`'s waits for its design. `td`'s has its towers to
  hold, and no kind of the catalog sights a tower: a kind `tower` first.
- **An entry as built.** Once a ninth member is proved, the next newcomer
  enters on one round whatever it scored. It is one place of probation a
  proof, under what a run's fielded pair can show: owed a read as an arm by
  who enters and who leaves, off the learner's own events, before it is changed.

### Close what learning leaves open outside a solo run

- Priority: P3
- Affected files: `internal/app/script.go` (`learn`, `newGame`),
  `internal/bot/catalog.go` (`weight`)
- Guide: [Bots: Learning](bots.md#learning)

- A bot learns in a session only its cursor holds. Two bots or more in one
  do not: a round there ends at the first defeat. Nor does a guest's bot, alone
  in another's session, close a round at its limit, since only the host starts
  a game: it would have to ask its host, as a guest's machine asks for a
  transition.
- The source `kill` pays a code species' kill flat, whatever its hit points.
  Paid by the hit points its body is made with, at `default`'s 12 a point, a
  round of the embedded scenario pays five times as much for kills as for its
  Krakens ([measured](bots.md#tried-and-not-built)): a source of its own with
  a weight of its own, and `default`'s read again under it.

### Let a scenario expose values of its own to a bot

- Priority: P3
- Affected files: `internal/bot/catalog.go`, `internal/engine/resource.go`

A network reads the world, and a guard the bot's status rows, among them whether
a region of the scenario runs (`fsm.<region>.index`). Neither reads a state of
the scenario by name or a variable of its own, as `cave`'s `raid_rank`, though
`ScenarioResource` lends variables to behavior programs. An input over one needs
a scale its scenario declares, and a bot listing it plays only such scenarios.
Nor does a score pay for one.

### Draw a watched run's wait on its screen

- Priority: P3
- Affected files: `internal/app/presentation_terminal.go` (`showStartupStatus`),
  `internal/app/play.go` (`player.frame`)
- Guide: [Runtime §1.1](runtime.md#11-script-pacing-and-presentation)

A watched script or bot that hosts or joins waits in its lobby drawn at its
run's size, in the screen's top left corner: the wait's frame is laid out for
the simulated screen (`App.frame`), where each frame after it is laid out for
the viewer's. Lay the wait out as `player.frame` does.

### Train bot graphs offline

- Priority: P3
- Affected files: `internal/bot`, `lixenwraith/genetic`,
  `internal/system/nugget.go`

A solo `-headless` run already learns with a game on every core. Not built:
`lixenwraith/genetic`'s batch `Engine` scoring whole runs over a fixed matrix of
scenarios and seeds, and a graph's own constants as genes. No source pays a
nugget, since nothing pushes `EventNuggetCollected`: push it in
`NuggetSystem.collectNugget`, drop that function's own `statCollected`, and add
its row to `sources`.

### Teach a bot its player's style, for an auto-pilot

- Priority: P3, an aspiration for the last stages of the bot work
- Affected files: `internal/bot`, `internal/app` (`replay.go`, `loop.go`),
  `internal/engine/scheduler.go`

Wanted (Lixen, 2026-11-18), not to be built yet: a bot that learns to play as
its player does, and an auto-pilot that flies the player's ship until they take
it back. Decided 2026-11-20:

- The auto-pilot is the player's own cursor with its control handed to a
  trained bot, as one key's very long macro. The cursor stays the player's, so
  a defeat under the auto-pilot is the player's defeat.
- The learner watches the player and fits one set of weights: what a network's
  inputs read, and what the player then did, is one sample and one step of the
  network's gradient. It is no search over a population.

Owed, in order:

1. A reader of a person's play, on their own run: a `mind` that only reads
   gives a network's inputs, and the dispatch tap what they did, their
   `EventCursorMoveRequest` for `move` and `EventFireSpecialRequest` for
   `special`. Typing is the graph's and is not learned.
2. Samples from a journal first: a replay of a `-j` run under that reader, kept
   as rows of inputs and outputs, since a journal replays only on its build.
3. The fit: a row pairs the inputs of a few ticks back, the player's reaction
   time, with the action now, and rows are taken on a clock so standing still
   is learned. Several fits at once differ by lag, step or inputs, and the one
   with the least error on rows held back is kept; weights are not crossed.
4. A seam for a learner that is no population: `Driver.learners` holds
   `*Learner` and needs only its `take`, `settle` and `Stats`.
5. The style kept as a graph of the player's, written by `Graph.Document`.
6. The auto-pilot as a driver on the person's own run, since a seat cannot take
   a held slot: the tick left to the run, and a hand-over each way that resets
   the input machine, returns to Normal and lets go of held keys.

To decide then: what it does in a typed scenario, and whether the session's
others are told.

## Authoring

The map editor leads this section. Its foundation is the P1 items, in the order
they stand; the editor follows at P2, on a terminal and in the desktop window, with
[armada's terrain](#scroll-a-terrain-past-armada) its first scenario.
The rest wait.

### Settle what a bot's defeat leaves standing

- Priority: P3
- Affected files: `internal/system/meta.go`, `internal/system/network.go`,
  `internal/engine/world.go`, `internal/app/internal/membership/session.go`,
  `internal/status`, `wad/scenario/base/recall/scenario.toml`
- Guide: [Multiplayer §5.4](multi-player.md#54-wraiths)

A bot's defeat ends a game only where no person holds a place
(`World.DefeatDecides`), and beside a person, one away included, it comes back a
second of game time later. What that leaves:

- A dedicated host sizes its map by the first joiner that names no holder, a
  run a bot plays itself among them (`noteJoinerReport`'s `firstPerson`).
- No status key says which slots are bots', so a scenario's guard cannot ask.
- A defeat a bot's owner crossed inside its lead of the bot's return latches
  the bot again, and it comes back a second time (`NetworkSystem.settles`).
  Refuse a report produced before the slot's last spawn.
- `recall` arms every cursor again at any cursor's spawn, a bot's return among
  them (`PlayerActive`'s `EventCursorSpawned` transition).

### Describe a scenario document in the schema

- Priority: P1
- Affected files: `internal/manifest/schema.go`, `internal/event` (the registry
  generator), `internal/fsm/config.go`, `internal/resource/scenario.go`
  (`entryKeys`), `internal/behavior/program.go`
- Guide: [Behavior machines §5](machine-design.md#5-dialect-kernel)

The editor's forms and pickers come from one description, the one `-schema`
prints, and it holds too little for them: every event with its fields, the names
each enum takes, three vocabularies with their arguments and what a bot's network
reads and asks for. Missing:

- **An event's class and line.** Nothing says an event is local, shared or a
  crossing, or what it does, though its type's comment holds both, so nothing
  tells a picker which a scenario may emit or trigger on.
- **A field's shape.** A type is its Go spelling: a nested one
  (`[]event.MazeRoomSpec`, `component.WallVisualConfig`, `color.RGB`) is a name
  with no fields, a `time.Duration` hides that it takes nanoseconds, and nothing
  marks the pair that is a cell, the entity a capture fills, or the string naming
  one of the scenario's own (a program, a loot kind).
- **The document's shape.** The keys of the entry (`entryKeys` and the hand
  readers of `[map]`, `[content]` and `[navigation]`), of a region, a state, a
  transition and an action's modifiers, of a program and its `[prototype]`, and
  of a bot document's `[bot]` and `[network.<name>]`, each with its kind, as
  `dialect.Fields` gives an argument's.
- **The systems.** The names a scenario toggles and what each requires, which
  `-check` holds a scenario to and nothing lists.
- **The status rows.** What `StatusIntCompare` and `StatusToVar` may read lives in
  a built world's registry, some rows the scenario's own
  (`behavior.<program>.count`, `fsm.<region>.*`): `-schema -s <name>` would print
  them from a world built for that scenario and never run.

Each is a field more in the JSON under one `SchemaVersion`. The editor reads the
same description in the binary, never a table of its own.

### Name a load fault by its file and key

- Priority: P1
- Affected files: `internal/resource/check.go`, `internal/fsm/loader.go`,
  `file_loader.go`, `internal/behavior/compile.go`, `loot.go`
- Guide: [FSM §12](fsm-and-configuration.md#12-validation-rules-and-diagnostics)

A fault is a line of text naming its place in its stage's own words (`state 'A':
unknown key "on_entr"`, `program 'scarab': state 'Lock': on_update[0]: …`), joined
with its stage's others, and a state's fault names no file. `ValidateScenario`
stops at the first stage that fails, of the programs, the loot, the machine and
the systems, so an author clears one to meet the next. The editor has to land each
fault on the node or the cell that made it.

What closes it: one fault type, a file, a key path and a message, that every stage
returns and `-check` prints as it prints text today, and a validation that goes
on to every stage a failed one does not starve. A path counts an array's elements
as expanded, so an action past an `each` maps back to the element written by
expanding each written element alone, which asks nothing of the macro library.

### Lay a painted map a scenario carries

- Priority: P1
- Affected files: `internal/behavior/maps.go`, `internal/component/declared.go`
  (`MapLayout`), `internal/system/wall.go`, `internal/pattern`,
  `internal/resource/scenario.go` (the bounds)
- Guide: [FSM reference: Maps](fsm-reference.md#maps),
  [Content §8](content-assets-and-tools.md#8-image-and-pattern-pipeline)

A scenario's map lays a program's spawn from its legend and nothing else
(`EventMapLayRequest`), so a formation is drawn and terrain is not. A wall event
still takes its cells one by one, or lays a maze, a cave or an `.aimg` from the
`image/` category, which the digest leaves out and a joiner is not sent. What
remains, tiles first and [images after](#carry-an-image-in-the-scenario):

- **A tile.** A legend entry that is a wall: its glyph, colors, box style and
  block mask named once. `MapLayout` gains its cells beside `Spawns`, and the wall
  system lays them on the lay request as it lays a pattern, as one composite or as
  loose walls, a batch a block mask and box style. A 500 by 250 map is 125 KB of
  rows, half a file's bound.
- **Its own size.** A map is as large as its rows, and the scenario holds the map
  to it (`[map] fixed`, its map setup). A layout that stretches with the
  terminal is not this item's.
- **A loot, and a named cell** a payload takes, the marker the editor drags, each
  a kind of legend entry its own system lays.

Laying a large map costs what td's maze does at a reset, tens of milliseconds
([Engine](#trim-what-a-tick-spends-outside-the-position-index)).

### Write a scenario back as `vif.toml` is written

- Priority: P1
- Affected files: `internal/resource` (a scenario store beside `SaveDownload`),
  `internal/paths` (`OwnRoot`, `Shared`), `internal/app/internal/membership`
  (`RestartAsk`)
- Guide: [Filesystem §2](filesystem-layout.md#2-installed-configuration-tree),
  [§3](filesystem-layout.md#3-resolution-policy)

An edit goes into the author's file through `toml.Document`, as a setting goes
into `vif.toml` (`saveTable`), and the library writes only what then reads
otherwise: the editor changes the tree it read and sets it. Run against the
shipped scenarios, one action changed in the 40 lines of td's `TdSetup.on_enter`
changes that line alone, its 10 comments kept; an argument of an invocation is
set in place among the others; a state under a header has its keys set, added
and deleted; and a raider's `[[frames]]` keeps every table that reads the same.
What it leaves out is a name for one table of an array of tables: the array is
set. The rest is vif's:

- **Whose file.** A scenario in one of the player's roots is written in place
  (`paths.OwnRoot`). One in a shared root, the checkout's `wad/` or a system
  root, or among the downloads, is copied under a name of the author's into the
  user root's `scenario/` at the first change, as `SettingsFor` seeds a `vif.toml`.
- **Nothing the author did not say.** A store writes the edit and adds no
  default: a key left out stays out, and a scenario's `[keymap]` is never filled
  as a player's keymap is, only held to what this build reads.
- **Never stored broken.** What is stored validates, as `saveTable` reads its
  document again, and a save over several files lands whole or not at all, as
  `SaveDownload`'s does. To decide: what becomes of edits that do not validate
  yet.
- **A test play before a save.** `Options.Provided` already runs a scenario held
  in memory, a joiner's, but a restart names one by name
  (`RestartAsk.Scenario`): the ask carries the documents as edited.

### Read a block's form from its macro

- Priority: P1
- Affected files: `internal/fsm` (beside `file_loader.go`),
  `internal/manifest/schema.go`
- Prerequisite: [Describe a scenario document in the schema](#describe-a-scenario-document-in-the-schema)
- Design: [TOML macros](toml-macros.md)

The palette is the scenario's macro set and a block's form its parameters. The
macro library defines, expands and folds and lists nothing of a set, and need
not: a definition is plain data in the parsed document, its `params`, `defaults`
and `doc`, and its `base` and `with`, whose open parameters a walk of the bases
gives. What a form needs beyond that is derived here:

- **A parameter's kind.** A `string` may name a state, a region, a variable, an
  event or a species, and two `int`s a cell. Expanding the macro once over marker
  arguments shows where each lands in the document's shape: in a `target` it
  names a state and takes a picker and an edge of the graph, in a payload's cell
  a marker on the map. A name spelled from an argument (`"${theme}Fg"`) cannot be
  probed and stays a plain field.
- **A fault's block.** A state written as an invocation keeps its name, so a
  state's fault is its block's
  ([Name a load fault](#name-a-load-fault-by-its-file-and-key)).
- **Blocks from plain tables.** `Fold` writes back as invocations what a set's
  macros expand to, so a scenario written out by hand takes a library's blocks.

The library's part is wanted, not needed to start. A table has no order and a
definition refuses a key it does not know, so a form lists its fields by name
under the macro's one `doc`: a parameter's place and a line of its own are two
keys of a definition (P2). A block of several states is the root `[[expand]]`
[TOML macros §7](toml-macros.md#7-rejected-alternatives) defers (P3).

### Keep a block library a scenario copies from

- Priority: P2
- Affected files: `wad/block/`, `internal/resource/resolve.go`, `internal/paths`,
  `Makefile` (`install-config`)
- Design: [TOML macros §5](toml-macros.md#5-where-macros-live)

No macro comes with the binary, since a scenario must expand alike on two builds,
so a block a scenario did not write is copied into it. Nothing holds blocks to
copy: td's, cave's and the campaign's macro files are their scenarios' own, and
each scenario writes the common shapes again. What closes it:

- **A category in the roots**, `block/`, each file a macro file, shipped in
  `wad/block/` and resolved as every resource is.
- **A copy on first use.** The macro, and those its body or its base names, go
  into a macro file of the scenario that `macro_files` lists. A set refuses a
  name defined twice, so a copy meeting one of the scenario's is refused or
  renamed.
- **Its first blocks**, from what the shipped scenarios repeat: the player's
  lifecycle seven of them copy (boot, its retry, the defeat watch on
  `session.any_defeated`, the strobe and the reset), td's `SpawnAwait` and
  `Announce`, cave's `RaiderStates`, armada's `Ship` and its wave loop.

A copy never follows its library: a scenario plays its own bytes.

### Build the map and scenario editor

- Priority: P2
- Affected files: `internal/mode`, `internal/render/renderer`, `internal/resource`,
  `internal/fsm`, `doc/multi-platform.md`
- Prerequisite: the P1 items above
- Design: [TOML macros](toml-macros.md)

The editor writes ordinary scenarios: a directory under a root's `scenario/` that
loads, digests, transfers and lists as one written by hand, so nothing at run time
knows an editor made it. It brings no format and no table of its own: documents
are edited in place, blocks are macros, forms come from the schema and a fault
lands on what made it, each an item above.

- **In the game.** An operator's mode beside Command and Overlay
  ([Input §2](input-and-modes.md#2-modes)): it holds a solo game, is no part of
  the journal, and is refused in a live session and in a replay. Applying an edit
  validates and restarts on it as `:n` does, since a run plays one scenario.
- **The map is the canvas.** The held world draws as it plays. The cursor moves
  by Normal's motions, counts, marks and search, a selection is Visual's, and the
  mouse paints. A tile, a spawn and a marker are entries of the map's legend.
- **Forms are menu pages.** The palette, a block's form and the scenario's
  settings are pages of the [menu](config-menu.md), searched, drawn and
  navigated as it is.
- **The graph.** A scenario's regions, states and blocks as nodes and each
  `target` an edge, drawn through an overlay as `:graph` draws a route graph,
  read-only first.
- **Hosts.** A terminal and the desktop window first. Android and a browser
  refuse it with the sentence `unavailable` gives
  ([next item](#edit-on-android-and-in-a-browser)), a difference
  `doc/multi-platform.md` records.

In order: the read-only graph; placing and editing blocks; painting a map and its
markers; the test play; library copies.

### Edit on Android and in a browser

- Priority: P2
- Affected files: `internal/resource`, `cmd/vif/browser.go`, `web/vif.js`,
  lixenwraith/terminal (`web/page.js`, the gestures), `android/app`,
  `doc/multi-platform.md`
- Prerequisite: [Build the map and scenario editor](#build-the-map-and-scenario-editor)

A browser has no root to write a scenario in, and its storage is the few MiB the
settings and the places share; Android's user root is in the app's files, out of
its player's reach. Both need the store the downloads ask for
([Manage the downloads](#manage-the-downloads-from-the-menu-and-the-command-line)),
keyed by name for an author's own; a scenario file taken in and handed out, the
`.vifscen` of [Fetch a scenario by its digest](#fetch-a-scenario-by-its-digest); and
the gestures a canvas takes of a touch screen, a drag that paints and two fingers
that pan.

### Carry an image in the scenario

- Priority: P3
- Affected files: `internal/resource/scenario.go` (`validScenarioName`, the
  bounds), `internal/service/adapter_file.go`, `internal/system/wall.go`
- Prerequisite: [Lay a painted map a scenario carries](#lay-a-painted-map-a-scenario-carries)

An `.aimg` or `.aimgz` the entry names joins the scenario's files, under its
bounds and its digest, so a backdrop reaches a joiner and a browser as its maps
do. The container admits `.toml` alone, `WallPatternSpawnRequest` opens a name
below a root's `image/` through the file service, and an image's block mask is
its request's, one for every cell.

### Give the vim keys what their second round left

- Priority: P3
- Affected files: `internal/input/machine.go`, `actionnames.go`,
  `internal/mode/operators.go`, `router.go`
- Guide: [Input and modes §3](input-and-modes.md#3-normal-mode-grammar),
  [§10](input-and-modes.md#10-keymap-override-files)

A vim key whose letter vif reads its own way has an action no default key binds,
which the vim keymap (`-k vim`) and the tutorial attach. What is left:

- **What the vim keymap cannot give back.** `H` and `L` go to the map's top and
  bottom rows where vim's go to the window's, and `J`, `K`, Ctrl-F, Ctrl-B, Ctrl-E
  and Ctrl-Y have no action: a join, a lookup, a whole page and a scrolled view.
  Enter, Space, Backspace, Tab, `[`, `]` and `gh gj gk gl` stay the game's.

- **Named registers.** `"a` before a yank, a delete or a put would name one of 26
  registers beside the unnamed one, sharing the macros' letters but not their
  store.
- **A count on undo and redo.** Each press takes back one change: a second undo
  in the same press reads cells the first has not restored yet, so a count needs
  the settle between them that `.` gets for its Insert text (`Machine.Next`).
- **Visual's other keys.** `x`, `X`, `D`, `C`, `s`, `S` and the puts act there as
  in Normal; vim's take the selection, `p` replacing it.
- **Stacked glyphs.** The register and an undo keep the glyph drawn in a cell,
  where a delete also takes the layers under it
  ([below](#decide-what-a-delete-removes-under-a-drift-stack)).

### Grow the tutorial

- Priority: P3
- Affected files: `wad/scenario/base/tutorial/`
- Guide: [Tutorial §3](tutorial.md#3-the-next-round)

The next round of lessons ([Tutorial §2](tutorial.md#2-what-it-holds) lists
what runs) adds `[ ]`, the markers and macros, practices the keys it only tells
(`N`, `M`, `C`, `X`, `P`), lays a lesson again on a resize, centers from the
map's edge (`origin`, [FSM reference](fsm-reference.md#event-emission)),
and cues a lesson cleared.

## Browser sessions

### Fetch a scenario by its digest

- Priority: P2
- Affected files: `internal/resource`, `internal/mode` (the Scenarios page),
  `web/vif.js`, `Makefile` (`wasm`); on the site, `make vif` and the lobby
- Guide: [Multi-platform §7](multi-platform.md#7-external-maps-and-assets)

`-s` reads a scenario from a web link to its directory, a fetch a file (36 for
`cave`), and checks nothing against a digest; a page's menu lists the embedded
`fighter` alone, as a page cannot list the site's directory. What closes it:

- One file a scenario, `.vifscen`, its canonical form, which `make wasm` writes
  beside an index (name, description, players, digest) and the site publishes
  under its digest: one fetch, held to the digest as a transfer is. The index,
  named to the launcher as `data-scenarios`, is the Scenarios page's list and
  what `-s td` resolves through in a page.
- To decide: a visitor's own scenario, picked or dropped on the start screen or
  carried in a link's fragment, and whether a stranger's may run; a native run's
  link to another origin, and a publisher's signature for it; the extension, whose
  page reaches the site's files only through the site's CORS or a host permission.

### Manage the downloads from the menu and the command line

- Priority: P2
- Affected files: `internal/mode` (`config_menu.go`, `commands.go`),
  `internal/resource` (`SaveDownload`), `cmd/vif/browser.go`, `web/vif.js`
- Guide: [Filesystem layout §2](filesystem-layout.md#2-installed-configuration-tree)

`[session] save_downloads` keeps every scenario a host sends in
`scenario/downloads/`, and only a file manager removes one, so a player who joins
many sessions collects them, up to 1 MiB each. The Scenarios page would list the
downloads apart from the player's own, with size and age, and remove one or all
behind a confirm, as a command would (`:downloads`, `:downloads rm <name>`); a
Settings row would turn saving on where `vif.toml` is out of reach, as on Android.
A browser saves none: a page has only the site's storage, a few MiB that the
settings and places share, so saving there waits on these controls, a cap in
bytes that evicts the oldest, and the digest as the key (IndexedDB). A save cut
off mid-write leaves a hidden `.<name>.*` directory beside them for the same
clean-up.

### Launch the page and Android without their start screen

- Priority: P3
- Affected files: `web/vif.js`, `cmd/vif/browser.go`,
  `android/app/src/main/java/com/lixen/vif/StartScreen.java`

Both open on their start screen whatever `[play] wait` says. The page's is up
before its program runs, so it cannot read the setting in time, and its press is
also the gesture a browser needs before it plays sound; Android's could close at
once, but would flash. Running the page's program behind its screen, as Android's
runs behind its own (`Host.Away`), would let `wait = false` skip both.

### Join a pasted game link from the Android app

- Priority: P2
- Affected files: `android/app/src/main/java/com/lixen/vif/StartScreen.java`,
  `MainActivity.java`, `android/game/game.go`

The site hands out a game's link in three shapes: the terminal command (`vif -join
vifs://lixen.com:7777/<game>`), the bare `vifs://` link, and the browser's
`https://lixen.com/vif/wasm/?arg=-join=wss://…`. The app takes none of them short
of typing `:join` and the link on the soft keyboard. A Join button on its start
screen could read the clipboard (or a field to paste into), strip a leading `vif
-join` and the browser link's `?arg=-join=` wrapper, keep the `vifs://` or `wss://`
link and join it as `:join` does; a `vifs://` intent filter would open links
tapped elsewhere. Unchecked without an emulator or a phone, so not built yet.

### Warn in the site's lobby when a session runs another build

- Priority: P2
- Affected files: the Hugo site's `vif-lobby.js`, `.github/workflows/release.yml`

Each row of the router's session list names the build its session runs
(`state.build`), as the refusal a page of another build reads does. The site's
lobby can show it, and warn before a Play that would be refused once it knows the
page's own build, which the release's web bundle does not name yet.

### Sign in from the extension

- Priority: P2
- Affected files: `internal/app/internal/membership/host.go` (`Login`),
  `internal/network/session.go` (a sign-in is shown over TLS alone, and a
  page's `wss://` socket reads as none), `cmd/vif/browser.go` and `web/vif.js`
  (a kept document for the token, as `places`)

A player signed in at the site joins from the Chrome extension unnamed: the
router reads the sign-in cookie from the site's origin alone, and Chrome sends
none on the extension's upgrade, so the ticket names the address, with no
account, no attested name and no account join budget. Once the site's
permission is held, `:register` there makes the account; `:login` reads `Signed
in to lixen.com as <name>` and sets the site's cookie in the browser, which
signs in the site's own pages and charges the extension's creates to the
account, and seats nobody by name; `:logout` signs those pages out too. Closes
with a page that is not its site's signing in as a terminal does: the token kept
in the page's storage, as its places are, and shown in the admission exchange
over `wss://`, which the router takes from any origin.

### List the site's games in the extension

- Priority: P2
- Affected files: `deploy/package/chrome/` (a page and its script), `Makefile`
  (`extension`)

The extension opens on a solo start screen, and a game at the site is reached by
typing `:join` or through the menu, so its share button, which shows only behind
a game the page's address names, never shows. A page of the extension's own
listing the site's games (`GET /vif/api/sessions`, behind the permission the
first join asks for), each row opening `index.html?arg=-join=<ws_url>`, gives it
the site's lobby. Open: its own page or rows on the start screen, as
[Android's](#offer-lixencom-games-on-the-start-screen); hosting from it.

### Open the extension in Chrome's side panel

- Priority: P3
- Affected files: `deploy/package/chrome/manifest.json`, `background.js`

The toolbar button opens a tab. Chrome's side panel (`side_panel` and the
`sidePanel` permission, Chrome 114) would hold the same page beside another, at a
phone's width. Open before it is built: whether the map reads at 360 px on a
desktop's cell, which entry the button takes, and the store's rule that a side
panel serve the page beside it, which a game does not.

### Word a failed socket by where the page stands

- Priority: P3
- Affected files: `pkg/network/websocket_wasm.go` (`connectionFailed`)

In the Chrome extension the page's host is its id, so every `wss://` host is
another's, and a join the site's own route refused ends with the hint for a
player's self-signed door. Closes with the hint kept from a route its site lists,
which the lookup that follows a failed dial already knows.

### Keep the menu off the start screen after two quick Backs

- Priority: P3
- Affected files: `web/vif.js` (`showAway`, `applyMenu`)

The first Back's Escape opens the menu a frame after the second Back looked for
one to close, so the menu's window stands over the start screen. Closes with
`applyMenu` closing a menu that arrives while the page is away.

## Packaging

Distribution-specific detail lives in [Packaging](packaging.md).

### Promote a nightly build to the first stable release

- Priority: P1
- Affected files: `doc/packaging.md`, `README.md`
- Prerequisite: choose a nightly commit after the complete verification gate
  passes

Tag the selected commit `v0.1.0` and publish the draft release the tag creates,
which already carries the source archive and its checksum; then drop the "no final
release" lines in `README.md` and [Packaging](packaging.md) §2.

### Rewrite the history without the third-party names and files

- Priority: P1
- Affected files: every commit
- Prerequisite: the names PR merged and no other PR open

The names audit (2026-11-14) took third-party game, brand and song names out of
the tree, replaced a downloaded picture (`wad/image/test.aimg`) and deleted a
stray 13.5 MB binary (`vif-router`). Older trees, commit messages and squash
titles still carry them. One `git filter-repo` pass takes them out of every
branch and tag:
`--strip-blobs-with-ids` with the binary's blob
`d6c2405eab3fc7ddc344b63a0513af09ebf1703d` and the old picture's
`dfede4b85aa34b34e968bba8b73180e11876cfed`, `--invert-paths` for the deleted
plan doc, `--path-rename` for each renamed scenario, test and doc, since a text
replacement renames no path, and `--replace-text` with `--replace-message`, from
the list of names and paths kept in the project's files, outside the repository,
since it spells them. What it costs, and what it does not reach:

- Every hash changes: each clone and fork re-clones, open branches are rebased
  by hand, signed commits lose their signatures, and each release tag is set
  again on its rewritten commit; release assets already uploaded keep the old
  files.
- vif has no version tags, only `nightly`, so no published version's checksum
  is at stake, but proxy.golang.org keeps any pseudo-version someone fetched as
  it was, the binary and the names with it.
- GitHub keeps every PR's `refs/pull/<n>/head`, which no push deletes, so the
  old commits stay reachable there and by hash in cached views. GitHub Support
  removes only sensitive data that rotating a credential cannot make safe, so it
  will not purge them. PR titles and bodies are edited on GitHub by hand.

### Put the font's license beside the page

- Priority: P2
- Affected files: `Makefile` (`page`), `.gitignore`

`make wasm` writes `NOTICES` beside `vif.wasm` but no `LICENSE.font`, which, of
the page's copies, only the release workflow's web archive and the extension
carry, so a deployment of `web/` serves Hack's web font, inside `vif.wasm`,
without its license beside it: only the font's own name table holds the text.
Closes with `page` installing the terminal module's `atlas/LICENSE` as
`$(PAGE_DIR)/LICENSE.font`, which `.gitignore` takes as it takes the page's
scripts.

### Wrap and scroll the About window

- Priority: P2
- Affected files: `internal/render/renderer/overlay.go`

The About window neither wraps a value nor scrolls, so at 80x24 it stops above
its `license` and `notices` rows; once it does both it can name what the build
links, from its build info.

### Sign the Android build with a release key

- Priority: P2
- Affected files: `.github/workflows/release.yml`, `android/app/build.gradle.kts`,
  `android/README.md`

The nightly and a tagged build are signed with `android/debug.keystore`, which
anyone can sign with, so a build from elsewhere installs over them. Before a
release a device should trust: generate a release keystore (`keytool -genkeypair
-keyalg RSA -keysize 4096 -validity 10000`), add it base64-encoded as
`ANDROID_KEYSTORE_BASE64` with its passwords and alias as the three other secrets
the workflow reads, and keep the keystore out of the repository; a key once
published cannot be rotated for installed apps. The first signed build will not
install over a debug-signed one: uninstall first, which drops the app's settings.
A store listing (Play wants an AAB and a `targetSdk` within a year of the newest
release) is a separate decision.

### Run the Windows window build on Windows

- Priority: P1
- Affected files: `cmd/vif/gui_windows.go`, `cmd/vif/wasapi.go`, the terminal
  module's `window/window_windows.go`
- Guide: [Build profiles](multi-platform.md) §4 and §8

The Windows zip and Steam depot were built and played under Wine 9.0 alone, which
is not Windows where it matters: one display scale, its own GDI and mixer
timings, a console every program inherits. Closes with one run on Windows 10 and
11, what it finds becoming its own item: the zip opened from Explorer and from a
console (`-version` prints there, `-cli` shows its box only from Explorer),
sound through speakers and through Bluetooth, a default device switched and
unplugged mid-game, a display at 125 and 150 %, a second monitor of another
scale, the terminal module's own Windows checks (its `doc/todo.md`), and a
launch from the Steam client with its overlay.

### Sign the Windows binary

- Priority: P2
- Affected files: `.github/workflows/release.yml`

`bin\vif.exe` is unsigned, so SmartScreen asks before a downloaded zip's first
run; a Steam install is not asked. Closes with the owner's decision on a code
signing certificate and, with one, a signing step for the zip's and the depot's
binary whose key the repository never holds.

### Build the Windows window for arm64

- Priority: P3
- Affected files: `Makefile` (`WINDOW_PACKAGE`), `.github/workflows/release.yml`

The window build cross-compiles for windows/arm64 and is neither released nor
run; an arm64 Windows machine runs the amd64 zip emulated. Closes with a run on
one and a second zip.

### Bind the Steamworks SDK behind a tag of its own

- Priority: P2
- Affected files: a new binding package, `cmd/vif`, `Makefile` (`steam-linux`,
  `verify`), `doc/packaging.md` §9
- Prerequisite: Lixen's go, and a design for controller glyphs in the help and
  prompts, which every host shares

The depot links nothing of Valve's, so a Steam build has no rich presence, no
join from a friend, no achievements, and on a Deck no text entry or glyphs, which
Verified wants. Closes with a flat-API binding of vif's own under `vif_steam`,
passed by the depot targets alone, with `make verify` holding that no other
build lists it:

- `SteamAPI_InitFlat`, `RunCallbacks` and `Shutdown`; `ISteamFriends` rich
  presence; `ISteamUtils` `ShowFloatingGamepadTextInput`; `ISteamUserStats`
  achievements; later `ISteamInput` action sets and glyphs. It loads
  `libsteam_api.so` beside the binary with purego, and on Windows
  `steam_api64.dll`.
- A friend's join reads rich presence's `connect`, which holds a router session
  link, never a player's own door address, and maps to `-join`.
- The floating text input opens on entering Insert or Command mode in Big
  Picture or on a Deck; achievements come from scenario events.

Not go-steamworks, whose repository carries Valve's binaries: none of Valve's
headers or binaries enters this repository or a GitHub archive, only a depot. No
`SteamAPI_RestartAppIfNecessary`, and no DRM.

### Build the Steam depot for linux/arm64

- Priority: P3
- Affected files: `Makefile` (`steam-linux`), `deploy/package/steam/`

Steam Linux Runtime 4.0 publishes an arm64 platform, and the depot is amd64
alone. Closes with `steam-linux` assembling an arm64 depot beside it once the
partner site takes a native arm64 Linux depot.

### Keep the window's zoom on its machine under Steam Cloud

- Priority: P3
- Affected files: `internal/paths` (`SaveCell`), `doc/packaging.md` §9

Auto-Cloud syncs `vif.toml` whole, so the zoom a player left on a Steam Deck
(`[presentation] cell`) opens their desktop at it. Closes with the cell kept in
the state root, read over `vif.toml`'s.

### Have vif.nvim install and update the game

- Priority: P2
- Affected files: `vif.nvim` (`lua/vif/init.lua`, `lua/vif/health.lua`,
  `README.md`, `doc/vif.txt`), `doc/packaging.md` §10
- Guide: [Packaging §10](packaging.md#10-neovim)

The plugin runs the `vif` it finds and brings none, so on a machine without one
`:Vif` only says so. Closes with these in the plugin:

- **The first launch installs.** `:Vif` with no `vif` on `PATH` and no `bin` in
  `setup` fetches the game, then starts it, and `:Vif install` does the same
  when asked. A first argument that is a bare word is the plugin's, every
  argument of the game's starting with `-`. Whether that first fetch asks
  before it downloads, as `vim.pack` asks before it installs a plugin, is the
  owner's to decide.
- **`:Vif update`** replaces the game the plugin installed with the newest build
  and says the version it had and the one it has. Nothing looks for a newer one
  unasked, at start-up or at `:Vif`. A `vif` on `PATH` or named in `setup`
  stays first and is never replaced: `:Vif update` then says whose it is.
- **What it fetches**: the release archive of the machine's system and
  architecture, of the newest tagged release, or of the nightly where there is
  none or `setup` asks for it, checked against that release's `SHA256SUMS`
  before it is unpacked. It goes under `stdpath('data')` as a `bin/` with the
  wad archive's files as the `wad/` beside it, where the binary finds them
  ([Filesystem layout §3](filesystem-layout.md#3-resolution-policy)), so every
  scenario comes with it. Where no archive is built for the machine, as on
  macOS, it says so and names `go install`. `:checkhealth vif` names the binary
  in use, where it came from and its version.
- **The free mouse.** The pointer moving with no button held does not reach the
  game in a Neovim terminal, so the game's cursor does not follow it; Neovim
  passes it on only under `'mousemoveevent'`. Either the plugin sets the option
  while the game's window is the current one and puts it back after, or its
  README and help say so where a player looks first.
- **The install snippets, in place.** The README shows where each goes: a
  minimal `init.lua` for lazy.nvim and one for `vim.pack`, each whole with its
  `setup` call, where it now gives a bare line a visitor has to place.

### Host the game in Neovim natively

- Priority: P3
- Affected files: a presentation beside `terminal/window` and the page
  (`app.Host`), `vif.nvim`
- Guide: [Build profiles §8](multi-platform.md#8-adding-another-renderer-or-android-host)

`vif.nvim` plays the terminal build in a terminal buffer, which every Neovim UI
draws. A native host would have vif speak msgpack-RPC to `$NVIM` as a
presentation, behind a tag of its own: each frame's changed cells written to a
buffer with extmark highlights, keys read through `vim.on_key`, the mouse through
maps. One or two weeks, for little: Neovim has no key release to hand a plugin
either, and the terminal buffer already shows every frame the game changes (20.5
of 20.5 a second). Worth it only if Neovim's terminal becomes the limit.

### Tell a Neovim player their keys tap

- Priority: P3
- Affected files: the terminal module's `keyprotocol.go`, or Neovim's
  `src/nvim/vterm/state.c`
- Guide: [Input and modes](input-and-modes.md) §1

Neovim's terminal (0.11.7, 0.12.6) answers the key protocol's query with every
flag pushed (`CSI ? 31 u`) and implements `disambiguate` alone, so the module
reports releases where a text key taps and Escape, an arrow or an F-key stays held
until its next press (`KeyHolds.Report`), but `EventInputReport` says `releases`
and the notice a `releases = true` capture owes never shows.
Closes with Neovim's query naming only the flags its terminal acts on, reported
upstream; failing that, the module declining the protocol where `$NVIM` is set,
which goes by the environment where it now goes by answers.

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vif-git` alongside the release-based AUR package.

### Give the Windows terminal build sound

- Priority: P3
- Affected files: `Makefile` (`windows`), `cmd/vif/wasapi.go`
- Guide: [Build profiles](multi-platform.md) §4

The terminal build for Windows has no sound: it leaves the engine out, and
WASAPI's speakers build into the window builds alone. A terminal run opens the
build's own speakers as a window's does, so it closes with `wasapi.go` built
without the window and the `windows` target keeping audio.
Its mouse comes from Windows 10's own console host, and likely its flicker (§4);
confirm that Windows Terminal has neither, with the terminal module's synchronized
frames and re-hidden cursor. `sandbox/termbench` tells hosts apart: `rawin` prints the
records a mouse report arrives as, `live -lib terminal|tcell` plays a scene on
each library's own backend.

### Report key releases in a Windows console

- Priority: P3
- Affected files: the terminal module's Windows backend and key protocol
- Guide: [Input and modes](input-and-modes.md) §1

The window build reports releases; in a console a key is still a tap: it reads as
VT input, which carries presses alone. A console host with win32-input-mode
(Windows Terminal's) reports each key's press and release once asked with DECSET
9001, as `CSI Vk;Sc;Uc;Kd;Cs;Rc _` (tcell asks for it and drops the releases). A
Windows key source would ask for it as the protocol source asks for the key
protocol, answer presses where the host lacks it, and pair releases through
`KeyHolds`. Decide first whether holds in a console are worth it.

## Fleet

The design and what it cost are in [The session fleet](kubernetes-fleet.md), and
the procedure in [Fleet deploy](kube-docker-deploy.md).

### Hand off a deployment a stranger can install

- Priority: P1
- Affected files: `doc/kube-docker-deploy.md`, `deploy/`
- Guide: [Fleet deploy](kube-docker-deploy.md#13-first-session) §13

Rehearse the procedure from bare Arch Linux and from bare Ubuntu, record their
package and service differences, and fix every command that assumes the
production node. Done when both reach a first session with no step their operator
had to invent, and every resource value in `deploy/k3s/30-session.yaml` cites a
number from [Measure a full roster](#measure-a-full-roster).

### Measure a full roster

- Priority: P1
- Affected files: `deploy/k3s/30-session.yaml`, `deploy/k3s/10-quota.yaml`,
  `tool/vif-router/manifest.go`, `deploy/guest/vif-router.env`, the Hugo site's
  `vif-lobby.js`
- Guide: [Session fleet](kubernetes-fleet.md#6-resources) §6

Drive nine sessions with headless joiners for the fleet-level readings (CPU,
memory, tmpfs, log rate, rotations), plus one real four-player session over real
links, through a tower and a storm and on `wad/scenario/base/td`, for the hour the
Job's deadline allows, which tick slips and correction magnitude need. Requests,
limits and the quota totals then come from that measurement rather than from
single-guest history and estimates. It passes with no OOM, liveness restart,
sustained tick slip or growing correction magnitude, the deadline ending each
session. The socket readings (`TestNetcodeReadingsOverSockets`) cannot seat
sixteen td guests inside their join wait, so the td-16 column of netcode design
§3.1 has not been re-read since phase 1 and is this measurement's to take.

A session's own bots are read here before a request may ask for them
(`VIF_ROUTER_BOTS_MAX`, 0 until then):
- K=3 with `./script/test.sh load` or the deploy guide, replacing the sandbox
  figures the per-bot envelope stands on (2 sessions of 2 bots for 30 s: about
  190–200 ms of CPU a second and 90–95 MiB peak per host, no evictions), with the
  digest mismatches that run counted, 0–5 per host, watched as the load grows.
- A seat's cost on a map past the 4,800 cells bots are held to (`botMapCells`),
  which keeps them off td.
- The quota: its 1500m of CPU, 392m of it the standing sessions', fills at three
  sessions of three bots, and the router refuses the next as `fleet_full`. Raise
  it, or accept the fewer sessions.
- Then the site's `vif-lobby.js` sends `bots` and reads `bots_max`.

### Apply the fleet log retention on the node

- Priority: P1
- Affected files: `deploy/guest/vif-fleet-log-cleanup.py`, `doc/kube-docker-deploy.md`

Apply the cleanup's 75-minute stale age on the node: reinstall the cleanup script
as [Fleet deploy](kube-docker-deploy.md) §7 does, then watch `findmnt` after a
full hour of play.

### Promote verified images to the node

- Priority: P2
- Affected files: `deploy/guest/update-vif-image.sh`, `.github/workflows/release.yml`,
  `deploy/k3s/30-session.yaml`, `tool/vif-router`
- Guide: [Fleet deploy](kube-docker-deploy.md#8-the-session-image) §8

Nightly CI publishes the headless image to GHCR under moving and commit-addressed
tags, and the node builds and imports its own. What remains is choosing the
node's registry and promotion policy, authenticating pulls without a long-lived
off-node deployment credential, and moving new sessions to a verified digest while
existing ones finish on theirs.

### Put a cluster through its failures

- Priority: P2
- Affected files: `doc/kube-docker-deploy.md`
- Guide: [Session fleet](kubernetes-fleet.md#7-verification) §7

Not yet tried on a cluster: `tc netem` latency, loss and reordering, which should
recover at the next bounded correction or keyframe; a pod delete or a node
failure, whose connected-client behavior should be recorded, with no replacement
advertised as the same match; and a full log tmpfs, which should lose a bounded
amount of log without ending a game.

### Provision audio to the fleet when a host needs it

- Priority: P3
- Affected files: `deploy/guest/update-vif-wad.sh`, `tool/vif-router/manifest.go`,
  `deploy/k3s/30-session.yaml`

What a fleet session reads from the node volume is one list in three places —
`categories` in the installer, `wadCategories` in the router, and the mounts in
the template. `scenario` and `image` are on it. `audio` goes on it the day a
dedicated host renders anything, and `content` never does. Nothing else changes;
the volume already carries whatever the installer puts there.

### Keep the standing learner's population across pods

- Priority: P3
- Affected files: `tool/vif-router/manifest.go`, `deploy/k3s/30-session.yaml`,
  `internal/resource` (`Populations`), `doc/kube-docker-deploy.md`

The standing `cave` session's learner keeps its population in memory, and loses
it with its pod ([Session fleet §9](kubernetes-fleet.md#9-the-session-routes)).
Kept across pods: a writable store at the root the learner saves under, as a
setting's root is; a session pod has a read-only root and two claims, its log and
the wad, so a third, on the node, beside the log's directory or in one of its own,
chosen with the node's setup (Lixen, 2026-11-28). One writer at a time, since two
pods of one population would each lose the other's rounds.

## Multiplayer

### Hear a participant again once its link reopens

- Priority: P1
- Affected files: `internal/system/network.go` (`announceDeparture`,
  `removeParticipant`, `addParticipant`, `receiveAuthority`, `receiveDeparture`),
  `internal/converge/authority.go` (`onReport`, `takeBack`, `reconcile`),
  `internal/converge/reach.go` (`admitPeerLink`), `internal/app/migration_test.go`
- Guide: [Multi-player §5.1](multi-player.md#51-succession),
  [§5.2](multi-player.md#52-the-roster-after-a-loss),
  [§5.3](multi-player.md#53-reachability-under--authority-migrate)

An instance marks a participant departed as it announces its lost link
(`announceDeparture`): the coordinator any link, a guest one the participant was
behind. The coordinator's crossing marks it on every instance (`removeParticipant`).
Only an arrival clears the mark (`addParticipant`), or on a guest a join through it
(`drainWith`), and while it stands `receiveAuthority` and `receiveDeparture` drop
whatever that participant sends. A reopened peer link clears nothing:
`admitPeerLink` admits any roster member, a Wraith included, so the link stands and
the succession hears nothing on it. A person whose link to its host drops is
unaffected, since its redial proves its key and the key's arrival clears the mark.
Two cases are not:
- A participant that joined through a guest holds a link to the host too, as every
  participant does to a successor (§5.3 item 3), and its link to that guest drops
  and reopens. The guest floods a notice the host, linked to the participant,
  ignores (`receiveDeparture`), so nothing crosses and nothing clears, and the guest
  ignores it for the rest of the session. When the host goes and the guest takes
  the term, it drops the other's notices (`MsgAuthorityReport`) and crosses its
  departure at the floor, and the other takes the next term.
- A survivor's link to the candidate drops once the candidate holds the term and
  reopens before the survivor adopts a record. The new host crossed its departure,
  so the notices on the reopened link are dropped, the survivor is never answered,
  and it takes a term of its own. Only a report another participant relays passes
  the mark; one on the survivor's own link never reaches `takeBack`.

Either way the session splits: two hosts, each holding the other and the lost host
as Wraiths. A survivor with no path to the candidate is meant to split off (§5.1);
one whose link came back is not. A three-participant `meshSession` probe, 2-3 cut
with `MeshPort.Disconnect` and linked again with `Mesh.Peer`, ended with 2 on term
2 and 3 on term 3 in both: 3 joined through 2 and linked to the host beside (links
1-2, 2-3, 3-1), cut before the host closes, and each joined through the host (1-2,
2-3, 1-3), cut once 2 holds term 2. The second shape cut before the host closes, or
linked again as 2 takes the term, no longer splits: neither guest marks the other,
and both end on term 2.

A relayed person the silence window retired (§5.4) that finds another path only
afterwards is a third case, with no split: the host admits its epochs for a Wraith,
and it plays on unseen until its key comes back through a door. Its authority is
not lost, so it walks nothing and sends no notice for the remedy below to answer;
an epoch it produced after its departure applied is the evidence a host has.

What would close it:
- A link admitted after the mark was set clears it, so a mark nothing crossed is
  gone the moment the link is back. That fixes the first case alone.
- For a participant whose departure crossed, the reopened link carries its walk: the
  host answers its notice, takes its membership report, and gives the place back
  through `CrossReturns`, whose arrival clears the mark everywhere. `takeBack`
  declines a report that lands before the floor (`reconcileAt` still set), and
  `recordReport` never offers it again, so the floor has to give back an away
  participant that reported at its term. One that lands after the floor crossed
  the departure and before it applies finds nobody away yet and is lost the same
  way (read off `takeBack`, not reproduced).
- The coordinator still crosses one departure per participant: clearing the mark
  must not let a second drop of the same link cross a second departure for a Wraith.
- A test per case on the probe's shape, with `meshSession` handing back its mesh:
  participant 3 follows 2 at term 2, and no Wraith but the lost host's remains.
  `TestALateSurvivorsPlaceIsGivenBackWithItsBots` reads the host alone: the host
  closes a survivor's own link as the floor's departure applies
  (`removeParticipant`), so the survivor's side of a place given back needs the
  reopened link this item brings.

### Close what a relayed player's departure still leaves open

- Priority: P3
- Affected files: `internal/converge/authority.go` (`driveSilence`),
  `internal/system/network.go` (`removeParticipant`, `receiveDeparture`,
  `announceDeparture`, `publishBarrierTelemetry`),
  `internal/app/internal/membership/session.go` (`keyedLocked`, `leaveRoster`)
- Guide: [Multi-player §3.6](multi-player.md#36-holding-a-session),
  [§5](multi-player.md#5-membership-topology-and-authority-continuity),
  [§5.4](multi-player.md#54-wraiths)

The authority retires a person it holds no link to once it has had no news of it
for the silence window (§5.4), crosses out behind its arrival a joiner lost before
that applies, and hears of a lost link only from the instance its participant is
behind. What those rules leave:
- **Under a hold** the window does not run, a held authority taking no tick. A
  person whose relay left under one stays live until the hold ends and the window
  passes, its key refused `RefusalReturning` meanwhile, past its dial's 30 s. And a
  departure the window crosses within a lead of a hold stays scheduled through it,
  the relay's link standing: a key that comes back through another door, its old
  link dropping after, is announced by the old relay behind its arrival
  (`announceDeparture`) and retired again as the hold ends, which costs it a redial.
- **An arrival and a departure applying in one tick**, as those crossed under a
  hold do, both run before the arrival's cursor is made. A bot arriving as its
  holder departs is missed by the holder's group (`removeParticipant`) and stays
  live and undriven until its holder returns or is dismissed, the window judging no
  bot. A seat its holder drops before its arrival applies is read as no seat
  (`receiveDeparture`) and waits a Wraith's two minutes. An arrival whose spawn
  finds no free cell leaves the door's entry away with no Wraith to time it out.
- **A relay more than a redial behind its authority** applies the departure a
  returned player came back from after that player's gate, a tick ahead of the
  arrival, and closes the new link, leaving the player to the window and a second
  dial (read off `removeParticipant`, not reproduced).
- **Telemetry.** A guest that takes a departure from a capture rather than its
  crossing never marks the source, and it marks no participant it was not carrying:
  a person whose relay left, playing on behind another guest, counts that relay in
  `network.barrier_peer_lag_ticks` and `network.barrier_ran_without_peer` for the
  rest of the run. Nothing reads either.

What would close them: under a hold, the first rule below kept to one, the door
reading as departing a person whose epochs last came in on a link that is; for one
tick, the handlers asking what an arrival of that tick is about to make (`rosterDue`
keeping its holder, and the spawn's refusal reaching the door); for telemetry, the
capture's rebind marking each Wraith's source.

Two rules were weighed against the window and not taken:
- **Only after another departure.** The authority counts from the departure of the
  link a person's epochs came in on. A stall alone retires nobody and a hold needs
  no clock, but a person lost with no departure before it stays a live cursor
  nobody drives, as one its adopter stopped hearing before the link died or whose
  notice outran its relay's word that it had joined, and it takes a case list and
  a record of each person's last link.
- **No timer, only a returning key.** A proved key takes back a place the host
  holds no link to. Nobody is retired while it plays, but a lost player holds its
  slot as a live cursor until it comes back, a key cannot tell a lost relay from a
  live one whose link is half open, and the old relay's late notice then retires
  the returned player again.

### Hold a crossing's content to what its sender could mean

- Priority: P1
- Affected files: `internal/system/network.go` (`crossingRules`),
  `internal/system/death.go` (`processBatch`), `internal/system/combat.go`
  (`applyHeal`, `applyHitDirect`, `applyHitArea`)
- Guide: [Multiplayer §2](multi-player.md#2-current-contract) (Trust)

A crossing's type, tag and author are checked where it applies; what it names
inside that is not. A modified participant's `EventDeathBatch` lists any entity a
death is not kept from, a species among them, whose end every instance then
derives and a scenario's machine moves on; its `EventCombatHealRequest` names any
live target and any amount, and its `EventSpeciesLandRequest` any species and
cell. What would close it: each receiver holding a batch to the gold members
under its sender's drain, a heal to a drain's hit points, and a landing to a
fusion its sender was elected for.

A crossing's target is an entity number too, its producer's: an instance that
applied an arrival late allocates the cursor after what the others allocated
first, and holds both under other numbers until a correction. A hit's
`TargetEntity` and `HitEntity` are not checked against what their producer meant,
as a typed member's end is (`CompositeSystem.destroyMember`).

### Bound the router's whole

- Priority: P3
- Affected files: `tool/vif-router` (`config.go`, `audience.go`, the doors),
  `deploy/guest/vif-router.env`, `doc/kubernetes-fleet.md`
- Guide: [Fleet §6](kubernetes-fleet.md#the-routers-own-cost)

Routed games open on a ceiling over the router's whole: memory and the uplink
bound it, not its CPU, a routed game's bytes crossing the uplink twice and its
spectators weighing more than its players, 2,560 streams at today's settings. One
setting over its connections and spectators together, sized from the node's
uplink and the router's memory limit, which `-tunnels` then shares between relay
doors and routed games; at the ceiling a publish is refused as full, and the
listing's `doors` reaching `doors_max` says so, which the site's Host area warns by.

### Publish a player's session at the router

- Priority: P3
- Affected files: `tool/vif-router`, `internal/app/internal/membership/host.go`,
  `internal/network`, `web/vif.js`; on the site, `vif-lobby.js`
- Guide: [Multiplayer](multi-player.md#56-relay-doors-and-the-router) §5.6

A run publishes its `-relay` door through the router's reverse tunnel and is
reached at the site; a host's door published the same way is a **routed game**, a
player's own session reached there from a terminal, a page or Android, the router
neither allocating nor authoring, the word the site's lobby uses too. The deployment this
leads to serves as many pods as the node has resources for (`-sessions-max`,
twelve today, two of them standing) and routes every session beyond them. The tunnel carries a door already
(`tool/vif-router/README.md`, Doors). Joins and creates are budgeted by account
where a sign-in names one; a publication is budgeted by address alone, and its
`Resume` token is the last bearer secret on the wire
([Auth and session design §2](auth-session-design.md#2-what-authenticates)).

Steam Networking Sockets and its datagram relay would hide a player-hosted
session's addresses too, and tie hosting to Steam: an option beside this one, not
part of it.

What the feature itself needs, best shaped by deploying it step by step:
- `-host vif://<site>:7777` opening the door through the router rather than binding
  one (only a joined run's `-relay` publishes today, `config.RelayRouter`), and the
  host's run reporting its state on the control connection (players, ceiling,
  scenario, whether it admits) so the router lists it without a `/health` read
  through the tunnel.
- The session's row in `/vif/api/sessions`, with `kind` `routed` beside the pods',
  listed only where its player asked (`-listing public`, a `PublishRequest` field;
  `private` when the flag is absent, as a menu row and `:host` take it too), a
  private one routed all the same and reached by its link alone; its lifetime (the
  control connection's, plus the resume window), and the create budget charged to
  the publisher's address as a pod's is. `limits.host_target` names the address a
  host publishes at: the site's lobby shows its Host area's command by it, and
  lists public `routed` rows under Routed.
- The publish ticket: the publish frame carries the run's key and, over TLS, its
  sign-in (`PublishRequest` is JSON, so a field is added without a protocol change);
  the router asks which account the sign-in names and refuses a publication by none
  (D12: publishing a door takes an account), budgets doors by account beside
  `-doors-per-address`, and a resume signs the router's nonce with the run's key in
  place of presenting the bearer `Resume` token, so no bearer secret is left on the
  wire ([Auth and session design §3.6](auth-session-design.md#36-admission-tickets-and-accounts),
  [§3.7](auth-session-design.md#37-budgets)). Implication: a `-relay` or hosted
  publication needs `:login` first; an anonymous run still joins through a published
  door.
- Routed games open after [Bound the router's whole](#bound-the-routers-whole),
  and a page hosts after [Host a routed game from a page](#host-a-routed-game-from-a-page).
- `-doors-per-address` counted as the join budget counts an address. It counts the
  dialer's whole host (`addressHost`), so one IPv6 /64 holds that many doors an
  address, up to `-tunnels`; the join budget and a session's spectators are
  counted by `network.AdmissionKey`.

### Host a routed game from a page

- Priority: P3
- Affected files: `tool/vif-router`, `internal/network` (`DoorTunnel`),
  `internal/app/internal/config` (`ErrBrowserHosting`), `web/vif.js`
- Guide: [Multiplayer](multi-player.md#56-relay-doors-and-the-router) §5.6

`DoorTunnel` dials through `link.Dial`, which speaks `wss://` in a page, but the
browser route wants a session id in its path before the first frame, so a publish
needs a path of its own, which the listing names as `limits.host_ws_target`: the
site's Host area then opens the page as `?arg=-s=<link>&arg=-host=<that path>`,
with `-listing=public` where asked. A hosting page must be exempt from
`LEAVE_HIDDEN_MS`, plays a scenario from its link (`-s`), and is never a successor
door; until then a page's `-host` is refused (`config.ErrBrowserHosting`). Wasm
runs host work about 3.9x slower than a native core: `fighter` with eight
participants takes under half the page's thread and `td` does not fit, so the
authority belongs in a Worker of its own.

### Project a correction outside the live lock

- Priority: P2
- Affected files: `internal/app/internal/corrector/snapshot_stage.go`, `internal/system/network.go`
- Guide: [Netcode design](netcode-design.md#less-work-under-the-live-lock)

The live lock still holds an install's projection: each projected tick adds a
simulation tick, 2.6 ms at td, so a repair 16 ticks behind holds it past 45 ms.
Moving it out: project to the live tick off the lock, then under it feed
what the barrier retained since the first feed, project the ticks the live world
advanced meanwhile, and write. Any out-of-lock or incremental design must keep
[Multi-player](multi-player.md) §3.3: fence membership, pending barrier crossings
not fed, the ledger settled before projection, the journal's place and mark, and no
live tick between the final projection and the write; the second feed needs a
retention watermark the barrier does not keep, since `RetainedAfter` selects by
tick and would feed the first feed's records again. WASM gains scheduling time, not
parallel CPU.

### Split combat telemetry by domain

- Priority: P3
- Affected files: `internal/system/combat.go`, `internal/status/key.go`
- Guide: [Domain model §7](domain-design.md#7-current-limits)

Combat's counters mix Shared results with per-instance ones, so two instances'
Shared combat outcomes do not compare directly. Splitting them is a telemetry
redesign rather than a domain-boundary fix.

### Offer lixen.com games on the start screen

- Priority: P1
- Affected files: `android/app/src/main/java/com/lixen/vif/StartScreen.java`,
  `android/app/src/main/java/com/lixen/vif/MainActivity.java`, `android/game/game.go`

Android opens on a start screen with one choice, "Tap to start", a solo run; the
site is reached only from inside the run, through the menu's Multiplayer page
(join a link, or request a session from `https://lixen.com`, its default;
`:register` and `:login` keep a sign-in in the app's files, which a request to the
site sends). Put a second choice beside the tap: host a game at the site, as that
request does, and/or join one of the running games the site lists
(`GET /vif/api/sessions`), so `game.Start` takes a join target. The
page needs nothing: the site's lobby (`/vif/lobby/`) is its choice, and its Play
link opens the page with `-join`, which joins at its start screen's tap. A run that
ends in an error, such as a refused join, shows it on the start screen, as the page
does; today Android finishes the activity and leaves the reason in logcat. Open:
host, list or both; a second screen or rows on this one; what a session that ends
returns to.

### Run nearby games on a phone

- Priority: P1
- Affected files: `android/app/src/main/java/com/lixen/vif/MainActivity.java`,
  `android/game/game.go`, `pkg/network/discovery.go`
- Guide: [Multiplayer §5.7](multi-player.md#57-games-on-the-local-network)

Finding games on the local network was run between two runs on one Linux machine;
on Android the APK builds and no device has run it. On one Wi-Fi, a phone and a
desktop or two phones, a desktop's firewall passing 7777 in for tcp and udp: the
host enters `:host :7777` and `:lan on`; the other
opens Menu, Multiplayer and sees it under Nearby games within two seconds, and
its row joins it; then the roles swap. What only a device shows:

- the multicast lock is taken with no crash, and dropped when the app is left
  (`adb shell dumpsys wifi`, the multicast locks held);
- a phone hosting on its own hotspot is found by its guests;
- the list empties five seconds after the host enters `:lan off`;
- Android 17 and GrapheneOS reach the local network at target 36.

### Ask for the local network on Android at target SDK 37

- Priority: P3
- Affected files: `android/app/build.gradle.kts`,
  `android/app/src/main/AndroidManifest.xml`,
  `android/app/src/main/java/com/lixen/vif/MainActivity.java`, `android/game/game.go`
- Guide: [Multiplayer §5.7](multi-player.md#57-games-on-the-local-network)

The app targets SDK 36, where local network access comes with `INTERNET`. From
target 37 Android blocks every packet to or from a local address, tcp and udp
alike, until the player grants `ACCESS_LOCAL_NETWORK` (the `NEARBY_DEVICES`
group), and its documentation says not to declare or ask for it below 37. So the
bump declares it and asks on the player's gesture: turning Visible on this
network on, opening Nearby games, `:host`, and a `:join` to a local address,
each refused in the player's words when it is denied. Play has asked new apps and
updates for target 36 since 31 August 2026 and by its yearly step would ask for
37 a year on, which it has not announced; a sideloaded APK is never asked. The
system's own picker (`NsdManager`, `DiscoveryRequest.FLAG_SHOW_PICKER`) needs no
permission, but it finds mDNS services, which vif does not announce.

### Watch a session as a spectator

- Priority: P2
- Affected files: `internal/app/internal/membership/watch.go`, `host.go`,
  `internal/mode/commands.go` (`handleSpectateCommand`), `config_form.go`,
  `cmd/vif`, `tool/vif-router/audience.go`,
  `internal/app/internal/scribe/journal.go`, `internal/system/particle.go`,
  `glyph.go`
- Guide: [Multiplayer §5.8](multi-player.md#58-watching-a-session),
  [Session fleet §9](kubernetes-fleet.md#9-the-session-routes)

A run spectates a session from the one feed its authority writes, a fleet
session's through the router, and looks from any player's cursor (§5.8). What
that leaves:

- **Consent.** Who may be watched is a ceiling set where a session is served:
  `-spectators` on a host, 8 unless set and a flag alone, and the router's for its
  whole fleet. Missing: "Let others watch" on the Multiplayer page, saved with the
  rest of `Play`, and a ceiling a host changes mid-session; a fleet session whose
  creator asks for none (a field of the create request, the pod's `-spectators 0`,
  the listing saying so); and a player's own switch that keeps the view off its
  cursor, carried on the roster entry as `Key` is, seats taking their holder's.
  Players are told nothing: only a host's `:session` counts its spectators, and a
  fleet pod counts one, the router.
- **A passphrase on `:spectate` and the menu's form.** A spectator shows a gated
  door the phrase its run already holds (`-passphrase`, an earlier `:join`);
  neither `:spectate <target> [slot]` nor Spectate a session takes one, as
  `:join <target> [passphrase]` does.
- **A session whose host went.** A viewer that lost its feed's link dials the
  address it watched and starts a local game when nothing answers
  (`Membership.feedEnded`). Under `-authority migrate` the session plays on at a
  successor's port, which a viewer is never shown (`SessionOffer.Watched()` carries
  no chain), so that nobody's address reaches a run that only watches. Following
  takes a door that names no participant: the session's own at the router
  ([Publish a player's session at the router](#publish-a-players-session-at-the-router)).
- **A door a run published, and a relay's.** A guest's door serves joins alone and
  refuses a watch dial (`errNotHosting`; the router's `ErrUnwatched` for a tunnel).
  A guest subscribed to its host's feed could repeat it at its door
  (`FeedHub.Repeat`, as the router does), which would also take the audience's
  bytes off the host. A routed game's row whose door carries the feed is marked
  `watchable`, which the site's lobby offers Watch by.
- **A lobby that has not started.** A watch dial at a session in its lobby is
  refused as not started (`ErrUnstarted`), for a person to retry; held at the door
  and fed from the first tick, a spectator would see the start.
- **Spectators behind one address.** A watch dial is one of an address's six a
  minute at a host's door, and the router adds `-spectators-per-address` (4): a
  classroom behind one NAT shares both. A viewer shows no sign-in, so no account's
  budget applies. Options: a wider budget for a dial that only watches, or an
  account's share for a viewer that chooses to show one.
- **A spectator's journal.** A viewer keeps none: `-spectate` refuses `-journal`
  and `:journal` is unavailable while spectating. Its feed from a starting point
  on is a whole recording of the shared world: written to a file as it arrives, a
  viewer replays the session from it, with its changes of view journaled as local
  events so the replay looks from the same players.
- **The periodic world.** Every `FeedSyncTicks` (600) each viewer is sent a whole
  world, 3.9 to 9.1 kB and a tenth to a fifth of the feed, which one already
  reading only checks itself against. A digest would check the same in a few
  bytes, the whole world kept for a newcomer alone; read what that does to the
  tail a newcomer catches up through (netcode design §3.8, `VIF_FEED=1`).
- **A shorter record.** `FeedRecordTicks` (10) and `FeedSlackTicks` (4) put a
  viewer 13 to 14 ticks behind. Fewer ticks a record bring it closer for more
  records and less to deflate against: read both at 5 before choosing.
- **An audience on the node.** The copy's cost was read on loopback in a container
  (netcode design §3.8). Read 256 spectators of a four-player session through the
  edge: the router's CPU and egress beside the pod's, and what the edge's own
  connection limits pass.
- **Waves and a scenario's lines.** The decay and blossom waves a shared state
  machine raises fall on a spectator's screen as on every run's
  (`ParticleSystem.spawnWave`), over no text, while the lines a scenario lays, a
  tutorial's lessons and scenery, are glyphs and so are not drawn there
  (`EventGlyphLineRequest`). If either reads wrong, a viewer's world skips the waves
  as it skips the field, or draws a scenario's scenery lines.

A spectator sees the Shared domain, what any remote participant sees, from the
cursor of the player it follows, whose owner-synced values the status bar reads;
its own run lays no glyph or nugget. Showing a player's own domain is not pursued
(Lixen, 2026-11-14): Player-domain state is never rebuilt on another instance
(D-2) and the authority holds none of it, so that player's glyphs, drains,
nuggets, projectiles and loot stay on its run.

Beside it, from the same review of the journal's slot handling: a journal records
its slot from `LocalSlot` at the opening (`JournalCapture.Slot`,
`JournalAnchor.Slot`), its replay binds input there (`Scribe.OpenedLocked`), and
later installs bind by identity (`RebindCursorRoster`). One a joiner opens before
its arrival may record `NoPlayerSlot` and must bind when the arrival or a
correction brings the cursor, which wants a test.

### Serve a session's feed as cacheable segments

- Priority: P3, to evaluate when a cache fronts the site or an audience outgrows
  the sockets one router holds
- Affected files: `pkg/network/feed.go`, `tool/vif-router/audience.go`,
  `internal/app/internal/membership/watch.go`, `web/vif.js`
- Guide: [Multiplayer §5.8](multi-player.md#58-watching-a-session),
  [Netcode design §3.8](netcode-design.md#38-the-feed)

The choice made: one feed, written once and copied to every viewer down a socket
each holds open, by the host's hub or the router's. A viewer stands about 0.7 s
behind at the default tick, and each costs its feeder the feed's bytes again, 0.9
to 1.9 kB/s, and about 10 µs of CPU a record.

What the other form does: the router also cuts the feed into segments, a few
seconds each, and serves them over HTTP beside a short list of the newest, as
live video is served. A viewer fetches the list, a starting point and each segment
as it appears, and holds no socket. A cache in front of the site answers every
viewer after the first, so the node sends each segment about once however many
watch.

What it entails: a segment is the records between two cuts (`FeedCut` restarts the
deflate history already), named by its session, game and first tick, with the
starting point fetched apart, since a segment carrying one would be mostly world;
a route at the router serving them with cache headers and expiring them; a
`FeedPort` that reads fetched segments, `net/http` natively and `fetch` in a page;
and a viewer three to six seconds behind, a segment's length and its fetch,
against 0.7 s. It can sit on the feed as built: the router's subscription
is the source of both.

Why it was not chosen: its cost is flat only where a cache fronts the site, which
is the edge's to say and no part of this repository. With none, the node answers
every fetch itself, a request a viewer a segment, which costs more than a held
socket's copy; and the lag is paid either way.

Evaluate it when a cache or CDN serves the site's paths, or when an audience is
wanted past what one router holds open (`FeedAudienceMax`, 1024 a session). Ask
which the site has; the repository cannot say.

### Seat a spectator as a guest with no cursor

- Priority: P3, to evaluate if a spectator must stand on its host's tick
- Affected files: `internal/network/session.go` (`RosterEntry`, `SessionOffer`),
  `internal/app/internal/membership/join.go`, `internal/converge`,
  `internal/engine/world.go`
- Guide: [Multiplayer §5.8](multi-player.md#58-watching-a-session),
  [Domain design §1](domain-design.md#1-domains-and-identity)

The choice made: a viewer is nobody in its session. It reads the one feed every
viewer is sent, runs a tick only once the feed closed it, and so needs no
correction exchange and sends nothing.

What the other form does: a spectator joins as a participant with no cursor, a
second kind of cursorless roster entry beside a dedicated host's coordinator. It
holds a peer link, predicts the shared world on its own clock as a guest does and
is corrected over that link, so it stands on the session's tick, not a record
behind.

What it entails: the least new code, since the join, the capture at the seat and
the corrections exist; an entry not counted in `MaxPlayers`, never a successor and
no stall for the start gate or the hold; and a link each at the authority, with
its cadence, manifests and repairs, 1.8 to 2.5 kB/s measured to a guest against
0.9 to 1.9 for the feed every viewer shares. The spectator is in the roster, so
the players' runs are told of it, and it sends: one more link a session must
bound and can be stalled by.

Why it was not chosen: each spectator costs a guest's share of the host's CPU and
uplink, which holds an audience to about what a session seats, far from a lobby's
hundreds; and a roster entry is a place, which a spectator is kept from holding,
so that it never takes a session over and nothing names it or where it listens.

Evaluate it for a few spectators who must be live to the tick, as a referee or a
commentator. The feed's half second is `FeedRecordTicks`, and a shorter record is
the cheaper answer to try first.

## Netcode

### Price the log's and journal's 50 ms sync

- Priority: P3
- Affected files: `internal/vlog/vlog.go`

With `-log DEBUG` and a journal open, the same session's terminal ran 14% system CPU
and about 6,000 block writes a second (`proc.io_write_s`) for some 80 kB/s of
records: both files sync every `flushIntervalMs` (50 ms). The journal's crash
promise needs it; the log's does not. Measure a log that syncs on its own cadence,
or only on WARN and above.

### Read a touch client's uplink

- Priority: P3
- Affected files: `internal/mode/router.go`, `android/game/view.go`,
  `internal/system/network.go`

`network.link_bps` is the rate the far end reports receiving this instance's bytes
at, so a client reading two to seven times another's on the same link, as the
Android client did against a terminal at the pod, is sending that much more unless
`network.link_saturated` says the link was the limit. The candidate is input shape:
a drag crosses a placement every tick the finger moves, to the newest cell it
reached (`Router.moveMouseCursor`, `PushPointerMove`), where a key moves one cell a
press, and god mode multiplies the actions. Compare the two clients'
`network.sent_epoch_bytes`, `network.sent_sync_bytes` and `network.crossings_sent`
over one window, and `network.pace_late_ticks` for a client the OS throttles.

### Land a joining seat inside its pace band

- Priority: P2
- Affected files: `internal/app/internal/membership/host.go`
  (`ResumeJoinedSession`), `internal/app/script.go` (`drive`),
  `internal/converge/correction.go` (`ProvedWorld`)
- Guide: [Multi-player §3.5](multi-player.md#35-command-frames-the-commit-and-pacing),
  [§3.6](multi-player.md#36-holding-a-session)

A join's catch-up runs the joiner to the newest epoch it heard and its first tick
follows at once, so a seat in its holder's process stands a tick ahead of it nine
samples in ten, the late edge of its band, where its pace rests: an authority
crossing reaches it with what the join took as its whole margin. Over 3 s on an
idle machine `network.barrier_late` read 2 to 12, a correction each. A hold that
late lands between ticks (§3.6), but a guest that ran a whole tick further stands
past its host, and a relay there refuses every join for the hold's length
(`ProvedWorld` waits for a proof at its own tick). What would close it: the
joiner's first tick waits part of an interval, or the catch-up stops a tick short,
read over the divergence harness's four shapes and a socket seat's
`network.barrier_late`; and a held relay serves the proof it holds.

### Settle a replay's placements as its run's install did

- Priority: P3
- Affected files: `internal/system/network.go` (`AdoptSnapshot`),
  `internal/app/replay.go` (`replayTarget.Install`), `internal/engine/resource.go`
  (`PlayerResource.Reconcile`)
- Guide: [Multi-player §3.3](multi-player.md#33-projection),
  [Logging: Written worlds and notes](logging-and-diagnostics.md#written-worlds-and-notes)

A run settles its D-18 queue when a world it installs already contains placements
it had in flight (`AdoptSnapshot` reconciles one prediction for each own move it
prunes). No record says so, and a replay has no barrier to prune, so its queue
keeps them: from that tick its view record's `predicted` depth differs, and with it
the cell the camera and a shot from the local cursor read. It takes a guest behind
its host with moves in flight, which a correction then carries forward: a newcomer
joined under a hold whose host ran 80 ticks before its first tick diverged in 6 of
6 runs. What would close it: the install's record carries how many placements it
settled, or the settle is journaled as a note, and `replayTarget.Install`
reconciles as many.

### Let the selective-apply test see an adopted owner-authored cell

- Priority: P2
- Affected files: `internal/app/migration_test.go`

`TestSelectiveApplyKeepsItsExclusionsAcrossAHandoffAndARelay` lowers the
follower's energy so a correction adopting it would show, but owner sync hands
the successor the new value first and the receiver compares its tick-close
capture, so no correction carries a differing cell: the test stays green with
every D-13 guard off at once (`applyShard`'s owner check, the encoder's
exclusion, both owner-authored cursor sets, `RebindCursorRoster`'s restore).
Perturb where the successor's mirror still differs at a tick close, and show the
test then fails with the guards off.

### Move the netcode out of the network system, then split what stays

- Priority: P3
- Affected files: `internal/system/network.go`, `pkg/network`, `internal/event`
  (the wire batch), `internal/system/domain_test.go`

`NetworkSystem` is one file of about 3,900 lines: the roster and departures,
crossings and the barrier, the relay tree, the lead and pacing, owner syncs, the
replay suffix and the snapshot's adoption, and telemetry. The netcode is meant to
plug into other projects, so the first step moves every part that is not this
game's to the package that should own it, `pkg/network` or one beside it: the
sequence `window`, the barrier's schedule of artifacts by apply tick and source,
the replay suffix, the relay tree (`up`, `down`, `carries`, `behind`,
`Downstream`), the flooding of departures, and the lead's pacing (`paceDecision`,
`driveOwnLead`). A `pkg/` package imports nothing under `internal/`, so what moves
takes a frame type of its own or a type parameter where it holds `event.WireBatch`,
and a small interface where it reads the world, the log or the status registry.
What stays is the game's: a cursor's owner syncs (`readCursorState`, D-13), the
actor a crossing may name (`actsAsOwn`), the status cells and the ECS hooks.

Then what stays splits by concern, as pure moves. The domain suite credits a file
to the system whose `Name` it declares (`declaredName`), so it must first credit a
file by its methods' receiver, or each new file is an unchecked helper that
`TestHelperFilesArePinned` refuses. Both steps conflict with every open branch on
the file, so they wait for a pause between waves of netcode work.

### Tell a host that stalls from one that is gone

- Priority: P2
- Affected files: `internal/converge/authority.go` (`driveShedding`, `peerLost`),
  `internal/converge/correction.go` (`observeFloor`), `internal/converge/reach.go`
  (`Reach.cutOff`), `internal/render/renderer/status_bar.go`
- Guide: [Multi-player §3.5](multi-player.md#35-command-frames-the-commit-and-pacing),
  [§5.1](multi-player.md#51-succession)

A guest reads its host's silence from its link alone: one that stops answering is
dead at `NetworkLinkDeadAfter`, and the guest redials or walks (§5.1). Two stalls
fall between that and a host that only slows:
- **A host whose loop stalls while its transport answers**, a deadlock on the world
  lock or a pathological tick: the probe is answered on transport goroutines, so the
  link stays up and the guest predicts alone for as long as the stall lasts. Its
  badge stays green, since `slow!` needs a cadence only a publisher writes, and the
  one sign is the floor's 8 s alert. What would close it: a floor that stays
  breached over a link that answers counts as the authority's loss and opens the
  walk `Reach.cutOff` opens, and the badge shows a breach with no cadence.
- **A host frozen for 1.1 to 5 s**, which every link survives: its clock projects
  through the freeze, the scheduler drops the debt at once, and `driveShedding`
  takes that one drop for overload, shedding the newest bot, else the newest guest
  ("Removed by the host to keep its tick"). In a scratch converge test a 1 s freeze
  shed nobody, and 1.5 s and 4 s shed the guest. What would close it: a single drop
  longer than the link-dead time is not counted, or the share has to hold over two
  windows.

### Carry what no capture holds to a guest that lost its frame

- Priority: P3
- Affected files: `internal/system/network.go` (`leaveRun`, `snapshotContainsLocked`),
  `internal/snapshot/capture.go` (`CaptureHeader`), `internal/converge/correction.go`
  (`resolve`)
- Guide: [Multi-player §3.2](multi-player.md#32-the-capture-boundary),
  [§3.6](multi-player.md#36-holding-a-session)

A hold, a rate and a run's end are in no capture, so a guest that loses the frame
carrying one hears of it from nothing else. A run's end it takes from its
authority's first epoch of the next (`leaveRun`); three cases stay open:
- a new game that waits for its host's press sends no epoch, so a guest the reset
  never reached plays the old game on until the press, then starts unheld;
- a run only counts up, so a guest a run ahead of its authority, a successor having
  taken the term before the reset reached it, refuses all that authority sends;
- a lost hold lasts until its host resumes, and a lost rate until the next one.

What would close them: the capture header carries the hold, its kind and the rate
beside the run, an install adopts them, and a whole world naming another run is
taken as a join's is.

### Correct a guest that joined while its link read as silent

- Priority: P2
- Affected files: `internal/converge/selective.go` (`publishManifest`, `silenceLimit`,
  `serveOne`), `internal/converge/correction.go` (`publishRound`, `takeKeyframe`)
- Guide: [Netcode design §2](netcode-design.md#2-the-correction-pipeline)

A person that rejoins at its pinned fork's `-relay` door can go seconds with no
authoritative world. Its link is in the relay's publication set while its run still
joins, so the manifests sent meanwhile go unanswered, and past `silenceLimit` it is
sent bodies alone, deltas. A keyframe the relay takes for its own lagging bot
(`serveOne`, `takeKeyframe`) moves their baseline past the one the person holds and
keeps the scheduled keyframe from coming due, so each is refused until the limit's
jitter lets a manifest through. A bot the person adds meanwhile waits in its relayed
start gate (`Corrections.ProvedWorld`) until a proof comes or, past
`NetworkJoinReadyTimeout`, the gate refuses it as behind and its seat dials again.
Probed runs of
`TestAPinnedForkTakesThePeopleItRelayedAlong` stalled 2 to 3.4 s in 3 of 5. What
would close it: manifests sent before a link's participant installed its gate world
count toward no silence, and a peer fed deltas alone is owed the keyframe a fallback
took.

### Re-measure the largest world's capture under the columnar codec

- Priority: P2
- Affected files: `pkg/network/snapshot.go`, `doc/services-and-networking.md`

`MaxSnapshotBytes`, `MaxSnapshotPlainBytes` and services §8 rest on a walled
500x250 world measured at 24.3 MiB plain and 1.1 MiB on the wire, figures from the
JSON-row codec the column keyframes replaced. Owed: that world's capture read again,
plain and on the wire, and the two ceilings and their comments grounded on it.

### Lay services-and-networking's tables out for a terminal

- Priority: P3
- Affected files: `doc/services-and-networking.md`

Twelve Markdown tables remain (§1, §3 twice, §6, §7, §7.2, §8 three times, §8's
succession messages, §12, §14), several with paragraph-long cells, which a terminal
shows as their source. Owed: nested lists where a table does not fit, per AGENTS.md's
Docs rules.

## Session membership

A suggestion for the words, not a decision: a *session* is one world timeline under
one authority — a fleet pod, a terminal host, any instance — and its participants,
and solo play is a session of one. Commands then move participants between sessions:
`:open`/`:close` in place of `:host`, since every instance already is its own
session's authority; `:join`/`:leave` for a participant; `:drop` for an authority
removing one; `:merge`/`:split` for whole sessions.

### A passphrase prompt in the TUI and the launcher

- Priority: P3, in a session of its own
- Affected files: `internal/mode`, `internal/app/loop.go`, `web/vif.js`,
  `web/index.html`
- Guide: [Auth and session design §3.5](auth-session-design.md#35-passphrase-doors),
  [§3.6](auth-session-design.md#36-admission-tickets-and-accounts)

A dial that meets a passphrase door with none is refused with the advice to join
with one, and a player retries with `:join <target> <passphrase>`: the refusal is
the prompt. A prompt proper would ask in place, in the command line's form on a
terminal and in the launcher's join row on a page, and dial again with the answer;
the same form takes `:login`'s name and password, which today ride the command
line and so the terminal's history. Implication: the account commands become
usable on a page, where a password on the command line is not. The same form is
where a phrase would be cleared: `:host <doors> passphrase` sets or replaces one
and nothing takes it off, so a run that set one opens every later door gated
until it restarts.

### Tighten what a relay and a successor are trusted with

- Priority: P2, in a session of its own; every mechanism it audits is in place
- Affected files: `internal/network`, `internal/app/internal/membership`,
  `internal/converge`, `tool/vif-router`
- Guide: [Auth and session design §3.4](auth-session-design.md#34-session-passes),
  [§3.5](auth-session-design.md#35-passphrase-doors),
  [Multiplayer §5](multi-player.md#5-membership-topology-and-authority-continuity)

The premise: a participant, once admitted, is trusted with the session. A relay
carries others' crossings and runs the admission exchange for newcomers; a successor
takes the term, signs the handoff record and issues passes; both hold the passphrase
door's verifier, with which a participant can test guesses offline at Argon2's cost
and stand in for the door to others. A host that took over through a router-published
door cannot publish again through the router, so its session is reached through
relays alone. The scope: list what each role can do that the admission did not grant
it, decide which of it needs a check (the authority confirming a relay's admission,
a successor's record carrying no more than the roster lets it, the verifier's reach),
which needs a limit, and which is accepted as the price of play without a server; then
plug the holes found, one PR each. A relay names the relay behind it that asked for a
join (`RelayJoin.Path`), which the authority holds only to naming nobody it has a
link to: a relay can ask as any participant behind another.

### Review what a relay hosts once its host is gone

- Priority: P2, an audit (2026-11-14)
- Affected files: `internal/app/internal/membership/host.go` (`LeaveSession`,
  `leaveSessionLocked`, `relayedLocked`, `sessionRoleLocked`),
  `internal/app/internal/membership/relay.go` (`reopenRelayDoor`, `returnAcceptor`,
  `relaySeatingLocked`), `internal/app/internal/membership/converge.go` (`redialDoor`),
  `internal/converge/authority.go` (`beginSuccession`, `driveWalk`, `endFollow`,
  `followGone`)
- Guide: [Multi-player §5.1](multi-player.md#51-succession),
  [§5.5](multi-player.md#55-leaving-a-session),
  [§5.6](multi-player.md#56-relay-doors-and-the-router); Tighten what a relay and a
  successor are trusted with, above

The concern, Lixen's: a relay in a session pinned to its host, which never chose to
be a candidate to host the whole game, now goes on hosting one for the people it
relays once that host is gone. That is fine in general, since running `-relay` already
shared its resources, and what passes through it, with those people, in a limited
scope; but it needs a second look that the boundaries hold. And the words no longer
fit what is built (2026-11-16): a relay acts as the authority of what is behind it,
its bots, the people it relays and theirs, without advertising it to anyone, where a
take-over is advertised to every participant and asked for at both ends.

What to decide first, as one definition in place of the cases: every run is the
authority of what is behind it and follows what is ahead of it, a fork is that
authority taking effect when the way up is lost, and a take-over is a run becoming
the authority of runs that are not behind it, which alone needs an address shared
and a player's word. One rule would then say whose place a run keeps, whose door
takes whom back and what a session advertises, where today the roles (`sessionRoles`:
host, successor, kept, seats, left), `relaySeatingLocked`, `relayedLocked` and
`leaveSessionLocked`'s `keepAll` each answer part of it.

What the fork does, keeping the places of those behind it and its `-relay` door for
them alone, is Multi-player §5.5 and §5.6; `./script/test.sh tree` runs it over three
terminals.

What to look at:
- Whether a relay hosts its people only once it is asked to, by a switch of its own as
  Take over as host is, off by default, or by the `-relay` flag's word, rather than by
  default. Nothing is shared that was not before: each person dials the address it
  already used, and nobody's address goes to anyone new.
- Whether the door, which asks no passphrase, should ask the session's, and what it
  trusts a returning player's record with.
- The fork issues no passes (Issue a pass in a session opened past its first term).
- Gaps seen while building it, none worse than before, when each person forked alone at
  once: a person whose link to the relay blipped as it forked is not kept, nor one
  behind a door of theirs that passed nothing on within the epoch window; a relay
  that takes longer than a succession window to come back to its own relay leaves
  those behind it to fork alone; a host that stalled rather than went takes its
  relay back, and the person redials that relay's new run, whose host may still hold
  the person's place as linked; and a door the router publishes is covered by no test.
- A holder is the joiner's own word at the host (`noteJoinerReport`), so a place a
  direct guest of the host claimed was a relayed person's bot is kept too, and that
  person's own `-relay` door could seat it.
- `:host <addr> migrate` on the kept session moves a policy its people already hold
  as pinned: `beginHostingLocked` guards only a successor asking to pin, so the
  people who came back at `returnAcceptor` give up at this host's loss while its
  newcomers walk the list. Refuse a word other than the session's, as there.
- A relayed join whose relay loses its uplink before the join crosses in may leave
  its roster entry on the host (`relayRelease` drops `tellAuthority`'s false), read
  in code alone: that key would then be refused through that relay as a stale link.

### Derive a door's passphrase off the world lock

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`beginHostingLocked`,
  `changeScenarioLocked`), `internal/network/admission.go` (`NewPassphraseDoor`)
- Guide: [Auth and session design §3.5](auth-session-design.md#35-passphrase-doors)

`:host <addr> passphrase` derives the door's verifier, Argon2id over 64 MiB, under
the world lock and `sessionMu`, where an account command runs its KDF in a
goroutine: the ticks it takes are dropped for every participant. It is no one-line
move, since `openSessionLocked` closes the door it finds. What would close it: read
`engine.ticks_dropped` across the command first, then open the session, derive off
the lock and serve the listeners under it again.

### Widen how a guest offers to take a session over

- Priority: P3
- Affected files: `internal/app/internal/membership/converge.go` (`TakesOver`),
  `join.go`, `internal/converge/reach.go`, `internal/network` (`ChainEntry`),
  `internal/mode` (`handleJoinCommand`, the join form)
- Guide: [Multi-player §5.0](multi-player.md#50-who-listens-who-authors-who-can-be-reached),
  [§5.3](multi-player.md#53-reachability-under--authority-migrate)

A session migrates only where its host asked, and only to a guest whose player
turned Take over as host on: a saved switch, off by default, read as the run joins
(§5.3). That was chosen over two other shapes, and leaves two gaps.
- **Asked at each join, not taken.** A word on `:join`, `-join` and the join form,
  nothing saved: no standing exposure, since each session gets its own yes, but
  typed every time, and the menu's join rows, the page's launcher and Android each
  grow a field. The saved switch won as one habit in the place Relay for others
  already lives. It reopens if a switch left on from a private game is found
  sharing an address with a public one.
- **The host's migrate covering its guests, not taken.** Guests of a migrate
  session declare a port unless they opt out, as before this rule: least change,
  and a session never lacks a candidate, but a phone is exposed without its own
  yes, which is what the rule exists to stop. It reopens only for a session whose
  players already see each other's addresses, as one found on the local network.
- **The switch is the next session's.** Turned on in a session, it binds nothing
  there: the port is bound and declared as the offer is answered. The chain is
  append-only, so a late declaration is legal; it needs the bind moved behind the
  gate (`Reach`) and a message, or the join's declaration sent again.
- **Relaying needs the port taking over binds.** Relay for others takes a dropped
  guest's rejoin at the run's own port (`admitHosted`), which a guest that does
  not take over never binds, so the switch alone now carries nobody but those at
  its `-relay` door. A chain entry naming a door and no address, as a leaf's
  `-relay` door has (`SuccessionChain.WithDoor`), would publish the port for
  rejoins without making the run a candidate.

### Keep a site's certificate pin for the session it joined

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`joinSessionLocked`),
  `membership.go` (`RestartAsk`), `internal/app/config.go` (`restarted`),
  `internal/app/internal/config/membership.go`
- Guide: [Multi-player §5.6](multi-player.md#56-relay-doors-and-the-router),
  [Auth and session design §3.5](auth-session-design.md#35-passphrase-doors)

A site hands out a door's certificate fingerprint with its link, and the dial pins
it where `-ca` named nothing (`joinSessionLocked`, `link.TrustTLS`). The pin lives
on the dial's own copy of the config: the run the join builds holds none, so its
redial of that door after a dropped link verifies against the system roots and a
door the router certified itself is refused, the run forking where it should have
come back. What would close it: the restart carries the pin (`RestartAsk`) into a
field of its own, which the session's dials read ahead of `Trust` and the next
`:join` clears, so one site's pin never answers for another's door.

### Decide what going away ahead of a loss costs

- Priority: P2, a rule of play to choose before anything is built
- Affected files: `internal/system/cursor.go` (`restore`, `wraith`),
  `internal/system/meta.go` (`publishDefeatState`),
  `internal/app/internal/membership/session.go`
- Guide: [Multi-player §5.4](multi-player.md#54-wraiths)

A Wraith is out of play: nothing reaches it, a session counts none toward its
defeat, and its player comes back with what the cursor held where the game is
still the one it left and the record reads no defeat (§5.4). A reset ends that, so leaving ahead of a defeat
brings nothing into the next game. Inside one game it still pays: a player who
drops its link as its cursor is about to die waits untouched and returns whole,
while those who stayed took the damage. The choices, none of them the netcode's:
- **A return costs something**: the record held to a floor, or a wait before the
  place is given back. Small (`CursorSystem.restore`, the door), but an honest
  drop pays it too.
- **A Wraith stays in play for a grace**: the world reaches it for some seconds
  after its link goes. Closes the dodge exactly, but the authority has to drive a
  cursor it does not own through the grace (D-13), and the Wraith's stores rest on
  it holding nothing.
- **Leave it**: a drop is a drop, and friends police themselves; a public session
  is where it would bite.

### Drop a participant and drain from the router

- Priority: P3
- Affected files: `tool/vif-router` (a loopback listener, `allocator.go`),
  `internal/app/internal/membership/host.go`, the pod's `/health` neighbor,
  `deploy/guest/vif-router.env`, `deploy/update.sh`
- Guide: [Auth and session design D16](auth-session-design.md#0-decisions),
  [§3.4](auth-session-design.md#34-session-passes),
  [Session fleet §9](kubernetes-fleet.md#9-the-session-routes)

`:player drop <slot>` drops a player with its bots, or one bot, keeps the dropped
keys and account out of the session at every door and tells the dropped person, who
plays on alone as after `:leave`; a dedicated host has no console to type it on. The
operator's authority is a loopback listener of the router's (`-operator-listen`),
reached over ssh and nothing else: the sessions with their participants by slot and
subject; a drop of one by slot, key or account, carried to the pod on an operator
port the network policy admits from the node alone, where the pod runs the drop
its console would; and a drain that stops creates, lets the sessions end and
reports when the fleet is empty, which `update.sh`'s empty-fleet gate reads
instead of polling the listing. Implication: no operator account exists, so the
listener binds loopback and takes no credential; its routes are not under
`/vif/api/` and the edge never publishes them.

### Name a fleet session's creator to its pod

- Priority: P3
- Affected files: `tool/vif-router/server.go`, `tcp_proxy.go`,
  `internal/network/admission.go` (`AdmitCreate`), `internal/app/internal/membership`
- Guide: [Auth and session design D12, D16](auth-session-design.md#0-decisions),
  [§3.6](auth-session-design.md#36-admission-tickets-and-accounts)

A fleet session is created by whoever posts to `/vif/api/sessions`, and the pod
knows nothing of them: on a dedicated host nobody may `:player drop`. The create
ticket: when an account creates a session, the router mints a ticket of kind
`create` naming the account and hands it out with the link; the creator's first
dial shows it beside its play ticket, the pod records the account as the session's
owner on the roster entry, and the owner may drop a participant, as a host's console
can, and nothing more. An anonymous create names no owner, and the operator's
route (above) stays the only other authority. Open: whether an owner's right
outlives its own departure, and whether a later `:login` can claim an unowned
session.

### Take the auth readings

- Priority: P2
- Affected files: `tool/vif-router/tcp_proxy.go` (`splice`, `stallBounded`),
  `script/`, `doc/auth-session-design.md` §8
- Guide: [Auth and session design §8](auth-session-design.md#8-ten-thousand-routed-players-on-one-machine),
  [D10](auth-session-design.md#0-decisions)

What the design projects and has not measured: the router holding 10,000
connections through a stand-in door on a second process, its RSS and CPU before
and after moving the splice's stall deadline off the writer so TCP-to-TCP copies
take the kernel's `splice(2)` path, the admission exchange's cost per connection,
and `-route-max` and `routePlacing` tuned from them; the passphrase door's Argon2
(64 MiB, three passes, one lane) timed on a page and a phone, since a dial pays it
once a join; the Android binary's growth from `lixenwraith/auth`, which the native
and wasm readings put under one percent. The identity service's own readings (RSS at
10,000 verifiers, the launch-day introspection rate) are its repository's todo.
Implication: the budget in §8 stands as a projection until these are in; a reading
that misses it changes `-route-max`, `MemoryMax` or the splice before anything else.

### Give up a place to a newcomer while the session is held

- Priority: P3
- Affected files: `internal/app/internal/membership/session.go` (`yieldLocked`,
  `assignFor`), `internal/system/network.go` (`expireWraiths`, `addParticipant`)
- Guide: [Multi-player §3.6](multi-player.md#36-holding-a-session),
  [§5.4](multi-player.md#54-wraiths)

A held session takes a player back (§5.4) and admits a newcomer to a free place
(§3.6), but a full one gives no place up. The yield asks the authority to dismiss
the Wraith away longest, which it crosses from a tick (`expireWraiths`), and a held
session takes none: the dial is refused `RefusalYielding` until the hold ends, and
a player crossed out under the hold is not away to a newcomer at all. It was left
out of the held return because it is another rule, not the door's reading: a
place's identity and slot return to the pool as its dismissal applies
(`leaveRoster`), so the door would hand out a slot the newcomer's world still holds
as another's cursor, its arrival crossing behind the dismissal, and the dismissed
player's own key, its bots' places and a new game under the hold each need an
answer. Decide first whether a paused session should turn anyone out for a newcomer.

### Take a returned run into its successor's session

- Priority: P3
- Affected files: `internal/converge/authority.go` (`adopt`, `driveWalk`,
  `takeBack`), `internal/app/internal/membership/converge.go` (`takeOwnPlaceLocked`)
- Guide: [Multi-player §5.1](multi-player.md#51-succession),
  [§5.4](multi-player.md#54-wraiths)

A run that takes its place back under a hold waits for its own arrival (§5.4),
which only the authority it came back through scheduled: a held host flushes
nothing, so no successor's group holds it. If that authority goes first, the run
gives the session up and plays on alone in its own cursor (`arrivalLost`), and its
place waits as a Wraith in the successor's session for its key. Following the
successor instead left it in the session for good, its cursor live and driven by
nobody (a socket probe: host, successor and a guest that dropped and came back
under the hold, then the host closed). What would keep it in: the successor gives a
survivor that reports itself waiting a departure and an arrival of its own term,
which is the walk "Hear a participant again once its link reopens" gives a
reopened link (`CrossReturns`); do it with that item.

### Keep a Wraith's stats in the session

- Priority: P2
- Affected files: `internal/app/internal/membership/place.go`, `identity.go`,
  `host.go`, `internal/system/cursor.go`, `internal/system/network.go`
- Guide: [Multi-player](multi-player.md#54-wraiths) §5.4

A player coming back to its Wraith brings its cursor's stats itself
(`event.CursorRecord`: energy, heat, health and loadout): the last owner-state sync
its run made in that session, frozen once the run leaves it, so play in a fork since
never comes back. This is a compromise. The values are owner-authored
([D-13](domain-design.md#d-13--owner-authored-shared-values-have-one-writer)) and a
Wraith gives up everything the world reaches it through, so neither the host nor the
shared world keeps them, and the session takes the returning run's word for them.
`CursorSystem.restore` takes a record only of the game still played (`CursorRecord.Run`
against the queue's run, which a reset advances), inside the ranges a sync is held
to (`heldInRange`) and reading no defeat, so a reset's new game, an edited record
and a cursor that left down all leave the template. Inside those the word is still the returning run's, as a live sync is its
owner's: energy has no bound, and an edited record names any game.

The fix shares some data of every cursor, so the session holds what each Wraith
resumes with and a successor inherits it. That moves those values off the one-writer
rule (D-2, D-13) for every system that writes them owner-side, and costs bandwidth,
which is why it waits. A cheaper step, a Wraith keeping the copy the sync committed
at its departure tick, is outside the hash and the repair (D-13), so nothing holds
the instances' copies equal.

The record is built to be replaced: one type, made in one place
(`NetworkResource.OnOwnState` into the place keeper) and read in one
(`CursorSystem.restore`), carried by the arrival crossing the coordinator fills
(`ParticipantJoinedPayload.Record`). What would close it: decide which values the
session holds, have the coordinator fill the arrival from its own world instead of
the joiner's report, and drop the record from the keeper and the report.

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
the redirect and an authority, for a pod a router request, that starts from a
transferred world instead of a scenario.

### Run a scenario with no players

- Priority: P3
- Affected files: `internal/app/internal/membership/session.go`,
  `internal/app/serve.go`, `wad/scenario/base/td`
- Guide: [FSM and configuration](fsm-and-configuration.md#3-root-schema) §3

`min_players = 0` is accepted and, like 1, waits for one player. A game that runs
with nobody in it, such as td's two sides attacking each other's towers, needs a
lobby quorum of 0 so a server starts without a guest, `holdVacant` parking only
below the scenario's minimum, the ownerless boot cursor dropped without a park, and
spawns that do not anchor on a cursor: the gold cycle fails and retries with none.

### Issue a pass in a session opened past its first term

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`openSessionLocked`),
  `internal/app/internal/membership/session.go` (`issuePassLocked`, `checkPass`),
  `internal/converge/authority.go` (`extendTermKeys`)
- Guide: [Multi-player §5.4](multi-player.md#54-wraiths) (the pass)

A pass names the session's first authority key, `TermKeys[0]`. A session a run opens
after `:leave` or a fork starts at the next term with no term keys, so `Open` fills
the earlier terms with empty keys: `issuePassLocked` signs over an empty authority
key and fails ("pass not issued", logged for every bot the leave seats again), and
`checkPass` holds every pass shown to that empty key. Nobody there is given a pass,
so a passphrase `:host` sets on such a session later spares no redial its phrase.
What would close it: a pass names the first key the session's own terms hold, or a
reopened session keeps its own term's key first.

### Publish a new session's link once its port runs

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`openSessionLocked`),
  `internal/system/network.go` (`publishConnectionTelemetry`)
- Guide: [Multi-player §7](multi-player.md#7-diagnostics-and-operations)

`openSessionLocked` activates the session, which publishes `network.state`, before
it starts the port, so a port not yet running reads `down` from inside the command
until the next tick. The frame loop draws between ticks, so `:host` on a solo run,
`:bot add`, `:leave` and a fork can show `Net: down` until that tick, and after
`:host` the badge's 500 ms hold (`StatusNetworkHoldDuration`) keeps it up before
`Net: wait`; where the next tick reads `idle` the badge goes at once. Read in review,
not seen on a screen. What would close it: publish again once the port runs, or
start it before the session activates.

### Say what a run alone after a loss is

- Priority: P3
- Affected files: `internal/app/internal/membership/host.go` (`sessionSummaryLocked`),
  `internal/system/network.go` (`reportDisconnect`, `publishConnectionTelemetry`),
  `internal/app/loop.go` (`setAway`), `internal/engine/world.go` (`LatchSession`)
- Guide: [Multi-player §5.5](multi-player.md#55-leaving-a-session),
  [§7](multi-player.md#7-diagnostics-and-operations)

A fork, a `:leave` and a dropped player go on in a session with no door, which
shows no badge and keeps the places §5.5 says. What the run says of itself is
still off:
- `:session` reads `Session hosting` there, in a browser too, which cannot host,
  and a run frozen for a redial or a walk reads "continuing locally from the last
  authoritative state".
- A relayed person cut off from its relay is told "Host connection lost", though
  its host may be alive, and reads `Net: down` for 10 s and then nothing while it
  redials the relay and walks.
- A run with its own bots shows their in-process links as a network session's,
  `2P:0 4ms`.
- On Android, a fork made while the player is away plays on unpaused, since away
  is applied only when it changes (`setAway`); and a guest the phone froze past its
  host's Wraith wait forks on thaw saying its host connection was lost, though the
  host never went quiet.
- A replay's badge reads the journaled session latch (`publishConnectionTelemetry`),
  which says only whether a peer is linked, so a replay never reads `idle` or
  `waiting`: where the run showed no badge (`idle`) or `Net: wait`, at a fresh door or
  at one opened where the run's bots had been, the replay shows `Net: down` for 10 s,
  then nothing. The latch would carry what the port reads (`idle`, `waiting` or
  `down`) as it carries `Peers`, for presentation only, at the cost of a journal
  field.

### Keep the other guests' places in a host-policy fork

- Priority: P3, an option not taken (2026-11-14)
- Affected files: `internal/app/internal/membership/host.go` (`LeaveSession`,
  `leaveSessionLocked`), `internal/app/host_test.go`
  (`TestAPinnedForkKeepsNobodyItDidNotBring`)
- Guide: [Multi-player §5.5](multi-player.md#55-leaving-a-session),
  [§5.4](multi-player.md#54-wraiths)

The choice made: a fork in a session pinned to its host keeps its own place, its
bots' and those of the people it relayed (Multi-player §5.5), so the host's Wraith,
the host's bots and every other guest's place go at the fork. Another guest of that
host that later dials the door the fork's `:host` opens comes in as a new player,
without the cursor and record it had in that world.

What the other option does: the fork drops only the lost host and its bots, the
places of the authority `giveUp` leaves named (`m.authorityID()`), and keeps every
other place as a Wraith for `WraithTimeout`, which `:host` starts over. A guest that
dials a door opened in that time takes its place back by its key, as it could before
this rule, when those Wraiths waited for a door with no limit.

What it entails: those Wraiths stand on the fork's map for their 2 minutes whether
anyone comes back or not, as the host's did in the report that led here. A return
takes the fork's player opening a door and the other guest finding it: a host-policy
guest has no door for them, its `-relay` door taking back the people it relayed alone,
nothing tells the others its address (§5.0), and local network
visibility is off by default (§5.7). The three-player test inverts, and §5.1 and
§5.5 change with it.

Why it was not chosen: nobody in a host-policy session signed up to come back to a
guest's run, any more than that guest signed up for its host's Wraith, and keeping a
session's people together is what `-authority migrate` with a guest that takes over
is for.

Reopen it if players of a host-policy session regroup in one guest's fork often
enough that losing their progress there is felt.

## Combat

### Let mounted weapons strike more than cursors

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/profile/combat.go`

A mounted ray destroying walls and glyphs, mounted weapons striking other species, and each instance's own drains; Shared outcomes must resolve from Shared geometry, not a Player-domain shot.

### Let cursor weapons strike other cursors

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/system/bullet.go`, `internal/system/pulse.go`, `internal/system/interaction.go`, `internal/profile/combat.go`

PvP: a hit on a remote cursor crosses as its impact and the victim's owner applies it, as `strikeCursor` does for mounts.

A cursor then takes what an attack family does rather than a fixed `CursorDamage`, and a mount's pulse already carries its family for it (`PulseSpec.Attack`, `Charged`, set per mount), so mounted weapons gain the same effects:
- Profiles with `CombatEntityCursor` as the defender, a row per family that may strike a cursor; `strikeCursor` resolves one, a mount striking as its host's combat row.
- Owner-authored cursor statuses (D-13): a stun the owner's input and its D-18 prediction obey, never refreshed while it runs; a burn ticking heat on the owner each period.
- Knockback as a push: the owner moves its own cursor along the impulse through `PushCursorMove` to a free cell, or a Shared pulse re-derives the move on every instance, as the cave rock's push does.
- Per-attacker, per-family immunity on the cursor, as species keep it, so a sustained pulse cannot stun-lock.

### Place a pulse on a map cell

- Priority: P3
- Affected files: `internal/system/mount.go`, `internal/event/payload.go`

`EventMountRequest` naming a cell and no host places a bare Shared anchor carrying the mount, so a scenario can lay a sustained, hidden pulse over part of the map, such as one that pushes cursors back once they take pushes; a map setup's clear and a request remove it.

### Let a scenario shape the cursor's pulse

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/event/payload.go`

A request overriding the disruptor's `PulseSpec` for every cursor field by field, as a mount's is, carried in the weapon system's local save as the gun's aim is (`EventGunAimRequest`).

### Let a scenario mount and shape a shield

- Priority: P3
- Affected files: `internal/system/shield.go`, `internal/system/interaction.go`,
  `internal/event/payload.go`

A request naming an entity, a radius and a color, so a scenario mounts a shield on
a tower or a wall and sizes or tints it from what it watches, as a cursor's energy
turns its shield on and off and sets its polarity. `ShieldComponent` carries both,
the capture with it, and any shield draws at them; missing are the request, which
the entity's owner applies as a cursor's owner writes its own shield (D-2), and a
contact rule: `CursorContactAt` reads cursors' shields alone and only a cursor's
drains energy, so a mounted shield guards nothing yet.

### Lower the shields a scenario's switch-off leaves up

- Priority: P3
- Affected files: `internal/system/shield.go` (`HandleEvent`)
- Guide: [Gameplay §5](gameplay.md#5-energy-heat-boost-and-shield)

A scenario that turns the shield system off, by its toggle or by ending a region
that runs it, leaves a raised shield up: drawn, its ping bounds held and its drain
stopped, and a deactivate, the energy system's at zero energy or a scenario's own,
is refused while the system is off. The tutorial's Visual lesson lowers the shield
in its exit before it ends its region, so nothing stands there. The smallest fix
lowers each shield its owner simulates as the command turns the system off, as
`setActive` does (D-2).

## Species

### Let a target group anchor more than eight

- Priority: P3
- Affected files: `internal/parameter/navigation.go` (`MaxTargetsPerGroup`)

A group holds eight anchors, so past eight players the cursor group leaves cursors
out. A cap of 16 closes it.

### Let a player mechanic or a program fuse composites

- Priority: P3
- Affected files: `internal/system/fuse.go`, `internal/event/type.go`,
  `internal/system/behavior.go` (`EmitEvent`)
- Design: [D-3](domain-design.md#d-3--cross-the-smallest-shared-outcome)

A fusion takes unit composites only from a Shared request, one a region raises on
every instance, each consuming them alike. A participant's own request, pushed
player, takes its drains alone and fails naming a composite, and a program's
`EmitEvent` takes no stamped event, so neither a player mechanic nor a program can
fuse composites. A player mechanic would cross the Shared outcome as a bus artifact
naming the composites and the target's landing, barrier-bound since it retires
Shared entities; a program's request would push the request Shared, as a region's
does.

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

The colored markers `g` and a direction show before a color jump are placed once
(`showColoredMarkers`), so under a drift they sit on cells the glyphs have left.
Checking them every tick, as `validateBaseMarkers` checks the base markers, would
keep them on the text.

### Widen what drifts

- Priority: P3
- Affected files: `internal/system/environment.go`, `internal/parameter/environment.go`

Composite members (gold), particles, nuggets and species stay put; a side-scroller
may want some of them carried with the nearest layer. Decay and blossom change a
glyph's level, so a glyph they touch changes layer and speed mid-flight. In 256
colors normal and bright glyphs share one console entry, so only the dark layer
reads as farther; a distinct attribute for bright would separate the three.

## Rendering

### Console follow-ups

- Priority: P3
- Affected files: `internal/mode/console.go`, `internal/engine/overlay_geometry.go`,
  `internal/render/renderer/overlay_console.go`, `internal/mode/overlay.go`

The console landed ([Input §9](input-and-modes.md#9-command-mode)). Open:

- `StatusAnnounce` has its rank and colors, but nothing posts one: a server's word
  to its players needs a message on the wire first.
- A finger scrolls the console, as it does help, only on its one-cell scrollbar; a
  drag over its rows would suit a phone better.
- Help and telemetry still center on the whole screen under a touch keyboard, whose
  covered rows only the console keeps above (`ComputeConsoleGeometry`).
- With Wait to start off, `:n` typed on the console's line plays the new game behind
  the console: the reset's unpause (`Scheduler.executeReset`) ends the pause the
  console asked again. Closes with the reset asking it once more after its entry
  actions settle, while an overlay or the command line that pauses stands open.

The options Lixen did not take (2026-11-14), each with what would reopen it:

- Placement: a drop-down from the top, as Quake's, or help's centered window. The band
  at the foot keeps the newest line beside the status bar it came from and leaves the
  pinned HUD at the top-left in view. Reopen if a scenario needs its map's foot seen
  while the console is open.
- A line of the console's own for commands: command mode's line, opened under it,
  already edits, completes and recalls, and a second would be two mechanisms for one
  job. Reopen if the console comes to take input that is not a command, as chat.
- On a touch screen, a key strip button (the page has a strip, Android none, so the
  hosts would differ) or a tap on the bar's message (the hosts' triple tap reaches
  the run tap by tap, so telling a lone tap apart waits 500 ms). The menu's Console
  row and `:console` work on every host. Reopen when Android has a key strip.
- History kept in the state directory, as a shell's: its lines carry addresses
  (`:session`) and what the run did, which the address privacy rule keeps off the
  disk by default. Reopen if players ask for it across launches, behind a setting
  that is off by default.
- Under a touch keyboard, hanging from the screen's top while it is up (Go alone,
  but landscape still hides its foot), or staying under it.

### Configuration menu follow-up

- Priority: P2
- Affected files: `internal/mode/config_menu.go`, `internal/paths/settings.go`
- `[play]` persists the audio channels, sfx volume, auto-fire, mouse, pointer
  following and the HUD, and a scenario change carries them over. What `:new`
  keeps beyond those (rate, pins, standing breakpoints) a restart or scenario
  change still drops. A Music volume row under Sound, stepped as Effects volume
  is and kept as `[play] music_volume`, needs a music gain in the mixer with an
  authoritative readback first; the row then follows `sfx_volume`'s shape on
  every host, the native menu's stepper included. Keymap, backend, paths and
  color depth remain startup settings; `:d break` stays on the command line,
  since a row cannot take its arguments. A Keymap row choosing among the built-in
  table and the keymaps a name finds (`vim`) would reach them where no flag does,
  on Android and for a browser's player, and needs the key table rebuilt in a run.

### Camera edge follow-ups

- Priority: P3
- Affected files: `internal/engine/resource.go` (`EdgeCells`, `FollowCamera`),
  `internal/engine/world.go` (`PushPointerMove`)

One `[play] camera_edge` holds for every input ([Rendering §2](rendering.md#2-render-context-and-coordinates)).
The options Lixen did not take (2026-11-14), each with what would reopen it:

- The edge as a share of the shorter side, as far from every side: a wide screen's
  long sides get the short side's distance, so the view looks less far ahead there
  than the area's side gives. Reopen if wide screens feel cramped beside tall ones.
- The same share of each side: simplest, but a wide screen's sides sit further in
  than its top and bottom, so the view follows sooner sideways than up and down.
  Reopen if a percent should read as a share of each side.
- A narrower edge for a pointer than for keys, as before: a key after the mouse moved
  the view by the gap when the cursor sat between the two. Reopen with a camera that
  eases to its new place rather than jumping, which would smooth the change of hands.
- A cap on how far one pointer report scrolls: a pointer pushed into the edge scrolls
  by its depth each report, so a slow pass over the 15% edge scrolls about 280
  columns on a full terminal, against 36 over the old 8-column pointer margin. A cap
  leaves the cursor inside the edge, so the next key jumps the view again. Reopen if
  the edge scrolls too hard under a mouse or a finger.

### Give each shipped `[play]` key a comment of its own

- Priority: P3
- Affected files: `internal/asset/vif.toml`
- The `[play]` keys before `camera_edge` share one block above the table's header,
  which an upgrade never brings to a player's copy that has the table, so a key
  added there arrives bare and the block goes stale ([Filesystem §3](filesystem-layout.md#3-resolution-policy)).
  A comment line above each key, the block cut to what the table is, carries each
  later key's explanation with it; a copy keeps its old block until the player
  edits it.

### Drop a settings file's keys in one read

- Priority: P3
- Affected files: `lixenwraith/toml` (`Document.Delete`, `Document.Set`),
  `internal/paths/settings.go` (`UpgradeSettings`), `internal/input/keyconfig.go`
  (`UpgradeKeymap`)
- Each `Delete` reads the document again, so an upgrade dropping n keys costs n
  reads: a `vif.toml` or keymap holding 3000 unknown keys takes seconds to start.
  A `Delete` of many keys in one edit makes it one read. No file vif writes comes
  near; one a player pastes another program's settings into could. A `Set` over
  tables written under headers costs the same, a read a statement it sets: 300
  of 3,000 tables changed in one array took 2.7 s.

### Write a hard-linked settings file in place

- Priority: P3
- Affected files: `internal/paths/settings.go` (`FileStore.Store`)
- A store replaces `vif.toml` or a keymap by rename, which leaves a hard link's
  other names on the old file. Writing in place keeps every name but tears the
  file for a run reading it, or for good when the write is cut short; a link count
  above one could choose it, with the tear the price. The rename stays, as the
  write before the upgrades did.

### Keep what a system root's vif.toml names shared in the player's copy

- Priority: P3
- Affected files: `internal/paths/settings.go` (`FileStore.Load`),
  `internal/resource/resolve.go` (`Options.ApplySettings`)
- A system root's `vif.toml` naming a `[paths] root` or `keymap` outside the
  system roots makes it another's, read and never written. The user root's copy
  its first change makes names it too, as the player's own, so a run writes a
  keymap there back up to date where it may. The copy could leave out what the
  seed's `[paths]` names, or mark it; one inside a system root stays shared
  (`paths.Shared`).

### Let a run on a shared config root keep its changes

- Priority: P3
- Affected files: `internal/paths/settings.go` (`SettingsFor`, `LoadSettings`)
- A `-config-dir` naming the checkout's `wad/` or a system root reads that root's
  `vif.toml` before the user root's, while the copy the first change makes in the
  user root takes every later one, so such a run starts from the shared file each
  time. Reading the user root's copy first when there is one would keep them, at
  the price of the copy shadowing what `-config-dir` names; the shared file stays
  unwritten.

### Review the settings store's write path

- Priority: P1
- Affected files: `internal/paths/settings.go` (`FileStore.Store`, `storeTarget`),
  `internal/paths/owner_unix.go` (`keepOwner`), `tool/vif-settings`
- A start rewrites the player's own `vif.toml` and keymap, and `make install` the
  installed ones through `tool/vif-settings`, as root under `sudo`: by rename,
  through a link, keeping the file's mode and owner. No review of that path's
  safety has been finished. One reading the code, with tests only in temporary
  directories, closes it, asking whether the file a link names can change between
  `storeTarget`'s stat and the rename, what mode and owner the temporary file has
  before its `Chmod`, what `keepOwner` does in a run as root, and what a link into
  a directory other users may write leads to.
- Seen in an audit, read in code alone: `-check` says a run writes a file back by
  the file's type and mode (`WriteBack`), where `Store` also needs its directory
  writable, so on a read-only mount the two disagree; two runs saving at once each
  rename over the other's key, `saving` being one process's; and a crash between
  `CreateTemp` and the rename leaves a `vif.toml.<n>.tmp` nothing removes.

### Review when a start writes the settings and what it shows

- Priority: P2
- Affected files: `cmd/vif/main.go` (`upgradeOwn`, `sayNotice`),
  `internal/app/play.go` (`player.begin`), `internal/paths/settings.go` (`Shared`,
  `resolved`), `internal/parameter/audio.go`
- The last fixes to the upgrade have tests but no second reader, as their review
  stopped before it reported: the upgrade runs once the invocation is validated
  and its site session taken, so a refused run writes nothing; a replay, script or
  bot shows the notice on the status line; a shared root is found through links;
  and the `[audio] buffer_ms` bounds sit in `internal/parameter`, keeping headless
  and audio-free builds off the audio library. An independent reading closes it.

### Check in CI that a headless build links no audio

- Priority: P2
- Affected files: `.github/workflows/test.yml`, `Makefile` (`verify`)
- `make verify` fails when a `vif_headless` build of `cmd/vif` imports
  `internal/render` or the `lixenwraith/audio` package. CI builds the profile but
  never asks, so it passes a change that links either, as the `[audio] buffer_ms`
  check once did. The same `go list -deps` step in `test.yml` closes it.

### Use sixteen console backgrounds on Linux

- Priority: P3
- Affected files: `internal/render/console.go`

The framebuffer console shows a bright background under SGR 5, which vgacon blinks instead; telling them apart would give Linux FreeBSD's sixteen backgrounds.

### Paint a whole frame faster on Android

- Priority: P2
- Affected files: `android/app/src/main/java/com/lixen/vif/CellDrawer.java`

A zoom, a rotation and a resume repaint every cell, a rectangle and a glyph each
through a software canvas, and the frame's time follows the cells: on a Pixel 8
Pro (2026-11-20) 67 to 70 ms for 7,800 cells at 9.5 dp, 77 to 90 ms for about
13,000 at 7 dp and 146 to 164 ms for 29,000 at the 5 dp floor, 5 to 9 µs a cell,
where an idle frame's mean is 9 to 14 ms. Go's share is the encoding, 0.3 ms for
112x124 cells on amd64. Profile one on a device before choosing: a whole frame
composed straight into the picture's pixels, or a GLES drawer taking the same
frames.

### Post nothing to Android's render thread once it has quit

- Priority: P3
- Affected files: `android/app/src/main/java/com/lixen/vif/GameView.java`

`surfaceDestroyed` quits the render thread and keeps its `Handler`, so a resume
before the next surface (`away`) and a keyboard that moves then
(`keyboardCovers`) post to it: Android logs "sending message to a Handler on a
dead thread" and drops the post, which `surfaceChanged` makes up for. Clearing
`handler` there, which the UI thread alone reads, closes it.

## Audio

The audio library's own items are in [its todo](https://github.com/lixenwraith/audio/blob/main/doc/todo.md),
and those of stapes (formerly soundlab), the rip's among them, in [its
own](https://github.com/lixenwraith/stapes/blob/main/doc/todo.md). An item here
is vif's part of a change; a part the library or stapes makes is an item there,
linked from it. Stapes on Android, its live rip with it, is first an app of
stapes' own, its items in [stapes'
todo](https://github.com/lixenwraith/stapes/blob/main/doc/todo.md#android); vif's
app bundles it long after ([below](#bundle-stapes-in-the-android-app)).

### Measure the Windows sound on Windows

- Priority: P1
- Affected files: `cmd/vif/wasapi.go`, `doc/multi-platform.md` §8
- Guide: [Build profiles](multi-platform.md) §8

The window's speakers on Windows were measured under Wine alone: 28 ms from a
trigger to its sound leaving the device at a 20 ms period, Wine's own path
behind that. On a Windows 10 and a Windows 11 machine take the latency through
a loopback of the endpoint, the CPU against the null backend and ten minutes'
underruns at 50, 20 and 10 ms; unplug, disable and reformat the device mid-run,
sleep the machine, and play through a Bluetooth and a 44.1 kHz device. The same
runs settle what was left out for want of them: the multimedia class scheduler
for the render thread, one device period held instead of two, and
`IAudioClient3`'s shorter period. The bench that took the figures is not in the
repository; `sandbox/` is where it would live, built with the sink laid in by
`go build -overlay`.

### Feed the Windows speakers a shorter period by default

- Priority: P2
- Affected files: `cmd/vif/gui.go`, `cmd/vif/main.go` (`buildConfig`),
  `internal/asset/vif.toml`
- Prerequisite: the measurement above

The mixer's period is `[audio] buffer_ms`, which the shipped `vif.toml` sets to
50 on every platform. Under Wine the Windows speakers played a trigger 16 ms
sooner at the median and 34 ms at the worst at 20 ms, for 0.8 % of a core, with
no underrun in ten minutes; on Linux 20 ms gained nothing on PulseAudio and
dropped out more under load. Closes with the owner's decision once Windows is
measured: a period the window's speakers name where a player's `vif.toml` names
none, which needs the shipped file to stop naming one, or 50 ms kept.

### Follow the default output device on Windows

- Priority: P2
- Affected files: `cmd/vif/wasapi.go`

A stream opened on the default endpoint stays on its device when Windows'
default changes while that device remains (a headset plugged in beside working
speakers): the game plays on the old one until it is restarted. Have the render
thread ask the enumerator for the default endpoint's id once a second and
reopen when it is no longer the one it opened.

### Keep the mixer on the device's clock

- Priority: P2
- Affected files: `lixenwraith/audio` `mixer.go`, `cmd/vif/wasapi.go`,
  `cmd/vif/pulse.go`, `cmd/vif/alsa.go`

The mixer renders on a ticker, the system's clock, and a device plays on its
own: one that runs fast drains what its sink holds and plays a period of
silence every 10 ms / drift, every 100 s at 100 ppm, through any backend, and a
slow one fills whatever queue is there. Let a host's speakers ask the mixer for
each period, or have a sink that reports it blocks pace the mixer, so that the
mixer always waits on the device.

### Render nearer the device's call on Windows

- Priority: P3
- Affected files: `cmd/vif/wasapi.go`

A write returns as the device takes its last frames, so the mixer renders a
whole device period before the device asks again. Returning 6 ms later took
10 ms off the median at a 20 ms period under Wine and cost 30 underruns at
10 ms, where the device holds no spare period: hold a write only while the
device keeps one, and measure it on Windows.

### Hear the build's own speakers on real devices

- Priority: P1
- Affected files: `cmd/vif/pulse.go`, `cmd/vif/alsa.go`, `doc/multi-platform.md`

The stream was measured on null sinks in a container, the card in a VM on
`snd-aloop`. Owed, each beside `-ab pacat`: an hour of play on an ALSA device
under PipeWire and under PulseAudio with the stream's latency read every minute
(`pactl list sink-inputs`), which shows a device clock's drift (a pipe should
creep toward its 371 ms on a slow one, the stream stop at 112 ms); a Bluetooth
sink; the first seconds after the device was suspended; ten minutes with nothing
else running, counting dropouts; the game's CPU on a GPU; the game started by
Steam, on a PulseAudio host, a PipeWire host and a Steam Deck (the runtime was
checked in a chroot); FreeBSD without `/dev/dsp`; the card on a machine with no
sound server, at 44.1 and at 48 kHz, a USB card unplugged and plugged back, and a
sleep mid-run.

### Render a conductor timeline offline

- Priority: P3
- Affected files: `tool/vif-log`, `internal/system/music.go`

The conductor's timeline, patterns by tier from an APM trace, lacks a path to
the engine's offline render, `AudioEngine.RenderBars`, which takes a tempo a
bar or a beat as the conductor's slew needs. A verb of vif's, since stapes
imports nothing of the game, would drive an APM timeline through the
conductor's tier and tempo policy over it and write the WAV.

### Play a song in the game

- Priority: P3
- Affected files: `internal/system/music.go`

The conductor plays tiers, not bars. A scenario that wants a fixed piece would play
a `SongDef` through the sequencer as stapes conducts one, each bar's patterns
scheduled while the bar before plays.

### Bundle stapes in the Android app

- Priority: P3
- Affected files: `android/game`, `android/app`, `android/go.mod`,
  `doc/audio.md`, `doc/package-map.md`
- Prerequisite: stapes mobile fully working on its own ([stapes'
  todo](https://github.com/lixenwraith/stapes/blob/main/doc/todo.md#android))

A later version of the app carries stapes beside the game, its live rip with
it, over one storage, so a pattern written or ripped on the phone plays in the
phone's game from the user root's `audio/`. `android/go.mod` then requires
stapes, and the root module still imports none of it. What closes it: the
start screen offers stapes beside the game, as stapes mobile runs it.

### Tune the binaural beat by ear

- Priority: P3
- Affected files: `internal/parameter/audio.go`, `internal/parameter/music.go`
- Guide: [Audio §2](audio.md#2-adaptive-game-conductor)

The binaural beat's figures were set without hearing it. Each is a constant to
move after a session on headphones, which stapes' `binaural <carrier> <beat>
<level>` gives over any pattern: the level (`BinauralLevel`, 0.03 of full
scale), the carrier's octave (`BinauralCarrierFloor`: 150 Hz gives E3 under the
conductor's E, 175 E4), the dynamic beat's lock to the tempo
(`BinauralTempoLock`: 8 runs 13.3 to 26.7 Hz, 4 runs 6.7 to 13.3) and its glide
(`BinauralGlide`).

The dynamic mapping is one function, `parameter.BinauralBeat`. The candidates
not taken, each a few lines there:

- **follow**: the beat rises with the APM on the tempo's square root, from
  `relax` at rest to `hyper` at the ceiling;
- **by tier**: one beat a tier, a row each in the preset table, through
  `TierTracker`'s hysteresis;
- **counter**: the beat falls as the APM rises, against the rising tempo.

## Engine

### Trim what a tick spends outside the position index

- Priority: P2
- Affected files: `internal/system/interaction.go`, `internal/engine/store.go`,
  `internal/engine/world.go`, `internal/event/queue.go`
- Guide: [Position §5](position-package.md#5-costs)

A td tick with sixteen cursors on its 500×250 map (`TestTickProfileTD16`, sixteen
runs merged) is led by costs outside the index, the math and the physics. Each
share is measured and none of its code changed:

- A species' contact with the cursors (`CheckCursorOverlaps`, `overlapOf`), 15%:
  every entity reads each roster slot's cursor and shield again before it tests
  its cells against them. Reading the roster once a tick is the smallest step.
- The stores' entity lookup (`sparseIndex.get`), 9.5% across the systems, a
  third of it under a position read.
- The route graph (`RouteSolver.Compute`), 12%, and the maze a reset lays again
  (`executeBatchSpawn`), 15%, a third of that `World.componentMask`, a Go map
  written once a component. A reset's tick takes about 20 ms, 15 of them the wall
  system's event.
- Three allocations every tick: a `Stamp` in `EventQueue.BeginTick`, one in
  `NextBoundary`, and the system list the membership's off-tick drain copies
  (`World.Systems`).

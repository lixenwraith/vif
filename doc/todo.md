# TODO

Scratch notes that travel with the repository. Each item states what remains and
the smallest thing that would close it. Delete an item when it lands; this file
is not a changelog. Source files do not carry parallel TODO comments, so a
code-originated item names its source here.

Priorities: P0 blocks a release or leads the feature development push, P1 is
wanted next, P2 is convenient follow-up, and P3 is an idea.

## Vanguard

The side-scroller's first play test (2026-10-27) found it sound for a first
iteration: its scroll reads as lag, its cave is narrow, its raiders are plain. The
refinements lead, grouped by area so an agent can take a group, beside the items
this task left and the navigation ones. Order: the machine's certain faults landed;
the raider types' play test next; the position package last, in a session of its
own. Until that lands, nothing here adds engine surface for rock or caves: a
cave's shape stays in `pkg/cave` and its scenario, the rock behind
`Positions.Rock()` and the wall system and renderers that read it, and a query a
footprint needs is built by its caller from the existing ones, so the move inherits
nothing new.

### Raiders

The prototype's look and the four raider types landed (invader, scarab, construct,
storm eye: one program each over `RaiderStates` in vanguard's `macros.toml`, drawn
at random by `RaidSpawn`, spawned directly with no fuse). What is left waits on a
play test in the opened cave.

#### Play the raider types and settle the shortlist

- Priority: P1
- Affected files: `wad/scenario/vanguard/behavior/`, `wad/scenario/vanguard/macros.toml`,
  `wad/scenario/vanguard/scenario.toml` (`RaidSpawn`), `doc/gameplay.md` §10
- Guide: [Gameplay §10](gameplay.md#10-embedded-campaign-progression),
  [FSM reference: Behavior Programs](fsm-reference.md#behavior-programs)

The types' numbers are starting values, hit points times chase speed 96 for each as
the eye types balance theirs (`parameter.EyeTypeTable`): the invader 12 at the
swarm's 8, the scarab 8 at the snake's 12, the construct and the storm eye 48 at
the drain's and the quasar's 2, the slow two carrying their threat in a long lock
and charge. Play them at the faster scroll and in the jagged cave, against a gun of
one shot forward, and retune hit points, lock and charge per type, and its speed,
acceleration and drag in its `[prototype.physics]`.
Candidates beside them, from `sandbox/swarm` by ID: Interceptor (1) and Bat-Wing
(12) for their wings, Chevron (18) mirrored to `<<<<` for a fast type, Hover-Tank
(7), and the flame and frost eyes; `BehaviorProgramsMax` (8) holds four more. The
aura and border sweep `sandbox/eye` draws around its eyes are a renderer effect of
their own, worth it only if the storm eye stays. Keep every type within 5×3 until
the position work gives each footprint its own navigation grid.

### Gun and loot

The gun path and the `U` this task added, moved from Combat.

#### Fan more than three gun lanes along a cardinal

- Priority: P3
- Affected files: `internal/parameter/tracer.go`, `internal/system/weapon.go`

A level of the gun's upgrade path names its lanes per cardinal, and
`TracerLaneOffsets` stops at the centre and its two sides, so a level asking for
more fires three. A wider fan needs offsets two or more rows or columns out, and a
volley spacing that still reads as lanes in a 15-row cave channel.

#### Aim a seat's gun as the scenario aims the player's

- Priority: P3
- Affected files: `internal/system/weapon.go`, `internal/event/payload.go`

`EventGunAimRequest` is local and sets the aim and path of the cursor this instance
drives, so a bot seat, or a cursor an instance adopts, fires the gun's own four
cardinals and stops at three charges. An entity field, as `EventKeyCaptureRequest`
carries, or a path the weapon system holds per scenario would reach every cursor
an instance simulates.

#### Declare loot kinds and drop tables in a scenario

- Priority: P2
- Affected files: `internal/component/loot.go`, `internal/parameter/visual/loot.go`,
  `internal/profile/loot.go`, `internal/system/loot.go`

Loot kinds, their rewards and runes are a Go enum and the drop tiers are fixed per
code species, so a scenario's own pickup is a Go change that reaches players only
as an event drop: vanguard's `U` is `LootUpgrade`, the gun drop's reward under its
own rune, raised by its FSM where a player shoots a raider down because no tier can
hold it. Kinds a scenario declares (rune, colours, reward) and tiers that name them
would let a program list its drops and roll them with pity, with `activeLootTypes`
and the pity counts sized from the declared kinds rather than `LootCount`.
[Give data species attack, beam and drop rows of their own](#give-data-species-attack-beam-and-drop-rows-of-their-own)
is the drop half of this for programs.

### Machine follow-ups

What vanguard met in the machine and is still open, its certain faults having
landed. Open items elsewhere it meets:
[payload field names at load](#check-payload-field-names-against-their-event-at-load),
[a program spawning another](#let-a-program-spawn-another-program),
[attack, beam and drop rows](#give-data-species-attack-beam-and-drop-rows-of-their-own),
[loot kinds in a scenario](#declare-loot-kinds-and-drop-tables-in-a-scenario),
[a seat's gun](#aim-a-seats-gun-as-the-scenario-aims-the-players),
[a one-tick defeat in a session](#forgive-a-one-tick-defeat-in-a-session-as-solo-play-does)
and [spawn placement](#audit-spawn-placement-across-the-species).

#### Give a program an entrance of its own through the fuse

- Priority: P1
- Affected files: `internal/system/fuse.go` (`handleBehaviorFuse`,
  `landBehavior`), `internal/behavior/program.go`, `internal/system/behavior.go`,
  `internal/system/materialize.go`, `wad/scenario/vanguard/scenario.toml`
- Guide: [Behavior machines §6](machine-design.md#6-data-species)

Vanguard's raiders spawn at once (`EventBehaviorSpawnRequest`), the fuse off with
the drains, because a program's entrance through the fuse is fixed: the swarm's
one-second beam over a footprint `FindFreeAreaInside` chose when the fuse was
asked, then a direct spawn at what the beam covered, and nothing of it reaches the
machine, which starts in `initial` the tick the body stands. Lixen expects
entrances to grow for machines (2026-10-27). What a program would declare:

- The beam: its duration and style, the swarm's or the nugget's until a program has
  [beam rows of its own](#give-data-species-attack-beam-and-drop-rows-of-their-own),
  and whether a countdown shows over it.
- An entering state the machine runs while the beam holds, so a program can
  telegraph, re-aim or cancel its arrival; while it runs the body neither strikes
  nor takes damage, as no body stands yet.
- A landing its probe chooses (`FindLanding` or its own), tried again when the beam
  ends, since rock scrolls under a beam as it does under a teleport.

The beam is Shared, so the entering state's clock and probe must run alike on every
instance and a capture mid-beam must restore them (`snapshot.Schema`); the drains'
fusions stay as they are.

#### Count a group of programs

- Priority: P2
- Affected files: `internal/behavior/program.go`, `internal/system/behavior.go`
  (`behaviorTelemetry`), `doc/telemetry-reference.md`

The raid's cap reads `behavior.count`, every data species, while raiders are the
only ones. A prototype `group` with `behavior.group.<g>.count` would let a scenario
that mixes programs cap each kind.

#### Draw a countdown in a smaller splash font

- Priority: P3
- Affected files: `internal/asset/splash_font.go`, `internal/system/splash.go`
  (`calculateTimerOffset`), `internal/render/renderer/splash.go`

A `Telegraph` countdown draws a splash digit, a 12×12 box whose ink spans at most
8×9, and is placed by that ink, taking the slot that overlaps the fewest blocked
cells when none is clear. In 16 headless vanguard games (8 seeds at 120×40 and 200×60, 2,400 ticks
each) 961 of 10,560 countdown ticks still ink a cell over rock, 3,538 of 272,730
inked cells. A smaller font, chosen by the body's size or the room around it, would
read in vanguard's narrowest cave (9 rows).

### Position, blocking and navigation

A full session of its own, last: it moves what the rest builds on.

#### Move Position into a package of its own, walls and rock one blocking model

- Priority: P0
- Affected files: `internal/engine/position.go`, `internal/engine/spatial_grid.go`,
  `internal/engine/rock.go`, `internal/engine/snapshot_world_gen.go` (generated),
  `internal/snapshot/digest.go`, `internal/system/wall.go`,
  `internal/system/navigation.go`, `pkg/navigation/composite.go`,
  `internal/render/renderer/rock.go`, and the 473 `Positions.` calls in 80 files (42
  in `internal/system`, 23 in the renderers)
- Guide: [ECS and events §3](ecs-and-events.md#3-position-and-spatial-queries),
  [D-12](domain-design.md#d-12--shared-geometry-may-inspect-both-domains),
  [D-14](domain-design.md#d-14--map-bounds-are-shared-authority),
  [Rendering §2.2](rendering.md#22-the-cell-aspect),
  [Navigation §3](navigation-design.md#3-passability-tiers-and-wall-changes)

`engine/rock.go` put one kind of environment into the engine, a cave's columns with
their stone look: right for a first iteration, wrong for an engine meant to outlive
this game, and hard to move because `Position` is the spatial index and the game's
geometry at once. It holds a dense row store and a grid of fixed-capacity cells (31
entities, Shared before Player); the wall queries, where `HasBlockingWallAt` reads
the rock, then each Shared occupant's `Wall` component; a `WallTest` grid per mask,
with the wall epoch and change log navigation reads incrementally; the area and line
queries (point and area line of sight, `FindFreeAreaSpiral`, `FindFreeFromPattern`,
`FindPlacementAroundExclusion`, `ScanLine`); typing's `TopGlyphAt`; batches and
`Shift`. It reads `World.Components.Wall` and `Resources.Config` (bounds,
`CellAspect`), so nothing in it stands alone.

Goals:

- A position package that knows no game: placement, the cell index and the geometry
  queries, given its masks and its blockers rather than reading the world. A 1:1
  cell for a graphical host, and later a third axis, enter there: authored geometry
  stays at `RefAspect` and the package owns the conversion.
- One blocking model: a blocker is a single cell (a wall entity) or a cluster (a
  field a system owns: a cave's columns, a maze, a composite wall), each with its
  mask and bounds, behind one surface for point, area and line tests and the change
  log. The cave's columns and look leave the engine for the wall system and
  `pkg/cave`, the look becoming the rock renderer's input.
- The rock's bounds check for walls where it pays: the rock answers an area by a
  column's bounds (`RockField.blocksArea`), not cell by cell; a maze kept as
  per-mask row bitsets (or runs) updated from the wall log answers an area or a line
  in words, with no component lookup a cell. td (500×250, narrow mazes everywhere)
  proves it and navigation with it: its passability
  (`CompositePassability.Compute`), the search grids `applyWalls` refills whole, its
  direct-path tests.
- Footprints: each navigator's own body, size and header offset, where today every
  composite navigates on one grid computed for the eye's 5×3 and the direct-path
  test centres the footprint and accepts it turned 90°
  (`HasAreaLineOfSightRotatable`): in 16 headless vanguard games (8 seeds at 120×40
  and 200×60, 2,400 ticks each), 1,988 of the 25,168 chase ticks it called direct,
  7.9%, had the raider's own body blocked on the way. The footprint registry of
  [Bound navigation's worst tick on busy maps](#bound-navigations-worst-tick-on-busy-maps)
  belongs here.
- A sparse or tiled grid for a ceiling-sized empty map
  ([Shrink the spatial grid on ceiling-sized maps](#shrink-the-spatial-grid-on-ceiling-sized-maps)).

What it must keep, across multiplayer, netcode and interactions:

- Determinism: Shared positions, each cell's order (`WorldOrder.Cells`) and every
  query's answer alike on every instance; the journal digest hashes positions, and
  the divergence harness and the cross-instance tests read as before.
- Domains: the Shared and Player halves of a cell (`ReservedPlayerPerCell`),
  geometry over both (D-12), bounds as Shared authority (D-14), prediction's private
  writes (D-18), the staging world's grid.
- Records: positions ship as rows and the index is derived; a cluster derives from
  its owner's record as the rock does, so an idle harness still matches main; any
  change to what ships moves `snapshot.Schema` and `JournalSchema`.
- Interactions: the wall push and crush (`displaceComposite`,
  `World.ShiftComposite`), drift's layer `Shift`, splash, loot and spawn placement,
  tracer and line walks, typing's top glyph, every renderer's read.
- Cost, measured before and after: `TestTickProfileTD16`, `BenchmarkCaveStep`,
  `BenchmarkCaveStepNavigation`, the renderer benches, the harness counts.

A full session at the highest effort (or ultracode): the plan first, each of its
choices confirmed before any code. The choices to settle early: the package's
boundary and what stays in `engine` (the `World` wiring, the generated snapshot
code); a cluster's representation (bitsets, runs, a summed-area table) and its
update path; whether a maze's walls stay entities a cluster indexes or become the
cluster; the coordinate type (2D with a third axis reserved, or 3D now); the
migration (a façade keeping the `World.Positions` surface while the call sites move,
then narrowed); the footprint grids' cost on td.

#### Audit spawn placement across the species

- Priority: P2
- Affected files: `internal/engine/position.go` (`FindFreeAreaSpiral`,
  `FindFreeAreaInside`), `internal/system/swarm.go`, `internal/system/quasar.go`,
  `internal/system/eye.go`, `internal/system/snake.go`, `internal/system/gateway.go`,
  `pkg/vmath/physics/kinetic.go` (`ReflectBoundsX`, `ReflectBoundsY`),
  `wad/scenario/vanguard/scenario.toml` (`RaidSpawn`)

A data species places a spawn with `FindFreeAreaInside`, the spiral search from an
origin pulled in from the map's edges until the footprint fits, so a request names
an edge blind to the body. The code species still search `FindFreeAreaSpiral` from
the origin as given, with sizes their Go code holds, so one requested at an edge
lands where the spiral first fits. An audit of every spawn path (the species'
requests, the fuse's beams, the gateway, a scenario's spawns) would put each on the
one search over the footprint it spawns. The code composites reach the map's last
column and row by passing the cell past it to `IntegrateWithBounceStats`, as the
data species do; bounds inclusive in `ReflectBounds` for every caller, the
reflection checking the wall it lands on, would take that arithmetic from each.
The raid still spends nine actions before its request to name a row (two
`ConfigToVar`, `DecrementVar`, two `IncrementVar`, `ModuloVar`, `SetVar` and two
`RandomVar`, each draw's index bumped by hand); a map-relative anchor on the request
(an edge and an inset) or a draw advancing its own index would shorten it.

#### Bound navigation's worst tick on busy maps

- Priority: P2
- Affected files: `pkg/navigation/flowfield.go`, `internal/system/navigation.go`,
  `internal/system/loot.go`
- Guide: [Navigation §10](navigation-design.md#10-costs)

On td with eight walking cursors and seven snake heads a storm tick peaks at 30 ms
against 6.4 ms on average, and no wall change computes more than two fields in a
tick. A td tick with sixteen cursors on one core reads 0.55 ms
(`TestTickProfileTD16`), 42% of it the level resets' maze spawn and route graphs
and 13% `applyWalls`, the search grids refilled whole when a level's walls land.
What is left, for a need rather than a measurement: a per-tick field budget
in canonical group order, if a browser frame still stalls, and a footprint tier
registry deriving the grids to keep from the live navigators, so a data-driven
species' footprint costs one row. A 2×2 coarse field is not planned: it changes
the answer, and the shared grid already took the cost it would have saved.

#### Spend less on fields over a moving map

- Priority: P3
- Affected files: `pkg/navigation/flowfield.go`, `pkg/navigation/cache.go`
- Guide: [Navigation §10](navigation-design.md#10-costs)

A cell closing drops every field of its tier, so on a scrolling cave a navigator
out of sight recomputes its field after every column. Vanguard's jagged cave, a
column a tick, makes that most of the tick: with eight composites out of sight
(`BenchmarkCaveNavigation`, far) 410 µs a tick at 500×254, three fifths of it
settling fields, and 120 µs at 200×60. Two exact cuts remain, for a need rather
than a measurement. Settle a field only until every reader's cell is settled, resuming
when a farther cell is read: a Dijkstra's settled cells do not depend on where it
stopped, so an instance reading more of it moves nothing shared; it pays when the
readers out of sight are near their target. Repair a field where cells closed, as
an opening repairs it, re-settling the cells whose way ran through them: it pays
for a local change on an open map, not on the cave, where every bend moves each
column. Keeping a field across a closing for a few ticks is not planned
([Navigation §3](navigation-design.md#3-passability-tiers-and-wall-changes)): a
joiner computes on the grid it has.

## Bots

### Open fleet sessions to bots

- Priority: P2
- Affected files: the Hugo site's `vif-lobby.js`, `pkg/network/endpoint.go`,
  `internal/mode/config_form.go`, `tool/vif-router`, `deploy/guest/vif-router.env`,
  `deploy/k3s/10-quota.yaml`
- Guide: [Session fleet](kubernetes-fleet.md) §6

The router takes `bots`, but `bots_max` stays 0 until these hold:
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

### Expose a bot difficulty knob

- Priority: P3
- Affected files: `internal/app/seat.go`, `cmd/vif`, `internal/mode/config_form.go`,
  `tool/vif-router`

`actions_per_second` is the only knob, and nothing caps it. Options: a rate suffix
such as `-bot 2:default@6` that `BotSpecs` parses, so `-bot`, `:bot add` and the
menu share it; graph variants per level; or a ceiling the driver clamps every
graph to. The fleet request carries it once the site exposes one.

### Let a watched holder pause with only its own bots

- Priority: P3
- Affected files: `internal/app/play.go`, `internal/app/seat.go`

A watched bot with seats is in a session, so the viewer forces it unpaused and
ignores pause keys. A person's run pauses with only its own bots; the viewer's pause
key could start the session hold `:pause` starts when the holder is its session's
host, which already stops its seats at one tick.

### Name participants beyond their slot

- Priority: P3
- Affected files: `internal/network/session.go`, `internal/snapshot`,
  `internal/render/renderer`

A hexadecimal slot identifies a cursor for one session, and participants have no
names. A label needs a roster and join-reply field, a capture schema change to
reach late joiners, and a trust decision, since each peer would choose its own.

### Train bot graphs offline

- Priority: P3
- Affected files: `internal/bot`, `pkg/genetic`, `internal/system/nugget.go`
- Guide: [Bots](bots.md#fitness-and-comparing-graphs)

First decide how a graph declares bounded tunable numbers: a `[bot.params]` table,
or bounded macro parameters shared with the editor's macro metadata. A genome is
those numbers as a bounded vector; `pkg/genetic`'s `Engine` evaluates whole
headless runs over a matrix of scenarios and seeds, and `pkg/genetic/fitness`
scalarises the result. A solo run depends only on its seed, so evaluation needs no
mesh. Decide where trained values ship. Bot genomes never enter the in-game
genetic registry, which captures carry. Fitness weighs no nugget, since nothing
pushes `EventNuggetCollected`: push it in `NuggetSystem.collectNugget` and drop
that function's own `statCollected` increment, which the event's handler repeats.

### Adapt bots to the session's players

- Priority: P3
- Prerequisite: [Train bot graphs offline](#train-bot-graphs-offline)

A small model of each other participant from Shared state, keyed by participant
for the session and dropped at its end, and a bandit over trained genomes. Decide
what is modelled and whether players see it; nothing is persisted.

## Authoring

### Check payload field names against their event at load

- Priority: P2
- Affected files: `internal/fsm/loader.go`, `internal/fsm/std/payload.go`,
  `internal/fsm/std/args.go`, `internal/bot/mind.go`, `internal/resource/check.go`

A payload guard's `field` (bot's `Own` too), a transition's `capture_vars` and an
`EmitEvent`'s `payload_vars` name payload fields looked up only when an event
arrives, so a misspelt one fails its guard or is skipped while `-check` prints
`scenario ok` (verified on a td copy with `field = "specie"` and
`capture_vars = { xx = "dead_x" }`). Shipped scenarios and the default bot graph
name only fields their events carry (a scratch scan of all 174 names). The smallest
fix resolves each name with `fsm.FieldByTag` at load: against the trigger's payload
prototype where `compileTransitions` resolves the trigger, which the payload guard
builders then take, and against the decoded payload in `compileEmitEvent`; a payload
guard on a `Tick` transition or as an action guard, which see no payload, fails too.
A behavior spawn or cancel request naming no program of the scenario is the same
gap one level down: the behavior system drops it and counts
`behavior.unknown_program_rejects`. `ValidateScenario`, which holds the machine and
the compiled programs, can resolve each request's `program` against `[behaviors]`.

### Author the tonal instruments as data

- Priority: P3
- Affected files: `pkg/audio/voice.go`, `pkg/audio/sound_spec.go`, `cmd/soundlab`

The tonal voices (bass, piano, pad, lead, pluck) are a table in code, so a new
timbre or a retuned bass is a Go change and soundlab cannot audition one. A
`[[voice]]` table in `sounds.toml` with the same fields, loaded like a sound and
hot-swapped like one, would put them under the author's hand; the bass voicing
alternatives rendered for the music-smoothing review would then be a file edit.
The offline conductor render that produced those comparisons (an APM timeline
through the sequencer, no device) is a test-only harness in the review branch's
scratch repository, and would be a `soundlab` or `vif-log` verb.

### Build the map and scenario editor

- Priority: P2
- Affected files: `pkg/tomlmacro`, `internal/fsm`, `internal/manifest/schema.go`,
  `internal/mode`, `internal/render/renderer`, `internal/resource`
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
- **Vocabulary.** Action, guard and event pickers read `-schema`, which lists each
  vocabulary's actions and guards with their arguments and every event's payload
  fields ([Behavior machines §5](machine-design.md#5-dialect-kernel)).
- **Map layer.** Geometry is generated at run time (`EventLevelSetup`, maze rooms,
  `.vifimg` walls). A painted map needs a static form the digest covers, and
  `.vifimg` assets come from the `image/` category, which it does not and a joiner
  is not sent. Decide between a TOML wall encoding (rectangles or run-length rows)
  and admitting `.vifimg` to the scenario container under its bounds.
- **In the game.** An editor mode beside command and visual in `internal/mode` acts on
  a paused world and draws through an overlay like `:graph`. Applying an edit
  validates with `resource.ValidateScenario` and restarts on it as `:n` does, since
  a run plays one scenario. Editing inside a multiplayer session is out of scope.

In order: reference kinds, order and provenance; a read-only graph of a scenario's
regions, states and blocks; placing and editing blocks into editor-owned files; the
map layer and point markers; root `[[expand]]` blocks and library copies.

## Browser sessions

### Budget a fleet pod's joins per player

- Priority: P3
- Affected files: `internal/network/session.go` (`AdmissionLimiter`),
  `tool/vif-router`

A pod sees every player the router splices at the node's address, so its own
budget of 6 joins a minute is one per session rather than one per player: a
seventh join into one session within a minute is refused, whoever makes it,
though the router has already budgeted each player. Close it when a session
that busy appears, by exempting the node's address at the pod or by having the
router name the player to it.

### Add browser admission authentication

- Priority: P2
- Affected files: router/session adapter, website, future authentication
  dependency
- Prerequisite: every route carries real play without transport faults, the
  built-in WebSocket included; no auth layer goes in while that is being settled

Issue a short-lived, session-scoped admission credential after authentication and
consume it during the WebSocket handshake without putting it in page history or
logs. Creation takes one too: `network.RequestSession` is the terminal's side of
`POST /vif/api/sessions`, and already shows a 401 or 403 as the router's
message. Decide whether the expanded router remains the credential boundary or
is renamed/split before adding the planned `github.com/lixenwraith/auth`
Argon2-SCRAM dependency. A terminal's own door takes the same handshake: a
`-selfsign` door encrypts with no authority and a native joiner takes it as it is,
open to a man in the middle until the SCRAM exchange with `WithChannelBinding` over
the leaf certificate's digest authenticates both ends on that channel, as
`lixenwraith/chess` authenticates; `-ca` pins it meanwhile. A browser trusts a
self-signed door only by being told to: pinning from a page needs WebTransport's
`serverCertificateHashes`, which the route does not speak.

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

### Launch the page and Android without their start screen

- Priority: P3
- Affected files: `web/terminal.js`, `web/worker.js`, `pkg/browser`,
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
vif://lixen.com:7777/<game>`), the bare `vif://` link, and the browser's
`https://lixen.com/vif/wasm/?arg=-join=wss://…`. The app takes none of them short
of typing `:join` and the link on the soft keyboard. A Join button on its start
screen could read the clipboard (or a field to paste into), strip a leading `vif
-join` and the browser link's `?arg=-join=` wrapper, keep the `vif://` or `wss://`
link and join it as `:join` does; a `vif://` intent filter would open links
tapped elsewhere. Unchecked without an emulator or a phone, so not built yet.

### Warn in the site's lobby when a session runs another build

- Priority: P2
- Affected files: the Hugo site's `vif-lobby.js`, `.github/workflows/release.yml`

Each row of the router's session list names the build its session runs
(`state.build`), as the refusal a page of another build reads does. The site's
lobby can show it, and warn before a Play that would be refused once it knows the
page's own build, which the release's web bundle does not name yet.

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

### Publish a development AUR package

- Priority: P3
- Affected files: AUR packaging metadata and `doc/packaging.md`
- Prerequisite: establish the release-package metadata first

Publish `vif-git` alongside the release-based AUR package.

### Move the Android toolchain from sdkmanager to the Android CLI

- Priority: P3
- Affected files: `android/toolchain.sh`, `android/README.md`

The command-line tools `toolchain.sh` pins (22.0) warn that `sdkmanager` is
deprecated for `android sdk`, which ships in the same package. Switch `setup` to it
once its install, licence and SDK-root flags are settled, bumping the pinned build
and its three SHA-256s together.

### Fix the Windows zip's mouse, sound and status bar flicker

- Priority: P3
- Affected files: `Makefile`, `.github/workflows/release.yml`, the terminal
  module's Windows backend
- Guide: [Build profiles](multi-platform.md) §4

Played in Alacritty, the zip ignores the mouse, has no sound and flickers status
bar cells on redraw. Try the mouse in Windows Terminal, whose console host is newer
than the system's, to tell a console-host gap from a backend one; try a build
without `vif_noaudio` with ffplay on `PATH`; and try the flicker with the frame
wrapped in synchronized output.

### Cache a received scenario to the user root

- Priority: P3
- Affected files: `internal/resource`, `internal/app/session.go`

A scenario received from a coordinator lives in memory and is dropped at exit, so
rejoining the same session downloads it again. Writing it under the user root
behind an explicit opt-in would keep it, and needs a trust decision first: the
bytes came from a peer, and nothing about a plaintext link says they are the
operator's.

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
  `tool/vif-router/manifest.go`
- Guide: [Session fleet](kubernetes-fleet.md#6-resources) §6

Drive nine sessions with headless joiners for the fleet-level readings (CPU,
memory, tmpfs, log rate, rotations), plus one real four-player session over real
links, through a tower and a storm and on `wad/scenario/td`, for the hour the
Job's deadline allows, which tick slips and correction magnitude need. Requests,
limits and the quota totals then come from that measurement rather than from
single-guest history and estimates. It passes with no OOM, liveness restart,
sustained tick slip or growing correction magnitude, the deadline ending each
session. The socket readings (`TestNetcodeReadingsOverSockets`) cannot seat
sixteen td guests inside their join wait, so the td-16 column of netcode design
§3.1 has not been re-read since phase 1 and is this measurement's to take.

### Apply the fleet log retention on the node

- Priority: P1
- Affected files: `deploy/guest/vif-fleet-log-cleanup.py`, `doc/kube-docker-deploy.md`

Decided: the cleanup's stale age is 75 minutes, just past the session Job's
`activeDeadlineSeconds`, so the tmpfs holds live sessions and the ones that just
ended instead of five hours of finished ones; a byte budget or a larger tmpfs
were the alternatives. Apply on the node: reinstall the cleanup script as
[Fleet deploy](kube-docker-deploy.md) §7 does, then watch `findmnt` after a full
hour of play.

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
failure, whose connected-client behaviour should be recorded, with no replacement
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

### Shrink the spatial grid on ceiling-sized maps

- Priority: P3
- Affected files: `internal/engine/spatial_grid.go`
- Guide: [Session fleet](kubernetes-fleet.md#6-resources) §6

The grid is sized to the map, so only a ceiling-sized one such as `td` (500×250)
reserves about 30.5 MiB per world, and a guest's staging world a second: 92 MB of
the 240 MiB a td host, guest and staging world held after a collection, the largest
owner (netcode design §3.7). A sparse or tiled grid for a map this empty, or one
staging world sharing the live grid's shape without its cells, takes it.

## Multiplayer

### Publish a player's session at the router

- Priority: P3
- Affected files: `tool/vif-router`, `internal/app/host.go`, `internal/network`
- Guide: [Multiplayer](multi-player.md#56-relay-doors-and-the-router) §5.6

A run publishes its `-relay` door through the router's reverse tunnel and is
reached at the site; a host's door published the same way is a player's own session
reached there, the router neither allocating nor authoring. The deployment this
leads to serves as many pods as the node has resources for (`-port-first`/`-port-last`,
ten today) and routes every session beyond them, with accounting and
authentication added later. The tunnel carries a door already, and the foundation
stands: a name is resumed only by the run it was issued to, within
`NetworkTunnelResumeWindow`, so no published name shadows a pod's; one address
holds at most `-doors-per-address` doors; every connection placed is accounted for
as it closes (`session route closed`: session, door or pod, bytes each way,
seconds); and the listing's `limits` carry `sessions_max`, `doors` and `doors_max`,
so the site can say when the fleet is full and offer a hosted session instead.

What the feature itself needs, best shaped by deploying it step by step:
- `-host vif://<site>:7777` opening the door through the router rather than binding
  one, and the host's run reporting its state on the control connection (players,
  ceiling, scenario, whether it admits) so the router lists it without a `/health`
  read through the tunnel.
- The session's row in `/vif/api/sessions`, with a `kind` beside the pods', its
  lifetime (the control connection's, plus the resume window), and the create
  budget charged to the publisher's address as a pod's is.
- A credential in the publish frame (`PublishRequest` is JSON, so a field is added
  without a protocol change) once authentication exists, and accounting keyed by
  that identity rather than the address, over the per-connection lines above.
- The front door's `-tunnels` ceiling split between relay doors and hosted
  sessions, or one budget for both, decided from how the first deployments fill.

### Project a correction outside the live lock

- Priority: P2
- Affected files: `internal/app/snapshot_stage.go`, `internal/system/network.go`
- Guide: [Netcode design](netcode-design.md#less-work-under-the-live-lock)

An install compares the staging world's stores against the live ones in place and
writes the cells that differ, with no capture of either (`ReconcileSharedFrom`); the
deepest td install of netcode design §3.7 holds the live lock 8 ms, 7.7 of them the
write, down from 48. What the lock still holds is the projection: each projected
tick adds a simulation tick, 2.6 ms at td, so a repair 16 ticks behind holds it
past 45 ms. Moving it out: project to the live tick off the lock, then under it feed
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

Build it once the join refactor lands and its join matrix passes. Android opens on
a start screen with one choice, "Tap to start", a solo run; the site is reached only
from inside the run, through the menu's Multiplayer page (join a link, or request a
session from `https://lixen.com`, its default). Put a second choice beside the tap:
host a game at the site, as that request does, and/or join one of the running games
the site lists (`GET /vif/api/sessions`), so `game.Start` takes a join target. The
page needs nothing: the site's lobby (`/vif/lobby/`) is its choice, and its Play
link opens the page with `-join`, which joins at its start screen's tap. A run that
ends in an error, such as a refused join, shows it on the start screen, as the page
does; today Android finishes the activity and leaves the reason in logcat. Open:
host, list or both; a second screen or rows on this one; what a session that ends
returns to.

## Netcode

### Stop an internal transition proposing itself again in a session

- Priority: P2
- Affected files: `internal/fsm/machine.go` (`take`, `updateRegion`)

In a session an internal `Tick` transition crosses once per tick of the lead
rather than once: `take` sets `region.proposed = !trans.Internal`, so an internal
one leaves its region unparked, and `updateRegion` passes the same guard on every
tick until the first crossing's actions land. Simon's `Listen` plays three notes at
level 1 in a session and one solo. An internal transition in flight has to stop its
own re-proposal without parking the region's events (D-20); a test plays simon on a
mesh pair (`shapedPair`) and counts `EventFSMTransition` by owner and index. An
internal transition on an event crosses once: vanguard's U drop on a mesh pair
(`TestVanguardAKillInAPairDropsEachPlayerOneU`) holds it, and a fix keeps it so.

### Forgive a one-tick defeat in a session as solo play does

- Priority: P2
- Affected files: `internal/system/energy.go`, `internal/system/heat.go`,
  `internal/system/combat.go`, `internal/system/meta.go`

A cursor's defeat (`pushDefeatTransition`) and the gun hits that pay energy back
(`applyEnergyDrain`, 100 a hit) each cross the barrier. Solo, a cursor standing on a
pylon reached zero heat and energy on a tick whose four hits paid 400 back, so the
defeat and its undoing landed in that tick and the monitor never saw it. Hosted,
both land a lead later, and the recovery the hits' energy produces crosses only
then, so `session.any_defeated` reads true for a lead (3 ticks) and td's monitor
ended the run every time. Options: a monitor that confirms a defeat lasting longer
than a lead, or the hit's reward paid as the owner's own energy when it fires, so a
defeat and its undoing cross in one batch.
Proper approach is a few second immunity period. Consider having cursor as Wraith till
world is settled, and then allow play. Visual representation of Wraith state for the local
and remote cursors to be added.

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
a drag moves the cursor to every cell the finger crosses (`Router.moveMouseCursor`,
`PushPointerMove`), a crossing each, where a key moves one cell a press, and god
mode multiplies the actions. Compare the two clients' `network.sent_epoch_bytes`,
`network.sent_sync_bytes` and `network.crossings_sent` over one window, and
`network.pace_late_ticks` for a client the OS throttles; if the epochs carry it,
decide whether a drag's intermediate cells are a move each or one move to the cell
the tick ends on.

### Give a lone host the authority's one-tick lead

- Priority: P3
- Affected files: `internal/system/network.go`

`driveOwnLead` returns while `PeerCount()` is 0, so a host with no guests keeps the
3-tick default (`NetworkBarrierDelayTicks`) where `ownLead` gives an authority 1,
and a guest's arrival lowers it only after the 300-tick renegotiation window. Each
crossing a host playing alone makes lands two ticks later than it needs to. Push
the authority's lead when hosting starts, or let `driveOwnLead` run for an
authority with no peers.

## Session membership

A suggestion for the words, not a decision: a *session* is one world timeline under
one authority — a fleet pod, a terminal host, any instance — and its participants,
and solo play is a session of one. Commands then move participants between sessions:
`:open`/`:close` in place of `:host`, since every instance already is its own
session's authority; `:join`/`:leave` for a participant; `:drop` for an authority
removing one; `:merge`/`:split` for whole sessions.

### Let an authority drop a participant

- Priority: P2
- Affected files: `internal/network`, `internal/app`, `internal/mode/commands.go`,
  `tool/vif-router`

`:player drop <slot>` lets a host drop a player with its bots, or one bot, and the
dropped person starts a new local game. What remains: a reason the dropped client
prints, playing on alone as after `:leave` instead of starting over, and an
operator route on the router, since a dedicated host has no console. Keeping a
dropped player out is a separate decision: the session name is a routing key, not
an identity.

### Keep a Wraith's stats in the session

- Priority: P2
- Affected files: `internal/app/claim.go`, `internal/app/identity.go`,
  `internal/app/host.go`, `internal/system/cursor.go`, `internal/system/network.go`
- Guide: [Multi-player](multi-player.md#54-wraiths) §5.4

A player coming back to its Wraith brings its cursor's stats itself
(`event.CursorRecord`: energy, heat, health and loadout): the last owner-state sync
its run made in that session, frozen once the run leaves it, so play in a fork since
never comes back. This is a compromise. The values are owner-authored
([D-13](domain-design.md#d-13--owner-authored-shared-values-have-one-writer)) and a
Wraith gives up everything the world reaches it through, so neither the host nor the
shared world keeps them, and the session takes the returning run's word for them.

The fix shares some data of every cursor, so the session holds what each Wraith
resumes with and a successor inherits it. That moves those values off the one-writer
rule (D-2, D-13) for every system that writes them owner-side, and costs bandwidth,
which is why it waits. A cheaper step, a Wraith keeping the copy the sync committed
at its departure tick, is outside the hash and the repair (D-13), so nothing holds
the instances' copies equal.

The record is built to be replaced: one type, made in one place
(`NetworkResource.OnOwnState` into the claim keeper) and read in one
(`CursorSystem.restore`), carried by the arrival crossing the coordinator fills
(`ParticipantJoinedPayload.Record`). What would close it: decide which values the
session holds, have the coordinator fill the arrival from its own world instead of
the joiner's report, and drop the record from the keeper and the report.

### Place an arrival where its scenario places a cursor

- Priority: P2
- Affected files: `internal/system/network.go`, `internal/app/join.go`,
  `internal/engine/resource.go`

A participant's cursor lands on the map centre whatever the scenario's boot request
asked: `addParticipant` and a joiner's roster both push `Center: true` with the
template's heat and energy, so a simon guest starts at the centre and not below the
buttons where the boot cursor stands. Anything hostile a scenario puts at the centre
strikes each arrival on its first ticks. Keep the boot request's placement beside
the heat and energy template (`PlayerResource.SetInitialResources`) and spawn
arrivals from it, or let a scenario name spawn cells by slot.

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
- Affected files: `internal/app/session.go`, `internal/app/serve.go`, `wad/scenario/td`
- Guide: [FSM and configuration](fsm-and-configuration.md#3-root-schema) §3

`min_players = 0` is accepted and, like 1, waits for one player. A game that runs
with nobody in it, such as td's two sides attacking each other's towers, needs a
lobby quorum of 0 so a server starts without a guest, `holdVacant` parking only
below the scenario's minimum, the ownerless boot cursor dropped without a park, and
spawns that do not anchor on a cursor: the gold cycle fails and retries with none.

## Combat

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

### Move storm's green circle onto a disruptor mount

- Priority: P3
- Affected files: `internal/system/storm.go`

Its area pulse is the hosted disruptor's shape; the red circle's turret mount is the pattern.

## Species

### Pace a quasar fusion as its own timer did

- Priority: P3
- Affected files: `internal/component/materialize.go` (`MaterializeStyles`),
  `internal/parameter/effect.go`, `internal/system/fuse.go` (`fuseInto`)

A quasar fusion's quasar lands as its beam ends, a whole spawn beam, where the
timer the beam replaced landed it a little after half of it. A session sweep with
both fusions waiting a whole beam cost quasar kills, the one gap beyond the noise;
a drain pair's swarm lands at half a second again, as its spirits arrive, so the
sweep decides whether the quasar's wait alone still costs them. A fusion row in
`MaterializeStyles` with a shorter fill, hold and recede would give the old pacing
back, spawn and visual still ending on one tick.

### Show a species' machine state in a debug overlay

- Priority: P3
- Affected files: `pkg/machine/machine.go` (`Graph.States`, `Graph.Flags`),
  `internal/render/renderer`

`Graph.States` lists a graph's states and `Graph.Flags` a state's flags, for an
overlay that labels each instance of a species with its state and progress, as
`:graph` draws a route graph. Nothing reads either, and only tests read
`Graph.Name`: build the overlay, or delete the two reads until one is wanted.

### Push a data species with the wind

- Priority: P3
- Affected files: `internal/system/environment.go` (`applyPlanarWind`)

`applyPlanarWind` accelerates each mobile species by its profile mass, and a data
species is none of them, so a wind leaves a raider where it stands though its
prototype has a `mass`. The smallest fix ranges the `Behavior` store in the Shared
pass with each prototype's mass from `Resources.Behaviors`, skipping a massless one.

### Count a data species' stalled transitions

- Priority: P3
- Affected files: `internal/behavior/compile.go`, `internal/system/behavior.go`

A program's transition whose candidates all fail retries or holds without a trace,
where the swarm counts its in `swarm.transition_stalls`. The swarm's observer is a
method value bound in `NewSwarmSystem`, reaching its counter through the system:
one graph, one counter. A program's graph is compiled before the generic system
exists, with no observer, so a per-program `behavior.<program>.transition_stalls`
needs an observer that reaches a counter bound later, in `NewBehaviorSystem` with
the lifecycle rows.

### Let a program spawn another program

- Priority: P3
- Affected files: `internal/system/behavior.go` (`SpawnSpecies`)

`SpawnSpecies` raises a swarm or a quasar by their spawn requests; a program cannot
raise another program, or more of itself, because the vocabulary is built before
any program compiles. Its build could take any program name and push
`EventBehaviorSpawnRequest`, which drops and counts an unknown one; the load-time
check of that name belongs with the payload names under Authoring.

### Give data species attack, beam and drop rows of their own

- Priority: P3
- Affected files: `internal/profile/combat.go`, `internal/profile/loot.go`,
  `internal/component/materialize.go`

A prototype names a combat row (`swarm` or `quasar`), a beam style (`swarm` or
`nugget`) and a code species' drop tiers, so a new attack profile, beam or drop
table is a Go change ([Behavior machines §6.4](machine-design.md#64-beyond-the-machine)).
Rows of its own would grow those tables per scenario from the prototype, as soft
collision builds a data species' rows from its `mass`.

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

### Open a console of the command and message history on backtick

- Priority: P1
- Affected files: `internal/asset/input/keymap.toml`, `internal/mode/router.go`,
  `internal/mode/commands.go`, `internal/engine/game_context.go`,
  `internal/render/renderer/status_bar.go`, a console renderer and its row in
  `internal/manifest/definition.go`, `android/app/src/main/java/com/lixen/vif/GameView.java`,
  `web/terminal.js`, `doc/input-and-modes.md`, `doc/rendering.md`

Backtick, unbound in normal mode today, drops a panel from the top of the screen
as Quake's console does: several rows of the messages the run posted and the
commands typed, newest at the foot, scrolled back with the keys an overlay
already takes, over the command line; backtick again or Escape raises it. The
status bar keeps showing the newest message, rising onto the items' row when it
needs two lines, and cuts a longer one: Android's 85-character log path at 18 dp
in portrait, and anything past about 15 characters in the one-row strip above its
keyboard at the default zoom. The console is where such a message reads whole, on
every host alike. `GameContext` keeps only the message shown (`statusMessage`),
so it needs a bounded history of them (kind, wall time, text); the router's
256-command ring (`cmdHistory`) already holds the commands. Open for its own
session: whether the console's line is command mode's or a second one; how a
touch host opens it, as no soft keyboard shows backtick on its first page (a key
strip button, a gesture, a tap on the bar's message); what a backtick is in
insert mode, where it types; whether a solo run pauses while it is down, as the
menu does; its height and what a resize does; and whether the history outlives
the run in the state directory, as a shell's does.

### Give the configuration menu a touch target on Android

- Priority: P1
- Affected files: `internal/engine/overlay_geometry.go`, `internal/render/renderer/overlay.go`,
  `internal/mode/config_menu.go`, `internal/mode/overlay.go`, `internal/app/presentation.go`,
  `android/game/view.go`, `android/app/src/main/java/com/lixen/vif/GameView.java`

A menu row is one cell, 12 dp on a phone against Android's 48 dp touch target, so
a tap lands on a neighbour. Three ways, in rising cost:

1. A row pitch on a touch host. `OverlayGeometry` gains a pitch the renderer,
   `pointConfigMenu` and the scrollbar's track in `Router.overlayView` share: 1 on
   a terminal, 3 or 4 where the presentation reports touch (one method on
   `Presentation`, true for Android and the browser's touch strip), so a row is 36
   to 48 dp and the list shows a third of its rows a page. The menu stays the `tui`
   one; every host behaves the same; smallest change.
2. A menu the host draws. The overlay content (`core.OverlayMenu`: rows with key,
   label, value, description and disabled reason; the form) crosses the binding as
   it changes, through an optional `menuPresentation` on `Presentation`
   (`ShowMenu`, `HideMenu`), and the host answers with the keys the router already
   takes plus a select-by-key the terminal vocabulary has no event for, so a new
   event or a call into the router under the world lock. Java draws a radial menu
   (the console style) or a list with 48 dp targets; the browser could do the same
   in HTML. A second renderer for one screen and a protocol for its state.
3. A separate Activity or bottom sheet: 2 with Android's own widgets, and the
   game's Surface left behind while it shows.

Recommended: 1 now, since it serves the browser's touch strip too; 2 only if a
radial menu is wanted for its own sake, after 1 shows what a bigger row is worth.
The typing strip above the keyboard already repeats the status row from the same
picture (`CellDrawer.drawStrip`), the pattern 2 would extend.

### Configuration menu follow-up

- Priority: P2
- Affected files: `internal/mode/config_menu.go`, `internal/paths/settings.go`
- `[play]` persists the audio channels, sfx volume, auto-fire, mouse, pointer
  following and the HUD, and a scenario change carries them over. What `:new`
  keeps beyond those (rate, pins, standing breakpoints) a restart or scenario
  change still drops. Music gain needs an authoritative readback before joining
  the sfx volume in the menu. Keymap, backend, paths and colour depth remain
  startup settings; `:d break` stays on the command line, since a row cannot take
  its arguments.

### Use sixteen console backgrounds on Linux

- Priority: P3
- Affected files: `internal/render/console.go`

The framebuffer console shows a bright background under SGR 5, which vgacon blinks instead; telling them apart would give Linux FreeBSD's sixteen backgrounds.

### Spare the overlay cards their per-frame titles

- Priority: P3
- Affected files: `internal/render/renderer/overlay.go`

The help and telemetry overlays run with the game live, and `renderCard` builds
each visible card's padded title string every frame, a dozen small allocations
a frame. Keeping the titles beside the masonry items at relayout, where the
layout is already cached, removes them. The menu and form overlays allocate
likewise but show while `PauseForInput` holds the simulation, so they cost play
nothing.

### Take the ember further only with a visual trade-off

- Priority: P3
- Affected files: `internal/render/renderer/ember.go`

The painter's remaining cost is the ring pass, about two fifths of a low-heat
paint, and the per-frame 256-bucket jagged table; both are exact today. What is
left needs a compromise: a coarser table for the ring Gaussian keyed by the
squared plane distance, fewer jagged buckets, or ring evaluation only inside the
band the pulse can light. None is visible in principle, none is bit-identical.
Revisit only if `:d prof` ranks `draw ember` high again.

### Sample the kraken's tentacles more coarsely

- Priority: P3
- Affected files: `internal/parameter/kraken.go`, `internal/component/kraken.go`

What the kraken still costs scales with its tentacle samples, about 780 discs a
tick at `KrakenSampleStep` 0.35, which the footprint and the shading each stamp.
A step of 0.7 in the browser build took the first frame after a tick from about
0.35 to 0.24 ms and the kraken region's tick from about 0.9 to 0.8 ms, at a
visible cost over the same poses: 0.6% of drawn cells appear or vanish at the
edges, 1.8% take another rim glyph and 0.5% another color, and the footprint's
edges move by a cell in places, so contact and wall clearing shift with them.
Revisit only if `:d prof` ranks `sys kraken` or `draw kraken` high again.

## Audio

### Bring soundlab to Android (soundlab mobile)

- Priority: P1
- Affected files: `cmd/soundlab`, `android/game`, `android/app`,
  `doc/multi-platform.md`

Soundlab runs on Android in-process, as the game does, over the same engine and
the user root's `audio/` documents, so a pattern written on the phone plays in the
phone's game. The engine already runs there (`AudioConfig.Sink` takes the
`Speaker`), and the host already turns taps, drags, keys and the system
keyboard's text into `terminal.Event`s and flushes cell frames through
`glyph.Frames`. What remains:

1. The session, documents, command table and `Execute`, with the TUI, move out of
   `cmd/soundlab`'s `package main` into `internal/soundlab`; `cmd/soundlab` keeps
   its flags and the REPL. The TUI already runs over `terminal.Terminal` alone.
2. A `terminal.Terminal` over the Android view (its grid as `Size`, its event
   queue as `PollEvent`, `glyph.Frames.Flush`) runs it, started by a second bound
   entry beside `game.Start` that hands `NewSession` the `Speaker` as its sink;
   the start screen offers soundlab beside the game.
3. Touch: the TUI reads keys alone. A tap hit-tests what was drawn: a key of the
   keyboard plays its note, a step toggles, a tab or a pane takes the keys, a mixer
   row mutes its slot, a browser row selects. A strip of buttons stands for the
   keys a phone lacks (record, play, delete, save, new, edit, tempo, Tab, Esc), and
   the system keyboard serves `:` and the prompts, as in the game.
4. Size: the views need 60×16 cells. A phone held landscape fits at the default
   cell; portrait needs the pinch zoom out, or a keyboard of fewer keys.

### Rename the audio mute event to the audio vocabulary

- Priority: P2
- Affected files: `internal/event/type.go`, `internal/event/payload.go`,
  `internal/system/audio.go`, `internal/mode/config_menu.go`, `doc/audio.md`

`EventSoundMuteToggle` changes the audio channel mask, sfx and music alike, so it
is `EventAudioMuteToggle`, the request beside `EventAudioMuteChanged`, with its
payload. It waits for the netcode work to settle the event registry and journal
schema the rename regenerates and bumps.

### Spin off `pkg/audio` as its own module

- Priority: P2
- Affected files: `pkg/audio`, `pkg/audio/model`, `pkg/tomlmacro`, `cmd/soundlab`

Two in-repository imports remain: `pkg/vmath` for three hash and stream
constants, which inline, and `pkg/tomlmacro` for the macros both document
loaders expand, which moves with it or into `lixenwraith/toml`.

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

### Give the composite lifecycle methods a user

- Priority: P3
- Affected files: `internal/system/composite.go`

`CompositeSystem` carries `handleEmptyComposite`, `CreateHeader`, `AddMember`,
`DestroyComposite` and `GetAnchorForMember`, designed for building and tearing
down composites at run time, but no system or scenario uses them. A species or
scenario that assembles composites would; it reaches them through events, since
another system may not call them directly.

### Start the next tick where a late frame let the last one start

- Priority: P3
- Affected files: `internal/engine/scheduler.go` (`schedulerLoop`, `awaitFrame`)

A tick that, with the frame it waits for, outlasts the 50 ms interval (from about
40 ms on one thread, as in a browser) starts the next tick late, and since the
deadline is not re-anchored the tick after comes early: drawn for two frames, then
four. Re-anchoring the deadline when `awaitFrame` waited evens the ticks after at
the cost of the run trailing wall time by the wait, which a guest's trim and step
read, so the divergence harness decides it. It cannot help the frame a long tick
delays itself, and no region entry comes near: the heaviest, a cold kraken entry,
ticks in 13 ms in the browser build ([Runtime §3](runtime.md#3-runtime-clocks-and-cadence)).

### Let a paused run sleep

- Priority: P2
- Affected files: `internal/engine/scheduler.go` (`eventLoop`), `internal/app/loop.go`
  (`Loop`'s tickers), `android/app/src/main/java/com/lixen/vif/GameView.java` (`doFrame`)

A paused run keeps every clock of a running one: the event loop wakes each 4 ms
with or without a queued event, the input tick each 16 ms, the frame tick at 60 Hz,
and on Android the render thread asks for a frame at each vsync. On the Pixel 7
emulator that is 19 to 23% of a core on the start screen and 9% with the app in
the background, where nothing is drawn (2026-10-22); the background is what costs
a phone its battery. While a solo run is away, the event loop could wait on a push
instead of its ticker, the app loop drop its input and frame tickers until a report
or event comes, and the render thread stop its vsync callback once it has painted
the last frame flushed.

### Run every clock for the span it is set to

- Priority: P3
- Affected files: `internal/engine/scheduler.go` (`processTick`), `internal/event`
  (the journal's settle groups), `doc/runtime.md` §5

A light set to 250 ms should show for 250 ms whoever lights it. Today simon's Show
buttons, set to 250 ms, show for 200, and a level-1 note set to 550 ms shows for
500, solo and in a session alike; the same light armed between ticks, as an input
arms one, shows for its whole span. Every relative clock behaves so: a timer slot, a
carried age (the ember simon lights at a turn spends its first heat point a tick
early), a species countdown, a machine's elapsed time.

Each advances in its owner's `Update`, and nothing renders inside a tick, so a clock
started in a tick before its owner runs is advanced once before any frame shows it
([Runtime §5](runtime.md#5-simulation-tick-steps)). Three settles start clocks there:

- wire: every crossing, which in a session carries each input and each FSM
  transition with its actions;
- pre: what was pushed since the last tick and not yet settled;
- post: the FSM's events, which carry every scenario action in a solo run and the
  delayed and `on_update` ones in a session.

The pre and post settles also follow the tick's time advance, so their clocks read a
tick ahead of game time; the wire settle precedes it, so its clocks only show short.

The fix is to advance, then settle: game time, the FSM's update and the systems run
first, then one settle takes what the tick applies (the wire artifacts due, what was
pushed since the last tick, the FSM's and the systems' events). Every clock started
in a tick then first counts on the next, with no rule at any start. Its costs: a
system's `Update` first sees a crossing or an FSM action a tick later than today,
though its handler still runs in the tick it lands; the journal's settle groups
change, so its format moves; tests pinned to such an effect's tick move. Moving only
the pre and post settles would leave a crossing's clock a tick short on screen, and
a session's lights a tick shorter than a solo run's; skipping each clock's first
pass would put a rule in every owner.

## Structure

### Move the three run types to packages

- Priority: P3
- Affected files: `internal/app`

`corrector`, `membership` and `scribe` each read the run through one interface
(`correctionRun`, `membershipRun`, `scribeRun`), and `App` is the composition
root the package map names, but all three stay in `internal/app`, which is
still 33,000 lines. Each builds runs (the staging world, the journal twin, a
dial's run, a seat's bot), so a package for one needs the run's constructor as a
second seam, and the tests drive whole Apps through `NewHeadless`, so none could
move its tests with it. A package per type waits on a constructor seam the tests
can be written against.

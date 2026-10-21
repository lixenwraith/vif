# Bots

A bot is a participant driven by a graph instead of a terminal. It holds a roster
slot and a cursor, unless the map places none, and its intents (keys and mouse
alike) pass through the same mode router as a person's, under the same session
rules. Bots are cooperative: they play beside people against the environment.

## Quick start

```sh
vif -bot                              # you and one default bot
vif -bot 2:default                    # you and two bots
vif -host :7777 -bot 2                # host with two bots; others may join
vif -join 127.0.0.1:7777              #   joining it from another terminal
vif -join 127.0.0.1:7777 -bot 2       # join with two bots of your own
vif -bot -watch                       # watch one bot play; no player of yours
vif -bot default -headless -speed max -j   # record a solo bot flat out; Ctrl-C ends it
vif -replay <journal>                 # replay what it recorded
vif -serve :7777 -size 120x40 -bot 2  # dedicated host with two bots and room for people
```

## Flags

| Flag | Effect |
|---|---|
| `-bot [N[:graph]\|graph]` | Add N bots playing graph. N is 0..16 and 0 adds none; omitted operands mean `1:default`. |
| `-bots [N[:graph]\|graph]` | The same grammar; requests from both flags add together. |
| `-watch` | Show the first bot on this terminal in place of a player; any further bots are seated beside it. Not with `-serve`. |
| `-headless` | Run the first bot with no terminal, then print `Bot stopped: N ticks, M actions, fitness F`. Not with `-watch` or `-serve`. |
| `-speed <rate>` | A watched or headless bot's pace: `1/8`..`8`, or `max` for no pacing. A watched bot defaults to `1`; a solo headless bot runs flat out unless set. |
| `-size <WxH>` | A headless bot's view, default 120x40; a watched bot takes the terminal's unless one is given. A seat takes its holder's screen: a person's terminal as it resizes, or the holder's `-size`, else 120x40. With `-serve` it sizes the map, which otherwise the first person sets, or a server's own bots at 120x40. |
| `-seed <n>` | Root seed. A solo bot run depends only on it and the configuration. |
| `-serve <addr>` | A dedicated host with no player of its own; `-bot` seats its bots. |
| `-host`, `-join` | Keep your player; bots join beside you. |
| `-config-dir <dir>` | The first root searched for `bot/<name>.toml`. |
| `-j` | Journal this process's front instance: your player, or the watched or headless bot. |
| `-check` | Validate every bot graph in the configuration roots and the embedded ones, with the rest of the configuration. |

## Compositions

| | On the host | On a guest |
|---|---|---|
| A person | `vif`, or `vif -host <addr>` | `vif -join <addr>` |
| A person with bots | `vif -bot N`, `vif -host <addr> -bot N`, or `:bot add` | `vif -join <addr> -bot N`, or `:bot add` |
| Bots alone | `vif -serve <addr> -bot N`, or `-bot N -watch` / `-headless`, with `-host <addr>` to let others in | `vif -join <addr> -bot N -watch` / `-headless` |

## Ownership and leaving

A bot belongs to the run that started it, its holder.

| Bot of | Joins | Leaves when |
|---|---|---|
| the host | its holder's listener over loopback; a run in no session opens one on loopback first | the host ends, it is dropped, or a `-serve` host begins its drain |
| a guest | through the guest's own link, on an in-process stream the guest serves | the guest leaves or loses its session, or it is dropped |

- After a hand-off, a holder that inherited authority admits new bots on a
  loopback listener of its own; a guest keeps seating bots through whoever holds
  authority.
- A person who joins at a guest's `-relay` address plays through the guest the
  same way but is not its bot: it holds no holder, is dropped on its own, and
  keeps its slot when the guest leaves.
- A seated bot belongs to its holder, so it is never elected: succession
  considers only independent participants. A watched or headless bot is one and
  follows `-no-advertise` like any other.
- `:bot drop <slot>` drops one of your bots; the host may drop any bot.
  `:player drop <slot>` (host only) drops a player with all its bots, or one bot.
- A dropped person starts a new local game without its bots; a dropped watched or
  headless run stops. Dropping ends membership; it is not a ban.
- A headless bot ends once its host leaves and no participant, itself included,
  takes over; one that takes over plays on with its own bots, as any session only
  bots occupy does. A watched bot plays on alone, as a person does.

## Commands and the menu

| Command | Effect |
|---|---|
| `:bot` | List this run's bots: slot, graph, joining or leaving. |
| `:bot add [N[:graph]\|graph]` | Seat more bots; a solo run first opens a loopback session. A count of 0 is refused. |
| `:bot drop <slot>` | Drop one of your bots, or as host any bot. |
| `:player` | List the slots: `3 player` or `4 bot of 3`. |
| `:player drop <slot>` | Host only: drop a player and its bots, or one bot. |

Slots accept `10`, `A` or `0xA` for the cursor marked `A`. The
[configuration menu](config-menu.md#bots) offers the same operations.

A watched bot takes `:` and Ctrl-G. While you hold its command line or the menu,
its graph supplies no input: a run in a session keeps ticking, a solo run waits.
`:n`, `:bot add`, `:bot drop` and `:player` work there.

## Telling cursors apart

Every other cursor is marked with its slot, `0` to `F`; yours is unmarked. The
status bar's `4P:1` means four cursors, yours in slot 1. `:player` tells players
from bots and names each bot's holder. Slots identify cursors for this session
only, and a departed slot may be reused.

## Pause, speed and session lifetime

- A host may pause only while every other participant is its own bot; its bots
  stand still with it, and anyone joining ends the pause. A guest, a host with
  other people in, or a watched bot in a session cannot pause.
- Speed changes are refused in a live session, one only your bots share included.
  `-speed max` is for one solo watched or headless bot.
- Bots count as occupancy. A `-serve` session only its own bots occupy plays on
  until a termination signal, which sends them away at once; in the fleet, also
  until the session Job's one-hour deadline. A guest's bots count only while the
  guest is in the session.
- The fleet allocator takes a `bots` count, closed by default (`bots_max` 0); see
  [The session fleet](kubernetes-fleet.md). Neither `-join <site>` nor the menu's
  request form asks for bots yet.
- A browser build seats bots only as a guest of a session it joined; it cannot
  host, so it cannot add bots to a solo run.

## Writing a graph

A graph resolves as a path, then as `bot/<name>.toml` under `-config-dir`, the user
root, the checkout's `wad/` beside a `bin/` binary, `/usr/local/etc/vif` and
`/etc/vif`, then as the embedded copy. `make install-config` installs the embedded
`default` into the user root. `roam` and `patrol` load `default` unless a root holds
a file of that name. `-d` does not pin bot graphs.

### Document

One self-contained TOML file in the [FSM dialect](fsm-reference.md):

| Table | Holds |
|---|---|
| `[bot]` | `actions_per_second`: intents sent per second of game time, default 12. |
| `[regions.<name>]` | `initial` only. |
| `[states.<name>]` | States as in a scenario: hooks, transitions, `parent`. |
| `[macros]` | Inline macros, expanded at load. |

Anything else is refused at load: `description`, `[systems]`, `macro_files`, and a
region's `file`, `enabled_systems`, `disabled_systems`, `only_systems` or
`background`. Queued intents wait in a queue of 256; an intent that finds it full
is dropped and counted. Allowance left idle builds up to one second's worth.

### Actions

| Action | Payload | Queues |
|---|---|---|
| `Intent` | `name`: an action of the [default keymap](../internal/asset/input/keymap.toml), or a list drawn from, or `press` or `release`; `count`: a number or a `[min, max]` range drawn from; `char` for a char-wait action, or the key `press` presses and `release` lets go; `hold` for `press`: ticks, or `true` until its `release` | one key press or release |
| `Text` | `text` | one character per rune |
| `Command` | `text`: an ex-command, the leading `:` optional, not empty | the command-line round trip |
| `TypeGlyph` | none | the glyph under the cursor, switching to Insert first; nothing when there is none |
| `PointAt` | `target`; `key` picks the `button` on that key; `fire = true` clicks instead of moving; `away = true` aims away | a pointer move or click |

`Intent` refuses an action that only opens a key sequence (`operator_delete`,
`prefix_g`, `prefix_macro_play`, `marker_glyph_*`); `quit` ends the run. `press`
presses a key a scenario captured, as a typed key or a click on its button would,
and does nothing while the key is not captured. The bot lets go of every key it
presses, at once, after `hold` ticks, or with `hold = true` at a `release` of the
key, so each press crosses its own release and the bot's slot reports releases
([Input and modes](input-and-modes.md) §1). A press of a key it holds lets go
first, a `release` of one it does not hold sends nothing, and play stopping ends
its hold as it ends a person's. A press and its release are one action; a
`release` is one of its own.

`PointAt` aims at the nearest target of its class, clamped to the cells the view
shows, and for `glyph` at the left edge of that text run. If the cursor may not
enter the cell, it lands on the nearest shown cell it may enter, and does nothing
when none is shown. A `button` is aimed at its shown cell nearest the cursor, and
a click presses its key wherever the cursor stands; one the view does not show
draws a move toward it and no click, which would fire where it landed. `away`
aims at the target's mirror through the cursor, or, when the view's edge pins the
cursor in line with it, at the shown open cell farthest from it; it refuses
`fire`, `button` and `random`, and does nothing while the target is on the cursor
or no cell gains ground.

### Guards

| Guard | Args | Passes when |
|---|---|---|
| `HasTarget` | `target`, `key`, `within` | a target of the class exists, within `within` cells when non-zero |
| `OnTarget` | `target`, `key` | the cursor's cell holds one |
| `InMode` | `mode`: `normal`, `visual`, `insert`, `search`, `command`, `overlay` | the router is in that mode |
| `Chance` | `percent`, 1..100 | a draw from the bot's own stream |
| `Idle` | none | the intent queue is empty |
| `Own` | `field` | that event payload field names the bot's own cursor |

A guard refuses an argument it does not read.

### Targets

| Target | Is |
|---|---|
| `glyph` | typeable text: a glyph neither gold nor part of a composite |
| `gold` | a gold sequence member |
| `nugget` | a nugget |
| `species` | a combat entity other than a cursor or a tower |
| `threat` | a species, a hostile bullet or a hostile missile, all weighted alike |
| `cursor` | another participant's cursor |
| `button` | a scenario's button, or with `key` the one on that key, at its cell nearest the cursor |
| `random` | a cell the view shows; `PointAt` only |

Distance counts rows twice, as the terminal draws them. `HasTarget` and `OnTarget`
see the whole local world; `PointAt` reaches only what the view shows. On a map
without cursors only buttons are sighted: `PointAt` clicks the leftmost, or the
one on `key`, at its middle and moves nowhere, `HasTarget` passes only without
`within`, and `OnTarget` never does.

### Events

A transition may name an event as its trigger. A bot sees the events its own
instance dispatched during its previous step, in dispatch order, before this
update's `Tick` transitions. Payload guards, `Own` and `capture_vars` read them:

```toml
{ trigger = "EventCursorDefeatState", target = "Down", guard = "And", guard_args = { guards = [
    { name = "Own", args = { field = "entity" } },
    { name = "PayloadBoolEquals", args = { field = "defeated" } },
] } },
```

It does not see the event queue, crossings the session has not yet released, or
ticks run while you hold a watched bot's command line.

### Shared vocabulary

The [FSM reference](fsm-reference.md)'s generic actions and guards work too:
variables, `StateTimeExceeds`, `And`, `Or`, `Not`, payload guards, region control
over the bot's own machine, status guards over the bot's own registry (bare keys
such as `heat.current` are its own slot, `player.<n>.*` any slot), and config
guards over its own view. Actions that write the world are refused at load:
`EmitEvent`, `EnableSystem`, `DisableSystem`, `ReconcileSystems`.

### Timing and randomness

- A graph updates once a tick, every 50 ms of game time.
- `PointAt` and `TypeGlyph` read the world, so each acts only when all it queues
  is sent before the next tick. `Intent`, `Text` and `Command` queue
  regardless; wait for them with `Idle`.
- Draws come from the bot's own stream, seeded from the session seed and its
  participant. A payload with no choice draws nothing, so a fixed sequence leaves
  the stream alone.

### Examples

A fixed sequence:

```toml
[bot]
actions_per_second = 8

[regions.play]
initial = "Out"

[states.Out]
on_enter = [
    { action = "Intent", payload = { name = "motion_right", count = 12 } },
    { action = "Intent", payload = { name = "fire_main" }, delay_ms = 500 },
]
transitions = [{ trigger = "Tick", target = "Back", guard = "StateTimeExceeds", guard_args = { ms = 1500 } }]

[states.Back]
on_enter = [{ action = "Intent", payload = { name = "motion_left", count = 12 } }]
transitions = [{ trigger = "Tick", target = "Out", guard = "StateTimeExceeds", guard_args = { ms = 1500 } }]
```

The shipped [`default`](../internal/asset/bot/default.toml) types text left to
right and collects nuggets between patrols:

| State | Does | Moves on |
|---|---|---|
| `Seek` | points at the nearest glyph | to `Gold` if gold exists, `Nugget` if one is within 12, `Type` on a glyph, `Walk` with no glyph |
| `Type` | types the glyph under the cursor | to `Seek` once off text |
| `Gold` | jumps to gold and types it | to `Seek` when off gold, 1.5 s after entering |
| `Walk` | now and then moves 2 to 10 cells | to `Seek`, `Gold` or `Nugget` once one exists; at 1% an update to `Leap` |
| `Leap` | clicks a random shown cell | to `Walk` after 0.4 s |
| `Nugget` | jumps to the nugget | to `Seek` after 0.3 s |

Run `vif -check` after editing: it prints `bot ok: <path>` for each graph that
loads and fails naming each that does not. A graph outside every root is checked
by putting it under `<dir>/bot/` and running `vif -check -config-dir <dir>`.

## Fitness and comparing graphs

A bot's fitness weighs the events its own steps dispatched. It measures the graph;
players have no score.

| Event | Weight |
|---|---|
| a correctly typed character (`EventBoostReward`) | +1 |
| a typing error (`EventBoostDeactivate`) | -1 |
| a species it killed (`EventSpeciesKilled`, or `EventSpeciesKillConfirmed` once a predicted kill is proved) | +5 |
| a gold sequence it completed (`EventGoldCompleted`) | +25 |
| its cursor defeated (`EventCursorDefeatState`) | -100 |

Fitness is in the `-headless` summary, the `bot stopped` log record and a seat's
`bot left` record. From a checkout with `bin/vif` built,
`./script/test.sh botfitness [graph-file] [seeds] [secs]` plays `default` headless
and flat out at 120x40 for `secs` (60) over seeds 1..`seeds` (4) on the embedded
scenario, `td` and `kraken`, and prints each scenario's sum. Given a graph file, it
fails unless that graph beats `default` on every scenario; with none, the sums
are the baseline to beat. It appends a `stop` region (states `StopWait` and
`StopQuit`), so a graph cannot use those names.

## Recording and replaying

`-j` journals the front instance from its start and `:journal start` from where
it stands; `:journal start <slot>` journals a seat of this process's the same way,
`:journal stop [slot]` closes either, and `:journal` names every file open
([Logging and diagnostics §9](logging-and-diagnostics.md#opening-mid-run)). A
journal records what the router produced, so `vif -replay <journal>` needs no graph
and never restarts one. Playback runs to the recorded end, quiet trailing ticks
included, and shows `END`. `vif -replay <journal> -headless` verifies its digests
unattended.

## Limits

- A bot perceives its own instance's present state and its last step's events,
  never the event queue, crossings the session still holds, RNG positions,
  pending delayed actions, the prediction ledger or network state. A keystroke can
  miss, as a person's can.
- Nothing limits which commands a graph may issue, `:q` and `:bot add` included.
- It reaches only what its view shows. A seat's view is its holder's screen, so a
  person's bots see what that person sees and play differently across terminals.
- It plays cooperatively: cursor weapons do not strike cursors.
- A solo bot run is reproducible from its seed. In a session only a journal
  reproduces a run: the front instance's, or a seat's this process journals.
- Graph state is in no capture: a correction or a reset reaches a graph only
  through the world it reads next.
- A guest's bot has no link of its own, so it shows no throughput and is not
  evicted as slow; what it sends counts against its holder's ingress budget.
- Links are unauthenticated, and every participant needs the same build.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `bot graph "x" not found as a path, in a configuration root or embedded` | Check the name and `-config-dir`, or give a path. |
| `bot <name>: …` at start or on `:bot add` | The load error names the graph and its fault; fix it and run `vif -check`. |
| A `roam` or `patrol` bot plays an old policy | An installed file of that name shadows `default`: use `-bot default` or update the file. |
| `dropped` above 0 in `bot stopped` or `bot left` | The graph queues faster than its rate; guard its actions with `Idle` or raise `actions_per_second`. |
| `-speed max cannot pace a session` | Run one bot alone, without `-host`, `-join` or further bots. |
| `-headless requires -bot, -bots or -replay …` | Give it a bot; `-watch` and `-serve` do not combine with it. |
| `Bot <graph>: …` on the status bar | The seat failed to join; the log has `bot not seated` with the error. |

The holder logs `bot seated`, `bot left` and `bot not seated` for each seat, whose
own instance logs under `instance` `seat N`; a watched or headless bot logs
`bot stopped`.

## Not built yet

- [Open fleet sessions to bots](todo.md#open-fleet-sessions-to-bots)
- [Beat the default graph](todo.md#beat-the-default-graph)
- [Let a bot graph use operators and marks](todo.md#let-a-bot-graph-use-operators-and-marks)
- [Limit the commands a bot graph may issue](todo.md#limit-the-commands-a-bot-graph-may-issue)
- [Expose a bot difficulty knob](todo.md#expose-a-bot-difficulty-knob)
- [Judge a relayed bot's lateness](todo.md#judge-a-relayed-bots-lateness)
- [Seat a host's bots without a listener](todo.md#seat-a-hosts-bots-without-a-listener)
- [Let a watched holder pause with only its own bots](todo.md#let-a-watched-holder-pause-with-only-its-own-bots)
- [Name participants beyond their slot](todo.md#name-participants-beyond-their-slot)
- [Train bot graphs offline](todo.md#train-bot-graphs-offline)
- [Adapt bots to the session's players](todo.md#adapt-bots-to-the-sessions-players)

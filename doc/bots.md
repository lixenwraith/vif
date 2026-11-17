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
vif -bot -watch -size fit             #   on a map the size of this terminal
vif -bot default -headless -speed max -j   # record a solo bot flat out; Ctrl-C ends it
vif -s cave -bot cave -watch          # watch a network bot learn the cave
vif -s cave -bot cave -headless -speed max   # the same flat out on every core, kept at each Ctrl-C
vif -replay <journal>                 # replay what it recorded
vif -serve :7777 -size 120x40 -bot 2  # dedicated host with two bots and room for people
vif -spectate 127.0.0.1:7777          #   spectating it from another terminal, in no seat
```

## Flags

| Flag | Effect |
|---|---|
| `-bot [N[:graph]\|graph]` | Add N bots playing graph. N is 0..16 and 0 adds none; omitted operands mean `1:default`. |
| `-watch` | Show the first bot on this terminal in place of a player; any further bots are seated beside it. Not with `-serve`. A session another run hosts is watched with `-spectate` ([Multiplayer §5.8](multi-player.md#58-watching-a-session)). |
| `-headless` | Run the first bot with no terminal, then print `Bot stopped: N ticks, M actions, fitness F`, summed and led by `G games,` where it played several, and for each network of a graph that learned `Bot learned <network>: rounds R of T, recent M, fielding S over C`: the rounds its learner scored this run and in all, their recent mean, and the score of the member it fields with the rounds behind it ([Networks and learning](#networks-and-learning)). Not with `-watch` or `-serve`. |
| `-games <n>` | With `-headless`, how many games a bot alone whose graph learns plays at once, all teaching its learners: one more than the cores unless given ([Learning](#learning)). Any other run plays one, and refuses a higher count. |
| `-speed <rate>` | A watched or headless bot's pace: `1/8`..`8`, or `max` for no pacing. A watched bot defaults to `1`; a solo headless bot runs flat out unless set. |
| `-size <WxH\|fit>` | A bot's own view, watched or headless, default 120x40: the terminal only shows it. `fit` takes the screen's as the run starts, the terminal, window or page that shows it or, headless, the terminal it was started in, and 120x40 with none. A seat takes its holder's screen: a person's terminal as it resizes, or the holder's `-size`, else 120x40. With `-serve` it sizes the map, which otherwise, as with `fit`, the first person sets, or a server's own bots at 120x40. |
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

A bot belongs to the run that started it, its holder, and joins on an in-process
stream the holder serves, which spends no join budget. A run in no session first
opens one with no listener, which only its bots can join, so a browser seats bots
solo too. `:host` opens that session to others, `:join` takes the bots along
into the session it joins, and `:leave` takes them out of one with the run.

| Bot of | Admitted | Leaves when |
|---|---|---|
| the host | by the host, as any joiner | the host ends, it is dropped, or a `-serve` host begins its drain |
| a guest | through the guest's own way to the host, the authority deciding | the guest leaves or loses its session, or it is dropped |

- After a hand-off, a holder that inherited authority admits new bots itself; a
  guest keeps seating bots through whoever holds authority.
- A person who joins at a guest's `-relay` address plays through the guest the
  same way but is not its bot: it holds no holder, is dropped on its own, and
  keeps its slot when the guest leaves. It seats bots of its own and relays for
  others as any run does, however many relays stand between it and the host.
- When its holder leaves, a bot's cursor waits as a Wraith with the holder's and
  goes when the holder's does: after two minutes, or when it is dropped. A holder
  that comes back seats its bots again, and each takes back its own place.
- A seated bot belongs to its holder, so it is never a candidate: the
  succession list holds only independent participants. A watched or headless bot is one and
  takes over as any other does, where its run says so (`-listen`, `[play] takeover`).
- `:bot drop <slot>` drops one of your bots; the host may drop any bot.
  `:player drop <slot>` (host only) drops a player with all its bots, or one bot.
- `:leave` takes your bots out of the session with you: they leave it first and
  are seated again in the session your run goes on in, each taking back the place
  it had. A run that loses its host and finds nobody to take over goes on the same
  way, keeping every other player's place too where its session migrates.
- A dropped person starts a new local game without its bots; a dropped watched or
  headless run stops. Dropping ends membership; it is not a ban.
- A headless bot ends once its host leaves and no participant, itself included,
  takes over; one that takes over plays on with its own bots, as any session only
  bots occupy does. A watched bot plays on alone, as the fork.

## Commands and the menu

| Command | Effect |
|---|---|
| `:bot` | List this run's bots: slot, graph, joining or leaving. |
| `:bot add [N[:graph]\|graph]` | Seat more bots; a solo run first opens a session only they can join. A count of 0 is refused. |
| `:bot drop <slot>` | Drop one of your bots, or as host any bot. |
| `:player` | List the slots: `3 player`, `3 alice` for a signed-in player, `4 bot of 3`, or `5 player (away)` for one who left. |
| `:player drop <slot>` | Host only: drop a player and its bots, or one bot. |

Slots accept `10`, `A` or `0xA` for the cursor marked `A`. The
[configuration menu](config-menu.md#bots) offers the same operations.

A watched bot takes `:`, the menu keys and the help key. While you hold its command
line, the menu or help, its graph supplies no input: a run in a session keeps
ticking, a solo run waits.
`:n`, `:pause`, `:bot add`, `:bot drop` and `:player` work there.

## Telling cursors apart

Every other cursor is marked with its slot, `0` to `F`; yours is unmarked. A player
who left is drawn as its mark alone, in its colour, where it stood, until it is
dropped two minutes later. The status bar's `4P:1` means four cursors, yours in
slot 1. `:player` tells players from bots and names each bot's holder. Slots
identify cursors for this session only, and a dropped player's slot may be reused.

## Pause, speed and session lifetime

- A session stops only by its host: its `:pause`, or `p`, stops every participant
  and spectator at the same tick, bots included, until the next one. A watched bot
  whose run hosts gives it from its own terminal, by `p` as by `:pause`; a guest's
  is refused, a bot's run included, and a `-serve` host has nobody to give one.
- A menu or the command line pauses a person's host only while every other
  participant is its own bot; its bots stand still with it, and anyone joining ends
  that pause. A watched bot in a session plays on under them.
- A session runs at its host's `:speed`, which no guest changes. `-speed max` is
  for one solo watched or headless bot.
- Bots count as occupancy. A `-serve` session only its own bots occupy plays on
  until a termination signal, which sends them away at once; in the fleet, also
  until the session Job's one-hour deadline. A guest's bots count only while the
  guest is in the session.
- A site's session takes the bots its players bring, as any session does:
  `-join <link> -bot N`, `:bot add`. Its own, which the site runs, are the Bots
  field of the menu's Request a session; a site offers as many as it chooses, and
  lixen.com none yet ([The session fleet](kubernetes-fleet.md)). `-join <site>`
  asks for none: `-bot` there seats yours.

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
| `[bot]` | `actions_per_second`: intents sent per second of game time, default 12. `auto_fire`: the auto-fire its run starts each game on, `off`, `main` or `on` as `[play]` spells them, default the embedded `[play]`'s. `learn = true`: a solo run evolves the networks' weights, and `round_seconds`: the game time after which it ends a round the game has not, default 0 for never ([Networks and learning](#networks-and-learning)). |
| `[network.<name>]` | A policy `Steer` runs: `inputs`, `hidden` and, if any, weights written by hand. |
| `[regions.<name>]` | `initial` only. |
| `[states.<name>]` | States as in a scenario: hooks, transitions, `parent`. |
| `[macros]` | Inline macros, expanded at load. |

Anything else is refused at load: `description`, `[systems]`, `macro_files`, a
region's `file`, `enabled_systems`, `disabled_systems`, `only_systems` or
`background`, and, as in a scenario ([FSM reference](fsm-reference.md#notes)), a
key no table, action or guard reads or a value of the wrong type. Queued intents
wait in a queue of 256; an intent that finds it full is dropped and counted.
Allowance left idle builds up to one second's worth.

### Actions

| Action | Payload | Queues |
|---|---|---|
| `Intent` | `name`: a canonical action name (`internal/input/actionnames.go`), `mode_append` among them though no default key binds it, or a list drawn from, or `press` or `release`; `count`: a number or a `[min, max]` range drawn from; `char` for a char-wait action, or the key `press` presses and `release` lets go; `hold` for `press`: ticks, or `true` until its `release` | one key press or release |
| `Text` | `text` | one character per rune |
| `TypeGlyph` | none | the glyph under the cursor, switching to Insert first; nothing when there is none |
| `PointAt` | `target`; `key` picks the `button` on that key; `fire = true` clicks instead of moving; `away = true` aims away | a pointer move or click |
| `Steer` | `network`: the `[network.<name>]` to run, which a graph with one may leave out | a pointer move to the cell that network names, after the special attack where it asks for it |

`Intent` refuses an action that only opens a key sequence (`operator_delete`,
`prefix_g`, `prefix_macro_play`, `marker_glyph_*`) or that opens the command line
or the menu (`mode_command`, `config_menu`); `quit` ends the run. `press`
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

### Targets

| Target | Is |
|---|---|
| `glyph` | typeable text: a glyph neither gold nor part of a composite |
| `gold` | a gold sequence member |
| `nugget` | a nugget |
| `species` | a combat entity other than a cursor or a tower |
| `kraken` | a Kraken, by its body |
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
`EmitEvent`, `EnableSystem`, `DisableSystem`, `ReconcileSystems`. `vif -schema`
lists the bot's vocabulary, these left out, beside the scenario's.

### Timing and randomness

- A graph updates once a tick, every 50 ms of game time.
- `PointAt`, `Steer` and `TypeGlyph` read the world, so each acts only when all it
  queues is sent before the next tick. `Intent` and `Text` queue regardless; wait for
  them with `Idle`.
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
| a species it killed (`EventSpeciesKilled`, or `EventSpeciesKillConfirmed` once a predicted kill is proved) | +5; a data species' hit points, what it took to shoot down; a Kraken's the ticks its hit points take at four a second, for one shot down that fast or faster, and for a slower one the share that pace is of its own, a quarter at least |
| a gold sequence it completed (`EventGoldCompleted`) | +25 |
| its cursor defeated (`EventCursorDefeatState`) or crushed (`EventCursorCrushed`) | -100, once a round: a crush is a defeat too, and a last hit's energy can lift a defeat and drop it again before the new game |

A round is one game, from its start to the new game its own play brought on, and
scores how far its cave scrolled, in columns, plus what the rows above earned in
it, the last aside: a defeat ends every round, so it tells none from another.
Time alone scores nothing. A run that closes a round by the clock
(`round_seconds`) would else pay a ship for outlasting it, hopping about a
Kraken it barely scratches; as it is the scroll halts for the fight, the Kraken
shot down is what pays, and the sooner it is down the further the round
travels. `cave`'s first cave scrolls a column a tick, so there a round's score
is near its ticks; its rounds at 120x40, the mean of 800 over seeds 1 to 8:

```
graph                      score   ticks
no input                     165     159
default                      160     155
flee threats within 20       245     240
```

Fitness is in the `-headless` summary, the `bot stopped` log record, which also
holds the rounds and their summed score, and a seat's `bot left` record. From a
checkout with `bin/vif` built,
`./script/test.sh botfitness [graph-file] [seeds] [secs]` plays `default` headless
and flat out at 120x40 for `secs` (60) over seeds 1..`seeds` (4) on the embedded
scenario and `td`, and prints each scenario's sum. Given a graph file, it
fails unless that graph beats `default` on every scenario; with none, the sums
are the baseline to beat. It appends a `stop` region (states `StopWait` and
`StopQuit`), so a graph cannot use those names.

## Networks and learning

A graph may declare networks: policies whose weights are numbers a search can
tune, where a graph's states are not. `Steer` reads a network's sensors, runs
them through it and points the cursor at the cell its outputs name. A graph whose
scenario plays in phases gives each a network and steers by the one its guards
pick: the shipped [`cave`](../internal/asset/bot/cave.toml) flies the `cave`
scenario by two, `raid` while its raiders fly and `kraken` while its Kraken is
up, which share next to nothing. A network needs the pointer free, and a bot's
run starts on the embedded `[play]`'s pointer whatever the player's own says, and
on the auto-fire its `[bot]` names. `cave` takes `main`, the gun alone: the
special attack fired unasked spends in a game's first seconds the heat that
outlasts the energy, and what its blasts happen to catch is luck in the score.

```toml
[network.raid]
hidden = 6
inputs = ["ceiling", "floor", "species_x", "species_y", "species_near_x", "column"]

[states.Fly]
on_update = [
    { action = "Steer", payload = { network = "kraken" }, guard = "HasTarget", guard_args = { target = "kraken" } },
    { action = "Steer", payload = { network = "raid" }, guard = "Not", guard_args = { guard = { name = "HasTarget", args = { target = "kraken" } } } },
]
```

- A network's name is lowercase letters, digits and underscores, as it is part
  of a file's name.
- `inputs`: the sensors read, in the order the weights take them. Each reads the
  cells the view shows: an offset is from the cursor's cell, in view widths
  across and view heights down, and what is not sighted reads 0.
  - `ceiling`, `floor`: the rock's edges above and below the cursor's column,
    the view's own rows where no rock stands, as at `cave`'s Kraken, where they
    are the map's edges; `ceiling_near` and `floor_near` an eighth of the view
    ahead, `ceiling_far` and `floor_far` a quarter.
  - `species_x`, `species_y`, `species_enraged`: the nearest species, as the
    `species` target counts them, and 1 while it is enraged; `species2_*` the
    second nearest and `species3_*` the third. `cave` flies three raiders at
    most and any may charge or teleport, so its network lists all three: the
    nearest two would drop one for no reason its charge knows.
  - `species_near_x`, `species_near_y`, and the same of `species2` and
    `species3`: that species' nearness, a vector toward it 1 long beside the
    cursor and falling to nothing a quarter of the view's width away, rows
    counted twice. An offset grows with distance, so only a nearness lets one
    weight back off what is close and leave alone what is far.
  - `landing_x`, `landing_y`, `landing_near_x`, `landing_near_y`: the nearest
    cell something is about to land on and its nearness, a raider's landing
    marker, where its charge or teleport ends, or the cell a Kraken's charge
    will end on while it spins up and charges. `landing_soon` is how soon, from
    0 as the warning starts to 1 as the body leaves for it, which a player
    watches too. `landing2_*` and `landing3_*` are the next two, so every
    landing a cave can hold is read.
  - `kraken_x`, `kraken_y`: the Kraken's body; `kraken_near_x`, `kraken_near_y`
    the nearness of the nearest cell it covers, legs and all; `kraken_reach` how
    far out its striking legs are, 0 to 1.
  - `column`: the cursor's place across the view, -1 at its left edge, 1 at its
    right.
  - `energy`: 0 with none, a half at 1,000, toward 1; `heat`: a half at 10.
  - Loot has no sensor: a drop homes on its owner, so its place steers nothing.
- `hidden`: the tanh units between the inputs and the outputs, 0 to 32.
- The outputs are three, each the direct sum of its weights over the inputs plus
  the hidden units' and its bias, a weight within 4 either side of zero:
  - `x` and `y` are an offset in view widths and heights, kept to the view.
  - `special` asks for the special attack while it is positive, ahead of the
    move and as often as auto-fire would.
- No weight is written: `cave` leaves its networks' to the learner, which draws
  them at random and keeps what plays best, so nothing it does was put there by
  hand, the use of its heat among it. A graph may still write some, as the
  tables `x`, `y` and `special` of a network, each an output's weights from the
  inputs it names and its `bias`; they are then every run's first candidate.
  With no weight and no population a network asks for nothing and the ship
  holds still.

### Learning

With `[bot] learn = true` a solo watched or headless run learns each network's
weights as it plays: a genome plays a round, and its outcome is the round's score
([Fitness](#fitness-and-comparing-graphs)) from the tick its network first
steered in it, the whole round for `raid` and from the Kraken's rise for
`kraken`.

- Each network has a learner of its own, the `StreamingEngine` of
  `lixenwraith/genetic` ([AI §3](ai-physics-and-evolution.md#3-genetic-library))
  with one evaluation in flight: an archive of the 32 best genomes scored. A
  population's first 32 rounds play genomes drawn at random, each weight three
  hundredths of its bound about zero; after them children are mostly one
  parent's, mixed a hidden unit or an output at a time, three in a hundred of
  their weights stepped. A member's score is the mean of its rounds, and every
  third candidate is the best member short of eight rounds again, so what tops
  the archive is proved or falls.
- The learners explore in turn. One plays a candidate and the rest the members
  they field, unscored, so every score in an archive is of a round the other
  networks played at their best, and follows from the candidate's own change.
  The turn passes once a round has settled the candidate, and at once where the
  round never reached its network, as `kraken`'s in a round lost to a raider, so
  no learner waits on another.
- A round's score is mostly luck, the game it drew: a candidate whose first round
  would enter a full archive plays a second, and enters or not on that round
  alone. A run fields the best member eight rounds have proved, which is seldom
  the top of the archive, and with none the weights its graph wrote, if any.
- A step is a share of a weight's range: a new population starts at 0.1 and
  settles at 0.03 as its best stops improving.
- A round the game has not ended within the graph's `round_seconds` is closed
  as far as it came, by a new game the run asks for, so a candidate nothing
  beats is scored and the next one plays. `cave` takes half an hour.
- A run in a session does not learn, since any participant's defeat ends every
  round there: its bots, seats among them, play what is fielded. A round an
  operator ends (`:n`) scores nothing.
- A bot alone plays `-size`, else 120x40, watched or not, so one population is
  scored by one game: stop a headless run, start a watched one, and it goes on
  from the same files.
- A headless run alone and flat out plays its graph in several games at once,
  `-games` of them, each a run of its own on a seed drawn from the run's, and all
  teach the one learner a network and keep the one file. Each game carries one
  candidate of one network beside the members the rest field, as a lone game
  does.
  - A round closes into the learners in the order of the tick it closed at and
    then of its game, as if the games took turns a tick at a time, so one seed
    and one count of games search alike however the cores run them.
  - A game whose round closes ahead of the others' ticks waits for them, and its
    core stands free meanwhile: the count is one more than the cores unless
    `-games` names it, which keeps each core playing.
  - A run its graph quits ends every game at the tick its own reached, and
    `-games 1` learns as a watched run does, round for round.
- A watched run's bar says what each round came to as it closes and how far each
  learner is, `Round 12 scored 5480 | kraken recent 2210, fielding 2790 over 9 |
  raid recent 4100, fielding 5300 over 31`; a headless run prints `Bot learned
  raid: rounds R of T, recent M, fielding S over C` for each as it stops, and
  each save writes a `bot population kept` log record, a learner's each
  however many games play. `recent` is the mean of the learner's last rounds,
  candidates and all, which no luck picked.

#### The population file

A network's population is kept in the user root as
`bot/<graph>.<scenario>.<network>.json`, written each half minute of play and as
the run stops; a browser keeps none. Removing the file starts that population
over, as does a change of layout (`inputs` or `hidden`), which the file's
`layout` digest names.

- `rounds`: every round scored since the population began, over all its runs.
- `recent`: their mean lately, and `step`: the mutation step in force, which the
  next run takes up.
- `members`: the archive, best `score` first. It is no generation: a member
  stays for as long as its score keeps it among the 32, so members of every age
  stand side by side.
  - `score` is the mean of the rounds the member has played and `rounds` how
    many: 1 or 2 is a guess, dozens a measure.
  - `born` is the population's round the member entered at.
  - `genes` are the weights: each hidden unit's input weights and bias, then
    each output's direct weights, hidden weights and bias.
- `fielded`: the member a round plays where its learner tries no candidate, -1
  while none has eight rounds.

What is not kept: the candidate in play, the engine's queue and its random
stream, so a run resumes the archive and not the exact search.

A run whose disk fails learns on in memory and says so once, in the log (`bot
population not kept`) and on a watched run's bar:

- a file that cannot be read is left as it is, and the run keeps nothing;
- a file that cannot be parsed starts that population over, and the next save
  replaces it;
- a save that fails is tried again each half minute, and only the first is said.

A run that ends without keeping what it learned ends with that error.

What a run learns from nothing in `cave`, held out over 60 rounds of four seeds
no run learned on, each closed at the graph's limit, where the weights last
written by hand score 11,896 and a ship that holds still 165: after 22 minutes
on four cores, five games' 25 million ticks or one game's 6 million beside three
others, the runs field

```
seed                1       2       3       4
five games       3720    5464    6141    5891
  its median     1277    1070    2320    1344
one game         3887    4556    4753    3159
  its median     1105    1057    1382    1198
```

- Half the rounds or more are lost in the first cave, where the hand weights
  lose a quarter, and what five games field after two million ticks is about
  what they field after 25: a game on every core gets there sooner, no further.
- It finds the special attack and leans on it, about ten blasts in the first
  cave, the first as the raiders close, and held off it loses every round
  there; it holds no heat back
  ([todo](todo.md#train-a-network-bot-live-in-cave)).
- A round snowballs, since a ship that outlives the first cave has its gun
  raised and its energy deep, so a few long rounds carry the mean: read the
  median, and several seeds.

A watched run at speed 1 shows the play, and a headless one flat out fills the
files. What a game on every core added while the graph wrote its weights by
hand and a round scored its ticks, on that score, is in the same todo item.

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
- It issues no commands.
- It reaches only what its view shows. A seat's view is its holder's screen, so a
  person's bots see what that person sees and play differently across terminals.
- It plays cooperatively: cursor weapons do not strike cursors.
- A solo bot run is reproducible from its seed, and a learning one from its
  seed, its count of games and the population it started with. In a session only
  a journal reproduces a run: the front instance's, or a seat's this process
  journals; of a learning run's games, the run's own.
- Graph state is in no capture: a correction or a reset reaches a graph only
  through the world it reads next.
- A guest's bot has no link of its own, so it shows no throughput and is not
  evicted as slow; what it sends counts against its holder's ingress budget.
- The door admits any key, and every participant needs the same build.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `bot graph "x" not found as a path, in a configuration root or embedded` | Check the name and `-config-dir`, or give a path. |
| `bot <name>: …` at start or on `:bot add` | The load error names the graph and its fault; fix it and run `vif -check`. |
| A `roam` or `patrol` bot plays an old policy | An installed file of that name shadows `default`: use `-bot default` or update the file. |
| `dropped` above 0 in `bot stopped` or `bot left` | The graph queues faster than its rate; guard its actions with `Idle` or raise `actions_per_second`. |
| `-speed max cannot pace a session` | Run one bot alone, without `-host`, `-join` or further bots. |
| `-headless requires -bot or -replay …` | Give it a bot; `-watch` and `-serve` do not combine with it. |
| `-games N: only a -headless bot alone and flat out whose graph learns plays more than one game` | Run the graph alone, with no session, further bots or `-speed` but `max`, and with `learn = true` in its `[bot]`; or leave the count out. |
| `Bot <graph>: …` on the status bar | The seat failed to join; the log has `bot not seated` with the error. |

The holder logs `bot seated`, `bot left` and `bot not seated` for each seat, whose
own instance logs under `instance` `seat N`; a watched or headless bot logs
`bot stopped`.

## Not built yet

- [Expose a bot difficulty knob](todo.md#expose-a-bot-difficulty-knob)
- [Train a network bot live in `cave`](todo.md#train-a-network-bot-live-in-cave):
  what the learner still lacks
- [Train bot graphs offline](todo.md#train-bot-graphs-offline)
- [Adapt bots to the session's players](todo.md#adapt-bots-to-the-sessions-players)

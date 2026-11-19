# Bots

A bot is a participant driven by a graph instead of a terminal. It holds a roster
slot and a cursor, unless the map places none, and its intents (keys and mouse
alike) pass through the same mode router as a person's, under the same session
rules. Bots are cooperative: they play beside people against the environment.

## Quick start

```sh
vif -bot                              # you and one bot: the scenario's own, or default
vif -bot 2:default                    # you and two default bots
vif -host :7777 -bot 2                # host with two bots; others may join
vif -join 127.0.0.1:7777              #   joining it from another terminal
vif -join 127.0.0.1:7777 -bot 2       # join with two bots of your own
vif -bot -watch                       # watch one bot play; no player of yours
vif -bot -watch -size fit             #   on a map the size of this terminal
vif -bot default -headless -speed max -j   # record a solo bot flat out; Ctrl-C ends it
vif -s cave -bot -watch               # watch cave's own bot fly it, learning on as it goes
vif -s cave -bot -headless -speed max # the same flat out on every core, kept at each Ctrl-C
vif -s cave -bot seed -headless -speed max   # learn from its start written by hand instead
vif -replay <journal>                 # replay what it recorded
vif -serve :7777 -size 120x40 -bot 2  # dedicated host with two bots and room for people
vif -spectate 127.0.0.1:7777          #   spectating it from another terminal, in no seat
```

## Flags

| Flag | Effect |
|---|---|
| `-bot [N[:graph]\|graph]` | Add N bots playing graph. N is 0..16 and 0 adds none; no count is one, and no graph the scenario's own bot, `default` where it carries none ([Writing a graph](#writing-a-graph)). |
| `-bot <verb> …` | Keep bot files and exit: `list`, `create`, `import`, `export` or `clear` with what follows it ([A player's bot files](#a-players-bot-files)). |
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
| `-check` | Validate every bot graph in the configuration roots, the scenario's own and the embedded ones, with the rest of the configuration. |

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
| `:bot [N[:graph]\|graph]`, `:bot add …` | Seat more bots, one where no count is given; a solo run first opens a session only they can join. A count of 0 is refused. |
| `:bot drop <slot>` | Drop one of your bots, or as host any bot. |
| `:bot list` | Every graph a seat can play with what it has learned here, then this run's bots: slot, graph, joining or leaving. |
| `:bot create`, `import`, `export`, `clear` | Keep a player's bot files ([below](#a-players-bot-files)). |
| `:player` | List the slots: `3 player`, `3 alice` for a signed-in player, `4 bot of 3`, or `5 player (away)` for one who left. |
| `:player drop <slot>` | Host only: drop a player and its bots, or one bot. |

Slots accept `10`, `A` or `0xA` for the cursor marked `A`. The
[configuration menu](config-menu.md#bots) offers the same operations.

The words after `-bot` are the words after `:bot`, one grammar
(`engine.BotVerbs`): `-bot`, `-bot 2:cave` and `-bot add 2:cave` seat bots as the
run starts, `-bot drop` is refused, a run's alone, and `-bot` with a verb that
keeps bot files acts, prints and exits, for the scenario `-s` names.

A watched bot takes `:`, the menu keys and the help key. While you hold its command
line, the menu or help, its graph supplies no input: a run in a session keeps
ticking, a solo run waits.
`:n`, `:pause`, `:bot`, `:bot drop` and `:player` work there, and what its viewer
changes of `[play]` or the zoom is saved as a player's is: a preference is the
operator's, whatever the run presents.

### A player's bot files

The user root's `bot/` holds a player's own graphs, `<name>.toml`, beside what
every graph learned ([The population file](#the-population-file)). The verbs
that keep them take a fixed count of words and print a line each, in a run as
status messages the console lists; a browser has no files and reads `Bot files
unavailable in a browser`, `list` excepted.

- `list`: each graph a seat can play, yours first, then the installed, the
  scenario's and the shipped `default`, with each population it keeps in the
  run's scenario: `raid 814 rounds, recent 51440, fields 140138 over 12`.
- `create <name>`: a new graph of yours, the scenario's own with no weight,
  which learns from nothing. A name is one word that is no count and no verb.
  - `create <name> import <graph>` copies that graph's document whole.
  - `create <name> import <graph>.<network>` copies it with the weights of
    that network alone, so one population starts from a scenario's and another
    fresh: `-s cave -bot create mine import cave.kraken`.
- `import <graph>[.<network>] <name>`: writes a graph's weights, every
  network's or the one's named, into a graph of yours that exists
  (`bot.Graph.Fit`).
  - A weight goes by its input's name, its output's and its hidden unit's place;
    one yours has and the other lacks is zero.
  - A network, an input, an output or a hidden unit yours lacks lands nothing:
    a network is taken whole or not at all.
  - What a population of yours already kept still plays, and learns on, the
    new weights a candidate among its members; `clear` it to start from them.
- `export <graph>[.<network>] <file>`: the graph's document with the member
  each of its populations fields in the run's scenario, or the one network's
  named, and what it wrote where none is proved: a file a scenario carries as
  its bot, or another player imports. It reads back the same genome bit for
  bit (`bot.Graph.Document`), each weight not zero written, a hidden unit a
  `[[network.<n>.units]]` table and an output a `[network.<n>.<unit>]` one, at
  the document's end.
- `clear <graph>[.<network>]`: removes what the graph of that name learned in
  the run's scenario, every network's population or the one's named, which
  start over. A run learning one keeps it again as it saves.

`<graph>` is a graph as `-bot` names one, by its name or a file's path, or
failing that a scenario's name, for that scenario's own bot. `.<network>` after
it is one of its `[network.<name>]`, and `<name>` a graph of yours by its name.
The scenario is never written: it is the run's, `-s` on the command line. A
member is its genes, whatever scored them: `export` reads a population of any
`version` whose `layout` is the network's.

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
`/etc/vif`, a player's own; then as the bot of that name the run's scenario
carries; then as the embedded `default`, the generic bot, bound to no scenario.
`make install-config` installs `default` into the user root. `roam` and `patrol`
load `default` unless a root holds a file of that name. `-d` does not pin bot
graphs.

A scenario carries the graphs made for its map among its own files, its bots,
which its entry lists, as `cave`'s does:

```toml
bots = ["bot/cave.toml", "bot/seed.toml"]
```

- Each is a graph, named by its file as a root's `bot/<name>.toml` is, one word
  with no dot. They are part of the scenario: digested with it, sent with it to
  a session's guests, parsed by `-check`.
- The first is the scenario's own bot: what a seat that names no graph plays, from
  `-bot`, `-bot 2`, `:bot` and `:bot 2` alike. A scenario that lists none has
  `default` play.
- A player's graph of the same name plays in the scenario's place, so a copy is
  how one is changed, which `create <name> import <graph>` writes
  ([A player's bot files](#a-players-bot-files)); what any of them learns is
  kept in the user root ([The population file](#the-population-file)).
- A run that joins a session resolves its graphs once it has joined, against the
  session's scenario. A seat held over a change of scenario whose graph the new
  one does not carry plays the new one's own.

### Document

One self-contained TOML file in the [FSM dialect](fsm-reference.md):

| Table | Holds |
|---|---|
| `[bot]` | `actions_per_second`: intents sent per second of game time, default 12. `auto_fire`: the auto-fire its run starts each game on, `off`, `main` or `on` as `[play]` spells them, default the embedded `[play]`'s. `learn = true`: a solo run evolves the networks' weights, and `round_seconds`: the game time after which it ends a round the game has not, default 0 for never. `inputs`: the sensors every network reads ahead of its own ([Networks and learning](#networks-and-learning)). |
| `[network.<name>]` | A policy `Steer` runs: `inputs`, `hidden`, `outputs`, the `while` that has it steer and, if any, weights written by hand. |
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
| `Steer` | `network`: the `[network.<name>]` to run, or a list of them, the first whose `while` holds running; a graph with one may leave it out | a pointer move to the cell that network names, after the special attack where it asks for it |

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
| `InState` | `region`, `states` | the scenario's region runs and, where `states` names any, is in one of them or in a state within one |
| `Own` | `field` | that event payload field names the bot's own cursor |

### Targets

A target is one of the kinds the [catalogue](bot-catalogue.md#kinds) marks as
one, the nearest of it to the cursor.

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
lists the bot's vocabulary, these left out, beside the scenario's, and what a
network reads and asks for.

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
| `Walk` | now and then moves 2 to 10 cells, and points where its network `walk` asks | to `Seek`, `Gold` or `Nugget` once one exists; at 1% an update to `Leap` |
| `Leap` | clicks a random shown cell | to `Walk` after 0.4 s |
| `Nugget` | jumps to the nugget | to `Seek` after 0.3 s |

`walk` is `default`'s one network and the catalogue's stress test: loaded with
every input, then left the 161 that read anything while it steered in the
embedded scenario, `cave` and `td`. Text, gold and nuggets are the typing
states' to reach, and no other cursor, missile or third landing was in view. No
weight is written for it, so a bot seated beside a player asks nothing of it and
plays as its states say. A run of `default` alone learns it
([Learning](#learning)), a population a scenario, and closes a round each half
hour where the game ends none. What that fields held out, from nothing on three
seeds: in the embedded scenario after four games' 4,800,000 ticks, 132 rounds
and two minutes on four cores, over 24 half-hour rounds; in `cave` after one
game's 600,000, some 3,700 rounds, over 100:

```
scenario    in Walk   unlearned   walk learned
embedded      18%       622,620   704,339  681,384  674,730
cave          90%           215       258      247      225
td           0.1%
```

- The embedded scenario pays by its Krakens: 97 in 100 of what `default` earns
  is their bounty, 50,000 each, twelve a half hour and a thirteenth in one
  round of 24. A learned walk shoots one or two more down. Seed 1's fields
  698,025 on 24 rounds of other seeds, and written into the document earns
  12.7 million over an unbroken game's 600,000 ticks where none earns 11.4.
- A round must span what pays: closed every five minutes, before the second
  Kraken, 99 rounds learned nothing that 48 could tell from none.
- What is learned there is no place to stand. The three walks idle mostly in
  the view's lower corners, 16 to 17 ticks in 100 for the unlearned 20, yet a
  walk written to hold a corner of the view loses 6 to 12 in 100 and one that
  holds its middle 17.
- What is learned is its scenario's. The embedded scenario's walk written into
  `default` earns -8,304 and -6,352 over `armada`'s 60,000 ticks where none
  earns 7,868 and 7,024, and nothing either way in `td` and `cave`. So
  `default` writes none, and a bot seated beside a player has a learned walk
  only from a graph that writes one, as `export` makes of a population.
- In `cave` auto-fire's special spends the heat a ship lives on, so `walk`
  learns little there, a round of 158 ticks grown to 170 to 195. A copy that
  sets `auto_fire = "main"` and drops its `outputs` line, so `walk` asks for
  the special too, fields 35,341, 1,734 and 3,001 after the same ticks, their
  medians 2,446, 910 and 1,067. With every input, 369, `walk` learns `cave`
  as shipped no better: 251, 236 and 253.

Run `vif -check` after editing: it prints `bot ok: <path>` for each graph that
loads and fails naming each that does not. A graph the scenario carries is held
to it as well: an `InState` that names a region or a state the scenario has not
loads and never holds, so `-check` fails naming it; a player's graph, which any
scenario may play, is held to none. A graph outside every root is checked by
putting it under `<dir>/bot/` and running `vif -check -config-dir <dir>`.

## Fitness and comparing graphs

A bot's fitness weighs the events its own steps dispatched. It measures the graph;
players have no score.

| Event | Weight |
|---|---|
| a correctly typed character (`EventBoostReward`) | +1 |
| a typing error (`EventBoostDeactivate`) | -1 |
| a species it killed (`EventSpeciesKilled`, or `EventSpeciesKillConfirmed` once a predicted kill is proved) | +5; a data species' twelve times its hit points, what it took to shoot down; a Kraken's the ticks its hit points take at four a second, for one shot down that fast or faster, and for a slower one the share that pace is of its own, a quarter at least |
| a gold sequence it completed (`EventGoldCompleted`) | +25 |
| its cursor defeated (`EventCursorDefeatState`) or crushed (`EventCursorCrushed`) | -100, once a round: a crush is a defeat too, and a last hit's energy can lift a defeat and drop it again before the new game |

A round is one game, from its start to the new game its own play brought on, and
scores how far its cave scrolled, in columns, plus what the rows above earned in
it, the last aside: a defeat ends every round, so it tells none from another.
Time alone scores nothing. A run that closes a round by the clock
(`round_seconds`) would else pay a ship for outlasting it, hopping about a
Kraken it barely scratches; as it is the scroll halts for the fight, the Kraken
shot down is what pays, and the sooner it is down the further the round
travels. `cave`'s first cave scrolls a column a tick and its raiders have 8 hit
points, 96 a kill; its rounds at 120x40, the mean of 800 over seeds 1 to 8, the
kills auto-fire's own:

```
graph                      score   ticks   kills
no input                     225     159    0.74
default                      205     155    0.57
flee threats within 20       304     240    0.71
```

Fitness is in the `-headless` summary, the `bot stopped` log record, which also
holds the rounds and their summed score, and a seat's `bot left` record. From a
checkout with `bin/vif` built,
`./script/test.sh botfitness [graph-file] [seeds] [secs]` plays `default` as
written, its learning left off, headless and flat out at 120x40 for `secs` (60)
over seeds 1..`seeds` (4) on the embedded scenario and `td`, and prints each
scenario's sum. Given a graph file, it
fails unless that graph beats `default` on every scenario; with none, the sums
are the baseline to beat. It appends a `stop` region (states `StopWait` and
`StopQuit`), so a graph cannot use those names.

## Networks and learning

A graph may declare networks: policies whose weights are numbers a search can
tune, where a graph's states are not. `Steer` reads a network's sensors, runs
them through it and points the cursor at the cell its outputs name. A graph whose
scenario plays in phases gives each a network, bound by its `while` to the
states it is for, and steers by a list of them. The `cave` scenario's own bot,
[`cave`](../wad/scenario/base/cave/bot/cave.toml), flies it by two, `raid` while
its raiders fly and `kraken` while its Kraken is up, which share next to
nothing, each picked by a guard on a `Steer` of its own. A network needs the
pointer free, and a bot's run starts on the embedded `[play]`'s pointer whatever
the player's own says, and on the auto-fire its `[bot]` names. `cave` takes
`main`, the gun alone: the special attack fired unasked spends in a game's
first seconds the heat that outlasts the energy, and what its blasts happen to
catch is luck in the score.

```toml
[bot]
inputs = ["ceiling", "floor", "column"]

[network.kraken]
while = { name = "InState", args = { region = "boss", states = ["BossFight"] } }
inputs = ["kraken_near_x", "kraken_near_y"]
outputs = ["move"]
x = { kraken_near_x = -1.0, column = -0.1 }

[network.raid]
hidden = 6
inputs = ["species_x", "species_y", "species_near_x"]
y = { species_y = 0.9, hidden = [0.5] }
units = [{ species_near_x = 1.0, bias = -0.2 }]

[states.Fly]
on_update = [{ action = "Steer", payload = { network = ["kraken", "raid"] } }]
```

- A network's name is lowercase letters, digits and underscores, as it is part
  of a file's name.
- `while`: the guard that has the network steer, written `{ name, args }` as
  `And` lists one. It is asked of no state of the graph, so a guard of a state's
  time (`StateTimeExceeds`) or of an event's payload is refused at load.
  `InState` binds a network to the scenario's own states. A `Steer` plays a
  network only while its `while` holds, and over a list asks each in turn,
  once an update, and plays the first whose does; a network with none always
  steers, so only a list's last may have none.
- `inputs`: the sensors read, in the order the weights take them, after those
  `[bot] inputs` lists for every network of the graph. Each is named in the
  [catalogue](bot-catalogue.md#inputs): the nearest of a kind by rank and field
  (`species_x`, `species2_near_y`, `bullet_vx`, `landing3_soon`), the rock's
  edges at the cursor's column and ahead, the first cell that blocks the cursor
  each way, and the bot's own, from its place in the view to its weapons. Each
  reads the cells the view shows and keeps to its scale, and what is not
  sighted reads 0. A `Steer` scans the world for the kinds its network's inputs
  name and for no other, and allocates nothing: in `cave` its reading and its
  pass take 1.2 µs while raiders fly and 3.8 µs with a Kraken up, whose members
  it walks, of a tick's 180. A scan tells the one field it holds of each thing,
  as a species' `enraged`; an input reads any other of the thing's entity, a
  lookup each. A network with no weight written neither reads nor asks.
  - Every input at once, 367 through six hidden units, is what
    `BenchmarkEveryInput` (`internal/bot`) times: 4.4 µs with eight of each kind
    in view, 10 µs with 64 and 43 µs with 512. Played, the reading alone takes
    7 µs in the embedded scenario, 9 µs in `cave` while raiders fly and 19 µs
    under its Kraken and in `td`.
  - An offset grows with distance, so only a nearness lets one weight back off
    what is close and leave alone what is far.
  - `cave` flies three raiders at most and any may charge or teleport, so its
    network lists all three and their three landings: the nearest two would
    drop one for no reason its charge knows. How soon a landing lands is what a
    player watches too.
  - The rock's edges are the map's own where no rock stands, as at `cave`'s
    Kraken.
  - Loot has no sensor: a drop homes on its owner, so its place steers nothing.
- `hidden`: the tanh units between the inputs and the outputs, 0 to 32.
- `outputs`: those it has of the [catalogue's](bot-catalogue.md#outputs),
  `special` and `move` when it lists none. One left out is never asked for and
  has no weights. Each unit of an output, `special`, `x` and `y`, is the direct
  sum of its weights over the inputs plus the hidden units' and its bias, a
  weight within 4 either side of zero.
- A graph may write any weight: a unit's table (`x`, `y`, `special`) gives its
  weight from each input by name, its `bias` and, as `hidden`, a weight a hidden
  unit in their order, and `units` lists a table a hidden unit, its weight from
  each input and its `bias`. What is written is every run's first candidate and
  what is not is zero: with no weight and no population a network asks for
  nothing and the ship holds still. The cave scenario's two bots are the same two
  networks: [`seed`](../wad/scenario/base/cave/bot/seed.toml) writes a start by
  hand, twelve direct weights that hold the nearest raider's row, back off what
  is near and keep to the middle, and `cave` a model learned from it, the member
  each network's population fielded, 119 of its 540 weights not zero. Over 100
  rounds of seeds 101 to 104 `seed` fields 27,614 and `cave` 300,260, 85 of them
  closed at the graph's limit; what runs learn is [below](#the-population-file).
  - A move is an offset from the ship in view widths and heights, taken each
    update. A positive weight on a thing's offset closes that share of the gap
    an update: in `cave` 0.6 to 1.2 on `species_y` all hold a raider's row.
  - A negative weight on an offset runs the ship to the rock. Back off through
    a nearness, and pair it with a centring term, `column` across and
    `ceiling` and `floor` down, or the ship pins to an edge.
  - Zero is holding still, and a small bias a steady drift: a move is a cell
    once it reaches half of one, 0.0042 in `x` and 0.014 in `y` on the 119
    columns and 36 rows a headless bot's screen shows. Leave every weight not
    meant unwritten.

### Learning

With `[bot] learn = true` a solo watched or headless run learns each network's
weights as it plays: a genome plays a round, and its outcome is its network's
part of the round's score ([Fitness](#fitness-and-comparing-graphs)), the
columns of the ticks the network steered and what they earned. In `cave` that
is the fight for `kraken`, from the Kraken's rise to its death or the round's
end, and all the rest for `raid`.

- Each network has a learner of its own, the `StreamingEngine` of
  `lixenwraith/genetic` ([AI §3](ai-physics-and-evolution.md#3-genetic-library))
  with one evaluation in flight: an archive of 32 genomes scored, ranked by
  what their rounds prove (below).
  Where its graph writes no weight a population's first 32 rounds play genomes
  drawn at random, each weight three hundredths of its bound about zero; where
  it writes some the population starts from them alone. Children are mostly
  one parent's, mixed a hidden unit or an output at a time, three in a hundred
  of their weights stepped. A member's score is the mean of its rounds, and every
  third candidate is a member again, by turns the one short of eight rounds it
  trusts most, so a guess is proved or falls, and the less played of the member
  fielded and its rival, so the title is tested for as long as it is held. A
  candidate the archive already holds is that member again, as a kept
  population's written start is, and a member pushed off the archive while a
  round of it plays comes back, if at all, on the mean of all its rounds.
- Every learner plays a candidate, and a round's ticks are the network's that
  steered last: `kraken` learns only while a Kraken is up and `raid` everywhere
  else, neither paid for the other's ticks. A candidate whose network a round
  never reached plays on until one does, as `kraken`'s through the rounds lost
  to a raider, so no round passes unscored and none scores a network that did
  not play it.
- A round's score is mostly luck, the game it drew, so the archive ranks by
  what a member's rounds prove and not by its mean, through the engine's
  `SetRank`. A member's mean is trusted as if eight more rounds had scored the
  population's recent mean, so eight lucky rounds do not unseat thirty good
  ones, and eight rounds prove it.
  - The proved stand first, the most trusted at their head, eight of them at
    most; the other 24 places are probation, ranked among themselves the same
    way.
  - A candidate whose first round would enter a full archive plays a second,
    and enters or not on that round alone. It takes the place of the last on
    probation and never of a proved member, however high its round.
  - A member that reaches its proof joins the proved. With nine, the last of
    them leaves as the next newcomer enters: another where the one just proved
    ranks above it, and the one just proved where it does not.
  - A run fields the first of the proved, and with none the weights its graph
    wrote, if any; its rival is the next of them.
- A step is a share of a weight's range: a new population starts at 0.1 and
  settles at 0.03 as its best stops improving.
- A round the game has not ended within the graph's `round_seconds` is closed
  as far as it came, by a new game the run asks for, so a candidate nothing
  beats is scored and the next one plays. `cave` takes half an hour: a limit
  under a fight's length scores it before its kill pays, and with 150 seconds
  its own model learned on to one that fields 20,441 for 300,260.
- A run in a session does not learn, since any participant's defeat ends every
  round there: its bots, seats among them, play what is fielded. A round an
  operator ends (`:n`) scores nothing.
- A bot alone plays `-size`, else 120x40, watched or not, so one population is
  scored by one game: stop a headless run, start a watched one, and it goes on
  from the same files.
- A headless run alone and flat out plays its graph in several games at once,
  `-games` of them, each a run of its own on a seed drawn from the run's, and all
  teach the one learner a network and keep the one file. Each game carries a
  candidate of every network, as a lone game does.
  - A round closes into the learners in the order of the tick it closed at and
    then of its game, as if the games took turns a tick at a time, so one seed
    and one count of games search alike however the cores run them.
  - A game whose round closes ahead of the others' ticks waits for them, and its
    core stands free meanwhile: the count is one more than the cores unless
    `-games` names it, which keeps each core playing.
  - A run its graph quits ends every game at the tick its own reached, and
    `-games 1` learns as a watched run does, round for round. No round a game
    closes past that tick is scored; a game ahead of the run's own has played
    past it all the same, so the ticks and actions a run sums over its games
    are not its seed's as its populations are.
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
over, as does a change of layout (the inputs read, `hidden`, or an output left
out), which the file's `layout` digest names, and one of what a round scores,
which its `version` counts.

- `rounds`: every round scored since the population began, over all its runs.
- `recent`: their mean lately, and `step`: the mutation step in force, which the
  next run takes up.
- `members`: the archive in its order, the proved first with the most trusted
  at their head, then those on probation, the most trusted first. It is no
  generation: a proved member stays until nine are proved and it is the last
  of them, and one on probation until a newcomer ranks above it, so members of
  every age stand side by side.
  - `score` is the mean of the rounds the member has played and `rounds` how
    many: 1 or 2 is a guess, dozens a measure.
  - `born` is the population's round the member entered at.
  - `genes` are the weights: each hidden unit's input weights and bias, then
    each output's direct weights, hidden weights and bias.
- `fielded`: the member a round plays where its learner tries no candidate, the
  first of the proved, and -1 while none is.

What is not kept: the candidate in play, the engine's queue and its random
stream, so a run resumes the archive and not the exact search.

A run whose disk fails learns on in memory and says so once, in the log (`bot
population not kept`) and on a watched run's bar:

- a file that cannot be read is left as it is, and the run keeps nothing;
- a file that cannot be parsed starts that population over, and the next save
  replaces it;
- a file another run has taken further on, by its `rounds`, is left as it is:
  two runs may learn one population, as a watched run opened beside a headless
  one, and the one further on keeps the file;
- a save that fails is tried again each half minute, and only the first is said.

A run that ends without keeping what it learned ends with that error.

What a run learns in `cave` is read by what it fields held out: 100 rounds of
seeds 101 to 104 with no learner, each closed at the graph's limit. From
`seed`'s weights, which field 27,614 themselves with a median of 33,174 and
reach the Kraken in 73 rounds, four games' three million ticks, two minutes on
four cores; from nothing, `cave`'s graph with no weight written, where a ship
that holds still scores 159, one game's 600,000. Each beside what the archive
fielded while it ranked by the plain mean, a newcomer's one round against a
member's forty; `lost` is the rounds of 100 that end before the first Kraken:

```
                    by the plain mean          by what rounds prove
start     seed     mean   median    lost     mean   median    lost
seed's       1    52588    59824       7    47746    60407      18
             2    87882    80992       8    68569    62922       0
             3   113267    86658       1    78865    87994       9
nothing      1     6435     2874      75     7063     2970      70
             2     2304     1694      85     3420     2134      83
             3     4431     2703      82     7652     2766      67
             4     4603     2405      81     2604     2022      89
             5     4384     2944      75     2939     2057      86
             6     4104     2530      75     5264     2718      77
```

- Neither start is told from luck. From nothing the six means come to 4,377
  and 4,824. From `seed`'s the three come to 84,579 and 65,060, and three are
  no measure there: ranked by the plain mean, ten seeds field 42,704 to
  113,267, a mean of 58,005 and a median of 48,268, only the two that learn
  `kraken`'s fight within the run past 53,000. By its own rounds `raid`
  fields the same under both, 34,800 and 35,700. What the ranking changes is
  what an archive keeps, below.
- Width is not what limits a start from nothing. With every input in both
  networks, 369 for `raid`'s 39 and `kraken`'s 15 and 3,348 weights each for
  378 and 162, the six seeds field 2,252 2,558 3,170 3,587 5,022 and 5,196, a
  mean of 3,631 inside the spread of 4,824's. The game's 600,000 ticks take
  82 s for 79, and a population file 2.2 MB for 240 KB.
- A round snowballs, since a ship that outlives the first cave has its gun
  raised and its energy deep, so a few long rounds carry the mean: read the
  median, and several seeds.
- `cave`'s own weights are what a longer run of five games learned from `seed`'s:
  they field a mean of 300,260 and a median of 374,046, reach the Kraken in 88
  rounds and shoot down 767, and its half hour, 36,000 ticks, closes 85 rounds
  of the 100.
- What the learners fielded while they explored in turn, a round each scored
  whole, and what longer runs did then, is in the
  [todo](todo.md#train-a-network-bot-live-in-cave).

Training on from `cave`'s own weights is read the same way, four games on four
cores, and a mean of 100 such rounds moves by 13,000 with luck alone. The runs
to thirty and forty-five million ticks are seed 1's again, each on the
populations the run before it kept:

```
                          by the plain mean          by what rounds prove
ticks          seed       mean   median   lost       mean   median   lost
cave's own weights      300260   374046     12
 3,000,000        1     300260   374046     12     300260   374046     12
                  2     300260   374046     12     318164   373067      6
15,000,000        1     330680   374835      5     321365   374498      7
                  2     324772   374859      8     318164   373067      6
30,000,000        1                                341376   374570      2
45,000,000        1                                318082   374609      9
```

What each archive holds after fifteen million ticks, a member proved by eight
rounds, and where the one it fields stands in it:

```
                 by the plain mean                     by what rounds prove
seed 1  raid     5 proved, the 30th of 32 over 73      8 proved, the 1st over 55
        kraken   1 proved, the 26th over 10            8 proved, the 1st over 40
seed 2  raid     1 proved, the 32nd over 16            8 proved, the 1st over 72
        kraken   none proved: the written weights      8 proved, the 1st over 32
```

- Every run adds its rounds to the population, a few seconds' as well: two runs
  of 600,000 ticks left `raid` 47 rounds and then 90. Flat out a game closes a
  round every second or two; watched at speed 1 a round the ship survives is
  half an hour, and a run stopped inside one scores nothing for it.
- Ranked by the plain mean, a member many rounds had brought down to what it
  is worth was pushed off by newcomers one round put above it. From `seed`'s
  in three million ticks `raid` lost 11, 8 and 7 members of eight rounds or
  more, one tested over 50, and on seed 2 here `kraken` kept none, every
  member at the limit's score on a round or two. Ranked by what rounds prove,
  none leaves to a newcomer: on seed 1 ten proved `raid` members left in
  fifteen million ticks, each to a ninth the trusted mean put above it, the
  most tested over 15 rounds.
- What a run fields is within luck of what the plain mean's archive fielded.
  On seed 2 both titles pass within three million ticks, to the members that
  hold them still at fifteen. On seed 1 `raid`'s passes within fifteen million
  to a member 55 rounds have tested, stays with it through thirty, and at
  forty-five lies level between it and `cave`'s own, 50,682 over 196 rounds
  and 50,934 over 197; `kraken`'s lies as level between two of 101 and 102.
- Probation was read at 8 places and at 24: they field the same pair on both
  seeds here and on two of `seed`'s three, as the share binds only once more
  are proved than it leaves room for. With 24, parents are drawn as before
  the ranking: 98 tournaments in 100 hold a member on probation, where 8
  leaves 58.
- A candidate that entered on its first round, with no second, fielded 48,109
  from `seed`'s where 64,839 with one: two rounds in three then go to the
  first rounds of new candidates, most of them worse than their parents.
- The trusted mean's weight errs to the long tested, and the title is tested
  only against the next most trusted: after forty-five million ticks a `raid`
  at 66,694 over eight rounds stands third and no turn plays it again
  ([todo](todo.md#have-a-scenario-describe-the-bots-that-play-it)).

A watched run at speed 1 shows the play, and a headless one flat out fills the
files.

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
| `bot graph "x" not found as a path, in a configuration root, in scenario <name> or embedded` | Check the name and `-config-dir`, or give a path; a scenario's bot is found only in a run of that scenario. |
| `cave` flies as if it had learned nothing | A `bot/cave.toml` in a root is a player's own and plays in the scenario's place; installs before the scenario carried its bots left one there, which `make install-config` names. Remove it. |
| `bot <name>: …` at start or on `:bot add` | The load error names the graph and its fault; fix it and run `vif -check`. |
| A `roam` or `patrol` bot plays an old graph | An installed file of that name shadows `default`: use `-bot default` or update the file. |
| `dropped` above 0 in `bot stopped` or `bot left` | The graph queues faster than its rate; guard its actions with `Idle` or raise `actions_per_second`. |
| `-speed max cannot pace a session` | Run one bot alone, without `-host`, `-join` or further bots. |
| `-headless requires -bot or -replay …` | Give it a bot; `-watch` and `-serve` do not combine with it. |
| `-games N: only a -headless bot alone and flat out whose graph learns plays more than one game` | Run the graph alone, with no session, further bots or `-speed` but `max`, and with `learn = true` in its `[bot]`; or leave the count out. |
| `Bot <graph>: …` on the status bar | The seat failed to join; the log has `bot not seated` with the error. |

The holder logs `bot seated`, `bot left` and `bot not seated` for each seat, whose
own instance logs under `instance` `seat N`; a watched or headless bot logs
`bot stopped`.

## Not built yet

- [Have a scenario describe the bots that play it](todo.md#have-a-scenario-describe-the-bots-that-play-it)
- [Train a network bot live in `cave`](todo.md#train-a-network-bot-live-in-cave):
  what the learner still lacks
- [Train bot graphs offline](todo.md#train-bot-graphs-offline)
- [Teach a bot its player's style, for an auto-pilot](todo.md#teach-a-bot-its-players-style-for-an-auto-pilot)

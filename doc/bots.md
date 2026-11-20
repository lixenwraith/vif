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
- A bot's defeat or crush ends the game only where every cursor in play is a
  bot's, this run's or another's: a seat, or a run `-bot` plays itself. Beside a
  person it comes back a second later as a new game makes it, at the cell and
  with the heat, energy and loadout a cursor starts on, and the game ends at a
  person's defeat, at `:n`, or as the last person leaves a bot inside that
  second ([Multiplayer §5.4](multi-player.md#54-wraiths)).
- `:bot drop <slot>` drops one of your bots; the host may drop any player's.
  `:player drop <slot>` (host only) drops a player with all its bots, one bot,
  or a run a bot plays itself, which `:bot drop` does not reach.
- `:leave` takes your bots out of the session with you: they leave it first and
  are seated again in the session your run goes on in, each taking back the place
  it had. A run that loses its host and finds nobody to take over goes on the same
  way, keeping every other player's place too where its session migrates.
- A dropped person starts a new local game without its bots; a dropped watched or
  headless run stops. A dropped player is kept out of that session: its key, and
  its account where the roster holds one, is refused there.
- A headless bot ends once its host leaves and no participant, itself included,
  takes over; one that takes over plays on with its own bots, as any session only
  bots occupy does. A watched bot plays on alone, as the fork.

## Commands and the menu

| Command | Effect |
|---|---|
| `:bot [N[:graph]\|graph]`, `:bot add …` | Seat more bots, one where no count is given; a solo run first opens a session only they can join. A count of 0 is refused. |
| `:bot drop <slot>` | Drop one of your bots, or as host any player's. |
| `:bot list` | Every graph a seat can play with what it has learned here, then this run's bots: slot, graph, joining or leaving. |
| `:bot create`, `import`, `export`, `clear` | Keep a player's bot files ([below](#a-players-bot-files)). |
| `:player` | List the slots: `3 player`, `3 alice` for a signed-in player, `4 bot of 3` for a seat, `6 bot` for a run a bot plays itself, or `5 player (away)` for one who left. |
| `:player drop <slot>` | Host only: drop a player and its bots, or one bot. |

Slots accept `10`, `A` or `0xA` for the cursor marked `A`. The
[configuration menu](config-menu.md#bots) offers the same operations.

Which bot a seat plays is its player's to choose, and each plays what the
player's runs have taught it in the scenario, or what its graph writes where
they taught it nothing. After `-bot`, after `:bot` and in the menu alike:

- no graph: the scenario's own, the model it carries, or `default` where it
  carries none;
- `seed`, or any other the scenario carries: as `cave`'s start written by hand;
- `default`: the shipped generic bot;
- `mine`, a copy of yours to train on: `create mine import cave` or `import
  seed` first, a run of it alone learns, and `clear mine` starts it over;
- a fresh one: `create mine`, the scenario's own with no weight;
- a graph you wrote: its path, or its name once it is in the user root's `bot/`.

A graph of the scenario's or `default` that your runs taught plays as taught;
`clear` it to have what is shipped again, or train a copy and leave it be.

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

- `list`: each graph a seat can play and whose it is, in the order the roots
  are searched, then the scenario's and the shipped `default`, with each
  population it keeps in the run's scenario: `raid 814 rounds, recent 51440,
  fields 140138 over 12`.
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
failing that a scenario's name, for that scenario's own bot; `clear` takes a
name alone. `.<network>` after it is one of its `[network.<name>]`, and
`<name>` a graph of yours by its name.
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
- The embedded scenario carries `main`, `default`'s graph with a walk learned
  there ([Examples](#examples)).
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
| `[regions.<name>]` | `initial` only. Regions update in name order, so what one queues in a step goes ahead of a later one's. |
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
| `Steer` | `network`: the `[network.<name>]` to run, or a list of them, the first whose `while` holds running; a graph with one may leave it out. `outputs`: those of the network's this `Steer` acts on, every one when none is named | a pointer move to the cell that network names, after the special attack where it asks for it |

`Intent` refuses an action that only opens a key sequence (`operator_delete`,
`prefix_g`, `prefix_macro_play`, `marker_glyph_*`), one that opens the command
line, the menu or the console, and a pause; `quit` ends the run. `press`
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
| `Asks` | `output`, `above`, `network` | the network's output of one unit, `special` or `wheel`, reads above `above` (0) this update; a graph's one network needs no name, and one with no weight asks nothing |

### Targets

A target is one of the kinds the [catalogue](#kinds) marks as
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
right, backs off a threat on it and collects nuggets between patrols:

| State | Does | Moves on |
|---|---|---|
| `Seek` | points at the nearest glyph | to `Flee` if a threat is within 3, `Walk` if its network asks for the wheel, `Gold` if gold exists, `Nugget` if one is within 12, `Type` on a glyph, `Walk` with no glyph |
| `Type` | types the glyph under the cursor | to `Flee` or `Walk` as `Seek` does, else to `Seek` once off text |
| `Gold` | jumps to gold and types it | to `Seek` when off gold, 1.5 s after entering |
| `Walk` | now and then moves 2 to 10 cells, and points where its network `walk` asks | stays while `walk` asks for the wheel; to `Seek`, `Gold` or `Nugget` once one exists; at 1% an update to `Leap` |
| `Flee` | points away from the nearest threat | to `Seek` once none is within 5 |
| `Leap` | clicks a random shown cell | to `Walk` after 0.4 s |
| `Nugget` | jumps to the nugget | to `Seek` after 0.3 s |
| `Fire`, in the region `special` | has `walk` ask for the special attack, whatever the states above do | never |

The other shipped bots, and what each scores, are under
[The shipped bots](#the-shipped-bots). Run `vif -check` after editing a graph:
it prints `bot ok: <path>` for each graph that loads and fails naming each that
does not. A graph a scenario carries is also held to that scenario: an `InState`
naming a region or a state the scenario lacks would load and never hold, so
`-check` fails naming it. A player's graph, which any scenario may play, is held
to none. A graph outside every root is checked by putting it under `<dir>/bot/`
and running `vif -check -config-dir <dir>`.

## Fitness and comparing graphs

A bot is measured two ways, both from the events its own steps dispatched.
Players have no score.

**Fitness** is one sum over the whole run:

- a correctly typed character (`EventBoostReward`): +1;
- a typing error (`EventBoostDeactivate`): -1;
- a gold sequence completed (`EventGoldCompleted`): +25;
- a kill (`EventSpeciesKilled`, or `EventSpeciesKillConfirmed` once a predicted
  kill is proved):
  - of a species a scenario declares: 12 times the hit points its program
    gives it, 96 for a raider of `cave`'s first cave;
  - of a Kraken: the ticks its hit points take at four a second, for a kill at
    that pace or faster. A slower kill earns the share that pace is of its
    own, a quarter at least;
  - of any other species: +5;
- its cursor defeated (`EventCursorDefeatState`) or crushed
  (`EventCursorCrushed`): -100, once a round.

**A round's score** is what learning reads. A round is one game, from its start
to the new game its own play brought on. Its score is the columns the cave
scrolled in it, which only `cave` has, plus the fitness earned in it without
the -100: a defeat ends every round, so it tells no round from another.

- Time alone scores nothing. A run may close a round by the clock
  (`round_seconds`), and a score for time would pay a ship for outlasting it.
- In `cave` the scroll halts while a Kraken is up. Shooting it down is what
  pays there, and the sooner it is down the further the round travels.
- In a scenario with no cave a round's score is the fitness earned in it.

Fitness is in the `-headless` summary, in the `bot stopped` log record, which
also holds the rounds and their summed score, and in a seat's `bot left`
record. From a checkout with `bin/vif` built,
`./script/test.sh botfitness [graph-file] [seeds] [secs]` plays `default` as
written, its learning left off, headless and flat out at 120x40 for `secs` (60)
over seeds 1..`seeds` (4) on the embedded scenario and `td`, and prints each
scenario's sum. Given a graph file, it fails unless that graph beats `default`
on every scenario. It appends a `stop` region (states `StopWait` and
`StopQuit`), so a graph cannot use those names.

## Networks and learning

A graph may declare networks: policies whose weights are numbers a search can
tune, where a graph's states are not. `Steer` reads a network's inputs, runs
them through it and acts on its outputs: it asks for the special attack where
they ask, then points the cursor at the cell they name.

A network needs the pointer free, so a bot's run starts each game on the
embedded `[play]`'s pointer whatever the player's own says, and on the
auto-fire its `[bot]` names. Every shipped bot takes `auto_fire = "main"`, the
gun alone, and gives its networks the output `special`: the special attack is a
network's to ask for, and each ask costs one action of the bot's rate. Left to
auto-fire in `cave` it spends, in a game's first seconds, the heat a ship lives
on once its energy is gone.

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
- `inputs`: what it reads, in the order its weights take them, after those
  `[bot] inputs` lists for every network of the graph. Each is a name of the
  [catalogue](#inputs): the nearest of a kind by rank and field (`species_x`,
  `species2_near_y`, `landing3_soon`), the rock's edges, the first cell that
  blocks the cursor each way, and the bot's own readings.
  - Each reading covers the cells the view shows and keeps to its scale. What
    is not sighted reads 0.
  - An offset grows with distance. Only a nearness lets one weight back off
    what is close and leave alone what is far.
  - List every one of a kind the scenario can field. `cave` flies three
    raiders at most, so its `raid` reads all three and their three landings:
    reading two would drop one for no reason the network can know.
  - The rock's edges are the view's own where no rock stands, as at `cave`'s
    Kraken. Loot has no input: a drop homes on its owner.
- `hidden`: the tanh units between the inputs and the outputs, 0 to 32.
- `outputs`: those it has of the [catalogue's](#outputs), `special` and `move`
  when it lists none. One left out is never asked for and has no weights. Each
  unit of an output (`special`, `x`, `y`, `wheel`) is the sum of its weights
  over the inputs, plus its weights over the hidden units, plus its bias. A
  weight lies within 4 either side of zero.
  - `wheel` is had only by listing it, and `Steer` does nothing with it. The
    graph reads it by the guard `Asks`, so its states hand over to the network
    while it asks and take over again when it stops.
- `while`: the guard that has the network steer, written `{ name, args }` as
  `And` lists one. `InState` binds a network to the scenario's own states. It
  is asked of no state of the graph, so a guard of a state's time
  (`StateTimeExceeds`) or of an event's payload is refused at load.
  - A `Steer` plays a network only while its `while` holds. Given a list, it
    asks each in turn, once an update, and plays the first whose `while`
    holds. A network with none always steers, so only a list's last may have
    none.
  - A guard on the `Steer` action itself does the same job, and is what the
    shipped bots use: `cave` steers by `kraken` while `HasTarget` sights a
    Kraken and by `raid` otherwise.
- Weights: a unit's table (`x`, `y`, `special`, `wheel`) gives its weight from
  each input by name, its `bias` and, as `hidden`, a weight for each hidden
  unit in their order. `units` lists a table for each hidden unit: its weight
  from each input and its `bias`. What is not written is zero.
  - A network with no weight written neither reads nor asks, and the ship
    holds still.
  - A move is an offset from the ship in view widths and heights, taken each
    update. A positive weight on a thing's offset closes that share of the gap
    an update: in `cave`, 0.6 to 1.2 on `species_y` holds a raider's row.
  - A negative weight on an offset runs the ship to the rock. Back off
    through a nearness, and pair it with a centring term: `column` across,
    `ceiling` and `floor` down.
  - A small bias is a steady drift: a move is a cell once it reaches half of
    one, 0.0042 in `x` and 0.014 in `y` on the 119 columns and 36 rows a
    headless bot's screen shows.
- Cost: a network is read and passed once an update, however many guards and
  `Steer`s ask. A `Steer` scans the world only for the kinds its inputs name
  and allocates nothing. In `cave` reading and passing take 1.2 µs while
  raiders fly and 3.8 µs with a Kraken up, of a tick's 180 µs.

### Learning

**What is learned.** The weights of each network of a graph whose `[bot]` says
`learn = true`. Each network has a learner and a population of its own. The
graph's states and rates are not learned.

**When.** Only while the bot plays alone: a `-watch` or `-headless` run of the
graph with no session. Each round such a run plays one candidate for each
network and scores it. In a session a bot does not learn, a seat beside its
player included, because a round there is not its own: another's defeat ends
it. It plays what its population *fields*, the member its rounds have proved
best, or the weights its graph writes where none is proved.

**From what signal.** [A round's score](#fitness-and-comparing-graphs), split
between the networks by who steered.

- The ticks of a round belong to the network that steered last, until another
  steers. A network's part of the round is the columns scrolled and the fitness
  earned in its ticks.
- In `cave`, `kraken`'s part runs from the Kraken's rise to its death or the
  round's end, and `raid`'s is all the rest.
- A candidate whose network never steered in a round stays in play until a
  round reaches it, so no round scores a network that did not play it.
- A round ends when the game starts over by its own play, as at a defeat. One
  that lasts `round_seconds` of game time is closed by a new game the run asks
  for, and scored as far as it came. A round the operator ends (`:n`) scores
  nothing.
- `cave` and `default` close a round at half an hour. The limit must span what
  pays: the embedded scenario pays by its Krakens, about twelve in half an
  hour, and a run closing rounds every five minutes learned nothing there.

**How the search goes.** Each learner drives the `StreamingEngine` of
`lixenwraith/genetic` ([AI §3](ai-physics-and-evolution.md#3-genetic-library)):
an archive of 32 members, each a genome with the mean score of the rounds it
has played and their count.

- A new population starts from the weights its graph writes, where it writes
  any. Where it writes none, its first 32 rounds play genomes drawn at random,
  each weight about zero with a spread of 0.12.
- A new candidate is bred from members of the archive: mostly one parent's,
  mixed a hidden unit or an output at a time, with three weights in a hundred
  stepped by a normal draw of 0.24, which is 0.03 of a weight's range.
- Every third candidate is a member played again. By turns it is the unproved
  member the archive trusts most, and whichever of the fielded member and its
  rival has played fewer rounds.
- A round is mostly luck, so the archive ranks a member by a *trusted* mean:
  its mean weighed as if eight more rounds had scored the population's recent
  mean. Eight lucky rounds then do not outrank thirty good ones.
- A member with eight rounds is *proved*. The proved rank above the rest, eight
  of them at most. The other 24 places are probation.
- A candidate whose first round would enter a full archive plays a second
  round, and enters or not on that one alone, since the first was picked for
  its luck. It takes the place of the last on probation, never of a proved
  member. When a ninth member is proved, the least trusted of the proved leaves
  as the next newcomer enters.
- The member fielded is the most trusted of the proved. Its *rival* is the
  next.

**Several games at once.** A headless run alone and flat out plays its graph in
`-games` games at once, each on a seed drawn from the run's. All of them teach
the one learner of each network and keep its one file.

- The count is one more than the cores unless `-games` names it. A game whose
  round closes ahead of the others' ticks waits for them, and the extra game
  uses the core it frees.
- Rounds close into the learners in the order of the tick they closed at, then
  of their game. One seed and one count of games therefore search alike however
  the cores run them, and `-games 1` learns as a watched run does.
- A run its graph quits ends every game at the tick its own reached. A game
  ahead of it has played past that tick all the same, so the ticks and actions
  a run sums over its games are not reproducible as its populations are.
- A core plays 4,700 to 7,000 ticks of `cave` a second. Five games on four
  cores play 27,000 a second where one game plays 6,900.

**Start, stop, reset, export.** A bot alone plays `-size`, else 120x40, watched
or not, so a watched run and a headless one teach the same population:

```sh
vif -s cave -bot -headless -speed max      # learn flat out, a game on every core
vif -s cave -bot -watch                    # the same population, watched at speed 1
vif -s cave -bot seed -headless -speed max # learn from the start written by hand
vif -s cave -bot list                      # what each population holds
vif -s cave -bot clear cave                # start cave's populations over
vif -s cave -bot clear cave.raid           #   or one network's
vif -s cave -bot export cave model.toml    # what is fielded, as a bot document
vif -s cave -bot create mine import cave   # a copy of yours to train, cave's left be
```

Ctrl-C stops a run, which saves as it stops. A run stopped inside a round
scores nothing for it: flat out a round closes every second or two, and watched
at speed 1 a round the ship survives is half an hour.

**Telling whether it got better.** A learner reports three figures, and only
two can be trusted:

- `recent` is the mean score of the learner's last 200 or so rounds, candidates
  and all. No luck picked those rounds, so it is the figure to compare two runs
  or two settings by.
- `fielding S over C` is the archive's own mean S for the member it fields,
  over the C rounds it has played. It overstates: the members still in an
  archive are those whose luck held. In runs from nothing it was 1.7 to 3 times
  what the same member scored held out.
- *Held out* is what the fielded members score over fresh rounds, on seeds the
  run never played, with no learner. It is what a seat will play. No command
  reads it yet: it takes a scratch test
  ([agent notes](agent-notes.md#measuring)).

Luck is wide in `cave`: a round scores from a few hundred, lost in the first
cave, to 375,000 for nine Krakens down in its half hour. Read a median and
what ended the rounds beside a mean, on several seeds. Between two samples of
100 held-out rounds the mean of `cave`'s model moves by 13,000. What a run
fields when it stops moves by more: two runs on one seed, whose learners
differed only in a population's first fifteen rounds, fielded 28,511 apart.

A watched run's bar says what each round came to as it closes and how far each
learner is: `Round 12 scored 5480 | kraken recent 2210, fielding 2790 over 9 |
raid recent 4100, fielding 5300 over 31`. A headless run prints `Bot learned
raid: rounds R of T, recent M, fielding S over C` for each learner as it stops,
and each save writes a `bot population kept` log record.

#### The population file

A network's population is kept in the user root as
`bot/<graph>.<scenario>.<network>.json`, written each half minute of play and
as the run stops. A browser keeps none. The learner lives beside the driver,
outside the world and its captures. The file is JSON because one cut short
never parses, where a TOML one could read as fewer members.

A population starts over when its file is removed (`clear`), when the network's
layout changes (the inputs read, `hidden`, or the outputs it has), which the
file's `layout` digest names, or when what a round scores changes, which its
`version` counts.

- `rounds`: every round scored since the population began, over all its runs.
- `recent`: their mean lately. `step`: the mutation step, a constant 0.03.
- `members`: the archive in its order, the proved first and the most trusted
  at their head, then those on probation. It is no generation: members of
  every age stand side by side.
  - `score` is the mean of the rounds the member has played and `rounds` how
    many: 1 or 2 is a guess, dozens a measure.
  - `born` is the population's round the member entered at.
  - `genes` are the weights: each hidden unit's input weights and bias, then
    each output unit's input weights, hidden weights and bias.
- `fielded`: the place in `members` of the member a run fields, -1 while none
  is proved.

The candidate in play, the engine's queue and its random stream are not kept:
a run resumes the archive, not the exact search. A learner in a run takes an
`import` or a `clear` only from its next run.

A run whose disk fails learns on in memory and says so once, in the log (`bot
population not kept`) and on a watched run's bar:

- a file that cannot be read is left as it is, and the run keeps nothing;
- a file that cannot be parsed starts that population over, and the next save
  replaces it;
- a file another run has taken further, by its `rounds`, is left as it is. Two
  runs may learn one population, as a watched run opened beside a headless
  one, and the one further on keeps the file. What the other learned is lost;
- a save that fails is tried again each half minute, and only the first is
  said.

A run that ends without keeping what it learned ends with that error.

### The shipped bots

- [`default`](../internal/asset/bot/default.toml), embedded, the generic bot
  bound to no scenario ([Examples](#examples)). Its one network, `walk`, reads
  seventeen inputs: where the two nearest species and the Kraken are, the
  Kraken's hit points, and the bot's own column, row, energy and heat.
  - `walk` steers only in the state `Walk`. `default` is there while no text
    is left, or while `walk`'s output `wheel` reads above 1, which weights
    drawn at random seldom reach: text comes first until a run learns
    otherwise.
  - It writes one weight, the bias that has `walk` ask for the special attack
    always. A walk learned in one scenario loses in another, so it writes no
    other.
  - A run of `default` alone learns `walk`, in a population for each scenario.
    What that population fields is what `default` plays in that scenario from
    then on, as a seat too.
  - It sends sixteen actions a second, twelve for the keys and four for the
    special attack. The region `special` updates after `play`, so a key goes
    out ahead of a special attack asked for in the same step: the other way
    round, the blast took the glyph the key was for.
- [`main`](../internal/asset/scenario/bot/main.toml), the embedded scenario's
  own: `default`'s graph with a `walk` learned there from nothing in six
  minutes, 29 weights. A solo run learns on from them in `main.main.walk.json`.
  The two graphs have one layout, so a population of `default`'s copied to that
  name is `main`'s.
- [`seed`](../wad/scenario/base/cave/bot/seed.toml), `cave`'s start written by
  hand. It has two networks, `raid` and `kraken`, each its inputs through six
  hidden units. Twelve direct weights hold the nearest raider's row, back off
  what is near and keep to the middle.
- [`cave`](../wad/scenario/base/cave/bot/cave.toml), the `cave` scenario's
  own: `seed`'s two networks with a model learned from `seed`'s weights, 194 of
  its 540 weights not zero. A run of five games learned it, and a trainer then
  ran on from it in cycles of fifty minutes. The pair it fielded after the
  fourth cycle ships, and fifteen more cycles fielded no better. The trainer is
  stopped; its populations are on the branch `claude/cave-training-state`.

What each scores held out, with no learner. In `cave`: 100 rounds of seeds 101
to 104, each closed at half an hour, 36,000 ticks.

```
bot                    mean     median   rounds that reach the Kraken
cave, no weight         159        146                              0
seed                 27,614     33,174                             73
cave                341,701    373,798                             97
```

`cave` closes 90 of the 100 rounds at the limit and shoots down 8.5 Krakens a
round. A half-hour round pays by its Krakens, 375,000 for nine, 214,000 for
eight and 132,000 for seven, and none has shot down ten: the model is near what
the limit allows, and a bot document's `round_seconds` is its own to raise.

In the embedded scenario: 100 rounds of half an hour, seeds 201 to 204.

```
bot                    mean     median   Krakens met in most rounds
default, unlearned  622,467    620,447   twelve, in 84 rounds
main                718,430    721,430   fourteen, in 92 rounds
```

Nearly all of that is Kraken bounty, 50,000 each: 97 in 100 of what `default`
earns. A `default` that never fires the special attack scores 402,940.

`default` in the other scenarios: the mean score of a held-out round, unlearned
and with a `walk` learned from nothing on three seeds. `walking` is the share
of ticks spent in `Walk`:

```
scenario   walking   unlearned   learned, three seeds         learning run       held out
embedded      18%      620,489   683,519  708,624  694,133    4,800,000 ticks     24 rounds
armada        92%           85     7,437    5,729    9,091    2,400,000 ticks    100 rounds
cave          90%          220    15,049    8,341      668      600,000 ticks    100 rounds
td           0.1%       17,088   not read                                         12 rounds
```

- A few long rounds carry `cave`'s means: the three medians are 832, 910 and
  610. A round of `td` runs from 83 to 48,360, and the median of its twelve is
  8,504.
- Backing off a threat is a state of the graph, `Flee`, and no weight. Without
  it the median round of `td` scores 3,936, and with it 6,494.
- No run has learned a use for `wheel`. `td` is all text, so `wheel` is the one
  thing `walk` decides there, and 142 rounds of learning scored 3,767 where
  the unlearned scored 3,923, both read before `Flee` existed.

### Tried and not built

Each line is a change that was measured and left out. Do not try one again
without a new reason. Unless a line says otherwise, *fielded* is the mean score
of 100 held-out `cave` rounds, as above, of what a learning run fielded when it
stopped, with the change against the learner as built. Luck alone moves that
figure by tens of thousands on a learned model.

The learner's rules:

- A lighter trust weight, two or four rounds at the recent mean in place of
  eight. `raid`'s title changed hands 38 and 23 times in fifteen million ticks
  where it changes 9, and no run fielded more. Kept as a commit under closed
  PR 1125.
- A rival picked by plain mean: the title tested against the proved member
  with the highest plain mean, in place of the next most trusted. It played
  every proved member left waiting, and fielded 281,099 against 321,544 on the
  one start that told them apart. Kept as a commit under closed PR 1125.
- A title held only on 24 rounds, the proof for rank and breeding left at
  eight. On six seeds from each of three starts it moved neither the
  candidates' recent mean nor the proved members' held-out score by two
  standard errors. Never pushed.
- Ranking by the plain mean, the rule before the trusted mean. A newcomer's one
  lucky round displaced tested members: `raid` lost 11, 8 and 7 members of
  eight rounds or more in three million ticks from `seed`'s weights.
- Entry on a candidate's first round, with no second: fielded 48,109 against
  64,839 from `seed`'s weights.
- Eight places of probation in place of 24: fielded the same pair of members
  on four starts of five.
- A step schedule, `AdaptiveStrength` between 0.03 and 0.2. It stood at 0.03
  for 98 rounds in 100, and a step fixed there fielded 307,705 against its
  310,936 over six seeds.
- A step of 0.06: `raid`'s recent mean fell to 22,864 from 28,760.
- A share of weights stepped of 0.1 or 0.01, in place of 0.03: fielded 0.6 and
  0.67 times as much, over twelve seeds from weights written by hand.
- Rank by the mean of log scores, a repeat every second or every fifth
  candidate, a step floor of 0.06 or 0.015: each within one standard error of
  the learner as built, over twelve seeds.
- A new population drawn about a written start, every weight of it stepped. The
  start itself was lost: fielded 4,863 against 40,865 for starting from it
  alone.
- A first draw three times as wide, 0.1 of a weight's bound: from nothing
  fielded 974 and 1,339 against 2,906 and 3,654 over 40 rounds.
- Learners exploring in turn, one network's candidate a round and scored on
  the whole round: from nothing fielded 3,029 and 2,614 against 6,115 and
  6,640 for both at once.
- The same games for a candidate and its parent. Two members on one game part
  as soon as they differ, so pairing would take a tenth off a comparison's
  variance at most.
- A separable CMA-ES in place of the archive, on `lixenwraith/genetic`'s
  stand-in game: 3,290 against 3,545.

What a round scores, and when it ends:

- No column paid while a block stands within two rows of the ship. The ship was
  crushed less, and fielded 313,827 and 287,986 against 300,260 for the model
  it began from.
- Rounds closed at 150 seconds in place of half an hour: `kraken` is scored on
  fights cut before the kill pays. Fielded 20,441 and 107,101 against 300,260.
- A scenario that opens on the Kraken, to score `kraken` every round: fielded
  20,666 to 50,244 where `kraken` learned in `cave` fielded 23,074 to 51,120,
  on four seeds.
- A raider's kill paid a flat 100, a probe: one run of four learned to shoot
  in the first cave. What is built pays 12 times a species' hit points.

What a network reads and asks for:

- A move that names a place in the view, in place of an offset from the ship:
  a five-minute probe from nothing scored no better.
- Every input in both of `cave`'s networks, 369 for `raid`'s 39 and `kraken`'s
  15: from nothing six seeds fielded a mean of 3,631 against 4,824.
- All 161 inputs that read anything, for `walk`'s seventeen. Its candidates'
  recent mean in the embedded scenario was 624,000 against 662,000.
- The rock, the blocks and a landing added to `walk`'s seventeen: fielded
  656,410 against 692,286 in the embedded scenario.
- The embedded scenario's learned walk written into `default`: it scored
  -8,304 over 60,000 ticks of `armada` where no walk scored 7,868.
- Backing off a threat written as `walk`'s weights, in place of the state
  `Flee`: in `td` it gained seven tenths of what the state gains at most.

<!-- catalogue: generated, see its first lines -->
## Catalogue

What a graph's guards and `PointAt` take as a target, and what a network
reads and asks for. This section is generated from the tables of
`internal/bot/catalogue.go`: change a row there, then run
`VIF_WRITE_GENERATED=1 go test ./internal/bot -run Reference`. `vif -schema`
lists the same inputs, outputs and scales under `bot_catalogue`.

### Kinds

A kind is a class of thing the world holds. A guard or `PointAt` names one as
its `target`, nearest the cursor over the whole world; a network reads the
nearest ones the view shows, each by its fields.

- `glyph`: typeable text: a glyph neither gold nor part of a composite. A
  target. A network reads the 8 nearest, each by `x`, `y`, `near_x`, `near_y`.
- `gold`: a gold sequence member. A target. A network reads the 8 nearest, each
  by `x`, `y`, `near_x`, `near_y`.
- `nugget`: a nugget. A target. A network reads the 8 nearest, each by `x`, `y`,
  `near_x`, `near_y`.
- `species`: a combat entity other than a cursor or a tower. A target. A network
  reads the 8 nearest, each by `x`, `y`, `near_x`, `near_y`, `enraged`, `hp`,
  `size`, `vx`, `vy`.
- `kraken`: a Kraken, by its body, which may stand past a view that shows a leg;
  its nearness is that of the nearest cell it covers, legs and all. A target. A
  network reads the nearest, by `x`, `y`, `near_x`, `near_y`, `reach`, `hp`,
  `health`.
- `cursor`: another participant's cursor. A target. A network reads the 8
  nearest, each by `x`, `y`, `near_x`, `near_y`.
- `threat`: a species, a hostile bullet or a hostile missile, all weighted
  alike. A target.
- `button`: a scenario's button, or with key the one on that key, at its cell
  nearest the cursor. A target.
- `random`: a cell the view shows; PointAt only. A target.
- `landing`: a cell something is about to land on: a raider's landing marker,
  where its charge or teleport ends, or the cell a Kraken's charge ends on while
  it spins up and charges. A network reads the 8 nearest, each by `x`, `y`,
  `near_x`, `near_y`, `soon`.
- `bullet`: a hostile bullet. A network reads the 8 nearest, each by `x`, `y`,
  `near_x`, `near_y`, `vx`, `vy`.
- `missile`: a hostile missile. A network reads the 8 nearest, each by `x`, `y`,
  `near_x`, `near_y`, `vx`, `vy`.

### Inputs

A network's `inputs` name what it reads, in the order its weights take them.
`<kind>_<field>` is the nearest of a kind, `<kind>2_<field>` the second nearest
and so on. Every reading keeps to its scale, and what is not sighted reads 0.

#### Fields

- `x`, across: its offset across.
- `y`, down: its offset down.
- `near_x`, near: its nearness across.
- `near_y`, near: its nearness down.
- `enraged`, flag: 1 while it is enraged.
- `soon`, share: how soon it lands, from 0 as its warning starts to 1 as the
  body leaves for it.
- `reach`, share: how far out its striking legs are.
- `hp`, half at 32: the hit points it has left.
- `health`, share: the hit points it has left, of those it rose with.
- `size`, half at 16: the cells its body covers.
- `vx`, rate at 20: the cells a second it moves across.
- `vy`, rate at 20: the cells a second it moves down.

#### The rock

- `ceiling`, down: the rock's edge above a column, the view's own row where no
  rock stands.
- `floor`, down: the rock's edge below a column, the view's own row where no
  rock stands.

Each is read at a column, by what its name ends in:

- `ceiling`, `floor`: the cursor's column.
- `ceiling_near`, `floor_near`: the column an eighth of the view ahead.
- `ceiling_far`, `floor_far`: the column a quarter of the view ahead.

#### Blocks

A block is a cell the cursor may not enter: a wall, the rock or the map's edge.
Each is an offset from the cursor, to the cell past the view's edge where none
it shows blocks:

- `block_up`, down: the first cell above the cursor that blocks it.
- `block_down`, down: the first cell below the cursor that blocks it.
- `block_left`, across: the first cell left of the cursor that blocks it.
- `block_right`, across: the first cell right of the cursor that blocks it.

#### The bot's own

- `column`, place: the cursor's place across the view, -1 at its left edge and 1
  at its right.
- `row`, place: the cursor's place down the view, -1 at its top edge and 1 at
  its bottom.
- `energy`, half at 1000: its energy, whichever its polarity.
- `heat`, half at 10: its heat.
- `overheat`, share: its overheat, of what lights its ember.
- `ember`, flag: 1 while its ember burns.
- `shield`, flag: 1 while its shield is up.
- `boost`, share: its boost, by the time it has left of what it was lit for.
- `weapon_rod`, share: its rod, by the charges it holds of those it may.
- `weapon_launcher`, share: its launcher, by the charges it holds of those it
  may.
- `weapon_disruptor`, share: its disruptor, by the charges it holds of those it
  may.
- `weapon_turret`, share: its turret, by the charges it holds of those it may.
- `weapon_emitter`, share: its emitter, by the charges it holds of those it may.
- `weapon_gun`, share: its gun, by the charges it holds of those it may.

#### Scales

- across, -1 to 1: an offset from the cursor's cell in view widths, within 1 of
  zero for a thing the view shows.
- down, -1 to 1: an offset from the cursor's cell in view heights, within 1 of
  zero for a thing the view shows.
- near, -1 to 1: one part of a nearness: a vector toward the thing, 1 long
  beside the cursor and falling to nothing a quarter of the view's width away,
  rows counted twice.
- place, -1 to 1: a place in the view, -1 at one edge and 1 at the other.
- flag, 0 to 1: 0 or 1.
- share, 0 to 1: how much of its full it stands at.
- half, 0 to 1: a count c read as c/(c+half): 0 with none and toward 1, a half
  at the count its row names.
- rate, -1 to 1: a rate v read as v/(|v|+half): 0 at rest and toward 1 either
  way, a half at the rate its row names.

### Outputs

What a network asks for, in the order `Steer` acts on it. A graph writes an
output's weights in a table named for each of its units.

- `special`, units `special`: asks for the special attack while it is positive,
  ahead of the move and as often as auto-fire would.
- `move`, units `x`, `y`: points at a cell: x and y are its offset from the
  cursor in view widths and heights, kept to the cells the view shows.
- `wheel`, units `wheel`: asks its graph for the wheel: Steer does nothing with
  it, and the guard Asks holds while it is above what the guard names. A network
  has it only by listing it.
<!-- /catalogue -->

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
- It reaches only what its view shows. A seat's view is the size of its holder's
  screen, about the bot's own cursor: on a map no larger than that screen it
  shows the cells its holder sees, and on a larger one a window of that size
  around the bot. So a person's bots play differently across terminals.
- It plays cooperatively: cursor weapons do not strike cursors.
- It plays the game a person plays. The game differs for a bot in two decided
  ways only: its defeat ends a game only where every cursor in play is a bot's,
  and beside a person it comes back a second later
  ([Ownership and leaving](#ownership-and-leaving)). No other mechanic, event
  or engine path is made for a bot, and none is to be added (Lixen,
  2026-11-20).
- A solo bot run is reproducible from its seed, and a learning one from its
  seed, its count of games and the population it started with. In a session only
  a journal reproduces a run: the front instance's, or a seat's this process
  journals; of a learning run's games, the run's own.
- Graph state is in no capture: a correction or a reset reaches a graph only
  through the world it reads next.
- A guest's bot has no link of its own, so it shows no throughput and is not
  evicted as slow; what it sends counts against its holder's ingress budget.

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
| `-games is how many games a -headless -bot run learns from at once, 1 or more` | Give `-games` only with `-headless -bot`. |
| `Bot <graph>: …` on the status bar | The seat failed to join; the log has `bot not seated` with the error. |

The holder logs `bot seated`, `bot left` and `bot not seated` for each seat, whose
own instance logs under `instance` `seat N`; a watched or headless bot logs
`bot stopped`, and each further game a learning run plays logs under `game N`.

## Not built yet

- [Have a scenario describe the bots that play it](todo.md#have-a-scenario-describe-the-bots-that-play-it)
- [Train a network bot live in `cave`](todo.md#train-a-network-bot-live-in-cave):
  what the learner still lacks
- [Train bot graphs offline](todo.md#train-bot-graphs-offline)
- [Teach a bot its player's style, for an auto-pilot](todo.md#teach-a-bot-its-players-style-for-an-auto-pilot)

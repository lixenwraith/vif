# Menu

Press **Ctrl-G** or **F10**, hold a left press (a finger) on the status bar, or
enter **`:menu`** (`:g`, `:config`) during play. A **scenario** is the set of
regions that defines a game; a **game** is a scenario running in the engine. The
Settings page and Relay for others are `vif.toml`'s `[play]`: a run writes them
back as they change, and the next run starts as this one was left; the rest
changes this run alone, and the keymap is never rewritten. The terminal, the
desktop window and the browser draw the menu as the overlay below; Android draws
the same menu with its own widgets ([Android](#android)).

## Navigation

| Where | Keys | Action |
|---|---|---|
| Menu | Up/Down or `j/k` | Select a page or row; section titles are skipped. |
| Menu | Left/Right or `h/l` | Change a setting or mark a scenario; Right also enters a page. Text, form and action rows need Enter or Space. |
| Menu | Enter or Space | Enter a page, advance a setting, mark a scenario, type in a text box, or run an action. |
| Menu | `/` | Search every page: the rows matching all the words typed (in the page, section, label or description) list as `Page / Row` and act in place. Up/Down select while typing; Enter keeps the search for `j/k`, `/` edits it again, Escape clears it. |
| Menu | Home/End, PageUp/PageDown | Navigate longer lists. |
| Menu | A press (a click, a tap) | Select the row; on the selected row, or any row of the scenario list, act as Enter, or as Left on a setting's left half. |
| Menu | The wheel | Scroll the list `OverlayWheelRows` rows a notch, wherever the pointer is; the selection stays. |
| Menu | A press on the scrollbar, then a drag | Move the thumb's middle to the press, or take hold where it lands on the thumb, and carry it until the release; the selection stays. |
| Text box | Printable keys, Left/Right, Home/End, Backspace/Delete | Edit the row's text in place. Enter submits; an error stays beside the text. Up/Down or a press on another row leaves it, as Escape does. |
| Form | Up/Down | Move between fields; the focused field's help appears below. |
| Form | Left/Right, Home/End, Backspace/Delete | Edit the field. Printable characters, including `q`, enter text. |
| Form | Enter | Submit all fields. Invalid input keeps the form and its values. |
| Form | A press on a field | Focus it. |
| Anywhere | Escape | Cancel a text box or form, clear a search, return to the pages, then close. `q` also goes back in menus. A press outside the window does the same. |
| Anywhere | Ctrl-G or F10 | Close the menu and discard an unsubmitted form. |

A form of one field is a text box on its row; a longer one opens its own window.
Submitting a session action closes the menu and resumes play. Joining, router
requests and bot admission complete asynchronously; the status bar reports
progress or failure. Cancelling before submission starts no action. Replay
viewers retain their inspection-only command restrictions. A bracketed value marks
a row that is unavailable or read only; for an unavailable row the description
below the list starts with the reason, the sentence its command gives (`Logging
unavailable in a browser`, `Audio unavailable in this run`).

## Android

The Android app draws the menu with the platform's widgets, in the site's dark
theme, from a snapshot of the same pages and rows the run holds, and the run applies
each tap as it applies a key ([Native menu](native-menu.md)); a page's content, its
commands, its refusals and what `[play]` keeps are the same. The differences are the
host's:

- The six pages are headings, each ruled off from the one above, that expand in
  place under a chevron: an open page's rows sit under it in a tinted panel behind a
  stripe, its sections small capitals over a rule; which pages are open is kept for
  the run. A row that opens something ends in a chevron. A search field under the
  title lists the rows of every page matching the words typed, each with its page, in
  place of the tree, and they act in place.
- A setting is a switch, a value that opens a radio list, or − and + around a value;
  a scenario is a radio row, and Start names the one marked; an action is a row that
  acts when tapped, after a confirming dialog where it restarts, starts or leaves a
  game or drops a player or a bot; a one-field form is a field on its row, whose
  keyboard key submits, and a longer one opens a sheet with the focused field's help
  under it and Submit and Cancel; a read-only row is a label over its description.
  A field raises the keyboard its input takes: numbers for Count and Players,
  addresses for Address, Target and Site.
- A row that cannot act is dimmed and starts its description with the reason. The
  status bar's line, a refusal included, shows at the menu's foot while it is up.
- Back closes the innermost of a confirm, a radio list and a sheet, then clears a
  search, then closes the menu; so does a tap outside the window. × closes it.
- A hardware keyboard works as in the overlay, except in a field being typed in:
  `j`/`k` move, `h`/`l` change the focused row, `/` finds the search field, `q` and
  Escape go back, and Ctrl-G, F10 and F1 close the menu or open help over it.

## Pages

- **Game**: the game running now. Pause, Restart game, Game speed; a replay adds
  Restart replay.
- **Scenarios**: what to play. The installed scenarios, Start, Other scenario.
- **Multiplayer**: playing with others. Session, Players, Bots.
- **Settings**: your preferences, each saved to `[play]`. Sound, Controls,
  Display, Pause, Start.
- **Developer**: logging, the flight recorder, profiling, telemetry, the
  navigation overlays and the replay journal.
- **Launch options**: what a running game cannot change and the file or flag
  that sets it, on a platform without a command line where its launch arguments or
  `vif.toml` go.

### Game

**Pause** stops the game until **Resume**, as `:pause` or `p` does, and closes the
menu, which leaves it stopped; the status bar shows `PAUSED`. In a session only the
host can, the row reads Pause everyone, every participant stops at the same tick,
anyone joining lands paused, and the host's Resume resumes everyone. A game waiting
for its first press, or a session frozen while its host changes, refuses it with
the reason. **Restart game** starts the game over from its scenario's first tick, as
`:n` does. **Game speed** runs from 1/8x through 8x, in a session the host's.

### Scenarios

One row per scenario `:n <name>` would start: `main` and every
`scenario/<name>/scenario.toml` the configuration roots hold, the running one
marked `current`. Enter or a press marks one `(*)`; **Start &lt;name&gt;** below
starts a game of it, or starts the current one over, so a phone needs no name to
type. **Other scenario** is a text box for what `-s` takes, a name or a path, for
one the list does not hold. In a session only the host can start either, and its
participants rejoin.

### Multiplayer

**Current session** heads the page with the session's summary. **Host this
game** takes a listening address such as `:4242`, `127.0.0.1:4242` or
`ws://:4242`, and an Authority: blank keeps the startup policy, `host` ends the
session when its authority leaves, `migrate` allows a successor. The status bar
reports the bound address; share one your guests can reach, since the menu does
not change network reachability. A browser can join a session but not host one,
since a page cannot accept connections; the row says so.

**Join a session** is a text box for `host:port[/name]`, `vif://` and `tcp://`
links or `ws(s)://` URLs; an `http(s)://` site requests a new session with server
defaults. Joining replaces the current solo game only after admission succeeds.
An active session, a pending join, or a driven run without a restart loop
disables it.

**Request a session** asks a vif-router site for a session and joins it: **Site**
(default `https://lixen.com`), **Players** (total slots including you; `0` the
server default) and **Scenario** (the server's name; blank its default, local files
are not uploaded). It sends `POST /vif/api/sessions`; the server controls
capacity, scenarios, quotas and availability, and refusals appear in the status
bar with the current game kept. It asks for no bots of the session's own: add
yours under Bots once joined.

**Leave the session** runs `:leave`: the run plays on in a session of its own from
where this one stands, its bots seated again beside it, and the others play on
without it. A solo game, a session holding only your own bots, a pending join, or
a driven run without the play loop disables it. See
[Multiplayer §5.5](multi-player.md#55-leaving-a-session).

**Relay for others** runs `:relay on|off` and is saved in `[play]`: on, this run
carries the game for a player who reaches it and not the host, as after a host
loss; off, the default, it carries only those that joined through it, its bots and
its `-relay` door. A browser cannot relay, and the row says so. See
[Multiplayer §5.1](multi-player.md#51-succession).

Under **Players**, every other participant has a **Drop &lt;slot&gt;: player|bot**
row that only the host can activate. Dropping a player removes its bots; dropping a
bot leaves its holder and siblings. Commands are `:player` and
`:player drop <slot>`; `:bot drop <slot>` also lets the host remove any bot.
Decimal `10`, hexadecimal `A` and `0xA` all refer to the cursor marked `A`. A
player who left reads `player, away` while its Wraith waits; dropping it ends the
wait. Slots may be reused once a departed player is dropped.

### Bots

**Add bots** takes a count and policy graph name or path. `default` is the shipped
policy; a blank policy also uses it. Policies follow the normal config-root
resolution rules. Each bot occupies a player slot and leaves when its holder
leaves. Both hosts and guests can bring bots. The rows below list this run's bots,
including joining and leaving states: **Remove &lt;slot&gt;: &lt;policy&gt;** drops
one, its hexadecimal slot matching its cursor label, and **Cancel joining** stops a
pending one, marked leaving until its admission unwinds. Bots held by another
player are outside this list.

Adding bots to a solo game opens a session only they can join, in a browser too.
To invite remote players, **host at the intended listening address before adding
bots**. An existing session cannot be rebound through the menu. The same
operations are `:bot add [N[:graph]|graph]` and `:bot drop <slot>`; see
[Bots](bots.md).

### Settings

- **Sound**: sound effects, their volume in 5% steps, music.
- **Controls**: auto-fire (`off`, `main`, `on`), mouse input, pointer following.
- **Display**: Max FPS, the telemetry HUD.
- **Pause**: Pause in menu (`[play] pause_menu`, `:pause menu`) and Pause in
  command line (`pause_command`, `:pause command`), both on by default; see
  Pausing below.
- **Start**: Wait to start (`:wait`) holds each game a person plays, started or
  restarted, at its first tick until a key, click or tap the held game would
  ignore, the host's in a session, and a join for the press that takes its place
  there; a scenario's own restart never waits.

## Pausing

A game that can pause, solo or a host whose only other participants are its own
bots, pauses while the menu, help or telemetry is open unless Pause in menu is
off, and while the command line is open unless Pause in command line is off; a
new arrival resumes such a host. A session with other people keeps running. The
menu's title ends with what the game does meanwhile: `game paused`, `game running`
or `game waiting to start`; turning Pause in menu on or off with the menu open
shows at once. Game → Pause, `:pause` and `p` are another kind of pause, which
stands until resumed, whatever opens or closes; with the command line's pause off,
they are how a game is paused from the keyboard.

## Key binding and persistence

The `config_menu` action is bound to `ctrl_g` and `f10` in `normal_keys`,
`text_keys` and `overlay_keys`, and `pause` to `p` in `normal`, in
`input/keymap.toml`. Override those sections (or use `-k`) to add or change keys;
`:menu` remains available. Forms and text boxes use the text-key bindings, while
menu navigation uses overlay bindings.

Action rows (`Enter`) run the command they name and close the menu: `:pause`,
`:n`, `:leave`, `:t unpin`, `:t save`, `:log rec flush`, `:d cpu|heap|mutex|trace`
and `:replay restart`; Start and Other scenario submit `:n <scenario>` the same
way. Views (`:t`, `:d`, help), developer state commands (`:god`, `:energy` and the
like), `:step`, `:d break`, `:emit`, `:system`, `:region` and `:replay tick|time`
stay on the command line.

Use CLI arguments and `vif.toml` for launch defaults. `[play]` is written back as
it changes, from the menu or the command line, and a scenario change carries it
into the next run. A flag (`-mute`, `-nw`) sets its value for its run alone and is
never written. Every platform writes it the same way: the `vif.toml` the run read
when it is the player's own, else the user root's, created at the first change; a
browser keeps it in the site's storage
([Filesystem layout §3](filesystem-layout.md#3-resolution-policy)). Only a run
with no user root, or a browser refused its storage, keeps it to itself. The game
speed, HUD pins and standing breakpoints stay this run's. Exact values outside
menu presets remain available through commands such as `:log stat N`.

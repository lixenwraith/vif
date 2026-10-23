# Configuration menu

Press **Ctrl-G** or **F10**, hold a left press (a finger) on the status bar, or
enter **`:g`** (`:config`) during play. Settings take effect in the current run.
The Audio and Controls rows, the HUD, Relay and Wait to start are also
`vif.toml`'s `[play]`: a run writes them back as they change, and the next run
starts as this one was left; the rest changes this run alone, and the keymap is
never rewritten.

## Navigation

| Where | Keys | Action |
|---|---|---|
| Menu | Up/Down or `j/k` | Select a category, setting or action. |
| Menu | Left/Right or `h/l` | Change a setting; Right also enters a category. Form and action rows need Enter or Space. |
| Menu | Enter or Space | Enter a category, advance a setting, or activate an action. |
| Menu | Home/End, PageUp/PageDown | Navigate longer lists. |
| Menu | A press (a click, a tap) | Select the row; on the selected row act as Enter, or as Left on a setting's left half. |
| Form | Up/Down | Move between fields; the focused field's help appears below. |
| Form | Left/Right, Home/End, Backspace/Delete | Edit the field. Printable characters, including `q`, enter text. |
| Form | Enter | Submit all fields. Invalid input keeps the form and its values. |
| Form | A press on a field | Focus it. |
| Anywhere | Escape | Cancel a form, return to categories, then close. `q` also goes back in menus. A press outside the window does the same. |
| Anywhere | Ctrl-G or F10 | Close the menu and discard an unsubmitted form. |

Submitting a session action closes the menu and resumes play. Joining, allocator
requests and bot admission complete asynchronously; the status bar reports
progress or failure. Cancelling a form before submission starts no action.

Solo play pauses while the menu is open. A host may pause when its only other
participants are its own bots; a guest or a session with other players keeps
running. A new arrival resumes a paused host. Game → Pause is another kind of pause,
which stands until it is turned off. Replay viewers retain their inspection-only
command restrictions. A bracketed value marks a row that is unavailable or read
only; for an unavailable row the description below the list starts with the reason,
the sentence its command gives (`Logging unavailable in a browser`, `Audio
unavailable in this run`).

## Pages

| Page | Controls |
|---|---|
| Game | Pause; a new game; a row per scenario the configuration roots hold, which starts it; another scenario by name or path. |
| Bots | Add bots by count and policy; remove a bot held by this run or cancel its admission. |
| Multiplayer | Host this run, join an existing session, request an allocated session, leave the session, relay for others, inspect status, drop a player with its bots or an individual bot (host only). |
| Audio | Music, sound effects, effects volume in 5% steps. |
| Controls | Auto-fire (`off`, `main`, `on`), mouse input, pointer following. |
| Display | Telemetry HUD and clearing its pins, navigation flow field and route graph. |
| Simulation | Solo speed from 1/8x through 8x; a replay's restart. |
| Diagnostics | Logging, level and scope presets, snapshot interval, flight recorder depth, FSM trigger and flush, profiler, CPU/heap/mutex profiles and execution trace, telemetry snapshot, the replay journal toggle. |
| Startup settings | Guidance for paths/scenario, audio device/buffer, colour depth, key bindings and identity. |

### Game

**Pause** stops the game until it is turned off, as `:pause` does; closing the menu
leaves it stopped. In a session only the host can, every participant stops at the
same tick, anyone joining lands paused, and the host turning it off resumes
everyone. **New game** restarts the running scenario from its first tick, as `:n`
does. **Wait to start** (`:wait`) holds each new game a person plays at its first
tick until a key, click or tap the held game would ignore, the host's in a session,
and a join for the press that takes its place there; a scenario's own restart never
waits. Below them, one row per scenario names what
`:n <name>` would start: `main` and every `scenario/<name>/scenario.toml` the
configuration roots hold, the running one marked `current`; a tap or Enter starts
it, so a phone needs no name to type.
**Scenario by name** takes what `-s` takes, a name or a path, for one the rows do
not list. In a session only the host can start either, and its participants rejoin.

### Bots

**Add bots** takes a count and policy graph name or path. `default` is the shipped
policy; a blank policy also uses it. Policies follow the normal config-root
resolution rules. Each bot occupies a player slot and leaves when its holder
leaves. Both hosts and guests can bring bots.

The list shows this run's bots, including joining and leaving states. Select
**Remove &lt;slot&gt;: &lt;policy&gt;** and activate it to drop that bot. The hexadecimal
slot matches its cursor label. **Cancel joining** stops a pending bot; it stays
marked as leaving until its admission unwinds. Bots held by another player are
outside this list.

Adding bots to a solo run opens a session only they can join, in a browser too. To
invite remote players, **host at the intended listening address before adding
bots**. An existing session cannot be rebound through the menu. The same
operations are `:bot add [N[:graph]|graph]` and `:bot drop <slot>`; see
[Bots](bots.md).

### Multiplayer

A read-only **Current session** row summarises the session, and every other
participant has a **Drop &lt;slot&gt;: player|bot** row that only the host can
activate. Dropping a player removes its bots; dropping a bot leaves its holder
and siblings. Commands are `:player` and `:player drop <slot>`;
`:bot drop <slot>` also lets the host remove any bot. Decimal `10`, hexadecimal
`A` and `0xA` all refer to the cursor marked `A`. A player who left reads
`player, away` while its Wraith waits; dropping it ends the wait. Slots may be
reused once a departed player is dropped.

**Host this run** takes a listening address such as `:4242`, `127.0.0.1:4242` or
`ws://:4242`. Authority may be blank to keep the startup policy, `host` to end
the session when its authority leaves, or `migrate` to allow a successor. The
status bar reports the bound address. Share an address reachable by your guests;
the menu does not change network reachability. A browser can join a session but
not host one.

**Join a session** accepts `host:port[/name]`, `vif://` and `tcp://` links or
`ws(s)://` URLs. An `http(s)://` site requests a new session with server defaults.
Joining replaces the current solo run only after admission succeeds. An active
session, a pending join, or a driven run without a restart loop disables joining.

**Request a session** exposes the existing vif-allocator request API:

| Field | Meaning |
|---|---|
| Site | Allocator HTTP(S) origin; defaults to `https://lixen.com`. |
| Players | Total player slots including you; `0` uses the server default. |
| Scenario | Server-side scenario name; blank uses its default. Local files are not uploaded. |

Submitting sends `POST /vif/api/sessions` and joins the returned link. The
server controls capacity, scenarios, quotas and availability; refusals appear
in the status bar and preserve the current run. The form cannot ask for the
session's own bots; the Bots page adds bots held by your run after joining.

**Leave the session** runs `:leave`: the run plays on in a session of its own from
where this one stands, its bots seated again beside it, and the others play on
without it. A solo run, a session holding only your own bots, a pending join, or
a driven run without the play loop disables it. See
[Multiplayer §5.5](multi-player.md#55-leaving-a-session).

**Relay for others** runs `:relay on|off` and is saved in `[play]`: on, this run
carries the game for a player who reaches it and not the host, as after a host
loss; off, the default, it carries only those that joined through it, its bots and
its `-relay` door. A browser cannot relay, and the row says so. See
[Multiplayer §5.1](multi-player.md#51-succession).

## Key binding and persistence

The `config_menu` action is bound to `ctrl_g` and `f10` in `normal_keys`,
`text_keys` and `overlay_keys` in `input/keymap.toml`. Override those sections (or
use `-k`) to add or change its keys; `:g` remains available. Forms use the
text-key bindings, while menu navigation uses overlay bindings.

Action rows (`Enter`) run the command they name and close the menu: `:n`, `:leave`,
`:t unpin`, `:t save`, `:log rec flush`, `:d cpu|heap|mutex|trace`
and `:replay restart`; a scenario row and the Scenario by name form submit
`:n <scenario>` the same way. Views (`:t`, `:d`, help), developer state commands (`:god`, `:energy` and the
like), `:step`, `:d break`, `:emit`, `:system`, `:region` and `:replay tick|time`
stay on the command line.

Use CLI arguments and `vif.toml` for startup defaults. The Startup settings page
identifies options that need a restart; it does not edit files. `[play]` is the
exception: sound effects, music, effects volume, auto-fire, mouse input, Follow
mouse, the telemetry HUD, Relay and Wait to start are written back as they change,
from the menu or the command line, and a scenario change carries them into the
next run. A flag (`-mute`, `-nw`) sets its value for its run alone and is never
written. Every platform writes them the same way: the `vif.toml` the run read when
it is the player's own, else the user root's, created at the first change; a
browser keeps them in the site's storage
([Filesystem layout §3](filesystem-layout.md#3-resolution-policy)). Only a run
with no user root, or a browser refused its storage, keeps them to itself. The
game speed, HUD pins and standing breakpoints stay this run's. Exact values
outside menu presets remain available through commands such as `:log stat N`.

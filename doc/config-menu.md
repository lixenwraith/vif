# Configuration menu

Press **Ctrl-G** or enter **`:g`** (`:config`) during play. Settings take effect
in the current run; the menu does not rewrite `vif.toml` or the keymap.

## Navigation

| Where | Keys | Action |
|---|---|---|
| Menu | Up/Down or `j/k` | Select a category, setting or action. |
| Menu | Left/Right or `h/l` | Change a setting; Right also enters a category. Session actions require activation. |
| Menu | Enter or Space | Enter a category, advance a setting, or activate an action. |
| Menu | Home/End, PageUp/PageDown | Navigate longer lists. |
| Form | Up/Down | Move between fields; the focused field's help appears below. |
| Form | Left/Right, Home/End, Backspace/Delete | Edit the field. Printable characters, including `q`, enter text. |
| Form | Enter | Submit all fields. Invalid input keeps the form and its values. |
| Anywhere | Escape | Cancel a form, return to categories, then close. `q` also goes back in menus. |
| Anywhere | Ctrl-G | Close the menu and discard an unsubmitted form. |

Submitting a session action closes the menu and resumes play. Joining, allocator
requests and bot admission complete asynchronously; the status bar reports
progress or failure. Cancelling a form before submission starts no action.

Solo play pauses while the menu is open. A host may pause when its only other
participants are its own bots; a guest or a session with other players keeps
running. A new arrival resumes a paused host. Replay viewers retain their
inspection-only command restrictions. Bracketed rows are unavailable or read
only; their description explains the reason.

## Pages

| Page | Controls |
|---|---|
| Bots | Add bots by count and policy; remove a bot held by this run or cancel its admission. |
| Multiplayer | Host this run, join an existing session, request an allocated session, inspect status, drop a player with its bots or an individual bot (host only). |
| Audio | Music, sound effects, effects volume in 5% steps. |
| Controls | Auto-fire (`off`, `main`, `on`), mouse input, pointer following. |
| Display | Telemetry HUD and clearing its pins, navigation flow field and route graph. |
| Simulation | Solo speed from 1/8x through 8x; a new game, or another scenario by name; a replay's restart. |
| Diagnostics | Logging, level and scope presets, snapshot interval, flight recorder depth, FSM trigger and flush, profiler, CPU/heap/mutex profiles and execution trace, telemetry snapshot, starting a replay journal. |
| Startup settings | Guidance for paths/scenario, audio device/buffer, colour depth, key bindings and identity. |

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

Adding bots to a solo run opens a loopback session. To invite remote players,
**host at the intended listening address before adding bots**. An existing
session cannot be rebound through the menu. The same operations are available
as `:bot add [N[:graph]|graph]` and `:bot drop <slot>`.
See [Bots](todo-bots.md) for current development and lifecycle details.

### Multiplayer

Hosts also see **Drop <slot>: player/bot** rows. Dropping a player removes its
bots; dropping a bot leaves its holder and siblings. Guests cannot remove other
participants. Commands are `:player` and `:player drop <slot>`; `:bot drop <slot>`
also lets the host remove any bot. Decimal `10`, hexadecimal `A` and `0xA` all
refer to the cursor marked `A`. Slots may be reused after departure.

**Host this run** takes a listening address such as `:4242`, `127.0.0.1:4242` or
`ws://:4242`. Authority may be blank to keep the startup policy, `host` to end
the session when its authority leaves, or `migrate` to allow a successor. The
status bar reports the bound address. Share an address reachable by your guests;
the menu does not change network reachability. Browser builds cannot host.

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
in the status bar and preserve the current run. Allocator-side bot provisioning
is still planned: this form requests a session, and the Bots page adds bots
held by your local run after joining.

## Key binding and persistence

The `config_menu` action is bound to `ctrl_g` in `normal_keys`, `text_keys` and
`overlay_keys` in `input/keymap.toml`. Override those sections (or use `-k`) to
change its shortcut; `:g` remains available. Forms use the text-key bindings,
while menu navigation uses overlay bindings.

Action rows (`Enter`) run the command they name and close the menu: `:n`,
`:n <scenario>`, `:t unpin`, `:t save`, `:log rec flush`, `:d cpu|heap|mutex|trace`,
`:journal start` and `:replay restart`. Views (`:t`, `:d`, help), developer state
commands (`:god`, `:energy` and the like), `:step`, `:emit`, `:system`, `:region`
and `:replay tick|time` stay on the command line.

Use CLI arguments and `vif.toml` for startup defaults. The Startup settings page
identifies options that need a restart; it does not edit files. Exact values
outside menu presets remain available through commands such as `:log stat N`.

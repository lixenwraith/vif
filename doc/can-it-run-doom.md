# Can it run Doom?

Not today. Yes, as a program of its own drawn through vif's hosts, for about two
thousand lines to first play, most of them outside this repository; Doom as a vif
scenario inside a session is out of scope. A design for future implementation:
nothing here is built.

## 1. Three readings

| Reading | Verdict |
|---|---|
| A Doom engine drawn and heard through vif's hosts: terminal, desktop window, browser, Android | Doable; §2 to §5 |
| Doom as a scenario inside a vif session, under the ECS, journal, capture and correction | Out of scope (§6) |
| A Doom map (a WAD's linedefs) imported as vif walls | Small, and only after the WAD reader exists (§7) |

## 2. The engine

cgo is off on every target, wasm and Android included, so the engine must be pure
Go: doomgeneric and every C port are out. The candidate is
[gore](https://github.com/AndreRenaud/gore), doomgeneric converted with
`modernc.org/ccgo/v4` and cleaned by hand, standard library only. Its frontend
contract is small: `DrawFrame(*image.RGBA)` once a tic, `GetEvent` for keys and
the mouse, and `CacheSound`/`PlaySound` with 8-bit mono 11025 Hz effects. It has
no music, keeps global state (one game per process) and still uses `unsafe` in
places.

gore is GPL-2.0, as Doom's source is, and vif is BSD-3-Clause. A binary that links
gore is GPL, so the game lives in a module of its own, best a repository of its
own (`vif-doom`), that imports vif's `pkg/` and the terminal module. vif's
binaries, nightly archives and APK never link it. `pkg/` never importing
`internal/` is what makes the hosts reusable from there: `pkg/window` and
`pkg/browser` already expose `Events() <-chan terminal.Event`, `Size` and
`Flush(cells)`. The one internal thing it needs, the font atlas
(`asset.DefaultFont`), is a file it copies. A clean-room BSD engine is the
alternative and is tens of thousands of lines: out of scope.

## 3. Blockers

| Blocker | Where it is today | What closes it | Size |
|---|---|---|---|
| No key release | A terminal reports none ([Input and modes](input-and-modes.md) §1) and `terminal.Event` has no field for one; `pkg/window` handles only `KeyPress`, and the page and Android post keys as a terminal would | The kitty keyboard protocol in the terminal module and a key action on `Event`; each host posts its release (§3.1) | ~500 lines |
| Cells, not pixels | `render.Surface` and every host draw a cell grid ([Multi-platform §8](multi-platform.md#8-adding-another-renderer-or-android-host)) | A half-block frame converter in `pkg/ascimage` (§3.2); a pixel path for graphical hosts later | ~150; pixel path ~400 |
| Effects | The mixer is synthesized and 44.1 kHz stereo | Resample each 8-bit lump to float once, at `CacheSound`, and play it through `AudioEngine.PlayBuffer`; centered and unstoppable, since that call has no pan and no voice handle | ~100 |
| Music | Doom's is MUS played on OPL2 FM from the `GENMIDI` bank | An OPL2 emulator in Go, or MUS channels mapped onto the sequencer's voices; deferred, as gore defers it | ~1,000–1,500 |
| Game data | No IWAD can ship in `wad/` | The player supplies one (§3.3) | ~80 |

Doom's 35 Hz tic needs nothing from vif: `vif-doom` runs gore's own loop and never
`app.Run`, so the 50 ms scheduler, pause and the `Away` contract do not apply.

### 3.1 Key release

Doom moves while a key is held (`ev_keydown`/`ev_keyup`); gore's own terminal
frontend makes the player tap repeatedly. The terminal module pushes the kitty
protocol's progressive enhancement with event types and every key as an escape
(flags 1, 2 and 8: `CSI > 11 u` on start, `CSI < u` on exit and suspend), so a
letter's press and release both arrive as `CSI code ; mods:event u`, where 1 is
press, 2 repeat and 3 release, and parses that into a `KeyAction` on `Event` beside
`MouseAction`. kitty, foot, Ghostty, Alacritty and WezTerm (behind its
`enable_kitty_keyboard` setting) report it; xterm does not, and a multiplexer has
to pass it through. Without it the fallback holds a key
until no repeat arrives within the repeat interval, which stutters for the first
repeat's delay (250–600 ms): a fallback, not play. The window posts X11
`KeyRelease`, the page `keyup` and Android the key's `ACTION_UP`, a few lines each.

The same foundation is what vif's input hook lacks for held keys
([todo](todo.md#widen-the-input-hook-past-order-only-play)), so step 1 of §5
serves vif as well.

### 3.2 Frames as cells

`▀` with the top pixel as foreground and the bottom as background gives two pixels
a cell vertically and one horizontally, square at the reference aspect of 0.5. The
whole 320x200 frame wants a 320x100 grid; a 200x60 terminal shows it area-averaged
at 200x120. Quadrant cells, the other `pkg/ascimage` mode, double the horizontal
detail but hold two colors a cell, which fringes on Doom's textures. The atlas has
`▀`, `▄` and the quadrants, so the window, the page and Android draw the same cells
unchanged. `ascimage.ConvertImage` allocates its result each call; a frame loop
needs a variant that fills a caller's `[]terminal.Cell`.

A truecolor cell costs about 40 bytes, so a fully changed 200x60 frame at 35 fps is
about 17 MB/s. A local emulator takes that, an SSH link does not. The terminal
writes on its own goroutine holding only the newest frame, so a slow terminal drops
frames rather than stalling the game. Drawing every second tic halves it; the
xterm-256 palette (`38;5;n`, about 23 bytes a cell) saves two fifths more, banding
Doom's palette onto the nearest of xterm's.

In the window at `-scale 1`, a 12x24 cell makes a 1920x1080 screen 160x45 cells,
160x90 Doom pixels. That is where a pixel path pays: an optional interface on
`pkg/window`, `pkg/browser` and the Android view that takes the RGBA frame as the
texture it already uploads. It is the one change here to the hosts' contract, so
it waits until the cell version plays.

### 3.3 Game data

id's shareware `DOOM1.WAD` may be passed on only unmodified under id's terms, which
a BSD repository should not carry. Freedoom is BSD-licensed but tens of megabytes,
past what the binary embeds, the APK should carry or a browser fetches as a
scenario (1 MiB). So the player supplies the IWAD: `-iwad`, else `DOOMWADDIR` and
`DOOMWADPATH` as source ports read them, else `doom/` in the user root. In the
browser the page takes a file the player picks and keeps it in origin-private
storage.

## 4. Platforms

| Host | First cut | Missing for good play |
|---|---|---|
| Terminal | Plays wherever vif's does | Held keys only on kitty-protocol terminals |
| Desktop window | Plays as cells; Linux and FreeBSD | `KeyRelease`; the pixel path for resolution |
| Browser | gore in the Worker under `js/wasm`, with its own copy of `web/` and the IWAD picker | A measurement: Go wasm speed for Doom's renderer is unmeasured |
| Android | A `gomobile` module of its own, as `android/` is | Touch controls: the strip sends keys, not a stick |
| Windows | Terminal only, without sound, as vif's zip | What vif's zip lacks |

## 5. Plan

Each step is one PR; the first three are BSD changes in vif and the terminal module,
useful without Doom.

1. Terminal module: kitty event types and `Event.KeyAction`, restored on exit and
   suspend. ~500 lines with tests.
2. Hosts: window `KeyRelease`, page `keyup`, Android key up. ~100 lines.
3. `pkg/ascimage`: a half-block converter into a caller's cells. ~150 lines.
4. `vif-doom` (GPL-2.0): a gore frontend per host (frame to cells to `Flush`, host
   events to Doom keys, effects to `PlayBuffer`), IWAD resolution, the hold
   fallback, and a keymap with vi keys beside Doom's. ~800–1,200 lines.

That is about 1,500 to 2,000 lines to first play in a terminal, window and browser.
Music, the pixel path and Android touch controls follow, about 2,000 more.

## 6. Why not inside a session

- gore keeps one game per process, and a vif process holds more than one `App`: a
  `:join` or scenario change constructs a new one, and a replay's takeover copy runs
  beside the original.
- Doom tics at 35 Hz in 16.16 fixed point; vif ticks at 50 ms. A session's capture,
  correction and journal would have to carry Doom's zone memory to join, repair or
  replay it.
- Doom has its own lockstep netgame of tic commands. Running it under vif's
  convergence is two mechanisms for one problem. A multiplayer `vif-doom` would send
  Doom's packets over vif's transport (`pkg/websocket`, the allocator's route), not
  through vif's simulation.

## 7. Doom maps as vif walls

A WAD reader written from the format's documentation is not derived from Doom's
code, so it can be BSD. With it, rasterizing a map's one-sided linedefs onto the
cell grid at the map's aspect writes the dual-mode wall pattern `internal/pattern`
already spawns ([Content §8](content-assets-and-tools.md)), and things could seed
spawns later. Doors, heights and sectors have no meaning on a flat grid. It belongs
with the editor's imports ([todo](todo.md#build-the-map-and-scenario-editor)), about
300 lines; the maps come from the player's IWAD or Freedoom's, never the repository.

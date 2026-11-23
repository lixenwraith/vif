<!-- The README of each release archive. make release-readme renders it for one
     RELEASE_TARGET: a block from "if <tags>" to "end" stays when one of its tags
     is the target's, and @LABEL@, @TARGET@ and @PLATFORM@ are filled in. -->
# vif @LABEL@ for @PLATFORM@

vif is a real-time roguelike typing shooter played with vi-style motions, solo or
with other players over the network. Its page is https://lixen.com/vif/, and its
source and documentation are at https://github.com/lixenwraith/vif.

<!-- if nightly -->
This is a nightly build: a development build of one commit, replaced every night,
whose flags, files and formats may change without notice.

<!-- end -->
<!-- if client headless web -->
## In this archive

<!-- end -->
<!-- if steam zip -->
## In this folder

<!-- end -->
<!-- if client -->
- `vif`: the game, in a terminal or, with `-g`, in a desktop window
<!-- end -->
<!-- if steamlinux -->
- `bin/vif`: the game, in a window of its own
<!-- end -->
<!-- if winfolder -->
- `bin\vif.exe`: the game, in a window of its own
<!-- end -->
<!-- if steam zip -->
- `wad/`: the scenarios, the typing corpus, wall images and the shipped keymaps,
  bot graphs and `vif.toml`, which the game reads from beside its `bin/`
<!-- end -->
<!-- if headless -->
- `vif`: the dedicated game server, a static binary with no terminal, window or
  sound
<!-- end -->
<!-- if web -->
- `index.html`, with its scripts, stylesheet and `fonts/`, the font of its text
  with that font's license: the page that runs the game
- `vif.wasm`: the game, which the page runs in a Worker through `wasm_exec.js`
<!-- end -->
- `LICENSE`: vif's license, BSD-3-Clause
- `NOTICES`: the licenses of Go and of the modules this build links
<!-- if client steam zip -->
- `LICENSE.font`: the license of Hack, the font the desktop window draws its cells
  in: MIT, with Bitstream Vera's
<!-- end -->
<!-- if web -->
- `LICENSE.font`: the license of Hack, the font the page draws its cells in, which
  `vif.wasm` carries: MIT, with Bitstream Vera's
<!-- end -->

<!-- if client web steam zip -->
## Play

<!-- end -->
<!-- if steam -->
Steam starts the game full screen, in a window of its own. What you type under
Properties, Launch Options is added to its command line: `-fullscreen=false` opens
a plain window, `-scale 2` doubles its font for a HiDPI screen, and `-s td` starts
the tower defense map. Ctrl with `=` or `-`, or with the wheel, zooms, and Ctrl-0
puts its font back.

`F1` (or `:help`) lists the keys, `Ctrl-G` or `F10` (or `:g`) opens the settings
menu, and `:q` quits.

<!-- end -->
<!-- if steamlinux -->
Sound goes to PulseAudio, or to PipeWire through its pulse server, whichever the
system runs; `-ab <name>` in Launch Options names a player instead.

<!-- end -->
<!-- if zip -->
Unzip the archive where you like and open `bin\vif.exe`: the game plays in a
window of its own, with sound, and reads its scenarios from the `wad\` beside its
`bin\`, so keep the folder whole. Windows may ask before it runs a program it has
not seen: this one is not signed. It has no console: started from one it prints
there, and a mistake on its command line is shown in a box. What a shortcut's
Target adds is its command line: `-fullscreen` opens it full screen, `-scale 2`
doubles its font, and `-s td` starts the tower defense map. F11 or Alt+Enter
toggles full screen, Ctrl with `=` or `-`, or with the wheel, zooms, and Ctrl-0
puts its font back.

`F1` (or `:help`) lists the keys, `Ctrl-G` or `F10` (or `:g`) opens the settings
menu, and `:q` quits. This build keeps no log and no replay journal.

<!-- end -->
<!-- if client -->
```sh
tar -xzf vif-@LABEL@-@TARGET@.tar.gz
cd vif-@LABEL@-@TARGET@
./vif
```

To keep it, copy `vif` onto your `PATH`, as in `install -m 0755 vif ~/.local/bin/`.

<!-- end -->
<!-- if linux -->
It needs glibc, since it links libc to load the desktop window's libraries: on
musl (Alpine) or under Termux, build the terminal alone from the source with
`make cli`.

<!-- end -->
<!-- if client -->
`./vif -g` opens a desktop window instead of playing in the terminal, on X11 or on
Wayland through XWayland. It loads `libX11.so.6`, `libEGL.so.1` and
`libGLESv2.so.2` when it opens, or desktop OpenGL (`libOpenGL.so.0`, else
`libGL.so.1`) where GLES 2 is not installed, and `-scale 2` doubles its font for
a HiDPI screen. Ctrl with `=` or `-`, or with the wheel, zooms it, and Ctrl-0
puts its font back.

<!-- end -->
<!-- if web -->
The page has to come from a web server; opened from disk, it cannot start the
game. Any static server will do:

```sh
cd vif-@LABEL@-web
python3 -m http.server 8080
```

Then open http://localhost:8080/. The page opens on a start screen, where a click
or a key starts the game and `?` lists the mouse and keyboard controls; the game
plays with the scenario and typing corpus it carries.

<!-- end -->
<!-- if client -->
`F1` (or `:help`) lists the keys, `Ctrl-G` or `F10` (or `:g`) opens the settings
menu, `:q` quits, and `vif -h` prints every flag.

<!-- end -->
<!-- if web -->
`F1` (or `:help`) lists the keys, `Ctrl-G` or `F10` (or `:g`) opens the settings
menu, and `:q` quits.

<!-- end -->
<!-- if linux -->
Sound is on: `Ctrl-S` cycles through the channels, and `-mute` starts muted. It
plays through the first of `pacat` (PulseAudio or PipeWire),
`pw-cat`, `aplay`, SoX's `play` or `ffplay` on `PATH`; with none, the game is
silent.

<!-- end -->
<!-- if freebsd -->
Sound is on: `Ctrl-S` cycles through the channels, and `-mute` starts muted. It
plays through the first of `pacat`, `pw-cat`, `aplay`, SoX's `play` or `ffplay` on
`PATH`, or else OSS on `/dev/dsp`.

<!-- end -->
<!-- if web -->
Sound is on from the click, tap or key that starts the game, as a browser asks:
`Ctrl-S` cycles through the channels, and `?arg=-mute` starts muted. The settings
menu's choices are kept in the browser's storage for the next visit. A browser
plays sound only on a secure page, which localhost is; served to other machines,
the page needs HTTPS for sound.

<!-- end -->

<!-- if headless -->
## Run a game server

```sh
tar -xzf vif-@LABEL@-@TARGET@.tar.gz
cd vif-@LABEL@-@TARGET@
./vif -serve :7777 -size 120x40
```

Players join with `vif -join <server-address>:7777`. The server waits for the
first, then runs the game on its own and admits the rest as they come, up to
`-players`. `-size` is the screen the map fits; without it the first player's
terminal sets it. `-serve ws://:7777` serves WebSocket instead of TCP, which is
what a browser joins by. It cannot play a game itself: the client archives do.

`-probe :9090` serves `/health` and Prometheus `/metrics`, `-log-stdout` writes the
log to stdout as JSON, and `-first-join`, `-empty` and `-drain` end a server that
nobody joined or everyone left, or let a termination signal wait for the players.
Nightly builds of this binary are also the container image
`ghcr.io/lixenwraith/vif:nightly`. `./vif -h` prints every flag.

<!-- end -->
<!-- if client headless steam zip -->
## Scenarios and settings

<!-- end -->
<!-- if steam -->
`wad/` holds the `td`, `recall`, `blank`, `cave`, `armada`, `magic`, `dummy` and
`tutorial` scenarios beside the main one, the full typing corpus, wall images and
copies of the keymaps, bot graphs and `vif.toml` to read. Steam replaces it at
every update and the game never writes there, so what is yours lives elsewhere.

<!-- end -->
<!-- if zip -->
`wad\` holds the `td`, `recall`, `blank`, `cave`, `armada`, `magic`, `dummy` and
`tutorial` scenarios beside the main one, the full typing corpus, wall images and
copies of the keymaps, bot graphs and `vif.toml` to read. A later release brings
its own and the game never writes there, so what is yours lives elsewhere.

<!-- end -->
<!-- if winfolder -->
The settings menu saves its choices to `%AppData%\vif\vif.toml`, created at the
first change. A keymap of your own is `%AppData%\vif\input\keymap.toml`, a
scenario of your own a folder under `%AppData%\vif\scenario\`, and what your
bots learn is kept in `%AppData%\vif\bot\`. Recorded music (`-mw`) goes under
`%LocalAppData%\vif\music\`.

<!-- end -->
<!-- if steamlinux -->
The settings menu saves its choices to `~/.config/vif/vif.toml`, created at the
first change. A keymap of your own is `~/.config/vif/input/keymap.toml`, a scenario
of your own a directory under `~/.config/vif/scenario/`, and what your bots learn
is kept in `~/.config/vif/bot/`. Logs (`-l`), replay journals (`-j`) and recorded
music (`-mw`) go under `~/.local/state/vif/`.

<!-- end -->
<!-- if client -->
The binary carries the main scenario and a tutorial typing corpus. The `td`,
`recall`, `blank`, `cave`, `armada`, `magic`, `dummy` and `tutorial` scenarios,
the full corpus, wall images and editable copies of the keymaps, bot graphs and
`vif.toml` are the wad, `vif-@LABEL@-wad.tar.gz` in the same release. It unpacks
as the `vif/` configuration directory:

<!-- end -->
<!-- if headless -->
The binary carries the main scenario. The `td`, `recall`, `blank`, `cave`,
`armada`, `magic`, `dummy` and `tutorial` scenarios and their wall images are in
the wad, `vif-@LABEL@-wad.tar.gz` in the same release, which unpacks as the
`vif/` configuration directory:

<!-- end -->
<!-- if client headless -->
```sh
tar -xzf vif-@LABEL@-wad.tar.gz -C ~/.config          # for you
tar -xzf vif-@LABEL@-wad.tar.gz -C /usr/local/etc     # for everyone, as root
```

<!-- end -->
<!-- if client -->
Then `-s td` plays the tower defense map, and the settings menu saves its sound,
mouse and HUD choices to `~/.config/vif/vif.toml`, created at the first change
from the one installed under `/usr/local/etc`, if any, which stays as it is. Logs
(`-l`), replay journals (`-j`) and recorded music (`-mw`) go under
`~/.local/state/vif/`. A later wad unpacked the same way replaces your `vif.toml`
and keymap: add `--exclude vif/vif.toml --exclude vif/input/keymap.toml` to keep
them. A run reads the ones kept brought up to date and writes those under
`~/.config` back so; those under `/usr/local/etc` it never writes.

<!-- end -->
<!-- if headless -->
Then `-s td` serves the tower defense map, and a player who lacks the scenario is
sent it on joining. Logs (`-l`) go under `~/.local/state/vif/log/`.

<!-- end -->

<!-- if client web steam zip -->
## Play with others

<!-- end -->
<!-- if steam zip -->
The lobby at https://lixen.com/vif/lobby/ hosts games on demand and lists the
running ones, each with a link to join it by. In the game, `:join <link>` joins
one, as the settings menu's Join a session row does, and `:join https://lixen.com`
hosts a new game there and joins it.

On your own network, `:host :7777` opens your game to other players, who join it
with `:join <your-address>:7777` or, once you turn on Visible on this network,
from the menu's Nearby games. `:bot add` seats a bot beside you, and
`:spectate <link>` watches a game and takes no seat in it.

<!-- end -->
<!-- if client -->
The lobby at https://lixen.com/vif/lobby/ hosts games on demand and lists the
running ones, each with a link to join it by:

```sh
vif -join vif://lixen.com:7777/<game>
vif -join https://lixen.com            # host a new game there and join it
```

On your own network, one player hosts and the others join that address:

```sh
vif -host :7777
vif -join <host-address>:7777
```

`-bot` seats a bot beside you, and `-bot 3` three. `-spectate <address>` watches a
game and takes no seat in it.

<!-- end -->
<!-- if web -->
A browser joins a game over WebSocket only. The lobby at
https://lixen.com/vif/lobby/ opens its games in the copy of this page at
https://lixen.com/vif/wasm/. Served over plain http, this page joins a native
`vif -host ws://:7777` with `?arg=-join=ws://<host-address>:7777`; a page served
over https, that copy included, opens only `wss://`, which a native host serves
with `-host wss://:7778` and `-cert` or `-selfsign`, once the browser trusts that
certificate. Launch arguments are such `arg` query parameters, one per flag, or
`window.VIF_ARGS`, set by a script loaded before `terminal.js`.

<!-- end -->
## More

- Documentation: https://github.com/lixenwraith/vif/tree/main/doc
- Issues: https://github.com/lixenwraith/vif/issues

<!-- The README of each release archive. make release-readme renders it for one
     RELEASE_TARGET: a block from "if <tags>" to "end" stays when one of its tags
     is the target's, and @LABEL@, @TARGET@ and @PLATFORM@ are filled in. -->
# vif @LABEL@ for @PLATFORM@

vif is a real-time roguelike typing shooter played with Vim motions, solo or with
other players over the network. Its page is https://lixen.com/vif/, and its source
and documentation are at https://github.com/lixenwraith/vif.

<!-- if nightly -->
This is a nightly build: a development build of one commit, replaced every night,
whose flags, files and formats may change without notice.

<!-- end -->
## In this archive

<!-- if client -->
- `vif`: the game, in a terminal or, with `-g`, in a desktop window
- `LICENSE`: vif's licence, BSD-3-Clause
- `LICENSE.purego`: the licence of purego, which the desktop window links

<!-- end -->
<!-- if headless -->
- `vif`: the dedicated game server, a static binary with no terminal, window or
  sound
- `LICENSE`: vif's licence, BSD-3-Clause

<!-- end -->
<!-- if windows -->
- `vif.exe`: the game, in a terminal
- `LICENSE`: vif's licence, BSD-3-Clause

<!-- end -->
<!-- if web -->
- `index.html`, with its scripts and stylesheet: the page that runs the game
- `vif.wasm`: the game, which the page runs in a Worker through `wasm_exec.js`
- `LICENSE`: vif's licence, BSD-3-Clause

<!-- end -->
<!-- if client windows web -->
## Play

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
`libGLESv2.so.2` when it opens, and `-scale 2` doubles its font for a HiDPI
screen.

<!-- end -->
<!-- if windows -->
Unzip the archive, open a terminal such as Windows Terminal or Alacritty in its
folder, and run `.\vif.exe`, which the commands below call `vif`.

The Windows build is experimental. Played in Alacritty it runs, with three known
issues: the mouse does nothing, there is no sound because this build leaves audio
out, and some status bar cells flicker as the screen redraws. It has no logging
and no replay journal either.

<!-- end -->
<!-- if web -->
The page has to come from a web server; opened from disk, it cannot start the
game. Any static server will do:

```sh
cd vif-@LABEL@-web
python3 -m http.server 8080
```

Then open http://localhost:8080/. The game plays with the scenario and typing
corpus it carries.

<!-- end -->
<!-- if client windows -->
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
Sound is on once the page is first touched or typed in, as a browser asks:
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
<!-- if client windows headless -->
## Scenarios and settings

<!-- end -->
<!-- if client windows -->
The binary carries the main scenario and a tutorial typing corpus. The `td`,
`kraken`, `simon` and `blank` scenarios, the full corpus, wall images and editable
copies of the keymap, bot graphs and `vif.toml` are the wad,
`vif-@LABEL@-wad.tar.gz` in the same release. It unpacks as the `vif/`
configuration directory:

<!-- end -->
<!-- if headless -->
The binary carries the main scenario. The `td`, `kraken`, `simon` and `blank`
scenarios and their wall images are in the wad, `vif-@LABEL@-wad.tar.gz` in the
same release, which unpacks as the `vif/` configuration directory:

<!-- end -->
<!-- if client headless -->
```sh
tar -xzf vif-@LABEL@-wad.tar.gz -C ~/.config          # for you
tar -xzf vif-@LABEL@-wad.tar.gz -C /usr/local/etc     # for everyone, as root
```

<!-- end -->
<!-- if windows -->
```powershell
tar -xzf vif-@LABEL@-wad.tar.gz -C $env:APPDATA
```

<!-- end -->
<!-- if client -->
Then `-s td` plays the tower defense map, and the settings menu saves its sound,
mouse and HUD choices to `~/.config/vif/vif.toml`, created at the first change
from the one installed under `/usr/local/etc`, if any, which stays as it is. Logs
(`-l`), replay journals (`-j`) and recorded music (`-mw`) go under
`~/.local/state/vif/`.

<!-- end -->
<!-- if windows -->
Then `-s td` plays the tower defense map, and the settings menu saves its choices
to `%AppData%\vif\vif.toml`, created at the first change.

<!-- end -->
<!-- if headless -->
Then `-s td` serves the tower defense map, and a player who lacks the scenario is
sent it on joining. Logs (`-l`) go under `~/.local/state/vif/log/`.

<!-- end -->

<!-- if client windows web -->
## Play with others

<!-- end -->
<!-- if client windows -->
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

`-bot` seats a bot beside you, and `-bot 3` three.

<!-- end -->
<!-- if web -->
A browser joins a game over WebSocket only. The lobby at
https://lixen.com/vif/lobby/ opens its games in the copy of this page at
https://lixen.com/vif/wasm/. Served over plain http, this page joins a native
`vif -host ws://:7777` with `?arg=-join=ws://<host-address>:7777`; a page served
over https, that copy included, opens only `wss://`, which a native host does not
serve yet. Launch arguments are such `arg` query parameters, one per flag, or
`window.VIF_ARGS`, set by a script loaded before `terminal.js`.

<!-- end -->
## More

- Documentation: https://github.com/lixenwraith/vif/tree/main/doc
- Issues: https://github.com/lixenwraith/vif/issues

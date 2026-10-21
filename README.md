<p align="center">
  <img src="lixen-logo.svg" alt="vif Logo" width="160"/>
  <br>
  <br>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.27.2-00ADD8?style=flat&logo=go" alt="Go"></a>
  <a href="https://opensource.org/licenses/BSD-3-Clause"><img src="https://img.shields.io/badge/License-BSD_3--Clause-blue.svg" alt="License"></a>
  <a href="doc/kubernetes-fleet.md"><img src="https://img.shields.io/badge/k8s%20|%20k3s-1.29+-326ce5?style=flat&logo=kubernetes" alt="Kubernetes"></a>
  <img src="https://img.shields.io/badge/Platform-Linux%20|%20FreeBSD%20|%20Android%20|%20WASM-lightgrey" alt="Platforms">
  <a href="doc/"><img src="https://img.shields.io/badge/Docs-Available-green.svg" alt="Documentation"></a>
</p>

# vif

vif is a real-time roguelike typing shooter played with Vim motions, solo or with
others over the network: in a terminal, a desktop window, on Android or in the
browser.

It is pure Go with cgo off and few dependencies. The terminal, TOML, color and
logging primitives are modules of their own ([terminal](https://github.com/lixenwraith/terminal),
[toml](https://github.com/lixenwraith/toml), [color](https://github.com/lixenwraith/color),
[log](https://github.com/lixenwraith/log)); vif owns the ECS, gameplay, input,
compositor, scenarios, renderers, audio and simulation libraries.

## Highlights

- Vim's Normal, Insert, Visual, Search and Command modes as the controls: counts,
  motions, operators, search repeat, undo and recorded macros.
- A typed sparse-set ECS on a fixed-step scheduler with bounded event settling,
  driven by TOML-authored hierarchical state machines for encounters and bots.
- A layered terminal-cell compositor: truecolor, a 256-color tier settled on a
  text console's sixteen colors, blend modes, masks and post-processing.
- Pure-Go synthesis and a sequencer whose tempo and arrangement follow the
  player's APM, streamed to common host audio tools, FreeBSD OSS, the browser,
  Android or a WAV file.
- Enemies that adapt: aspect-aware flow fields, route graphs a bandit learns, and
  streaming genetic optimization.
- Bots that play as participants, through the same input router as a person.
- Multiplayer over TCP or WebSocket: a deterministic shared simulation with
  owner-authored cursors, selective authoritative correction, joins at any tick
  and authority that can move on when its host leaves.
- Dedicated game servers on K3s, one container per game, created on request.
- Replay journals, tick scripts, structured logs, status snapshots and a flight
  recorder for reproducing and diagnosing a run.

Headless and replay runs are deterministic for one build from their seed,
configuration and input; live play is not promised bit-for-bit, since it uses
`float64` and live scheduling.

## Play

- In the browser: [lixen.com/vif/wasm](https://lixen.com/vif/wasm/).
- With others: the [lobby](https://lixen.com/vif/lobby/) hosts games on demand and
  gives a link to join from a terminal or the browser.
- Downloads: the [nightly build](https://github.com/lixenwraith/vif/releases/tag/nightly)
  has Linux and FreeBSD clients, the game server, an Android APK, the browser
  bundle and an experimental Windows zip, each archive with a README for its
  platform. It is a development build; no final release exists yet.

## Build and run

```bash
git clone https://github.com/lixenwraith/vif --depth 1
cd vif
make release
./bin/vif
```

`make release` builds the terminal game with the desktop window `-g` opens on
Linux and FreeBSD; where the window cannot build (Termux) it says so and names
`make cli`, the terminal alone. `make release install` builds and installs:

| | `sudo make install` | `make install` as a user | Linux package |
|---|---|---|---|
| Binary | `/usr/local/bin/vif` | `~/.local/bin/vif` | `/usr/bin/vif` |
| Wad | `/usr/local/etc/vif/` | `~/.config/vif/` | `/etc/vif/` |
| Manual, completion, docs | `/usr/local/share/` | `~/.local/share/` | `/usr/share/` |
| Logs, journals, music (`-l`, `-j`, `-mw`) | `~/.local/state/vif/` | `~/.local/state/vif/` | `~/.local/state/vif/` |

The wad is the scenarios, typing corpus, wall images, bot graphs, keymap and
`vif.toml` the binary does not need to play its embedded main scenario. A file in
`~/.config/vif` overrides the system copy of it. `make uninstall` keeps the wad,
and `make install-config` installs only the wad, into `~/.config/vif`.
Uninstalled, `./bin/vif` plays the checkout's `wad/`. The
[filesystem layout](doc/filesystem-layout.md) has the precedence, and
[packaging](doc/packaging.md) the distribution checklists.

## Playing

`F1` (or `:help`) lists the keys, `Ctrl-G` or `F10` (or `:g`) opens the
[configuration menu](doc/config-menu.md), and `:q` quits. Sound is on: `Ctrl-S`
cycles through the channels, and `-mute` starts muted. The menu's sound, controls
and HUD settings are written back for the next run, to your own `vif.toml` (the
user root's, created at the first change when the run read none of yours); the
rest last the run.

`vif -h` prints every flag, to stdout; `man vif` is the same list after an
install. The ones most runs want:

- `-s <name|path>` picks a scenario (`td`, `kraken`, `simon`, `blank`), and `-d`
  plays the embedded one.
- `-bot [N[:graph]]` seats bots beside you; `-watch` lets one play in your place
  ([Bots](doc/bots.md)).
- `-g` plays in the desktop window, and `-cli` in the terminal whatever
  `vif.toml` says.
- `-j` records a replay journal and `-r <file>` replays one.
- `-check` validates the installed configuration without opening the game.

## Multiplayer

```bash
./bin/vif -host :7777                      # play, and let others join
./bin/vif -join 127.0.0.1:7777             # from a second terminal
./bin/vif -serve :7777 -size 120x40        # a game server nobody plays at
./bin/vif -join vif://lixen.com:7777/<game>
./bin/vif -join https://lixen.com          # host a new game there and join it
```

A host plays from its first tick and admits players while it runs, sending each a
snapshot of the world at whatever tick the game has reached; a player who
dropped comes back the same way. `-host ws://:7777` serves WebSocket instead of
TCP, which is how a browser joins. Sessions are trusted-peer and plaintext: a
dial is rate-limited and bounded in what it can allocate, but nobody is
authenticated, so a host reachable from an untrusted network needs that in front
of it. [Multiplayer](doc/multi-player.md) has the design.

`deploy/` runs the lobby's game servers: a `scratch` image of the static
`vif_headless` binary, K3s objects for one Job per game, and `vif-allocator`,
which creates games on request and routes each by name.
[doc/kube-docker-deploy.md](doc/kube-docker-deploy.md) installs it and
[doc/kubernetes-fleet.md](doc/kubernetes-fleet.md) is its design.

## Documentation

Start with the [documentation index](doc/README.md): architecture, package and
runtime maps, and references for gameplay, ECS and events, input, state
machines, rendering, audio, navigation and evolution, content, networking,
multiplayer, deployment and development. `cmd/soundlab` and `cmd/ascimage`
author sounds and wall images, and [script/](script/README.md) holds the
end-to-end checks Go tests cannot make.

## License

BSD-3-Clause.

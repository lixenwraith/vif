# Agent Notes

What agents working on ascimage learned that no rule in [AGENTS.md](../AGENTS.md)
and nothing in the [README](../README.md) or [format.md](format.md) states: facts
that cost a session an hour to rediscover, and how to check them. Read it before
starting. When a task teaches you one, add it in the same PR, one bullet under its
topic; delete a note that proves wrong or that a document now states.

## Toolchain

- The `gofmt` on a container's `PATH` can be older than the module's Go; run
  `"$(go env GOROOT)/bin/gofmt" -l`.
- The terminal library is `github.com/lixenwraith/terminal`, with `tui` and
  `inline`: a change to it lands there, under its own rules, and ascimage takes
  its tag. vif reads `.aimg` through this module and takes ascimage's tag.

## Looking at the TUI without a device

- Run the command in a tmux pane, `COLORTERM=truecolor TERM=xterm-256color` for
  true color, `TERM=linux COLORTERM=` for a console's 16, and drive it with
  `tmux send-keys`; `tmux resize-window -x W -y H` walks the layouts (64 columns
  side by side, a tall narrow pane stacked, under 20x4 too small).
- `tmux capture-pane -p` reads the screen as text, and the status line alone says
  the zoom, the grid and where the view is; `-e` keeps the SGR colors. A
  converter from the capture to HTML spans must carry the SGR state from one line
  into the next, then Playwright shoots it.
- The mouse is SGR text a pane reads as input: `tmux send-keys -l
  $'\e[<65;10;5M'` is a wheel step down at column 10, row 5 (one-based), `0` and
  a lowercase `m` a left press and release, `32` a drag, 16 added for Ctrl.
- Smoothness is read, not guessed: send a run of keys with `send-keys -l` and
  time until the status line shows where they end, as a pan at 400% of a
  1600x1000 JPEG drew 80 steps in 249 ms on a 160x48 pane.

## The TUI

- The app posts itself two events, a decode done and a tick, both
  `KeyNone` with a rune telling them apart, which no terminal sends. Ticks run
  only while a decode or a toast shows, so an idle app takes no wakeups; with at
  most two posted at once, `PostEvent`'s 16-slot channel never drops one.
- The loader's goroutine sees only its two channels; every source it decodes
  belongs to the app's goroutine once received, which alone mutates it (an
  `.aimg`'s converted cells). A test of the loader runs under `-race` once.

## Measuring

- The compression figures in `format.md` come from 54 shapes over nine images:
  Go's `image/testdata/video-001.jpeg` and `image/png/testdata/benchRGB.png` from
  the toolchain, LibreOffice's gallery `personas/sand` preview and header, its
  `bigapple.gif` and `intro-highres.png`, a 512x512 icon with alpha, and two
  generated ones; a container without them measures on what it has and says so.

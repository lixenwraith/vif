# Terminal Package — Design Document

## Overview

Direct ANSI terminal control library replacing `gdamore/tcell/v2`. Provides true color rendering, double-buffered output with cell-level diffing, and raw stdin input parsing. Bypasses terminfo/termcap entirely.

**Target Environment:** Unix systems (Linux, macOS, FreeBSD) with xterm-compatible terminals.

---

## Architecture
```
┌─────────────────────────────────────────────────────────────────┐
│                         Terminal                                │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Public Interface                      │   │
│  │  Init() Fini() Size() Flush() Clear() PollEvent() ...    │   │
│  └──────────────────────────────────────────────────────────┘   │
│         │                    │                    │             │
│         ▼                    ▼                    ▼             │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐        │
│  │ outputBuffer│     │ inputReader │     │resizeHandler│        │
│  │             │     │             │     │             │        │
│  │ Double-buf  │     │ Raw stdin   │     │ SIGWINCH    │        │
│  │ Diffing     │     │ ESC parser  │     │ ioctl       │        │
│  │ ANSI emit   │     │ Key mapping │     │             │        │
│  └──────┬──────┘     └──────┬──────┘     └──────┬──────┘        │
│         │                   │                   │               │
│         ▼                   ▼                   ▼               │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    OS Layer                             │    │
│  │  stdout (bufio.Writer)  │  stdin (unix.Poll/Read)  │    │    │
│  │                         │  unix.TIOCGWINSZ         │    │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | File | Role |
|-----------|------|------|
| `termImpl` | terminal.go | Lifecycle, state coordination, public API |
| `outputBuffer` | output.go | Double buffering, diff computation, ANSI emission |
| `inputReader` | input.go | Non-blocking stdin, escape sequence parsing |
| `resizeHandler` | resize_unix.go | SIGWINCH signal handling, dimension queries |
| ANSI helpers | ansi.go | Zero-allocation sequence builders |
| Color logic | color.go | Detection, RGB→256 LUT |

---

## Data Structures

### Cell
```go
type Cell struct {
    Rune  rune
    Fg    RGB
    Bg    RGB
    Attrs Attr
}
```

Single terminal cell. `Rune == 0` treated as space. Attrs are bitmask-combinable.

### RGB
```go
type RGB struct { R, G, B uint8 }
```

24-bit color. `Equal()` method for fast comparison. Zero value is black.

### Attr
```go
type Attr uint8
const (
    AttrNone      Attr = 0
    AttrBold      Attr = 1 << 0
    AttrDim       Attr = 1 << 1
    AttrItalic    Attr = 1 << 2
    AttrUnderline Attr = 1 << 3
    AttrBlink     Attr = 1 << 4
    AttrReverse   Attr = 1 << 5
)
```

Bitmask for text attributes. OR-able for combinations.

### Event
```go
type Event struct {
    Type      EventType  // EventKey, EventResize, EventError, EventClosed
    Key       Key        // Parsed key (KeyRune, KeyEscape, KeyUp, etc.)
    Rune      rune       // Valid when Key == KeyRune
    Modifiers Modifier   // ModShift, ModAlt, ModCtrl
    Width     int        // Valid when Type == EventResize
    Height    int
    Err       error      // Valid when Type == EventError
}
```

Unified input event. Designed for expansion (mouse, paste).

---

## Output Pipeline

### Double Buffering
```
Application         outputBuffer              Terminal
    │                    │                        │
    │  Flush(cells)      │                        │
    │───────────────────>│                        │
    │                    │                        │
    │              ┌─────┴─────┐                  │
    │              │ Compare   │                  │
    │              │ front vs  │                  │
    │              │ cells     │                  │
    │              └─────┬─────┘                  │
    │                    │                        │
    │              ┌─────┴─────┐                  │
    │              │ Emit ANSI │                  │
    │              │ for diffs │                  │
    │              └─────┬─────┘                  │
    │                    │                        │
    │                    │  Write to bufio.Writer │
    │                    │───────────────────────>│
    │                    │                        │
    │              ┌─────┴─────┐                  │
    │              │ Copy cells│                  │
    │              │ to front  │                  │
    │              └───────────┘                  │
```

**Front buffer:** What's currently displayed. Updated after successful write.

**Back buffer:** Incoming cells from application. Compared against front.

### Diff Algorithm
```
for each cell (y, x):
    if front[y][x] == cells[y][x]:
        skip  // No change
    else:
        if cursor not at (x, y):
            emit cursor move OR spaces (whichever cheaper)
        if style changed:
            emit SGR sequences
        emit rune
        update front[y][x]
```

**Cursor Move Optimization:** For gaps < 4 cells on same row, emit spaces instead of CSI sequence (fewer bytes).

**Style Coalescing:** Only emit color/attr codes when changed from previous cell.

### ANSI Emission (Zero-Alloc)
```go
// Pre-allocated byte slices
var csiFgRGB = []byte("\x1b[38;2;")  // True color foreground prefix

// Integer writing without fmt.Sprintf
func writeInt(w *bufio.Writer, n int) {
    if n < 10 {
        w.WriteByte('0' + byte(n))
        return
    }
    // ... direct digit extraction
}
```

All sequences built by writing to `bufio.Writer` (64KB buffer). No intermediate string allocations.

### Color Mode Handling
```
Flush path:
    if ColorModeTrueColor:
        emit \x1b[38;2;R;G;Bm
    else:
        emit \x1b[38;5;{LUT[R][G][B]}m
```

Mode determined once at `Init()`. Output functions branch on mode.

---

## Input Pipeline

### State Machine
```
                    ┌──────────────┐
                    │              │
     ┌──────────────│   GROUND     │◄────────────┐
     │              │              │             │
     │              └──────┬───────┘             │
     │                     │                     │
     │    0x1b (ESC)       │    0x00-0x1a        │
     │                     │    (control)        │
     ▼                     ▼                     │
┌─────────┐          ┌───────────┐               │
│         │          │           │               │
│  ESC    │          │  Emit     │               │
│  WAIT   │          │  Ctrl+X   │───────────────┤
│         │          │           │               │
└────┬────┘          └───────────┘               │
     │                                           │
     │    '[' (CSI)                              │
     │    'O' (SS3)                              │
     │    0x20-0x7e (Alt+key)                    │
     ▼                                           │
┌─────────┐                                      │
│         │                                      │
│  Parse  │──────────────────────────────────────┘
│  Seq    │    Lookup in csiMap/ss3Map
│         │    Emit Key + Modifiers
└─────────┘
```

### Non-Blocking Read with Poll
```go
func (r *inputReader) readLoop() {
    for {
        select {
        case <-r.stopCh:
            return
        default:
        }
        
        // Poll with 100ms timeout
        ready, _ := r.pollRead(100)
        if !ready {
            continue  // Check stopCh again
        }
        
        n, _ := unix.Read(r.fd, buf)
        r.parseInput(buf[:n])
    }
}
```

**Why Poll:** `os.File.Read()` blocks indefinitely. Using `unix.Poll()` allows periodic `stopCh` checks for clean shutdown.

### Escape Sequence Timeout

Standalone ESC vs sequence start ambiguity resolved by 50ms timeout:
```
User presses ESC:        [0x1b] ... 50ms ... → emit KeyEscape
User presses Arrow Up:   [0x1b] [0x5b] [0x41] → emit KeyUp
                         └─ within 50ms ─────┘
```

### Key Mapping Tables
```go
var csiSequences = []escapeSequence{
    {"A", KeyUp, ModNone},
    {"1;5A", KeyUp, ModCtrl},  // Ctrl+Up
    // ...
}

var csiMap = buildSequenceMap(csiSequences)  // O(1) lookup
```

**Extensibility:** Add entries to `csiSequences` or `ss3Sequences` for new key support.

---

## Color Detection

### Priority Order
```
1. COLORTERM="truecolor" | "24bit"     → TrueColor
2. Terminal-specific env vars:
   - KITTY_WINDOW_ID
   - KONSOLE_VERSION  
   - ITERM_SESSION_ID
   - ALACRITTY_WINDOW_ID
   - WEZTERM_PANE                      → TrueColor
3. TERM contains "truecolor|24bit|direct" → TrueColor
4. Default                             → 256-Color
```

**Rationale:** `COLORTERM` is the modern standard. tcell's TERM-first approach causes quantization in terminals that set `TERM=screen-256color` inside tmux but support true color.

### RGB → 256 Lookup Table
```go
var rgb256LUT [256][256][256]uint8  // 16MB, computed at init
```

**Trade-off:** 16MB memory for O(1) conversion. Computed once at package init.

**Algorithm:**
1. Check if color is near-grayscale (R ≈ G ≈ B)
2. If yes, compare grayscale ramp (232-255) vs color cube distance
3. Otherwise, map to nearest 6×6×6 cube index (16-231)

---

## Resize Handling
```
                SIGWINCH
                    │
                    ▼
            ┌───────────────┐
            │ resizeHandler │
            │   sigCh       │
            └───────┬───────┘
                    │
                    ▼
            ioctl(TIOCGWINSZ)
                    │
                    ▼
            ┌───────────────┐
            │  eventCh (1)  │──► PollEvent() returns EventResize
            └───────────────┘
                    │
            (old event dropped
             if not consumed)
```

**Channel Buffer:** Size 1, drops stale events. Prevents resize event queue buildup during rapid resizing.

---

## Lifecycle Management

### Init Sequence
```
1. DetectColorMode()
2. getTerminalSize()
3. term.MakeRaw(stdin)     → Save old state
4. Create outputBuffer (64KB bufio.Writer)
5. Create inputReader
6. Create resizeHandler
7. Emit: alternate screen, hide cursor
8. Clear screen
9. Start input goroutine
10. Start resize goroutine
```

### Fini Sequence
```
1. Stop inputReader (with 100ms timeout)
2. Stop resizeHandler
3. Emit: show cursor
4. Emit: exit alternate screen
5. Emit: reset attributes
6. term.Restore(stdin, oldState)
```

**Idempotency:** `finalized` flag prevents double-cleanup.

### Panic Recovery
```go
// Application wrapper pattern
defer func() {
    if r := recover(); r != nil {
        term.Fini()
        // or: terminal.EmergencyReset(os.Stdout)
    }
}()
```

`EmergencyReset()` emits cursor show + alternate screen exit + SGR reset + RIS (full reset). Use as last resort.

---

## Performance Characteristics

### Output

| Scenario | Bytes/Frame (100×50) | Notes |
|----------|---------------------|-------|
| Static content | ~100 | Diff skips unchanged cells |
| Full random | ~100KB | Every cell different, worst case |
| Typical game | 5-20KB | Partial updates, style coalescing |

### Memory

| Allocation | Size | Frequency |
|------------|------|-----------|
| Front buffer | W×H×32 bytes | Once per resize |
| bufio.Writer | 64KB | Once at init |
| RGB→256 LUT | 16MB | Once at package init |
| Input buffer | 256 bytes | Once at init |

### CPU

| Operation | Complexity |
|-----------|------------|
| Cell comparison | O(1) |
| RGB→256 conversion | O(1) table lookup |
| Escape sequence lookup | O(1) map lookup |
| Flush (diff) | O(W×H) |

---

## Extension Points

### Adding Keys
```go
// keys.go
var csiSequences = []escapeSequence{
    // Add new entry:
    {"1;6A", KeyUp, ModShift | ModCtrl},  // Ctrl+Shift+Up
}
```

Rebuild `csiMap` happens at init via `buildSequenceMap()`.

### Adding Event Types
```go
// input.go
const (
    EventKey EventType = iota
    EventResize
    EventPaste   // ← Enable with bracketed paste mode
    EventMouse   // ← Enable with mouse tracking mode
    EventError
    EventClosed
)
```

Event struct has Width/Height fields that can be repurposed for mouse coordinates.

### Custom Color Modes
```go
// color.go - Add new mode:
const (
    ColorMode256 ColorMode = iota
    ColorModeTrueColor
    ColorMode16  // ← Basic 16-color mode
)

// ansi.go - Add emission path:
func writeFgColor(w *bufio.Writer, c RGB, mode ColorMode) {
    switch mode {
    case ColorMode16:
        // Map to SGR 30-37, 90-97
    // ...
    }
}
```

---

## Known Limitations

| Limitation | Rationale |
|------------|-----------|
| No Windows support | Requires Windows Console API, fundamentally different |
| No mouse support | Scope control; can be added via escape sequence parsing |
| No bracketed paste | Scope control; requires mode enable/disable sequences |
| No Unicode width | Caller responsibility; would require wcwidth table |
| 16MB LUT memory | Trade-off for O(1) color conversion; acceptable for game use |
| 100ms input poll | Balance between responsiveness and CPU usage |

---

## File Reference

| File | Lines | Purpose |
|------|-------|---------|
| terminal.go | ~250 | Interface, lifecycle, coordination |
| output.go | ~180 | Double buffer, diff, flush |
| input.go | ~280 | Raw read, escape parsing, events |
| keys.go | ~180 | Key constants, sequence tables |
| ansi.go | ~150 | Sequence builders, UTF-8 encoding |
| color.go | ~130 | Detection, LUT generation |
| resize_unix.go | ~80 | SIGWINCH, ioctl |
| compat.go | ~50 | tcell migration helpers |

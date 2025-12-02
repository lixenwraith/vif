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
| `outputBuffer` | output.go | Double buffering, diff computation, coalesced ANSI emission |
| `inputReader` | input.go | Non-blocking stdin, zero-alloc escape sequence parsing |
| `resizeHandler` | resize_unix.go | SIGWINCH signal handling, dimension queries |
| ANSI helpers | ansi.go | Zero-allocation sequence builders |
| Color logic | color.go | Detection, compute-based RGB→256 conversion |

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

24-bit color. Compared using direct struct equality (`c == other`). Zero value is black.

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
    AttrFg256     Attr = 1 << 6  // Fg.R is 256-color palette index
    AttrBg256     Attr = 1 << 7  // Bg.R is 256-color palette index
)

// AttrStyle masks only the style bits (excludes color mode flags)
const AttrStyle Attr = AttrBold | AttrDim | AttrItalic | AttrUnderline | AttrBlink | AttrReverse
```

Bitmask for text attributes. OR-able for combinations. `AttrFg256`/`AttrBg256` flags indicate 256-color palette mode (where Fg.R/Bg.R holds palette index).

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
    │              │ coalesced │                  │
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
    if cellEqual(front[y][x], cells[y][x]):
        skip  // No change
    else:
        if cursor not at (x, y):
            emit cursor move OR spaces (whichever cheaper)

        for each contiguous dirty cell:
            if style changed from last:
                emit coalesced SGR sequence (attrs+colors)
            emit rune
            update front cell
```

**Cursor Move Optimization:** For gaps < 4 cells on same row, emit spaces instead of CSI sequence (fewer bytes).

**Style Coalescing:** Single combined SGR sequence emits reset + attributes + foreground + background when style changes. Minimizes escape sequence overhead.

### Coalesced SGR Rendering
```go
// Single escape sequence combines all changes:
// \x1b[0;1;38;2;R;G;B;48;2;R;G;Bm
//      │ │ └─fg────┘ └─bg────┘
//      │ └─ bold
//      └─ reset

func (o *outputBuffer) writeStyleCoalesced(w *bufio.Writer, fg, bg RGB, attr Attr) {
    if attrChanged {
        // Emit: reset + style attrs + inline fg + inline bg
        w.Write(csi)
        w.WriteByte('0')  // reset
        // ... emit style attributes
        o.writeFgInline(w, fg, attr)  // ;38;2;R;G;B or ;38;5;N
        o.writeBgInline(w, bg, attr)  // ;48;2;R;G;B or ;48;5;N
        w.WriteByte('m')
    } else {
        // Minimal update for color-only changes
        // ...
    }
}
```

**Zero-Alloc:** All sequences built by writing directly to `bufio.Writer` (128KB buffer). No intermediate string allocations.

### Color Mode Handling
```
Flush path:
    if attr & AttrFg256:
        emit \x1b[38;5;{Fg.R}m          // 256-color palette index
    else if ColorModeTrueColor:
        emit \x1b[38;2;{Fg.R};{Fg.G};{Fg.B}m
    else:
        emit \x1b[38;5;{RGBTo256(Fg)}m  // Compute-based conversion
```

Mode determined once at `Init()`. Output functions branch on mode and attribute flags.

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
│  Seq    │    Zero-alloc lookup in csiMap/ss3Map
│         │    Emit Key + Modifiers
└─────────┘
```

### Fast Path for Printable ASCII
```go
func (r *inputReader) parseInput(data []byte) {
    for i < len(data) {
        b := data[i]

        // Fast path: printable ASCII (most common case)
        if b >= 0x20 && b < 0x7f {
            r.sendEvent(Event{Type: EventKey, Key: KeyRune, Rune: rune(b)})
            i++
            continue
        }

        // Handle ESC, control chars, UTF-8...
    }
}
```

**Optimization:** Single comparison eliminates most complex parsing for typical text input.

### Zero-Alloc Escape Parsing
```go
type inputReader struct {
    // ...
    escBuf [16]byte  // Embedded buffer for escape sequences
}

func (r *inputReader) parseEscape(data []byte) (int, Event) {
    if len(data) < 2 {
        extra := r.readMoreWithTimeout()  // Reads into escBuf
        if extra == 0 {
            return 0, Event{}  // Standalone ESC
        }
        // Rare: split packet, must allocate
        combined := make([]byte, len(data)+extra)
        copy(combined, data)
        copy(combined[len(data):], r.escBuf[:extra])
        data = combined
    }
    // ...
}
```

**Allocation avoidance:** 16-byte embedded buffer handles complete escape sequences in typical cases.

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

var csiMap = buildSequenceMap(csiSequences)  // Built at init

// Zero-alloc lookup via compiler optimization
func lookupCSI(seq []byte) (Key, Modifier, bool) {
    if s, ok := csiMap[string(seq)]; ok {  // No allocation
        return s.key, s.mod, true
    }
    return KeyNone, ModNone, false
}
```

**Extensibility:** Add entries to `csiSequences` or `ss3Sequences` for new key support. Map lookup converts `[]byte` to `string` without allocation (compiler optimization).

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
   - ALACRITTY_LOG
   - WEZTERM_PANE                      → TrueColor
3. TERM contains "truecolor|24bit|direct" → TrueColor
4. Default                             → 256-Color
```

**Rationale:** `COLORTERM` is the modern standard. tcell's TERM-first approach causes quantization in terminals that set `TERM=screen-256color` inside tmux but support true color.

### RGB → 256 Compute-Based Conversion
```go
func RGBTo256(c RGB) uint8 {
    r, g, b := int(c.R), int(c.G), int(c.B)

    // Exact grayscale fast path
    if r == g && g == b {
        if r < 8 { return 16 }
        if r > 238 { return 231 }
        return uint8(232 + (r-8)/10)
    }

    // Near-grayscale check (threshold 6)
    avg := (r + g + b) / 3
    dr, dg, db := abs(r-avg), abs(g-avg), abs(b-avg)
    if dr < 6 && dg < 6 && db < 6 {
        // Use grayscale ramp (232-255)
        // ...
    }

    // 6×6×6 color cube (16-231)
    return 16 + 36*cubeIndex[r] + 6*cubeIndex[g] + cubeIndex[b]
}
```

**Performance:** ~20 ALU ops, cache-friendly. Pre-computed `cubeIndex` lookup table (256 bytes) maps 0-255 to nearest cube level 0-5.

**Algorithm:**
1. Fast path for exact grayscale (r == g == b)
2. Near-grayscale check with threshold 6
3. Map to grayscale ramp (232-255) if close to gray
4. Otherwise use 6×6×6 color cube (16-231)

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
4. Create outputBuffer (128KB bufio.Writer)
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

**Thread Safety:** All public methods acquire `mu` lock and check `initialized`/`finalized` flags.

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
| bufio.Writer | 128KB | Once at init |
| cubeIndex table | 256 bytes | Once at package init |
| Input buffer | 256 bytes | Once at init |
| Escape buffer | 16 bytes | Embedded in inputReader |

### CPU

| Operation | Complexity |
|-----------|------------|
| Cell comparison | O(1) |
| RGB→256 conversion | O(1) compute (~20 ALU ops) |
| Escape sequence lookup | O(1) map lookup |
| Flush (diff) | O(W×H) |

### Zero-Allocation Paths

- **Input parsing:** Embedded `escBuf[16]` avoids allocation for complete escape sequences
- **Map lookups:** `lookupCSI([]byte)` and `lookupSS3([]byte)` use compiler optimization for zero-alloc string conversion
- **ANSI emission:** All sequences written directly to `bufio.Writer`
- **Integer formatting:** `writeInt()` uses direct digit extraction (no `strconv`)

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

// output.go - Add emission path in writeStyleCoalesced
```

---

## Known Limitations

| Limitation | Rationale |
|------------|-----------|
| No Windows support | Requires Windows Console API, fundamentally different |
| No mouse support | Scope control; can be added via escape sequence parsing |
| No bracketed paste | Scope control; requires mode enable/disable sequences |
| No Unicode width | Caller responsibility; would require wcwidth table |
| 100ms input poll | Balance between responsiveness and CPU usage |

---

## File Reference

| File | Lines | Purpose |
|------|-------|---------|
| terminal.go | ~350 | Interface, lifecycle, coordination, locking |
| output.go | ~350 | Double buffer, diff, coalesced flush |
| input.go | ~390 | Raw read, zero-alloc escape parsing, events |
| keys.go | ~195 | Key constants, sequence tables, zero-alloc lookups |
| ansi.go | ~130 | Sequence builders, integer formatting |
| color.go | ~110 | Detection, compute-based RGB→256 |
| resize_unix.go | ~80 | SIGWINCH, ioctl |
| compat.go | ~50 | tcell migration helpers |

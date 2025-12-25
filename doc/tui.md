# TUI Package — Design Document

## Overview

Immediate-mode TUI primitives for the `terminal` package. Provides layout calculations, drawing operations, and text utilities operating on cell buffers. Pure computation — no I/O, no state machines, no terminal control.

**Design Philosophy:** Application owns the render loop. TUI provides tools, not frameworks.

---

## Architecture
```
┌─────────────────────────────────────────────────────────────┐
│                      Application                            │
│  ┌───────────────────────────────────────────────────────┐  │
│  │                    Render Loop                        │  │
│  │  1. Create cell buffer                                │  │
│  │  2. Create root Region                                │  │
│  │  3. Layout (Split, Center, Grid)                      │  │
│  │  4. Draw (Text, Box, Progress, etc.)                  │  │
│  │  5. Flush to terminal                                 │  │
│  └───────────────────────────────────────────────────────┘  │
│         │                                                   │
│         ▼                                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                    tui Package                      │    │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐    │    │
│  │  │ Region  │ │ Layout  │ │  Draw   │ │  Text   │    │    │
│  │  │         │ │         │ │         │ │         │    │    │
│  │  │ Sub()   │ │ SplitH  │ │ Box()   │ │Truncate │    │    │
│  │  │ Inset() │ │ SplitV  │ │ Card()  │ │ Wrap()  │    │    │
│  │  │ Cell()  │ │ Center  │ │Progress │ │ Pad()   │    │    │
│  │  │ Fill()  │ │ Grid    │ │ Gauge() │ │         │    │    │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘    │    │
│  └─────────────────────────────────────────────────────┘    │
│         │                                                   │
│         ▼                                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              []terminal.Cell buffer                 │    │
│  │         (owned by application, not TUI)             │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | File | Role |
|-----------|------|------|
| `Region` | region.go | Bounded view into cell buffer, coordinate translation |
| Layout | layout.go | Splitting, centering, grid, responsive breakpoints |
| Draw | draw.go | Visual primitives (box, progress, checkbox, etc.) |
| Scroll | scroll.go | Scroll offset calculation, scrollbar rendering |
| Text | text.go | Truncation, padding, wrapping, measurement |

---

## Standalone vs Terminal Usage

### With Terminal Package (Typical)
```go
func main() {
    term := terminal.New()
    term.Init()
    defer term.Fini()

    for {
        w, h := term.Size()
        cells := make([]terminal.Cell, w*h)

        root := tui.NewRegion(cells, w, 0, 0, w, h)
        root.Fill(bgColor)
        // ... draw using TUI ...

        term.Flush(cells, w, h)
    }
}
```

### Standalone (Testing / Headless)
```go
func TestCardRendering(t *testing.T) {
    // No terminal init required
    cells := make([]terminal.Cell, 80*24)
    root := tui.NewRegion(cells, 80, 0, 0, 80, 24)

    content := root.Card("TEST", tui.LineDouble, borderColor)
    content.Text(0, 0, "Hello", fgColor, bgColor, 0)

    // Assert on cell contents
    if cells[81].Rune != 'H' {
        t.Error("expected 'H' at position (1,1)")
    }
}
```

### Buffer Composition (Multiple Sources)
```go
// Compose UI from independent components
func renderUI(cells []terminal.Cell, w, h int) {
    root := tui.NewRegion(cells, w, 0, 0, w, h)
    left, right := tui.SplitHFixed(root, 40)

    // Different subsystems render to their regions
    renderFileTree(left)    // Owns left region
    renderEditor(right)     // Owns right region
}
```

---

## Data Structures

### Region
```go
type Region struct {
    Cells  []terminal.Cell  // Reference to backing buffer
    TotalW int              // Total width of backing buffer
    X, Y   int              // Absolute position in buffer
    W, H   int              // Region dimensions
}
```

Core abstraction. All coordinates in Region methods are relative to region origin. Automatic bounds clipping prevents buffer overruns.

**Key Properties:**
- Value type — pass by value, no pointer indirection
- Non-owning — references external cell slice
- Composable — `Sub()` creates nested regions
- Safe — all operations bounds-checked

### LineType
```go
type LineType uint8

const (
    LineSingle  LineType = iota  // ┌─┐│└┘
    LineDouble                   // ╔═╗║╚╝
    LineRounded                  // ╭─╮│╰╯
    LineHeavy                    // ┏━┓┃┗┛
    LineNone                     // spaces (invisible border)
)
```

### CheckState
```go
type CheckState uint8

const (
    CheckNone    CheckState = iota  // [ ]
    CheckPartial                    // [o]
    CheckFull                       // [x]
    CheckPlus                       // [+]
)
```

---

## API Reference

### region.go — Core Abstraction

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewRegion` | `(cells []Cell, totalW, x, y, w, h int) Region` | Create region referencing cell slice with bounds |
| `Sub` | `(r Region) (x, y, w, h int) Region` | Nested region with relative coords, clipped to parent |
| `Inset` | `(r Region) (n int) Region` | Region shrunk by n cells on all sides |
| `Cell` | `(r Region) (x, y int, ch rune, fg, bg RGB, attr Attr)` | Set single cell with bounds checking |
| `Fill` | `(r Region) (bg RGB)` | Fill entire region with background color |
| `Clear` | `(r Region) ()` | Fill with spaces and zero colors |
| `Width` | `(r Region) () int` | Return region width |
| `Height` | `(r Region) () int` | Return region height |
| `Bounds` | `(r Region) () (x, y, w, h int)` | Return absolute position and dimensions |

### layout.go — Responsive Helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `Center` | `(outer Region, w, h int) Region` | Centered region of given size within outer |
| `SplitH` | `(r Region, ratios ...float64) []Region` | Split horizontally by ratios (0.0-1.0) |
| `SplitV` | `(r Region, ratios ...float64) []Region` | Split vertically by ratios |
| `SplitHFixed` | `(r Region, leftW int) (left, right Region)` | Split with fixed left width |
| `SplitVFixed` | `(r Region, topH int) (top, bottom Region)` | Split with fixed top height |
| `Columns` | `(availableW, itemW, gap int) int` | Calculate column count that fits in width |
| `GridLayout` | `(r Region, cols, rows, gapX, gapY int) []Region` | Grid of equally sized regions |
| `FitOrScroll` | `(contentH, availableH int) bool` | True if content exceeds available height |
| `BreakpointH` | `(w int, breakpoints ...int) int` | Index of first breakpoint <= w (descending order) |
| `BreakpointV` | `(h int, breakpoints ...int) int` | Index of first breakpoint <= h |

### draw.go — Visual Primitives

| Function | Signature | Description |
|----------|-----------|-------------|
| `Text` | `(r Region) (x, y int, s string, fg, bg RGB, attr Attr)` | Render text, truncate at region edge |
| `TextRight` | `(r Region) (y int, s string, fg, bg RGB, attr Attr)` | Right-aligned text on row |
| `TextCenter` | `(r Region) (y int, s string, fg, bg RGB, attr Attr)` | Centered text on row |
| `Box` | `(r Region) (line LineType, fg RGB)` | Border around region edge |
| `Card` | `(r Region) (title string, line LineType, fg RGB) Region` | Titled border, returns inner content region |
| `HLine` | `(r Region) (y int, line LineType, fg RGB)` | Horizontal line across width |
| `VLine` | `(r Region) (x int, line LineType, fg RGB)` | Vertical line across height |
| `Progress` | `(r Region) (x, y, w int, pct float64, fg, bg RGB)` | Horizontal progress bar (0.0-1.0) |
| `ProgressV` | `(r Region) (x, y, h int, pct float64, fg, bg RGB)` | Vertical progress bar (bottom-up) |
| `Spinner` | `(r Region) (x, y int, frame int, fg RGB)` | Animated spinner (use frame counter) |
| `Gauge` | `(r Region) (x, y, w int, value, max int, fg, bg RGB)` | Labeled gauge `[████░░] 75%` |
| `Checkbox` | `(r Region) (x, y int, state CheckState, fg RGB)` | Checkbox indicator `[x]`, `[o]`, `[ ]`, `[+]` |

### scroll.go — Scroll Management

| Function | Signature | Description |
|----------|-----------|-------------|
| `AdjustScroll` | `(cursor, scroll, visible, total int) int` | New scroll offset keeping cursor visible |
| `ScrollPercent` | `(scroll, visible, total int) int` | Scroll position as 0-100 percentage |
| `ScrollBar` | `(r Region, x int, offset, visible, total int, fg RGB)` | Vertical scrollbar with thumb |
| `ScrollIndicator` | `(r Region, y int, offset, visible, total int, fg RGB)` | Compact indicator: `Top`, `Bot`, `XX%` |
| `PageDelta` | `(visible int) int` | Recommended page scroll amount (visible/2) |
| `ClampScroll` | `(scroll, visible, total int) int` | Ensure scroll within valid range |
| `ClampCursor` | `(cursor, total int) int` | Ensure cursor within valid range |

### text.go — Text Utilities

| Function | Signature | Description |
|----------|-----------|-------------|
| `Truncate` | `(s string, maxLen int) string` | Truncate with `…` suffix |
| `TruncateLeft` | `(s string, maxLen int) string` | Truncate with `…` prefix |
| `TruncateMiddle` | `(s string, maxLen int) string` | Keep start/end, `…` in middle |
| `PadRight` | `(s string, width int) string` | Pad with trailing spaces |
| `PadLeft` | `(s string, width int) string` | Pad with leading spaces |
| `PadCenter` | `(s string, width int) string` | Center within width |
| `RuneLen` | `(s string) int` | Display width (rune count) |
| `WrapText` | `(s string, width int) []string` | Wrap at word boundaries |
| `RepeatRune` | `(r rune, n int) string` | String of n repeated runes |

---

## Usage Patterns

### Responsive Layout
```go
func render(root tui.Region) {
    // Breakpoints in descending order
    switch tui.BreakpointH(root.W, 120, 80, 40) {
    case 0:  // >= 120: three columns
        cols := tui.SplitH(root, 0.33, 0.34, 0.33)
        renderPane1(cols[0])
        renderPane2(cols[1])
        renderPane3(cols[2])
    case 1:  // >= 80: two columns
        cols := tui.SplitH(root, 0.5, 0.5)
        renderPane1(cols[0])
        // Stack panes 2+3 in right column
        top, bot := tui.SplitV(cols[1], 0.5, 0.5)
        renderPane2(top)
        renderPane3(bot)
    case 2:  // >= 40: single column
        rows := tui.SplitV(root, 0.33, 0.33, 0.34)
        renderPane1(rows[0])
        renderPane2(rows[1])
        renderPane3(rows[2])
    default: // < 40: minimal
        renderMinimal(root)
    }
}
```

### Modal Overlay
```go
func renderModal(root tui.Region, title, message string) {
    // Dim background (optional - draw over existing content)
    
    // Center a box
    boxW, boxH := 50, 10
    if boxW > root.W-4 {
        boxW = root.W - 4
    }
    if boxH > root.H-4 {
        boxH = root.H - 4
    }

    modal := tui.Center(root, boxW, boxH)
    modal.Fill(bgColor)
    content := modal.Card(title, tui.LineDouble, borderColor)
    
    // Word-wrap message
    lines := tui.WrapText(message, content.W)
    for i, line := range lines {
        if i >= content.H {
            break
        }
        content.Text(0, i, line, fgColor, bgColor, 0)
    }
}
```

### Scrollable List
```go
type ListView struct {
    Items  []string
    Cursor int
    Scroll int
}

func (lv *ListView) Render(r tui.Region) {
    visible := r.H
    lv.Cursor = tui.ClampCursor(lv.Cursor, len(lv.Items))
    lv.Scroll = tui.AdjustScroll(lv.Cursor, lv.Scroll, visible, len(lv.Items))

    // Reserve scrollbar column
    list, scrollCol := tui.SplitHFixed(r, r.W-1)

    // Render visible items
    for i := 0; i < visible && lv.Scroll+i < len(lv.Items); i++ {
        idx := lv.Scroll + i
        isCursor := idx == lv.Cursor

        bg := bgColor
        if isCursor {
            bg = cursorBg
        }

        // Fill row
        for x := 0; x < list.W; x++ {
            list.Cell(x, i, ' ', fgColor, bg, 0)
        }

        text := tui.Truncate(lv.Items[idx], list.W)
        list.Text(0, i, text, fgColor, bg, 0)
    }

    // Draw scrollbar
    tui.ScrollBar(scrollCol, 0, lv.Scroll, visible, len(lv.Items), dimColor)
}
```

### Header + Content + Footer
```go
func renderApp(root tui.Region) {
    // Fixed header
    header, rest := tui.SplitVFixed(root, 1)
    header.Fill(headerBg)
    header.Text(1, 0, "APP TITLE", accentColor, headerBg, terminal.AttrBold)

    // Fixed footer
    content, footer := tui.SplitVFixed(rest, rest.H-1)
    footer.Fill(headerBg)
    footer.Text(1, 0, "Status: OK", dimColor, headerBg, 0)

    // Remaining space for content
    renderContent(content)
}
```

### Progress Dashboard
```go
func renderDashboard(r tui.Region) {
    inner := r.Card("SYSTEM STATUS", tui.LineDouble, borderColor)

    y := 0
    inner.Text(0, y, "CPU:", labelColor, bgColor, 0)
    inner.Gauge(6, y, inner.W-6, 45, 100, accentColor, dimColor)
    y++

    inner.Text(0, y, "MEM:", labelColor, bgColor, 0)
    inner.Gauge(6, y, inner.W-6, 78, 100, warnColor, dimColor)
    y++

    inner.Text(0, y, "DSK:", labelColor, bgColor, 0)
    inner.Gauge(6, y, inner.W-6, 23, 100, goodColor, dimColor)
    y += 2

    inner.Text(0, y, "Tasks:", labelColor, bgColor, 0)
    inner.Progress(8, y, inner.W-8, 0.6, accentColor, dimColor)
}
```

---

## Performance Characteristics

### Memory

| Allocation | Size | Frequency |
|------------|------|-----------|
| Region struct | 40 bytes | Per region, stack-allocated |
| Box chars table | 120 bytes | Package init, static |
| Spinner frames | 40 bytes | Package init, static |

### CPU

| Operation | Complexity |
|-----------|------------|
| `NewRegion` | O(1) |
| `Sub` / `Inset` | O(1) |
| `Cell` | O(1) with bounds check |
| `Fill` | O(W×H) |
| `Text` | O(len(string)) |
| `Box` | O(W+H) |
| `SplitH/V` | O(n) where n = partition count |
| `AdjustScroll` | O(1) |
| `WrapText` | O(len(string)) |

### Zero-Allocation Operations

All Region methods operate on the backing slice without allocation:
- `Sub()`, `Inset()` return new Region value (stack)
- `Cell()`, `Fill()`, `Text()`, `Box()` write directly to cells
- `SplitH()`, `SplitV()`, `GridLayout()` allocate result slice only

---

## Integration with Terminal Package

### Render Loop Pattern
```go
func main() {
    term := terminal.New()
    if err := term.Init(); err != nil {
        os.Exit(1)
    }
    defer term.Fini()

    for {
        ev := term.PollEvent()
        
        switch ev.Type {
        case terminal.EventKey:
            if ev.Key == terminal.KeyCtrlC {
                return
            }
            handleInput(ev)

        case terminal.EventResize:
            // Buffer recreated on next render
        }

        // Render
        w, h := term.Size()
        cells := make([]terminal.Cell, w*h)
        
        root := tui.NewRegion(cells, w, 0, 0, w, h)
        root.Fill(bgColor)
        renderUI(root)
        
        term.Flush(cells, w, h)
    }
}
```

### Buffer Reuse (Optional Optimization)
```go
var cellPool = sync.Pool{
    New: func() any {
        return make([]terminal.Cell, 0, 200*50)
    },
}

func render(term terminal.Terminal) {
    w, h := term.Size()
    size := w * h

    cells := cellPool.Get().([]terminal.Cell)
    if cap(cells) < size {
        cells = make([]terminal.Cell, size)
    } else {
        cells = cells[:size]
    }
    defer cellPool.Put(cells)

    root := tui.NewRegion(cells, w, 0, 0, w, h)
    root.Fill(bgColor)
    // ... render ...
    term.Flush(cells, w, h)
}
```

---

## Limitations

| Limitation | Rationale |
|------------|-----------|
| No Unicode width calculation | Caller responsibility; would require wcwidth |
| No bi-directional text | Complexity; left-to-right only |
| No color themes | Application concern, not library |
| No retained widget state | By design — immediate mode |
| No input handling | Separation of concerns; use terminal package |
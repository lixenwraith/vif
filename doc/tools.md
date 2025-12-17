# Development Tools

vi-fighter includes two development tools for testing and benchmarking the render pipeline.

## blend-tester

Interactive tool for testing and visualizing the render package'r color blending operations and visual effects.

### Building and Running

```bash
go build -o blend-tester ./cmd/blend-tester
./blend-tester [--256 | --tc]
```

**Options:**
- `--256`: Force 256-color mode
- `--tc` or `--truecolor`: Force truecolor mode
- No flag: Auto-detect from environment

### Modes

The tool has four modes accessible via function keys or Tab/Shift-Tab:

#### 1. Palette Mode (F1)

Browse all named colors defined in `render/colors.go`.

**Controls:**
- `↑/↓`: Navigate colors
- `j/k`: Navigate (vi-style)
- Shows color name, RGB values, hex code, and visual preview

#### 2. Blend Mode (F2)

Interactive testing of blend operations with real-time preview.

**Features:**
- Source/destination color selection
- Blend operation cycling (Replace, Alpha, Add, Max, SoftLight, Screen, Overlay)
- Alpha value adjustment (0.0-1.0)
- Background preset selection (Black, White, GameBg, Custom)
- Custom hex color input
- Live formula display

**Controls:**
- `r/S`: Navigate source colors
- `d/D`: Navigate destination colors
- `o`: Cycle blend operations
- `a/A`: Adjust alpha (±0.1/±0.01)
- `b`: Cycle background presets
- `h`: Enter custom hex color

#### 3. Effect Mode (F3)

Visual effect testing for in-game effects.

**Sub-modes:**
- **Shield** (`1`): Test shield rendering with radius and opacity controls
- **Trail** (`2`): Test cleaner/materializer trail rendering
- **Flash** (`3`): Test flash animation timing
- **Heat** (`4`): Test heat meter visualization

**Controls vary by sub-mode** (displayed in UI)

#### 4. Analyze Mode (F4)

Color inspection and conversion tool.

**Features:**
- RGB to hex conversion
- Hex to RGB conversion
- Grayscale conversion preview
- Blend mode result previews
- Color analysis utilities

**Controls:**
- `h`: Enter hex color to analyze
- Shows conversions and various representations

### Global Controls

- `Q` or `Esc`: Quit
- `Tab`: Next mode
- `Shift-Tab`: Previous mode
- `F1-F4`: Jump to specific mode

### Use Cases

- **Color Design**: Browse palette and test color combinations
- **Blend Testing**: Verify blend mode behavior before implementing effects
- **Effect Preview**: Test visual effect parameters (shield size, trail length, etc.)
- **Color Debugging**: Analyze RGB/hex values and conversions

---

## render-benchmark

Performance benchmarking tool for measuring render pipeline throughput.

### Building and Running

```bash
go build -o render-benchmark ./cmd/render-benchmark
./render-benchmark [-duration=20s]
```

**Options:**
- `-duration`: Benchmark duration (default: 20 seconds)

### What It Does

Creates an animated scene with three distinct entities demonstrating different blend modes:

1. **"The Sun"** - Additive blending with hot core and corona
2. **"The Bubble"** - SoftLight/Overlay with rim lighting
3. **"The Pulse"** - Screen blending with interference patterns

Background includes a starfield with twinkling stars and a vertical gradient.

### Metrics Reported

- **Resolution**: Terminal dimensions and total cells
- **Total Frames**: Number of frames rendered
- **Average FPS**: Frames per second
- **Average Render Time**: Time spent compositing per frame
- **Average Flush Time**: Time spent writing to terminal per frame
- **Memory Stats**: Total allocations and malloc count

### Implementation Details

- Entities bounce within terminal bounds with velocity physics
- Aspect ratio correction for terminal cells (~2:1 width:height)
- Bounding box optimization for entity rendering
- Normalized distance calculations for gradient effects
- Direct cell array manipulation for performance

### Use Cases

- **Performance Testing**: Measure render pipeline efficiency
- **Regression Detection**: Compare benchmark results across changes
- **Blend Mode Demonstration**: Visual showcase of different blend modes
- **Terminal Compatibility**: Test rendering on different terminals/color modes

### Example Output

```
=== Visual Benchmark Results ===
Resolution:   120x40 (4800 cells)
Total Frames: 1547
Total Time:   20.00s
Average FPS:  77.35
------------------------------
Avg Render:   6.2ms
Avg Flush:    2.8ms
Total Alloc:  18432 bytes
Mallocs:      247
```

---

## Minimum Requirements

Both tools require:
- Go 1.24 or later
- Terminal with color support (256-color or truecolor)
- Minimum terminal size: 100×30 (blend-tester)

## Color Mode Support

Both tools support:
- **Truecolor** (24-bit RGB): Full color fidelity
- **256-color**: Downsampled via closest ANSI color matching

Color mode is auto-detected from environment variables (`COLORTERM`) but can be overridden via command-line flags (blend-tester only).
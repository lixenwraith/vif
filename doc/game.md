# Vi-Fighter Player Guide

Welcome to vi-fighter, a terminal-based typing game that challenges your vi/vim proficiency and typing speed! This guide will teach you everything you need to master the game.

## Table of Contents

1. [Game Objective](#game-objective)
2. [Getting Started](#getting-started)
3. [Controls & Vi Motion Commands](#controls--vi-motion-commands)
4. [Game Modes](#game-modes)
5. [Understanding the Screen](#understanding-the-screen)
6. [Sequence Types](#sequence-types)
7. [Core Mechanics](#core-mechanics)
8. [Scoring System](#scoring-system)
9. [Strategy Guide](#strategy-guide)
10. [Advanced Tips](#advanced-tips)

---

## Game Objective

**vi-fighter is a high-energy endurance game.** Your goal is to maximize your energy by typing character sequences that appear on screen while managing heat (your typing momentum) and avoiding penalties from the dynamic decay system.

There is no end state - the challenge is to survive as long as possible while the game gradually increases pressure through faster decay cycles.

---

## Getting Started

### Basic Gameplay Loop

1. **Navigate** to a character using vi/vim motion commands
2. Press **`i`** to enter INSERT mode
3. **Type** the character under the cursor
4. If correct, the character disappears and your energy increases
5. Continue typing the rest of the sequence
6. Press **`ESC`** to return to NORMAL mode and navigate to new targets

### Quick Tips for Beginners

- Start by typing **bright green or blue sequences** - they give the most points
- Avoid **red sequences** - they penalize your energy
- Watch for **gold** (bright yellow) - completing it fills your heat meter instantly
- Use simple motions like `h`/`j`/`k`/`l` at first, then graduate to `w`, `e`, `f<char>` for efficiency

---

## Controls & Vi Motion Commands

### Game Controls

- **`i`** - Enter INSERT mode (start typing sequences)
- **`/`** - Enter SEARCH mode (find text patterns)
- **`ESC`** - Return to NORMAL mode from Insert/Search/Command; activate ping grid in NORMAL mode (1 second)
- **`Enter`** - In NORMAL mode: Spawn 4-directional cleaners from cursor (requires heat ≥ 10, costs 10 heat)
- **`Ctrl+C`** or **`Ctrl+Q`** - Quit game

### Basic Navigation (NORMAL Mode)

#### Cardinal Movement
- **`h`** - Move left
- **`j`** - Move down
- **`k`** - Move up
- **`l`** - Move right
- **`Space`** - Move right (same as `l`)

**Count Prefixes**: Most motions can be prefixed with a number
- `5j` - Move down 5 lines
- `10l` - Move right 10 characters
- `3w` - Move forward 3 words
- `2fa` - Find the 2nd 'a' on the current line (multi-keystroke with count)

#### Line Navigation
- **`0`** - Jump to start of line (column 0)
- **`^`** - Jump to first non-whitespace character on line
- **`$`** - Jump to end of line (rightmost character)

#### Screen Navigation
- **`H`** - Jump to top of screen (same column)
- **`M`** - Jump to middle of screen (same column)
- **`L`** - Jump to bottom of screen (same column)
- **`gg`** - Jump to top-left corner (row 0, same column)
- **`G`** - Jump to bottom (last row, same column)
- **`go`** - Jump to absolute top-left (row 0, column 0)

#### Word Motions (Vim-style)

**Lowercase (word boundaries)**:
- **`w`** - Move to start of next word (alphanumeric or punctuation blocks)
- **`b`** - Move to start of previous word
- **`e`** - Move to end of current/next word

**Uppercase (WORD boundaries - space-delimited only)**:
- **`W`** - Move to start of next WORD
- **`B`** - Move to start of previous WORD
- **`E`** - Move to end of current/next WORD

Examples:
- Text: `foo-bar baz_qux`
- `w` stops at: `foo`, `-`, `bar`, `baz`, `_`, `qux`
- `W` stops at: `foo-bar`, `baz_qux`

#### Paragraph Motions
- **`{`** - Jump to previous empty line
- **`}`** - Jump to next empty line
- **`%`** - Jump to matching bracket (works with (), {}, [], <>)

#### Find & Search
- **`f<char>`** - Find character forward on current line (moves cursor TO the character)
  - Example: `fa` finds next 'a' on the current line
  - **Count-aware**: `2fa` finds the 2nd 'a', `3fb` finds the 3rd 'b'
  - If count exceeds available matches, moves to last match
- **`F<char>`** - Find character backward on current line (moves cursor TO the character)
  - Example: `Fa` finds previous 'a' on the current line
  - **Count-aware**: `2Fa` finds the 2nd 'a' backward, `3Fb` finds the 3rd 'b' backward
  - If count exceeds available matches, moves to first match (furthest back)
- **`t<char>`** - Till character forward (moves cursor one position BEFORE the character)
  - Example: `ta` finds next 'a' and stops one position before it
  - **Count-aware**: `2ta` finds the 2nd 'a', `3tb` finds the 3rd 'b'
  - Useful for editing: `dta` deletes up to (but not including) the next 'a'
- **`T<char>`** - Till character backward (moves cursor one position AFTER the character)
  - Example: `Ta` finds previous 'a' and stops one position after it
  - **Count-aware**: `2Ta` finds the 2nd 'a' backward, `3Tb` finds the 3rd 'b' backward
- **`;`** - Repeat last find/till command in the same direction
  - After `fa`, pressing `;` finds the next 'a' forward
  - After `Ta`, pressing `;` finds the previous 'a' backward
- **`,`** - Repeat last find/till command in the opposite direction
  - After `fa`, pressing `,` finds the previous 'a' backward (reverses to `Fa`)
  - After `Ta`, pressing `,` finds the next 'a' forward (reverses to `ta`)
- **`/<pattern>`** - Search for text pattern (enters SEARCH mode)
  - Type pattern, press `Enter` to jump to first match
- **`n`** - Repeat last search forward
- **`N`** - Repeat last search backward

### Delete Operations (Advanced)

**Warning**: Deleting Green or Blue sequences resets your heat!

**Note**: Gold sequences are protected and cannot be deleted with delete operators.

- **`x`** - Delete character at cursor
- **`dd`** - Delete entire line
- **`d<motion>`** - Delete with motion
  - `dw` - Delete word
  - `d$` - Delete to end of line
  - `d5j` - Delete current line + 5 lines down
- **`D`** - Delete to end of line (same as `d$`)

### INSERT Mode Behaviors

When in INSERT mode (white cursor):

- **Type matching character** - Character disappears, energy increases, cursor moves right, energy background flashes character color (200ms)
- **Type wrong character** - Red cursor flash (200ms), heat resets to zero, boost deactivates, energy background flashes black with red text (200ms)
- **`Space`** - Move right without typing (no heat change)
- **`ESC`** - Return to NORMAL mode

---

## Game Modes

vi-fighter has four input modes, similar to vi/vim:

### NORMAL Mode (Orange Cursor)
- **Purpose**: Navigate around the screen
- **Cursor**: Orange background
- **Status**: Shows "NORMAL" in light blue at bottom-left
- **Commands**: All vi motion commands available
- **Special Actions**:
  - `ESC` - Activate ping grid for 1 second (row/column highlight)
  - `Enter` - Spawn 4-directional cleaners from cursor (requires heat ≥ 10, costs 10 heat)
- **Entering**: Press `ESC` from INSERT, SEARCH, or COMMAND mode

### INSERT Mode (White Cursor)
- **Purpose**: Type character sequences
- **Cursor**: White background
- **Status**: Shows "INSERT" in light green at bottom-left
- **Entering**: Press `i` from NORMAL mode
- **Exiting**: Press `ESC` to return to NORMAL mode

### SEARCH Mode (No Cursor in Game Area)
- **Purpose**: Find text patterns on screen
- **Status**: Shows "SEARCH" in orange at bottom-left
- **Cursor**: Visible only in search input area (bottom status bar)
- **Entering**: Press `/` from NORMAL mode
- **Usage**: Type pattern, press `Enter` to jump, then `n`/`N` to cycle matches
- **Exiting**: Press `ESC` to return to NORMAL mode

### COMMAND Mode (No Cursor in Game Area)
- **Purpose**: Execute game commands
- **Status**: Shows "COMMAND" in dark purple at bottom-left
- **Cursor**: Visible only in command input area (bottom status bar)
- **Entering**: Press `:` from NORMAL mode
- **Usage**: Type command, press `Enter` to execute
- **Available Commands**:
  - `:quit` or `:q` - Exit the game
  - `:new` or `:n` - Start a new game (instant restart, clears all entities and state)
  - `:boost` - Activate boost mode for 10 seconds (2x spawn rate, 2x energy)
  - `:debug` or `:d` - Show debug overlay with system state information
  - `:help` or `:h` - Show help overlay with game instructions
- **Exiting**: Press `ESC` to return to NORMAL mode
- **Pause Behavior**:
  - **Game pauses**: All game time stops (decay timer, gold timeout, boost timer freeze)
  - **UI stays active**: Cursor continues blinking for visual feedback
  - **Visual dimming**: All characters dimmed to 70% brightness to indicate paused state
  - **Frame updates continue**: Screen still refreshes to show dimmed characters
  - **Time preservation**: When you exit COMMAND mode, timers resume from where they stopped

### OVERLAY Mode (Modal Window)
- **Purpose**: Display debug information or help content in a modal popup
- **Status**: Shows "OVERLAY" in status bar
- **Entering**: Execute `:debug` or `:help` command from COMMAND mode
- **Display**: Bordered window covering ~80% of screen with title and scrollable content
- **Controls**:
  - **ESC** or **ENTER**: Close overlay and return to NORMAL mode
  - **Up Arrow** or **k**: Scroll content up
  - **Down Arrow** or **j**: Scroll content down
- **Pause Behavior**: Game remains paused while overlay is displayed (same as COMMAND mode)
- **Input Hijacking**: All input is captured by overlay - no game commands available while overlay is active

---

## Understanding the Screen

### Top Bar (Heat Meter)
```
[████████████████████                    ]
 ^
 10-segment rainbow colored heat bar
```

- **Display**: 10-segment heat bar spanning full terminal width
- **Segments**: 0-10 filled blocks representing heat levels
- **Colors**: Red → Orange → Yellow → Green → Cyan → Blue → Purple (as segments fill)
- **Calculation**: Heat divided by 10 determines filled segments
  - Heat 0-9: 0 segments filled
  - Heat 10-19: 1 segment filled
  - Heat 90-99: 9 segments filled
  - Heat 100 (max): 10 segments filled (all segments)

### Left Margin (Relative Line Numbers)
```
  3
  2
  1
→ 0  ← Current line (orange background in NORMAL, orange text in SEARCH)
  1
  2
```

Shows distance from your cursor's current line.

### Main Game Area

The play field where character sequences appear:

- **Blue sequences** - Positive scoring, decays to Green
- **Green sequences** - Positive scoring, decays to Red
- **Red sequences** - Negative scoring (penalties)
- **Gold** - Bright yellow, 10 characters, fills heat to max

Each color has three brightness levels (Bright, Normal, Dark).

### Bottom Column Indicators

```
    |    1    |    2    |    3
         ^         ^         ^
      Every 5   Every 10  Every 10
```

Shows relative column positions from cursor:
- Current column marked as `0` (orange)
- Vertical bars `|` every 5 columns
- Numbers every 10 columns

### Bottom Status Bar

```
NORMAL    5j                    Boost: 0.3s  Decay: 45.2s  Energy: 1247  APM: 180  GT: 1234  FPS: 60
^         ^                     ^            ^             ^             ^         ^         ^
Mode      Last command          Boost timer  Decay timer   Total energy  Actions   Game      Frames
                                                                          /Minute   Ticks     /Second
```

**Left Section**:
- Mode indicator (NORMAL/INSERT/SEARCH/COMMAND)
- Last command executed (yellow text)

**Center Section**:
- Search pattern (when in SEARCH mode)
- Command text (when in COMMAND mode)

**Right Section** (from right to left):
- **FPS** - Cyan background, frames per second (rendering performance)
- **GT** - Light orange background, total game ticks since start (50ms intervals)
- **APM** - Lime green background, actions per minute (60-second rolling average)
- **Energy** - White background with black text, flashes character color on correct typing (200ms), total points
- **Decay** - Red background, countdown to next decay
- **Grid** - White text, ping grid timer (only when active)
- **Boost** - Pink background, boost multiplier timer (only when active)

### Drain Entities

Hostile drain entities that scale with heat:

- **Spawn Conditions**:
  - Heat >= 10
  - Count = floor(Heat / 10), max 10 drains
  - Example: 25 heat = 2 drains, 100 heat = 10 drains
- **Spawn Telegraph**: 1-second materialize animation per drain
  - Four cyan blocks ('█') converge from screen edges
  - Random position near cursor (±10 offset), skips occupied cells
  - Location locked at animation start
  - Staggered spawns (4 game ticks apart)
- **Visual**: Light cyan ╬ character
- **Movement**: Toward cursor, 1 cell/second
- **Shield Interaction**:
  - **Shield Active** (Energy > 0): Drains inside shield drain 100 energy/tick, persist
  - **Shield Inactive** (Energy <= 0): Cursor collision costs 10 heat, drain despawns
  - **Drain-Drain**: Multiple drains same cell mutually destroy
- **Despawn Triggers**:
  - Energy <= 0 AND Shield inactive
  - Heat decreases (LIFO - newest first)
  - Cursor collision without shield
  - Drain-drain collision
- **Strategic Notes**:
  - Shield automatically activates when Energy > 0
  - Shield protection has energy cost (passive 1/sec + zone 100/tick/drain)
  - Shield automatically deactivates when Energy drops to 0
  - Can lead drains to destroy Red sequences
  - Heat management controls drain count

### Visual Effects

**Ping Grid** (Press `ESC` in NORMAL mode):
- Highlights cursor's row and column
- Color: Almost-black (RGB: 5,5,5) in Normal mode, dark orange (RGB: 60,40,0) in Insert mode
- Lasts 1 second
- Helps locate cursor position quickly

**Decay Animation**:
- Dark gray background (RGB: 60,60,60) sweeps row by row during decay
- Animation speed: 100ms per row
- Indicates which characters are being degraded

**Error Cursor**:
- Red background flash for 200ms
- Appears when typing incorrect character in INSERT mode

**Energy Display Feedback**:
- **Default**: White background with black text
- **Correct Character**: Flashes the character's color (Blue, Green, or Gold) for 200ms
- **Error**: Flashes black background with bright red text for 200ms
- Provides instant visual feedback for typing accuracy

**Splash Feedback** (Success Indicator):
- **Display**: Large block characters (16×12 pixels each) appear in screen quadrant avoiding cursor and gold
- **Trigger Conditions**:
  - Successfully typing a character in INSERT mode
  - Collecting a nugget
  - Executing a command in NORMAL mode (e.g., `dd`, `dw`)
- **Animation**: 1-second fade-out from full opacity to transparent
- **Color Coding**:
  - Green sequences: Green (normal brightness)
  - Blue sequences: Blue (normal brightness)
  - Red sequences: Red (normal brightness)
  - Gold sequences: Bright yellow
  - Nuggets: Orange
  - Normal mode commands: Dark orange
- **Smart Positioning**: Quadrant-based placement avoiding cursor and active gold sequences
  - Opposite quadrant from cursor preferred
  - Avoids quadrants containing gold sequences
  - Automatically clamped to game area boundaries
- **Max Length**: Up to 8 characters for command strings
- **Purpose**: Provides satisfying visual confirmation of successful actions without blocking gameplay
- **Uniqueness**: Only one typing feedback splash active at a time (replaced on new action)

**Gold Countdown Timer**:
- **Display**: Large single-digit countdown (9 → 0) in bright yellow
- **Position**: Anchored to gold sequence (centered horizontally, 2 rows above/below)
- **Lifecycle**: Appears when gold spawns, disappears when gold completed/timeout/destroyed
- **Update**: Real-time countdown synchronized with gold timeout duration
- **Purpose**: Clear visual indicator of remaining time to complete gold sequence
- **Behavior**: Persistent until gold sequence finishes (not replaced by typing feedback)

---

## Sequence Types

The game features four types of character sequences, each with distinct behaviors:

### Green Sequences
- **Appearance**: Green text at three brightness levels (Bright/Normal/Dark)
- **Spawning**: Generated from Go source code in `data/` directory
- **Scoring**: Positive points (Heat × Level Multiplier × 1)
- **Level Multipliers**:
  - Bright: ×3
  - Normal: ×2
  - Dark: ×1
- **Decay Path**: Bright → Normal → Dark → **Red (Bright)**
- **Strategy**: Primary scoring source, prioritize bright sequences

### Blue Sequences
- **Appearance**: Blue text at three brightness levels (Bright/Normal/Dark)
- **Spawning**: Generated from Go source code in `data/` directory
- **Scoring**: Positive points (Heat × Level Multiplier × 1)
- **Level Multipliers**: Same as Green (Bright ×3, Normal ×2, Dark ×1)
- **Decay Path**: Bright → Normal → Dark → **Green (Bright)**
- **Strategy**: Same value as Green, color only matters for boost matching

### Red Sequences
- **Appearance**: Red text at three brightness levels
- **Spawning**: **NEVER spawned directly** - only created when Green (Dark) sequences decay
- **Scoring**: **Negative points** (penalties) - Heat × Level Multiplier × 1
- **Level Multipliers**: Bright ×3, Normal ×2, Dark ×1 (but all negative!)
- **Decay Path**: Bright → Normal → Dark → **Destroyed** (removed from screen)
- **Heat Effect**: Typing red characters resets heat to zero
- **Strategy**: Avoid if possible, or clear quickly if energy is low

### Gold
- **Appearance**: Bright yellow, always 10 characters long
- **Spawning**: Appears randomly after any decay animation completes
- **Content**: Random alphanumeric characters (a-z, A-Z, 0-9)
- **Duration**: 10 seconds (game time) before timeout - timer freezes during COMMAND mode pause
- **Countdown Timer**: Large yellow digit (9 → 0) appears above/below sequence showing remaining time
- **Reward**: Completing all 10 characters fills heat meter to maximum
- **Bonus Mechanic**: If heat is already at maximum when gold completed, triggers **Cleaners**
- **Scoring**: Typing gold characters does NOT affect heat or energy during typing
- **Pause Behavior**: Gold timeout uses game time, so it freezes when you enter COMMAND mode (`:`)
- **Strategy**: **Highest priority target** - can turn around a low-heat situation instantly

### Cleaners (Advanced Mechanic)

The game features two types of cleaner mechanics:

#### Horizontal Row Cleaners
- **Trigger**: Automatically activated when you complete gold while heat meter is already at maximum
- **Visual**: Bright yellow blocks that sweep horizontally across the screen
- **Behavior**: Cleaners scan for and automatically destroy Red characters on contact
- **Pattern**: Alternating sweep direction (odd rows left-to-right, even rows right-to-left)
- **Duration**: Configurable animation duration (default: 1 second from spawn to completion)
- **Selectivity**: **Only removes Red characters** - Blue and Green sequences are completely safe
- **Effect**: Provides instant relief when overwhelmed by Red penalty sequences
- **Strategic Use**:
  - Complete gold at max heat to clear accumulated Red characters
  - Allows aggressive high-heat play without Red penalty accumulation
  - Most effective when multiple Red sequences have decayed across different rows
- **Animation**: Configurable frame rate (default: 60 FPS) with trailing fade effect for visual clarity
- **Trail Effect**: Configurable trail length (default: 10 positions) with gradient fade (bright yellow → transparent)
- **Removal Flash**: Configurable flash duration (default: 150ms) appears when Red characters are destroyed

#### Directional Cleaners (4-Way Burst)
- **Triggers**:
  - Collect a nugget when heat is at maximum (100)
  - Press `Enter` in NORMAL mode when heat ≥ 10 (costs 10 heat)
- **Visual**: Four bright yellow blocks spawning outward from cursor position
- **Direction**: Simultaneously move right, left, down, and up from origin
- **Position Lock**: Each cleaner locks its row (horizontal) or column (vertical) at spawn time - cursor movement after spawn doesn't affect cleaner paths
- **Behavior**: Each cleaner destroys Red characters in its path (row or column)
- **Duration**: Same animation duration as horizontal cleaners (default: 1 second)
- **Selectivity**: **Only removes Red characters** - Blue and Green sequences are completely safe
- **Strategic Use**:
  - Manual activation via Enter key gives control over timing
  - Collect nuggets at max heat for free 4-directional clear
  - Position cursor strategically before spawning for maximum Red coverage
  - Useful when Red sequences are scattered across multiple rows and columns

#### Cleaner Configuration
The Cleaner system supports the following configurable parameters (see `constants/ui.go`):
- **Animation Duration**: Time for cleaner to traverse screen (default: 1.0 second)
- **Speed**: Movement speed in characters/second (default: auto-calculated from animation duration)
- **Trail Length**: Number of trailing positions (default: 10)
- **Trail Fade Time**: Duration for trail to fade completely (default: 0.3 seconds)
- **Trail Fade Curve**: Interpolation method - linear or exponential (default: linear)
- **Max Concurrent Cleaners**: Limit on simultaneous cleaners (default: unlimited)
- **Scan Interval**: Periodic scan interval for Red characters (default: disabled, only triggers on gold completion)
- **FPS**: Target frame rate for animation (default: 60)
- **Character**: Unicode character for cleaner block (default: '█')
- **Flash Duration**: Duration of removal flash effect in milliseconds (default: 150)

---

## Core Mechanics

### Heat System

**What is Heat?**
Heat represents your typing momentum and skill level. It's the most important mechanic in the game.

**Gaining Heat:**
- +1 for each correct Green or Blue character typed (normal)
- +2 for each correct Green or Blue character typed (boost active)
- Instant fill to maximum when completing gold

**Losing Heat:**
- Resets to 0 when:
  - Typing incorrect character
  - Typing any red character (correct or not)
  - Using arrow keys
  - Using `x` or delete motions on Green/Blue sequences
  - Using `h`/`j`/`k`/`l` more than 3 times consecutively

**Heat Effects:**
- **Scoring**: Heat directly multiplies your energy per character
  - Example: At heat 50, typing a Bright Green character = 50 × 3 = 150 points
- **Decay Speed**: Higher heat means faster decay (more pressure)
  - 0% heat: 60 seconds between decays
  - 50% heat: 35 seconds between decays
  - 100% heat: 10 seconds between decays
- **Boost Activation**: Must reach maximum heat to activate boost

**Maximum Heat:**
Heat caps at 100. The visual heat bar displays 10 segments, with each segment representing a 10-point range (first segment fills at heat 10, second at heat 20, etc., with all 10 segments filled only at heat 100).

### Boost System

**What is Boost?**
A 2× heat multiplier activated at maximum heat.

**Activation:**
1. Heat reaches maximum (100)
2. Boost activates with 0.5s timer
3. Tied to color of last character that maxed heat

**Color Matching:**
- Same color (Blue or Green): +0.5s extension (timer extended)
- Different color: Timer continues unchanged, BoostColor updates to new color (allows future extension)
- Example: Blue triggered boost → typing Green changes BoostColor to Green but timer keeps running

**Effects:**
- +2 heat per character (instead of +1)
- Faster heat rebuilding after mistakes
- **Does NOT multiply energy** - only heat gain

**Deactivation:**
- Timer expires (no extension within 0.5s)
- Typing incorrect character
- Typing red character
- Using arrow keys

**Visual Indicators:**
- Pink "Boost: X.Xs" in status bar

**Strategy:**
- Type same color to extend boost timer; different colors update BoostColor without penalty

### Shield System

**What is Shield?**
An energy-powered protective field that defends against drain attacks.

**Activation:**
- **Automatic**: Activates when Energy > 0
- **Deactivation**: Automatically deactivates when Energy drops to 0

**Energy Costs:**
- **Passive Drain**: 1 energy/second while active
- **Defense Cost**: 100 energy/tick per drain inside shield zone

**Protection:**
- Drains inside shield zone drain energy (not heat)
- Without shield: Drain collision costs 10 heat, drain despawns
- With shield: Drain collision costs energy only, drain persists

**Visual:**
- **Elliptical field** around cursor with gradient fade
- **Color**: Reflects last typed Blue/Green character (sequence type and brightness level)
- **Visibility**: Only visible when shield is active (Energy > 0)

**Strategy:**
- Monitor energy levels - shield costs can drain fast with multiple drains
- Shield most valuable when multiple drains are clustered near cursor
- Energy management is critical - let shield deactivate if energy too low
- Position cursor to maximize drains inside shield ellipse for optimal protection
- Shield color changes dynamically based on your typing

### Decay System

**What is Decay?**
An automated pressure mechanic that gradually degrades all sequences on screen, making them less valuable and eventually harmful.

**Timing:**
- Occurs every 10-60 seconds based on current heat level
- Higher heat = faster decay = more pressure
- Formula: `60 - (50 × heat_percentage)` seconds
- Example: At 50% heat, decay occurs every 35 seconds

**Visual Animation:**
- Matrix-style falling character effect from top to bottom
- One falling character per column with randomized speeds (5-15 rows/second)
- Characters change randomly as they fall (~40% chance per row)
- Each column processed independently for complete screen coverage
- When a falling character reaches a game character, decay is applied once
- Animation completes when slowest falling character reaches the bottom

**Decay Progression:**

**Phase 1: Brightness Decay** (within same color)
1. Bright → Normal (×3 to ×2 multiplier)
2. Normal → Dark (×2 to ×1 multiplier)
3. Dark → Color transition (see Phase 2)

**Phase 2: Color Decay Chain**
- **Blue (Dark)** → **Green (Bright)** - Changes to different positive color
- **Green (Dark)** → **Red (Bright)** - Becomes penalty sequence!
- **Red (Dark)** → **Destroyed** - Removed from screen

**What Gets Decayed:**
- All Blue, Green, and Red sequences
- Gold is **immune** to decay

**Pause Behavior:**
- Decay timer uses game time and freezes during COMMAND mode pause
- When you exit COMMAND mode, decay timer resumes from where it stopped

**Strategy Implications:**
- Bright sequences are time-sensitive (2 decays until color change)
- Dark sequences are close to becoming different colors
- Green (Dark) is ONE decay away from becoming Red
- Red (Dark) will disappear next decay (can wait it out)

### Spawn System

**Content Source:**
New sequences are generated from Go source code in `data/` directory.

**6-Color Limit System:**
Only 6 color/level combinations can exist simultaneously:
- Blue: Bright, Normal, Dark (3 slots)
- Green: Bright, Normal, Dark (3 slots)

When all characters of one color/level are cleared, that slot opens for new spawns.

**Spawn Rate:**
- Base: Every 2 seconds
- **<30% screen filled**: 2× faster (1 second) - aggressive spawning
- **>70% screen filled**: 2× slower (4 seconds) - reduced spawning

**Placement Intelligence:**
- Random position on screen
- Avoids collisions with existing characters
- Maintains cursor exclusion zone (±5 horizontal, ±3 vertical)
- Each line attempts placement 3 times before being discarded

**Maximum Capacity:**
200 characters can be on screen at once.

---

## Scoring System

### Energy Formula

**Points = Heat × Level Multiplier × Type Modifier**

**Level Multipliers:**
- Bright sequences: ×3
- Normal sequences: ×2
- Dark sequences: ×1

**Type Modifiers:**
- Green/Blue sequences: ×1 (positive)
- Red sequences: ×1 (but negative!)
- Gold sequences: No energy during typing

### Example Calculations

**Example 1**: Heat at 50, typing Bright Green character
- Energy = 50 × 3 × 1 = **+150 points**

**Example 2**: Heat at 30, typing Normal Blue character
- Energy = 30 × 2 × 1 = **+60 points**

**Example 3**: Heat at 80, typing Dark Red character (penalty!)
- Energy = 80 × 1 × (-1) = **-80 points**
- Plus heat resets to 0!

**Example 4**: Heat at 100 (max), boost active, typing Bright Blue
- Heat gain = +2 (boost multiplier)
- Energy = 100 × 3 × 1 = **+300 points**
- New heat = 102, boost extends +0.5s

### Scoring Strategy

**Maximize Energy:**
1. Build heat as high as possible before typing bright sequences
2. Type Bright sequences (×3) > Normal (×2) > Dark (×1)
3. Use boost to quickly rebuild heat after mistakes
4. Avoid red sequences (negative scoring + heat reset)

**Energy vs. Heat Trade-off:**
- Low heat + Bright sequence = Modest energy, +1 heat
- High heat + Dark sequence = Good energy, +1 heat
- **Optimal**: High heat + Bright sequence = Maximum energy!

---

## Strategy Guide

### Beginner Strategy (0-500 points)

**Goal**: Learn motions and build heat safely

1. **Focus on Green and Blue sequences** - All positive scoring
2. **Start with simple motions**: `h`/`j`/`k`/`l` for navigation
3. **Avoid red sequences** completely until comfortable
4. **Let decay happen** - Don't panic about the timer
5. **Chase gold** aggressively - easiest way to build heat
6. **Don't use delete** (`x`, `dd`, etc.) - it resets heat
7. **Understand Drains**: When heat >= 10, drains spawn (count = floor(heat/10), max 10). Watch for cyan blocks converging (1-second warning). Shield activates automatically when Energy > 0 for protection, but costs energy (1/sec passive + 100/tick per drain in shield).

**Motion Practice:**
- Use `0` and `$` to jump to line edges
- Try `w` and `e` for word navigation
- Experiment with `f<char>` to find specific characters

### Intermediate Strategy (500-2000 points)

**Goal**: Master heat management and boost activation

1. **Prioritize bright sequences** when heat is high (50+)
   - Bright Green/Blue at high heat = massive energy
2. **Activate boost deliberately**:
   - Build heat to max
   - Scan screen for clusters of same color (Blue or Green)
   - Type same color repeatedly to extend boost
3. **Manage decay pressure**:
   - Watch decay timer in status bar
   - Clear Green (Dark) sequences before they become Red
   - Learn the 35-second rhythm at medium heat
4. **Use efficient motions**:
   - `gg`, `G`, `H`, `M`, `L` for screen jumps
   - `w`, `b`, `e` instead of repeated `l`/`h`
   - `/<pattern>` to find specific text
5. **Drain & Shield management**:
   - Drains spawn at heat >= 10 (count = floor(heat/10))
   - Shield activates automatically when Energy > 0
   - **Without shield** (Energy <= 0): Cursor collision costs 10 heat, drain despawns
   - **With shield** (Energy > 0): Drains in shield cost 100 energy/tick, persist
   - Shield passive cost: 1 energy/second
   - Lower heat to despawn excess drains (newest first)
   - Shield most valuable with multiple drains clustered
   - Energy management critical when shield active

**Heat Recovery:**
- If heat drops below 20, hunt for gold
- Use boost to quickly rebuild from mistakes
- Balance aggression (high heat) with sustainability

### Advanced Strategy (2000+ points)

**Goal**: Survive extreme decay pressure and maximize multipliers

1. **Color management during boost**:
   - Type same color to extend timer (+0.5s per character)
   - Different colors update BoostColor but timer continues (no penalty)
   - Scan 2-3 sequences ahead for matching color opportunities
   - Navigate to next target BEFORE finishing current sequence
   - Aim for 5+ extensions (2.5+ seconds of boost time)

2. **Dynamic heat management**:
   - **70-90% heat**: Optimal zone - high scoring, manageable decay (15-20s)
   - **100% heat**: Dangerous - 10s decay, only worth it with boost active
   - **30-50% heat**: Safe zone - 35-45s decay, recovery period

3. **Sequence triage** (what to type when):
   - **Immediate priority**:
     - Gold sequences (10s timeout)
     - Green (Dark) near decay timer (about to become Red)
     - Red sequences when energy is low
   - **High priority**: Bright sequences when heat >70
   - **Medium priority**: Normal sequences, Blue (Dark)
   - **Low priority**: Dark sequences, Red (Dark) that will decay away

4. **Decay timing exploitation**:
   - Note decay timer when it appears (e.g., "Decay: 35.2s")
   - Calculate time to next decay during typing
   - Save Bright sequences for after decay (they refresh to normal)
   - Clear Dark sequences just before decay to avoid color change

5. **Advanced motion efficiency**:
   - Chain motions: `3w` then `2e` instead of counting characters
   - Use `{` and `}` to navigate between sequence blocks
   - Search (`/`) for rare characters to jump across screen
   - `go` to reset to top-left when screen gets chaotic

6. **Energy optimization**:
   - Never type Dark sequences at low heat (<20) - poor return
   - Type Bright sequences ONLY at 70+ heat (×3 multiplier = 210+ points)
   - Clear Red sequences immediately if energy <1000 (big percentage loss)
   - Let Red (Dark) decay away instead of typing (no heat reset)

7. **Endurance tactics**:
   - Maintain 50-80% heat for sustainable play
   - Use gold as "panic buttons" for heat recovery
   - Accept small energy hits to avoid heat resets
   - Take calculated risks: typing one Red to clear screen space

8. **Advanced drain & shield tactics**:
   - **Heat-based spawn control**: floor(heat/10) = drain count - manage heat to control pressure
   - **Telegraph timing**: 1-second materialize animation, 4 ticks stagger between spawns
   - **Shield energy economics**:
     - Automatic activation when Energy > 0
     - Passive: 1 energy/sec while active
     - Defense: 100 energy/tick per drain in shield ellipse
     - Break-even: Shield worth it when drains would cost >101 energy/sec without it
   - **Shield zone positioning**: Position cursor to maximize drains inside shield ellipse
   - **Energy-zero despawn**: Drains despawn when Energy drops to 0 (shield deactivates)
   - **Collision tactics**:
     - Without shield (Energy <= 0): -10 heat per drain collision, drain despawns
     - With shield (Energy > 0): Energy cost only, no heat loss, drain persists
     - Drain-drain: Multiple drains same cell mutually destroy
   - **Strategic energy management**: Build energy reserves before high-drain situations
   - **Heat manipulation**: Lower heat to despawn excess drains (LIFO - newest first)
   - **Exploit drain paths**: Let drains destroy Red sequences (saves typing penalty)

---

## Advanced Tips

### Motion Efficiency

**Avoid Consecutive Move Penalty:**
Using `h`/`j`/`k`/`l` more than 3 times in a row resets heat!

- **Bad**: `l` `l` `l` `l` (4th press resets heat!)
- **Good**: `4l` (count prefix, single motion)
- **Better**: `w` or `e` (word motion, fewer keystrokes)

**Motion Combinations:**
- `3w` then `fe` - "Go forward 3 words, then find letter 'e'"
- `G` then `0` - "Jump to bottom, then to start of line"
- `/func` then `n` - "Search for 'func', cycle to next match"

### Heat Preservation

**Actions That Reset Heat (AVOID!):**
- Typing incorrect character
- Typing red characters
- Using arrow keys (use `h`/`j`/`k`/`l` or better motions instead)
- Using `x`, `dd`, or other deletes on Green/Blue
- Using `h`/`j`/`k`/`l` 4+ times consecutively

**Safe Actions (No Heat Loss):**
- Navigation with `w`, `b`, `e`, `gg`, `G`, `H`, `M`, `L`, `0`, `$`, `{`, `}`
- Search with `/`, `f`, `n`, `N`
- Mode switching (`i`, `ESC`)
- Using `Space` in INSERT mode (moves without typing)
- Deleting red sequences with `x` or `dd`

### Boost Mastery

**Maximizing Boost Duration:**
1. Before reaching max heat, scan screen for color availability
2. Once boost active, type same color to extend timer (+0.5s per character)
3. Can switch colors without timer penalty - BoostColor updates for future extensions
4. Navigate to next target during typing to maintain momentum
5. Use `f<char>` to quickly find matching characters on current line

**Boost Timing Strategy:**
- **Abundant same-color sequences**: Focus on extensions for maximum boost time
- **Mixed colors available**: Type whatever's closest, return to matching color when convenient
- **Color flexibility**: Timer persists when switching, allowing adaptive play

### Gold Tactics

**When to Chase Gold:**
- Heat below 30 (urgent heat recovery)
- Just made a mistake and heat reset to 0
- About to lose boost and need max heat again
- High-pressure situation (10s decay, many Red sequences)

**When to Ignore Gold:**
- Heat already at 90%+ and boost active
- Only 1-2 seconds remaining on timeout (not enough time)
- Currently maintaining 5+ boost extensions (momentum too valuable)

**Gold Typing Tips:**
- Use `/` search for first 2-3 characters if far from cursor
- Gold is always 10 characters - muscle memory the rhythm
- No heat/energy during typing, so type quickly without worry
- Timer uses game time - pausing (COMMAND mode) stops the countdown

### Screen Reading

**Visual Patterns to Recognize:**
- **Decay warning**: Green (Dark) sequences = 1 decay from becoming Red
- **Safe to ignore**: Red (Dark) sequences = will disappear next decay
- **High value**: Clusters of Bright sequences at high heat
- **Boost opportunity**: 3+ same-color sequences in close proximity

**Color Vision at a Glance:**
- **Bright**: Lightest shade, crisp text
- **Normal**: Medium shade, clear text
- **Dark**: Darkest shade, still readable

**Heat Bar Colors (Top Bar):**
- Red/Orange (0-30%): Low heat, safe zone, slow decay
- Yellow/Green (30-60%): Medium heat, balanced play
- Cyan/Blue (60-90%): High heat, high scoring, faster decay
- Purple/Pink (90-100%): Maximum heat, extreme pressure, boost ready

### Risk Management

**When to Type Red Sequences:**
- Your energy is very low (< 500) - percentage impact is small
- Red is blocking path to high-value Bright sequence
- Screen is overcrowded (>70% full) - need to clear space
- Red is Bright level and your heat is low (paradox: penalty, but you're resetting soon anyway)

**When to Avoid Red:**
- Heat is high (50+) - you'll lose valuable momentum
- Energy is high (2000+) - big point penalties hurt
- Red is Dark level - will disappear next decay (just wait)
- Currently on boost streak (red breaks boost)

### Performance Under Pressure

**At 10-Second Decay (100% Heat):**
- Prioritize staying alive over scoring
- Type ONLY Bright sequences (maximum return on time)
- Keep boost active at all costs (need +2 heat to maintain)
- Consider letting some Green decay to Red, then ignore Red (Dark)
- Gold becomes critical for momentary relief

**Recovery from Heat Reset:**
1. Don't panic - decay slows down at low heat (60s intervals)
2. Locate gold if present (instant recovery)
3. Type 2-3 Bright sequences to build heat to 30-40 quickly
4. Re-assess situation: Do you chase max heat again or stabilize?

### Muscle Memory Development

**Practice These Patterns:**
- `i` [type sequence] `ESC` `w` `i` - Word-by-word flow
- `/<text>` `Enter` `i` [type] `ESC` - Search-type-exit
- `gg` → scan → `G` → scan - Full screen assessment
- `f<char>` `i` [type] `ESC` - Find-type on current line

**Speed Drills:**
- Navigate to 10 different sequences using only `w`/`b`/`e` (no `hjkl`)
- Complete gold in under 5 seconds
- Maintain boost for 3+ seconds (7+ character extensions)
- Type 20 characters without heat reset

---

## Quick Reference Card

### Essential Commands
| Command | Effect |
|---------|--------|
| `i` | Enter INSERT mode (start typing) |
| `ESC` | Return to NORMAL mode |
| `h` `j` `k` `l` | Move left/down/up/right |
| `w` `b` `e` | Word forward/back/end |
| `0` `$` | Start/end of line |
| `gg` `G` | Top/bottom of screen |
| `f<char>` | Find character on line |
| `/<text>` | Search for text |
| `Enter` | Activate ping grid (1s) |

### Sequence Priority (Descending)
1. **Gold** (timeout in <3s or heat <30)
2. **Bright** Green/Blue (heat >70)
3. **Green (Dark)** near decay timer
4. **Normal** Green/Blue
5. **Dark** Green/Blue
6. **Red** (only if energy <1000 or blocking)

### Heat Guidelines
| Heat % | Decay Timer | Risk Level | Strategy |
|--------|-------------|------------|----------|
| 0-30% | 60-45s | Low | Build safely, learn motions |
| 30-60% | 45-25s | Medium | Balanced play, steady scoring |
| 60-90% | 25-12s | High | High scoring, boost focus |
| 90-100% | 12-10s | Extreme | Boost required, survival mode |

---

**Good luck, and may your heat bar stay maxed and your boost timer never expire!**
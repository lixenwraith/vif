# Game Interaction

## Game Controls

- **`i`** - Enter INSERT mode (start typing sequences)
- **`/`** - Enter SEARCH mode (find text patterns)
- **`ESC`** - Return to NORMAL mode
- **`Tab`** - Jump to nugget (costs 10 score if score >= 10)
- **`Enter`** - Activate ping grid for 1 second (shows row/column guides)
- **`Ctrl+C`** or **`Ctrl+Q`** - Quit game

## Game Modes

vi-fighter has three input modes, just like vi/vim:

### NORMAL Mode (Orange Cursor)
- **Purpose**: Navigate around the screen
- **Cursor**: Orange background
- **Status**: Shows "NORMAL" in light blue at bottom-left
- **Commands**: All vi motion commands available
- **Entering**: Press `ESC` from INSERT or SEARCH mode

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

## Vi Motion Commands

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

**Important**: Using `h`/`j`/`k`/`l` more than 3 times consecutively resets heat. Use count prefixes or word motions for efficiency.

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

**Examples**:
- Text: `foo-bar baz_qux`
- `w` stops at: `foo`, `-`, `bar`, `baz`, `_`, `qux`
- `W` stops at: `foo-bar`, `baz_qux`

#### Paragraph Motions
- **`{`** - Jump to previous empty line
- **`}`** - Jump to next empty line
- **`%`** - Jump to matching bracket (works with (), {}, [], <>)

#### Find & Search

**Find on Current Line**:
- **`f<char>`** - Find character forward (moves cursor TO the character)
  - Example: `fa` finds next 'a' on the current line
  - **Count-aware**: `2fa` finds the 2nd 'a', `3fb` finds the 3rd 'b'
  - If count exceeds available matches, moves to last match
- **`F<char>`** - Find character backward (moves cursor TO the character)
  - Example: `Fa` finds previous 'a' on the current line
  - **Count-aware**: `2Fa` finds the 2nd 'a' backward, `3Fb` finds the 3rd 'b' backward
  - If count exceeds available matches, moves to first match (furthest back)

**Till on Current Line**:
- **`t<char>`** - Till character forward (moves cursor one position BEFORE the character)
  - Example: `ta` finds next 'a' and stops one position before it
  - **Count-aware**: `2ta` finds the 2nd 'a', `3tb` finds the 3rd 'b'
  - Useful for editing: `dta` deletes up to (but not including) the next 'a'
- **`T<char>`** - Till character backward (moves cursor one position AFTER the character)
  - Example: `Ta` finds previous 'a' and stops one position after it
  - **Count-aware**: `2Ta` finds the 2nd 'a' backward, `3Tb` finds the 3rd 'b' backward

**Repeat Find/Till**:
- **`;`** - Repeat last find/till command in the same direction
  - After `fa`, pressing `;` finds the next 'a' forward
  - After `Ta`, pressing `;` finds the previous 'a' backward
- **`,`** - Repeat last find/till command in the opposite direction
  - After `fa`, pressing `,` finds the previous 'a' backward (reverses to `Fa`)
  - After `Ta`, pressing `,` finds the next 'a' forward (reverses to `ta`)

**Search**:
- **`/<pattern>`** - Search for text pattern (enters SEARCH mode)
  - Type pattern, press `Enter` to jump to first match
- **`n`** - Repeat last search forward
- **`N`** - Repeat last search backward

### Delete Operations (Advanced)

**Warning**: Deleting Green or Blue sequences resets your heat!

- **`x`** - Delete character at cursor
- **`dd`** - Delete entire line
- **`d<motion>`** - Delete with motion
  - `dw` - Delete word
  - `d$` - Delete to end of line
  - `d5j` - Delete current line + 5 lines down
  - `dfa` - Delete from cursor to next 'a' (inclusive)
  - `d2fa` - Delete from cursor to 2nd 'a' (inclusive)
- **`D`** - Delete to end of line (same as `d$`)

### INSERT Mode Behaviors

When in INSERT mode (white cursor):

- **Type matching character** - Character disappears, score increases, cursor moves right, score background flashes character color (200ms)
- **Type wrong character** - Red cursor flash (200ms), heat resets to zero, boost deactivates, score background flashes black with red text (200ms)
- **`Space`** - Move right without typing (no heat change)
- **`Tab`** - Jump to nugget (costs 10 score if score >= 10)
- **`ESC`** - Return to NORMAL mode

## Visual Effects

### Ping Grid (Press `Enter` in NORMAL mode)
- Almost black background (RGB: 5,5,5) highlights cursor's row and column
- Lasts 1 second
- Helps locate cursor position quickly

### Decay Animation
- Dark gray background (RGB: 60,60,60) sweeps row by row during decay
- Animation speed: 100ms per row
- Indicates which characters are being degraded

### Error Cursor
- Red background flash for 200ms
- Appears when typing incorrect character in INSERT mode

### Score Display Feedback
- **Default**: White background with black text
- **Correct Character**: Flashes the character's color (Blue, Green, or Gold) for 200ms
- **Error**: Flashes black background with bright red text for 200ms
- Provides instant visual feedback for typing accuracy

### Nugget Cursor Contrast
- When cursor is on a nugget, the nugget character darkens to dark brown for better readability
- Orange cursor background maintained for color identity

## Heat Preservation Tips

**Actions That Reset Heat (AVOID!)**:
- Typing incorrect character
- Typing red characters
- Using arrow keys (use `h`/`j`/`k`/`l` or better motions instead)
- Using `x`, `dd`, or other deletes on Green/Blue
- Using `h`/`j`/`k`/`l` 4+ times consecutively

**Safe Actions (No Heat Loss)**:
- Navigation with `w`, `b`, `e`, `gg`, `G`, `H`, `M`, `L`, `0`, `$`, `{`, `}`
- Search with `/`, `f`, `F`, `t`, `T`, `n`, `N`, `;`, `,`
- Mode switching (`i`, `ESC`)
- Using `Space` in INSERT mode (moves without typing)
- Deleting red sequences with `x` or `dd`
- Tab jump to nugget (costs score, not heat)

## Motion Efficiency Tips

### Avoid Consecutive Move Penalty
Using `h`/`j`/`k`/`l` more than 3 times in a row resets heat!

- **Bad**: `l` `l` `l` `l` (4th press resets heat!)
- **Good**: `4l` (count prefix, single motion)
- **Better**: `w` or `e` (word motion, fewer keystrokes)

### Motion Combinations
- `3w` then `fe` - "Go forward 3 words, then find letter 'e'"
- `G` then `0` - "Jump to bottom, then to start of line"
- `/func` then `n` - "Search for 'func', cycle to next match"

---

[← Back to Game Index](game-index.md)

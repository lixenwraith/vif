# Vi-Fighter Player Guide

Welcome to vi-fighter, a terminal-based typing game that challenges your vi/vim proficiency and typing speed!

## Documentation

- **[Game Interaction](game-interaction.md)** - Controls, vi motion commands, game modes, input handling
- **[Game Mechanics](game-mechanics.md)** - Sequence types, heat system, boost, decay, spawn, scoring, nuggets
- **[Strategy Guide](game-strategy.md)** - Beginner, intermediate, and advanced tactics
- **[Quick Reference](game-reference.md)** - Essential commands and sequence priorities

## Game Objective

**vi-fighter is a high-score endurance game.** Your goal is to maximize your score by typing character sequences that appear on screen while managing heat (your typing momentum) and avoiding penalties from the dynamic decay system.

There is no end state - the challenge is to survive as long as possible while the game gradually increases pressure through faster decay cycles.

## Basic Gameplay Loop

1. **Navigate** to a character using vi/vim motion commands
2. Press **`i`** to enter INSERT mode
3. **Type** the character under the cursor
4. If correct, the character disappears and your score increases
5. Continue typing the rest of the sequence
6. Press **`ESC`** to return to NORMAL mode and navigate to new targets

## Quick Tips for Beginners

- Start by typing **bright green or blue sequences** - they give the most points
- Avoid **red sequences** - they penalize your score
- Watch for **gold sequences** (bright yellow) - completing them fills your heat meter instantly
- Look for **nuggets** (orange '●') - collecting them boosts heat
- Use simple motions like `h`/`j`/`k`/`l` at first, then graduate to `w`, `e`, `f<char>` for efficiency

## Understanding the Screen

### Top Bar (Heat Meter)
```
[████████████████████                    ]
```
- **Display**: 10-segment heat bar spanning full terminal width
- **Segments**: 0-10 filled blocks representing 0-100% heat
- **Colors**: Red → Orange → Yellow → Green → Cyan → Blue → Purple (as segments fill)

### Left Margin (Relative Line Numbers)
Shows distance from your cursor's current line.

### Main Game Area
The play field where character sequences appear:
- **Blue sequences** - Positive scoring, decays to Green
- **Green sequences** - Positive scoring, decays to Red
- **Red sequences** - Negative scoring (penalties)
- **Gold sequences** - Bright yellow, 10 characters, fills heat to max
- **Nuggets** - Orange '●', collectible heat bonus

### Bottom Status Bar
```
NORMAL    5j                              Boost: 0.3s  Decay: 45.2s  Score: 1247
```
- Mode indicator (NORMAL/INSERT/SEARCH/COMMAND)
- Last command executed
- Boost timer (when active)
- Decay timer
- Total score

## Game Controls (Quick Overview)

- **`i`** - Enter INSERT mode (start typing sequences)
- **`/`** - Enter SEARCH mode (find text patterns)
- **`ESC`** - Return to NORMAL mode
- **`Tab`** - Jump to nugget (costs 10 score)
- **`Enter`** - Activate ping grid for 1 second
- **`Ctrl+C`** or **`Ctrl+Q`** - Quit game

---

For detailed information on each topic, see the linked documentation files above.

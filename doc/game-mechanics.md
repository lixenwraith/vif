# Game Mechanics

## Sequence Types

The game features four types of character sequences, plus collectible nuggets:

### Green Sequences
- **Appearance**: Green text at three brightness levels (Bright/Normal/Dark)
- **Spawning**: Generated from Go source code in `assets/data.txt`
- **Scoring**: Positive points (Heat × Level Multiplier × 1)
- **Level Multipliers**:
  - Bright: ×3
  - Normal: ×2
  - Dark: ×1
- **Decay Path**: Bright → Normal → Dark → **Red (Bright)**
- **Strategy**: Primary scoring source, prioritize bright sequences

### Blue Sequences
- **Appearance**: Blue text at three brightness levels (Bright/Normal/Dark)
- **Spawning**: Generated from Go source code in `assets/data.txt`
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
- **Strategy**: Avoid if possible, or clear quickly if score is low

### Gold Sequences
- **Appearance**: Bright yellow, always 10 characters long
- **Spawning**: Appears randomly after any decay animation completes
- **Content**: Random alphanumeric characters (a-z, A-Z, 0-9)
- **Duration**: 10 seconds before timeout (disappears if not completed)
- **Reward**: Completing all 10 characters fills heat meter to maximum
- **Bonus Mechanic**: If heat is already at maximum when gold completed, triggers **Cleaners**
- **Scoring**: Typing gold characters does NOT affect heat or score during typing
- **Strategy**: **Highest priority target** - can turn around a low-heat situation instantly

### Nuggets (Collectibles)
- **Appearance**: Orange alphanumeric character (randomly selected from a-z, A-Z, 0-9)
- **Spawning**: Random position every 5 seconds (only one active at a time)
- **Collection**: Type **the matching alphanumeric character** shown at nugget position
- **Reward**: +10% of max heat (minimum 1, e.g., 80-char screen → +8 heat)
- **Tab Jump**: Press `Tab` to jump cursor directly to nugget (costs 10 score, requires score >= 10)
- **Decay Interaction**: Falling decay entities destroy nuggets on contact
- **Respawn**: Automatic respawn 5 seconds after collection or destruction
- **Visual**: Dark brown foreground when cursor overlaps for better contrast
- **Invariant**: At most one nugget active at any time (enforced via atomic operations)
- **Strategy**: Free heat boost, use Tab jump when score allows, prioritize before decay reaches it

## Heat System

**What is Heat?**
Heat represents your typing momentum and skill level. It's the most important mechanic in the game.

**Gaining Heat:**
- +1 for each correct Green or Blue character typed (normal)
- +2 for each correct Green or Blue character typed (boost active)
- +10% of max heat when collecting nugget (minimum 1)
- Instant fill to maximum when completing gold sequences

**Losing Heat:**
- Resets to 0 when:
  - Typing incorrect character
  - Typing any red character (correct or not)
  - Using arrow keys
  - Using `x` or delete motions on Green/Blue sequences
  - Using `h`/`j`/`k`/`l` more than 3 times consecutively

**Heat Effects:**
- **Scoring**: Heat directly multiplies your score per character
  - Example: At heat 50, typing a Bright Green character = 50 × 3 = 150 points
- **Decay Speed**: Higher heat means faster decay (more pressure)
  - 0% heat: 60 seconds between decays
  - 50% heat: 35 seconds between decays
  - 100% heat: 10 seconds between decays
- **Boost Activation**: Must reach maximum heat to activate boost

**Maximum Heat:**
Heat caps at screen width (typically 80-200 depending on terminal size). The visual heat bar displays 10 segments regardless of actual heat value, with each segment representing 10% of maximum heat.

## Boost System

**What is Boost?**
A 2× multiplier for heat gain that activates when you reach maximum heat and type matching color sequences.

**Activation:**
1. Fill heat bar to maximum (type Green/Blue characters correctly)
2. Boost activates automatically with 0.5 second timer
3. Boost is tied to the color of the LAST character that maxed out heat

**Color Matching Mechanic:**
- Typing the **same color** (Blue or Green) extends boost by 0.5 seconds per character
- Typing a **different color** deactivates boost immediately
- Example: If Blue triggered boost, keep typing Blue sequences to maintain it

**Effects:**
- +2 heat per character instead of +1
- Allows faster heat rebuilding after mistakes
- **Does NOT multiply score** - only affects heat gain

**Deactivation:**
- Timer expires (no same-color typing within 0.5 seconds)
- Typing incorrect character
- Typing red character
- Using arrow keys
- Typing different color at max heat

**Visual Indicator:**
Pink background showing "Boost: X.Xs" in bottom-right status bar.

**Strategy:**
- Commit to one color (Blue or Green) when boost activates
- Scan ahead for same-color sequences to maintain boost
- Boost is most valuable when recovering from heat loss

## Decay System

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
- **Nuggets destroyed on contact** (triggers 5-second respawn)

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
- Gold sequences are **immune** to decay
- **Nuggets are destroyed on contact** (not decayed, immediately removed)

**Strategy Implications:**
- Bright sequences are time-sensitive (2 decays until color change)
- Dark sequences are close to becoming different colors
- Green (Dark) is ONE decay away from becoming Red
- Red (Dark) will disappear next decay (can wait it out)
- Nuggets at risk during decay animation (use Tab jump to collect quickly)

## Spawn System

**Content Source:**
New sequences are generated from Go source code in `assets/data.txt`.

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

## Scoring System

### Score Formula

**Points = Heat × Level Multiplier × Type Modifier**

**Level Multipliers:**
- Bright sequences: ×3
- Normal sequences: ×2
- Dark sequences: ×1

**Type Modifiers:**
- Green/Blue sequences: ×1 (positive)
- Red sequences: ×1 (but negative!)
- Gold sequences: No score during typing
- Nuggets: No score impact

### Example Calculations

**Example 1**: Heat at 50, typing Bright Green character
- Score = 50 × 3 × 1 = **+150 points**

**Example 2**: Heat at 30, typing Normal Blue character
- Score = 30 × 2 × 1 = **+60 points**

**Example 3**: Heat at 80, typing Dark Red character (penalty!)
- Score = 80 × 1 × (-1) = **-80 points**
- Plus heat resets to 0!

**Example 4**: Heat at 100 (max), boost active, typing Bright Blue
- Heat gain = +2 (boost multiplier)
- Score = 100 × 3 × 1 = **+300 points**
- New heat = 102, boost extends +0.5s

**Example 5**: Collecting nugget at 80-char screen
- Heat gain = 80 / 10 = **+8 heat**
- Score impact = **0 points**

### Scoring Strategy

**Maximize Score:**
1. Build heat as high as possible before typing bright sequences
2. Type Bright sequences (×3) > Normal (×2) > Dark (×1)
3. Use boost to quickly rebuild heat after mistakes
4. Avoid red sequences (negative scoring + heat reset)
5. Collect nuggets for free heat without score penalty

**Score vs. Heat Trade-off:**
- Low heat + Bright sequence = Modest score, +1 heat
- High heat + Dark sequence = Good score, +1 heat
- **Optimal**: High heat + Bright sequence = Maximum score!

## Cleaners (Advanced Mechanic)

- **Trigger**: Automatically activated when you complete a gold sequence while heat meter is already at maximum
- **Visual**: Bright yellow blocks that sweep horizontally across the screen
- **Behavior**: Cleaners scan for and automatically destroy Red characters on contact
- **Pattern**: Alternating sweep direction (odd rows left-to-right, even rows right-to-left)
- **Duration**: Configurable animation duration (default: 1 second from spawn to completion)
- **Selectivity**: **Only removes Red characters** - Blue and Green sequences are completely safe
- **Effect**: Provides instant relief when overwhelmed by Red penalty sequences
- **Strategic Use**:
  - Complete gold sequences at max heat to clear accumulated Red characters
  - Allows aggressive high-heat play without Red penalty accumulation
  - Most effective when multiple Red sequences have decayed across different rows
- **Animation**: Configurable frame rate (default: 60 FPS) with trailing fade effect for visual clarity
- **Trail Effect**: Configurable trail length (default: 10 positions) with gradient fade (bright yellow → transparent)
- **Removal Flash**: Configurable flash duration (default: 150ms) appears when Red characters are destroyed

## Drain (Pressure Mechanic)

**What is Drain?**
A hostile entity that spawns when you have positive score, constantly pursues the cursor, and drains score over time when positioned on top of it.

**Visual Appearance:**
- **Character**: ╬ (cross character, Unicode U+256C)
- **Color**: Light Cyan (RGB: 224, 255, 255)
- **Background**: Transparent - inherits background from underlying layers (grid, cleaner trails, cursor, etc.)
- **Cursor Overlap**: When on cursor, appears as cyan character on orange background (NORMAL mode) or white background (INSERT mode)

**Spawn & Despawn:**
- **Spawn Trigger**: Appears when score becomes positive (score > 0)
- **Spawn Location**: Centered on cursor position at time of spawn
- **Despawn Trigger**: Disappears when score reaches zero or becomes negative (score ≤ 0)
- **Lifecycle**: Only one drain entity exists at a time

**Movement Behavior:**
- **Speed**: Moves 1 character every 1 second (1000ms intervals)
- **Pathfinding**: Uses 8-directional movement (horizontal, vertical, and diagonal)
- **Direction Calculation**: Recalculates shortest path to cursor position right before each move
- **Target**: Always moves toward current cursor position
- **Cannot be escaped**: Drain continuously tracks and chases the cursor

**Score Drain:**
- **Drain Rate**: 10 points per second (1000ms intervals)
- **Activation**: Only drains when drain entity is positioned exactly on cursor
- **Effect**: Continuously reduces score while on cursor
- **Consequence**: If score reaches zero, drain despawns

**Collision Interactions:**
Drain destroys any entity it collides with during movement or at spawn:

- **Blue/Green/Red Sequences**: Entire character disappears on contact, decrements color counters
- **Gold Sequences**: All characters in the gold sequence are destroyed, triggers phase transition to `PhaseGoldComplete` (same as completing the gold sequence)
- **Nuggets**: Destroyed immediately on collision
- **Falling Decay Entities**: Destroyed on collision during decay animation
- **Cleaners**: No interaction (different rendering layers)

**Strategic Implications:**
- **Constant Pressure**: Drain adds urgency to gameplay at positive scores
- **Risk vs. Reward**: Higher scores mean longer drain lifetime and more score loss potential
- **Movement Tax**: Players must continuously move or accept score drain
- **Collision Threat**: Valuable targets (Gold, Nuggets, bright sequences) can be destroyed by drain's path
- **Score Management**: Letting score drop to zero removes drain but resets progress
- **Gold Interaction**: Drain hitting gold sequences triggers completion - can be strategic or detrimental

**Rendering Order:**
Drain is rendered after removal flashes but before the cursor, ensuring it's visible but cursor takes priority for visual clarity.

## Content Management

### Content Source
- **Location**: `assets/` directory at project root
- **Format**: `.txt` files containing Go source code
- **Auto-discovery**: Scans all valid `.txt` files at initialization
- **Processing**: Lines trimmed, empty lines and comments ignored

### Block Selection
- **Size**: Random 3-15 consecutive lines per spawn
- **Grouping**: Lines grouped by indent level and structure
- **Validation**: Files must have at least 10 valid lines after processing
- **Caching**: Pre-validated content cached for performance

---

[← Back to Game Index](game-index.md)

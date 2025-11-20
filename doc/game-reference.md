# Quick Reference

## Essential Commands

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
| `Tab` | Jump to nugget (costs 10 score) |
| `Enter` | Activate ping grid (1s) |

## Sequence Types (Descending Priority)

1. **Gold** (timeout in <3s or heat <30)
2. **Nugget** (orange '●') before decay reaches it
3. **Bright** Green/Blue (heat >70)
4. **Green (Dark)** near decay timer
5. **Normal** Green/Blue
6. **Dark** Green/Blue
7. **Red** (only if score <1000 or blocking)

## Heat Guidelines

| Heat % | Decay Timer | Risk Level | Strategy |
|--------|-------------|------------|----------|
| 0-30% | 60-45s | Low | Build safely, learn motions, collect nuggets |
| 30-60% | 45-25s | Medium | Balanced play, steady scoring |
| 60-90% | 25-12s | High | High scoring, boost focus |
| 90-100% | 12-10s | Extreme | Boost required, survival mode |

## Level Multipliers

- **Bright sequences**: ×3 (highest value)
- **Normal sequences**: ×2
- **Dark sequences**: ×1 (lowest value)

## Heat Gain/Loss

**Heat Gain:**
- +1 per correct Green/Blue character (normal)
- +2 per correct Green/Blue character (boost active)
- +10% max heat per nugget collected (minimum 1)
- Fill to max when completing gold sequence

**Heat Loss (Resets to 0):**
- Typing incorrect character
- Typing red character
- Using arrow keys
- Using `h`/`j`/`k`/`l` 4+ times consecutively
- Deleting Green/Blue sequences with `x`/`dd`

## Boost System

**Activation:**
- Reach max heat
- Tied to last color typed (Blue or Green)

**Extension:**
- Type same color: +0.5s per character
- Type different color: Deactivates immediately

**Effect:**
- 2× heat gain multiplier (+2 instead of +1)
- Does NOT multiply score

## Nugget System

**Appearance:** Orange '●' (filled circle)

**Collection:**
- Type any alphanumeric character on nugget position
- Reward: +10% max heat (minimum 1)
- No score impact

**Tab Jump:**
- Press `Tab` to jump cursor to nugget
- Cost: 10 score (requires score >= 10)
- Useful when decay approaching or screen crowded

**Respawn:** 5 seconds after collection or decay destruction

**Invariant:** Only one nugget active at any time

## Decay System

**Timing:**
- 60 seconds at 0% heat
- 35 seconds at 50% heat
- 10 seconds at 100% heat

**Effects:**
- Brightness decay: Bright → Normal → Dark
- Color decay chain:
  - Blue (Dark) → Green (Bright)
  - Green (Dark) → Red (Bright)
  - Red (Dark) → Destroyed
- **Nuggets destroyed on contact**

## Cleaners

**Trigger:** Complete gold sequence while heat already at max

**Behavior:**
- Bright yellow blocks sweep horizontally
- Destroy Red characters only (Blue/Green safe)
- Alternating direction (L→R odd rows, R→L even rows)
- Duration: ~1 second

**Strategy:** Allows aggressive high-heat play without Red accumulation

## Scoring Formula

**Points = Heat × Level Multiplier × Type Modifier**

**Examples:**
- Heat 50, Bright Green: 50 × 3 = **+150**
- Heat 30, Normal Blue: 30 × 2 = **+60**
- Heat 80, Dark Red: 80 × 1 × (-1) = **-80** (and heat reset!)
- Heat 100, Bright Blue (boost): 100 × 3 = **+300** (heat +2)

## Screen Elements

**Top Bar:** 10-segment heat bar (0-100% in 10% increments)

**Left Margin:** Relative line numbers (distance from cursor row)

**Bottom Bar (Right to Left):**
- Score (white background)
- Decay timer (red background)
- Boost timer (pink background, when active)
- Grid timer (white text, when active)

## Motion Efficiency Tips

**Avoid Heat Reset:**
- Use `4l` instead of `l` `l` `l` `l`
- Use `w`/`b`/`e` instead of repeated `h`/`l`
- Use count prefixes: `5j`, `3w`, `2fa`

**Quick Navigation:**
- `gg` - Top-left corner
- `G` - Bottom of screen
- `0` - Start of line
- `$` - End of line
- `/<pattern>` - Search and jump
- `Tab` - Jump to nugget

## Visual Feedback

**Cursor Colors:**
- Orange: NORMAL mode
- White: INSERT mode
- Red flash: Incorrect typing

**Score Display:**
- White background: Default
- Character color flash: Correct typing (200ms)
- Black bg + red text: Error (200ms)

**Nugget Contrast:**
- Orange foreground: Normal (not at cursor)
- Dark brown foreground: At cursor (better contrast)

## Common Mistakes to Avoid

1. ❌ Using `h`/`j`/`k`/`l` 4+ times consecutively
2. ❌ Typing Dark sequences at low heat
3. ❌ Switching boost color (deactivates boost)
4. ❌ Deleting Green/Blue with `x`/`dd` (resets heat)
5. ❌ Ignoring nuggets (free heat)
6. ❌ Letting Green (Dark) decay to Red
7. ❌ Tab jumping when score < 10 (no effect)

## Optimal Plays

1. ✅ High heat + Bright sequences = Maximum score
2. ✅ Same color typing during boost = Extended boost
3. ✅ Gold sequence completion = Instant max heat
4. ✅ Nugget collection = Consistent heat maintenance
5. ✅ Tab jump before decay = Save nugget from destruction
6. ✅ Red (Dark) sequences = Wait for decay to destroy
7. ✅ Word motions (`w`/`b`/`e`) = Efficient navigation

---

[← Back to Game Index](game-index.md)

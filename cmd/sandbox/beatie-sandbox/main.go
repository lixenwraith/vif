package main

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// EnemyTemplate holds the structural DNA of a specific text-based horror.
type EnemyTemplate struct {
	Width, Height int
	Color         color.RGB
	BgColor       color.RGB // Per-species background aura/glow
	Frames        [][]string
}

// Enemy represents a spawned instance of a template.
type Enemy struct {
	X, Y       int
	Template   *EnemyTemplate
	AnimOffset int // Offsets animation so they don't all tick perfectly in sync
}

// 25 Unique Styles (Sizes: 2x2, 3x2, 4x2, 5x3, 6x3)
// Note: Backslashes (\) are escaped as \\ in Go strings.
var bestiary = []EnemyTemplate{

	// ========================================================================
	// SWARM — 2x2, fast cheap fodder
	// ========================================================================
	{2, 2, color.Red, color.BlackRed, [][]string{
		{`/\`, `\/`},
		{`||`, `||`},
	}},
	{2, 2, color.VibrantCyan, color.Black, [][]string{
		{`><`, `""`},
		{`><`, `^^`},
	}},
	{2, 2, color.Lime, color.BlackGreen, [][]string{
		{`##`, `/\`},
		{`##`, `--`},
		{`##`, `\/`},
	}},
	{2, 2, color.ElectricViolet, color.DeepPurple, [][]string{
		{`\\`, `//`},
		{`//`, `\\`},
	}},
	{2, 2, color.Amber, color.DarkAmber, [][]string{
		{`00`, `/\`},
		{`00`, `\/`},
	}},
	// Firefly — blinks
	{2, 2, color.LemonYellow, color.Black, [][]string{
		{`**`, `  `},
		{`  `, `**`},
		{`**`, `**`},
	}},
	// Mite — twitchy
	{2, 2, color.Rust, color.Black, [][]string{
		{`..`, `vv`},
		{`::`, `^^`},
		{`..`, `^^`},
	}},

	// ========================================================================
	// SCOUTS — 3x2, fast flankers
	// ========================================================================
	{3, 2, color.FlameOrange, color.Black, [][]string{
		{`<0>`, `/ \`},
		{`<0>`, `\ /`},
	}},
	{3, 2, color.SkyTeal, color.Black, [][]string{
		{`[+]`, `v v`},
		{`[+]`, `^ ^`},
	}},
	{3, 2, color.HotPink, color.DarkPlum, [][]string{
		{`\ /`, `[=]`},
		{`/ \`, `[=]`},
	}},
	{3, 2, color.MintGreen, color.Black, [][]string{
		{`===`, `> <`},
		{`===`, `< >`},
	}},
	{3, 2, color.Gold, color.DarkAmber, [][]string{
		{`>-<`, `/ \`},
		{`>-<`, `- -`},
	}},
	// Bat — flapping
	{3, 2, color.DarkViolet, color.Black, [][]string{
		{`\./`, ` v `},
		{`/.\`, ` ^ `},
		{`-.-`, ` | `},
	}},
	// Spark — electric jitter
	{3, 2, color.BrightCyan, color.DeepNavy, [][]string{
		{`~*~`, ` | `},
		{`*~*`, ` | `},
		{`~*~`, ` ! `},
	}},

	// ========================================================================
	// SOLDIERS — 4x2, standard creeps
	// ========================================================================
	{4, 2, color.NeonGreen, color.Black, [][]string{
		{`OOOO`, `/\/\`},
		{`OOOO`, `\/\/`},
	}},
	{4, 2, color.Coral, color.Black, [][]string{
		{`\//\`, ` || `},
		{`//\\`, ` || `},
	}},
	{4, 2, color.CobaltBlue, color.DeepNavy, [][]string{
		{`[##]`, `|  |`},
		{`[##]`, `/  \`},
	}},
	{4, 2, color.RoseRed, color.BlackRed, [][]string{
		{`~-~-`, `v v `},
		{`-~-~`, ` v v`},
	}},
	{4, 2, color.Silver, color.Black, [][]string{
		{`{oo}`, `/\/\`},
		{`[oo]`, `\/\/`},
	}},
	// Crab — side-scuttle
	{4, 2, color.Terracotta, color.Black, [][]string{
		{`>oo<`, `/  \`},
		{`>oo<`, `\  /`},
		{` oo `, `>  <`},
	}},
	// Shield Bearer — heavy step
	{4, 2, color.SteelBlue, color.BlueCharcoal, [][]string{
		{`[==]`, `|/\|`},
		{`[==]`, `|\\/`},
	}},
	// Toxic — pulsing poison
	{4, 2, color.YellowGreen, color.BlackGreen, [][]string{
		{`{~~}`, ` :: `},
		{`(~~)`, ` ;; `},
		{`{~~}`, ` :: `},
		{`<~~>`, ` .. `},
	}},

	// ========================================================================
	// ELITES — 5x3, dangerous mid-tier
	// ========================================================================
	{5, 3, color.Orchid, color.DarkPlum, [][]string{
		{`/\_/\`, `[( )]`, ` / \ `},
		{`\_/_/`, `[( )]`, ` \ / `},
	}},
	{5, 3, color.FlameOrange, color.DarkAmber, [][]string{
		{`[===]`, `<|#|>`, ` / \ `},
		{`[===]`, `>|#|<`, ` \ / `},
	}},
	{5, 3, color.Gold, color.DarkAmber, [][]string{
		{` \ / `, `>|=| `, ` / \ `},
		{` / \ `, `>|=| `, ` \ / `},
	}},
	{5, 3, color.BrightRed, color.BlackRed, [][]string{
		{`_/#\_`, `\[X]/`, ` / \ `},
		{`-\#/-`, `/[X]\`, ` | | `},
	}},
	{5, 3, color.VibrantCyan, color.Black, [][]string{
		{` /-\ `, ` ||| `, ` >-< `},
		{` \-/ `, ` ||| `, ` <-> `},
	}},
	// Wraith — phasing flicker
	{5, 3, color.MutedPurple, color.Obsidian, [][]string{
		{` .M. `, `(   )`, ` |~| `},
		{` :M: `, `{   }`, ` |~| `},
		{` 'M' `, `[   ]`, ` |_| `},
	}},
	// Scorpion — tail strike
	{5, 3, color.WarmOrange, color.Black, [][]string{
		{`  /| `, `(oo) `, `/||\\`},
		{` /|  `, ` (oo)`, `/||\\`},
		{`/|   `, `(oo) `, `/||\\`},
	}},
	// Djinn — swirling
	{5, 3, color.SoftLavender, color.DeepPurple, [][]string{
		{` ~~~ `, `( @ )`, ` ))) `},
		{` ~~~ `, `( @ )`, `((( `},
		{` ~~~ `, `( @ )`, ` ||| `},
	}},
	// Beetle — armored march
	{5, 3, color.Bronze, color.DarkAmber, [][]string{
		{`/===\`, `|ooo|`, `\/ \/`},
		{`/===\`, `|ooo|`, `/\ /\`},
	}},

	// ========================================================================
	// HEAVIES — 6x3, tanky
	// ========================================================================
	{6, 3, color.Magenta, color.DarkPlum, [][]string{
		{`\ // /`, `[####]`, `/ \\ \`},
		{`/ \\ \`, `[####]`, `\ // /`},
	}},
	{6, 3, color.Cyan, color.Black, [][]string{
		{` <><> `, `(====)`, ` /\/\ `},
		{` ><>< `, `(====)`, ` \/\/ `},
	}},
	{6, 3, color.LightGreen, color.BlackGreen, [][]string{
		{` /--\ `, `/|  |\`, `\    /`},
		{` |--| `, `\|  |/`, `/    \`},
	}},
	{6, 3, color.Rust, color.Black, [][]string{
		{`^^^^^^`, `[MMMM]`, ` /  \ `},
		{`^^^^^^`, `[MMMM]`, ` \  / `},
	}},
	{6, 3, color.DodgerBlue, color.DeepNavy, [][]string{
		{`_/__\_`, `\/  \/`, ` /  \ `},
		{`_\__/_`, `/\  /\`, ` \  / `},
	}},
	// Golem — lumbering stone
	{6, 3, color.Taupe, color.DarkSlate, [][]string{
		{`[####]`, `|<  >|`, ` |  | `},
		{`[####]`, `|>  <|`, ` /  \ `},
		{`[####]`, `|<  >|`, ` \  / `},
	}},
	// Hive Carrier — spawns swarm
	{6, 3, color.OliveYellow, color.DarkAmber, [][]string{
		{`/~~~~\`, `|*||*|`, `\____/`},
		{`/~~~~\`, `|+||+|`, `\____/`},
		{`/~~~~\`, `|*||*|`, `\_/\_/`},
	}},
	// Reaver — blade arms
	{6, 3, color.Vermilion, color.BlackRed, [][]string{
		{`\    /`, `-[XX]-`, `/    \`},
		{` \  / `, `-[XX]-`, ` /  \ `},
		{`\    /`, `=[XX]=`, `/    \`},
	}},

	// ========================================================================
	// CHAMPIONS — 8x4, mini-boss creeps
	// ========================================================================
	{8, 4, color.BrightRed, color.Obsidian, [][]string{
		{` /\  /\ `, `( @  @ )`, ` \<XX>/ `, `  /  \  `},
		{` /\  /\ `, `( @  @ )`, ` /<XX>\ `, `  \  /  `},
		{` /\  /\ `, `( @  @ )`, ` |<XX>| `, `  |  |  `},
	}},
	// War Machine — treaded
	{8, 4, color.IronGray, color.DarkSlate, [][]string{
		{` [====] `, ` |<HH>| `, `=|    |=`, `{OOOOOO}`},
		{` [====] `, ` |<HH>| `, `=|    |=`, `{OOOOOO}`},
	}},
	// Hydra — writhing heads
	{8, 4, color.SeaGreen, color.BlackGreen, [][]string{
		{`  /  \  `, ` /    \ `, `< @  @ >`, ` \\||// `},
		{` /    \ `, `/      \`, `< @  @ >`, `  \\//  `},
		{`  /  \  `, ` / \/ \ `, `< @  @ >`, ` //||\\ `},
	}},
	// Floating Eye — pulsing iris
	{8, 4, color.DodgerBlue, color.DeepNavy, [][]string{
		{` /----\ `, `| (00) |`, `| \--/ |`, ` \----/ `},
		{` /----\ `, `|  (0) |`, `|  --  |`, ` \----/ `},
		{` /----\ `, `| (00) |`, `| /--\ |`, ` \----/ `},
	}},
	// Inferno Elemental — flame dance
	{8, 4, color.FlameOrange, color.DarkRust, [][]string{
		{` ,  /\  `, `/ \/ /\ `, `\ /\/ /\`, ` \/  \/ `},
		{`  /\  , `, `/\ \/ / `, `/\/ /\ \`, ` \/  \/ `},
		{` /  \   `, `/ /\ /\ `, `\/\/ \/ `, ` /\  /\ `},
		{`   /\ / `, ` /\/ /\ `, `/ /\ \/ `, ` \/  \/ `},
	}},

	// ========================================================================
	// BOSSES — 10x5, wave-ending threats
	// ========================================================================
	// Demon Lord
	{10, 5, color.Vermilion, color.BlackRed, [][]string{
		{` /\\    /\\ `, ` \  \\//  / `, `  | >..< |  `, `  | \\// |  `, `  /||  ||\\ `},
		{` /\\    /\\ `, ` \\  \\/  / `, `  | >..< |  `, `  | //\\ |  `, `  \\||  ||/ `},
	}},
	// Siege Titan
	{10, 5, color.CoolSilver, color.DarkSlate, [][]string{
		{`  [======]  `, `  |<IIII>|  `, ` /|      |\\ `, `/ |  {}  | \\`, `{OO}    {OO}`},
		{`  [======]  `, `  |<IIII>|  `, ` /|      |\\ `, `\\ |  {}  | /`, `{OO}    {OO}`},
		{`  [======]  `, `  |>IIII<|  `, `  |      |  `, `  | ={} = |  `, `{OO}    {OO}`},
	}},
	// Lich — necromantic pulse
	{10, 5, color.PaleLavender, color.Obsidian, [][]string{
		{`   /==\\   `, `  / oo \\  `, `  | -- |   `, ` /|    |\\  `, `~ \\~~~~/ ~ `},
		{`   /==\\   `, `  / ** \\  `, `  | -- |   `, ` ~|    |~  `, `  \\~~~~/ ~ `},
		{`   /==\\   `, `  / oo \\  `, `  | ~~ |   `, ` /|    |\\  `, `~ /~~~~\\ ~ `},
	}},
	// Kraken — tentacle thrash
	{10, 5, color.Teal, color.DeepNavy, [][]string{
		{`   /__\\   `, `  / @@ \\  `, ` /|    |\\  `, `/ \\~~~~/ \\`, `~  ~~~~  ~ `},
		{`   /--\\   `, `  / @@ \\  `, `  |    |   `, ` \\/ ~~ \\/  `, `~~ ~~~~  ~~`},
		{`   /__\\   `, `  / @@ \\  `, ` \\|    |/ `, `  \\ ~~ /   `, ` ~~ ~~~~ ~ `},
	}},
}

var enemies []Enemy

func main() {
	term := terminal.New()
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()

	// Initial layout setup
	w, h := term.Size()
	layoutEnemies(w, h)

	// Update ticker: 300ms is a nice scurrying speed for bugs
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	// Background goroutine pushing tick events to terminal's event loop
	go func() {
		for range ticker.C {
			term.PostEvent(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone})
		}
	}()

	tickCount := 0
	render(term, tickCount)

	// Main event loop
	for {
		ev := term.PollEvent()
		switch ev.Type {
		case terminal.EventClosed, terminal.EventError:
			return
		case terminal.EventKey:
			// Quit on Escape, Ctrl+C, or 'q'
			if ev.Key == terminal.KeyEscape || ev.Key == terminal.KeyCtrlC || ev.Rune == 'q' || ev.Rune == 'Q' {
				return
			}
			// Animation tick
			if ev.Key == terminal.KeyNone {
				tickCount++
				render(term, tickCount)
			}
		case terminal.EventResize:
			// Recalculate layout based on new window size
			layoutEnemies(ev.Width, ev.Height)
			render(term, tickCount)
		}
	}
}

// layoutEnemies dynamically maps enemy positions across the screen like a word-wrap algorithm
func layoutEnemies(w, h int) {
	enemies = nil
	startX, startY := 4, 3 // Start with margins (Y=3 avoids the title)
	currX, currY := startX, startY
	lineHeight := 0

	for i := range bestiary {
		t := &bestiary[i]

		// Wrap to next row if it overflows width
		if currX+t.Width+8 > w {
			currX = startX
			currY += lineHeight + 3 // Vertical gap between rows
			lineHeight = 0
		}

		enemies = append(enemies, Enemy{
			X:          currX,
			Y:          currY,
			Template:   t,
			AnimOffset: i % 2, // Desynchronizes the leg movements slightly
		})

		currX += t.Width + 8 // Horizontal spacing between enemies

		if t.Height > lineHeight {
			lineHeight = t.Height
		}
	}
}

// render draws all enemies and titles to the terminal double-buffer
func render(term terminal.Terminal, tick int) {
	w, h := term.Size()
	if w <= 0 || h <= 0 {
		return
	}

	// Create blank frame filled with empty cells
	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Bg: color.Black}
	}

	// Draw Title
	title := " TERMINAL BESTIARY: TEXT-BASED HORRORS "
	titleX := (w - len(title)) / 2
	if titleX < 0 {
		titleX = 0
	}
	drawText(cells, w, h, titleX, 1, title, color.White, terminal.AttrBold)

	// Draw Footer
	footer := " Press ESC or Q to quit "
	footX := (w - len(footer)) / 2
	if footX < 0 {
		footX = 0
	}
	drawText(cells, w, h, footX, h-2, footer, color.DimGray, terminal.AttrNone)

	// Draw Entities
	for _, e := range enemies {
		frameIdx := (tick + e.AnimOffset) % len(e.Template.Frames)
		frameLines := e.Template.Frames[frameIdx]

		for y, line := range frameLines {
			for x, char := range line {
				screenX := e.X + x
				screenY := e.Y + y
				if screenX < 0 || screenX >= w || screenY < 0 || screenY >= h {
					continue
				}
				idx := screenY*w + screenX
				bg := e.Template.BgColor
				if char == ' ' {
					// Transparent foreground, but still paint bg aura if non-black
					if bg == color.Black {
						continue
					}
					cells[idx] = terminal.Cell{Rune: ' ', Bg: bg}
				} else {
					cells[idx] = terminal.Cell{
						Rune:  char,
						Fg:    e.Template.Color,
						Bg:    bg,
						Attrs: terminal.AttrBold,
					}
				}
			}
		}
	}

	// Dispatch flush to underlying terminal buffer logic
	term.Flush(cells, w, h)
}

// drawText is a quick utility to embed horizontal strings in the cell buffer
func drawText(cells []terminal.Cell, w, h, x, y int, text string, fg color.RGB, attr terminal.Attr) {
	if y < 0 || y >= h {
		return
	}
	for i, r := range text {
		sx := x + i
		if sx >= 0 && sx < w {
			cells[y*w+sx] = terminal.Cell{
				Rune:  r,
				Fg:    fg,
				Bg:    color.Black,
				Attrs: attr,
			}
		}
	}
}

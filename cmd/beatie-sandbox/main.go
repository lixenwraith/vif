package main

import (
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// EnemyTemplate holds the structural DNA of a specific text-based horror.
type EnemyTemplate struct {
	Width, Height int
	Color         terminal.RGB
	BgColor       terminal.RGB // Per-species background aura/glow
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
	{2, 2, terminal.Red, terminal.BlackRed, [][]string{
		{`/\`, `\/`},
		{`||`, `||`},
	}},
	{2, 2, terminal.VibrantCyan, terminal.Black, [][]string{
		{`><`, `""`},
		{`><`, `^^`},
	}},
	{2, 2, terminal.Lime, terminal.BlackGreen, [][]string{
		{`##`, `/\`},
		{`##`, `--`},
		{`##`, `\/`},
	}},
	{2, 2, terminal.ElectricViolet, terminal.DeepPurple, [][]string{
		{`\\`, `//`},
		{`//`, `\\`},
	}},
	{2, 2, terminal.Amber, terminal.DarkAmber, [][]string{
		{`00`, `/\`},
		{`00`, `\/`},
	}},
	// Firefly — blinks
	{2, 2, terminal.LemonYellow, terminal.Black, [][]string{
		{`**`, `  `},
		{`  `, `**`},
		{`**`, `**`},
	}},
	// Mite — twitchy
	{2, 2, terminal.Rust, terminal.Black, [][]string{
		{`..`, `vv`},
		{`::`, `^^`},
		{`..`, `^^`},
	}},

	// ========================================================================
	// SCOUTS — 3x2, fast flankers
	// ========================================================================
	{3, 2, terminal.FlameOrange, terminal.Black, [][]string{
		{`<0>`, `/ \`},
		{`<0>`, `\ /`},
	}},
	{3, 2, terminal.SkyTeal, terminal.Black, [][]string{
		{`[+]`, `v v`},
		{`[+]`, `^ ^`},
	}},
	{3, 2, terminal.HotPink, terminal.DarkPlum, [][]string{
		{`\ /`, `[=]`},
		{`/ \`, `[=]`},
	}},
	{3, 2, terminal.MintGreen, terminal.Black, [][]string{
		{`===`, `> <`},
		{`===`, `< >`},
	}},
	{3, 2, terminal.Gold, terminal.DarkAmber, [][]string{
		{`>-<`, `/ \`},
		{`>-<`, `- -`},
	}},
	// Bat — flapping
	{3, 2, terminal.DarkViolet, terminal.Black, [][]string{
		{`\./`, ` v `},
		{`/.\`, ` ^ `},
		{`-.-`, ` | `},
	}},
	// Spark — electric jitter
	{3, 2, terminal.BrightCyan, terminal.DeepNavy, [][]string{
		{`~*~`, ` | `},
		{`*~*`, ` | `},
		{`~*~`, ` ! `},
	}},

	// ========================================================================
	// SOLDIERS — 4x2, standard creeps
	// ========================================================================
	{4, 2, terminal.NeonGreen, terminal.Black, [][]string{
		{`OOOO`, `/\/\`},
		{`OOOO`, `\/\/`},
	}},
	{4, 2, terminal.Coral, terminal.Black, [][]string{
		{`\//\`, ` || `},
		{`//\\`, ` || `},
	}},
	{4, 2, terminal.CobaltBlue, terminal.DeepNavy, [][]string{
		{`[##]`, `|  |`},
		{`[##]`, `/  \`},
	}},
	{4, 2, terminal.RoseRed, terminal.BlackRed, [][]string{
		{`~-~-`, `v v `},
		{`-~-~`, ` v v`},
	}},
	{4, 2, terminal.Silver, terminal.Black, [][]string{
		{`{oo}`, `/\/\`},
		{`[oo]`, `\/\/`},
	}},
	// Crab — side-scuttle
	{4, 2, terminal.Terracotta, terminal.Black, [][]string{
		{`>oo<`, `/  \`},
		{`>oo<`, `\  /`},
		{` oo `, `>  <`},
	}},
	// Shield Bearer — heavy step
	{4, 2, terminal.SteelBlue, terminal.BlueCharcoal, [][]string{
		{`[==]`, `|/\|`},
		{`[==]`, `|\\/`},
	}},
	// Toxic — pulsing poison
	{4, 2, terminal.YellowGreen, terminal.BlackGreen, [][]string{
		{`{~~}`, ` :: `},
		{`(~~)`, ` ;; `},
		{`{~~}`, ` :: `},
		{`<~~>`, ` .. `},
	}},

	// ========================================================================
	// ELITES — 5x3, dangerous mid-tier
	// ========================================================================
	{5, 3, terminal.Orchid, terminal.DarkPlum, [][]string{
		{`/\_/\`, `[( )]`, ` / \ `},
		{`\_/_/`, `[( )]`, ` \ / `},
	}},
	{5, 3, terminal.FlameOrange, terminal.DarkAmber, [][]string{
		{`[===]`, `<|#|>`, ` / \ `},
		{`[===]`, `>|#|<`, ` \ / `},
	}},
	{5, 3, terminal.Gold, terminal.DarkAmber, [][]string{
		{` \ / `, `>|=| `, ` / \ `},
		{` / \ `, `>|=| `, ` \ / `},
	}},
	{5, 3, terminal.BrightRed, terminal.BlackRed, [][]string{
		{`_/#\_`, `\[X]/`, ` / \ `},
		{`-\#/-`, `/[X]\`, ` | | `},
	}},
	{5, 3, terminal.VibrantCyan, terminal.Black, [][]string{
		{` /-\ `, ` ||| `, ` >-< `},
		{` \-/ `, ` ||| `, ` <-> `},
	}},
	// Wraith — phasing flicker
	{5, 3, terminal.MutedPurple, terminal.Obsidian, [][]string{
		{` .M. `, `(   )`, ` |~| `},
		{` :M: `, `{   }`, ` |~| `},
		{` 'M' `, `[   ]`, ` |_| `},
	}},
	// Scorpion — tail strike
	{5, 3, terminal.WarmOrange, terminal.Black, [][]string{
		{`  /| `, `(oo) `, `/||\\`},
		{` /|  `, ` (oo)`, `/||\\`},
		{`/|   `, `(oo) `, `/||\\`},
	}},
	// Djinn — swirling
	{5, 3, terminal.SoftLavender, terminal.DeepPurple, [][]string{
		{` ~~~ `, `( @ )`, ` ))) `},
		{` ~~~ `, `( @ )`, `((( `},
		{` ~~~ `, `( @ )`, ` ||| `},
	}},
	// Beetle — armored march
	{5, 3, terminal.Bronze, terminal.DarkAmber, [][]string{
		{`/===\`, `|ooo|`, `\/ \/`},
		{`/===\`, `|ooo|`, `/\ /\`},
	}},

	// ========================================================================
	// HEAVIES — 6x3, tanky
	// ========================================================================
	{6, 3, terminal.Magenta, terminal.DarkPlum, [][]string{
		{`\ // /`, `[####]`, `/ \\ \`},
		{`/ \\ \`, `[####]`, `\ // /`},
	}},
	{6, 3, terminal.Cyan, terminal.Black, [][]string{
		{` <><> `, `(====)`, ` /\/\ `},
		{` ><>< `, `(====)`, ` \/\/ `},
	}},
	{6, 3, terminal.LightGreen, terminal.BlackGreen, [][]string{
		{` /--\ `, `/|  |\`, `\    /`},
		{` |--| `, `\|  |/`, `/    \`},
	}},
	{6, 3, terminal.Rust, terminal.Black, [][]string{
		{`^^^^^^`, `[MMMM]`, ` /  \ `},
		{`^^^^^^`, `[MMMM]`, ` \  / `},
	}},
	{6, 3, terminal.DodgerBlue, terminal.DeepNavy, [][]string{
		{`_/__\_`, `\/  \/`, ` /  \ `},
		{`_\__/_`, `/\  /\`, ` \  / `},
	}},
	// Golem — lumbering stone
	{6, 3, terminal.Taupe, terminal.DarkSlate, [][]string{
		{`[####]`, `|<  >|`, ` |  | `},
		{`[####]`, `|>  <|`, ` /  \ `},
		{`[####]`, `|<  >|`, ` \  / `},
	}},
	// Hive Carrier — spawns swarm
	{6, 3, terminal.OliveYellow, terminal.DarkAmber, [][]string{
		{`/~~~~\`, `|*||*|`, `\____/`},
		{`/~~~~\`, `|+||+|`, `\____/`},
		{`/~~~~\`, `|*||*|`, `\_/\_/`},
	}},
	// Reaver — blade arms
	{6, 3, terminal.Vermilion, terminal.BlackRed, [][]string{
		{`\    /`, `-[XX]-`, `/    \`},
		{` \  / `, `-[XX]-`, ` /  \ `},
		{`\    /`, `=[XX]=`, `/    \`},
	}},

	// ========================================================================
	// CHAMPIONS — 8x4, mini-boss creeps
	// ========================================================================
	{8, 4, terminal.BrightRed, terminal.Obsidian, [][]string{
		{` /\  /\ `, `( @  @ )`, ` \<XX>/ `, `  /  \  `},
		{` /\  /\ `, `( @  @ )`, ` /<XX>\ `, `  \  /  `},
		{` /\  /\ `, `( @  @ )`, ` |<XX>| `, `  |  |  `},
	}},
	// War Machine — treaded
	{8, 4, terminal.IronGray, terminal.DarkSlate, [][]string{
		{` [====] `, ` |<HH>| `, `=|    |=`, `{OOOOOO}`},
		{` [====] `, ` |<HH>| `, `=|    |=`, `{OOOOOO}`},
	}},
	// Hydra — writhing heads
	{8, 4, terminal.SeaGreen, terminal.BlackGreen, [][]string{
		{`  /  \  `, ` /    \ `, `< @  @ >`, ` \\||// `},
		{` /    \ `, `/      \`, `< @  @ >`, `  \\//  `},
		{`  /  \  `, ` / \/ \ `, `< @  @ >`, ` //||\\ `},
	}},
	// Floating Eye — pulsing iris
	{8, 4, terminal.DodgerBlue, terminal.DeepNavy, [][]string{
		{` /----\ `, `| (00) |`, `| \--/ |`, ` \----/ `},
		{` /----\ `, `|  (0) |`, `|  --  |`, ` \----/ `},
		{` /----\ `, `| (00) |`, `| /--\ |`, ` \----/ `},
	}},
	// Inferno Elemental — flame dance
	{8, 4, terminal.FlameOrange, terminal.DarkRust, [][]string{
		{` ,  /\  `, `/ \/ /\ `, `\ /\/ /\`, ` \/  \/ `},
		{`  /\  , `, `/\ \/ / `, `/\/ /\ \`, ` \/  \/ `},
		{` /  \   `, `/ /\ /\ `, `\/\/ \/ `, ` /\  /\ `},
		{`   /\ / `, ` /\/ /\ `, `/ /\ \/ `, ` \/  \/ `},
	}},

	// ========================================================================
	// BOSSES — 10x5, wave-ending threats
	// ========================================================================
	// Demon Lord
	{10, 5, terminal.Vermilion, terminal.BlackRed, [][]string{
		{` /\\    /\\ `, ` \  \\//  / `, `  | >..< |  `, `  | \\// |  `, `  /||  ||\\ `},
		{` /\\    /\\ `, ` \\  \\/  / `, `  | >..< |  `, `  | //\\ |  `, `  \\||  ||/ `},
	}},
	// Siege Titan
	{10, 5, terminal.CoolSilver, terminal.DarkSlate, [][]string{
		{`  [======]  `, `  |<IIII>|  `, ` /|      |\\ `, `/ |  {}  | \\`, `{OO}    {OO}`},
		{`  [======]  `, `  |<IIII>|  `, ` /|      |\\ `, `\\ |  {}  | /`, `{OO}    {OO}`},
		{`  [======]  `, `  |>IIII<|  `, `  |      |  `, `  | ={} = |  `, `{OO}    {OO}`},
	}},
	// Lich — necromantic pulse
	{10, 5, terminal.PaleLavender, terminal.Obsidian, [][]string{
		{`   /==\\   `, `  / oo \\  `, `  | -- |   `, ` /|    |\\  `, `~ \\~~~~/ ~ `},
		{`   /==\\   `, `  / ** \\  `, `  | -- |   `, ` ~|    |~  `, `  \\~~~~/ ~ `},
		{`   /==\\   `, `  / oo \\  `, `  | ~~ |   `, ` /|    |\\  `, `~ /~~~~\\ ~ `},
	}},
	// Kraken — tentacle thrash
	{10, 5, terminal.Teal, terminal.DeepNavy, [][]string{
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
		cells[i] = terminal.Cell{Rune: ' ', Bg: terminal.Black}
	}

	// Draw Title
	title := " TERMINAL BESTIARY: TEXT-BASED HORRORS "
	titleX := (w - len(title)) / 2
	if titleX < 0 {
		titleX = 0
	}
	drawText(cells, w, h, titleX, 1, title, terminal.White, terminal.AttrBold)

	// Draw Footer
	footer := " Press ESC or Q to quit "
	footX := (w - len(footer)) / 2
	if footX < 0 {
		footX = 0
	}
	drawText(cells, w, h, footX, h-2, footer, terminal.DimGray, terminal.AttrNone)

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
					if bg == terminal.Black {
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
func drawText(cells []terminal.Cell, w, h, x, y int, text string, fg terminal.RGB, attr terminal.Attr) {
	if y < 0 || y >= h {
		return
	}
	for i, r := range text {
		sx := x + i
		if sx >= 0 && sx < w {
			cells[y*w+sx] = terminal.Cell{
				Rune:  r,
				Fg:    fg,
				Bg:    terminal.Black,
				Attrs: attr,
			}
		}
	}
}
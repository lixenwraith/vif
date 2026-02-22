package main

import (
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// EnemyTemplate holds the structural DNA of a specific text-based horror.
type EnemyTemplate struct {
	Width, Height int
	Color         terminal.RGB
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
	// --- Size: 2x2 ---
	{2, 2, terminal.Red, [][]string{
		{`/`, `\/`},  // Frame 1
		{`||`, `||`}, // Frame 2
	}},
	{2, 2, terminal.VibrantCyan, [][]string{
		{"><", `""`},
		{"><", `^^`},
	}},
	{2, 2, terminal.Lime, [][]string{
		{`##`, `/\`},
		{`##`, `--`},
	}},
	{2, 2, terminal.ElectricViolet, [][]string{
		{`\\`, `//`},
		{`//`, `\\`},
	}},
	{2, 2, terminal.Amber, [][]string{
		{`00`, `/\`},
		{`00`, `\/`},
	}},

	// --- Size: 3x2 ---
	{3, 2, terminal.FlameOrange, [][]string{
		{`<0>`, `/ \`},
		{`<0>`, `\ /`},
	}},
	{3, 2, terminal.SkyTeal, [][]string{
		{`[+]`, `v v`},
		{`[+]`, `^ ^`},
	}},
	{3, 2, terminal.HotPink, [][]string{
		{`\ /`, `[=]`},
		{`/ \`, `[=]`},
	}},
	{3, 2, terminal.MintGreen, [][]string{
		{`===`, `> <`},
		{`===`, `< >`},
	}},
	{3, 2, terminal.Gold, [][]string{
		{`>-<`, `/ \`},
		{`>-<`, `- -`},
	}},

	// --- Size: 4x2 ---
	{4, 2, terminal.NeonGreen, [][]string{
		{`OOOO`, `/\/\`},
		{`OOOO`, `\/\/`},
	}},
	{4, 2, terminal.Coral, [][]string{
		{`\//\`, ` || `},
		{`//\\`, ` || `},
	}},
	{4, 2, terminal.CobaltBlue, [][]string{
		{`[##]`, `|  |`},
		{`[##]`, `/  \`},
	}},
	{4, 2, terminal.RoseRed, [][]string{
		{`~-~-`, `v v `},
		{`-~-~`, ` v v`},
	}},
	{4, 2, terminal.Silver, [][]string{
		{`{oo}`, `/\/\`},
		{`[oo]`, `\/\/`},
	}},

	// --- Size: 5x3 ---
	{5, 3, terminal.Orchid, [][]string{
		{`/\_/\`, `[( )]`, ` / \ `},
		{`\_/_/`, `[( )]`, ` \ / `},
	}},
	{5, 3, terminal.FlameOrange, [][]string{
		{`[===]`, `<|#|>`, ` / \ `},
		{`[===]`, `>|#|<`, ` \ / `},
	}},
	{5, 3, terminal.Gold, [][]string{
		{` \ / `, `>|=| `, ` / \ `},
		{` / \ `, `>|=| `, ` \ / `},
	}},
	{5, 3, terminal.BrightRed, [][]string{
		{`_/#\_`, `\[X]/`, ` / \ `},
		{`-\#/-`, `/[X]\`, ` | | `},
	}},
	{5, 3, terminal.VibrantCyan, [][]string{
		{` /-\ `, ` ||| `, ` >-< `},
		{` \-/ `, ` ||| `, ` <-> `},
	}},

	// --- Size: 6x3 ---
	{6, 3, terminal.Magenta, [][]string{
		{`\ // /`, `[####]`, `/ \\ \`},
		{`/ \\ \`, `[####]`, `\ // /`},
	}},
	{6, 3, terminal.Cyan, [][]string{
		{` <><> `, `(====)`, ` /\/\ `},
		{` ><>< `, `(====)`, ` \/\/ `},
	}},
	{6, 3, terminal.LightGreen, [][]string{
		{` /--\ `, `/|  |\`, `\    /`},
		{` |--| `, `\|  |/`, `/    \`},
	}},
	{6, 3, terminal.Rust, [][]string{
		{`^^^^^^`, `[MMMM]`, ` /  \ `},
		{`^^^^^^`, `[MMMM]`, ` \  / `},
	}},
	{6, 3, terminal.DodgerBlue, [][]string{
		{`_/__\_`, `\/  \/`, ` /  \ `},
		{`_\__/_`, `/\  /\`, ` \  / `},
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
				if char != ' ' { // space signifies transparency for the bugs
					screenX := e.X + x
					screenY := e.Y + y

					if screenX >= 0 && screenX < w && screenY >= 0 && screenY < h {
						idx := screenY*w + screenX
						cells[idx] = terminal.Cell{
							Rune:  char,
							Fg:    e.Template.Color,
							Bg:    terminal.Black,
							Attrs: terminal.AttrBold,
						}
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

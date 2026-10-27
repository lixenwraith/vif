// Command raider lays out numbered raider candidates for vanguard, each within the
// 5×3 a composite navigates, under one colour scheme a slider picks, on the game's
// truecolor tier or its console tier as a VGA or FreeBSD console shows it.
package main

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// A frame's fg grid names each glyph's part, which a scheme colours
const (
	partBody = iota
	partEyes
	partLimbs
	partAccent
	parts
)

// scheme colours the four parts, each in RGB and as the console entry a 256-colour
// host draws it in
type scheme struct {
	name    string
	rgb     [parts]color.RGB
	console [parts]uint8
}

type creature struct {
	name   string
	w, h   int
	ms     int    // per frame, the prototype's pattern_ms
	mixed  scheme // its own colours, the slider's last stop
	frames []visual.LookFrame
	look   component.Look
}

func rgb(r, g, b uint8) color.RGB { return color.RGB{R: r, G: g, B: b} }

// The slider's stops before Mixed: each colours every creature in one hue, its
// eyes the palest and its limbs the darkest
var schemes = []scheme{
	{"Purple", [parts]color.RGB{rgb(170, 90, 230), rgb(232, 205, 255), rgb(115, 60, 165), rgb(205, 145, 255)},
		[parts]uint8{visual.ConBrightMagenta, visual.ConBrightWhite, visual.ConMagenta, visual.ConBrightMagenta}},
	{"White", [parts]color.RGB{rgb(228, 228, 238), rgb(255, 255, 255), rgb(150, 150, 168), rgb(195, 195, 210)},
		[parts]uint8{visual.ConBrightWhite, visual.ConBrightWhite, visual.ConWhite, visual.ConWhite}},
	{"Orange", [parts]color.RGB{rgb(255, 140, 40), rgb(255, 215, 140), rgb(190, 90, 25), rgb(255, 100, 45)},
		[parts]uint8{visual.ConYellow, visual.ConBrightYellow, visual.ConYellow, visual.ConBrightRed}},
	{"Yellow", [parts]color.RGB{rgb(255, 225, 60), rgb(255, 255, 215), rgb(195, 160, 35), rgb(255, 240, 150)},
		[parts]uint8{visual.ConBrightYellow, visual.ConBrightWhite, visual.ConYellow, visual.ConBrightYellow}},
}

const mixedStop = "Mixed"

// The tiers a truecolor terminal can show; a 256-colour terminal shows only its own console
type tier struct {
	name    string
	console bool
	palette engine.ConsolePalette
}

var tiers = []tier{
	{"truecolor", false, engine.ConsolePalette{}},
	{"console VGA", true, render.VGAPalette},
	{"console FreeBSD", true, render.FreeBSDPalette},
}

// The states the game tints a raider in: calm, enraged through lock and charge, hit
const (
	stateCalm = iota
	stateEnraged
	stateHit
	states
)

var stateNames = [states]string{"calm", "enraged", "hit"}

const (
	cardW, cardH = 13, 5 // a label row, three body rows and a gap
	gridTop      = 3
	footerRows   = 4
	sliderX      = 10
	sliderStep   = 10
	backdropFill = 0.05 // the share of cells a drifting glyph holds
	driftCPS     = 10.0 // the backdrop's drift, the cave's at half its scroll
)

type glyph struct {
	x, y float64
	r    rune
	fg   color.RGB
}

type app struct {
	term     terminal.Terminal
	native   bool // the terminal draws 256 colours, so its own console is the only tier
	buf      *render.RenderBuffer
	w, h     int
	stop     int // the slider's position, len(schemes) for Mixed
	tier     int
	oneColor bool // the console draws a body's entry everywhere, as a look's fg256 pair does
	backdrop bool
	state    int
	scroll   int
	glyphs   []glyph
	start    time.Time
	dragging bool
}

func main() {
	for i := range bestiary {
		c := &bestiary[i]
		look, err := visual.CompileLook(c.w, c.h, visual.LookArt{Fg: c.mixed.rgb[:], Frames: c.frames})
		if err != nil {
			panic(fmt.Sprintf("%d %s: %v", i+1, c.name, err))
		}
		// Eyes draw bold, as the vanguard frames mark them
		for _, f := range look.Frames {
			for j := range f {
				if f[j].Fg == partEyes {
					f[j].Attr = terminal.AttrBold
				}
			}
		}
		c.look = look
	}

	term := terminal.New()
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()
	_ = term.SetMouseMode(terminal.MouseModeClick | terminal.MouseModeDrag)

	a := &app{term: term, native: term.ColorMode() != terminal.ColorModeTrueColor, backdrop: true, start: time.Now()}
	a.stop = len(schemes)
	a.resize(term.Size())

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			term.PostEvent(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone})
		}
	}()

	for {
		ev := term.PollEvent()
		switch ev.Type {
		case terminal.EventClosed, terminal.EventError:
			return
		case terminal.EventResize:
			a.resize(ev.Width, ev.Height)
		case terminal.EventMouse:
			a.mouse(ev)
		case terminal.EventKey:
			if !a.key(ev) {
				return
			}
		}
		a.render()
	}
}

func (a *app) resize(w, h int) {
	a.w, a.h = w, h
	mode := terminal.ColorModeTrueColor
	if a.native || tiers[a.tier].console {
		mode = terminal.ColorMode256
	}
	a.buf = render.NewRenderBuffer(mode, w, h)
	switch {
	case a.native:
		a.buf.SetConsole(render.ConsoleFor(render.DetectConsolePalette()))
	case tiers[a.tier].console:
		a.buf.SetConsole(render.ConsoleFor(tiers[a.tier].palette))
	}
	rng := rand.New(rand.NewPCG(1, 2))
	a.glyphs = a.glyphs[:0]
	for range int(float64(w*h) * backdropFill) {
		t, l := rng.IntN(3), rng.IntN(3) // green, blue and red, the types a game spawns most
		a.glyphs = append(a.glyphs, glyph{x: rng.Float64() * float64(w), y: float64(rng.IntN(h)),
			r: rune('a' + rng.IntN(26)), fg: visual.GlyphColorLUT[t][l]})
	}
	a.clampScroll()
}

func (a *app) key(ev terminal.Event) bool {
	switch {
	case ev.Key == terminal.KeyEscape || ev.Key == terminal.KeyCtrlC || ev.Rune == 'q':
		return false
	case ev.Key == terminal.KeyLeft || ev.Rune == 'h':
		a.stop = max(a.stop-1, 0)
	case ev.Key == terminal.KeyRight || ev.Rune == 'l':
		a.stop = min(a.stop+1, len(schemes))
	case ev.Key == terminal.KeyUp || ev.Rune == 'k':
		a.scroll--
	case ev.Key == terminal.KeyDown || ev.Rune == 'j':
		a.scroll++
	case ev.Key == terminal.KeyPageUp:
		a.scroll -= a.gridRows()
	case ev.Key == terminal.KeyPageDown:
		a.scroll += a.gridRows()
	case ev.Rune == 'c' && !a.native:
		a.tier = (a.tier + 1) % len(tiers)
		a.resize(a.w, a.h)
	case ev.Rune == 'o':
		a.oneColor = !a.oneColor
	case ev.Rune == 'b':
		a.backdrop = !a.backdrop
	case ev.Rune == 's':
		a.state = (a.state + 1) % states
	}
	a.clampScroll()
	return true
}

func (a *app) mouse(ev terminal.Event) {
	switch ev.MouseBtn {
	case terminal.MouseBtnWheelUp:
		a.scroll--
	case terminal.MouseBtnWheelDown:
		a.scroll++
	case terminal.MouseBtnLeft:
		track := a.h - footerRows
		if ev.MouseAction == terminal.MouseActionPress {
			a.dragging = ev.MouseY == track || ev.MouseY == track+1
		}
		if a.dragging && ev.MouseAction != terminal.MouseActionRelease {
			a.stop = min(max((ev.MouseX-sliderX+sliderStep/2)/sliderStep, 0), len(schemes))
		}
		if ev.MouseAction == terminal.MouseActionRelease {
			a.dragging = false
		}
	}
	a.clampScroll()
}

func (a *app) perRow() int { return max((a.w-2)/cardW, 1) }

// gridRows is the card rows the screen shows between the header and the footer
func (a *app) gridRows() int { return max((a.h-gridTop-footerRows)/cardH, 1) }

func (a *app) clampScroll() {
	rows := (len(bestiary) + a.perRow() - 1) / a.perRow()
	a.scroll = min(max(a.scroll, 0), max(rows-a.gridRows(), 0))
}

func (a *app) render() {
	b := a.buf
	b.Clear()
	elapsed := time.Since(a.start)
	if a.backdrop {
		a.drawBackdrop(elapsed)
	}

	b.SetWriteMask(visual.MaskComposite)
	per := a.perRow()
	for i := range bestiary {
		row := i/per - a.scroll
		if row < 0 || row >= a.gridRows() {
			continue
		}
		x, y := 1+(i%per)*cardW, gridTop+row*cardH
		c := &bestiary[i]
		a.text(x, y, fmt.Sprintf("%2d ", i+1), rgb(255, 255, 255), terminal.AttrBold)
		a.text(x+3, y, c.name, rgb(150, 150, 165), terminal.AttrNone)
		frame := c.look.Frames[int(elapsed.Milliseconds()/int64(c.ms))%len(c.look.Frames)]
		ox, oy := x+(cardW-1-c.w)/2, y+1+3-c.h // bottom-aligned, so legs line up
		for j := range frame {
			a.cell(ox+j%c.w, oy+j/c.w, &frame[j], a.schemeFor(c), elapsed)
		}
	}
	a.drawHeader()
	a.drawFooter()
	b.FlushToTerminal(surface{a})
}

func (a *app) schemeFor(c *creature) *scheme {
	if a.stop == len(schemes) {
		return &c.mixed
	}
	return &schemes[a.stop]
}

// cell paints one glyph as the game's look painter does: its part's colour, the
// enraged colour over every glyph, and a hit's flash, which a console shows reversed
func (a *app) cell(x, y int, cell *component.LookCell, sc *scheme, elapsed time.Duration) {
	if cell.Rune == 0 {
		return
	}
	part := max(int(cell.Fg), partBody)
	console := a.native || tiers[a.tier].console
	hit := a.state == stateHit && elapsed%(4*parameter.CombatHitFlashDuration) < parameter.CombatHitFlashDuration
	switch {
	case a.state == stateEnraged || (hit && !console):
		tint := visual.RgbCombatEnraged
		if hit {
			tint = color.Scale(visual.RgbCombatHitFlash, flashIntensity(elapsed%(4*parameter.CombatHitFlashDuration)))
		}
		a.buf.SetFgOnly(x, y, cell.Rune, tint, cell.Attr)
	case console:
		if a.oneColor {
			part = partBody
		}
		attr := terminal.AttrFg256 | cell.Attr
		if hit {
			attr |= terminal.AttrReverse
		}
		a.buf.SetFgOnly(x, y, cell.Rune, color.RGB{R: sc.console[part]}, attr)
	default:
		a.buf.SetFgOnly(x, y, cell.Rune, sc.rgb[part], cell.Attr)
	}
}

// flashIntensity steps a hit's flash as the game's does: dim, full, dim
func flashIntensity(t time.Duration) float64 {
	if p := float64(t) / float64(parameter.CombatHitFlashDuration); p > 0.33 && p <= 0.67 {
		return 1.0
	}
	return 0.6
}

func (a *app) drawBackdrop(elapsed time.Duration) {
	a.buf.SetWriteMask(visual.MaskGlyph)
	shift := elapsed.Seconds() * driftCPS
	for _, g := range a.glyphs {
		x := int(g.x-shift) % a.w
		if x < 0 {
			x += a.w
		}
		a.buf.SetFgOnly(x, int(g.y), g.r, g.fg, terminal.AttrNone)
	}
}

func (a *app) drawHeader() {
	a.buf.SetWriteMask(visual.MaskUI)
	a.clearRow(0)
	a.clearRow(1)
	a.text(1, 0, "RAIDER CANDIDATES", rgb(255, 255, 255), terminal.AttrBold)
	a.text(20, 0, "each within 5×3; numbers are what to pick by", rgb(150, 150, 165), terminal.AttrNone)
	help := "←/→ colour  c tier  o one colour  b backdrop  s state  ↑/↓ scroll  q quit"
	if a.native {
		help = "←/→ colour  o one colour  b backdrop  s state  ↑/↓ scroll  q quit"
	}
	a.text(1, 1, help, rgb(150, 150, 165), terminal.AttrNone)
}

// drawFooter draws the colour slider, a track with a stop per scheme and the knob
// on the chosen one, its names under it, and the view's settings
func (a *app) drawFooter() {
	b := a.buf
	b.SetWriteMask(visual.MaskUI)
	track := a.h - footerRows
	for r := track - 1; r < a.h; r++ {
		a.clearRow(r)
	}
	a.text(1, track, "colour", rgb(255, 255, 255), terminal.AttrBold)
	last := sliderX + len(schemes)*sliderStep
	for x := sliderX; x <= last; x++ {
		r := '─'
		switch {
		case x == sliderX:
			r = '├'
		case x == last:
			r = '┤'
		case (x-sliderX)%sliderStep == 0:
			r = '┼'
		}
		b.SetFgOnly(x, track, r, rgb(110, 110, 125), terminal.AttrNone)
	}
	for i := 0; i <= len(schemes); i++ {
		name, fg := mixedStop, rgb(255, 255, 255)
		if i < len(schemes) {
			name, fg = schemes[i].name, schemes[i].rgb[partBody]
		}
		x := sliderX + i*sliderStep
		attr := terminal.AttrNone
		if i == a.stop {
			b.SetFgOnly(x, track, '█', fg, terminal.AttrNone)
			attr = terminal.AttrBold
		} else {
			fg = rgb(120, 120, 135)
		}
		a.text(x-len(name)/2, track+1, name, fg, attr)
	}

	t := tiers[a.tier].name
	if a.native {
		t = "this console"
	}
	console := "parts"
	if a.oneColor {
		console = "one colour"
	}
	on := map[bool]string{true: "on", false: "off"}
	a.text(1, a.h-1, fmt.Sprintf("tier %s · console %s · backdrop %s · state %s", t, console, on[a.backdrop], stateNames[a.state]),
		rgb(150, 150, 165), terminal.AttrNone)
}

func (a *app) clearRow(y int) {
	for x := range a.w {
		a.buf.SetWithBg(x, y, ' ', visual.RgbBackground, visual.RgbBackground)
	}
}

func (a *app) text(x, y int, s string, fg color.RGB, attr terminal.Attr) {
	for _, r := range s {
		a.buf.SetFgOnly(x, y, r, fg, attr)
		x++
	}
}

// surface hands the frame to the terminal; on a truecolor terminal showing a
// console tier, each console entry turns into the colour that console shows
type surface struct{ a *app }

func (s surface) ColorMode() terminal.ColorMode { return s.a.term.ColorMode() }
func (s surface) Sync()                         { s.a.term.Sync() }

func (s surface) Flush(cells []terminal.Cell, w, h int) {
	if t := tiers[s.a.tier]; t.console && !s.a.native {
		for i := range cells {
			c := &cells[i]
			if c.Attrs&terminal.AttrFg256 != 0 {
				c.Fg = t.palette.Colors[c.Fg.R&15]
			}
			if c.Attrs&terminal.AttrBg256 != 0 {
				c.Bg = t.palette.Colors[c.Bg.R&15]
			}
			c.Attrs &^= terminal.AttrFg256 | terminal.AttrBg256
		}
	}
	s.a.term.Flush(cells, w, h)
}

// mixed builds a creature's own scheme from its parts' colours and console entries
func mixed(body, eyes, limbs, accent color.RGB, con [parts]uint8) scheme {
	return scheme{mixedStop, [parts]color.RGB{body, eyes, limbs, accent}, con}
}

// Colours the mixed schemes share
var (
	purple     = rgb(170, 90, 230)
	deepPurple = rgb(110, 60, 160)
	lavender   = rgb(215, 170, 255)
	white      = rgb(235, 235, 245)
	silver     = rgb(175, 175, 192)
	orange     = rgb(255, 145, 45)
	darkOrange = rgb(200, 100, 30)
	yellow     = rgb(255, 225, 60)
	gold       = rgb(215, 170, 40)
	red        = rgb(255, 70, 70)
	pink       = rgb(255, 120, 200)
)

func con(body, eyes, limbs, accent uint8) [parts]uint8 {
	return [parts]uint8{body, eyes, limbs, accent}
}

// f is a frame: glyph rows, then the part of each glyph, a digit per cell
func f(glyphs, fg []string) visual.LookFrame { return visual.LookFrame{Glyphs: glyphs, Fg: fg} }

func rows(r ...string) []string { return r }

const (
	cYel = visual.ConYellow
	cMag = visual.ConMagenta
	cWht = visual.ConWhite
	cRED = visual.ConBrightRed
	cYEL = visual.ConBrightYellow
	cMAG = visual.ConBrightMagenta
	cWHT = visual.ConBrightWhite
)

// bestiary is the candidates in the order they are numbered: vanguard's scarab and the
// two beastie creatures picked first, then the new ones by size
var bestiary = []creature{
	{name: "Scarab", w: 4, h: 2, ms: 350, mixed: mixed(purple, red, deepPurple, purple, con(cMAG, cRED, cMag, cMAG)), frames: []visual.LookFrame{
		f(rows("┌oo┐", "└──┘"), rows("0110", "0220")),
		f(rows("┌--┐", "└~~┘"), rows("0000", "0220")),
	}},
	{name: "Crawler", w: 4, h: 2, ms: 300, mixed: mixed(silver, orange, rgb(140, 140, 160), white, con(cWht, cYEL, cWht, cWHT)), frames: []visual.LookFrame{
		f(rows("{oo}", `/\/\`), rows("0110", "2222")),
		f(rows("[oo]", `\/\/`), rows("0110", "2222")),
	}},
	{name: "Scorpion", w: 5, h: 3, ms: 300, mixed: mixed(orange, yellow, darkOrange, red, con(cYel, cYEL, cYel, cRED)), frames: []visual.LookFrame{
		f(rows("  /| ", "(oo) ", `/||\\`), rows("  33 ", "0110 ", "22222")),
		f(rows(" /|  ", " (oo)", `/||\\`), rows(" 33  ", " 0110", "22222")),
		f(rows("/|   ", "(oo) ", `/||\\`), rows("33   ", "0110 ", "22222")),
	}},
	{name: "Crab", w: 4, h: 2, ms: 350, mixed: mixed(orange, white, darkOrange, red, con(cYel, cWHT, cYel, cRED)), frames: []visual.LookFrame{
		f(rows(">oo<", `/  \`), rows("3113", "2  2")),
		f(rows("<oo>", `\  /`), rows("3113", "2  2")),
	}},
	{name: "Gremlin", w: 4, h: 2, ms: 400, mixed: mixed(yellow, red, gold, yellow, con(cYEL, cRED, cYel, cYEL)), frames: []visual.LookFrame{
		f(rows("(oo)", `/""\`), rows("0110", "2002")),
		f(rows("(oo)", `\""/`), rows("0110", "2002")),
	}},
	{name: "Drone", w: 4, h: 2, ms: 150, mixed: mixed(white, red, silver, yellow, con(cWHT, cRED, cWht, cYEL)), frames: []visual.LookFrame{
		f(rows("-==-", "[oo]"), rows("3333", "0110")),
		f(rows("=--=", "[oo]"), rows("3333", "0110")),
	}},
	{name: "Chomper", w: 4, h: 2, ms: 250, mixed: mixed(purple, yellow, white, purple, con(cMAG, cYEL, cWHT, cMAG)), frames: []visual.LookFrame{
		f(rows("/oo\\", `\vv/`), rows("0110", "0220")),
		f(rows("/oo\\", `\--/`), rows("0110", "0220")),
	}},
	{name: "Imp", w: 5, h: 2, ms: 350, mixed: mixed(orange, yellow, darkOrange, red, con(cYel, cYEL, cYel, cRED)), frames: []visual.LookFrame{
		f(rows("(o_o)", `/| |\`), rows("01010", "20 02")),
		f(rows("(o_o)", `\| |/`), rows("01010", "20 02")),
		f(rows("(o_o)", `/| |\`), rows("01010", "20 02")),
		f(rows("(-_-)", `\| |/`), rows("01010", "20 02")),
	}},
	{name: "Saucer", w: 5, h: 2, ms: 250, mixed: mixed(silver, yellow, lavender, orange, con(cWht, cYEL, cMAG, cYel)), frames: []visual.LookFrame{
		f(rows("(o.o)", "<=*=>"), rows("21012", "00300")),
		f(rows("(o.o)", "<*=*>"), rows("21012", "03030")),
	}},
	{name: "Hornet", w: 4, h: 3, ms: 120, mixed: mixed(yellow, red, white, orange, con(cYEL, cRED, cWHT, cYel)), frames: []visual.LookFrame{
		f(rows(`\  /`, "(oo)", ` \/ `), rows("2  2", "0110", " 33 ")),
		f(rows("-  -", "(oo)", ` \/ `), rows("2  2", "0110", " 33 ")),
	}},
	{name: "Frog", w: 4, h: 3, ms: 400, mixed: mixed(purple, yellow, deepPurple, pink, con(cMAG, cYEL, cMag, cMAG)), frames: []visual.LookFrame{
		f(rows("@..@", "(--)", `/  \`), rows("1001", "0330", "2  2")),
		f(rows("@..@", "(><)", `\  /`), rows("1001", "0330", "2  2")),
	}},
	{name: "Slime", w: 5, h: 2, ms: 400, mixed: mixed(lavender, white, purple, pink, con(cMAG, cWHT, cMag, cMAG)), frames: []visual.LookFrame{
		f(rows(`/o o\`, `(___)`), rows("01010", "02220")),
		f(rows("(o o)", `\___/`), rows("01010", "02220")),
	}},
	{name: "Trooper", w: 4, h: 3, ms: 300, mixed: mixed(white, orange, silver, purple, con(cWHT, cYel, cWht, cMAG)), frames: []visual.LookFrame{
		f(rows("[oo]", "-||-", `/  \`), rows("0110", "3003", "2  2")),
		f(rows("[oo]", "=||=", ` /\ `), rows("0110", "3003", " 22 ")),
	}},
	{name: "Hopper", w: 4, h: 3, ms: 300, mixed: mixed(yellow, purple, gold, orange, con(cYEL, cMAG, cYel, cYel)), frames: []visual.LookFrame{
		f(rows("(oo)", `/||\`, `/  \`), rows("0110", "2002", "2  2")),
		f(rows("(oo)", `\||/`, ` \/ `), rows("0110", "2002", " 22 ")),
	}},
	{name: "Bat", w: 5, h: 3, ms: 180, mixed: mixed(purple, yellow, deepPurple, white, con(cMAG, cYEL, cMag, cWHT)), frames: []visual.LookFrame{
		f(rows(`\^ ^/`, "(o.o)", "     "), rows("23 32", "01010", "     ")),
		f(rows("_^ ^_", "(o.o)", "     "), rows("23 32", "01010", "     ")),
		f(rows(" ^ ^ ", "(o.o)", `/   \`), rows(" 3 3 ", "01010", "2   2")),
		f(rows("_^ ^_", "(o.o)", "     "), rows("23 32", "01010", "     ")),
	}},
	{name: "Owl", w: 5, h: 3, ms: 500, mixed: mixed(orange, yellow, darkOrange, white, con(cYel, cYEL, cYel, cWHT)), frames: []visual.LookFrame{
		f(rows("{o,o}", "/)__)", ` " " `), rows("01310", "20000", " 3 3 ")),
		f(rows("{o,o}", "(__(\\", ` " " `), rows("01310", "00002", " 3 3 ")),
		f(rows("{o,o}", "/)__)", ` " " `), rows("01310", "20000", " 3 3 ")),
		f(rows("{-,-}", "/)__)", ` " " `), rows("01310", "20000", " 3 3 ")),
	}},
	{name: "Lynx", w: 5, h: 3, ms: 450, mixed: mixed(purple, yellow, deepPurple, pink, con(cMAG, cYEL, cMag, cMAG)), frames: []visual.LookFrame{
		f(rows(`/\_/\`, "(o.o)", " >^< "), rows("22022", "01310", " 232 ")),
		f(rows(`/\_/\`, "(o.o)", " >^< "), rows("22022", "01310", " 232 ")),
		f(rows(`/\_/\`, "(-.-)", " >^< "), rows("22022", "01310", " 232 ")),
		f(rows(`/|_|\`, "(o.o)", " <^> "), rows("22022", "01310", " 232 ")),
	}},
	{name: "Ghost", w: 5, h: 3, ms: 350, mixed: mixed(white, purple, silver, lavender, con(cWHT, cMAG, cWht, cMAG)), frames: []visual.LookFrame{
		f(rows(" .-. ", "(o o)", `/\/\/`), rows(" 000 ", "01010", "22222")),
		f(rows(" .-. ", "(o o)", `\/\/\`), rows(" 000 ", "01010", "22222")),
	}},
	{name: "Skull", w: 5, h: 3, ms: 300, mixed: mixed(white, red, silver, white, con(cWHT, cRED, cWht, cWHT)), frames: []visual.LookFrame{
		f(rows(" ___ ", "(O O)", " VVV "), rows(" 000 ", "01010", " 222 ")),
		f(rows(" ___ ", "(O O)", " vvv "), rows(" 000 ", "01010", " 222 ")),
	}},
	{name: "Squid", w: 5, h: 3, ms: 300, mixed: mixed(pink, white, purple, pink, con(cMAG, cWHT, cMag, cMAG)), frames: []visual.LookFrame{
		f(rows(` /^\ `, "(o o)", `/|||\`), rows(" 000 ", "01010", "22222")),
		f(rows(` /^\ `, "(o o)", `\|||/`), rows(" 000 ", "01010", "22222")),
		f(rows(` /^\ `, "(o o)", `|/|\|`), rows(" 000 ", "01010", "22222")),
	}},
	{name: "Jelly", w: 5, h: 3, ms: 400, mixed: mixed(lavender, white, purple, pink, con(cMAG, cWHT, cMag, cMAG)), frames: []visual.LookFrame{
		f(rows(".---.", "(o o)", " ))) "), rows("00000", "01010", " 222 ")),
		f(rows(".---.", "(o o)", " ((( "), rows("00000", "01010", " 222 ")),
	}},
	{name: "Beetle", w: 5, h: 3, ms: 300, mixed: mixed(gold, red, darkOrange, yellow, con(cYel, cRED, cYel, cYEL)), frames: []visual.LookFrame{
		f(rows(`/===\`, "|o.o|", `/\ /\`), rows("03330", "01010", "22 22")),
		f(rows(`/===\`, "|o.o|", `\/ \/`), rows("03330", "01010", "22 22")),
	}},
	{name: "Robot", w: 5, h: 3, ms: 350, mixed: mixed(silver, orange, white, red, con(cWht, cYel, cWHT, cRED)), frames: []visual.LookFrame{
		f(rows(" _*_ ", "[o_o]", `/| |\`), rows(" 030 ", "01010", "20 02")),
		f(rows(" _o_ ", "[o_o]", `\| |/`), rows(" 030 ", "01010", "20 02")),
	}},
	{name: "Demon", w: 5, h: 3, ms: 350, mixed: mixed(orange, yellow, red, red, con(cYel, cYEL, cRED, cRED)), frames: []visual.LookFrame{
		f(rows(`\\_//`, "(o.o)", ` \v/ `), rows("33033", "01010", " 030 ")),
		f(rows(`\\_//`, "(o.o)", ` \-/ `), rows("33033", "01010", " 030 ")),
	}},
	{name: "Wraith", w: 5, h: 3, ms: 400, mixed: mixed(lavender, yellow, purple, white, con(cMAG, cYEL, cMag, cWHT)), frames: []visual.LookFrame{
		f(rows(" .M. ", "(o o)", " |~| "), rows(" 333 ", "01010", " 222 ")),
		f(rows(" :M: ", "{o o}", " |~| "), rows(" 333 ", "01010", " 222 ")),
		f(rows(" 'M' ", "[o o]", " |_| "), rows(" 333 ", "01010", " 222 ")),
	}},
	{name: "Djinn", w: 5, h: 3, ms: 350, mixed: mixed(purple, yellow, lavender, orange, con(cMAG, cYEL, cMAG, cYel)), frames: []visual.LookFrame{
		f(rows(" ~~~ ", "(o o)", " ))) "), rows(" 333 ", "01010", " 222 ")),
		f(rows(" ~~~ ", "(o o)", " ((( "), rows(" 333 ", "01010", " 222 ")),
		f(rows(" ~~~ ", "(o o)", " ||| "), rows(" 333 ", "01010", " 222 ")),
	}},
	{name: "Spider", w: 5, h: 3, ms: 250, mixed: mixed(purple, red, deepPurple, white, con(cMAG, cRED, cMag, cWHT)), frames: []visual.LookFrame{
		f(rows(`\/ \/`, "(o.o)", `/\ /\`), rows("22 22", "01010", "22 22")),
		f(rows(`/\ /\`, "(o.o)", `\/ \/`), rows("22 22", "01010", "22 22")),
	}},
	{name: "Puffer", w: 5, h: 3, ms: 450, mixed: mixed(yellow, purple, orange, white, con(cYEL, cMAG, cYel, cWHT)), frames: []visual.LookFrame{
		f(rows(` \|/ `, "(o o)", ` /|\ `), rows(" 232 ", "01010", " 232 ")),
		f(rows(`\\|//`, "(o o)", `//|\\`), rows("22322", "01010", "22322")),
	}},
	{name: "Mantis", w: 5, h: 3, ms: 300, mixed: mixed(yellow, purple, gold, orange, con(cYEL, cMAG, cYel, cYel)), frames: []visual.LookFrame{
		f(rows(`\(oo)`, " /|\\ ", "/ | \\"), rows("30110", " 202 ", "2 2 2")),
		f(rows(`|(oo)`, " /|\\ ", "/ | \\"), rows("30110", " 202 ", "2 2 2")),
	}},
}

// Command raider shows vanguard's raider types as its scenario declares them: a
// band for each stone art, its types weakest rank first between a ceiling and a
// floor of that art's rock, in the colours their programs give them or one hue a
// slider picks, on the game's truecolor tier or its console tier as a VGA or
// FreeBSD console shows it. Run it from the repository root, or name the scenario
// directory with -s.
package main

import (
	"flag"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"slices"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/pattern"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/system"
)

// A program's fg grid names each glyph's part, which a hue colours
const (
	partBody = iota
	partEyes
	partLimbs
	partAccent
	parts
)

// hue colours every raider's four parts alike, and every glyph in one console entry
type hue struct {
	name    string
	rgb     [parts]color.RGB
	console uint8
}

func rgb(r, g, b uint8) color.RGB { return color.RGB{R: r, G: g, B: b} }

// The slider's first stop is each program's own palette; the rest colour every
// raider in one hue, its eyes the palest and its limbs the darkest
const ownStop = "Vanguard"

var hues = []hue{
	{"Purple", [parts]color.RGB{rgb(170, 90, 230), rgb(232, 205, 255), rgb(115, 60, 165), rgb(205, 145, 255)}, visual.ConBrightMagenta},
	{"White", [parts]color.RGB{rgb(228, 228, 238), rgb(255, 255, 255), rgb(150, 150, 168), rgb(195, 195, 210)}, visual.ConBrightWhite},
	{"Orange", [parts]color.RGB{rgb(255, 140, 40), rgb(255, 215, 140), rgb(190, 90, 25), rgb(255, 100, 45)}, visual.ConYellow},
}

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
	cardW        = 13 // a raider's column
	nameW        = 9  // the cave's name, left of its first row of cards
	bandRows     = 2  // a ceiling's and a floor's rows of rock
	openRows     = 3  // the 5×3 a composite navigates, between them
	rowH         = 1 + 2*bandRows + openRows
	gridTop      = 2
	footerRows   = 4
	sliderX      = 10
	sliderStep   = 10
	backdropFill = 0.05 // the share of cells a drifting glyph holds
	driftCPS     = 10.0 // the backdrop's drift, the cave's at half its scroll
	caveCPS      = 20.0 // the rock's scroll, the cave's own
)

// raider is one program as the raid spawns it: its prototype, and its rank among
// its cave's, 1 for the fewest hit points
type raider struct {
	proto component.BehaviorPrototype
	rank  int
}

// cave is one stone art and the raider types the raid picks under it
type cave struct {
	art     pattern.ArtPreset
	raiders []raider
}

type glyph struct {
	x, y float64
	r    rune
	fg   color.RGB
}

type app struct {
	term     terminal.Terminal
	native   bool // the terminal draws 256 colours, so its own console is the only tier
	buf      *render.RenderBuffer
	caves    []cave
	w, h     int
	stop     int // the slider's position, 0 for the programs' own colours
	tier     int
	backdrop bool
	state    int
	scroll   int // in screen rows
	glyphs   []glyph
	start    time.Time
	dragging bool
}

func main() {
	dir := flag.String("s", "wad/scenario/vanguard", "the scenario directory whose raiders to show")
	flag.Parse()
	caves, err := load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "raider:", err)
		os.Exit(1)
	}

	term := terminal.New()
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()
	_ = term.SetMouseMode(terminal.MouseModeClick | terminal.MouseModeDrag)

	a := &app{term: term, native: term.ColorMode() != terminal.ColorModeTrueColor, caves: caves, backdrop: true, start: time.Now()}
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

// load compiles the scenario's programs as the game does and groups them as its
// raid picks them: the program pick's list split evenly over the stone art pick's,
// in order
func load(dir string) ([]cave, error) {
	merged, err := fsm.ResolveScenario(os.DirFS(dir), "scenario.toml")
	if err != nil {
		return nil, err
	}
	docs := map[string]map[string]any{}
	behaviors, _ := merged["behaviors"].(map[string]any)
	for name, doc := range behaviors {
		docs[name], _ = doc.(map[string]any)
	}
	set, err := system.CompileBehaviors(docs, merged)
	if err != nil {
		return nil, err
	}
	arts, names := pick(merged, "art"), pick(merged, "program")
	if len(arts) == 0 || len(names) == 0 || len(names)%len(arts) != 0 {
		return nil, fmt.Errorf("%d raider types do not split evenly over %d stone arts", len(names), len(arts))
	}
	byName := map[string]component.BehaviorPrototype{}
	for _, p := range set.Prototypes() {
		byName[p.Name] = p
	}

	per := len(names) / len(arts)
	caves := make([]cave, len(arts))
	for i, name := range arts {
		art, ok := pattern.ParseArtPreset(name)
		if !ok {
			return nil, fmt.Errorf("stone art %q is no preset", name)
		}
		c := cave{art: art}
		var hps []int
		for _, n := range names[i*per : (i+1)*per] {
			p, ok := byName[n]
			if !ok {
				return nil, fmt.Errorf("the raid picks %q, which no program defines", n)
			}
			c.raiders = append(c.raiders, raider{proto: p})
			if !slices.Contains(hps, p.HitPoints) {
				hps = append(hps, p.HitPoints)
			}
		}
		slices.Sort(hps)
		for j := range c.raiders {
			c.raiders[j].rank = slices.Index(hps, c.raiders[j].proto.HitPoints) + 1
		}
		caves[i] = c
	}
	return caves, nil
}

// pick returns the list of the first pick of field in the scenario's tree, in key order
func pick(v any, field string) []string {
	switch v := v.(type) {
	case map[string]any:
		if p, ok := v["pick"].(map[string]any); ok && p["field"] == field {
			from, _ := p["from"].([]any)
			out := make([]string, 0, len(from))
			for _, s := range from {
				if s, ok := s.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
		for _, k := range slices.Sorted(maps.Keys(v)) {
			if out := pick(v[k], field); out != nil {
				return out
			}
		}
	case []any:
		for _, e := range v {
			if out := pick(e, field); out != nil {
				return out
			}
		}
	}
	return nil
}

func (a *app) console() bool { return a.native || tiers[a.tier].console }

func (a *app) resize(w, h int) {
	a.w, a.h = w, h
	mode := terminal.ColorModeTrueColor
	if a.console() {
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
		a.stop = min(a.stop+1, len(hues))
	case ev.Key == terminal.KeyUp || ev.Rune == 'k':
		a.scroll -= rowH
	case ev.Key == terminal.KeyDown || ev.Rune == 'j':
		a.scroll += rowH
	case ev.Key == terminal.KeyPageUp:
		a.scroll -= a.viewRows()
	case ev.Key == terminal.KeyPageDown:
		a.scroll += a.viewRows()
	case ev.Rune == 'c' && !a.native:
		a.tier = (a.tier + 1) % len(tiers)
		a.resize(a.w, a.h)
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
		a.scroll -= rowH
	case terminal.MouseBtnWheelDown:
		a.scroll += rowH
	case terminal.MouseBtnLeft:
		track := a.h - footerRows
		if ev.MouseAction == terminal.MouseActionPress {
			a.dragging = ev.MouseY == track || ev.MouseY == track+1
		}
		if a.dragging && ev.MouseAction != terminal.MouseActionRelease {
			a.stop = min(max((ev.MouseX-sliderX+sliderStep/2)/sliderStep, 0), len(hues))
		}
		if ev.MouseAction == terminal.MouseActionRelease {
			a.dragging = false
		}
	}
	a.clampScroll()
}

// perRow is the cards a row holds right of the caves' names
func (a *app) perRow() int { return max((a.w-1-nameW)/cardW, 1) }

// viewRows is the screen rows between the header and the footer
func (a *app) viewRows() int { return max(a.h-gridTop-footerRows, 1) }

func (a *app) contentRows() int {
	n := 0
	for _, c := range a.caves {
		n += (len(c.raiders) + a.perRow() - 1) / a.perRow() * rowH
	}
	return n
}

func (a *app) clampScroll() {
	a.scroll = min(max(a.scroll, 0), max(a.contentRows()-a.viewRows(), 0))
}

func (a *app) render() {
	b := a.buf
	b.Clear()
	elapsed := time.Since(a.start)
	b.SetClip(render.RectWH(0, gridTop, a.w, a.viewRows()))
	if a.backdrop {
		a.drawBackdrop(elapsed)
	}
	per, y := a.perRow(), gridTop-a.scroll
	for i := range a.caves {
		c := &a.caves[i]
		for first := 0; first < len(c.raiders); first += per {
			a.drawRow(i, c.raiders[first:min(first+per, len(c.raiders))], first == 0, y, elapsed)
			y += rowH
		}
	}
	b.ClearClip()
	a.drawHeader()
	a.drawFooter()
	b.FlushToTerminal(surface{a})
}

// drawRow draws one row of a cave's cards: their labels, the cave's ceiling, the
// raiders bottom-aligned on its floor, so legs line up, and the floor
func (a *app) drawRow(cave int, rs []raider, first bool, y int, elapsed time.Duration) {
	c := &a.caves[cave]
	a.buf.SetWriteMask(visual.MaskUI)
	a.clearRow(y)
	if first {
		a.text(1, y, c.art.String(), a.swatch(&c.raiders[0].proto.Look), terminal.AttrBold)
	}
	for i, r := range rs {
		x := 1 + nameW + i*cardW
		a.text(x, y, fmt.Sprintf("R%d ", r.rank), rgb(255, 255, 255), terminal.AttrBold)
		a.text(x+3, y, r.proto.Name, rgb(150, 150, 165), terminal.AttrNone)
	}

	ceiling, open, floor := y+1, y+1+bandRows, y+1+bandRows+openRows
	scroll := int64(0)
	if a.backdrop {
		scroll = int64(elapsed.Seconds() * caveCPS)
	}
	for d := range bandRows {
		a.drawRock(c.art, uint64(cave+1), ceiling+bandRows-1-d, d, pattern.FaceDown, scroll)
		a.drawRock(c.art, uint64(cave+1), floor+d, d, pattern.FaceUp, scroll)
	}

	a.buf.SetWriteMask(visual.MaskComposite)
	for i := range rs {
		p := &rs[i].proto
		x := 1 + nameW + i*cardW
		frame := p.Look.Frames[0]
		if len(p.Look.Frames) > 1 && p.Pattern > 0 {
			frame = p.Look.Frames[int(elapsed/p.Pattern)%len(p.Look.Frames)]
		}
		ox, oy := x+(cardW-1-p.Width)/2, open+openRows-p.Height
		for j := range frame {
			a.cell(ox+j%p.Width, oy+j/p.Width, &frame[j], &p.Look, elapsed)
		}
	}
}

// drawRock draws a screen row of rock in art, depth cells from the open rows it faces,
// scrolled as the cave scrolls; on a console it takes the art's console indices
func (a *app) drawRock(art pattern.ArtPreset, seed uint64, y, depth int, face pattern.Face, scroll int64) {
	a.buf.SetWriteMask(visual.MaskWall)
	for x := range a.w {
		cell := pattern.ArtCell{X: int64(x) + scroll, Y: int64(y + a.scroll), Depth: depth, Face: face, W: a.w, H: bandRows}
		if a.console() {
			r, fg, bg := pattern.Art256(art, seed, cell)
			a.buf.SetBg256(x, y, bg)
			a.buf.SetFgOnly(x, y, r, color.RGB{R: fg}, terminal.AttrFg256)
			continue
		}
		r, fg, bg := pattern.Art(art, seed, cell)
		a.buf.SetWithBg(x, y, r, fg, bg)
	}
}

// swatch is the body colour the slider gives a look
func (a *app) swatch(look *component.Look) color.RGB {
	if a.stop > 0 {
		return hues[a.stop-1].rgb[partBody]
	}
	return look.Fg[partBody]
}

// cell paints one glyph as the game's look painter does: its part's colour, the
// enraged colour over every glyph, and a hit's flash, which a console shows reversed
func (a *app) cell(x, y int, cell *component.LookCell, look *component.Look, elapsed time.Duration) {
	if cell.Rune == 0 {
		return
	}
	fg, con := look.Color, look.Fg256
	if cell.Fg >= 0 {
		fg = look.Fg[cell.Fg]
	}
	if a.stop > 0 {
		h := &hues[a.stop-1]
		fg, con = h.rgb[min(max(int(cell.Fg), partBody), partAccent)], h.console
	}
	console := a.console()
	hit := a.state == stateHit && elapsed%(4*parameter.CombatHitFlashDuration) < parameter.CombatHitFlashDuration
	switch {
	case a.state == stateEnraged || (hit && !console):
		tint := visual.RgbCombatEnraged
		if hit {
			tint = color.Scale(visual.RgbCombatHitFlash, flashIntensity(elapsed%(4*parameter.CombatHitFlashDuration)))
		}
		a.buf.SetFgOnly(x, y, cell.Rune, tint, cell.Attr)
	case console:
		attr := terminal.AttrFg256 | cell.Attr
		if hit {
			attr |= terminal.AttrReverse
		}
		a.buf.SetFgOnly(x, y, cell.Rune, color.RGB{R: con}, attr)
	default:
		a.buf.SetFgOnly(x, y, cell.Rune, fg, cell.Attr)
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
	a.text(1, 0, "VANGUARD RAIDERS", rgb(255, 255, 255), terminal.AttrBold)
	a.text(19, 0, "each cave's types by tier, T1 the weakest", rgb(150, 150, 165), terminal.AttrNone)
	help := "←/→ colour  c tier  b backdrop  s state  ↑/↓ scroll  q quit"
	if a.native {
		help = "←/→ colour  b backdrop  s state  ↑/↓ scroll  q quit"
	}
	a.text(1, 1, help, rgb(150, 150, 165), terminal.AttrNone)
}

// drawFooter draws the colour slider, a track with a stop per hue after the
// programs' own and the knob on the chosen one, its names under it, and the view's
// settings
func (a *app) drawFooter() {
	b := a.buf
	b.SetWriteMask(visual.MaskUI)
	track := a.h - footerRows
	for r := track - 1; r < a.h; r++ {
		a.clearRow(r)
	}
	a.text(1, track, "colour", rgb(255, 255, 255), terminal.AttrBold)
	last := sliderX + len(hues)*sliderStep
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
	for i := 0; i <= len(hues); i++ {
		name, fg := ownStop, rgb(255, 255, 255)
		if i > 0 {
			name, fg = hues[i-1].name, hues[i-1].rgb[partBody]
		}
		x := sliderX + i*sliderStep
		attr := terminal.AttrNone
		if i == a.stop {
			b.SetFgOnly(x, track, '█', fg, terminal.AttrNone)
			attr = terminal.AttrBold
		} else {
			fg = rgb(120, 120, 135)
		}
		a.text(max(x-len(name)/2, 0), track+1, name, fg, attr)
	}

	t := tiers[a.tier].name
	if a.native {
		t = "this console"
	}
	on := map[bool]string{true: "on", false: "off"}
	a.text(1, a.h-1, fmt.Sprintf("tier %s · backdrop %s · state %s", t, on[a.backdrop], stateNames[a.state]),
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

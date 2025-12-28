package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Mode represents the current app mode
type Mode int

const (
	ModePalette Mode = iota
	ModeBlend
	ModeEffect
	ModeDiag
)

// EffectSubMode for effect mode
type EffectSubMode int

const (
	EffectShield EffectSubMode = iota
	EffectTrail
	EffectFlash
	EffectHeat
)

// ShieldColorState for shield color simulation
type ShieldColorState int

const (
	ShieldGray ShieldColorState = iota
	ShieldBlue
	ShieldGreen
)

// App state
type AppState struct {
	mode      Mode
	effectSub EffectSubMode
	running   bool
	width     int
	height    int
	colorMode terminal.ColorMode

	// Palette mode
	paletteIdx    int
	paletteScroll int

	// Blend mode
	blendSrcIdx   int
	blendDstIdx   int
	blendOp       int
	blendAlpha    float64
	blendBgIdx    int // 0=black, 1=white, 2=game-bg, 3=custom
	blendCustomBg render.RGB

	// Effect mode - Shield
	shieldRadiusX     float64
	shieldRadiusY     float64
	shieldOpacity     float64
	shieldState       ShieldColorState
	shieldBgIdx       int
	shieldCustomColor render.RGB // Custom shield color
	shieldColorMode   int        // 0=preset, 1=custom

	// Effect mode - Trail
	trailLength   int
	trailColorIdx int
	trailType     int // 0=cleaner, 1=materialize

	// Effect mode - Flash
	flashFrame    int
	flashDuration int
	flashColorIdx int

	// Effect mode - Heat
	heatValue int // 0-100
	heatBgIdx int // Background preset index

	// Diag mode
	diagInputHex string
	diagInputRGB render.RGB

	// Hex input state
	hexInputActive bool
	hexInputBuffer string
	hexInputTarget int // 0=blend custom bg, 1=diag input
}

const (
	MinWidth  = 100
	MinHeight = 30
)

var (
	term  terminal.Terminal
	state AppState
	buf   *render.RenderBuffer
)

// Blend operation names and formulas
var blendOps = []struct {
	name    string
	formula string
	mode    render.BlendMode
}{
	{"Replace", "Result = Src", render.BlendReplace},
	{"Alpha", "Result = Dst*(1-α) + Src*α", render.BlendAlpha},
	{"Set", "Result = min(Dst + Src, 255)", render.BlendAdd},
	{"Max", "Result = max(Dst, Src)", render.BlendMax},
	{"SoftLight", "Perez: df < 0.5 ? df-(1-2sf)*df*(1-df) : df+(2sf-1)*(G(df)-df)", render.BlendSoftLight},
	{"Screen", "Result = 255 - (255-Dst)*(255-Src)/255", render.BlendScreen},
	{"Overlay", "Dst<128 ? 2*Dst*Src/255 : 255-2*(255-Dst)*(255-Src)/255", render.BlendOverlay},
}

// Background presets
var bgPresets = []struct {
	name  string
	color render.RGB
}{
	{"Black", render.RGB{0, 0, 0}},
	{"White", render.RGB{255, 255, 255}},
	{"GameBg", render.RGB{26, 27, 38}},
	{"Custom", render.RGB{128, 128, 128}},
}

func main() {
	// Detect color mode from env, allow override
	colorMode := terminal.DetectColorMode()
	for _, arg := range os.Args[1:] {
		if arg == "--256" {
			colorMode = terminal.ColorMode256
		} else if arg == "--tc" || arg == "--truecolor" {
			colorMode = terminal.ColorModeTrueColor
		}
	}

	term = terminal.New(colorMode)
	if err := term.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "terminal init failed: %v\n", err)
		os.Exit(1)
	}
	defer term.Fini()

	w, h := term.Size()
	state = AppState{
		mode:          ModePalette,
		running:       true,
		width:         w,
		height:        h,
		colorMode:     colorMode,
		blendAlpha:    1.0,
		shieldRadiusX: 10.0,
		shieldRadiusY: 5.0,
		shieldOpacity: 0.6,
		trailLength:   8,
		flashDuration: 10,
		heatValue:     50,
		// Default analyze color to RgbPingNormal
		diagInputRGB: render.RgbPingLineNormal,
		diagInputHex: "996600",
	}
	buf = render.NewRenderBuffer(w, h)

	mainLoop()
}

func mainLoop() {
	renderFrame()

	for state.running {
		ev := term.PollEvent()
		switch ev.Type {
		case terminal.EventKey:
			handleInput(ev)
		case terminal.EventResize:
			state.width = ev.Width
			state.height = ev.Height
			buf.Resize(ev.Width, ev.Height)
			term.Sync()
		case terminal.EventError:
			state.running = false
		}
		renderFrame()
	}
}

func handleInput(ev terminal.Event) {
	// Hex input mode
	if state.hexInputActive {
		handleHexInput(ev)
		return
	}

	// Global keys
	switch ev.Key {
	case terminal.KeyEscape, terminal.KeyCtrlC:
		state.running = false
		return
	case terminal.KeyF1:
		state.mode = ModePalette
		return
	case terminal.KeyF2:
		state.mode = ModeBlend
		return
	case terminal.KeyF3:
		state.mode = ModeEffect
		return
	case terminal.KeyF4:
		state.mode = ModeDiag
		return
	case terminal.KeyTab:
		state.mode = (state.mode + 1) % 4
		return
	case terminal.KeyBacktab: // Shift+Tab
		state.mode = (state.mode + 3) % 4 // +3 same as -1 mod 4
		return
	}

	if ev.Key == terminal.KeyRune && (ev.Rune == 'q' || ev.Rune == 'Q') {
		state.running = false
		return
	}

	// Mode-specific keys
	switch state.mode {
	case ModePalette:
		handlePaletteInput(ev)
	case ModeBlend:
		handleBlendInput(ev)
	case ModeEffect:
		handleEffectInput(ev)
	case ModeDiag:
		handleDiagInput(ev)
	}
}

func handleHexInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		state.hexInputActive = false
		state.hexInputBuffer = ""
	case terminal.KeyEnter:
		if len(state.hexInputBuffer) == 6 {
			rgb := parseHex(state.hexInputBuffer)
			switch state.hexInputTarget {
			case 0:
				state.blendCustomBg = rgb
			case 1:
				state.diagInputRGB = rgb
				state.diagInputHex = state.hexInputBuffer
			case 2:
				state.shieldCustomColor = rgb
				state.shieldColorMode = 1
			}
		}
		state.hexInputActive = false
		state.hexInputBuffer = ""
	case terminal.KeyBackspace:
		if len(state.hexInputBuffer) > 0 {
			state.hexInputBuffer = state.hexInputBuffer[:len(state.hexInputBuffer)-1]
		}
	case terminal.KeyRune:
		if len(state.hexInputBuffer) < 6 {
			r := ev.Rune
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				state.hexInputBuffer += strings.ToUpper(string(r))
			}
		}
	}
}

func parseHex(s string) render.RGB {
	if len(s) != 6 {
		return render.RGB{}
	}
	r, _ := strconv.ParseUint(s[0:2], 16, 8)
	g, _ := strconv.ParseUint(s[2:4], 16, 8)
	b, _ := strconv.ParseUint(s[4:6], 16, 8)
	return render.RGB{R: uint8(r), G: uint8(g), B: uint8(b)}
}

func renderFrame() {
	buf.Clear()

	// Draw header
	drawHeader()

	// Draw mode content
	switch state.mode {
	case ModePalette:
		drawPaletteMode()
	case ModeBlend:
		drawBlendMode()
	case ModeEffect:
		drawEffectMode()
	case ModeDiag:
		drawDiagMode()
	}

	// Draw footer with size info
	drawFooter()

	buf.FlushToTerminal(term)
}

func drawHeader() {
	modes := []string{"F1:Palette", "F2:Blend", "F3:Effect", "F4:Analyze"}
	x := 1
	for i, m := range modes {
		fg := render.RGB{180, 180, 180}
		bg := render.RGB{40, 40, 40}
		if Mode(i) == state.mode {
			fg = render.RGB{0, 0, 0}
			bg = render.RGB{0, 255, 255}
		}
		drawText(x, 0, " "+m+" ", fg, bg)
		x += len(m) + 3
	}

	// Color mode indicator
	modeStr := "TC"
	if state.colorMode == terminal.ColorMode256 {
		modeStr = "256"
	}
	drawText(state.width-6, 0, "["+modeStr+"]", render.RGB{255, 255, 0}, render.RGB{40, 40, 40})
}

func drawFooter() {
	// Size info
	sizeStr := fmt.Sprintf("Size: %dx%d  Min: %dx%d", state.width, state.height, MinWidth, MinHeight)
	drawText(state.width-len(sizeStr)-1, state.height-1, sizeStr, render.RGB{100, 100, 100}, render.RGB{0, 0, 0})

	// Global keys
	drawText(1, state.height-1, "Q:Quit Tab/Shift-Tab:Mode", render.RGB{100, 100, 100}, render.RGB{0, 0, 0})
}
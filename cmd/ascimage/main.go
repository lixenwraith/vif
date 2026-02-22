package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/lixenwraith/vi-fighter/cmd/ascimage/ascimage"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

func main() {
	var (
		modeStr    string
		colorStr   string
		width      int
		output     string
		dualOutput string
		fitMode    bool
		noStatus   bool
		zoomLevel  int
		anchorX    int
		anchorY    int
	)

	flag.StringVar(&modeStr, "m", "quadrant", "Render mode: 'bg' or 'quadrant'")
	flag.StringVar(&colorStr, "c", "auto", "Color depth: 'auto', 'true', or '256'")
	flag.IntVar(&width, "w", 0, "Output width (file mode only, 0 = 80)")
	flag.StringVar(&dualOutput, "dual", "", "Output dual-mode .vfimg file")
	flag.StringVar(&output, "o", "", "Output ANSI to file ('-' for stdout), omit for interactive")
	flag.BoolVar(&fitMode, "fit", true, "Start in fit-to-screen mode (interactive only)")
	flag.BoolVar(&noStatus, "no-status", false, "Hide status bar (interactive only)")
	flag.IntVar(&zoomLevel, "z", 100, "Initial zoom level percent (interactive only)")
	flag.IntVar(&anchorX, "ax", 0, "Anchor X offset (dual-mode output)")
	flag.IntVar(&anchorY, "ay", 0, "Anchor Y offset (dual-mode output)")
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	inputPath := flag.Arg(0)
	colorMode := parseColorMode(colorStr)

	if isVfimg(inputPath) {
		runVfimgInput(inputPath, colorMode, output, noStatus)
	} else {
		runImageInput(inputPath, modeStr, colorMode, width, output, dualOutput,
			fitMode, noStatus, zoomLevel, anchorX, anchorY)
	}
}

func isVfimg(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".vfimg")
}

func runVfimgInput(path string, colorMode terminal.ColorMode, output string, noStatus bool) {
	dual, err := ascimage.LoadDualMode(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading vfimg: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Loaded: %s (%dx%d, %s)\n",
		path, dual.Width, dual.Height, dual.RenderMode.String())

	if output != "" {
		conv := dual.ToConvertedImage(colorMode)
		if err := ascimage.WriteANSI(conv, output, colorMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(1)
		}
		return
	}

	viewer := ascimage.NewViewerFromDual(dual)
	viewer.ColorMode = colorMode
	viewer.ShowStatus = !noStatus

	if err := runViewer(viewer, colorMode); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runImageInput(path, modeStr string, colorMode terminal.ColorMode, width int,
	output, dualOutput string, fitMode, noStatus bool, zoomLevel, anchorX, anchorY int) {

	img, err := loadImage(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading image: %v\n", err)
		os.Exit(1)
	}

	bounds := img.Bounds()
	fmt.Fprintf(os.Stderr, "Loaded: %s (%dx%d)\n", path, bounds.Dx(), bounds.Dy())

	renderMode := parseRenderMode(modeStr)

	if dualOutput != "" {
		runDualOutput(img, renderMode, width, dualOutput, anchorX, anchorY)
	} else if output != "" {
		runFileOutput(img, renderMode, colorMode, width, output)
	} else {
		runInteractive(img, renderMode, colorMode, fitMode, noStatus, zoomLevel)
	}
}

func runDualOutput(img image.Image, renderMode ascimage.RenderMode, width int, output string, anchorX, anchorY int) {
	if width <= 0 {
		width = 80
	}

	dual := ascimage.ConvertImageDual(img, width, renderMode)
	dual.AnchorX = anchorX
	dual.AnchorY = anchorY

	fmt.Fprintf(os.Stderr, "Dual-mode output: %dx%d cells\n", dual.Width, dual.Height)

	if err := ascimage.SaveDualMode(output, dual); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing dual-mode output: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: ascimage [options] <image|file.vfimg>")
	fmt.Fprintln(os.Stderr, "\nSupported formats: PNG, JPEG, GIF (input), .vfimg (view/convert)")
	fmt.Fprintln(os.Stderr, "\nOptions:")
	flag.PrintDefaults()
	fmt.Fprintln(os.Stderr, "\nModes:")
	fmt.Fprintln(os.Stderr, "  Image input:")
	fmt.Fprintln(os.Stderr, "    Dual-mode (-dual): write .vfimg for vi-fighter pattern system")
	fmt.Fprintln(os.Stderr, "    File output (-o):  write ANSI sequences to file")
	fmt.Fprintln(os.Stderr, "    Interactive:        view image with zoom/pan controls (default)")
	fmt.Fprintln(os.Stderr, "  .vfimg input:")
	fmt.Fprintln(os.Stderr, "    File output (-o):  convert .vfimg to ANSI sequences")
	fmt.Fprintln(os.Stderr, "    Interactive:        view with color mode toggle (default)")
	fmt.Fprintln(os.Stderr, "\nInteractive controls:")
	fmt.Fprintln(os.Stderr, "  q, Esc, Ctrl+C    Quit")
	fmt.Fprintln(os.Stderr, "  f                 Toggle fit/actual size (image only)")
	fmt.Fprintln(os.Stderr, "  m                 Toggle render mode (image only)")
	fmt.Fprintln(os.Stderr, "  c                 Toggle color mode")
	fmt.Fprintln(os.Stderr, "  +/-               Zoom in/out (image only)")
	fmt.Fprintln(os.Stderr, "  Arrow keys, hjkl  Pan viewport")
	fmt.Fprintln(os.Stderr, "  s                 Toggle status bar")
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

func parseRenderMode(s string) ascimage.RenderMode {
	switch s {
	case "bg", "background":
		return ascimage.ModeBackgroundOnly
	case "quadrant", "q":
		return ascimage.ModeQuadrant
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s, using quadrant\n", s)
		return ascimage.ModeQuadrant
	}
}

func parseColorMode(s string) terminal.ColorMode {
	switch s {
	case "auto":
		return terminal.DetectColorMode()
	case "true", "truecolor", "24":
		return terminal.ColorModeTrueColor
	case "256", "8":
		return terminal.ColorMode256
	default:
		fmt.Fprintf(os.Stderr, "Unknown color mode: %s, using auto\n", s)
		return terminal.DetectColorMode()
	}
}

func runFileOutput(img image.Image, renderMode ascimage.RenderMode, colorMode terminal.ColorMode, width int, output string) {
	if width <= 0 {
		width = 80
	}

	converted := ascimage.ConvertImage(img, width, renderMode, colorMode)
	fmt.Fprintf(os.Stderr, "Output: %dx%d cells\n", converted.Width, converted.Height)

	if err := ascimage.WriteANSI(converted, output, colorMode); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}
}

func runInteractive(img image.Image, renderMode ascimage.RenderMode, colorMode terminal.ColorMode, fitMode, noStatus bool, zoomLevel int) {
	viewer := ascimage.NewViewer(img)
	viewer.RenderMode = renderMode
	viewer.ColorMode = colorMode
	viewer.ShowStatus = !noStatus

	if !fitMode {
		viewer.ViewMode = ascimage.ViewActual
	}
	if zoomLevel != 100 {
		viewer.ViewMode = ascimage.ViewCustom
		viewer.ZoomLevel = zoomLevel
	}

	if err := runViewer(viewer, colorMode); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runViewer(viewer *ascimage.Viewer, colorMode terminal.ColorMode) error {
	term := terminal.New(colorMode)

	if err := term.Init(); err != nil {
		return fmt.Errorf("terminal init: %w", err)
	}
	defer term.Fini()

	termW, termH := term.Size()
	buf := render.NewRenderBuffer(colorMode, termW, termH)

	viewer.Update(termW, termH)
	renderFrame(viewer, buf, term, termW, termH)

	for {
		ev := term.PollEvent()

		switch ev.Type {
		case terminal.EventKey:
			action := handleKey(ev, viewer, termW, termH)
			if action == actionQuit {
				return nil
			}
			if action == actionRedraw {
				viewer.ForceUpdate(termW, termH)
			}

		case terminal.EventResize:
			termW, termH = ev.Width, ev.Height
			buf.Resize(termW, termH)
			viewer.Update(termW, termH)

		case terminal.EventError, terminal.EventClosed:
			return nil
		}

		renderFrame(viewer, buf, term, termW, termH)
	}
}

type keyAction int

const (
	actionNone keyAction = iota
	actionQuit
	actionRedraw
)

func handleKey(ev terminal.Event, viewer *ascimage.Viewer, termW, termH int) keyAction {
	smallStep := 1
	largeStep := 10
	pageStep := termH / 2

	switch ev.Key {
	case terminal.KeyEscape, terminal.KeyCtrlC, terminal.KeyCtrlD:
		return actionQuit
	case terminal.KeyRune:
		switch ev.Rune {
		case 'q', 'Q':
			return actionQuit
		case 'f', 'F':
			viewer.ToggleViewMode()
			return actionRedraw
		case 'm', 'M':
			viewer.ToggleRenderMode()
			return actionRedraw
		case 'c', 'C':
			viewer.ToggleColorMode()
			return actionRedraw
		case 's', 'S':
			viewer.ShowStatus = !viewer.ShowStatus
			return actionNone
		case '+', '=':
			viewer.AdjustZoom(10)
			return actionRedraw
		case '-', '_':
			viewer.AdjustZoom(-10)
			return actionRedraw
		case '0':
			viewer.ZoomLevel = 100
			viewer.ViewMode = ascimage.ViewCustom
			return actionRedraw
		case 'h':
			viewer.Pan(-smallStep, 0, termW, termH)
		case 'l':
			viewer.Pan(smallStep, 0, termW, termH)
		case 'j':
			viewer.Pan(0, smallStep, termW, termH)
		case 'k':
			viewer.Pan(0, -smallStep, termW, termH)
		case 'H':
			viewer.Pan(-largeStep, 0, termW, termH)
		case 'L':
			viewer.Pan(largeStep, 0, termW, termH)
		case 'J':
			viewer.Pan(0, largeStep, termW, termH)
		case 'K':
			viewer.Pan(0, -largeStep, termW, termH)
		case 'g':
			viewer.PanTo(0, 0, termW, termH)
		case 'G':
			viewer.PanTo(0, 999999, termW, termH)
		}

	case terminal.KeyLeft:
		step := smallStep
		if ev.Modifiers&terminal.ModShift != 0 {
			step = largeStep
		}
		viewer.Pan(-step, 0, termW, termH)
	case terminal.KeyRight:
		step := smallStep
		if ev.Modifiers&terminal.ModShift != 0 {
			step = largeStep
		}
		viewer.Pan(step, 0, termW, termH)
	case terminal.KeyUp:
		step := smallStep
		if ev.Modifiers&terminal.ModShift != 0 {
			step = largeStep
		}
		viewer.Pan(0, -step, termW, termH)
	case terminal.KeyDown:
		step := smallStep
		if ev.Modifiers&terminal.ModShift != 0 {
			step = largeStep
		}
		viewer.Pan(0, step, termW, termH)
	case terminal.KeyPageUp:
		viewer.Pan(0, -pageStep, termW, termH)
	case terminal.KeyPageDown:
		viewer.Pan(0, pageStep, termW, termH)
	case terminal.KeyHome:
		viewer.PanTo(0, viewer.ViewportY, termW, termH)
	case terminal.KeyEnd:
		viewer.PanTo(999999, viewer.ViewportY, termW, termH)
	}

	return actionNone
}

func renderFrame(viewer *ascimage.Viewer, buf *render.RenderBuffer, term terminal.Terminal, termW, termH int) {
	buf.Clear()
	viewer.Render(buf, termW, termH)
	buf.FlushToTerminal(term)
}
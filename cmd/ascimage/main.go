package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/lixenwraith/vi-fighter/cmd/ascimage/ascimage"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

func main() {
	var (
		modeStr   string
		colorStr  string
		fitMode   bool
		noStatus  bool
		zoomLevel int
	)

	flag.StringVar(&modeStr, "m", "quadrant", "Render mode: 'bg' or 'quadrant'")
	flag.StringVar(&colorStr, "c", "auto", "Color depth: 'auto', 'true', or '256'")
	flag.BoolVar(&fitMode, "fit", true, "Start in fit-to-screen mode")
	flag.BoolVar(&noStatus, "no-status", false, "Hide status bar")
	flag.IntVar(&zoomLevel, "z", 100, "Initial zoom level (percent)")
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	imagePath := flag.Arg(0)

	// Load image
	img, err := loadImage(imagePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading image: %v\n", err)
		os.Exit(1)
	}

	bounds := img.Bounds()
	fmt.Fprintf(os.Stderr, "Loaded: %s (%dx%d)\n", imagePath, bounds.Dx(), bounds.Dy())

	// Create viewer
	viewer := ascimage.NewViewer(img)

	// Apply CLI options
	switch modeStr {
	case "bg", "background":
		viewer.RenderMode = ascimage.ModeBackgroundOnly
	case "quadrant", "q":
		viewer.RenderMode = ascimage.ModeQuadrant
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", modeStr)
		os.Exit(1)
	}

	// Detect or set color mode
	colorMode := terminal.DetectColorMode()
	switch colorStr {
	case "auto":
		// Use detected
	case "true", "truecolor", "24":
		colorMode = terminal.ColorModeTrueColor
	case "256", "8":
		colorMode = terminal.ColorMode256
	}
	viewer.ColorMode = colorMode

	if !fitMode {
		viewer.ViewMode = ascimage.ViewActual
	}

	if zoomLevel != 100 {
		viewer.ViewMode = ascimage.ViewCustom
		viewer.ZoomLevel = zoomLevel
	}

	viewer.ShowStatus = !noStatus

	// Run interactive viewer
	if err := runViewer(viewer, colorMode); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: asc-image-viewer [options] <image>")
	fmt.Fprintln(os.Stderr, "\nSupported formats: PNG, JPEG, GIF")
	fmt.Fprintln(os.Stderr, "\nOptions:")
	flag.PrintDefaults()
	fmt.Fprintln(os.Stderr, "\nControls:")
	fmt.Fprintln(os.Stderr, "  q, Esc, Ctrl+C    Quit")
	fmt.Fprintln(os.Stderr, "  f                 Toggle fit/actual size")
	fmt.Fprintln(os.Stderr, "  m                 Toggle render mode (bg/quadrant)")
	fmt.Fprintln(os.Stderr, "  c                 Toggle color mode (24bit/256)")
	fmt.Fprintln(os.Stderr, "  +, =              Zoom in")
	fmt.Fprintln(os.Stderr, "  -, _              Zoom out")
	fmt.Fprintln(os.Stderr, "  0                 Reset zoom to 100%")
	fmt.Fprintln(os.Stderr, "  Arrow keys, hjkl  Pan viewport")
	fmt.Fprintln(os.Stderr, "  PgUp/PgDn         Pan vertically (large step)")
	fmt.Fprintln(os.Stderr, "  Home/End          Jump to left/right edge")
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

func runViewer(viewer *ascimage.Viewer, colorMode terminal.ColorMode) error {
	term := terminal.New(colorMode)

	if err := term.Init(); err != nil {
		return fmt.Errorf("terminal init: %w", err)
	}
	defer term.Fini()

	termW, termH := term.Size()
	buf := render.NewRenderBuffer(colorMode, termW, termH)

	// Initial conversion and render
	viewer.Update(termW, termH)
	renderFrame(viewer, buf, term, termW, termH)

	// Event loop
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
	// Pan step sizes
	smallStep := 1
	largeStep := 10
	pageStep := termH / 2

	switch ev.Key {
	// Quit
	case terminal.KeyEscape, terminal.KeyCtrlC, terminal.KeyCtrlD:
		return actionQuit
	case terminal.KeyRune:
		switch ev.Rune {
		case 'q', 'Q':
			return actionQuit

		// View mode
		case 'f', 'F':
			viewer.ToggleViewMode()
			return actionRedraw

		// Render mode
		case 'm', 'M':
			viewer.ToggleRenderMode()
			return actionRedraw

		// Color mode
		case 'c', 'C':
			viewer.ToggleColorMode()
			return actionRedraw

		// Status toggle
		case 's', 'S':
			viewer.ShowStatus = !viewer.ShowStatus
			return actionNone

		// Zoom
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

		// Vim-style navigation
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

		// Jump to edges
		case 'g':
			viewer.PanTo(0, 0, termW, termH)
		case 'G':
			viewer.PanTo(0, 999999, termW, termH) // clamp handles max
		}

	// Arrow keys
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

	// Page navigation
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
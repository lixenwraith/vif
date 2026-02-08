// Usage examples:
//
// # Interactive viewer with quadrant mode and truecolor
// ./png2ascii -m quadrant -c true image.png
//
// # Background-only mode with 256 colors, specify width
// ./png2ascii -m bg -c 256 -w 120 image.png
//
// # Output to file as ANSI sequences
// ./png2ascii -m quadrant -o output.ans image.png
//
// # Output to stdout (pipe to less -R, etc.)
// ./png2ascii -m bg -o - image.png | less -R

package main

import (
	"bufio"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

func main() {
	var (
		width    int
		modeStr  string
		depthStr string
		output   string
	)

	flag.IntVar(&width, "w", 0, "Output width in columns (0 = prompt interactively)")
	flag.StringVar(&modeStr, "m", "quadrant", "Render mode: 'bg' or 'quadrant'")
	flag.StringVar(&depthStr, "c", "true", "Color depth: 'true' (24-bit) or '256'")
	flag.StringVar(&output, "o", "", "Output ANSI to file (use '-' for stdout, omit for interactive)")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: png2ascii [options] <image.png>")
		fmt.Fprintln(os.Stderr, "\nOptions:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nModes:")
		fmt.Fprintln(os.Stderr, "  bg       - Background colors only (1 pixel per cell)")
		fmt.Fprintln(os.Stderr, "  quadrant - Quadrant characters with fg/bg (2x2 pixels per cell)")
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
	fmt.Fprintf(os.Stderr, "Image: %s (%dx%d)\n", imagePath, bounds.Dx(), bounds.Dy())

	// Parse render mode
	var mode RenderMode
	switch strings.ToLower(modeStr) {
	case "bg", "background":
		mode = ModeBackgroundOnly
	case "quadrant", "q":
		mode = ModeQuadrant
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s (use 'bg' or 'quadrant')\n", modeStr)
		os.Exit(1)
	}

	// Parse color depth
	var colorMode terminal.ColorMode
	switch strings.ToLower(depthStr) {
	case "true", "truecolor", "24", "24bit":
		colorMode = terminal.ColorModeTrueColor
	case "256", "8", "8bit":
		colorMode = terminal.ColorMode256
	default:
		fmt.Fprintf(os.Stderr, "Unknown color depth: %s (use 'true' or '256')\n", depthStr)
		os.Exit(1)
	}

	// Get width interactively if not specified
	if width <= 0 {
		width = promptWidth(bounds.Dx(), bounds.Dy())
	}

	// Convert image
	converted := ConvertImage(img, width, mode, colorMode)
	fmt.Fprintf(os.Stderr, "Output: %dx%d cells\n", converted.Width, converted.Height)

	// Output or display
	if output != "" {
		if err := writeANSI(converted, output, colorMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := displayInteractive(converted, colorMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
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

func promptWidth(srcW, srcH int) int {
	reader := bufio.NewReader(os.Stdin)
	aspectRatio := float64(srcH) / float64(srcW)

	for {
		fmt.Fprintf(os.Stderr, "Enter output width in columns (source: %dx%d): ", srcW, srcH)
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading input, using default 80")
			return 80
		}

		line = strings.TrimSpace(line)
		w, err := strconv.Atoi(line)
		if err != nil || w <= 0 {
			fmt.Fprintln(os.Stderr, "Please enter a positive integer")
			continue
		}

		// Show resulting height
		h := int(float64(w) * aspectRatio * 0.5)
		if h < 1 {
			h = 1
		}
		fmt.Fprintf(os.Stderr, "Output will be %dx%d cells\n", w, h)
		return w
	}
}

func displayInteractive(converted *ConvertedImage, colorMode terminal.ColorMode) error {
	term := terminal.New(colorMode)

	if err := term.Init(); err != nil {
		return fmt.Errorf("terminal init: %w", err)
	}
	defer term.Fini()

	termW, termH := term.Size()

	// Prepare display buffer (full terminal size)
	displayCells := make([]terminal.Cell, termW*termH)

	// Fill with black background
	for i := range displayCells {
		displayCells[i] = terminal.Cell{Rune: ' ', Bg: terminal.RGB{0, 0, 0}}
	}

	// Calculate centering offset
	offsetX := (termW - converted.Width) / 2
	offsetY := (termH - converted.Height) / 2
	if offsetX < 0 {
		offsetX = 0
	}
	if offsetY < 0 {
		offsetY = 0
	}

	// Copy converted cells into display buffer
	for y := 0; y < converted.Height && y+offsetY < termH; y++ {
		for x := 0; x < converted.Width && x+offsetX < termW; x++ {
			srcIdx := y*converted.Width + x
			dstIdx := (y+offsetY)*termW + (x + offsetX)
			displayCells[dstIdx] = converted.Cells[srcIdx]
		}
	}

	// Render
	term.Flush(displayCells, termW, termH)

	// Show help at bottom if space
	if termH > converted.Height+offsetY+1 {
		// We'd need to write help text, but for simplicity just wait for input
	}

	// Wait for quit key
	for {
		ev := term.PollEvent()
		switch ev.Type {
		case terminal.EventKey:
			switch ev.Key {
			case terminal.KeyEscape, terminal.KeyCtrlC, terminal.KeyCtrlD:
				return nil
			case terminal.KeyRune:
				if ev.Rune == 'q' || ev.Rune == 'Q' {
					return nil
				}
			}
		case terminal.EventResize:
			// Handle resize: recalculate and redraw
			termW, termH = term.Size()
			displayCells = make([]terminal.Cell, termW*termH)
			for i := range displayCells {
				displayCells[i] = terminal.Cell{Rune: ' ', Bg: terminal.RGB{0, 0, 0}}
			}
			offsetX = (termW - converted.Width) / 2
			offsetY = (termH - converted.Height) / 2
			if offsetX < 0 {
				offsetX = 0
			}
			if offsetY < 0 {
				offsetY = 0
			}
			for y := 0; y < converted.Height && y+offsetY < termH; y++ {
				for x := 0; x < converted.Width && x+offsetX < termW; x++ {
					srcIdx := y*converted.Width + x
					dstIdx := (y+offsetY)*termW + (x + offsetX)
					displayCells[dstIdx] = converted.Cells[srcIdx]
				}
			}
			term.Flush(displayCells, termW, termH)
		case terminal.EventError, terminal.EventClosed:
			return nil
		}
	}
}

// writeANSI outputs the converted image as ANSI escape sequences
func writeANSI(converted *ConvertedImage, output string, colorMode terminal.ColorMode) error {
	var w *bufio.Writer

	if output == "-" {
		w = bufio.NewWriter(os.Stdout)
	} else {
		f, err := os.Create(output)
		if err != nil {
			return err
		}
		defer f.Close()
		w = bufio.NewWriter(f)
	}
	defer w.Flush()

	var lastFg, lastBg terminal.RGB
	lastValid := false

	for y := 0; y < converted.Height; y++ {
		for x := 0; x < converted.Width; x++ {
			cell := converted.Cells[y*converted.Width+x]

			// Check if colors changed
			fgChanged := !lastValid || cell.Fg != lastFg
			bgChanged := !lastValid || cell.Bg != lastBg

			if fgChanged || bgChanged {
				// Emit SGR sequence
				w.WriteString("\x1b[0") // Reset first

				if colorMode == terminal.ColorMode256 {
					if cell.Attrs&terminal.AttrFg256 != 0 {
						fmt.Fprintf(w, ";38;5;%d", cell.Fg.R)
					}
					if cell.Attrs&terminal.AttrBg256 != 0 {
						fmt.Fprintf(w, ";48;5;%d", cell.Bg.R)
					}
				} else {
					fmt.Fprintf(w, ";38;2;%d;%d;%d", cell.Fg.R, cell.Fg.G, cell.Fg.B)
					fmt.Fprintf(w, ";48;2;%d;%d;%d", cell.Bg.R, cell.Bg.G, cell.Bg.B)
				}
				w.WriteByte('m')

				lastFg = cell.Fg
				lastBg = cell.Bg
				lastValid = true
			}

			// Write character
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			w.WriteRune(r)
		}
		w.WriteString("\x1b[0m\n") // Reset and newline
		lastValid = false
	}

	return nil
}
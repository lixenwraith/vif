package ascimage

import (
	"bufio"
	"fmt"
	"os"

	lcolor "github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// WriteANSI outputs converted image as ANSI escape sequences to file or stdout
func WriteANSI(converted *ConvertedImage, output string, colorMode terminal.ColorMode) error {
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

	var lastFg, lastBg lcolor.RGB
	lastValid := false

	for y := range converted.Height {
		for x := range converted.Width {
			cell := converted.Cells[y*converted.Width+x]

			fgChanged := !lastValid || cell.Fg != lastFg
			bgChanged := !lastValid || cell.Bg != lastBg

			if fgChanged || bgChanged {
				w.WriteString("\x1b[0")

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

			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			w.WriteRune(r)
		}
		w.WriteString("\x1b[0m\n")
		lastValid = false
	}

	return nil
}

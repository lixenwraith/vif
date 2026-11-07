package ascimage

import (
	"bufio"
	"io"
	"strconv"

	lcolor "github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// pen is one SGR color: the terminal's own, an RGB, or a palette index
type pen struct {
	kind uint8 // 0 the terminal's own color, 1 RGB, 2 palette
	rgb  lcolor.RGB
}

// WriteANSI writes img as lines of SGR-colored text, each cell's colors as its
// attributes say (true color, or the palette, its first 16 as the ANSI codes),
// a transparent cell in the terminal's own colors, every line reset at its end
func WriteANSI(w io.Writer, img *ConvertedImage) error {
	bw := bufio.NewWriter(w)
	sgr := make([]byte, 0, 48)
	for y := range img.Height {
		var fg, bg pen
		for _, cell := range img.Cells[y*img.Width : (y+1)*img.Width] {
			r := cell.Rune
			wantFg, wantBg := pen{}, pen{}
			if r != 0 {
				wantFg = penOf(cell.Fg, cell.Attrs&terminal.AttrFg256 != 0)
				wantBg = penOf(cell.Bg, cell.Attrs&terminal.AttrBg256 != 0)
			} else {
				r = ' '
			}
			if r == ' ' {
				wantFg = fg // a space shows no foreground
			}
			sgr = sgr[:0]
			if wantFg != fg {
				sgr = appendPen(sgr, wantFg, 30)
				fg = wantFg
			}
			if wantBg != bg {
				if len(sgr) > 0 {
					sgr = append(sgr, ';')
				}
				sgr = appendPen(sgr, wantBg, 40)
				bg = wantBg
			}
			if len(sgr) > 0 {
				bw.WriteString("\x1b[")
				bw.Write(sgr)
				bw.WriteByte('m')
			}
			bw.WriteRune(r)
		}
		bw.WriteString("\x1b[0m\n")
	}
	return bw.Flush()
}

func penOf(c lcolor.RGB, palette bool) pen {
	if palette {
		return pen{kind: 2, rgb: lcolor.RGB{R: c.R}}
	}
	return pen{kind: 1, rgb: c}
}

// appendPen appends p's SGR parameters at base 30 (foreground) or 40
// (background): the ANSI 16 as their own codes, the rest as 38/48
func appendPen(b []byte, p pen, base int) []byte {
	switch {
	case p.kind == 0:
		return strconv.AppendInt(b, int64(base+9), 10)
	case p.kind == 2 && p.rgb.R < 8:
		return strconv.AppendInt(b, int64(base+int(p.rgb.R)), 10)
	case p.kind == 2 && p.rgb.R < 16:
		return strconv.AppendInt(b, int64(base+60+int(p.rgb.R)-8), 10)
	case p.kind == 2:
		b = strconv.AppendInt(b, int64(base+8), 10)
		b = append(b, ";5;"...)
		return strconv.AppendInt(b, int64(p.rgb.R), 10)
	}
	b = strconv.AppendInt(b, int64(base+8), 10)
	b = append(b, ";2;"...)
	b = strconv.AppendInt(b, int64(p.rgb.R), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(p.rgb.G), 10)
	b = append(b, ';')
	return strconv.AppendInt(b, int64(p.rgb.B), 10)
}

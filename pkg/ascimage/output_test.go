package ascimage

import (
	"bytes"
	"testing"

	lcolor "github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// TestWriteANSIWritesEachCellAsItsAttributesSay: true color as 38;2/48;2, a
// palette entry as 38;5/48;5 but the ANSI 16 as their own codes, a space
// without its foreground, a transparent cell in the terminal's own colors
func TestWriteANSIWritesEachCellAsItsAttributesSay(t *testing.T) {
	img := &ConvertedImage{Width: 3, Height: 2, Cells: []terminal.Cell{
		{Rune: '▀', Fg: lcolor.RGB{R: 1, G: 2, B: 3}, Bg: lcolor.RGB{R: 4, G: 5, B: 6}},
		{Rune: '▀', Fg: lcolor.RGB{R: 1, G: 2, B: 3}, Bg: lcolor.RGB{R: 200}, Attrs: terminal.AttrBg256},
		{},
		{Rune: '▌', Fg: lcolor.RGB{R: 3}, Bg: lcolor.RGB{R: 12}, Attrs: terminal.AttrFg256 | terminal.AttrBg256},
		{Rune: ' ', Fg: lcolor.RGB{R: 9, G: 9, B: 9}, Bg: lcolor.RGB{R: 12}, Attrs: terminal.AttrBg256},
		{Rune: ' ', Bg: lcolor.RGB{R: 7, G: 7, B: 7}},
	}}
	var out bytes.Buffer
	if err := WriteANSI(&out, img); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[38;2;1;2;3;48;2;4;5;6m▀\x1b[48;5;200m▀\x1b[49m \x1b[0m\n" +
		"\x1b[33;104m▌ \x1b[48;2;7;7;7m \x1b[0m\n"
	if got := out.String(); got != want {
		t.Fatalf("WriteANSI = %q, want %q", got, want)
	}
}

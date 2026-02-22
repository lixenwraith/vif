package ascimage

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// DualModeImage holds both TrueColor and 256-color representations
type DualModeImage struct {
	Width      int
	Height     int
	RenderMode RenderMode
	AnchorX    int
	AnchorY    int
	Cells      []DualCell
}

// DualCell stores both color mode representations for one cell
type DualCell struct {
	Rune         rune
	TrueFg       terminal.RGB
	TrueBg       terminal.RGB
	Palette256Fg uint8
	Palette256Bg uint8
	Transparent  bool
}

// File format constants
const (
	dualMagic                 = "VFIMG"
	cellFlagTransparent uint8 = 1 << 0
	cellBytes                 = 13 // rune(4) + trueFg(3) + trueBg(3) + pal256Fg(1) + pal256Bg(1) + flags(1)
)

// ConvertImageDual converts image to both color modes in single pass
func ConvertImageDual(img image.Image, targetWidth int, mode RenderMode) *DualModeImage {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if srcW == 0 || srcH == 0 || targetWidth <= 0 {
		return &DualModeImage{Width: 0, Height: 0, RenderMode: mode}
	}

	aspectRatio := float64(srcH) / float64(srcW)
	charAspect := 0.5

	outW := targetWidth
	outH := int(float64(targetWidth) * aspectRatio * charAspect)
	if outH < 1 {
		outH = 1
	}

	cells := make([]DualCell, outW*outH)

	switch mode {
	case ModeBackgroundOnly:
		convertBackgroundDual(img, cells, outW, outH)
	case ModeQuadrant:
		convertQuadrantDual(img, cells, outW, outH)
	}

	return &DualModeImage{
		Width:      outW,
		Height:     outH,
		RenderMode: mode,
		Cells:      cells,
	}
}

func convertBackgroundDual(img image.Image, cells []DualCell, outW, outH int) {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			sx := bounds.Min.X + (x*srcW+srcW/2)/outW
			sy := bounds.Min.Y + (y*srcH+srcH/2)/outH

			if sx >= bounds.Max.X {
				sx = bounds.Max.X - 1
			}
			if sy >= bounds.Max.Y {
				sy = bounds.Max.Y - 1
			}

			idx := y*outW + x
			c := img.At(sx, sy)

			if colorIsTransparent(c) {
				cells[idx].Transparent = true
				continue
			}

			rgb := colorToRGB(c)
			cells[idx].Rune = ' '
			cells[idx].TrueBg = rgb
			cells[idx].Palette256Bg = terminal.RGBTo256(rgb)
		}
	}
}

func convertQuadrantDual(img image.Image, cells []DualCell, outW, outH int) {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	gridW := outW * 2
	gridH := outH * 2

	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			var pixels [4]terminal.RGB
			allTransparent := true

			gx := x * 2
			gy := y * 2

			offsets := [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}}

			for i, off := range offsets {
				sx := bounds.Min.X + ((gx+off[0])*srcW+srcW/2)/gridW
				sy := bounds.Min.Y + ((gy+off[1])*srcH+srcH/2)/gridH

				if sx >= bounds.Max.X {
					sx = bounds.Max.X - 1
				}
				if sy >= bounds.Max.Y {
					sy = bounds.Max.Y - 1
				}

				c := img.At(sx, sy)
				if !colorIsTransparent(c) {
					allTransparent = false
				}
				pixels[i] = colorToRGB(c)
			}

			idx := y*outW + x

			if allTransparent {
				cells[idx].Transparent = true
				continue
			}

			char, fg, bg := findBestQuadrant(pixels)

			cells[idx].Rune = char
			cells[idx].TrueFg = fg
			cells[idx].TrueBg = bg
			cells[idx].Palette256Fg = terminal.RGBTo256(fg)
			cells[idx].Palette256Bg = terminal.RGBTo256(bg)
		}
	}
}

func colorIsTransparent(c color.Color) bool {
	_, _, _, a := c.RGBA()
	return a == 0
}

// ToConvertedImage extracts single-mode ConvertedImage from dual representation
func (d *DualModeImage) ToConvertedImage(colorMode terminal.ColorMode) *ConvertedImage {
	cells := make([]terminal.Cell, len(d.Cells))

	for i, dc := range d.Cells {
		if dc.Transparent {
			continue
		}
		if colorMode == terminal.ColorMode256 {
			cells[i] = terminal.Cell{
				Rune:  dc.Rune,
				Fg:    terminal.RGB{R: dc.Palette256Fg},
				Bg:    terminal.RGB{R: dc.Palette256Bg},
				Attrs: terminal.AttrFg256 | terminal.AttrBg256,
			}
		} else {
			cells[i] = terminal.Cell{
				Rune: dc.Rune,
				Fg:   dc.TrueFg,
				Bg:   dc.TrueBg,
			}
		}
	}

	return &ConvertedImage{
		Cells:  cells,
		Width:  d.Width,
		Height: d.Height,
	}
}

// WriteDualMode writes dual-mode image to writer
// Format: readable header lines terminated by blank line, followed by binary cell data
func WriteDualMode(w io.Writer, img *DualModeImage) error {
	bw := bufio.NewWriter(w)

	fmt.Fprintf(bw, "%s\n", dualMagic)
	fmt.Fprintf(bw, "w:%d\n", img.Width)
	fmt.Fprintf(bw, "h:%d\n", img.Height)
	fmt.Fprintf(bw, "m:%d\n", img.RenderMode)
	fmt.Fprintf(bw, "ax:%d\n", img.AnchorX)
	fmt.Fprintf(bw, "ay:%d\n", img.AnchorY)
	fmt.Fprintf(bw, "\n")

	cellBuf := make([]byte, cellBytes)
	for _, cell := range img.Cells {
		binary.LittleEndian.PutUint32(cellBuf[0:4], uint32(cell.Rune))
		cellBuf[4] = cell.TrueFg.R
		cellBuf[5] = cell.TrueFg.G
		cellBuf[6] = cell.TrueFg.B
		cellBuf[7] = cell.TrueBg.R
		cellBuf[8] = cell.TrueBg.G
		cellBuf[9] = cell.TrueBg.B
		cellBuf[10] = cell.Palette256Fg
		cellBuf[11] = cell.Palette256Bg
		var flags uint8
		if cell.Transparent {
			flags |= cellFlagTransparent
		}
		cellBuf[12] = flags

		if _, err := bw.Write(cellBuf); err != nil {
			return err
		}
	}

	return bw.Flush()
}

// ReadDualMode reads dual-mode image from reader
func ReadDualMode(r io.Reader) (*DualModeImage, error) {
	br := bufio.NewReader(r)

	line, err := readHeaderLine(br)
	if err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if line != dualMagic {
		return nil, fmt.Errorf("invalid magic: %q", line)
	}

	img := &DualModeImage{}

	for {
		line, err = readHeaderLine(br)
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		if line == "" {
			break
		}

		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		switch key {
		case "w":
			img.Width, _ = strconv.Atoi(val)
		case "h":
			img.Height, _ = strconv.Atoi(val)
		case "m":
			m, _ := strconv.Atoi(val)
			img.RenderMode = RenderMode(m)
		case "ax":
			img.AnchorX, _ = strconv.Atoi(val)
		case "ay":
			img.AnchorY, _ = strconv.Atoi(val)
		}
	}

	if img.Width <= 0 || img.Height <= 0 {
		return nil, fmt.Errorf("invalid dimensions: %dx%d", img.Width, img.Height)
	}

	cellCount := img.Width * img.Height
	cells := make([]DualCell, cellCount)
	cellBuf := make([]byte, cellBytes)

	for i := 0; i < cellCount; i++ {
		if _, err := io.ReadFull(br, cellBuf); err != nil {
			return nil, fmt.Errorf("read cell %d: %w", i, err)
		}
		cells[i] = DualCell{
			Rune:         rune(binary.LittleEndian.Uint32(cellBuf[0:4])),
			TrueFg:       terminal.RGB{R: cellBuf[4], G: cellBuf[5], B: cellBuf[6]},
			TrueBg:       terminal.RGB{R: cellBuf[7], G: cellBuf[8], B: cellBuf[9]},
			Palette256Fg: cellBuf[10],
			Palette256Bg: cellBuf[11],
			Transparent:  cellBuf[12]&cellFlagTransparent != 0,
		}
	}

	img.Cells = cells
	return img, nil
}

func readHeaderLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// SaveDualMode writes dual-mode image to file
// Format: magic(5) + version(1) + width(2) + height(2) + mode(1) + cells(12 each)
func SaveDualMode(path string, img *DualModeImage) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return WriteDualMode(f, img)
}

// LoadDualMode reads dual-mode image from file
func LoadDualMode(path string) (*DualModeImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ReadDualMode(f)
}
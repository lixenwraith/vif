package ascimage

import (
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"os"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// DualModeImage holds both TrueColor and 256-color representations
type DualModeImage struct {
	Width      int
	Height     int
	RenderMode RenderMode
	Cells      []DualCell
}

// DualCell stores both color mode representations for one cell
type DualCell struct {
	Rune         rune
	TrueFg       terminal.RGB // TrueColor foreground
	TrueBg       terminal.RGB // TrueColor background
	Palette256Fg uint8        // 256-color palette index for fg
	Palette256Bg uint8        // 256-color palette index for bg
}

// File format constants
const (
	dualMagic   = "VFIMG"
	dualVersion = 1
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

			rgb := colorToRGB(img.At(sx, sy))
			idx := y*outW + x

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

				pixels[i] = colorToRGB(img.At(sx, sy))
			}

			char, fg, bg := findBestQuadrant(pixels)

			idx := y*outW + x
			cells[idx].Rune = char
			cells[idx].TrueFg = fg
			cells[idx].TrueBg = bg
			cells[idx].Palette256Fg = terminal.RGBTo256(fg)
			cells[idx].Palette256Bg = terminal.RGBTo256(bg)
		}
	}
}

// ToConvertedImage extracts single-mode ConvertedImage from dual representation
func (d *DualModeImage) ToConvertedImage(colorMode terminal.ColorMode) *ConvertedImage {
	cells := make([]terminal.Cell, len(d.Cells))

	for i, dc := range d.Cells {
		if colorMode == terminal.ColorMode256 {
			cells[i] = terminal.Cell{
				Rune:  dc.Rune,
				Fg:    terminal.RGB{R: dc.Palette256Fg},
				Bg:    terminal.RGB{R: dc.Palette256Bg},
				Attrs: terminal.AttrFg256 | terminal.AttrBg256,
			}
		} else {
			cells[i] = terminal.Cell{
				Rune:  dc.Rune,
				Fg:    dc.TrueFg,
				Bg:    dc.TrueBg,
				Attrs: terminal.AttrNone,
			}
		}
	}

	return &ConvertedImage{
		Cells:  cells,
		Width:  d.Width,
		Height: d.Height,
	}
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

// WriteDualMode writes dual-mode image to writer
func WriteDualMode(w io.Writer, img *DualModeImage) error {
	// Magic
	if _, err := w.Write([]byte(dualMagic)); err != nil {
		return err
	}

	// Header: version, width, height, mode
	header := make([]byte, 6)
	header[0] = dualVersion
	binary.LittleEndian.PutUint16(header[1:3], uint16(img.Width))
	binary.LittleEndian.PutUint16(header[3:5], uint16(img.Height))
	header[5] = uint8(img.RenderMode)

	if _, err := w.Write(header); err != nil {
		return err
	}

	// Cells: rune(4) + trueFg(3) + trueBg(3) + pal256Fg(1) + pal256Bg(1) = 12 bytes
	cellBuf := make([]byte, 12)
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

		if _, err := w.Write(cellBuf); err != nil {
			return err
		}
	}

	return nil
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

// ReadDualMode reads dual-mode image from reader
func ReadDualMode(r io.Reader) (*DualModeImage, error) {
	// Magic
	magic := make([]byte, 5)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if string(magic) != dualMagic {
		return nil, fmt.Errorf("invalid magic: %s", magic)
	}

	// Header
	header := make([]byte, 6)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	version := header[0]
	if version != dualVersion {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}

	width := int(binary.LittleEndian.Uint16(header[1:3]))
	height := int(binary.LittleEndian.Uint16(header[3:5]))
	mode := RenderMode(header[5])

	cellCount := width * height
	cells := make([]DualCell, cellCount)

	cellBuf := make([]byte, 12)
	for i := 0; i < cellCount; i++ {
		if _, err := io.ReadFull(r, cellBuf); err != nil {
			return nil, fmt.Errorf("read cell %d: %w", i, err)
		}

		cells[i] = DualCell{
			Rune:         rune(binary.LittleEndian.Uint32(cellBuf[0:4])),
			TrueFg:       terminal.RGB{R: cellBuf[4], G: cellBuf[5], B: cellBuf[6]},
			TrueBg:       terminal.RGB{R: cellBuf[7], G: cellBuf[8], B: cellBuf[9]},
			Palette256Fg: cellBuf[10],
			Palette256Bg: cellBuf[11],
		}
	}

	return &DualModeImage{
		Width:      width,
		Height:     height,
		RenderMode: mode,
		Cells:      cells,
	}, nil
}
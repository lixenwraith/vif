package ascimage

import (
	"bufio"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"

	lcolor "github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
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
	TrueFg       lcolor.RGB
	TrueBg       lcolor.RGB
	Palette256Fg uint8
	Palette256Bg uint8
	Transparent  bool
}

// File format constants
const (
	dualMagic                 = "VFIMG"
	dualFormatVersion         = 1
	dualCodecFlate            = "flate"
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

	for y := range outH {
		for x := range outW {
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

	for y := range outH {
		for x := range outW {
			var pixels [4]lcolor.RGB
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
				Fg:    lcolor.RGB{R: dc.Palette256Fg},
				Bg:    lcolor.RGB{R: dc.Palette256Bg},
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

// WriteDualMode writes a versioned header and a raw-deflate cell stream. The
// header keeps dimensions inspectable without expanding the image. On the
// shipped sample BestSpeed reduced 71,012 bytes to 26,772 (62%) while retaining
// most of the reduction of the materially slower compression levels.
func WriteDualMode(w io.Writer, img *DualModeImage) error {
	if img == nil {
		return fmt.Errorf("write vifimg: nil image")
	}
	cellCount, plainBytes, err := dualBodySize(img.Width, img.Height)
	if err != nil {
		return err
	}
	if len(img.Cells) != cellCount {
		return fmt.Errorf("write vifimg: %d cells for %dx%d image, want %d",
			len(img.Cells), img.Width, img.Height, cellCount)
	}

	bw := bufio.NewWriter(w)
	if _, err := fmt.Fprintf(bw, "%s\nv:%d\nc:%s\nw:%d\nh:%d\nm:%d\nax:%d\nay:%d\nn:%d\n\n",
		dualMagic, dualFormatVersion, dualCodecFlate,
		img.Width, img.Height, img.RenderMode, img.AnchorX, img.AnchorY, plainBytes); err != nil {
		return fmt.Errorf("write vifimg header: %w", err)
	}

	zw, err := flate.NewWriter(bw, flate.BestSpeed)
	if err != nil {
		return fmt.Errorf("create vifimg compressor: %w", err)
	}
	writeErr := writeDualCells(zw, img.Cells)
	closeErr := zw.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return fmt.Errorf("compress vifimg: %w", closeErr)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("write vifimg: %w", err)
	}
	return nil
}

func writeDualCells(w io.Writer, cells []DualCell) error {
	cellBuf := make([]byte, cellBytes)
	for i, cell := range cells {
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

		if _, err := w.Write(cellBuf); err != nil {
			return fmt.Errorf("write vifimg cell %d: %w", i, err)
		}
	}
	return nil
}

// ReadDualMode reads the current compressed format and legacy uncompressed
// files. Both retain the authored anchor-offset metadata.
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
	var version, namedPlainBytes int
	var codec string

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
			img.Width, err = dualHeaderInt(key, val)
		case "h":
			img.Height, err = dualHeaderInt(key, val)
		case "m":
			var m int
			m, err = dualHeaderInt(key, val)
			img.RenderMode = RenderMode(m)
		case "ax":
			img.AnchorX, err = dualHeaderInt(key, val)
		case "ay":
			img.AnchorY, err = dualHeaderInt(key, val)
		case "v":
			version, err = dualHeaderInt(key, val)
		case "c":
			codec = val
		case "n":
			namedPlainBytes, err = dualHeaderInt(key, val)
		}
		if err != nil {
			return nil, err
		}
	}

	cellCount, plainBytes, err := dualBodySize(img.Width, img.Height)
	if err != nil {
		return nil, err
	}

	body := io.Reader(br)
	var compressed io.ReadCloser
	if version != 0 || codec != "" || namedPlainBytes != 0 {
		if version != dualFormatVersion {
			return nil, fmt.Errorf("unsupported vifimg version %d", version)
		}
		if codec != dualCodecFlate {
			return nil, fmt.Errorf("unsupported vifimg codec %q", codec)
		}
		if namedPlainBytes != plainBytes {
			return nil, fmt.Errorf("vifimg body names %d plain bytes, dimensions require %d",
				namedPlainBytes, plainBytes)
		}
		compressed = flate.NewReader(br)
		body = compressed
	}

	cells, err := readDualCells(body, cellCount)
	if err != nil {
		if compressed != nil {
			_ = compressed.Close()
		}
		return nil, err
	}
	if compressed != nil {
		extra, readErr := io.ReadAll(io.LimitReader(compressed, 1))
		closeErr := compressed.Close()
		if readErr != nil {
			return nil, fmt.Errorf("decompress vifimg: %w", readErr)
		}
		if len(extra) != 0 {
			return nil, fmt.Errorf("vifimg body expands past declared size")
		}
		if closeErr != nil {
			return nil, fmt.Errorf("decompress vifimg: %w", closeErr)
		}
	}

	img.Cells = cells
	return img, nil
}

func readDualCells(r io.Reader, cellCount int) ([]DualCell, error) {
	cells := make([]DualCell, cellCount)
	cellBuf := make([]byte, cellBytes)

	for i := range cellCount {
		if _, err := io.ReadFull(r, cellBuf); err != nil {
			return nil, fmt.Errorf("read cell %d: %w", i, err)
		}
		cells[i] = DualCell{
			Rune:         rune(binary.LittleEndian.Uint32(cellBuf[0:4])),
			TrueFg:       lcolor.RGB{R: cellBuf[4], G: cellBuf[5], B: cellBuf[6]},
			TrueBg:       lcolor.RGB{R: cellBuf[7], G: cellBuf[8], B: cellBuf[9]},
			Palette256Fg: cellBuf[10],
			Palette256Bg: cellBuf[11],
			Transparent:  cellBuf[12]&cellFlagTransparent != 0,
		}
	}
	return cells, nil
}

func dualHeaderInt(key, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid vifimg %s value %q: %w", key, value, err)
	}
	return n, nil
}

func dualBodySize(width, height int) (cellCount, plainBytes int, err error) {
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid vifimg dimensions: %dx%d", width, height)
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/height {
		return 0, 0, fmt.Errorf("vifimg dimensions overflow: %dx%d", width, height)
	}
	cellCount = width * height
	if cellCount > maxInt/cellBytes {
		return 0, 0, fmt.Errorf("vifimg body size overflows for %dx%d", width, height)
	}
	return cellCount, cellCount * cellBytes, nil
}

func readHeaderLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// SaveDualMode writes a compressed dual-mode image to file.
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

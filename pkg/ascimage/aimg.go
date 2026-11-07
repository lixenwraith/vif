package ascimage

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	lcolor "github.com/lixenwraith/color"
)

// Extension names a plain .aimg and ExtensionGzip one gzipped whole, the
// forms doc/format.md specifies; a reader tells them apart by their bytes
const (
	Extension     = ".aimg"
	ExtensionGzip = ".aimgz"
)

// MaxSide and MaxCells bound a grid: ReadDualMode refuses a header past them
// before it allocates, and WriteDualMode never writes a file it would refuse
const (
	MaxSide  = 1 << 14
	MaxCells = 1 << 22
)

const (
	magic               = "ASCIMG"
	formatVersion       = 1
	cellFlagTransparent = 1 << 0
	cellBytes           = 13 // rune 4, true fg 3, true bg 3, 256 fg 1, 256 bg 1, flags 1
	maxHeaderLine       = 64
	maxHeaderLines      = 16
)

// headerKeys are the keys a version 1 header must hold; ax and ay may be left out
var headerKeys = []string{"v", "w", "h", "m"}

// WriteDualMode writes img as a plain .aimg, the text header and then its cells
func WriteDualMode(w io.Writer, img *DualModeImage) error {
	if img == nil {
		return errors.New("aimg: nil image")
	}
	cellCount, err := gridCells(img.Width, img.Height)
	if err != nil {
		return err
	}
	if len(img.Cells) != cellCount {
		return fmt.Errorf("aimg: %d cells for a %dx%d image, want %d",
			len(img.Cells), img.Width, img.Height, cellCount)
	}
	if img.RenderMode > ModeQuadrant {
		return fmt.Errorf("aimg: unknown render mode %d", img.RenderMode)
	}
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "%s\nv:%d\nw:%d\nh:%d\nm:%d\nax:%d\nay:%d\n\n",
		magic, formatVersion, img.Width, img.Height, img.RenderMode, img.AnchorX, img.AnchorY)
	if err := writeCells(bw, img.Cells); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("aimg: %w", err)
	}
	return nil
}

func writeCells(w io.Writer, cells []DualCell) error {
	buf := make([]byte, 0, cellBytes*512)
	for i, cell := range cells {
		if cell.Rune != 0 && !utf8.ValidRune(cell.Rune) {
			return fmt.Errorf("aimg: cell %d holds no rune (%#x)", i, cell.Rune)
		}
		var flags byte
		if cell.Transparent {
			flags |= cellFlagTransparent
		}
		buf = binary.LittleEndian.AppendUint32(buf, uint32(cell.Rune))
		buf = append(buf, cell.TrueFg.R, cell.TrueFg.G, cell.TrueFg.B,
			cell.TrueBg.R, cell.TrueBg.G, cell.TrueBg.B,
			cell.Palette256Fg, cell.Palette256Bg, flags)
		if len(buf) == cap(buf) || i == len(cells)-1 {
			if _, err := w.Write(buf); err != nil {
				return fmt.Errorf("aimg: write cells: %w", err)
			}
			buf = buf[:0]
		}
	}
	return nil
}

// Gzipped reports whether bytes that open a file are gzip's, an .aimgz's
func Gzipped(head []byte) bool {
	return len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b
}

// ReadDualMode reads an .aimg, plain or gzipped, as its bytes say whatever
// its name, refusing all that version 1 does not define: another magic or
// version, a header past its limits or missing a key, cells that hold no
// rune or that are fewer or more than it names, a gzip stream that fails
func ReadDualMode(r io.Reader) (*DualModeImage, error) {
	br := bufio.NewReader(r)
	if head, _ := br.Peek(2); Gzipped(head) {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("aimg: gzip: %w", err)
		}
		defer zr.Close()
		br = bufio.NewReader(zr)
	}

	line, err := headerLine(br)
	if err != nil {
		return nil, fmt.Errorf("aimg: %w", err)
	}
	if line != magic {
		return nil, fmt.Errorf("aimg: not an .aimg file (magic %q)", line)
	}
	img := &DualModeImage{}
	var version int
	seen := make(map[string]bool, len(headerKeys)+2)
	for n := 0; ; n++ {
		if n == maxHeaderLines {
			return nil, errors.New("aimg: header runs past its last line")
		}
		if line, err = headerLine(br); err != nil {
			return nil, fmt.Errorf("aimg: %w", err)
		}
		if line == "" {
			break
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("aimg: header line %q has no key", line)
		}
		if seen[key] {
			return nil, fmt.Errorf("aimg: header key %q twice", key)
		}
		seen[key] = true
		switch key {
		case "v":
			version, err = headerInt(key, val)
		case "w":
			img.Width, err = headerInt(key, val)
		case "h":
			img.Height, err = headerInt(key, val)
		case "m":
			var m int
			m, err = headerInt(key, val)
			if err == nil && (m < 0 || m > int(ModeQuadrant)) {
				err = fmt.Errorf("aimg: unknown render mode %d", m)
			}
			img.RenderMode = RenderMode(m)
		case "ax":
			img.AnchorX, err = headerInt(key, val)
		case "ay":
			img.AnchorY, err = headerInt(key, val)
		}
		if err != nil {
			return nil, err
		}
	}
	for _, key := range headerKeys {
		if !seen[key] {
			return nil, fmt.Errorf("aimg: header lacks %q", key)
		}
	}
	if version != formatVersion {
		return nil, fmt.Errorf("aimg: unsupported version %d", version)
	}
	cellCount, err := gridCells(img.Width, img.Height)
	if err != nil {
		return nil, err
	}
	if img.Cells, err = readCells(br, cellCount); err != nil {
		return nil, err
	}
	// A gzip stream checks its CRC-32 and length as it ends
	if _, err := br.ReadByte(); err != io.EOF {
		if err == nil {
			err = errors.New("data follows the cells")
		}
		return nil, fmt.Errorf("aimg: %w", err)
	}
	return img, nil
}

func readCells(r io.Reader, cellCount int) ([]DualCell, error) {
	cells := make([]DualCell, cellCount)
	buf := make([]byte, cellBytes*512)
	for i := 0; i < cellCount; {
		chunk := buf[:min(len(buf), (cellCount-i)*cellBytes)]
		if _, err := io.ReadFull(r, chunk); err != nil {
			return nil, fmt.Errorf("aimg: read cell %d: %w", i, err)
		}
		for b := chunk; len(b) > 0; b, i = b[cellBytes:], i+1 {
			ru := rune(binary.LittleEndian.Uint32(b[0:4]))
			if ru != 0 && !utf8.ValidRune(ru) {
				return nil, fmt.Errorf("aimg: cell %d holds no rune (%#x)", i, uint32(ru))
			}
			if b[12]&^cellFlagTransparent != 0 {
				return nil, fmt.Errorf("aimg: cell %d sets unknown flags %#x", i, b[12])
			}
			cells[i] = DualCell{
				Rune:         ru,
				TrueFg:       lcolor.RGB{R: b[4], G: b[5], B: b[6]},
				TrueBg:       lcolor.RGB{R: b[7], G: b[8], B: b[9]},
				Palette256Fg: b[10],
				Palette256Bg: b[11],
				Transparent:  b[12]&cellFlagTransparent != 0,
			}
		}
	}
	return cells, nil
}

// headerLine reads one line of at most maxHeaderLine bytes, its newline dropped
func headerLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxHeaderLine+1 {
		return "", errors.New("header line too long")
	}
	if err != nil {
		return "", fmt.Errorf("header: %w", err)
	}
	return string(line[:len(line)-1]), nil
}

func headerInt(key, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("aimg: header %s: %q is not a number", key, value)
	}
	return n, nil
}

// gridCells is a grid's cell count, inside the limits
func gridCells(width, height int) (int, error) {
	if width < 1 || height < 1 || width > MaxSide || height > MaxSide || width*height > MaxCells {
		return 0, fmt.Errorf("aimg: %dx%d cells: a side is 1 to %d and a grid at most %d",
			width, height, MaxSide, MaxCells)
	}
	return width * height, nil
}

// SaveDualMode writes img to path, gzipped at gzip's default level when the
// path ends in ExtensionGzip; a failed write removes the file it began
func SaveDualMode(path string, img *DualModeImage) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Ext(path), ExtensionGzip) {
		zw := gzip.NewWriter(f)
		if err = WriteDualMode(zw, img); err == nil {
			err = zw.Close()
		}
	} else {
		err = WriteDualMode(f, img)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// LoadDualMode reads the .aimg at path, plain or gzipped
func LoadDualMode(path string) (*DualModeImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadDualMode(f)
}

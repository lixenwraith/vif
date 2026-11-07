package ascimage

import (
	"bufio"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	lcolor "github.com/lixenwraith/color"
)

// Extension names an .aimg file, the format doc/format.md specifies
const Extension = ".aimg"

// MaxSide and MaxCells bound a grid: ReadDualMode refuses a header past them
// before it allocates, and WriteDualMode never writes a file it would refuse
const (
	MaxSide  = 1 << 14
	MaxCells = 1 << 22
)

const (
	magic               = "ASCIMG"
	formatVersion       = 1
	codecZlib           = "zlib"
	cellFlagTransparent = 1 << 0
	cellBytes           = 13 // rune 4, true fg 3, true bg 3, 256 fg 1, 256 bg 1, flags 1
	maxHeaderLine       = 64
	maxHeaderLines      = 16
)

// headerKeys are the keys a version 1 header must hold; ax and ay may be left out
var headerKeys = []string{"v", "c", "w", "h", "m", "n"}

// WriteDualMode writes img as an .aimg: the text header, then the cells under
// zlib at BestSpeed, which keeps most of what the slower levels save: on
// vif's 140x39 sample, 70,980 plain bytes take 26,778
func WriteDualMode(w io.Writer, img *DualModeImage) error {
	if img == nil {
		return errors.New("aimg: nil image")
	}
	cellCount, plainBytes, err := bodySize(img.Width, img.Height)
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
	fmt.Fprintf(bw, "%s\nv:%d\nc:%s\nw:%d\nh:%d\nm:%d\nax:%d\nay:%d\nn:%d\n\n",
		magic, formatVersion, codecZlib, img.Width, img.Height, img.RenderMode,
		img.AnchorX, img.AnchorY, plainBytes)
	zw, err := zlib.NewWriterLevel(bw, zlib.BestSpeed)
	if err != nil {
		return fmt.Errorf("aimg: %w", err)
	}
	if err := writeCells(zw, img.Cells); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("aimg: compress: %w", err)
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

// ReadDualMode reads an .aimg, refusing all that version 1 does not define:
// another magic or version, a header past its limits or missing a key, and
// cells that fail the body's checksum, outrun its size or hold no rune
func ReadDualMode(r io.Reader) (*DualModeImage, error) {
	br := bufio.NewReader(r)
	line, err := headerLine(br)
	if err != nil {
		return nil, fmt.Errorf("aimg: %w", err)
	}
	if line != magic {
		return nil, fmt.Errorf("aimg: not an .aimg file (magic %q)", line)
	}

	img := &DualModeImage{}
	var version, plain int
	var codec string
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
		case "c":
			codec = val
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
		case "n":
			plain, err = headerInt(key, val)
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
	if codec != codecZlib {
		return nil, fmt.Errorf("aimg: unsupported codec %q", codec)
	}
	cellCount, plainBytes, err := bodySize(img.Width, img.Height)
	if err != nil {
		return nil, err
	}
	if plain != plainBytes {
		return nil, fmt.Errorf("aimg: body names %d plain bytes, %dx%d cells take %d",
			plain, img.Width, img.Height, plainBytes)
	}

	zr, err := zlib.NewReader(br)
	if err != nil {
		return nil, fmt.Errorf("aimg: body: %w", err)
	}
	defer zr.Close()
	if img.Cells, err = readCells(zr, cellCount); err != nil {
		return nil, err
	}
	// Reading on to the stream's end checks its Adler-32; a body that expands
	// past its size leaves the rest of its stream, as trailing bytes would
	if _, err := io.Copy(io.Discard, io.LimitReader(zr, 1)); err != nil {
		return nil, fmt.Errorf("aimg: body: %w", err)
	}
	if _, err := br.ReadByte(); err != io.EOF {
		return nil, errors.New("aimg: data follows the declared body")
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

// bodySize is a grid's cell count and plain body bytes, inside the limits
func bodySize(width, height int) (cellCount, plainBytes int, err error) {
	if width < 1 || height < 1 || width > MaxSide || height > MaxSide || width*height > MaxCells {
		return 0, 0, fmt.Errorf("aimg: %dx%d cells: a side is 1 to %d and a grid at most %d",
			width, height, MaxSide, MaxCells)
	}
	return width * height, width * height * cellBytes, nil
}

// SaveDualMode writes img to path; a failed write removes the file it began
func SaveDualMode(path string, img *DualModeImage) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = WriteDualMode(f, img)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// LoadDualMode reads the .aimg at path
func LoadDualMode(path string) (*DualModeImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadDualMode(f)
}

package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lixenwraith/ascimage"
	"github.com/lixenwraith/terminal"
)

// maxPixels bounds a raster before it is decoded: a header can claim far more
// than its file holds, and the decoder would allocate it
const maxPixels = 1 << 27

// extensions are the names the browser lists and a bare path opens in view;
// what a file holds is read from its bytes
var extensions = []string{".png", ".jpg", ".jpeg", ".gif", ascimage.Extension, ascimage.ExtensionGzip}

// source is one input: a raster to convert, or an .aimg's cells
type source struct {
	path   string
	size   int64  // file bytes
	format string // png, jpeg, gif, aimg or aimgz
	w, h   int    // a raster's pixels, an .aimg's cells
	mtime  time.Time
	img    image.Image
	dual   *ascimage.DualModeImage

	conv      *ascimage.ConvertedImage // an .aimg's cells in convDepth, for the view
	convDepth terminal.ColorMode
}

func (s *source) dims() (int, int) { return s.w, s.h }

func (s *source) describe() string {
	w, h := s.dims()
	if s.dual != nil {
		return fmt.Sprintf(".%s, %dx%d cells, %s, anchor %d,%d, %s", s.format,
			w, h, strings.ToLower(s.dual.RenderMode.String()), s.dual.AnchorX, s.dual.AnchorY, humanSize(s.size))
	}
	return fmt.Sprintf("%s, %dx%d pixels, %s", strings.ToUpper(s.format), w, h, humanSize(s.size))
}

// openable reports whether a path names a directory or a file the browser lists
func openable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir() || fi.Mode().IsRegular() && listed(path)
}

func listed(name string) bool {
	ext := filepath.Ext(name)
	for _, e := range extensions {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// load reads a source by its bytes, whatever its name: an .aimg by its magic,
// an .aimgz by gzip's, a raster otherwise
func load(path string) (*source, error) { return open(path, true) }

// probe reads a raster's header alone, and an .aimg whole
func probe(path string) (*source, error) { return open(path, false) }

func open(path string, decode bool) (*source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	s := &source{path: path, size: fi.Size(), mtime: fi.ModTime()}
	br := bufio.NewReader(f)
	head, _ := br.Peek(len("ASCIMG\n"))
	if gz := ascimage.Gzipped(head); gz || bytes.Equal(head, []byte("ASCIMG\n")) {
		s.format = "aimg"
		if gz {
			s.format = "aimgz"
		}
		if s.dual, err = ascimage.ReadDualMode(br); err != nil {
			return nil, wrap(path, err)
		}
		s.w, s.h = s.dual.Width, s.dual.Height
		return s, nil
	}
	cfg, format, err := image.DecodeConfig(br)
	if err != nil {
		return nil, wrap(path, decodeError(err))
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, fmt.Errorf("%s: %dx%d pixels, past the %d this reads", path, cfg.Width, cfg.Height, maxPixels)
	}
	s.format, s.w, s.h = format, cfg.Width, cfg.Height
	if !decode {
		return s, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if s.img, _, err = image.Decode(bufio.NewReader(f)); err != nil {
		return nil, wrap(path, err)
	}
	return s, nil
}

func decodeError(err error) error {
	if errors.Is(err, image.ErrFormat) {
		return errors.New("not a PNG, JPEG, GIF, .aimg or .aimgz")
	}
	return err
}

func wrap(path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", path, err)
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

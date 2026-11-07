package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lixenwraith/ascimage"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 99, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestConvertWritesTheFormItsOutputNames: .aimg is plain, .aimgz gzipped,
// another name ANSI text, a raster's extension refused; what is written
// reads back by its bytes under a name that lies
func TestConvertWritesTheFormItsOutputNames(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	writePNG(t, in, 64, 40)
	for _, out := range []string{"a.aimg", "b.aimgz", "c.ans"} {
		if code := run([]string{"convert", "-w", "20", in, filepath.Join(dir, out)}); code != 0 {
			t.Fatalf("convert to %s: status %d", out, code)
		}
	}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if !bytes.HasPrefix(read("a.aimg"), []byte("ASCIMG\n")) || !ascimage.Gzipped(read("b.aimgz")) ||
		!bytes.HasPrefix(read("c.ans"), []byte("\x1b[")) {
		t.Fatal("an output is not the form its name asks for")
	}
	liar := filepath.Join(dir, "liar.png")
	if err := os.Rename(filepath.Join(dir, "b.aimgz"), liar); err != nil {
		t.Fatal(err)
	}
	if s, err := load(liar); err != nil || s.format != "aimgz" || s.w != 20 {
		t.Fatalf("an .aimgz named .png loads as %+v, %v", s, err)
	}
	if code := run([]string{"convert", in, filepath.Join(dir, "x.png")}); code != 2 {
		t.Fatalf("convert to a PNG: status %d, want 2", code)
	}
}

// TestZoomKeepsThePointUnderThePointer: the image's point under the pointer
// stays under it across a zoom, to a cell, wherever the view was panned
func TestZoomKeepsThePointUnderThePointer(t *testing.T) {
	s := &source{w: 1000, h: 600, img: image.NewRGBA(image.Rect(0, 0, 1000, 600))}
	lk := look{mode: ascimage.ModeQuadrant, aspect: ascimage.DefaultAspect}
	v := newView(false, 100)
	f := v.frame(s, 80, 24, lk)
	v.pan(f, 137, 41)
	f = v.frame(s, 80, 24, lk)
	at := func(f frame, mx, my int) (float64, float64) {
		return (float64(f.ox+mx) + 0.5) / float64(f.gw), (float64(f.oy+my) + 0.5) / float64(f.gh)
	}
	for _, pct := range []int{200, 50, 125} {
		mx, my := 13, 7
		x0, y0 := at(f, mx, my)
		v.zoomTo(s, f, pct, mx, my, lk)
		g := v.frame(s, 80, 24, lk)
		x1, y1 := at(g, mx, my)
		if math.Abs(x1-x0) > 1/float64(g.gw) || math.Abs(y1-y0) > 1/float64(g.gh) {
			t.Fatalf("zoom to %d%%: the point moved from %.4f,%.4f to %.4f,%.4f", pct, x0, y0, x1, y1)
		}
		f = g
	}
}

// TestALoaderDecodesAChangedFileAgain: a file rewritten since it was decoded
// is decoded again rather than shown as it was
func TestALoaderDecodesAChangedFileAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	l := newLoader(func() {})
	decoded := func() *source {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		for range 2000 {
			l.want(path)
			if s, err := l.get(path, fi.Size(), fi.ModTime()); s != nil || err != nil {
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("no decode")
		return nil
	}
	writePNG(t, path, 10, 10)
	first := decoded()
	writePNG(t, path, 30, 10)
	later := time.Now().Add(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if again := decoded(); again == first || again.w != 30 {
		t.Fatalf("after a rewrite the loader holds a %dx%d image, want 30x10", again.w, again.h)
	}
}

package ascimage

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	lcolor "github.com/lixenwraith/color"
)

func sampleImage() *DualModeImage {
	return &DualModeImage{
		Width: 2, Height: 1, RenderMode: ModeQuadrant, AnchorX: 5, AnchorY: -3,
		Cells: []DualCell{
			{Rune: '▞', TrueFg: lcolor.RGB{R: 1, G: 2, B: 3}, TrueBg: lcolor.RGB{R: 4, G: 5, B: 6}, Palette256Fg: 7, Palette256Bg: 8},
			{Transparent: true},
		},
	}
}

// TestBothFormsReadBackWhateverTheirName: a plain .aimg and an .aimgz read
// back as the image written, each saved under the other's extension too
func TestBothFormsReadBackWhateverTheirName(t *testing.T) {
	want := sampleImage()
	dir := t.TempDir()
	for _, saved := range []string{"x" + Extension, "x" + ExtensionGzip} {
		path := filepath.Join(dir, saved)
		if err := SaveDualMode(path, want); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if gz := strings.HasSuffix(saved, ExtensionGzip); Gzipped(raw) != gz {
			t.Fatalf("%s: gzipped %v, want %v", saved, !gz, gz)
		}
		swapped := filepath.Join(dir, "swapped"+map[bool]string{true: Extension, false: ExtensionGzip}[strings.HasSuffix(saved, ExtensionGzip)])
		if err := os.WriteFile(swapped, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{path, swapped} {
			got, err := LoadDualMode(p)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s reads %#v, want %#v", p, got, want)
			}
		}
	}
	var plain bytes.Buffer
	if err := WriteDualMode(&plain, want); err != nil {
		t.Fatal(err)
	}
	if head := "ASCIMG\nv:1\nw:2\nh:1\nm:1\nax:5\nay:-3\n\n"; !strings.HasPrefix(plain.String(), head) || plain.Len() != len(head)+2*cellBytes {
		t.Fatalf("plain file = %q, want %q and two cells", plain.Bytes(), head)
	}
}

func plainCells(cells ...DualCell) []byte {
	var b bytes.Buffer
	if err := writeCells(&b, cells); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func gz(b []byte) []byte {
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	zw.Write(b)
	zw.Close()
	return out.Bytes()
}

func TestReadDualModeRefusesWhatVersion1DoesNotDefine(t *testing.T) {
	one := plainCells(DualCell{Rune: ' '})
	head := func(lines ...string) string { return "ASCIMG\n" + strings.Join(lines, "\n") + "\n\n" }
	valid := head("v:1", "w:1", "h:1", "m:0")
	file := func(header string, cells []byte) []byte { return append([]byte(header), cells...) }
	badCRC := gz(file(valid, one))
	badCRC[len(badCRC)-5] ^= 1
	badRune := plainCells(DualCell{Rune: ' '})
	badRune[0], badRune[1], badRune[2], badRune[3] = 0x00, 0xd8, 0x00, 0x00 // U+D800, a surrogate
	badFlags := plainCells(DualCell{Rune: ' '})
	badFlags[12] = 0x80

	for name, data := range map[string][]byte{
		"vif's old magic":         file("VFIMG\nv:1\nw:1\nh:1\nm:0\n\n", one),
		"no version":              file(head("w:1", "h:1", "m:0"), one),
		"another version":         file(head("v:2", "w:1", "h:1", "m:0"), one),
		"no render mode":          file(head("v:1", "w:1", "h:1"), one),
		"an unknown mode":         file(head("v:1", "w:1", "h:1", "m:2"), one),
		"a key twice":             file(head("v:1", "w:1", "w:1", "h:1", "m:0"), one),
		"a line with no key":      file(head("v:1", "w:1", "h:1", "m:0", "loose"), one),
		"a side past MaxSide":     file(head("v:1", fmt.Sprintf("w:%d", MaxSide+1), "h:1", "m:0"), one),
		"a grid past MaxCells":    file(head("v:1", "w:4096", "h:4096", "m:0"), one),
		"a long header line":      file(head("v:1", "w:1", "h:1", "m:0", "x:"+strings.Repeat("9", 80)), one),
		"an endless header":       file("ASCIMG\n"+strings.Repeat("x:1\n", maxHeaderLines+1)+"\n", one),
		"too few cells":           file(head("v:1", "w:2", "h:1", "m:0"), one),
		"data after the cells":    append(file(valid, one), 0),
		"a cell with no rune":     file(valid, badRune),
		"an unknown cell flag":    file(valid, badFlags),
		"a failed gzip checksum":  badCRC,
		"a cut gzip stream":       gz(file(valid, one))[:30],
		"bytes after the gzip":    append(gz(file(valid, one)), 1, 2, 3),
		"gzipped cells too many":  gz(file(valid, plainCells(DualCell{Rune: ' '}, DualCell{Rune: ' '}))),
		"gzip around no .aimg":    gz([]byte("PNG pretending\n")),
		"an empty file":           {},
		"a header and no newline": []byte("ASCIMG"),
	} {
		if img, err := ReadDualMode(bytes.NewReader(data)); err == nil {
			t.Errorf("%s: read %dx%d, want an error", name, img.Width, img.Height)
		}
	}
	for _, ok := range [][]byte{file(valid, one), gz(file(valid, one))} {
		if _, err := ReadDualMode(bytes.NewReader(ok)); err != nil {
			t.Fatalf("a valid control: %v", err)
		}
	}
}

// FuzzReadDualMode: no input panics the reader, and what it accepts it writes
// back as the same image
func FuzzReadDualMode(f *testing.F) {
	var plain bytes.Buffer
	if err := WriteDualMode(&plain, sampleImage()); err != nil {
		f.Fatal(err)
	}
	f.Add(plain.Bytes())
	f.Add(gz(plain.Bytes()))
	f.Fuzz(func(t *testing.T, data []byte) {
		img, err := ReadDualMode(bytes.NewReader(data))
		if err != nil {
			return
		}
		var again bytes.Buffer
		if err := WriteDualMode(&again, img); err != nil {
			t.Fatalf("an accepted image does not write: %v", err)
		}
		back, err := ReadDualMode(&again)
		if err != nil || !reflect.DeepEqual(back, img) {
			t.Fatalf("rewritten image reads %v, %v; want %#v", back, err, img)
		}
	})
}

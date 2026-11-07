package ascimage

import (
	"bytes"
	"compress/zlib"
	"fmt"
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

func TestDualModeRoundTrip(t *testing.T) {
	want := sampleImage()
	var encoded bytes.Buffer
	if err := WriteDualMode(&encoded, want); err != nil {
		t.Fatal(err)
	}
	if head := "ASCIMG\nv:1\nc:zlib\nw:2\nh:1\nm:1\nax:5\nay:-3\nn:26\n\n"; !bytes.HasPrefix(encoded.Bytes(), []byte(head)) {
		t.Fatalf("header = %q, want %q", encoded.Bytes()[:min(encoded.Len(), len(head))], head)
	}
	got, err := ReadDualMode(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

// file is a header and the plain cells under zlib, as WriteDualMode lays them
func file(header string, plain []byte) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	zw := zlib.NewWriter(&b)
	zw.Write(plain)
	zw.Close()
	return b.Bytes()
}

func plainCells(cells ...DualCell) []byte {
	var b bytes.Buffer
	if err := writeCells(&b, cells); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func TestReadDualModeRefusesWhatVersion1DoesNotDefine(t *testing.T) {
	one := plainCells(DualCell{Rune: ' '})
	head := func(lines ...string) string { return "ASCIMG\n" + strings.Join(lines, "\n") + "\n\n" }
	valid := head("v:1", "c:zlib", "w:1", "h:1", "m:0", "n:13")
	var good bytes.Buffer
	if err := WriteDualMode(&good, sampleImage()); err != nil {
		t.Fatal(err)
	}
	badSum := bytes.Clone(good.Bytes())
	badSum[len(badSum)-1] ^= 1
	badRune := plainCells(DualCell{Rune: ' '})
	badRune[0], badRune[1], badRune[2], badRune[3] = 0x00, 0xd8, 0x00, 0x00 // U+D800, a surrogate
	badFlags := plainCells(DualCell{Rune: ' '})
	badFlags[12] = 0x80

	for name, data := range map[string][]byte{
		"vif's old magic":      file("VFIMG\nv:1\nc:zlib\nw:1\nh:1\nm:0\nn:13\n\n", one),
		"no version":           file(head("c:zlib", "w:1", "h:1", "m:0", "n:13"), one),
		"another version":      file(head("v:2", "c:zlib", "w:1", "h:1", "m:0", "n:13"), one),
		"a raw deflate body":   file(head("v:1", "c:flate", "w:1", "h:1", "m:0", "n:13"), one),
		"no render mode":       file(head("v:1", "c:zlib", "w:1", "h:1", "n:13"), one),
		"an unknown mode":      file(head("v:1", "c:zlib", "w:1", "h:1", "m:2", "n:13"), one),
		"a key twice":          file(head("v:1", "c:zlib", "w:1", "w:1", "h:1", "m:0", "n:13"), one),
		"a line with no key":   file(head("v:1", "c:zlib", "w:1", "h:1", "m:0", "n:13", "loose"), one),
		"a side past MaxSide":  file(head("v:1", "c:zlib", fmt.Sprintf("w:%d", MaxSide+1), "h:1", "m:0", fmt.Sprintf("n:%d", (MaxSide+1)*13)), one),
		"a grid past MaxCells": file(head("v:1", "c:zlib", "w:4096", "h:4096", "m:0", fmt.Sprintf("n:%d", 4096*4096*13)), one),
		"a plain size off":     file(head("v:1", "c:zlib", "w:1", "h:1", "m:0", "n:12"), one),
		"a long header line":   file(head("v:1", "c:zlib", "w:1", "h:1", "m:0", "n:13", "x:"+strings.Repeat("9", 80)), one),
		"an endless header":    file("ASCIMG\n"+strings.Repeat("x:1\n", maxHeaderLines+1)+"\n", one),
		"a failed checksum":    badSum,
		"a body past its size": file(valid, plainCells(DualCell{Rune: ' '}, DualCell{Rune: ' '})),
		"a short body":         file(head("v:1", "c:zlib", "w:2", "h:1", "m:0", "n:26"), one),
		"bytes after the body": append(file(valid, one), 0),
		"a cell with no rune":  file(valid, badRune),
		"an unknown cell flag": file(valid, badFlags),
	} {
		if img, err := ReadDualMode(bytes.NewReader(data)); err == nil {
			t.Errorf("%s: read %dx%d, want an error", name, img.Width, img.Height)
		}
	}
	if _, err := ReadDualMode(bytes.NewReader(file(valid, one))); err != nil {
		t.Fatalf("the valid control: %v", err)
	}
}

// FuzzReadDualMode: no input panics the reader, and what it accepts it writes
// back as the same image
func FuzzReadDualMode(f *testing.F) {
	var good bytes.Buffer
	if err := WriteDualMode(&good, sampleImage()); err != nil {
		f.Fatal(err)
	}
	f.Add(good.Bytes())
	f.Add(file("ASCIMG\nv:1\nc:zlib\nw:1\nh:1\nm:0\nn:13\n\n", plainCells(DualCell{Rune: ' '})))
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

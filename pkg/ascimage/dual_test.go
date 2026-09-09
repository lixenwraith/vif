package ascimage

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	lcolor "github.com/lixenwraith/color"
)

func TestDualModeCompressedRoundTrip(t *testing.T) {
	want := &DualModeImage{
		Width: 2, Height: 1, RenderMode: ModeQuadrant, AnchorX: 5, AnchorY: -3,
		Cells: []DualCell{
			{Rune: '▞', TrueFg: lcolor.RGB{R: 1, G: 2, B: 3}, TrueBg: lcolor.RGB{R: 4, G: 5, B: 6}, Palette256Fg: 7, Palette256Bg: 8},
			{Transparent: true},
		},
	}

	var encoded bytes.Buffer
	if err := WriteDualMode(&encoded, want); err != nil {
		t.Fatal(err)
	}
	header := encoded.Bytes()
	if !bytes.Contains(header, []byte("v:1\nc:flate\n")) {
		t.Fatalf("compressed header missing from %q", header[:min(len(header), 64)])
	}
	if !bytes.Contains(header, []byte("ax:5\nay:-3\n")) {
		t.Fatal("anchor offsets missing from vifimg header")
	}

	got, err := ReadDualMode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestReadDualModeAcceptsLegacyUncompressedBody(t *testing.T) {
	want := &DualModeImage{
		Width: 1, Height: 1, RenderMode: ModeBackgroundOnly, AnchorX: 12, AnchorY: -4,
		Cells: []DualCell{{Rune: ' ', TrueBg: lcolor.RGB{R: 10, G: 20, B: 30}, Palette256Bg: 17}},
	}

	var legacy bytes.Buffer
	fmt.Fprint(&legacy, "VFIMG\nw:1\nh:1\nm:0\nax:12\nay:-4\n\n")
	if err := writeDualCells(&legacy, want.Cells); err != nil {
		t.Fatal(err)
	}

	got, err := ReadDualMode(bytes.NewReader(legacy.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy read = %#v, want %#v", got, want)
	}
}

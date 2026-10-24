package logfile

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"testing"
)

// TestAPayloadRendersThroughTheAnchorsTable: the table alone turns a record's
// bytes into the key = value text the game shows, present fields only, nested
// structs and sequences inline, and refuses bytes the table does not account for.
func TestAPayloadRendersThroughTheAnchorsTable(t *testing.T) {
	ps, err := ParsePayloadSchema("EventFoo=FooPayload;FooPayload{entity:u,x:i,hit:b,scale:f,name:s,cells:[]Cell,spent:[2]u,record:*Cell};Cell{x:i,y:i}")
	if err != nil {
		t.Fatal(err)
	}
	// mask: entity, x, hit, scale, name, cells, spent set; record clear
	b := []byte{0b0111_1111}
	b = binary.AppendUvarint(b, 7)
	b = binary.AppendVarint(b, -3)
	b = binary.LittleEndian.AppendUint64(b, math.Float64bits(1.5))
	b = append(binary.AppendUvarint(b, 2), "ab"...)
	b = binary.AppendUvarint(b, 2) // two cells: {x = 1}, {y = -1}
	b = append(b, 0b01)
	b = binary.AppendVarint(b, 1)
	b = append(b, 0b10)
	b = binary.AppendVarint(b, -1)
	b = binary.AppendUvarint(b, 300) // spent[0], spent[1]
	b = binary.AppendUvarint(b, 0)
	raw := []byte(base64.StdEncoding.EncodeToString(b))
	got, err := ps.Render("EventFoo", raw)
	want := "entity = 7\nx = -3\nhit = true\nscale = 1.5\nname = \"ab\"\ncells = [{x = 1}, {y = -1}]\nspent = [300, 0]"
	if err != nil || got != want {
		t.Fatalf("rendered %q (%v), want %q", got, err, want)
	}
	if _, err := ps.Render("EventFoo", []byte(base64.StdEncoding.EncodeToString(append(b, 1)))); err == nil {
		t.Fatal("a trailing byte was rendered")
	}
	if _, err := ps.Render("EventFoo", []byte(base64.StdEncoding.EncodeToString(b[:len(b)-1]))); err == nil {
		t.Fatal("a truncated payload was rendered")
	}
}

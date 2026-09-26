package event

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
)

// captureSink records journal output in memory, for tests that need no file
type captureSink struct {
	records []JournalRecord
	anchors []JournalAnchor
}

func (c *captureSink) Record(r JournalRecord) { c.records = append(c.records, r) }
func (c *captureSink) Anchor(a JournalAnchor) { c.anchors = append(c.anchors, a) }
func (c *captureSink) Capture(JournalCapture) {}

// TestJournalPayloadRoundTrip populates every registered prototype, encodes it,
// and decodes it back. A field TOML cannot carry fails here, not at replay.
func TestJournalPayloadRoundTrip(t *testing.T) {
	InitRegistry()
	RangeEvents(func(name string, et EventType, proto any) {
		if proto == nil {
			return
		}
		t.Run(name, func(t *testing.T) {
			n := 0
			fillValue(reflect.ValueOf(proto).Elem(), &n)

			text, encErr := encodePayload(et, proto)
			if encErr != "" {
				t.Fatalf("encode: %s", encErr)
			}
			back := NewPayloadStruct(et)
			if err := toml.Unmarshal([]byte(text), back); err != nil {
				t.Fatalf("decode: %v\n%s", err, text)
			}
			if !reflect.DeepEqual(proto, back) {
				t.Fatalf("round trip mismatch\nwant %#v\ngot  %#v\ntoml:\n%s", proto, back, text)
			}
		})
	})
}

// TestPayloadFieldsEncodable rejects payload field types the encoder drops
// silently: a struct with no exported fields emits an empty table and decodes
// to zero, so the round-trip test cannot see the loss.
func TestPayloadFieldsEncodable(t *testing.T) {
	InitRegistry()
	seen := make(map[reflect.Type]bool)
	RangeEvents(func(name string, et EventType, proto any) {
		if proto == nil {
			return
		}
		checkEncodable(t, name, reflect.TypeOf(proto).Elem(), seen)
	})
}

func TestRecycledExplosionRetainsStorageButNoArtifactState(t *testing.T) {
	p := AcquireExplosionBatchRequest()
	p.StampCrossing(2, 17)
	p.Centers = append(p.Centers, ExplosionCenterEntry{X: 7, Y: 5})
	p.Entity, p.Radius, p.Attack = 11, 4, component.CombatAttackExplosion
	backing := p.Centers[:cap(p.Centers)]
	ReleaseDeferredPayload(p)

	// This serial test inspects the reset before any other consumer can acquire it.
	want := ExplosionBatchRequestPayload{Centers: backing[:0]}
	if !reflect.DeepEqual(*p, want) {
		t.Fatalf("recycled explosion retains artifact state: %+v", *p)
	}
	if cap(p.Centers) != len(backing) || &p.Centers[:cap(p.Centers)][0] != &backing[0] {
		t.Fatal("recycling discarded the centers' backing storage")
	}
}

// checkEncodable walks a payload type and flags any nested struct the encoder
// would emit as an empty table
func checkEncodable(t *testing.T, where string, typ reflect.Type, seen map[reflect.Type]bool) {
	for typ.Kind() == reflect.Ptr || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || seen[typ] {
		return
	}
	seen[typ] = true

	exported := 0
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.PkgPath != "" || f.Tag.Get("toml") == "-" {
			continue
		}
		exported++
		checkEncodable(t, where+"."+f.Name, f.Type, seen)
	}
	if typ.NumField() > 0 && exported == 0 {
		t.Errorf("%s: %s has no encodable fields; it marshals to an empty table", where, typ)
	}
}

// fillValue sets every settable field to a distinctive non-zero value, so a
// dropped field shows as a mismatch rather than a zero-equals-zero match
func fillValue(v reflect.Value, n *int) {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		*n++
		v.SetInt(int64(*n%100 + 1))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		*n++
		v.SetUint(uint64(*n%100 + 1))
	case reflect.Float32, reflect.Float64:
		*n++
		v.SetFloat(float64(*n%100) + 0.5)
	case reflect.String:
		*n++
		v.SetString("s" + strconv.Itoa(*n))
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := range 2 {
			fillValue(s.Index(i), n)
		}
		v.Set(s)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).PkgPath == "" {
				fillValue(v.Field(i), n)
			}
		}
	}
}

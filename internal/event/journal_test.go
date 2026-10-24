package event

import (
	"math"
	"reflect"
	"strconv"
	"testing"

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
func (c *captureSink) Finish(Stamp)           {}

// TestJournalPayloadRoundTrip populates every registered prototype, encodes it
// in its generated codec and decodes it back; the zero value too, since an
// absent field has to read as zero, and a negative zero keeps its sign.
func TestJournalPayloadRoundTrip(t *testing.T) {
	InitRegistry()
	RangeEvents(func(name string, et EventType, proto any) {
		if proto == nil {
			return
		}
		t.Run(name, func(t *testing.T) {
			for _, fill := range []bool{true, false} {
				if fill {
					n := 0
					fillValue(reflect.ValueOf(proto).Elem(), &n)
				} else {
					proto = NewPayloadStruct(et)
				}
				b, encErr := encodePayload(et, proto)
				if encErr != "" {
					t.Fatalf("encode: %s", encErr)
				}
				back, err := DecodePayload(et, b)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !reflect.DeepEqual(proto, back) {
					t.Fatalf("round trip mismatch\nwant %#v\ngot  %#v", proto, back)
				}
			}
		})
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
		if *n%7 == 0 {
			v.SetFloat(math.Copysign(0, -1))
			return
		}
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
	case reflect.Array:
		for i := range v.Len() {
			fillValue(v.Index(i), n)
		}
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillValue(v.Elem(), n)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).PkgPath == "" {
				fillValue(v.Field(i), n)
			}
		}
	}
}

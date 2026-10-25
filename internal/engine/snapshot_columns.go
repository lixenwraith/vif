package engine

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/core"
)

// A store's rows travel in columns (SharedWorldEncodeStoreColumns): the row count,
// the entities as zigzag deltas in capture order, then each leaf field over every
// row: varints, float bits, strings and times (RFC 3339) behind a length, a slice
// behind its length plus one so nil travels as nil. A row's entropy is its entity,
// so a column repeats where a row would not (netcode design §3.7).

func appendBool[T ~bool](dst []byte, v T) []byte {
	if v {
		return append(dst, 1)
	}
	return append(dst, 0)
}

func appendInt[T ~int | ~int8 | ~int16 | ~int32 | ~int64](dst []byte, v T) []byte {
	return binary.AppendVarint(dst, int64(v))
}

func appendUint[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr](dst []byte, v T) []byte {
	return binary.AppendUvarint(dst, uint64(v))
}

func appendF32[T ~float32](dst []byte, v T) []byte {
	return binary.LittleEndian.AppendUint32(dst, math.Float32bits(float32(v)))
}

func appendF64[T ~float64](dst []byte, v T) []byte {
	return binary.LittleEndian.AppendUint64(dst, math.Float64bits(float64(v)))
}

func appendString[T ~string](dst []byte, v T) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(v)))
	return append(dst, v...)
}

// appendTime writes the form JSON gives a time, so a time read back prints as the
// sender's did and hashes as its row did.
func appendTime(dst []byte, t time.Time) []byte {
	return appendString(dst, t.Format(time.RFC3339Nano))
}

func appendLen[T any](dst []byte, s []T) []byte {
	if s == nil {
		return append(dst, 0)
	}
	return binary.AppendUvarint(dst, uint64(len(s))+1)
}

func appendEntityColumn[T any](dst []byte, rows []StoreEntry[T]) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(rows)))
	var prev int64
	for i := range rows {
		e := int64(rows[i].Entity)
		dst = binary.AppendVarint(dst, e-prev)
		prev = e
	}
	return dst
}

func appendEntities(dst []byte, es []core.Entity) []byte {
	dst = appendLen(dst, es)
	var prev int64
	for _, e := range es {
		dst = binary.AppendVarint(dst, int64(e)-prev)
		prev = int64(e)
	}
	return dst
}

// columnReader reads columns back, keeping the first fault: a read after it does
// nothing, so a decoder reads to its end and asks once. promised is every row and
// element a count has claimed, which the bytes must cover at one each.
type columnReader struct {
	data     []byte
	pos      int
	promised int
	err      error
}

func newColumnReader(data []byte) *columnReader { return &columnReader{data: data} }

func (r *columnReader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("columns: "+format+" at byte %d", append(args, r.pos)...)
	}
}

func (r *columnReader) left() int { return len(r.data) - r.pos }

func (r *columnReader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.data[r.pos:])
	if n <= 0 {
		r.fail("a varint is cut short")
		return 0
	}
	r.pos += n
	return v
}

func (r *columnReader) varint() int64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Varint(r.data[r.pos:])
	if n <= 0 {
		r.fail("a varint is cut short")
		return 0
	}
	r.pos += n
	return v
}

func (r *columnReader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n > r.left() {
		r.fail("%d bytes claimed of %d left", n, r.left())
		return nil
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b
}

// promise admits a count of rows or elements the bytes left can cover.
func (r *columnReader) promise(n uint64) int {
	if r.err != nil {
		return 0
	}
	if n > uint64(r.left()) || r.promised+int(n) > len(r.data) {
		r.fail("%d rows claimed of %d bytes", n, r.left())
		return 0
	}
	r.promised += int(n)
	return int(n)
}

func (r *columnReader) rows() int { return r.promise(r.uvarint()) }

// done reports the first fault, or a body with bytes past its last column.
func (r *columnReader) done() error {
	if r.err != nil {
		return r.err
	}
	if r.pos != len(r.data) {
		return fmt.Errorf("columns: %d bytes past the last column", r.left())
	}
	return nil
}

func readBool[T ~bool](p *T, r *columnReader) {
	b := r.bytes(1)
	if len(b) != 1 {
		return
	}
	switch b[0] {
	case 0:
		*p = false
	case 1:
		*p = true
	default:
		r.fail("a bool of %d", b[0])
	}
}

func readInt[T ~int | ~int8 | ~int16 | ~int32 | ~int64](p *T, r *columnReader) {
	*p = T(r.varint())
}

func readUint[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr](p *T, r *columnReader) {
	*p = T(r.uvarint())
}

func readF32[T ~float32](p *T, r *columnReader) {
	if b := r.bytes(4); len(b) == 4 {
		*p = T(math.Float32frombits(binary.LittleEndian.Uint32(b)))
	}
}

func readF64[T ~float64](p *T, r *columnReader) {
	if b := r.bytes(8); len(b) == 8 {
		*p = T(math.Float64frombits(binary.LittleEndian.Uint64(b)))
	}
}

func readString[T ~string](p *T, r *columnReader) {
	n := r.uvarint()
	if n > uint64(r.left()) {
		r.fail("a string of %d bytes claimed of %d left", n, r.left())
		return
	}
	if b := r.bytes(int(n)); r.err == nil {
		*p = T(b)
	}
}

func readTime(p *time.Time, r *columnReader) {
	var s string
	readString(&s, r)
	if r.err != nil {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		r.fail("%v", err)
		return
	}
	*p = t
}

func readSlice[T any](p *[]T, r *columnReader) {
	n := r.uvarint()
	if n == 0 {
		*p = nil
		return
	}
	*p = make([]T, r.promise(n-1))
}

func readEntityColumn[T any](rows []StoreEntry[T], r *columnReader) {
	var prev int64
	for i := range rows {
		prev += r.varint()
		rows[i].Entity = core.Entity(prev)
	}
}

func readEntities(r *columnReader) []core.Entity {
	n := r.uvarint()
	if n == 0 {
		return nil
	}
	out := make([]core.Entity, r.promise(n-1))
	var prev int64
	for i := range out {
		prev += r.varint()
		out[i] = core.Entity(prev)
	}
	return out
}

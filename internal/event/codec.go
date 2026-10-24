package event

import (
	"encoding/binary"
	"unsafe"
)

// The payload codec is table-driven: payload_gen.go describes every payload struct
// as its fields' offsets and kinds, and the two walks below encode and decode
// through them, so the codec costs the binary a table a field rather than a
// function a struct, and reads no type information at run time.

// fieldKind is what a field holds, which decides its bytes and its zero test.
type fieldKind uint8

const (
	kindBool   fieldKind = iota // the mask bit is the value
	kindInt                     // zigzag varint, size bytes wide in the struct
	kindUint                    // varint, size bytes wide
	kindFloat                   // float64 as its bits
	kindString                  // length, bytes
	kindSlice                   // length, then elem per element, stride apart
	kindArray                   // n elements, stride apart
	kindPtr                     // sub, when not nil
	kindStruct                  // sub, inline
)

// field is one struct field, or one slice or array element at off zero.
type field struct {
	off    uintptr
	kind   fieldKind
	size   uintptr      // a scalar's width in the struct
	n      int          // an array's length
	stride uintptr      // an element's width
	elem   *field       // a slice's or array's element
	sub    *structCodec // a pointer's target or a nested struct
	make   func(p unsafe.Pointer, n int)
	new    func(p unsafe.Pointer)
}

// structCodec is one struct's fields in declaration order, behind a presence
// mask of one bit each.
type structCodec struct {
	size   uintptr
	fields []field
}

// payloadCodec is what every registered payload type carries (payload_gen.go).
type payloadCodec interface {
	codec() (*structCodec, unsafe.Pointer)
}

var maskZeros [32]byte

func appendStruct(b []byte, p unsafe.Pointer, c *structCodec) []byte {
	start := len(b)
	b = append(b, maskZeros[:(len(c.fields)+7)/8]...)
	for i := range c.fields {
		f := &c.fields[i]
		fp := unsafe.Add(p, f.off)
		if !present(fp, f) {
			continue
		}
		b[start+i/8] |= 1 << (i % 8)
		b = appendValue(b, fp, f)
	}
	return b
}

func readStruct(r *wireReader, p unsafe.Pointer, c *structCodec) {
	mask := r.bytes(uint64((len(c.fields) + 7) / 8))
	if r.err != nil {
		return
	}
	for i := range c.fields {
		if mask[i/8]&(1<<(i%8)) == 0 {
			continue
		}
		f := &c.fields[i]
		fp := unsafe.Add(p, f.off)
		if f.kind == kindBool {
			*(*bool)(fp) = true
			continue
		}
		readValue(r, fp, f)
		if r.err != nil {
			return
		}
	}
}

// present is a field's zero test: a bool's value, a float's bits, a slice's or
// string's length, an array's bytes, a pointer's nil; a struct is always written.
func present(fp unsafe.Pointer, f *field) bool {
	switch f.kind {
	case kindBool:
		return *(*bool)(fp)
	case kindInt, kindUint:
		return loadUint(fp, f.size) != 0
	case kindFloat:
		return *(*uint64)(fp) != 0
	case kindString:
		return len(*(*string)(fp)) != 0
	case kindSlice:
		return len(*(*[]byte)(fp)) != 0
	case kindArray:
		for _, v := range unsafe.Slice((*byte)(fp), f.stride*uintptr(f.n)) {
			if v != 0 {
				return true
			}
		}
		return false
	case kindPtr:
		return *(*unsafe.Pointer)(fp) != nil
	}
	return true
}

func appendValue(b []byte, fp unsafe.Pointer, f *field) []byte {
	switch f.kind {
	case kindBool:
		return b
	case kindInt:
		return binary.AppendVarint(b, loadInt(fp, f.size))
	case kindUint:
		return binary.AppendUvarint(b, loadUint(fp, f.size))
	case kindFloat:
		return binary.LittleEndian.AppendUint64(b, *(*uint64)(fp))
	case kindString:
		s := *(*string)(fp)
		b = binary.AppendUvarint(b, uint64(len(s)))
		return append(b, s...)
	case kindSlice:
		hdr := *(*[]byte)(fp)
		b = binary.AppendUvarint(b, uint64(len(hdr)))
		return appendElems(b, unsafe.Pointer(unsafe.SliceData(hdr)), len(hdr), f)
	case kindArray:
		return appendElems(b, fp, f.n, f)
	case kindPtr:
		return appendStruct(b, *(*unsafe.Pointer)(fp), f.sub)
	}
	return appendStruct(b, fp, f.sub)
}

func appendElems(b []byte, p unsafe.Pointer, n int, f *field) []byte {
	for i := range n {
		b = appendValue(b, unsafe.Add(p, uintptr(i)*f.stride), f.elem)
	}
	return b
}

func readValue(r *wireReader, fp unsafe.Pointer, f *field) {
	switch f.kind {
	case kindInt:
		storeInt(fp, f.size, r.varint())
	case kindUint:
		storeUint(fp, f.size, r.uvarint())
	case kindFloat:
		*(*uint64)(fp) = r.u64()
	case kindString:
		*(*string)(fp) = string(r.bytes(r.uvarint()))
	case kindSlice:
		n := r.length()
		if n == 0 {
			return
		}
		f.make(fp, n)
		readElems(r, unsafe.Pointer(unsafe.SliceData(*(*[]byte)(fp))), n, f)
	case kindArray:
		readElems(r, fp, f.n, f)
	case kindPtr:
		f.new(fp)
		readStruct(r, *(*unsafe.Pointer)(fp), f.sub)
	default:
		readStruct(r, fp, f.sub)
	}
}

func readElems(r *wireReader, p unsafe.Pointer, n int, f *field) {
	for i := 0; i < n && r.err == nil; i++ {
		readValue(r, unsafe.Add(p, uintptr(i)*f.stride), f.elem)
	}
}

func loadUint(p unsafe.Pointer, size uintptr) uint64 {
	switch size {
	case 1:
		return uint64(*(*uint8)(p))
	case 2:
		return uint64(*(*uint16)(p))
	case 4:
		return uint64(*(*uint32)(p))
	}
	return *(*uint64)(p)
}

func loadInt(p unsafe.Pointer, size uintptr) int64 {
	switch size {
	case 1:
		return int64(*(*int8)(p))
	case 2:
		return int64(*(*int16)(p))
	case 4:
		return int64(*(*int32)(p))
	}
	return *(*int64)(p)
}

func storeUint(p unsafe.Pointer, size uintptr, v uint64) {
	switch size {
	case 1:
		*(*uint8)(p) = uint8(v)
	case 2:
		*(*uint16)(p) = uint16(v)
	case 4:
		*(*uint32)(p) = uint32(v)
	default:
		*(*uint64)(p) = v
	}
}

func storeInt(p unsafe.Pointer, size uintptr, v int64) {
	switch size {
	case 1:
		*(*int8)(p) = int8(v)
	case 2:
		*(*int16)(p) = int16(v)
	case 4:
		*(*int32)(p) = int32(v)
	default:
		*(*int64)(p) = v
	}
}

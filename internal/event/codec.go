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

// op is a field's kind and width as one switch case, resolved once at init.
type op uint8

const (
	opBool op = iota
	opInt8
	opInt16
	opInt32
	opInt64
	opUint8
	opUint16
	opUint32
	opUint64
	opFloat
	opString
	opSlice
	opArray
	opPtr
	opStruct
)

// field is one struct field, or one slice or array element at off zero.
type field struct {
	off    uintptr
	kind   fieldKind
	op     op
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

func init() {
	for _, c := range payloadCodecs {
		resolveOps(c)
	}
}

// resolveOps folds each field's kind and width into its op, through the structs
// it reaches; a struct reached twice is resolved twice, to the same ops.
func resolveOps(c *structCodec) {
	if c == nil {
		return
	}
	for i := range c.fields {
		f := &c.fields[i]
		f.op = opOf(f)
		if f.elem != nil {
			f.elem.op = opOf(f.elem)
			resolveOps(f.elem.sub)
		}
		resolveOps(f.sub)
	}
}

func opOf(f *field) op {
	switch f.kind {
	case kindInt:
		return opInt8 + op(widthIndex(f.size))
	case kindUint:
		return opUint8 + op(widthIndex(f.size))
	}
	return [...]op{kindBool: opBool, kindFloat: opFloat, kindString: opString, kindSlice: opSlice,
		kindArray: opArray, kindPtr: opPtr, kindStruct: opStruct}[f.kind]
}

func widthIndex(size uintptr) int {
	switch size {
	case 1:
		return 0
	case 2:
		return 1
	case 4:
		return 2
	}
	return 3
}

var maskZeros [32]byte

// appendStruct writes the mask and the set fields; a scalar is tested and
// written in place, anything else through present and appendValue.
func appendStruct(b []byte, p unsafe.Pointer, c *structCodec) []byte {
	start := len(b)
	b = append(b, maskZeros[:(len(c.fields)+7)/8]...)
	for i := range c.fields {
		f := &c.fields[i]
		fp := unsafe.Add(p, f.off)
		set := true
		switch f.op {
		case opBool:
			set = *(*bool)(fp)
		case opInt8:
			if v := *(*int8)(fp); v != 0 {
				b = binary.AppendVarint(b, int64(v))
			} else {
				set = false
			}
		case opInt16:
			if v := *(*int16)(fp); v != 0 {
				b = binary.AppendVarint(b, int64(v))
			} else {
				set = false
			}
		case opInt32:
			if v := *(*int32)(fp); v != 0 {
				b = binary.AppendVarint(b, int64(v))
			} else {
				set = false
			}
		case opInt64:
			if v := *(*int64)(fp); v != 0 {
				b = binary.AppendVarint(b, v)
			} else {
				set = false
			}
		case opUint8:
			if v := *(*uint8)(fp); v != 0 {
				b = binary.AppendUvarint(b, uint64(v))
			} else {
				set = false
			}
		case opUint16:
			if v := *(*uint16)(fp); v != 0 {
				b = binary.AppendUvarint(b, uint64(v))
			} else {
				set = false
			}
		case opUint32:
			if v := *(*uint32)(fp); v != 0 {
				b = binary.AppendUvarint(b, uint64(v))
			} else {
				set = false
			}
		case opUint64:
			if v := *(*uint64)(fp); v != 0 {
				b = binary.AppendUvarint(b, v)
			} else {
				set = false
			}
		case opFloat:
			if v := *(*uint64)(fp); v != 0 {
				b = binary.LittleEndian.AppendUint64(b, v)
			} else {
				set = false
			}
		default:
			if set = present(fp, f); set {
				b = appendValue(b, fp, f)
			}
		}
		if set {
			b[start+i/8] |= 1 << (i % 8)
		}
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
		if f.op == opBool {
			*(*bool)(fp) = true
			continue
		}
		readValue(r, fp, f)
		if r.err != nil {
			return
		}
	}
}

// present is a compound field's zero test: a string's or slice's length, an
// array's bytes, a pointer's nil; a struct is always written.
func present(fp unsafe.Pointer, f *field) bool {
	switch f.op {
	case opString:
		return len(*(*string)(fp)) != 0
	case opSlice:
		return len(*(*[]byte)(fp)) != 0
	case opArray:
		for _, v := range unsafe.Slice((*byte)(fp), f.stride*uintptr(f.n)) {
			if v != 0 {
				return true
			}
		}
		return false
	case opPtr:
		return *(*unsafe.Pointer)(fp) != nil
	}
	return true
}

// appendValue writes a set field or an element, zero or not.
func appendValue(b []byte, fp unsafe.Pointer, f *field) []byte {
	switch f.op {
	case opInt8:
		return binary.AppendVarint(b, int64(*(*int8)(fp)))
	case opInt16:
		return binary.AppendVarint(b, int64(*(*int16)(fp)))
	case opInt32:
		return binary.AppendVarint(b, int64(*(*int32)(fp)))
	case opInt64:
		return binary.AppendVarint(b, *(*int64)(fp))
	case opUint8:
		return binary.AppendUvarint(b, uint64(*(*uint8)(fp)))
	case opUint16:
		return binary.AppendUvarint(b, uint64(*(*uint16)(fp)))
	case opUint32:
		return binary.AppendUvarint(b, uint64(*(*uint32)(fp)))
	case opUint64:
		return binary.AppendUvarint(b, *(*uint64)(fp))
	case opFloat:
		return binary.LittleEndian.AppendUint64(b, *(*uint64)(fp))
	case opString:
		s := *(*string)(fp)
		return append(binary.AppendUvarint(b, uint64(len(s))), s...)
	case opSlice:
		hdr := *(*[]byte)(fp)
		b = binary.AppendUvarint(b, uint64(len(hdr)))
		return appendElems(b, unsafe.Pointer(unsafe.SliceData(hdr)), len(hdr), f)
	case opArray:
		return appendElems(b, fp, f.n, f)
	case opPtr:
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
	switch f.op {
	case opInt8:
		*(*int8)(fp) = int8(r.varint())
	case opInt16:
		*(*int16)(fp) = int16(r.varint())
	case opInt32:
		*(*int32)(fp) = int32(r.varint())
	case opInt64:
		*(*int64)(fp) = r.varint()
	case opUint8:
		*(*uint8)(fp) = uint8(r.uvarint())
	case opUint16:
		*(*uint16)(fp) = uint16(r.uvarint())
	case opUint32:
		*(*uint32)(fp) = uint32(r.uvarint())
	case opUint64:
		*(*uint64)(fp) = r.uvarint()
	case opFloat:
		*(*uint64)(fp) = r.u64()
	case opString:
		*(*string)(fp) = string(r.bytes(r.uvarint()))
	case opSlice:
		n := r.length()
		if n == 0 {
			return
		}
		f.make(fp, n)
		readElems(r, unsafe.Pointer(unsafe.SliceData(*(*[]byte)(fp))), n, f)
	case opArray:
		readElems(r, fp, f.n, f)
	case opPtr:
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

package logfile

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"
)

// PayloadSchema is a journal anchor's payloads table: every event's payload
// struct and every struct's fields and kinds, so a record's binary payload renders
// without the build that wrote it (internal/event/payload_gen.go).
type PayloadSchema struct {
	events  map[string]string
	structs map[string][]schemaField
}

type schemaField struct {
	key  string
	kind string // b bool, i signed, u unsigned, f float64, s string, []k, [n]k, *S, S
}

// ParsePayloadSchema reads the table as the game writes it:
// Event=Struct;Struct{key:kind,...};...
func ParsePayloadSchema(s string) (*PayloadSchema, error) {
	ps := &PayloadSchema{events: map[string]string{}, structs: map[string][]schemaField{}}
	for _, entry := range strings.Split(s, ";") {
		if name, st, ok := strings.Cut(entry, "="); ok {
			ps.events[name] = st
			continue
		}
		name, rest, ok := strings.Cut(entry, "{")
		if !ok || !strings.HasSuffix(rest, "}") {
			return nil, errors.New("payloads: malformed entry " + entry)
		}
		fields := []schemaField{}
		if body := strings.TrimSuffix(rest, "}"); body != "" {
			for _, f := range strings.Split(body, ",") {
				key, kind, ok := strings.Cut(f, ":")
				if !ok || kind == "" {
					return nil, errors.New("payloads: malformed field " + f + " in " + name)
				}
				fields = append(fields, schemaField{key, kind})
			}
		}
		ps.structs[name] = fields
	}
	return ps, nil
}

// Render decodes one record's base64 payload for ev as the key = value lines the
// game's text codec showed, the present fields in declaration order, a nested
// struct inline in braces.
func (ps *PayloadSchema) Render(ev string, b64 []byte) (string, error) {
	st, ok := ps.events[ev]
	if !ok {
		return "", errors.New("no payload table for " + ev)
	}
	raw, err := base64.StdEncoding.AppendDecode(nil, b64)
	if err != nil {
		return "", err
	}
	r := payloadReader{b: raw}
	var sb strings.Builder
	ps.renderStruct(&sb, &r, st, "\n")
	if r.err != nil {
		return "", r.err
	}
	if len(r.b) != 0 {
		return "", errors.New("payload leaves " + strconv.Itoa(len(r.b)) + " bytes unread")
	}
	return strings.TrimSuffix(sb.String(), "\n"), nil
}

// renderStruct writes one struct's present fields, each ending in sep.
func (ps *PayloadSchema) renderStruct(sb *strings.Builder, r *payloadReader, name, sep string) {
	fields, ok := ps.structs[name]
	if !ok {
		r.err = errors.New("payloads: no table for struct " + name)
		return
	}
	mask := make([]byte, (len(fields)+7)/8)
	for i := range mask {
		mask[i] = r.u8()
	}
	first := true
	for i, f := range fields {
		if mask[i/8]&(1<<(i%8)) == 0 {
			continue
		}
		if !first && sep != "\n" {
			sb.WriteString(sep)
		}
		first = false
		sb.WriteString(f.key)
		sb.WriteString(" = ")
		if f.kind == "b" {
			sb.WriteString("true")
		} else {
			ps.renderValue(sb, r, f.kind)
		}
		if sep == "\n" {
			sb.WriteString(sep)
		}
		if r.err != nil {
			return
		}
	}
}

func (ps *PayloadSchema) renderValue(sb *strings.Builder, r *payloadReader, kind string) {
	switch {
	case kind == "i":
		sb.WriteString(strconv.FormatInt(r.varint(), 10))
	case kind == "u":
		sb.WriteString(strconv.FormatUint(r.uvarint(), 10))
	case kind == "f":
		sb.WriteString(strconv.FormatFloat(math.Float64frombits(r.u64()), 'g', -1, 64))
	case kind == "s":
		sb.WriteString(strconv.Quote(string(r.bytes(r.uvarint()))))
	case strings.HasPrefix(kind, "[]"):
		ps.renderSeq(sb, r, r.length(), kind[2:])
	case strings.HasPrefix(kind, "["):
		n, rest, _ := strings.Cut(kind[1:], "]")
		count, err := strconv.Atoi(n)
		if err != nil {
			r.err = errors.New("payloads: malformed array kind " + kind)
			return
		}
		ps.renderSeq(sb, r, count, rest)
	case strings.HasPrefix(kind, "*"):
		ps.renderInline(sb, r, kind[1:])
	default:
		ps.renderInline(sb, r, kind)
	}
}

func (ps *PayloadSchema) renderInline(sb *strings.Builder, r *payloadReader, name string) {
	sb.WriteString("{")
	ps.renderStruct(sb, r, name, ", ")
	sb.WriteString("}")
}

func (ps *PayloadSchema) renderSeq(sb *strings.Builder, r *payloadReader, n int, elem string) {
	sb.WriteString("[")
	for i := 0; i < n && r.err == nil; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		ps.renderValue(sb, r, elem)
	}
	sb.WriteString("]")
}

// payloadReader walks a payload with one sticky error, as the game's codec does.
type payloadReader struct {
	b   []byte
	err error
}

func (r *payloadReader) fail() {
	if r.err == nil {
		r.err = errors.New("payload truncated")
	}
}

func (r *payloadReader) u8() byte {
	if len(r.b) == 0 {
		r.fail()
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *payloadReader) u64() uint64 {
	if len(r.b) < 8 {
		r.fail()
		return 0
	}
	v := binary.LittleEndian.Uint64(r.b)
	r.b = r.b[8:]
	return v
}

func (r *payloadReader) uvarint() uint64 {
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *payloadReader) varint() int64 {
	v, n := binary.Varint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *payloadReader) bytes(n uint64) []byte {
	if n > uint64(len(r.b)) {
		r.fail()
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *payloadReader) length() int {
	n := r.uvarint()
	if n > uint64(len(r.b)) {
		r.fail()
		return 0
	}
	return int(n)
}

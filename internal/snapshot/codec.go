package snapshot

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	link "github.com/lixenwraith/vif/pkg/network"
)

// A snapshot body travels in a small versioned envelope,
// [magic:4][version:1][codec:1][plain bytes:4][body]: one JSON document, deflated
// or plain, or a capture as sections, each part's name, plain length, blob length
// and its own deflate, so a store unchanged since the last capture is sent from the
// bytes made then. A receiver bounds expansion by the declared plain sizes.
const (
	snapshotWireHeader    = 10
	snapshotWireVersion   = 1
	snapshotCodecPlain    = 0
	snapshotCodecFlate    = 1
	snapshotCodecSections = 2
	sectionPartsMax       = 256
	sectionNameMax        = 64
)

// sectionPart is one part of a capture in the sections codec: its name, the length
// of its JSON and the bytes that JSON deflates to, or the JSON itself once decoded.
type sectionPart struct {
	name  string
	plain int
	blob  []byte
}

var (
	snapshotWireMagic = [4]byte{'V', 'I', 'F', 'S'}
	snapshotDeflaters = sync.Pool{New: func() any {
		w, err := flate.NewWriter(io.Discard, flate.DefaultCompression)
		if err != nil {
			panic(err) // a standard-library level cannot fail
		}
		return w
	}}
	snapshotInflaters = sync.Pool{New: func() any { return flate.NewReader(bytes.NewReader(nil)) }}
)

// EncodeJSON marshals the schema body and compresses it for transport, at the level
// the wire gets: a deflated body passes each link's stream raw. Level 6 takes a td
// keyframe to 0.72 of level 1's bytes in three times its 12 ms, and the deeper
// levels buy nothing more for far longer.
func EncodeJSON(v any) ([]byte, error) { return encodeJSON(v, snapshotCodecFlate) }

// EncodePlainJSON is EncodeJSON left for the link's stream to compress: a small
// body that repeats itself message to message compresses against its predecessors
// there, and alone hardly at all.
func EncodePlainJSON(v any) ([]byte, error) { return encodeJSON(v, snapshotCodecPlain) }

func encodeJSON(v any, codec byte) ([]byte, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return encodePlain(plain, codec)
}

// encodePlain wraps a marshalled body in the envelope.
func encodePlain(plain []byte, codec byte) ([]byte, error) {
	if len(plain) == 0 || len(plain) > link.MaxSnapshotPlainBytes {
		return nil, fmt.Errorf("snapshot encode: %d plain bytes is outside 1..%d",
			len(plain), link.MaxSnapshotPlainBytes)
	}

	var out bytes.Buffer
	out.Grow(snapshotWireHeader + len(plain)/4)
	_, _ = out.Write(snapshotWireMagic[:])
	_ = out.WriteByte(snapshotWireVersion)
	_ = out.WriteByte(codec)
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(plain)))
	_, _ = out.Write(size[:])
	if codec == snapshotCodecPlain {
		_, _ = out.Write(plain)
		return out.Bytes(), nil
	}

	w := snapshotDeflaters.Get().(*flate.Writer)
	w.Reset(&out)
	_, writeErr := w.Write(plain)
	closeErr := w.Close()
	snapshotDeflaters.Put(w)
	if writeErr != nil {
		return nil, fmt.Errorf("snapshot compress: %w", writeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("snapshot compress: %w", closeErr)
	}
	if out.Len() > link.MaxSnapshotBytes {
		return nil, fmt.Errorf("snapshot encode: %d wire bytes exceeds the %d-byte ceiling",
			out.Len(), link.MaxSnapshotBytes)
	}
	return out.Bytes(), nil
}

// deflateBytes compresses one body at the wire's level.
func deflateBytes(plain []byte) ([]byte, error) {
	var out bytes.Buffer
	out.Grow(len(plain) / 4)
	w := snapshotDeflaters.Get().(*flate.Writer)
	w.Reset(&out)
	_, writeErr := w.Write(plain)
	closeErr := w.Close()
	snapshotDeflaters.Put(w)
	if writeErr != nil {
		return nil, fmt.Errorf("snapshot compress: %w", writeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("snapshot compress: %w", closeErr)
	}
	return out.Bytes(), nil
}

// inflateBytes expands one deflated body to exactly plain bytes.
func inflateBytes(blob []byte, plain int) ([]byte, error) {
	r := snapshotInflaters.Get().(io.ReadCloser)
	if err := r.(flate.Resetter).Reset(bytes.NewReader(blob), nil); err != nil {
		return nil, fmt.Errorf("snapshot decompress: %w", err)
	}
	out, readErr := io.ReadAll(io.LimitReader(r, int64(plain)+1))
	closeErr := r.Close()
	snapshotInflaters.Put(r)
	if readErr != nil {
		return nil, fmt.Errorf("snapshot decompress: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("snapshot decompress: %w", closeErr)
	}
	if len(out) != plain {
		return nil, fmt.Errorf("snapshot decompress: got %d plain bytes, envelope names %d", len(out), plain)
	}
	return out, nil
}

// encodeSections wraps deflated parts in the envelope under the plain size they
// sum to: the count, then each part's name, plain length, blob length and blob.
func encodeSections(parts []sectionPart) ([]byte, error) {
	total := 0
	for _, p := range parts {
		total += p.plain
	}
	if total == 0 || total > link.MaxSnapshotPlainBytes || len(parts) > sectionPartsMax {
		return nil, fmt.Errorf("snapshot encode: %d parts of %d plain bytes are outside the envelope", len(parts), total)
	}
	var out bytes.Buffer
	_, _ = out.Write(snapshotWireMagic[:])
	_ = out.WriteByte(snapshotWireVersion)
	_ = out.WriteByte(snapshotCodecSections)
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(total))
	_, _ = out.Write(size[:])
	_, _ = out.Write(binary.AppendUvarint(nil, uint64(len(parts))))
	for _, p := range parts {
		_, _ = out.Write(binary.AppendUvarint(nil, uint64(len(p.name))))
		_, _ = out.WriteString(p.name)
		_, _ = out.Write(binary.AppendUvarint(nil, uint64(p.plain)))
		_, _ = out.Write(binary.AppendUvarint(nil, uint64(len(p.blob))))
		_, _ = out.Write(p.blob)
	}
	if out.Len() > link.MaxSnapshotBytes {
		return nil, fmt.Errorf("snapshot encode: %d wire bytes exceeds the %d-byte ceiling",
			out.Len(), link.MaxSnapshotBytes)
	}
	return out.Bytes(), nil
}

// isSections reports a body in the sections codec, which a capture travels in.
func isSections(body []byte) bool {
	return len(body) >= snapshotWireHeader && bytes.Equal(body[:4], snapshotWireMagic[:]) &&
		body[5] == snapshotCodecSections
}

// decodeSections validates the envelope and expands each part to its JSON, every
// length checked against the declared plain size before anything is allocated.
func decodeSections(body []byte) ([]sectionPart, error) {
	plainBytes, err := envelopePlain(body, snapshotCodecSections)
	if err != nil {
		return nil, err
	}
	r := body[snapshotWireHeader:]
	next := func() (uint64, error) {
		v, n := binary.Uvarint(r)
		if n <= 0 {
			return 0, errors.New("snapshot envelope: a part's length is cut short")
		}
		r = r[n:]
		return v, nil
	}
	count, err := next()
	if err != nil || count == 0 || count > sectionPartsMax {
		return nil, fmt.Errorf("snapshot envelope: %d parts", count)
	}
	parts, left := make([]sectionPart, 0, count), int(plainBytes)
	for range count {
		nameLen, err := next()
		if err != nil || nameLen == 0 || nameLen > sectionNameMax || nameLen > uint64(len(r)) {
			return nil, errors.New("snapshot envelope: a part's name is outside the body")
		}
		name := string(r[:nameLen])
		r = r[nameLen:]
		plain, err := next()
		if err != nil || plain == 0 || plain > uint64(left) {
			return nil, fmt.Errorf("snapshot envelope: part %q names %d plain bytes of %d left", name, plain, left)
		}
		blobLen, err := next()
		if err != nil || blobLen > uint64(len(r)) {
			return nil, fmt.Errorf("snapshot envelope: part %q is cut short", name)
		}
		out, err := inflateBytes(r[:blobLen], int(plain))
		if err != nil {
			return nil, fmt.Errorf("part %q: %w", name, err)
		}
		r, left = r[blobLen:], left-int(plain)
		parts = append(parts, sectionPart{name: name, plain: int(plain), blob: out})
	}
	if left != 0 || len(r) != 0 {
		return nil, fmt.Errorf("snapshot envelope: %d plain bytes and %d wire bytes unaccounted for", left, len(r))
	}
	return parts, nil
}

// envelopePlain checks the header for one codec and returns the plain size it names.
func envelopePlain(body []byte, codec byte) (uint32, error) {
	if len(body) < snapshotWireHeader {
		return 0, fmt.Errorf("snapshot envelope: %d bytes, want at least %d", len(body), snapshotWireHeader)
	}
	if !bytes.Equal(body[:4], snapshotWireMagic[:]) {
		return 0, errors.New("snapshot envelope: bad magic")
	}
	if body[4] != snapshotWireVersion {
		return 0, fmt.Errorf("snapshot envelope: unsupported version %d", body[4])
	}
	if body[5] != codec && !(codec == snapshotCodecFlate && body[5] == snapshotCodecPlain) {
		return 0, fmt.Errorf("snapshot envelope: unsupported codec %d", body[5])
	}
	plainBytes := binary.BigEndian.Uint32(body[6:10])
	if plainBytes == 0 || plainBytes > link.MaxSnapshotPlainBytes {
		return 0, fmt.Errorf("snapshot envelope: names %d plain bytes", plainBytes)
	}
	return plainBytes, nil
}

// DecodeJSON validates and expands one JSON envelope. The plain-size declaration
// is checked before allocation and enforced while reading, so a small compressed
// body cannot expand past the expansion ceiling.
func DecodeJSON(body []byte, dst any) error {
	plainBytes, err := envelopePlain(body, snapshotCodecFlate)
	if err != nil {
		return err
	}
	if body[5] == snapshotCodecPlain {
		if len(body)-snapshotWireHeader != int(plainBytes) {
			return fmt.Errorf("snapshot envelope: carries %d plain bytes, names %d",
				len(body)-snapshotWireHeader, plainBytes)
		}
		return json.Unmarshal(body[snapshotWireHeader:], dst)
	}

	plain, err := inflateBytes(body[snapshotWireHeader:], int(plainBytes))
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, dst)
}

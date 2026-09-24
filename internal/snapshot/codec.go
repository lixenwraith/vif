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

	"github.com/lixenwraith/vi-fighter/internal/network"
)

// Snapshot bodies have a deliberately small, versioned envelope before the
// transport chunks them:
//
//	[magic:4][version:1][codec:1][plain bytes:4][compressed JSON]
//
// The JSON remains the schema and integrity surface. Compression is only a wire
// concern, outside the world lock, so it reduces joins and corrections without
// changing capture, diff or reconcile semantics. The declared plain size is what a
// receiver bounds decompression by, against the expansion ceiling rather than the
// wire one: the two differ by the ratio the codec achieves on a walled map.
const (
	snapshotWireHeader  = 10
	snapshotWireVersion = 1
	snapshotCodecPlain  = 0
	snapshotCodecFlate  = 1
)

var (
	snapshotWireMagic = [4]byte{'V', 'I', 'F', 'S'}
	snapshotDeflaters = sync.Pool{New: func() any {
		w, err := flate.NewWriter(io.Discard, flate.BestSpeed)
		if err != nil {
			panic(err) // BestSpeed is a standard-library constant and cannot fail.
		}
		return w
	}}
)

// EncodeJSON marshals the schema body and compresses it for transport.
// BestSpeed is intentional: the storm high-water measurement shows most of the
// available byte reduction at this level while keeping encode work below a
// millisecond on the reference machine.
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
	if len(plain) == 0 || len(plain) > network.MaxSnapshotPlainBytes {
		return nil, fmt.Errorf("snapshot encode: %d plain bytes is outside 1..%d",
			len(plain), network.MaxSnapshotPlainBytes)
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
	if out.Len() > network.MaxSnapshotBytes {
		return nil, fmt.Errorf("snapshot encode: %d wire bytes exceeds the %d-byte ceiling",
			out.Len(), network.MaxSnapshotBytes)
	}
	return out.Bytes(), nil
}

// DecodeJSON validates and expands one wire envelope. The plain-size
// declaration is checked before allocation and enforced while reading, so a small
// compressed body cannot expand past the expansion ceiling.
func DecodeJSON(body []byte, dst any) error {
	if len(body) < snapshotWireHeader {
		return fmt.Errorf("snapshot envelope: %d bytes, want at least %d", len(body), snapshotWireHeader)
	}
	if !bytes.Equal(body[:4], snapshotWireMagic[:]) {
		return errors.New("snapshot envelope: bad magic")
	}
	if body[4] != snapshotWireVersion {
		return fmt.Errorf("snapshot envelope: unsupported version %d", body[4])
	}
	if body[5] != snapshotCodecFlate && body[5] != snapshotCodecPlain {
		return fmt.Errorf("snapshot envelope: unsupported codec %d", body[5])
	}
	plainBytes := binary.BigEndian.Uint32(body[6:10])
	if plainBytes == 0 || plainBytes > network.MaxSnapshotPlainBytes {
		return fmt.Errorf("snapshot envelope: names %d plain bytes", plainBytes)
	}
	if body[5] == snapshotCodecPlain {
		if len(body)-snapshotWireHeader != int(plainBytes) {
			return fmt.Errorf("snapshot envelope: carries %d plain bytes, names %d",
				len(body)-snapshotWireHeader, plainBytes)
		}
		return json.Unmarshal(body[snapshotWireHeader:], dst)
	}

	r := flate.NewReader(bytes.NewReader(body[snapshotWireHeader:]))
	plain, readErr := io.ReadAll(io.LimitReader(r, int64(plainBytes)+1))
	closeErr := r.Close()
	if readErr != nil {
		return fmt.Errorf("snapshot decompress: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("snapshot decompress: %w", closeErr)
	}
	if len(plain) != int(plainBytes) {
		return fmt.Errorf("snapshot decompress: got %d plain bytes, envelope names %d",
			len(plain), plainBytes)
	}
	if err := json.Unmarshal(plain, dst); err != nil {
		return err
	}
	return nil
}

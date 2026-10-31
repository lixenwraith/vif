package audio

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
)

const wavHeaderSize = 44

// ReadWAV decodes a RIFF WAVE stream: PCM of 8, 16, 24 or 32 bits or IEEE float
// of 32 or 64, any channel count, mixed down to mono in [-1, 1] at the stream's
// own rate. Chunks it does not know are skipped, so a tagged export reads.
func ReadWAV(r io.Reader) (mono []float64, rate int, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, errors.New("wav: not a RIFF WAVE stream")
	}
	le := binary.LittleEndian
	var format, channels, bits int
	var pcm []byte
	for off := 12; off+8 <= len(data); {
		id, size := string(data[off:off+4]), int(le.Uint32(data[off+4:]))
		body := data[off+8:]
		if size > len(body) {
			size = len(body) // a header written before the stream ended
		}
		body = body[:size]
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, errors.New("wav: short fmt chunk")
			}
			format, channels = int(le.Uint16(body)), int(le.Uint16(body[2:]))
			rate, bits = int(le.Uint32(body[4:])), int(le.Uint16(body[14:]))
			if format == 0xFFFE && size >= 26 { // extensible: the sub-format's first word
				format = int(le.Uint16(body[24:]))
			}
		case "data":
			pcm = body
		}
		off += 8 + size + size&1
	}
	if pcm == nil || channels < 1 || channels > 32 || rate < 8000 || rate > 192000 {
		return nil, 0, fmt.Errorf("wav: no data, %d channels or %d Hz", channels, rate)
	}
	var sample func([]byte) float64
	switch {
	case format == 1 && bits == 8:
		sample = func(b []byte) float64 { return (float64(b[0]) - 128) / 128 }
	case format == 1 && bits == 16:
		sample = func(b []byte) float64 { return float64(int16(le.Uint16(b))) / 32768 }
	case format == 1 && bits == 24:
		sample = func(b []byte) float64 {
			return float64(int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24)>>8) / 8388608
		}
	case format == 1 && bits == 32:
		sample = func(b []byte) float64 { return float64(int32(le.Uint32(b))) / 2147483648 }
	case format == 3 && bits == 32:
		sample = func(b []byte) float64 { return float64(math.Float32frombits(le.Uint32(b))) }
	case format == 3 && bits == 64:
		sample = func(b []byte) float64 { return math.Float64frombits(le.Uint64(b)) }
	default:
		return nil, 0, fmt.Errorf("wav: format %d at %d bits is not PCM or float", format, bits)
	}
	width := bits / 8
	frame := width * channels
	mono = make([]float64, len(pcm)/frame)
	for i := range mono {
		var sum float64
		for c := range channels {
			sum += sample(pcm[i*frame+c*width:])
		}
		mono[i] = sum / float64(channels)
	}
	return mono, rate, nil
}

// WriteWAV emits samples as 16-bit stereo PCM. The sample path is floatToBytes
// — the same soft limiter, hard clip and L/R duplication the mixer uses — so an
// exported file is sample-identical to playback.
func WriteWAV(w io.Writer, samples []float64) error {
	if len(samples) > (math.MaxUint32-wavHeaderSize)/AudioBytesPerFrame {
		return fmt.Errorf("wav: %d samples exceeds the format limit", len(samples))
	}
	data := make([]byte, len(samples)*AudioBytesPerFrame)
	floatToBytes(samples, data)
	if err := writeWAVHeader(w, uint32(len(data))); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// writeWAVHeader emits the 44-byte canonical header for n bytes of PCM.
func writeWAVHeader(w io.Writer, n uint32) error {
	var h [wavHeaderSize]byte
	le := binary.LittleEndian
	copy(h[0:], "RIFF")
	le.PutUint32(h[4:], 36+n)
	copy(h[8:], "WAVEfmt ")
	le.PutUint32(h[16:], 16) // PCM fmt chunk size
	le.PutUint16(h[20:], 1)  // format: PCM
	le.PutUint16(h[22:], AudioChannels)
	le.PutUint32(h[24:], AudioSampleRate)
	le.PutUint32(h[28:], AudioSampleRate*AudioBytesPerFrame)
	le.PutUint16(h[32:], AudioBytesPerFrame)
	le.PutUint16(h[34:], AudioBitDepth)
	copy(h[36:], "data")
	le.PutUint32(h[40:], n)
	_, err := w.Write(h[:])
	return err
}

// wavSink is the "wav:<path>" backend: the mixer's byte stream captured to a
// file. The header is written with zero sizes at open and patched on Close, so
// a capture cut short by a crash still parses.
//
// The mutex is not optional. Write runs on the mix goroutine and Close on
// whoever calls Stop or failover, and Close seeks to the header — an
// interleaved Write would land at offset 0. It costs one uncontended
// lock per 50ms tick.
type wavSink struct {
	f      *os.File
	w      *bufio.Writer // a disk stall must not reach the mix goroutine each period
	mu     sync.Mutex
	n      uint64 // PCM bytes written
	closed bool
}

func newWAVSink(path string) (*wavSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := writeWAVHeader(f, 0); err != nil {
		f.Close()
		return nil, err
	}
	return &wavSink{f: f, w: bufio.NewWriterSize(f, wavBufferBytes)}, nil
}

// wavBufferBytes is about 1.5 s of audio between file writes.
const wavBufferBytes = 256 << 10

// Write runs on the mix goroutine. A write after Close reports success and
// discards: a closed sink is one the engine deliberately detached, and
// reporting an error there would set outBroken and push to errChan, making the
// supervisor fail over a second time for a backend it has already replaced.
// The dropped tail is at most one buffer.
func (s *wavSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return len(p), nil
	}
	n, err := s.w.Write(p)
	s.n += uint64(n)
	return n, err
}

// Close patches the header with the byte count. Idempotent: Stop and failover
// can both reach it.
func (s *wavSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	_ = s.w.Flush()
	n := min(s.n, math.MaxUint32-wavHeaderSize)
	if _, err := s.f.Seek(0, io.SeekStart); err == nil {
		writeWAVHeader(s.f, uint32(n))
	}
	return s.f.Close()
}

package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
)

const wavHeaderSize = 44

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
	return &wavSink{f: f}, nil
}

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
	n, err := s.f.Write(p)
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
	n := min(s.n, math.MaxUint32-wavHeaderSize)
	if _, err := s.f.Seek(0, io.SeekStart); err == nil {
		writeWAVHeader(s.f, uint32(n))
	}
	return s.f.Close()
}

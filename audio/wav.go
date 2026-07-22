package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
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
// a capture cut short by a crash still parses — players read zero frames rather
// than garbage.
type wavSink struct {
	f *os.File
	n uint64 // PCM bytes written; mixer-goroutine confined
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

// Write runs on the mixer goroutine.
func (s *wavSink) Write(p []byte) (int, error) {
	n, err := s.f.Write(p)
	s.n += uint64(n)
	return n, err
}

// Close patches the header. AudioEngine.Stop waits for the mix goroutine to
// return before calling it, so n is final.
func (s *wavSink) Close() error {
	n := s.n
	if n > math.MaxUint32-wavHeaderSize {
		n = math.MaxUint32 - wavHeaderSize
	}
	if _, err := s.f.Seek(0, io.SeekStart); err == nil {
		writeWAVHeader(s.f, uint32(n))
	}
	return s.f.Close()
}

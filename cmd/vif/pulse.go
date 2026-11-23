//go:build (linux || freebsd) && !vif_headless && !vif_nogui && !vif_noaudio

package main

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/lixenwraith/audio"
	"github.com/lixenwraith/vif/internal/parameter"
)

// The window plays through the sound server itself, where one answers: a machine
// that starts it need hold none of the players the engine finds by name
func init() { speakers = pulseSpeakers }

// PulseAudio's blocking client library, which PipeWire's pulse server is spoken
// to through as well
const libPulseSimple = "libpulse-simple.so.0"

// The stream's buffer attributes. The server asks its device for half of what the
// target holds past two requests, 12.5 ms as the engine's pacat line asks, and the
// stream holds the rest: two default periods and that, so a write waits only
// behind what a stall queued, and a stall queues no more. The prebuf is that
// line's too.
const (
	pulseTarget  = 125 * time.Millisecond
	pulseRequest = 50 * time.Millisecond
	pulsePrebuf  = 25 * time.Millisecond
)

// C int is int32, as in the window's bindings, and size_t uintptr; a null string
// is a zero uintptr, an empty Go string being an empty C one
var (
	paSimpleNew   func(server uintptr, name string, dir int32, device uintptr, stream string, spec *paSampleSpec, channels uintptr, attr *paBufferAttr, code *int32) uintptr
	paSimpleWrite func(s uintptr, data []byte, size uintptr, code *int32) int32
	paSimpleDrain func(s uintptr, code *int32) int32
	paSimpleFree  func(s uintptr)
)

const (
	paStreamPlayback = 1
	paSampleS16LE    = 3
	paDefault        = ^uint32(0) // a buffer attribute left to the server
)

type paSampleSpec struct {
	format   int32
	rate     uint32
	channels uint8
}

type paBufferAttr struct{ maxLength, targetLength, prebuf, minRequest, fragmentSize uint32 }

// pulseSpeakers opens the stream the mixer plays through. Nil leaves the engine
// to find a backend by name: where OSS is there to find, the library or one of
// its symbols is missing, or no server takes the stream.
func pulseSpeakers() io.WriteCloser {
	if _, err := audio.DetectBackends("oss"); err == nil {
		return nil
	}
	lib, err := purego.Dlopen(libPulseSimple, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	for name, fptr := range map[string]any{"pa_simple_new": &paSimpleNew, "pa_simple_write": &paSimpleWrite,
		"pa_simple_drain": &paSimpleDrain, "pa_simple_free": &paSimpleFree} {
		fn, err := purego.Dlsym(lib, name)
		if err != nil {
			return nil
		}
		purego.RegisterFunc(fptr, fn)
	}
	spec := paSampleSpec{format: paSampleS16LE, rate: audio.AudioSampleRate, channels: audio.AudioChannels}
	attr := paBufferAttr{maxLength: paDefault, targetLength: pulseBytes(pulseTarget), prebuf: pulseBytes(pulsePrebuf),
		minRequest: pulseBytes(pulseRequest), fragmentSize: paDefault}
	s := paSimpleNew(0, parameter.WindowTitle, paStreamPlayback, 0, "game", &spec, 0, &attr, nil)
	if s == 0 {
		return nil
	}
	return &pulseSpeaker{stream: s}
}

func pulseBytes(d time.Duration) uint32 {
	return uint32(audio.AudioSampleRate*d/time.Second) * audio.AudioBytesPerFrame
}

// pulseSpeaker is a playback stream on the sound server. A write blocks while the
// server holds its target length, which paces a mixer running ahead of the device.
type pulseSpeaker struct {
	mu     sync.Mutex // held through a write, so Close frees no stream a write is inside
	stream uintptr
}

func (p *pulseSpeaker) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream == 0 {
		return 0, io.ErrClosedPipe
	}
	if len(b) == 0 {
		return 0, nil
	}
	var code int32
	if paSimpleWrite(p.stream, b, uintptr(len(b)), &code) < 0 {
		return 0, fmt.Errorf("speakers: the sound server took no write, error %d", code)
	}
	return len(b), nil
}

// Close plays out what the mixer wrote and frees the stream. A write still inside
// the library is one its server never took, and the stream is left to the exit.
func (p *pulseSpeaker) Close() error {
	if !p.mu.TryLock() {
		return nil
	}
	defer p.mu.Unlock()
	if p.stream != 0 {
		paSimpleDrain(p.stream, nil)
		paSimpleFree(p.stream)
		p.stream = 0
	}
	return nil
}

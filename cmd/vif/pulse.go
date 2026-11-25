//go:build (linux || freebsd) && !vif_headless && !vif_nogui && !vif_noaudio

package main

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/lixenwraith/audio"
	"github.com/lixenwraith/vif/internal/parameter"
)

// The run plays through the sound server itself, where one answers: a machine
// that starts it need hold none of the players the engine finds by name
func init() { speakerOpens["pulse"] = pulseSpeakers }

// PulseAudio's blocking client library, which PipeWire's pulse server is spoken
// to through as well
const libPulseSimple = "libpulse-simple.so.0"

// The stream's buffer attributes. The server asks its device for half of what the
// target holds past two requests, 12.5 ms as the engine's pacat line asks, and the
// stream holds the rest: two default periods and that, so a period's write waits
// only where a stall has filled the stream, and no stall queues more. The prebuf
// is that line's too.
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

// pulseLoad binds the library's three calls once, for every stream opened after
var pulseLoad = sync.OnceValue(func() error {
	lib, err := purego.Dlopen(libPulseSimple, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return err
	}
	for name, fptr := range map[string]any{"pa_simple_new": &paSimpleNew, "pa_simple_write": &paSimpleWrite,
		"pa_simple_free": &paSimpleFree} {
		fn, err := purego.Dlsym(lib, name)
		if err != nil {
			return err
		}
		purego.RegisterFunc(fptr, fn)
	}
	return nil
})

// pulseSpeakers opens the stream the mixer plays through, kept playing across a
// server that goes and comes back. Where OSS is there to find it opens none
// unless -ab names it, since OSS comes first there.
func pulseSpeakers(forced bool) (io.WriteCloser, error) {
	if _, err := audio.DetectBackends("oss"); err == nil && !forced {
		return nil, errors.New("OSS plays here")
	}
	if err := pulseLoad(); err != nil {
		return nil, err
	}
	return reopening("pulse", pulseOpen)
}

func pulseOpen() (io.WriteCloser, error) {
	spec := paSampleSpec{format: paSampleS16LE, rate: audio.AudioSampleRate, channels: audio.AudioChannels}
	attr := paBufferAttr{maxLength: paDefault, targetLength: pulseBytes(pulseTarget), prebuf: pulseBytes(pulsePrebuf),
		minRequest: pulseBytes(pulseRequest), fragmentSize: paDefault}
	var code int32
	s := paSimpleNew(0, parameter.WindowTitle, paStreamPlayback, 0, "game", &spec, 0, &attr, &code)
	if s == 0 {
		return nil, fmt.Errorf("no sound server took the stream, error %d", code)
	}
	return &pulseStream{s}, nil
}

func pulseBytes(d time.Duration) uint32 {
	return uint32(audio.AudioSampleRate*d/time.Second) * audio.AudioBytesPerFrame
}

// pulseStream is a playback stream on the sound server. A write blocks while the
// server holds its target length, which paces a mixer running ahead of the device.
type pulseStream struct{ s uintptr }

func (p *pulseStream) Write(b []byte) (int, error) {
	var code int32
	if len(b) > 0 && paSimpleWrite(p.s, b, uintptr(len(b)), &code) < 0 {
		return 0, fmt.Errorf("the sound server took no write, error %d", code)
	}
	return len(b), nil
}

// Close frees the stream with no drain: a run fades out before its speakers close,
// and a server that stopped answering would hold the exit in pa_simple_drain
func (p *pulseStream) Close() error {
	paSimpleFree(p.s)
	return nil
}

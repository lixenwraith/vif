//go:build windows && !vif_headless && !vif_nogui && !vif_noaudio

package main

import (
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/lixenwraith/audio"
	"golang.org/x/sys/windows"
)

// The window plays through the system's own mixer, shared with every other
// program: Windows ships none of the players the engine finds by name
func init() {
	speakerOpens["wasapi"] = func(bool) (io.WriteCloser, error) {
		if s := wasapiSpeakers(); s != nil {
			return s, nil
		}
		return nil, errors.New("no output device answered")
	}
}

// wasapiStall is how long a device may take nothing before its stream is taken
// for lost, and a render thread not answer before a write or Close gives it up.
// wasapiWait is the longest the render thread sleeps without asking the device,
// which finds a stream that stopped calling, and wasapiRetry the least time
// between two opens.
const (
	wasapiStall = 2 * time.Second
	wasapiWait  = 200 * time.Millisecond
	wasapiRetry = time.Second
)

// What mmdeviceapi.h and audioclient.h name: the device enumerator's class, the
// interfaces asked for, the slots of the methods called in their tables, and a
// stream the device asks for by event in a format the system converts to its own
var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioRenderClient   = windows.GUID{Data1: 0xF294ACFC, Data2: 0x3146, Data3: 0x4483, Data4: [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}

	procCoCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")
)

const (
	comRelease                  = 2 // IUnknown
	enumGetDefaultAudioEndpoint = 4 // IMMDeviceEnumerator
	deviceActivate              = 3 // IMMDevice
	clientInitialize            = 3 // IAudioClient
	clientGetBufferSize         = 4
	clientGetCurrentPadding     = 6
	clientGetDevicePeriod       = 9
	clientStart                 = 10
	clientStop                  = 11
	clientSetEventHandle        = 13
	clientGetService            = 14
	renderGetBuffer             = 3 // IAudioRenderClient
	renderReleaseBuffer         = 4

	clsctxAll         = 0x17
	streamByEvent     = 0x00040000 // AUDCLNT_STREAMFLAGS_EVENTCALLBACK
	streamAutoConvert = 0x80000000 // AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM
	streamConvertRate = 0x08000000 // AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY
)

// comObject is a COM interface pointer: the object opens with its method table's
// address. A call converts its pointers inside SyscallN's own argument list, the
// one place a pointer to a local is held still for the call.
type comObject struct{ table *[15]uintptr }

func (o *comObject) release() {
	if o != nil {
		syscall.SyscallN(o.table[comRelease], uintptr(unsafe.Pointer(o)))
	}
}

// ok is whether a COM call succeeded: an HRESULT is negative on failure
func ok(hr, _ uintptr, _ syscall.Errno) bool { return int32(hr) >= 0 }

// waveFormat is WAVEFORMATEX, which Go lays out as the header packs it
type waveFormat struct {
	tag, channels     uint16
	rate, bytesPerSec uint32
	blockAlign, bits  uint16
	extra             uint16
}

// wasapiSpeaker is a shared stream on the default output device. A write is handed
// to the render thread, which gives the device what it asks for at each of its
// periods, and returns once the device holds it all: the mixer then renders its
// next period as late as the device allows, and nothing queues behind a stall.
type wasapiSpeaker struct {
	event windows.Handle // the device's call for frames, and Close's to the render thread
	done  chan struct{}  // closed once the render thread has let the device go
	stall *time.Timer    // bounds a write

	writing sync.Mutex // one write at a time, a mixer's that outlived its engine included
	mu      sync.Mutex
	pending []byte        // what the device has yet to take of the write in hand
	taken   chan struct{} // receives when another than its writer lets pending go
	playing bool          // a stream is open; with none a write is dropped, as by a null device
	closed  bool

	underruns atomic.Uint64 // times the device ran out of frames

	// The render thread's own
	client, render *comObject
	period, target uint32 // frames the device takes at a time, and the most it is left holding
	starved        bool
	opened, moved  time.Time // the last open, made or only tried, and when the device last had room
}

// wasapiSpeakers opens the default output device. Nil, where no device or no
// audio service answers, leaves the engine to find a backend by name.
func wasapiSpeakers() io.WriteCloser {
	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil
	}
	s := &wasapiSpeaker{event: event, done: make(chan struct{}), taken: make(chan struct{}, 1), stall: time.NewTimer(wasapiStall)}
	s.stall.Stop()
	opened := make(chan bool)
	go s.run(opened)
	if !<-opened {
		<-s.done
		windows.CloseHandle(event)
		return nil
	}
	return s
}

// run is the render thread. COM is initialized, used and released on one OS
// thread, so the goroutine keeps its thread, which ends with it. A stream that
// fails is opened again on whatever device is then the default.
func (s *wasapiSpeaker) run(opened chan<- bool) {
	runtime.LockOSThread()
	defer close(s.done)
	// 1 is S_FALSE: the thread held COM already, and that count is given back too
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil && err != syscall.Errno(1) {
		opened <- false
		return
	}
	defer windows.CoUninitialize()
	for first := true; ; first = false {
		live := s.open()
		if first {
			opened <- live
		}
		if live {
			s.play()
		}
		s.shut()
		if first && !live || !s.rest() {
			return
		}
	}
}

// open starts a stream on the default output device and reports whether it runs;
// what it leaves half made, shut releases. The buffer is left to the system, as a
// shared stream fed by event must leave it, and the sink keeps two of the
// device's periods in it: one being played, one against a render thread woken late.
func (s *wasapiSpeaker) open() bool {
	s.opened = time.Now()
	var enumerator, device *comObject
	if !ok(syscall.SyscallN(procCoCreateInstance.Addr(), uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enumerator)))) {
		return false
	}
	defer enumerator.release()
	if !ok(syscall.SyscallN(enumerator.table[enumGetDefaultAudioEndpoint], uintptr(unsafe.Pointer(enumerator)), 0, 0, uintptr(unsafe.Pointer(&device)))) {
		return false
	}
	defer device.release()
	if !ok(syscall.SyscallN(device.table[deviceActivate], uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0,
		uintptr(unsafe.Pointer(&s.client)))) {
		return false
	}
	c := s.client
	format := waveFormat{tag: 1, channels: audio.AudioChannels, rate: audio.AudioSampleRate,
		bytesPerSec: audio.AudioSampleRate * audio.AudioBytesPerFrame, blockAlign: audio.AudioBytesPerFrame, bits: audio.AudioBitDepth}
	var period int64 // in 100 ns
	var size uint32
	if !ok(syscall.SyscallN(c.table[clientGetDevicePeriod], uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(&period)), 0)) || period <= 0 ||
		!ok(syscall.SyscallN(c.table[clientInitialize], uintptr(unsafe.Pointer(c)), 0, streamByEvent|streamAutoConvert|streamConvertRate,
			0, 0, uintptr(unsafe.Pointer(&format)), 0)) ||
		!ok(syscall.SyscallN(c.table[clientSetEventHandle], uintptr(unsafe.Pointer(c)), uintptr(s.event))) ||
		!ok(syscall.SyscallN(c.table[clientGetBufferSize], uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(&size)))) ||
		!ok(syscall.SyscallN(c.table[clientGetService], uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(&iidIAudioRenderClient)), uintptr(unsafe.Pointer(&s.render)))) ||
		!ok(syscall.SyscallN(c.table[clientStart], uintptr(unsafe.Pointer(c)))) {
		return false
	}
	s.period = uint32(period * audio.AudioSampleRate / 10_000_000)
	s.target, s.starved, s.moved = min(2*s.period, size), true, s.opened
	s.mu.Lock()
	s.playing = !s.closed
	s.mu.Unlock()
	return true
}

// play gives the device frames each time it asks, until it fails, as one
// unplugged does, or the sink closes
func (s *wasapiSpeaker) play() {
	for {
		// A goroutine that leaves the system only to wait in it again never passes
		// the scheduler, whose monitor then takes its processor at every wait and
		// polls at full rate for the next: a yield a period lets the monitor sleep
		runtime.Gosched()
		windows.WaitForSingleObject(s.event, uint32(wasapiWait/time.Millisecond))
		var queued uint32
		if s.isClosed() || !ok(syscall.SyscallN(s.client.table[clientGetCurrentPadding], uintptr(unsafe.Pointer(s.client)), uintptr(unsafe.Pointer(&queued)))) {
			return
		}
		frames, room := uint32(0), max(s.target, queued)-queued
		if room > 0 {
			s.moved = time.Now()
			var data *byte
			if !ok(syscall.SyscallN(s.render.table[renderGetBuffer], uintptr(unsafe.Pointer(s.render)), uintptr(room), uintptr(unsafe.Pointer(&data)))) {
				return
			}
			s.mu.Lock()
			n := copy(unsafe.Slice(data, room*audio.AudioBytesPerFrame), s.pending)
			if s.pending = s.pending[n:]; len(s.pending) == 0 {
				s.drop()
			}
			s.mu.Unlock()
			frames = uint32(n / audio.AudioBytesPerFrame)
			if !ok(syscall.SyscallN(s.render.table[renderReleaseBuffer], uintptr(unsafe.Pointer(s.render)), uintptr(frames), 0)) {
				return
			}
		} else if time.Since(s.moved) > wasapiStall {
			return // a device that plays nothing, as one another program took for itself
		}
		// Left with less than a period, the device plays silence for the rest of
		// its next: an underrun, counted once however long it lasts
		starved := queued+frames < s.period
		if starved && !s.starved {
			s.underruns.Add(1)
		}
		s.starved = starved
	}
}

// shut stops the stream and lets its interfaces go; a write in hand is dropped
func (s *wasapiSpeaker) shut() {
	s.mu.Lock()
	s.playing = false
	s.drop()
	s.mu.Unlock()
	if s.client != nil {
		syscall.SyscallN(s.client.table[clientStop], uintptr(unsafe.Pointer(s.client)))
	}
	s.render.release()
	s.client.release()
	s.render, s.client = nil, nil
}

// rest waits out what is left of wasapiRetry since the last open, so a device that
// fails as it opens is not asked in a spin, and reports whether the sink is still open
func (s *wasapiSpeaker) rest() bool {
	for until := s.opened.Add(wasapiRetry); !s.isClosed(); {
		wait := time.Until(until)
		if wait <= 0 {
			return true
		}
		windows.WaitForSingleObject(s.event, uint32(wait/time.Millisecond)+1)
	}
	return false
}

func (s *wasapiSpeaker) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// drop lets go of the write in hand and tells its writer; mu is held
func (s *wasapiSpeaker) drop() {
	if s.pending != nil {
		s.pending = nil
		s.taken <- struct{}{}
	}
}

// Write returns once the device has taken b, which paces the mixer by the
// device's clock; with no stream open it returns at once and the mixer's own
// ticker paces it. An error is a render thread that did not answer, which the
// engine goes silent on: a stream that fails or stalls is reopened, not reported.
func (s *wasapiSpeaker) Write(b []byte) (int, error) {
	s.writing.Lock()
	defer s.writing.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if whole := len(b) - len(b)%audio.AudioBytesPerFrame; s.playing && whole > 0 {
		s.pending = b[:whole]
	}
	handed := s.pending != nil
	s.mu.Unlock()
	if !handed {
		return len(b), nil
	}
	// The bound is waited out twice: the first may pass in no time at all, on a
	// clock that counted a sleep the render thread is only now waking from
	for range 2 {
		s.stall.Reset(wasapiStall)
		select {
		case <-s.taken:
			s.stall.Stop()
			return len(b), nil
		case <-s.stall.C:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil { // let go as the bound passed
		<-s.taken
		return len(b), nil
	}
	s.pending = nil
	return 0, errors.New("speakers: the device took no write")
}

// Close stops the stream and releases the device and COM on the render thread,
// which a write in hand returns for. A render thread that does not answer is
// inside a call the system never returned from, and is left to the process's exit.
func (s *wasapiSpeaker) Close() error {
	s.mu.Lock()
	first := !s.closed
	s.closed = true
	s.drop()
	s.mu.Unlock()
	if !first {
		return nil
	}
	windows.SetEvent(s.event)
	hung := time.NewTimer(wasapiStall)
	defer hung.Stop()
	select {
	case <-s.done:
		windows.CloseHandle(s.event)
	case <-hung.C:
	}
	return nil
}

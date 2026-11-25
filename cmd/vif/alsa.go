//go:build linux && !vif_headless && !vif_noaudio

package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lixenwraith/audio"
)

// The run plays straight to a sound card through the kernel's own interface, with
// no library and no player, so a machine with no sound server sounds as well
func init() { speakerOpens["alsa"] = alsaSpeakers }

// alsaSpeakers opens the first card that takes the mixer's stream, kept playing
// across its loss. Where a sound server runs it opens none unless -ab names it:
// the server owns the card, and plays the run through it instead.
func alsaSpeakers(forced bool) (io.WriteCloser, error) {
	if !forced && soundServer() {
		return nil, errors.New("a sound server runs")
	}
	return reopening("alsa", openPCM)
}

// soundServer is whether PipeWire or a PulseAudio server listens for this user
func soundServer() bool {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	for _, name := range []string{"pipewire-0", "pulse/native"} {
		if _, err := os.Stat(filepath.Join(dir, name)); dir != "" && err == nil {
			return true
		}
	}
	return os.Getenv("PULSE_SERVER") != ""
}

// openPCM opens the first playback device, by card then device, that takes 16-bit
// stereo at the mixer's rate. A device another program holds answers busy at once.
func openPCM() (io.WriteCloser, error) {
	paths, _ := filepath.Glob("/dev/snd/pcmC*D*p")
	if len(paths) == 0 {
		return nil, errors.New("no playback device in /dev/snd")
	}
	slices.SortFunc(paths, func(a, b string) int {
		var ac, ad, bc, bd int
		fmt.Sscanf(filepath.Base(a), "pcmC%dD%dp", &ac, &ad)
		fmt.Sscanf(filepath.Base(b), "pcmC%dD%dp", &bc, &bd)
		return cmp.Or(cmp.Compare(ac, bc), cmp.Compare(ad, bd))
	})
	var why []string
	for _, path := range paths {
		f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			why = append(why, err.Error())
			continue
		}
		p := &pcm{f: f}
		if err := p.size(int(audio.AudioSampleRate * audio.AudioBufferDuration / time.Second)); err != nil {
			f.Close()
			why = append(why, path+": "+err.Error())
			continue
		}
		return p, nil
	}
	return nil, errors.New(strings.Join(why, "; "))
}

// pcm is a playback stream on a card. Its file is non-blocking, so a write waits
// in Go's poller for room in the ring, never in the kernel, and an open finds a
// device in use busy rather than waiting for it.
type pcm struct {
	f       *os.File
	frames  int    // a write's frames, which the ring was sized for
	silence []byte // one write of it leads the first after each prepare
	rolled  bool
}

// pcmParams is struct snd_pcm_hw_params as asound.h lays it out on 64-bit Linux:
// a mask a parameter, from access, then an interval a parameter, from sample bits
type pcmParams struct {
	flags     uint32
	masks     [3][8]uint32
	_         [5][8]uint32
	intervals [12]struct{ min, max, bits uint32 }
	_         [9][3]uint32
	rmask     uint32
	cmask     uint32
	info      uint32
	_         [3]uint32 // msbits and the rate, as the kernel reports them
	_         uint64    // the chip's FIFO
	_         [64]byte
}

// The parameters asked for, by asound.h's numbering, and the PCM ioctls used
const (
	pcmAccess, pcmFormat, pcmSubformat = 0, 1, 2
	pcmChannels, pcmRate               = 10, 11
	pcmPeriodTime, pcmBufferTime       = 12, 16

	pcmInterleaved = 3 // SNDRV_PCM_ACCESS_RW_INTERLEAVED
	pcmS16LE       = 2 // SNDRV_PCM_FORMAT_S16_LE

	ioPrepare = 'A'<<8 | 0x40
	ioDrop    = 'A'<<8 | 0x43
)

// ioHwParams is _IOWR('A', 0x11, struct snd_pcm_hw_params)
const ioHwParams = 3<<30 | uintptr(unsafe.Sizeof(pcmParams{}))<<16 | 'A'<<8 | 0x11

// size stops the device and sets it up for writes of frames: a ring of two to two
// and a half writes, woken twice to four times a write, then a looser one where
// the card holds no such ring. The ring bounds what a slow device clock queues.
func (p *pcm) size(frames int) error {
	us := uint32(frames * 1e6 / audio.AudioSampleRate)
	var err error
	for _, ring := range [][4]uint32{{us / 4, us / 2, 2 * us, 5 * us / 2}, {us / 8, us, 2 * us, 8 * us}} {
		var hw pcmParams
		for i := range hw.intervals {
			hw.intervals[i].max = ^uint32(0)
		}
		hw.masks[pcmAccess][0], hw.masks[pcmFormat][0], hw.masks[pcmSubformat][0] = 1<<pcmInterleaved, 1<<pcmS16LE, 1
		set := func(param int, lo, hi uint32) { hw.intervals[param-8].min, hw.intervals[param-8].max = lo, hi }
		set(pcmChannels, audio.AudioChannels, audio.AudioChannels)
		set(pcmRate, audio.AudioSampleRate, audio.AudioSampleRate)
		set(pcmPeriodTime, ring[0], ring[1])
		set(pcmBufferTime, ring[2], ring[3])
		hw.rmask, hw.info = ^uint32(0), ^uint32(0)
		p.ioctl(ioDrop, nil)
		if err = p.ioctl(ioHwParams, unsafe.Pointer(&hw)); err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	p.frames, p.silence = frames, make([]byte, frames*audio.AudioBytesPerFrame)
	return p.prepare()
}

func (p *pcm) prepare() error {
	p.rolled = false
	return p.ioctl(ioPrepare, nil)
}

// Write hands the device b once it has room. An underrun, or a machine that slept,
// prepares the device again and leads with a write of silence, so a period the
// mixer renders late finds the device still playing; one that recurs at once is
// a device lost.
func (p *pcm) Write(b []byte) (int, error) {
	if frames := len(b) / audio.AudioBytesPerFrame; frames != p.frames {
		if err := p.size(frames); err != nil {
			return 0, err
		}
	}
	n := 0
	for tries := 0; ; tries++ {
		var err error
		if !p.rolled {
			_, err = p.f.Write(p.silence)
			p.rolled = err == nil
		}
		if err == nil {
			var m int
			m, err = p.f.Write(b[n:])
			if n += m; err == nil {
				return n, nil
			}
		}
		if tries == 2 || !errors.Is(err, syscall.EPIPE) && !errors.Is(err, syscall.ESTRPIPE) {
			return n, err
		}
		if err := p.prepare(); err != nil {
			return n, err
		}
	}
}

// Close stops the device where it is: a run fades out before its speakers close
func (p *pcm) Close() error { return p.f.Close() }

// ioctl calls the device through the file's descriptor without taking it out of
// non-blocking mode, as File.Fd would
func (p *pcm) ioctl(req uintptr, arg unsafe.Pointer) error {
	rc, err := p.f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}

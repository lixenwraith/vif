//go:build linux && !vif_headless && !vif_noaudio

package main

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
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

// openPCM opens the first playback device, by card then device, that takes stereo
// in 16 or 32 bits at the mixer's rate or above. A device another program holds
// answers busy at once.
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
	rate    int        // the card's
	wide    bool       // the card takes 32-bit samples
	rs      *resampler // nil where the card takes the mixer's rate
	out     []byte     // a write as the card takes it
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

	pcmInterleaved = 3  // SNDRV_PCM_ACCESS_RW_INTERLEAVED
	pcmS16LE       = 2  // SNDRV_PCM_FORMAT_S16_LE
	pcmS32LE       = 10 // SNDRV_PCM_FORMAT_S32_LE

	ioPrepare = 'A'<<8 | 0x40
	ioDrop    = 'A'<<8 | 0x43
)

// ioHwParams is _IOWR('A', 0x11, struct snd_pcm_hw_params)
const ioHwParams = 3<<30 | uintptr(unsafe.Sizeof(pcmParams{}))<<16 | 'A'<<8 | 0x11

// size stops the device and sets it up for writes of frames: 16 bits where it
// takes them, the least rate from the mixer's up, and a ring of two to two and a
// half writes woken twice to four times a write, then a looser one where the card
// holds no such ring. The ring bounds what a slow device clock queues.
func (p *pcm) size(frames int) error {
	us := uint32(frames * 1e6 / audio.AudioSampleRate)
	var hw pcmParams
	var err error
	for _, ring := range [][4]uint32{{us / 4, us / 2, 2 * us, 5 * us / 2}, {us / 8, us, 2 * us, 8 * us}} {
		hw = pcmParams{}
		for i := range hw.intervals {
			hw.intervals[i].max = ^uint32(0)
		}
		hw.masks[pcmAccess][0], hw.masks[pcmFormat][0], hw.masks[pcmSubformat][0] = 1<<pcmInterleaved, 1<<pcmS16LE|1<<pcmS32LE, 1
		set := func(param int, lo, hi uint32) { hw.intervals[param-8].min, hw.intervals[param-8].max = lo, hi }
		set(pcmChannels, audio.AudioChannels, audio.AudioChannels)
		set(pcmRate, audio.AudioSampleRate, ^uint32(0))
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
	rate := int(hw.intervals[pcmRate-8].min)
	if rate != p.rate {
		p.rs = nil
		if rate != audio.AudioSampleRate {
			if p.rs, err = newResampler(rate); err != nil {
				return err
			}
		}
	}
	p.rate, p.wide = rate, hw.masks[pcmFormat][0]&(1<<pcmS32LE) != 0
	frame := audio.AudioBytesPerFrame
	if p.wide {
		frame *= 2
	}
	p.frames, p.silence = frames, make([]byte, (frames*rate+audio.AudioSampleRate-1)/audio.AudioSampleRate*frame)
	return p.prepare()
}

func (p *pcm) prepare() error {
	p.rolled = false
	return p.ioctl(ioPrepare, nil)
}

// Write hands the device b once it has room, at the card's rate and width. An
// underrun, or a machine that slept, prepares the device again and leads with a
// write of silence, so a period the mixer renders late finds the device still
// playing; one that recurs at once is a device lost.
func (p *pcm) Write(b []byte) (int, error) {
	if frames := len(b) / audio.AudioBytesPerFrame; frames != p.frames {
		if err := p.size(frames); err != nil {
			return 0, err
		}
	}
	data := b
	switch {
	case p.rs != nil:
		p.out = p.rs.write(p.out[:0], b, p.wide)
		data = p.out
	case p.wide:
		p.out = p.out[:0]
		for i := 0; i+1 < len(b); i += 2 {
			p.out = binary.LittleEndian.AppendUint32(p.out, uint32(binary.LittleEndian.Uint16(b[i:]))<<16)
		}
		data = p.out
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
			m, err = p.f.Write(data[n:])
			if n += m; err == nil {
				return len(b), nil
			}
		}
		if tries == 2 || !errors.Is(err, syscall.EPIPE) && !errors.Is(err, syscall.ESTRPIPE) {
			return 0, err
		}
		if err := p.prepare(); err != nil {
			return 0, err
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

// resampleTaps is the mixer's frames an output frame reads. With a Kaiser window
// of resampleBeta the band holds flat to 18 kHz and 3 dB down at 20, and the image
// a tone under 20 kHz leaves at the card's rate lies 76 dB or more under it.
const (
	resampleTaps = 32
	resampleBeta = 7.5
)

// resampler takes the mixer's frames to a card's higher rate through a windowed
// sinc: a row of taps for each of the up phases between two input frames, every
// output stepping down of them, so the two rates never drift apart. Each row sums
// to one, so no phase plays louder than another and nothing clicks between writes.
type resampler struct {
	up, down int       // the card's rate and the mixer's over their divisor
	taps     []float32 // resampleTaps a phase
	in       []float32 // the input frames the next output reads, both ears interleaved
	pos, ph  int       // the newest input frame the next output reads, and its phase
}

func newResampler(rate int) (*resampler, error) {
	g := gcd(rate, audio.AudioSampleRate)
	r := &resampler{up: rate / g, down: audio.AudioSampleRate / g, pos: resampleTaps - 1}
	if r.up > 1024 {
		return nil, fmt.Errorf("no resampling to %d Hz", rate)
	}
	r.in = make([]float32, 2*(resampleTaps-1))
	r.taps = make([]float32, r.up*resampleTaps)
	cut := 0.47 // cycles a mixer frame: under its Nyquist, which a higher rate's exceeds
	row := make([]float64, resampleTaps)
	for ph := range r.up {
		var sum float64
		for j := range row {
			x := float64(resampleTaps/2-1-j) + float64(ph)/float64(r.up)
			row[j] = kaiser(x/(resampleTaps/2), resampleBeta)
			if x != 0 {
				row[j] *= math.Sin(2*math.Pi*cut*x) / (math.Pi * x)
			} else {
				row[j] *= 2 * cut
			}
			sum += row[j]
		}
		for j, v := range row {
			r.taps[ph*resampleTaps+j] = float32(v / sum)
		}
	}
	return r, nil
}

// write appends to out the card's frames that b, the mixer's, completes, in 16
// bits or, wide, 32
func (r *resampler) write(out, b []byte, wide bool) []byte {
	for i := 0; i+1 < len(b); i += 2 {
		r.in = append(r.in, float32(int16(binary.LittleEndian.Uint16(b[i:]))))
	}
	for n := len(r.in) / 2; r.pos < n; {
		taps := r.taps[r.ph*resampleTaps : (r.ph+1)*resampleTaps]
		in := r.in[2*(r.pos-resampleTaps+1):]
		var left, right float32
		for j, c := range taps {
			left += c * in[2*j]
			right += c * in[2*j+1]
		}
		for _, v := range [2]float32{left, right} {
			s := int32(math.Round(float64(max(-32768, min(32767, v)))))
			if wide {
				out = binary.LittleEndian.AppendUint32(out, uint32(s)<<16)
			} else {
				out = binary.LittleEndian.AppendUint16(out, uint16(s))
			}
		}
		r.ph += r.down
		r.pos, r.ph = r.pos+r.ph/r.up, r.ph%r.up
	}
	keep := r.pos - resampleTaps + 1
	r.in = r.in[:copy(r.in, r.in[2*keep:])]
	r.pos -= keep
	return out
}

// kaiser is the Kaiser window of beta at x, from -1 to 1
func kaiser(x, beta float64) float64 {
	i0 := func(v float64) float64 {
		sum, term := 1.0, 1.0
		for k := 1; term > 1e-12*sum; k++ {
			term *= v * v / (4 * float64(k*k))
			sum += term
		}
		return sum
	}
	return i0(beta*math.Sqrt(max(0, 1-x*x))) / i0(beta)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
